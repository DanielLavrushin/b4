package asnprefix

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

type fakeRIPE struct {
	t        *testing.T
	mu       sync.Mutex
	hits     map[string]int
	respond  func(w http.ResponseWriter, r *http.Request, call, resource string)
	badAgent atomic.Bool
}

func (f *fakeRIPE) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	call := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/data/"), "/data.json")
	resource := r.URL.Query().Get("resource")
	if r.URL.Query().Get("sourceapp") != "b4" || r.Header.Get("User-Agent") != "b4" {
		f.badAgent.Store(true)
	}
	f.mu.Lock()
	f.hits[call+" "+resource]++
	f.mu.Unlock()
	f.respond(w, r, call, resource)
}

func (f *fakeRIPE) count(call, resource string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.hits[call+" "+resource]
}

func useFake(t *testing.T, respond func(w http.ResponseWriter, r *http.Request, call, resource string)) *fakeRIPE {
	t.Helper()
	f := &fakeRIPE{t: t, hits: map[string]int{}, respond: respond}
	srv := httptest.NewServer(f)
	prevBase, prevClient := baseURL, httpClient
	baseURL = srv.URL
	httpClient = srv.Client
	t.Cleanup(func() {
		baseURL, httpClient = prevBase, prevClient
		srv.Close()
		if f.badAgent.Load() {
			t.Error("every RIPEstat request carries sourceapp=b4 and User-Agent b4")
		}
	})
	return f
}

func useStore(t *testing.T) *config.AsnStore {
	t.Helper()
	s := config.InitAsnStore(filepath.Join(t.TempDir(), "b4.json"))
	drainWake()
	t.Cleanup(func() {
		config.InitAsnStore("")
		drainWake()
		statusMu.Lock()
		failures = map[string]string{}
		statusMu.Unlock()
	})
	return s
}

func drainWake() {
	for {
		select {
		case <-config.ASNRefreshRequests():
		default:
			return
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func realRIPE(t *testing.T) func(w http.ResponseWriter, r *http.Request, call, resource string) {
	return func(w http.ResponseWriter, r *http.Request, call, resource string) {
		switch call + " " + resource {
		case "ris-prefixes AS15169":
			writeJSON(w, 200, fixture(t, "ris_prefixes_AS15169.json"))
		case "ris-prefixes AS62041":
			writeJSON(w, 200, fixture(t, "ris_prefixes_AS62041.json"))
		case "as-overview AS15169":
			writeJSON(w, 200, fixture(t, "as_overview_AS15169.json"))
		case "as-overview AS62041":
			writeJSON(w, 200, fixture(t, "as_overview_AS62041.json"))
		case "network-info 142.250.120.139":
			writeJSON(w, 200, fixture(t, "network_info_142.250.120.139.json"))
		case "network-info 2001:4860:4860::8888":
			writeJSON(w, 200, fixture(t, "network_info_2001_4860_4860__8888.json"))
		default:
			writeJSON(w, 400, fixture(t, "ris_prefixes_bad.json"))
		}
	}
}

func TestFetchDecodesTheRealRisPrefixesAndOverviewResponses(t *testing.T) {
	useStore(t)
	useFake(t, realRIPE(t))

	info, err := Fetch(context.Background(), "AS62041")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"91.108.4.0/22", "91.108.8.0/22", "91.108.56.0/22", "95.161.64.0/20", "149.154.160.0/21", "2001:67c:4e8::/48"}
	if !slices.Equal(info.Prefixes, want) {
		t.Errorf("the 24+1 announced prefixes are collapsed to %v, got %v", want, info.Prefixes)
	}
	if info.ID != "62041" || info.Name != "Telegram Telegram Messenger Inc" || info.Source != config.AsnSourceRIPEstat {
		t.Errorf("id, holder name and source come from the responses: %+v", info)
	}
	if time.Since(time.Unix(info.UpdatedAt, 0)) > time.Minute {
		t.Errorf("updated_at is the fetch time, got %d", info.UpdatedAt)
	}

	google, err := Fetch(context.Background(), "15169")
	if err != nil {
		t.Fatal(err)
	}
	counts := google.Counts()
	if counts.V4 == 0 || counts.V6 == 0 || google.Name != "GOOGLE - Google LLC" {
		t.Errorf("both families and the holder are decoded: %+v %q", counts, google.Name)
	}
	if !slices.Equal(google.Prefixes, config.SanitizeASNPrefixes(google.Prefixes)) {
		t.Error("the stored list is already sanitized and collapsed")
	}
}

func TestFetchRejectsReservedASNsWithoutTouchingTheNetwork(t *testing.T) {
	useStore(t)
	f := useFake(t, realRIPE(t))
	for _, id := range []string{"AS64500", "0", "AS23456", "", "ASxyz", "4200000001"} {
		if _, err := Fetch(context.Background(), id); !errors.Is(err, ErrInvalidASN) {
			t.Errorf("%q: expected ErrInvalidASN, got %v", id, err)
		}
	}
	if n := len(f.hits); n != 0 {
		t.Errorf("no request is made for an invalid number, got %v", f.hits)
	}
}

func TestFetchReportsTheRIPEstatErrorMessage(t *testing.T) {
	useStore(t)
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		writeJSON(w, 400, fixture(t, "ris_prefixes_bad.json"))
	})
	_, err := Fetch(context.Background(), "AS15169")
	if err == nil || !strings.Contains(err.Error(), "unsupported resource type") || !strings.Contains(err.Error(), "400") {
		t.Fatalf("the message RIPEstat sent is in the error: %v", err)
	}
}

