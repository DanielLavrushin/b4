package mirror

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
)

func TestAnnounceStateFollowsTheHubAnswer(t *testing.T) {
	cases := []struct {
		name   string
		answer *Answer
		err    error
		want   string
	}{
		{"transport error", nil, errors.New("dial tcp: no such host"), AnnounceFailed},
		{"pending", &Answer{Status: http.StatusAccepted, Body: []byte(`{"id":"abc","kind":"mirror","status":"pending"}`)}, nil, "pending"},
		{"approved", &Answer{Status: http.StatusAccepted, Body: []byte(`{"status":"approved"}`)}, nil, "approved"},
		{"accepted without status", &Answer{Status: http.StatusAccepted, Body: []byte(`{}`)}, nil, AnnounceAccepted},
		{"unparseable", &Answer{Status: http.StatusOK, Body: []byte(`ok`)}, nil, AnnounceAccepted},
		{"quota", &Answer{Status: http.StatusTooManyRequests, Body: []byte(`{"code":"quota"}`)}, nil, AnnounceRefused},
		{"bad record", &Answer{Status: http.StatusBadRequest, Body: []byte(`{"status":"pending"}`)}, nil, AnnounceRefused},
	}
	for _, c := range cases {
		if got := announceState(c.answer, c.err); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIndexPageShowsTheStateWithoutInternals(t *testing.T) {
	svc, err := New(Options{
		Upstream:  "https://hub.example.net/",
		PublicURL: "https://m1.hub.example.net",
		Layout:    hubdata.Layout{Root: t.TempDir()},
		Version:   "1.0.1",
	})
	if err != nil {
		t.Fatal(err)
	}
	refreshed := time.Date(2026, 9, 19, 13, 0, 0, 0, time.UTC)
	svc.mu.Lock()
	svc.status.Manifest = &hubwire.Manifest{Epoch: 1789237867, Seq: 145, KeyID: "SIGNERKEYID", GeneratedAt: "2026-09-19T12:55:59Z", ExpiresAt: "2026-10-03T12:55:59Z"}
	svc.status.Sets = 32
	svc.status.LastRefresh = refreshed
	svc.status.LastAnnounce = refreshed.Add(-5 * time.Minute)
	svc.status.AnnounceNote = `202 {"id":"5d7a64a8d236312311c712b80a51747af2156a917d0c9519c5bcb05de662ccae","kind":"mirror","status":"pending"}`
	svc.status.AnnounceState = "pending"
	svc.mu.Unlock()

	render := func(lang string) (string, http.Header) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if lang != "" {
			req.Header.Set("Accept-Language", lang)
		}
		rec := httptest.NewRecorder()
		svc.Router().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("index answered %d", rec.Code)
		}
		return rec.Body.String(), rec.Header()
	}

	body, header := render("de-CH, en;q=0.8")
	for _, want := range []string{`lang="en"`, "m1.hub.example.net", `href="https://hub.example.net"`, ">hub.example.net<", "1789237867-145", "32 sets", "2026-09-19 12:55 UTC", "2026-10-03 12:55 UTC", "2026-09-19 13:00 UTC", "in sync with the hub", "accepted, awaiting approval", "b4hub 1.0.1"} {
		if !strings.Contains(body, want) {
			t.Errorf("english page lacks %q", want)
		}
	}
	for _, leak := range []string{"SIGNERKEYID", "5d7a64a8d236", "Queued", "202 {"} {
		if strings.Contains(body, leak) {
			t.Errorf("english page exposes %q", leak)
		}
	}
	if header.Get("Vary") != "Accept-Language" {
		t.Errorf("page must vary on Accept-Language, got %q", header.Get("Vary"))
	}

	body, _ = render("ru-RU,ru;q=0.9,en;q=0.8")
	for _, want := range []string{`lang="ru"`, "32 сета", "синхронизировано с хабом", "принято, ждёт одобрения"} {
		if !strings.Contains(body, want) {
			t.Errorf("russian page lacks %q", want)
		}
	}

	svc.mu.Lock()
	svc.status.LastError = `manifest: Get "https://hub.example.net/b4/hub/manifest.json": dial tcp: lookup hub.example.net on 127.0.0.11:53: no such host`
	svc.mu.Unlock()
	body, _ = render("")
	if !strings.Contains(body, "the last check failed, serving the previous copy") || strings.Contains(body, "127.0.0.11") || strings.Contains(body, "no such host") {
		t.Errorf("a failed check must be shown as a state, not as the error text")
	}

	svc.mu.Lock()
	svc.status.Manifest = nil
	svc.mu.Unlock()
	body, _ = render("")
	if !strings.Contains(body, "the last check failed") || strings.Contains(body, "previous copy") || !strings.Contains(body, "no copy yet") {
		t.Errorf("a failed check without a copy must not claim to serve one")
	}
}

