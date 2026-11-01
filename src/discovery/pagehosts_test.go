package discovery

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type pageSite struct {
	domain string
	path   string
	html   string
}

func pageHostsFixture(t *testing.T, sites ...pageSite) (*DiscoverySuite, *fakeHosts, *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	for _, site := range sites {
		html := site.html
		mux.HandleFunc(site.path, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(html))
		})
	}
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	var inputs []DomainInput
	results := map[string]*DomainDiscoveryResult{}
	for _, site := range sites {
		inputs = append(inputs, DomainInput{Domain: site.domain, CheckURL: srv.URL + site.path})
		results[site.domain] = &DomainDiscoveryResult{Domain: site.domain, Url: srv.URL + site.path, Results: map[string]*DomainPresetResult{}}
	}
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.System.Checker.ConfigPropagateMs = 0
	ds := &DiscoverySuite{
		CheckSuite:    NewCheckSuite(inputs),
		cfg:           &cfg,
		domainResults: results,
		dnsResults:    map[string]*DNSDiscoveryResult{},
		ipVersion:     "ipv4",
	}
	ds.Status = CheckStatusRunning
	ds.CurrentPhase = PhaseConfirm
	f := &fakeHosts{without: map[string][]hostProbe{}, with: map[string][]hostProbe{}, ds: ds}
	ds.hostProbeFn = f.probe
	ds.hostConfigFn = f.apply
	return ds, f, srv
}

func (ds *DiscoverySuite) winsWith(preset string, confirmed bool, domains ...string) {
	for _, d := range domains {
		ds.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.put(d, preset, CheckStatusComplete, PhaseStrategy, 1)
		if confirmed {
			ds.recordConfirmation(d, preset, confirmTries)
		}
	}
	ds.determineBest()
	ds.buildStrategyGroups()
}

func registered(t *testing.T, ds *DiscoverySuite) {
	t.Helper()
	RegisterSuite(ds.CheckSuite)
	t.Cleanup(func() {
		suitesMu.Lock()
		delete(activeSuites, ds.Id)
		suitesMu.Unlock()
	})
}

func stopOnFirstProbe(ds *DiscoverySuite, stop func()) {
	probe := ds.hostProbeFn
	var once sync.Once
	ds.hostProbeFn = func(ctx context.Context, rawURL, origin string, with bool) hostProbe {
		first := false
		once.Do(func() {
			first = true
			stop()
		})
		if first {
			select {
			case <-ctx.Done():
			case <-time.After(5 * time.Second):
			}
			return hostProbe{aborted: true}
		}
		return probe(ctx, rawURL, origin, with)
	}
}

func pageHosts(ds *DiscoverySuite, domain string) *HostCheck {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()
	return ds.domainResults[domain].PageHosts
}

func hostVerdicts(check *HostCheck) map[string]HostVerdict {
	out := map[string]HostVerdict{}
	if check != nil {
		for _, h := range check.Hosts {
			out[h.Host] = h.Verdict
		}
	}
	return out
}