func TestFetchRejectsAStatusOtherThanOk(t *testing.T) {
	useStore(t)
	body := strings.Replace(string(fixture(t, "ris_prefixes_AS62041.json")), `"status":"ok"`, `"status":"maintenance"`, 1)
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		writeJSON(w, 200, []byte(body))
	})
	if _, err := Fetch(context.Background(), "62041"); err == nil || !strings.Contains(err.Error(), "maintenance") {
		t.Fatalf("a non-ok status is an error: %v", err)
	}
}

func TestFetchRejectsAnEmptyResult(t *testing.T) {
	useStore(t)
	body := strings.Replace(string(fixture(t, "ris_prefixes_AS64500.json")), `"resource":"64500"`, `"resource":"213230"`, 1)
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		writeJSON(w, 200, []byte(body))
	})
	if _, err := Fetch(context.Background(), "AS213230"); !errors.Is(err, ErrNoPrefixes) {
		t.Fatalf("an ASN announcing nothing is an error, got %v", err)
	}
}

func TestFetchRejectsAResultWithOnlyBogons(t *testing.T) {
	useStore(t)
	body := strings.Replace(string(fixture(t, "ris_prefixes_AS62041.json")), `"91.108.4.0/23"`, `"10.0.0.0/8"`, 1)
	body = strings.Replace(body, `"resource":"62041"`, `"resource":"213230"`, 1)
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		if call == "ris-prefixes" {
			writeJSON(w, 200, []byte(body))
			return
		}
		writeJSON(w, 200, fixture(t, "as_overview_AS62041.json"))
	})
	info, err := Fetch(context.Background(), "213230")
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range info.Prefixes {
		if strings.HasPrefix(p, "10.") {
			t.Errorf("bogons are dropped, got %v", info.Prefixes)
		}
	}
}

func TestFetchRefusesAnAnswerForAnotherASN(t *testing.T) {
	useStore(t)
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		writeJSON(w, 200, fixture(t, "ris_prefixes_AS62041.json"))
	})
	if _, err := Fetch(context.Background(), "15169"); err == nil {
		t.Fatal("prefixes of AS62041 are never stored as AS15169")
	}
}

