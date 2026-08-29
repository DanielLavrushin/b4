package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"github.com/daniellavrushin/b4/log"
)

const (
	releasesTTL      = 15 * time.Minute
	releasesLimit    = 4 << 20
	changelogTTL     = 30 * time.Minute
	changelogLimit   = 2 << 20
	upstreamFetchTTL = 45 * time.Second
)

type upstreamCache struct {
	mu      sync.Mutex
	body    []byte
	fetched time.Time
}

var (
	releasesCache  upstreamCache
	changelogCache = map[string]*upstreamCache{}
	changelogMu    sync.Mutex
	upstreamGroup  singleflight.Group
)

// fetchCached serves the cached copy while it is fresh, and otherwise refreshes it.
// Concurrent callers that arrive on an expired entry share a single refresh rather than
// each opening their own, which keeps a burst of browser tabs from allocating the response
// several times over on a router and from spending the upstream rate limit N times.
func fetchCached(
	c *upstreamCache,
	key string,
	ttl time.Duration,
	fetch func(context.Context) ([]byte, error),
) ([]byte, string, error) {
	if body, fresh := c.get(ttl); fresh {
		return body, "HIT", nil
	}

	v, err, shared := upstreamGroup.Do(key, func() (interface{}, error) {
		if body, fresh := c.get(ttl); fresh {
			return body, nil
		}

		ctx, cancel := context.WithTimeout(context.Background(), upstreamFetchTTL)
		defer cancel()

		body, err := fetch(ctx)
		if err != nil {
			return nil, err
		}
		c.put(body)
		return body, nil
	})

	if err == nil {
		status := "MISS"
		if shared {
			status = "SHARED"
		}
		return v.([]byte), status, nil
	}

	if stale, _ := c.get(0); stale != nil {
		log.Warnf("Refresh of %s failed, serving the cached copy: %v", key, err)
		return stale, "STALE", nil
	}

	return nil, "", err
}

func (c *upstreamCache) get(ttl time.Duration) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.body != nil && time.Since(c.fetched) < ttl {
		return c.body, true
	}
	return c.body, false
}

func (c *upstreamCache) put(body []byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.body = body
	c.fetched = time.Now()
}

func changelogCacheFor(file string) *upstreamCache {
	changelogMu.Lock()
	defer changelogMu.Unlock()
	if c, ok := changelogCache[file]; ok {
		return c
	}
	c := &upstreamCache{}
	changelogCache[file] = c
	return c
}

// @Summary List b4 releases
// @Description Release list fetched by the service rather than the browser, so it works
// @Description from a LAN client whose own uplink cannot reach GitHub. The answer is cached
// @Description and concurrent callers share a single refresh, so a burst of open tabs costs
// @Description one upstream request rather than one each.
// @Tags System
// @Produce json
// @Success 200 {array} object
// @Failure 502 {object} map[string]string
// @Security BearerAuth
// @Router /system/releases [get]
func (api *API) handleReleases(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	url := githubAPIBase + "/repos/" + repoOwner + "/" + repoName + "/releases?per_page=25"
	mirrors := api.updateMirrors()

	body, status, err := fetchCached(&releasesCache, "releases", releasesTTL,
		func(ctx context.Context) ([]byte, error) {
			return fetchBytesMirrored(ctx, url, releasesLimit, []string{"/b4/api/releases"}, mirrors)
		})
	if err != nil {
		log.Warnf("Failed to fetch release list: %v", err)
		writeJsonError(w, http.StatusBadGateway, "Could not reach GitHub or any b4 mirror")
		return
	}

	writeCachedJSON(w, body, status)
}

// @Summary Fetch the localized changelog
// @Tags System
// @Produce text/markdown
// @Param lang query string false "Language code, ru for the Russian changelog"
// @Success 200 {string} string
// @Failure 502 {object} map[string]string
// @Security BearerAuth
// @Router /system/changelog [get]
func (api *API) handleChangelog(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}

	file := "changelog.md"
	if r.URL.Query().Get("lang") == "ru" {
		file = "changelog_ru.md"
	}

	url := githubRawBase + "/" + repoOwner + "/" + repoName + "/main/" + file
	mirrors := api.updateMirrors()

	body, status, err := fetchCached(changelogCacheFor(file), "changelog:"+file, changelogTTL,
		func(ctx context.Context) ([]byte, error) {
			return fetchBytesMirrored(ctx, url, changelogLimit, []string{"/b4/raw/main/" + file}, mirrors)
		})
	if err != nil {
		log.Warnf("Failed to fetch %s: %v", file, err)
		writeJsonError(w, http.StatusBadGateway, "Could not reach GitHub or any b4 mirror")
		return
	}

	writeCachedText(w, body, status)
}

func writeCachedJSON(w http.ResponseWriter, body []byte, status string) {
	setJsonHeader(w)
	w.Header().Set("X-B4-Cache", status)
	w.Write(body)
}

func writeCachedText(w http.ResponseWriter, body []byte, status string) {
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("X-B4-Cache", status)
	w.Write(body)
}