func TestPageHostsListsTheHostsEachPageLoads(t *testing.T) {
	ds, f, srv := pageHostsFixture(t,
		pageSite{domain: "a.example.com", path: "/a", html: `<html><head><script src="https://cdn.example.net/app.js"></script></head>
<body><img src="https://shared.example.net/a.png"><img src="https://img.a.example.com/b.png"></body></html>`},
		pageSite{domain: "b.example.com", path: "/b", html: `<html><body><img src="https://shared.example.net/b.png"><img src="https://other.example.net/c.png"></body></html>`},
	)
	ds.winsWith("combo", true, "a.example.com", "b.example.com")
	f.without["https://cdn.example.net/app.js"] = []hostProbe{blockedProbe, blockedProbe}
	f.without["https://shared.example.net/a.png"] = []hostProbe{blockedProbe, blockedProbe}
	var origins []string
	var mu sync.Mutex
	probe := ds.hostProbeFn
	ds.hostProbeFn = func(ctx context.Context, rawURL, origin string, with bool) hostProbe {
		mu.Lock()
		origins = append(origins, origin)
		mu.Unlock()
		return probe(ctx, rawURL, origin, with)
	}

	ds.checkPageHosts()

	a, b := pageHosts(ds, "a.example.com"), pageHosts(ds, "b.example.com")
	if a == nil || b == nil || a.Preset != "combo" || b.Preset != "combo" || a.Skipped != "" || b.Skipped != "" {
		t.Fatalf("page hosts a=%+v b=%+v", a, b)
	}
	if got := hostVerdicts(a); !reflect.DeepEqual(got, map[string]HostVerdict{"cdn.example.net": HostNeeded, "shared.example.net": HostNeeded, "img.a.example.com": HostCovered}) {
		t.Errorf("a.example.com hosts %v", got)
	}
	if got := hostVerdicts(b); !reflect.DeepEqual(got, map[string]HostVerdict{"shared.example.net": HostNeeded, "other.example.net": HostLoadsWithout}) {
		t.Errorf("b.example.com hosts %v: each page lists the hosts it names, a host two pages name is tested once", got)
	}
	if len(f.configs) != 3 {
		t.Fatalf("configurations %d, want the page read, b4 off and the strategy with the hosts", len(f.configs))
	}
	if read := f.configs[0].Sets[0]; !read.Enabled || !reflect.DeepEqual(read.Targets.SNIDomains, []string{"a.example.com", "b.example.com"}) {
		t.Errorf("the pages are read through the strategy as it is deployed, got %+v", read.Targets.SNIDomains)
	}
	if with := f.configs[2].Sets[0]; !reflect.DeepEqual(with.Targets.SNIDomains, []string{"a.example.com", "b.example.com", "cdn.example.net", "shared.example.net"}) {
		t.Errorf("the strategy pass adds the blocked hosts by name, got %v", with.Targets.SNIDomains)
	}
	for _, origin := range origins {
		if origin != srv.URL+"/" {
			t.Fatalf("Referer %q, want only the page's origin %q", origin, srv.URL+"/")
		}
	}
	if ds.CurrentPhase != PhaseHosts || ds.HostsTotal == 0 || ds.HostsDone != ds.HostsTotal {
		t.Errorf("phase %q, progress %d of %d", ds.CurrentPhase, ds.HostsDone, ds.HostsTotal)
	}
}

