package handler

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func newMirrorTestAPI(t *testing.T, configured ...string) *API {
	t.Helper()

	cfg := &config.Config{}
	cfg.System.Update.Mirrors = configured

	mux := http.NewServeMux()
	api := &API{mux: mux, cfgPtr: testCfgPtr(cfg)}
	api.mux.HandleFunc("/api/system/releases", api.handleReleases)
	api.mux.HandleFunc("/api/system/changelog", api.handleChangelog)

	resetUpstreamCaches(t)
	return api
}

func resetUpstreamCaches(t *testing.T) {
	t.Helper()

	releasesCache.mu.Lock()
	releasesCache.body = nil
	releasesCache.fetched = releasesCache.fetched.Add(0)
	releasesCache.mu.Unlock()

	changelogMu.Lock()
	changelogCache = map[string]*upstreamCache{}
	changelogMu.Unlock()

	t.Cleanup(func() {
		releasesCache.mu.Lock()
		releasesCache.body = nil
		releasesCache.mu.Unlock()

		changelogMu.Lock()
		changelogCache = map[string]*upstreamCache{}
		changelogMu.Unlock()
	})
}

func swapMirrors(t *testing.T, mirrors []string) {
	t.Helper()
	prev := b4Mirrors
	b4Mirrors = mirrors
	t.Cleanup(func() { b4Mirrors = prev })

	prevScheme := mirrorScheme
	mirrorScheme = "http://"
	t.Cleanup(func() { mirrorScheme = prevScheme })
}

func swapBases(t *testing.T, apiBase, rawBase, webBase string) {
	t.Helper()
	prevAPI, prevRaw, prevWeb := githubAPIBase, githubRawBase, githubWebBase
	githubAPIBase, githubRawBase, githubWebBase = apiBase, rawBase, webBase
	t.Cleanup(func() { githubAPIBase, githubRawBase, githubWebBase = prevAPI, prevRaw, prevWeb })
}

func deadServerURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := srv.URL
	srv.Close()
	return url
}

func TestIsMirrorableCoversEveryAllowlistedOwner(t *testing.T) {
	swapBases(t, "https://api.github.com", "https://raw.githubusercontent.com", "https://github.com")

	mirrorable := []string{
		"https://github.com/DanielLavrushin/b4/releases/download/v1.0.0/b4-linux-arm64.tar.gz",
		"https://raw.githubusercontent.com/DanielLavrushin/b4/main/install.sh",
		"https://api.github.com/repos/DanielLavrushin/b4/releases/latest",
		"https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geoip.dat",
		"https://raw.githubusercontent.com/runetfreedom/russia-v2ray-rules-dat/release/geosite.dat",
		"https://raw.githubusercontent.com/Flowseal/tg-ws-proxy/main/.github/cfproxy-domains.txt",
		"https://github.com/XTLS/Xray-core/releases/latest/download/x.zip",
	}
	for _, url := range mirrorable {
		if !isMirrorable(url) {
			t.Errorf("expected %s to be mirrorable", url)
		}
	}

	notMirrorable := []string{
		"https://github.com/torvalds/linux/archive/master.tar.gz",
		"https://example.com/DanielLavrushin/b4",
		"http://github.com/DanielLavrushin/b4/x",
		"https://github.com/DanielLavrushinEvil/b4/x",
	}
	for _, url := range notMirrorable {
		if isMirrorable(url) {
			t.Errorf("expected %s to be rejected", url)
		}
	}
}

func TestDownloadFileMirroredFallsBackToMirror(t *testing.T) {
	const payload = "b4-binary-payload"

	dead := deadServerURL(t)
	swapBases(t, dead, dead, dead)

	direct := dead + "/" + repoOwner + "/" + repoName + "/main/install.sh"
	wantPath := "/github/" + direct

	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b4/health":
			w.WriteHeader(http.StatusOK)
		default:
			if r.URL.RequestURI() != wantPath {
				t.Errorf("mirror path = %q, want %q", r.URL.RequestURI(), wantPath)
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Write([]byte(payload))
		}
	}))
	defer mirror.Close()

	swapMirrors(t, []string{mirror.URL})

	dest := filepath.Join(t.TempDir(), "install.sh")
	size, err := downloadFileMirrored(direct, dest, b4Mirrors)
	if err != nil {
		t.Fatalf("expected mirror fallback to succeed, got %v", err)
	}
	if size != int64(len(payload)) {
		t.Fatalf("size = %d, want %d", size, len(payload))
	}

	got, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("content = %q, want %q", got, payload)
	}
}