func TestPageLangHonoursQualityWeights(t *testing.T) {
	cases := map[string]string{
		"":                           "en",
		"de-CH":                      "en",
		"ru":                         "ru",
		"ru-RU,ru;q=0.9,en;q=0.8":    "ru",
		"en-US,en;q=0.9,ru;q=0.8":    "en",
		"ru;q=0, en;q=1":             "en",
		"de;q=1, ru;q=0.5, en;q=0.7": "en",
		"de;q=1, ru;q=0.7, en;q=0.5": "ru",
		"en;q=0.3,ru;q=0.3":          "en",
		"*, ru; Q=0.4":               "ru",
	}
	for header, want := range cases {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if header != "" {
			req.Header.Set("Accept-Language", header)
		}
		if got := pageLang(req); got != want {
			t.Errorf("%q: got %s, want %s", header, got, want)
		}
	}
}

func TestPluralsAndCounts(t *testing.T) {
	cases := []struct {
		lang string
		n    int
		want string
	}{
		{"en", 1, "1 set"}, {"en", 0, "0 sets"}, {"en", 21, "21 sets"},
		{"ru", 1, "1 сет"}, {"ru", 2, "2 сета"}, {"ru", 5, "5 сетов"}, {"ru", 11, "11 сетов"}, {"ru", 21, "21 сет"}, {"ru", 112, "112 сетов"},
	}
	for _, c := range cases {
		if got := count(c.lang, "sets", c.n); got != c.want {
			t.Errorf("%s %d: got %q, want %q", c.lang, c.n, got, c.want)
		}
	}
	if got := hostOf("https://hub.example.net/base/"); got != "hub.example.net/base/" {
		t.Errorf("hostOf keeps a path, got %q", got)
	}
	if got := hostOf("http://192.168.1.10:7100"); got != "192.168.1.10:7100" {
		t.Errorf("hostOf keeps a port, got %q", got)
	}
}

func TestPageStringsAgreeAcrossLanguages(t *testing.T) {
	forms := map[string][]string{"en": {"one", "other"}, "ru": {"one", "few", "many"}}
	base := func(lang string) map[string]bool {
		out := map[string]bool{}
		for key := range pageStrings[lang] {
			if noun, form, ok := strings.Cut(key, "_"); ok {
				if !slices.Contains(forms[lang], form) {
					t.Errorf("%s: %q is not a plural form of %s", lang, form, lang)
				}
				out[noun+"_*"] = true
				continue
			}
			out[key] = true
		}
		return out
	}
	en, ru := base("en"), base("ru")
	for key := range en {
		if !ru[key] {
			t.Errorf("ru lacks %q", key)
		}
	}
	for key := range ru {
		if !en[key] {
			t.Errorf("en lacks %q", key)
		}
	}
	for lang, want := range forms {
		for _, noun := range []string{"sets", "records"} {
			for _, form := range want {
				if pageStrings[lang][noun+"_"+form] == "" {
					t.Errorf("%s lacks %s_%s", lang, noun, form)
				}
			}
		}
	}
}