func TestPageHostsNeedAConfirmedStrategyAndSkipTheWatchdog(t *testing.T) {
	site := pageSite{domain: "a.example.com", path: "/a", html: `<script src="https://cdn.example.net/app.js"></script>`}

	unconfirmed, f, _ := pageHostsFixture(t, site)
	unconfirmed.winsWith("combo", false, "a.example.com")
	unconfirmed.checkPageHosts()
	if unconfirmed.CurrentPhase != PhaseConfirm || len(f.configs) != 0 || pageHosts(unconfirmed, "a.example.com") != nil {
		t.Errorf("a winner that was not confirmed is not used: phase %q, configs %d", unconfirmed.CurrentPhase, len(f.configs))
	}

	watchdog, f, _ := pageHostsFixture(t, site)
	watchdog.Source = SourceWatchdog
	watchdog.winsWith("combo", true, "a.example.com")
	watchdog.checkPageHosts()
	if watchdog.CurrentPhase != PhaseConfirm || len(f.configs) != 0 || pageHosts(watchdog, "a.example.com") != nil {
		t.Errorf("watchdog runs skip the step: phase %q, configs %d", watchdog.CurrentPhase, len(f.configs))
	}

	nothing, f, _ := pageHostsFixture(t, site)
	nothing.put("a.example.com", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	nothing.determineBest()
	nothing.buildStrategyGroups()
	nothing.checkPageHosts()
	if nothing.CurrentPhase != PhaseConfirm || len(f.configs) != 0 {
		t.Errorf("a run without a found strategy never enters the step: phase %q, configs %d", nothing.CurrentPhase, len(f.configs))
	}
}

func TestPageHostsUseTheGroupWinnerAndTheLiveSet(t *testing.T) {
	ds, f, _ := pageHostsFixture(t,
		pageSite{domain: "a.example.com", path: "/a", html: `<script src="https://cdn.example.net/a.js"></script><img src="https://media.live.example.org/a.png">`},
		pageSite{domain: "b.example.com", path: "/b", html: `<script src="https://cdn.example.net/b.js"></script>`},
	)
	ds.SetId = "set-a"
	live := config.NewSetConfig()
	live.Id = "set-a"
	live.Targets.SNIDomains = []string{"live.example.org"}
	ds.cfg.Sets = []*config.SetConfig{&live}
	ds.winsWith("fast-a", true, "a.example.com")
	ds.put("b.example.com", "fast-b", CheckStatusComplete, PhaseStrategy, 0)
	for _, d := range []string{"a.example.com", "b.example.com"} {
		ds.put(d, "joint", CheckStatusComplete, PhaseStrategy, 5)
		ds.recordConfirmation(d, "joint", confirmTries)
	}
	ds.determineBest()
	ds.buildStrategyGroups()
	ds.setVerdict = &SetVerdict{Status: SetVerdictCovered, WinnerPreset: "joint", Covered: []string{"a.example.com", "b.example.com"}, Set: strategySet("joint"), Confirmed: true}
	f.without["https://cdn.example.net/a.js"] = []hostProbe{blockedProbe, blockedProbe}

	ds.checkPageHosts()

	for _, d := range []string{"a.example.com", "b.example.com"} {
		if check := pageHosts(ds, d); check == nil || check.Preset != "joint" {
			t.Fatalf("%s: %+v, want the hosts tested with the winner of the group both domains share", d, check)
		}
	}
	if got := hostVerdicts(pageHosts(ds, "a.example.com")); got["media.live.example.org"] != HostCovered || got["cdn.example.net"] != HostNeeded {
		t.Errorf("hosts %v: a host the live set already names is covered", got)
	}
	if f.count("without https://media.live.example.org/") != 0 {
		t.Errorf("a covered host is never probed: %v", f.calls)
	}
}

func TestPageHostsInASetRunUseTheGroupWinnerTheCardsShow(t *testing.T) {
	ds, _, _ := pageHostsFixture(t,
		pageSite{domain: "a.example.com", path: "/a", html: `<script src="https://cdn.example.net/a.js"></script>`},
		pageSite{domain: "b.example.com", path: "/b", html: `<script src="https://cdn.example.net/b.js"></script>`},
	)
	ds.SetId = "set-a"
	cached := "cached-1-tcp_frag"
	domains := []string{"a.example.com", "b.example.com"}
	for _, d := range domains {
		ds.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
		ds.put(d, presetSetCurrent, CheckStatusComplete, PhaseCached, 0)
		ds.put(d, cached, CheckStatusComplete, PhaseCached, 0)
		ds.recordConfirmation(d, presetSetCurrent, confirmTries)
		ds.recordConfirmation(d, cached, confirmTries)
	}
	ds.determineBest()
	ds.buildStrategyGroups()
	if len(ds.StrategyGroups) != 1 || ds.StrategyGroups[0].WinnerPreset != cached {
		t.Fatalf("groups %+v, want the cached preset to win the tie with set-current by name", ds.StrategyGroups)
	}
	ds.setVerdict = &SetVerdict{Status: SetVerdictCurrentWorks, WinnerPreset: presetSetCurrent, Covered: domains, Set: strategySet(presetSetCurrent), Confirmed: true}

	ds.checkPageHosts()

	for _, d := range domains {
		if check := pageHosts(ds, d); check == nil || check.Preset != cached {
			t.Fatalf("%s: %+v, want the hosts tested with the group winner, the strategy the card and History offer", d, check)
		}
	}
}

func TestPageHostsCoverOnlyTheDomainsOfFoundGroups(t *testing.T) {
	ds, f, _ := pageHostsFixture(t,
		pageSite{domain: "a.example.com", path: "/a", html: `<img src="https://img.b.example.org/a.png"><img src="https://cdn.c.example.net/b.png">`},
		pageSite{domain: "b.example.org", path: "/b", html: `<html></html>`},
		pageSite{domain: "c.example.net", path: "/c", html: `<html></html>`},
	)
	ds.winsWith("one", true, "a.example.com")
	ds.put("c.example.net", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.winsWith("two", true, "b.example.org")
	f.without["https://cdn.c.example.net/b.png"] = []hostProbe{blockedProbe, blockedProbe}

	ds.checkPageHosts()

	got := hostVerdicts(pageHosts(ds, "a.example.com"))
	if got["img.b.example.org"] != HostCovered || f.count("without https://img.b.example.org/") != 0 {
		t.Errorf("hosts %v: a host under another found group's domain is covered and never probed", got)
	}
	if got["cdn.c.example.net"] != HostNeeded {
		t.Errorf("hosts %v: a host under a run domain nothing was found for is still tested", got)
	}
}

func TestPageHostsCutDuringThePageReadKeepsTheStepReason(t *testing.T) {
	for _, tc := range []struct {
		name   string
		skip   bool
		inPage bool
	}{
		{"skip during the apply", true, false},
		{"time limit during the apply", false, false},
		{"skip mid-page", true, true},
		{"time limit mid-page", false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			want := hostsSkippedByUser
			if !tc.skip {
				want = hostsOverBudget
				previous := hostRunBudget
				hostRunBudget = 300 * time.Millisecond
				t.Cleanup(func() { hostRunBudget = previous })
			}
			ds, _, _ := pageHostsFixture(t, pageSite{domain: "a.example.com", path: "/a", html: `<script src="https://cdn.example.net/a.js"></script>`})
			ds.winsWith("combo", true, "a.example.com")
			registered(t, ds)
			cut := func() {
				if tc.skip {
					_ = FinishCheckSuite(ds.Id)
				}
			}
			if tc.inPage {
				slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "text/html")
					_, _ = w.Write([]byte("<html><head>"))
					w.(http.Flusher).Flush()
					time.Sleep(100 * time.Millisecond)
					cut()
					select {
					case <-r.Context().Done():
					case <-time.After(5 * time.Second):
					}
				}))
				defer slow.Close()
				ds.Domains[0].CheckURL = slow.URL + "/a"
			} else {
				apply := ds.hostConfigFn
				var once sync.Once
				ds.hostConfigFn = func(cfg *config.Config) error {
					once.Do(func() {
						cut()
						time.Sleep(400 * time.Millisecond)
					})
					return apply(cfg)
				}
			}

			ds.checkPageHosts()

			if check := pageHosts(ds, "a.example.com"); check == nil || check.Skipped != want {
				t.Fatalf("page hosts %+v, want the page marked %q, not as unreadable through the strategy", check, want)
			}
		})
	}
}