func TestDownloadFileMirroredSkipsMirrorsForForeignURL(t *testing.T) {
	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("mirror must not be used for a non-allowlisted URL, got %q", r.URL.RequestURI())
	}))
	defer mirror.Close()

	dead := deadServerURL(t)
	swapBases(t, dead, dead, dead)
	swapMirrors(t, []string{mirror.URL})

	dest := filepath.Join(t.TempDir(), "x")
	foreign := dead + "/torvalds/linux/archive/master.tar.gz"
	if _, err := downloadFileMirrored(foreign, dest, b4Mirrors); err == nil {
		t.Fatal("expected a foreign URL to fail rather than reach a mirror")
	}
}

func TestReleasesEndpointServesUpstreamThenCaches(t *testing.T) {
	const body = `[{"tag_name":"v1.79.0","prerelease":false}]`

	var hits int
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Write([]byte(body))
	}))
	defer upstream.Close()

	swapBases(t, upstream.URL, upstream.URL, upstream.URL)
	api := newMirrorTestAPI(t)

	for i := 0; i < 3; i++ {
		rec := httptest.NewRecorder()
		api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/releases", nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("call %d: code = %d, want 200", i, rec.Code)
		}
		if rec.Body.String() != body {
			t.Fatalf("call %d: body = %q, want %q", i, rec.Body.String(), body)
		}
	}

	if hits != 1 {
		t.Fatalf("upstream hits = %d, want 1 (the rest must come from cache)", hits)
	}
}

func TestReleasesEndpointFallsBackToMirror(t *testing.T) {
	const body = `[{"tag_name":"v1.79.0"}]`

	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b4/health":
			w.WriteHeader(http.StatusOK)
		case "/b4/api/releases":
			w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mirror.Close()

	swapBases(t, deadServerURL(t), deadServerURL(t), deadServerURL(t))
	swapMirrors(t, []string{mirror.URL})
	api := newMirrorTestAPI(t)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/releases", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200", rec.Code)
	}
	if rec.Body.String() != body {
		t.Fatalf("body = %q, want %q", rec.Body.String(), body)
	}
}

func TestReleasesEndpointServesStaleWhenEverythingIsDown(t *testing.T) {
	const body = `[{"tag_name":"v1.79.0"}]`

	var serve bool
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !serve {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		w.Write([]byte(body))
	}))
	defer upstream.Close()

	swapBases(t, upstream.URL, upstream.URL, upstream.URL)
	swapMirrors(t, nil)
	api := newMirrorTestAPI(t)

	serve = true
	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/releases", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("warm-up code = %d, want 200", rec.Code)
	}

	releasesCache.mu.Lock()
	releasesCache.fetched = releasesCache.fetched.Add(-2 * releasesTTL)
	releasesCache.mu.Unlock()

	serve = false
	rec = httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/releases", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("stale code = %d, want 200", rec.Code)
	}
	if rec.Body.String() != body {
		t.Fatalf("stale body = %q, want the cached copy %q", rec.Body.String(), body)
	}
	if got := rec.Header().Get("X-B4-Cache"); got != "STALE" {
		t.Fatalf("X-B4-Cache = %q, want STALE", got)
	}
}

func TestReleasesEndpointReportsGatewayFailureWithNoCache(t *testing.T) {
	swapBases(t, deadServerURL(t), deadServerURL(t), deadServerURL(t))
	swapMirrors(t, nil)
	api := newMirrorTestAPI(t)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/releases", nil))

	if rec.Code != http.StatusBadGateway {
		t.Fatalf("code = %d, want 502", rec.Code)
	}
}

