package handler

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/daniellavrushin/b4/log"
)

const (
	repoOwner = "DanielLavrushin"
	repoName  = "b4"
)

var (
	githubAPIBase = "https://api.github.com"
	githubRawBase = "https://raw.githubusercontent.com"
	githubWebBase = "https://github.com"
	mirrorScheme  = "https://"
)

var b4Mirrors = []string{
	"https://proxy.b4core.app",
	"https://proxy2.b4core.app",
}

func normalizeMirror(raw string) string {
	m := strings.TrimSpace(raw)
	if m == "" {
		return ""
	}
	m = strings.TrimRight(m, "/")
	if !strings.HasPrefix(m, mirrorScheme) {
		return ""
	}

	for _, r := range m {
		if r < 0x20 || r == 0x7f || unicode.IsSpace(r) {
			return ""
		}
	}

	u, err := url.Parse(m)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return ""
	}

	return m
}

func mergeMirrors(configured []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(configured)+len(b4Mirrors))

	for _, list := range [][]string{configured, b4Mirrors} {
		for _, raw := range list {
			m := normalizeMirror(raw)
			if m == "" || seen[m] {
				continue
			}
			seen[m] = true
			out = append(out, m)
		}
	}

	return out
}

func (api *API) updateMirrors() []string {
	cfg := api.getCfg()
	if cfg == nil {
		return mergeMirrors(nil)
	}
	return mergeMirrors(cfg.System.Update.Mirrors)
}

var mirrorOwners = []string{
	repoOwner,
	"Loyalsoldier",
	"runetfreedom",
	"XTLS",
	"Flowseal",
}

var mirrorClient = &http.Client{
	Timeout: 10 * time.Minute,
	Transport: &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		ExpectContinueTimeout: time.Second,
	},
}

func isMirrorable(rawURL string) bool {
	for _, owner := range mirrorOwners {
		for _, prefix := range []string{
			githubRawBase + "/" + owner + "/",
			githubWebBase + "/" + owner + "/",
			githubAPIBase + "/repos/" + owner + "/",
		} {
			if strings.HasPrefix(rawURL, prefix) {
				return true
			}
		}
	}
	return false
}

func mirrorURL(base, rawURL string) string {
	return base + "/github/" + rawURL
}

func mirrorAlive(base string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/b4/health", nil)
	if err != nil {
		return false
	}

	resp, err := mirrorClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 64))

	return resp.StatusCode == http.StatusOK
}

func downloadFileMirrored(url, destPath string, mirrors []string) (int64, error) {
	size, err := downloadFile(url, destPath)
	if err == nil {
		return size, nil
	}
	if !isMirrorable(url) {
		return 0, err
	}

	log.Warnf("Direct download of %s failed: %v", url, err)

	for _, base := range mirrors {
		if !mirrorAlive(base) {
			continue
		}
		size, mirrorErr := downloadFile(mirrorURL(base, url), destPath)
		if mirrorErr == nil {
			log.Infof("Downloaded %s via mirror %s", url, base)
			return size, nil
		}
		log.Warnf("Mirror %s failed for %s: %v", base, url, mirrorErr)
	}

	return 0, err
}

func fetchBytes(ctx context.Context, url string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "b4/"+Version)

	resp, err := mirrorClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %s", url, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s returned more than the %d byte limit", url, limit)
	}
	if len(body) == 0 {
		return nil, fmt.Errorf("%s returned an empty body", url)
	}

	return body, nil
}

func fetchBytesMirrored(ctx context.Context, url string, limit int64, mirrorPaths []string, mirrors []string) ([]byte, error) {
	body, err := fetchBytes(ctx, url, limit)
	if err == nil {
		return body, nil
	}
	firstErr := err

	for _, base := range mirrors {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		if !mirrorAlive(base) {
			continue
		}

		candidates := []string{}
		for _, p := range mirrorPaths {
			candidates = append(candidates, base+p)
		}
		if isMirrorable(url) {
			candidates = append(candidates, mirrorURL(base, url))
		}

		for _, candidate := range candidates {
			body, mirrorErr := fetchBytes(ctx, candidate, limit)
			if mirrorErr == nil {
				return body, nil
			}
			log.Warnf("Mirror %s failed for %s: %v", base, url, mirrorErr)
		}
	}

	return nil, firstErr
}