func TestPageHostsReadAtMostThreePagesAtOnce(t *testing.T) {
	var mu sync.Mutex
	inFlight, peak := 0, 0
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		inFlight++
		peak = max(peak, inFlight)
		mu.Unlock()
		time.Sleep(150 * time.Millisecond)
		mu.Lock()
		inFlight--
		mu.Unlock()
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html></html>`))
	}))
	defer slow.Close()

	var sites []pageSite
	var domains []string
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		sites = append(sites, pageSite{domain: name + ".example.com", path: "/" + name})
		domains = append(domains, name+".example.com")
	}
	ds, _, _ := pageHostsFixture(t, sites...)
	for i := range ds.Domains {
		ds.Domains[i].CheckURL = slow.URL + "/" + ds.Domains[i].Domain
	}
	ds.winsWith("combo", true, domains...)

	ds.checkPageHosts()

	mu.Lock()
	defer mu.Unlock()
	if peak == 0 || peak > maxPageReads {
		t.Fatalf("%d pages read at once, want at most %d", peak, maxPageReads)
	}
}

func TestPageHostsStopAtTheTimeLimit(t *testing.T) {
	previous := hostRunBudget
	hostRunBudget = 300 * time.Millisecond
	t.Cleanup(func() { hostRunBudget = previous })

	ds, _, _ := pageHostsFixture(t, pageSite{domain: "a.example.com", path: "/a", html: `<script src="https://cdn.example.net/a.js"></script>`})
	ds.winsWith("combo", true, "a.example.com")
	ds.hostProbeFn = func(ctx context.Context, rawURL, origin string, with bool) hostProbe {
		select {
		case <-ctx.Done():
		case <-time.After(5 * time.Second):
		}
		return hostProbe{aborted: true}
	}

	start := time.Now()
	ds.checkPageHosts()

	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("the step ran %v past a %v limit", elapsed, hostRunBudget)
	}
	check := pageHosts(ds, "a.example.com")
	if check == nil || check.Skipped != hostsOverBudget || hostVerdicts(check)["cdn.example.net"] != HostUntested {
		t.Fatalf("page hosts %+v, want the host untested and the page marked as cut by the time limit", check)
	}
	if ds.HostsSkipped {
		t.Error("a time limit is not a user skip")
	}
}

func TestSkipDuringPageHostsEndsOnlyTheStep(t *testing.T) {
	for _, stop := range []struct {
		name string
		call func(id string) error
	}{
		{"finish", FinishCheckSuite},
		{"cancel", CancelCheckSuite},
	} {
		t.Run(stop.name, func(t *testing.T) {
			ds, _, _ := pageHostsFixture(t,
				pageSite{domain: "a.example.com", path: "/a", html: `<script src="https://cdn.example.net/a.js"></script><img src="https://img.example.net/b.png">`},
			)
			ds.SetId = "set-a"
			ds.winsWith("combo", true, "a.example.com")
			ds.setVerdict = &SetVerdict{Status: SetVerdictCovered, WinnerPreset: "combo", Covered: []string{"a.example.com"}, Set: strategySet("combo"), Confirmed: true}
			registered(t, ds)
			stopOnFirstProbe(ds, func() {
				if err := stop.call(ds.Id); err != nil {
					t.Errorf("%s: %v", stop.name, err)
				}
			})

			ds.checkPageHosts()

			if ds.canceled() || ds.finishing() || ds.Status != CheckStatusRunning || ds.StoppedEarly {
				t.Fatalf("a stop in the hosts step ends that step only: canceled=%v finishing=%v status=%q early=%v", ds.canceled(), ds.finishing(), ds.Status, ds.StoppedEarly)
			}
			if !ds.HostsSkipped {
				t.Fatal("the skip is recorded for the page")
			}
			if check := pageHosts(ds, "a.example.com"); check == nil || check.Skipped != hostsSkippedByUser {
				t.Fatalf("page hosts %+v, want the page marked as skipped", check)
			}

			if err := stop.call(ds.Id); err != nil {
				t.Fatal(err)
			}
			if ds.canceled() || ds.Status != CheckStatusRunning {
				t.Fatal("a stop that arrives after the step, before the run is written, changes nothing")
			}
			ds.CheckSuite.mu.Lock()
			ds.publishSetVerdictLocked()
			verdict := ds.SetVerdict
			ds.CheckSuite.mu.Unlock()
			if verdict == nil || verdict.Status != SetVerdictCovered {
				t.Fatalf("the covered verdict survives a stop in the hosts step, got %+v", verdict)
			}
		})
	}
}

func TestPageHostsRunAfterTheUserStoppedTheSearch(t *testing.T) {
	ds, f, _ := pageHostsFixture(t, pageSite{domain: "a.example.com", path: "/a", html: `<script src="https://cdn.example.net/a.js"></script>`})
	ds.winsWith("combo", true, "a.example.com")
	f.without["https://cdn.example.net/a.js"] = []hostProbe{blockedProbe, blockedProbe}
	registered(t, ds)
	ds.CurrentPhase = PhaseStrategy
	if err := FinishCheckSuite(ds.Id); err != nil || !ds.finishing() {
		t.Fatalf("finish: %v", err)
	}

	ds.checkPageHosts()

	check := pageHosts(ds, "a.example.com")
	if check == nil || check.Skipped != "" || hostVerdicts(check)["cdn.example.net"] != HostNeeded || ds.HostsSkipped {
		t.Fatalf("page hosts %+v: an earlier stop of the search does not stop the hosts step", check)
	}
}

func TestCancelDuringPageHostsKeepsTheFinishedRun(t *testing.T) {
	previous := suiteRetention
	suiteRetention = 10 * time.Millisecond
	t.Cleanup(func() { suiteRetention = previous })

	ds, _, _ := pageHostsFixture(t, pageSite{domain: "a.example.com", path: "/a", html: `<script src="https://cdn.example.net/a.js"></script>`})
	ds.SetId = "set-a"
	ds.put("a.example.com", presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	ds.put("a.example.com", "combo", CheckStatusComplete, PhaseStrategy, 1)
	ds.determineBest()
	withFakeConfirm(ds, nil)
	registered(t, ds)
	ds.initCancelContext()
	t.Cleanup(func() { ds.ctxCancel() })
	stopOnFirstProbe(ds, func() { _ = CancelCheckSuite(ds.Id) })

	ds.finishRun()

	if ds.Status != CheckStatusComplete || ds.SetVerdict == nil || ds.SetVerdict.Status != SetVerdictCovered || ds.SetVerdict.WinnerPreset != "combo" {
		t.Fatalf("status %q, verdict %+v: a cancel in the hosts step keeps the confirmed result", ds.Status, ds.SetVerdict)
	}
	if !ds.HostsSkipped || ds.StoppedEarly {
		t.Fatalf("skipped=%v early=%v, want the step marked skipped and the search not stopped", ds.HostsSkipped, ds.StoppedEarly)
	}
	history := LoadDiscoveryHistory(ds.cfg.ConfigPath)
	if len(history.Entries) != 1 || history.Entries[0].PageHosts == nil || history.Entries[0].PageHosts.Skipped != hostsSkippedByUser {
		t.Fatalf("history %+v, want the skipped step remembered", history.Entries)
	}
	for deadline := time.Now().Add(2 * time.Second); ; time.Sleep(10 * time.Millisecond) {
		if _, ok := GetCheckSuite(ds.Id); !ok {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the finished run is never released")
		}
	}
}

func TestLostRulesDuringPageHostsStillCancel(t *testing.T) {
	ds, _, _ := pageHostsFixture(t, pageSite{domain: "a.example.com", path: "/a", html: `<script src="https://cdn.example.net/a.js"></script>`})
	ds.SetId = "set-a"
	ds.winsWith("combo", true, "a.example.com")
	ds.setVerdict = &SetVerdict{Status: SetVerdictCovered, WinnerPreset: "combo", Covered: []string{"a.example.com"}, Set: strategySet("combo"), Confirmed: true}
	registered(t, ds)
	stopOnFirstProbe(ds, func() { cancelCheckSuite(ds.Id, true) })

	ds.checkPageHosts()

	if !ds.canceled() || ds.Status != CheckStatusCanceled || !ds.RulesChanged || ds.HostsSkipped {
		t.Fatalf("rules lost under the run stop it for real: canceled=%v status=%q rules=%v skipped=%v", ds.canceled(), ds.Status, ds.RulesChanged, ds.HostsSkipped)
	}
	if check := pageHosts(ds, "a.example.com"); check != nil {
		t.Fatalf("nothing is written for a canceled run, got %+v", check)
	}
	ds.CheckSuite.mu.Lock()
	ds.publishSetVerdictLocked()
	verdict := ds.SetVerdict
	ds.CheckSuite.mu.Unlock()
	if verdict == nil || verdict.Status != SetVerdictIncomplete {
		t.Fatalf("verdict %+v, want incomplete", verdict)
	}
}

func TestPageHostsNameTheHostARedirectLandsOn(t *testing.T) {
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body>moved</body></html>`))
	}))
	defer target.Close()
	_, port, _ := strings.Cut(strings.TrimPrefix(target.URL, "http://"), ":")
	stubResolver(t, map[string]stubName{"moved.example.org": {a: "127.0.0.1"}})

	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://moved.example.org:"+port+"/home", http.StatusFound)
	}))
	defer redirector.Close()
	ds, _, _ := pageHostsFixture(t, pageSite{domain: "a.example.com", path: "/a", html: ""})
	ds.Domains[0].CheckURL = redirector.URL + "/a"
	ds.winsWith("combo", true, "a.example.com")

	ds.checkPageHosts()

	check := pageHosts(ds, "a.example.com")
	if check == nil || len(check.Hosts) != 1 || check.Hosts[0].Host != "moved.example.org" || check.Hosts[0].Context != HostContextRedirect || !check.Hosts[0].Blocking {
		t.Fatalf("page hosts %+v, want the host the page moved to", check)
	}
}