func TestFetchCapsTheResponseBody(t *testing.T) {
	useStore(t)
	huge := make([]byte, maxBodyBytes+10)
	for i := range huge {
		huge[i] = ' '
	}
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		writeJSON(w, 200, huge)
	})
	if _, err := Fetch(context.Background(), "15169"); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("a body over the cap is refused: %v", err)
	}
}

func TestFetchKeepsPrefixesWhenTheHolderNameFails(t *testing.T) {
	useStore(t)
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		if call == "ris-prefixes" {
			writeJSON(w, 200, fixture(t, "ris_prefixes_AS62041.json"))
			return
		}
		http.Error(w, "down", http.StatusServiceUnavailable)
	})
	info, err := Fetch(context.Background(), "62041")
	if err != nil || len(info.Prefixes) == 0 || info.Name != "" {
		t.Fatalf("the name is optional: %+v %v", info, err)
	}
}

func TestResolveUsesAFreshEntryAndFetchesAStaleOne(t *testing.T) {
	s := useStore(t)
	f := useFake(t, realRIPE(t))
	if err := s.Put(&config.AsnInfo{ID: "62041", Name: "cached", Prefixes: []string{"91.108.4.0/22"}, UpdatedAt: time.Now().Add(-time.Hour).Unix(), Source: config.AsnSourceRIPEstat}); err != nil {
		t.Fatal(err)
	}

	got, err := Resolve(context.Background(), "AS62041", false)
	if err != nil || got.Name != "cached" || f.count("ris-prefixes", "AS62041") != 0 {
		t.Fatalf("a fresh entry is served from the store: %+v %v %v", got, err, f.hits)
	}

	got, err = Resolve(context.Background(), "62041", true)
	if err != nil || len(got.Prefixes) != 6 || f.count("ris-prefixes", "AS62041") != 1 {
		t.Fatalf("force fetches: %+v %v %v", got, err, f.hits)
	}
	if stored := s.Get("62041"); stored == nil || len(stored.Prefixes) != 6 {
		t.Fatalf("the fetched entry is stored: %+v", stored)
	}

	stale := s.Get("62041")
	stale.UpdatedAt = time.Now().Add(-StaleAfter - time.Minute).Unix()
	_ = s.Put(stale)
	if _, err := Resolve(context.Background(), "62041", false); err != nil || f.count("ris-prefixes", "AS62041") != 2 {
		t.Fatalf("a stale entry is fetched again: %v %v", err, f.hits)
	}
}

func TestResolveFailureKeepsTheStoredEntryAndRecordsTheError(t *testing.T) {
	s := useStore(t)
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		http.Error(w, "boom", http.StatusBadGateway)
	})
	old := &config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: []string{"91.108.4.0/22"}, UpdatedAt: 1, Source: config.AsnSourceRIPEstat}
	_ = s.Put(old)
	if _, err := Resolve(context.Background(), "62041", true); err == nil {
		t.Fatal("an upstream failure is an error")
	}
	if got := s.Get("62041"); got == nil || !slices.Equal(got.Prefixes, old.Prefixes) {
		t.Fatalf("the last good copy is kept: %+v", got)
	}
	if LastError("AS62041") == "" {
		t.Error("the failure is recorded for the UI")
	}
}

func TestResolveIsSingleFlightPerASN(t *testing.T) {
	useStore(t)
	release := make(chan struct{})
	var started sync.Once
	entered := make(chan struct{})
	f := useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		if call == "ris-prefixes" {
			started.Do(func() { close(entered) })
			<-release
		}
		realRIPE(t)(w, r, call, resource)
	})

	const callers = 8
	var wg sync.WaitGroup
	results := make(chan *config.AsnInfo, callers)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			info, err := Resolve(context.Background(), "AS62041", true)
			if err != nil {
				t.Error(err)
				return
			}
			results <- info
		}()
	}
	<-entered
	time.Sleep(50 * time.Millisecond)
	close(release)
	wg.Wait()
	close(results)
	if n := f.count("ris-prefixes", "AS62041"); n != 1 {
		t.Fatalf("concurrent resolves share one fetch, got %d", n)
	}
	for info := range results {
		if len(info.Prefixes) != 6 {
			t.Errorf("every caller gets the result: %+v", info)
		}
	}
}

