package handler

import (
	"context"
	"net/http"
	"sync"
	"time"

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
)

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
// @Description from a LAN client whose own uplink cannot reach GitHub, and so the whole
// @Description fleet shares one upstream request instead of spending a per-IP rate limit.
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

	if body, fresh := releasesCache.get(releasesTTL); fresh {
		writeCachedJSON(w, body, "HIT")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), upstreamFetchTTL)
	defer cancel()

	url := githubAPIBase + "/repos/" + repoOwner + "/" + repoName + "/releases?per_page=25"
	body, err := fetchBytesMirrored(ctx, url, releasesLimit, []string{"/b4/api/releases"}, api.updateMirrors())
	if err != nil {
		if stale, _ := releasesCache.get(0); stale != nil {
			log.Warnf("Release list refresh failed, serving cached copy: %v", err)
			writeCachedJSON(w, stale, "STALE")
			return
		}
		log.Warnf("Failed to fetch release list: %v", err)
		writeJsonError(w, http.StatusBadGateway, "Could not reach GitHub or any b4 mirror")
		return
	}

	releasesCache.put(body)
	writeCachedJSON(w, body, "MISS")
}

// @Summary Fetch the localized changelog
// @Tags System
// @Produce plain
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

	cache := changelogCacheFor(file)
	if body, fresh := cache.get(changelogTTL); fresh {
		writeCachedText(w, body, "HIT")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), upstreamFetchTTL)
	defer cancel()

	url := githubRawBase + "/" + repoOwner + "/" + repoName + "/main/" + file
	body, err := fetchBytesMirrored(ctx, url, changelogLimit, []string{"/b4/raw/main/" + file}, api.updateMirrors())
	if err != nil {
		if stale, _ := cache.get(0); stale != nil {
			writeCachedText(w, stale, "STALE")
			return
		}
		log.Warnf("Failed to fetch %s: %v", file, err)
		writeJsonError(w, http.StatusBadGateway, "Could not reach GitHub or any b4 mirror")
		return
	}

	cache.put(body)
	writeCachedText(w, body, "MISS")
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