func TestPageHostsCanBeReadWhileTheStepRuns(t *testing.T) {
	var html strings.Builder
	for i := 0; i < 20; i++ {
		html.WriteString(fmt.Sprintf(`<img src="https://h%d.example.net/a.png">`, i))
	}
	ds, f, _ := pageHostsFixture(t, pageSite{domain: "a.example.com", path: "/a", html: html.String()})
	ds.winsWith("combo", true, "a.example.com")
	for i := 0; i < 20; i += 2 {
		f.without[fmt.Sprintf("https://h%d.example.net/a.png", i)] = []hostProbe{blockedProbe, blockedProbe}
	}
	ds.DomainDiscoveryResults = ds.domainResults
	done := make(chan struct{})
	go func() {
		defer close(done)
		ds.checkPageHosts()
	}()
	for {
		if _, err := json.Marshal(ds.CheckSuite); err != nil {
			t.Fatal(err)
		}
		select {
		case <-done:
			if check := pageHosts(ds, "a.example.com"); check == nil || len(check.Hosts) != 20 {
				t.Fatalf("page hosts %+v", check)
			}
			return
		default:
		}
	}
}

func TestDroppedSetsTakeThePageHostsAlong(t *testing.T) {
	ds, _, _ := pageHostsFixture(t, pageSite{domain: "a.example.com", path: "/a", html: ""})
	ds.domainResults["a.example.com"].PageHosts = &HostCheck{Preset: "combo", Hosts: []HostResult{{HostCandidate: HostCandidate{Host: "cdn.example.net"}, Verdict: HostNeeded}}}
	ds.dropApplicableSetsLocked()
	if ds.domainResults["a.example.com"].PageHosts != nil {
		t.Fatal("hosts tested under rules that changed are dropped with the sets")
	}
}