func TestChangelogEndpointPicksTheLocalizedFile(t *testing.T) {
	var requested []string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		w.Write([]byte("## [1.79.0]"))
	}))
	defer upstream.Close()

	swapBases(t, upstream.URL, upstream.URL, upstream.URL)
	api := newMirrorTestAPI(t)

	for _, tc := range []struct{ query, want string }{
		{"", "/DanielLavrushin/b4/main/changelog.md"},
		{"?lang=ru", "/DanielLavrushin/b4/main/changelog_ru.md"},
		{"?lang=de", "/DanielLavrushin/b4/main/changelog.md"},
	} {
		requested = nil
		changelogMu.Lock()
		changelogCache = map[string]*upstreamCache{}
		changelogMu.Unlock()

		rec := httptest.NewRecorder()
		api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/changelog"+tc.query, nil))

		if rec.Code != http.StatusOK {
			t.Fatalf("%q: code = %d, want 200", tc.query, rec.Code)
		}
		if len(requested) != 1 || requested[0] != tc.want {
			t.Fatalf("%q: fetched %v, want [%s]", tc.query, requested, tc.want)
		}
	}
}

func TestMergeMirrorsPutsConfiguredFirstAndRejectsJunk(t *testing.T) {
	swapMirrors(t, []string{"https://proxy.b4core.app", "https://proxy2.b4core.app"})

	prevScheme := mirrorScheme
	mirrorScheme = "https://"
	t.Cleanup(func() { mirrorScheme = prevScheme })

	got := mergeMirrors([]string{
		"https://mine.workers.dev/",
		"  https://spaced.workers.dev  ",
		"https://proxy.b4core.app",
		"http://insecure.example",
		"https://bad.example/?x=1",
		"https://bad.example/#frag",
		"https://evil.example@good.example",
		"https://user:pw@good.example",
		"https://line.example\nX-Injected: 1",
		"https://line.example\r\nfoo",
		"https://vtab.exa\vmple",
		"https://nul.exa\x00mple",
		"https://inner space.example",
		"",
		"not-a-url",
	})

	want := []string{
		"https://mine.workers.dev",
		"https://spaced.workers.dev",
		"https://proxy.b4core.app",
		"https://proxy2.b4core.app",
	}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestMergeMirrorsWithNoConfigIsJustTheDefaults(t *testing.T) {
	swapMirrors(t, []string{"https://proxy.b4core.app"})
	prevScheme := mirrorScheme
	mirrorScheme = "https://"
	t.Cleanup(func() { mirrorScheme = prevScheme })

	got := mergeMirrors(nil)
	if len(got) != 1 || got[0] != "https://proxy.b4core.app" {
		t.Fatalf("got %v, want the built-in list", got)
	}
}

func TestReleasesEndpointPrefersAConfiguredMirror(t *testing.T) {
	const body = `[{"tag_name":"v1.79.0","from":"personal"}]`

	var personalHits, builtinHits int

	personal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b4/health":
			w.WriteHeader(http.StatusOK)
		case "/b4/api/releases":
			personalHits++
			w.Write([]byte(body))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer personal.Close()

	builtin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		builtinHits++
		w.WriteHeader(http.StatusOK)
	}))
	defer builtin.Close()

	swapBases(t, deadServerURL(t), deadServerURL(t), deadServerURL(t))
	swapMirrors(t, []string{builtin.URL})
	api := newMirrorTestAPI(t, personal.URL)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/releases", nil))

	if rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Fatalf("code = %d body = %q, want the personal mirror's answer", rec.Code, rec.Body.String())
	}
	if personalHits != 1 {
		t.Fatalf("personal mirror hits = %d, want 1", personalHits)
	}
	if builtinHits != 0 {
		t.Fatalf("built-in mirror was contacted %d times, want 0 once the personal one answered", builtinHits)
	}
}

func TestFetchBytesRejectsAnOversizedResponseInsteadOfTruncating(t *testing.T) {
	const limit = 1024

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), limit+1))
	}))
	defer srv.Close()

	if _, err := fetchBytes(context.Background(), srv.URL, limit); err == nil {
		t.Fatal("an oversized body must be refused, not silently truncated")
	}

	exact := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), limit))
	}))
	defer exact.Close()

	body, err := fetchBytes(context.Background(), exact.URL, limit)
	if err != nil {
		t.Fatalf("a body exactly at the limit must be accepted, got %v", err)
	}
	if int64(len(body)) != limit {
		t.Fatalf("len = %d, want %d", len(body), limit)
	}
}