func TestResolveCallerCanGiveUpWhileTheFetchFinishes(t *testing.T) {
	s := useStore(t)
	release := make(chan struct{})
	useFake(t, func(w http.ResponseWriter, r *http.Request, call, resource string) {
		if call == "ris-prefixes" {
			<-release
		}
		realRIPE(t)(w, r, call, resource)
	})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Resolve(ctx, "62041", true); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("the caller's deadline ends its wait: %v", err)
	}
	close(release)
	if got, err := Resolve(context.Background(), "62041", true); err != nil || len(got.Prefixes) == 0 {
		t.Fatalf("a later resolve works: %+v %v", got, err)
	}
	if s.Get("62041") == nil {
		t.Fatal("stored")
	}
}

func TestLookupIPUsesNetworkInfoAndNamesFromTheStoreOrOverview(t *testing.T) {
	s := useStore(t)
	f := useFake(t, realRIPE(t))

	res, err := LookupIP(context.Background(), "142.250.120.139:443")
	if err != nil {
		t.Fatal(err)
	}
	if res.IP != "142.250.120.139" || res.Prefix != "142.250.120.0/24" || len(res.ASNs) != 1 {
		t.Fatalf("prefix and origin come from network-info: %+v", res)
	}
	if a := res.ASNs[0]; a.ID != "15169" || a.Name != "GOOGLE - Google LLC" || a.Cached {
		t.Errorf("an unknown ASN is named by as-overview and not cached: %+v", a)
	}

	_ = s.Put(&config.AsnInfo{ID: "15169", Name: "Google (stored)", Prefixes: []string{"2001:4860::/32"}})
	before := f.count("as-overview", "AS15169")
	res, err = LookupIP(context.Background(), "[2001:4860:4860::8888]:53")
	if err != nil {
		t.Fatal(err)
	}
	if res.Prefix != "2001:4860::/32" || res.ASNs[0].Name != "Google (stored)" || !res.ASNs[0].Cached {
		t.Errorf("a stored ASN is named from the store: %+v", res)
	}
	if f.count("as-overview", "AS15169") != before {
		t.Error("no overview request for a stored name")
	}
}

func TestLookupIPNeverSendsPrivateAddresses(t *testing.T) {
	useStore(t)
	f := useFake(t, realRIPE(t))
	for _, ip := range []string{"10.0.0.1", "192.168.1.1:80", "::1", "fe80::1%eth0"} {
		res, err := LookupIP(context.Background(), ip)
		if err != nil || res.Prefix != "" || len(res.ASNs) != 0 {
			t.Errorf("%s: empty lookup expected, got %+v %v", ip, res, err)
		}
	}
	if len(f.hits) != 0 {
		t.Errorf("private addresses never leave the router: %v", f.hits)
	}
	if _, err := LookupIP(context.Background(), "example.com"); !errors.Is(err, ErrInvalidIP) {
		t.Errorf("a name is not an IP: %v", err)
	}
}

func TestParseIPAcceptsTheShapesTheUISends(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4":               "1.2.3.4",
		" 1.2.3.4:443 ":         "1.2.3.4",
		"[2001:db8::1]:443":     "2001:db8::1",
		"[2001:db8::1]":         "2001:db8::1",
		"::ffff:1.2.3.4":        "1.2.3.4",
		"2001:4860:4860::8888":  "2001:4860:4860::8888",
		"fe80::1%eth0":          "fe80::1",
		"[fe80::1%eth0]:12345":  "fe80::1",
		"2001:4860:4860::8888%": "",
		"":                      "",
		"1.2.3":                 "",
	}
	for in, want := range cases {
		addr, ok := ParseIP(in)
		got := ""
		if ok {
			got = addr.String()
		}
		if got != want {
			t.Errorf("ParseIP(%q) = %q, want %q", in, got, want)
		}
	}
}