func TestHistoryKeepsTheHostsToAdd(t *testing.T) {
	var hosts []HostResult
	for i := 0; i < 40; i++ {
		hosts = append(hosts, HostResult{
			HostCandidate: HostCandidate{Host: fmt.Sprintf("n%d.example.net", i), URLs: []string{"https://x/"}, Source: HostFromPage, Context: HostContextScript, Blocking: true},
			Verdict:       HostNeeded,
			URL:           "https://n.example.net/a.js",
			BytesRead:     2048,
			Error:         "detail",
		})
	}
	hosts = append(hosts,
		HostResult{HostCandidate: HostCandidate{Host: "open.example.net"}, Verdict: HostLoadsWithout},
		HostResult{HostCandidate: HostCandidate{Host: "covered.example.net"}, Verdict: HostCovered},
	)
	hosts[1].Verdict = HostNotFixed
	suite := &CheckSuite{
		Id:      "suite-1",
		Status:  CheckStatusComplete,
		Domains: []DomainInput{{Domain: "a.example.com"}, {Domain: "b.example.com"}, {Domain: "c.example.com"}},
		DomainDiscoveryResults: map[string]*DomainDiscoveryResult{
			"a.example.com": {Domain: "a.example.com", PageHosts: &HostCheck{Preset: "combo", Hosts: hosts}},
			"b.example.com": {Domain: "b.example.com", PageHosts: &HostCheck{Preset: "combo", Hosts: hosts[40:]}},
			"c.example.com": {Domain: "c.example.com", PageHosts: &HostCheck{Preset: "combo", Skipped: hostsOverBudget}},
		},
	}
	history := &DiscoveryHistory{}
	history.AddFromSuite(suite)
	data, err := json.Marshal(history)
	if err != nil {
		t.Fatal(err)
	}
	var back DiscoveryHistory
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatal(err)
	}
	entries := map[string]HistoryEntry{}
	for _, e := range back.Entries {
		entries[e.Domain] = e
	}

	kept := entries["a.example.com"].PageHosts
	if kept == nil || kept.Preset != "combo" || len(kept.Hosts) != maxHistoryHosts {
		t.Fatalf("history page hosts %+v, want %d of them", kept, maxHistoryHosts)
	}
	if kept.Hosts[1].Verdict != HostNotFixed || kept.Hosts[0].Verdict != HostNeeded {
		t.Errorf("needed and not fixed hosts are kept, got %+v", kept.Hosts[:2])
	}
	for _, h := range kept.Hosts {
		if h.URL != "" || h.Error != "" || h.BytesRead != 0 || len(h.URLs) != 0 || h.Context != HostContextScript || !h.Blocking {
			t.Fatalf("a history host keeps what the dialog needs and drops the probe details, got %+v", h)
		}
	}
	if entries["b.example.com"].PageHosts != nil {
		t.Errorf("nothing to add and nothing skipped stores nothing, got %+v", entries["b.example.com"].PageHosts)
	}
	if c := entries["c.example.com"].PageHosts; c == nil || c.Skipped != hostsOverBudget {
		t.Errorf("a page the time limit cut is remembered, got %+v", c)
	}
}