func TestOversizedUpstreamFallsBackToAMirrorAndDoesNotPoisonTheCache(t *testing.T) {
	const good = `[{"tag_name":"v1.79.0"}]`

	oversized := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(bytes.Repeat([]byte("x"), releasesLimit+1))
	}))
	defer oversized.Close()

	mirror := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/b4/health":
			w.WriteHeader(http.StatusOK)
		case "/b4/api/releases":
			w.Write([]byte(good))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer mirror.Close()

	swapBases(t, oversized.URL, oversized.URL, oversized.URL)
	swapMirrors(t, []string{mirror.URL})
	api := newMirrorTestAPI(t)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/releases", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("code = %d, want 200 from the mirror", rec.Code)
	}
	if rec.Body.String() != good {
		t.Fatalf("body = %.60q, want the mirror's answer, not a truncated upstream", rec.Body.String())
	}

	cached, _ := releasesCache.get(releasesTTL)
	if string(cached) != good {
		t.Fatalf("cache holds %.60q, want the mirror's answer", string(cached))
	}
}

func TestConcurrentRefreshesShareOneUpstreamCall(t *testing.T) {
	const body = `[{"tag_name":"v1.79.0"}]`
	const callers = 24

	var hits int64
	release := make(chan struct{})

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt64(&hits, 1)
		<-release
		w.Write([]byte(body))
	}))
	defer upstream.Close()

	swapBases(t, upstream.URL, upstream.URL, upstream.URL)
	swapMirrors(t, nil)
	api := newMirrorTestAPI(t)

	var wg sync.WaitGroup
	codes := make([]int, callers)
	bodies := make([]string, callers)

	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec := httptest.NewRecorder()
			api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/releases", nil))
			codes[i] = rec.Code
			bodies[i] = rec.Body.String()
		}(i)
	}

	for atomic.LoadInt64(&hits) == 0 {
		runtime.Gosched()
	}
	close(release)
	wg.Wait()

	if got := atomic.LoadInt64(&hits); got != 1 {
		t.Fatalf("upstream calls = %d, want 1 for %d concurrent callers", got, callers)
	}
	for i := range codes {
		if codes[i] != http.StatusOK || bodies[i] != body {
			t.Fatalf("caller %d got code=%d body=%q, want every caller to share the result", i, codes[i], bodies[i])
		}
	}
}

func TestAFailedRefreshDoesNotStickToLaterCallers(t *testing.T) {
	const body = `[{"tag_name":"v1.79.0"}]`

	var fail atomic.Bool
	fail.Store(true)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Write([]byte(body))
	}))
	defer upstream.Close()

	swapBases(t, upstream.URL, upstream.URL, upstream.URL)
	swapMirrors(t, nil)
	api := newMirrorTestAPI(t)

	rec := httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/releases", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("first call code = %d, want 502", rec.Code)
	}

	fail.Store(false)
	rec = httptest.NewRecorder()
	api.mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/releases", nil))
	if rec.Code != http.StatusOK || rec.Body.String() != body {
		t.Fatalf("retry after recovery got code=%d body=%q, want the fresh answer", rec.Code, rec.Body.String())
	}
}

func TestMirrorClientHonoursProxyEnvironment(t *testing.T) {
	tr, ok := mirrorClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("mirrorClient.Transport = %T, want *http.Transport", mirrorClient.Transport)
	}

	if tr.Proxy == nil {
		t.Fatal("mirrorClient must honour HTTP_PROXY/HTTPS_PROXY: a custom Transport with a nil Proxy silently ignores them, unlike http.DefaultTransport")
	}

	want := reflect.ValueOf(http.ProxyFromEnvironment).Pointer()
	if got := reflect.ValueOf(tr.Proxy).Pointer(); got != want {
		t.Fatal("mirrorClient.Transport.Proxy should be http.ProxyFromEnvironment")
	}
}
