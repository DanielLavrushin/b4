package discovery

import (
	"compress/gzip"
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/leaktest"
	"github.com/daniellavrushin/b4/netprobe"
	"github.com/daniellavrushin/b4/utils"
	"go.uber.org/goleak"
)

type fakeHosts struct {
	mu       sync.Mutex
	without  map[string][]hostProbe
	with     map[string][]hostProbe
	calls    []string
	configs  []*config.Config
	progress [][2]int
	ds       *DiscoverySuite
}

func (f *fakeHosts) next(results map[string][]hostProbe, rawURL string) hostProbe {
	queue := results[rawURL]
	if len(queue) == 0 {
		return hostProbe{ok: true, bytes: hostReadCap}
	}
	p := queue[0]
	if len(queue) > 1 {
		results[rawURL] = queue[1:]
	}
	return p
}

func (f *fakeHosts) probe(ctx context.Context, rawURL, origin string, with bool) hostProbe {
	if ctx.Err() != nil {
		return hostProbe{aborted: true}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ds != nil {
		f.ds.CheckSuite.mu.RLock()
		f.progress = append(f.progress, [2]int{f.ds.HostsDone, f.ds.HostsTotal})
		f.ds.CheckSuite.mu.RUnlock()
	}
	if with {
		f.calls = append(f.calls, "with "+rawURL)
		return f.next(f.with, rawURL)
	}
	f.calls = append(f.calls, "without "+rawURL)
	return f.next(f.without, rawURL)
}

func (f *fakeHosts) apply(cfg *config.Config) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.configs = append(f.configs, cfg)
	return nil
}

func (f *fakeHosts) count(prefix string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.calls {
		if strings.HasPrefix(c, prefix) {
			n++
		}
	}
	return n
}

var (
	blockedProbe     = hostProbe{failure: hostFailBlocked, err: "TLS handshake timed out (drop)"}
	unresolvedProbe  = hostProbe{failure: hostFailName, err: "DNS resolution failed (no such host)"}
	unreachableProbe = hostProbe{failure: hostFailUnreachable, err: "Connection refused"}
	passedProbe      = hostProbe{ok: true, bytes: hostReadCap}
)

func hostRunFixture(t *testing.T, domains ...string) (*DiscoverySuite, *fakeHosts) {
	t.Helper()
	inputs := make([]DomainInput, 0, len(domains))
	results := map[string]*DomainDiscoveryResult{}
	for _, d := range domains {
		inputs = append(inputs, DomainInput{Domain: d, CheckURL: "https://" + d + "/"})
		results[d] = &DomainDiscoveryResult{Domain: d, Url: "https://" + d + "/", Results: map[string]*DomainPresetResult{}}
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
	f := &fakeHosts{without: map[string][]hostProbe{}, with: map[string][]hostProbe{}, ds: ds}
	ds.hostProbeFn = f.probe
	ds.hostConfigFn = f.apply
	return ds, f
}

func sample(host string) string {
	return "https://" + host + "/a.js"
}

func cand(host string, ctx HostContext, blocking bool) HostCandidate {
	return HostCandidate{Host: host, URLs: []string{sample(host)}, Source: HostFromPage, Context: ctx, Blocking: blocking}
}

func verdicts(results []HostResult) map[string]HostResult {
	out := map[string]HostResult{}
	for _, r := range results {
		out[r.Host] = r
	}
	return out
}

func TestCheckHostsVerdicts(t *testing.T) {
	defer goleak.VerifyNone(t, leaktest.Options()...)
	ds, f := hostRunFixture(t, "news.example.com")
	f.without[sample("blocked.example.net")] = []hostProbe{blockedProbe, blockedProbe}
	f.without[sample("flaky.example.net")] = []hostProbe{blockedProbe, blockedProbe}
	f.with[sample("flaky.example.net")] = []hostProbe{passedProbe, blockedProbe}
	f.without[sample("gone.example.net")] = []hostProbe{unresolvedProbe, unresolvedProbe}
	f.without[sample("down.example.net")] = []hostProbe{blockedProbe, blockedProbe}
	f.with[sample("down.example.net")] = []hostProbe{unreachableProbe}

	results := ds.CheckHosts(context.Background(), HostTarget{
		Preset:  "combo",
		Set:     strategySet("combo"),
		Domains: []string{"news.example.com"},
		Origin:  "https://news.example.com/",
		Candidates: []HostCandidate{
			cand("open.example.net", HostContextScript, true),
			cand("blocked.example.net", HostContextStylesheet, true),
			cand("flaky.example.net", HostContextImage, false),
			cand("gone.example.net", HostContextImage, false),
			cand("down.example.net", HostContextImage, false),
			cand("static.news.example.com", HostContextImage, false),
			cand("www.youtube.com", HostContextScript, false),
		},
	})

	var order []HostVerdict
	for _, r := range results {
		order = append(order, r.Verdict)
	}
	wantOrder := []HostVerdict{HostNeeded, HostNotFixed, HostUnreachable, HostLoadsWithout, HostLoadsWithout, HostUnresolved, HostCovered}
	if !reflect.DeepEqual(order, wantOrder) {
		t.Fatalf("verdict order %v, want %v", order, wantOrder)
	}
	got := verdicts(results)
	for host, want := range map[string]HostVerdict{
		"blocked.example.net":     HostNeeded,
		"flaky.example.net":       HostNotFixed,
		"down.example.net":        HostUnreachable,
		"open.example.net":        HostLoadsWithout,
		"www.youtube.com":         HostLoadsWithout,
		"gone.example.net":        HostUnresolved,
		"static.news.example.com": HostCovered,
	} {
		if got[host].Verdict != want {
			t.Errorf("%s: %q (%s), want %q", host, got[host].Verdict, got[host].Error, want)
		}
	}
	if got["www.youtube.com"].Service != "youtube" {
		t.Errorf("a host of a known service names it, got %q", got["www.youtube.com"].Service)
	}
	if got["flaky.example.net"].Error != blockedProbe.err || got["down.example.net"].Error != unreachableProbe.err {
		t.Errorf("a host the strategy does not fix keeps the reason: %+v %+v", got["flaky.example.net"], got["down.example.net"])
	}
	if f.count("with "+sample("open.example.net")) != 0 || f.count("without "+sample("open.example.net")) != 1 {
		t.Errorf("a host that loads without b4 is probed once and never with the strategy: %v", f.calls)
	}
	if f.count("with "+sample("blocked.example.net")) != hostWithTries {
		t.Errorf("a needed host passes %d tries with the strategy: %v", hostWithTries, f.calls)
	}
	if f.count("with "+sample("flaky.example.net")) != 2 || f.count("with "+sample("down.example.net")) != 1 {
		t.Errorf("the with pass stops at the first failure: %v", f.calls)
	}
	if f.count("without "+sample("static.news.example.com")) != 0 || f.count("with "+sample("gone.example.net")) != 0 {
		t.Errorf("covered and unresolved hosts are never tested with the strategy: %v", f.calls)
	}
}

func TestCheckHostsRetriesAFailureWithoutB4BeforeTheStrategy(t *testing.T) {
	ds, f := hostRunFixture(t, "news.example.com")
	f.without[sample("cdn.example.net")] = []hostProbe{blockedProbe, passedProbe}

	results := ds.CheckHosts(context.Background(), HostTarget{
		Preset: "combo", Set: strategySet("combo"), Domains: []string{"news.example.com"},
		Candidates: []HostCandidate{cand("cdn.example.net", HostContextScript, true)},
	})

	if len(results) != 1 || results[0].Verdict != HostLoadsWithout {
		t.Fatalf("one failure without b4 is not a block, got %+v", results)
	}
	if f.count("without ") != 2 || f.count("with ") != 0 || len(f.configs) != 1 {
		t.Fatalf("the failed probe is retried at once under the same configuration, then nothing else runs: calls %v, configs %d", f.calls, len(f.configs))
	}
}

func TestCheckHostsTriesTheNextSampleWhenAReplyIsSmall(t *testing.T) {
	ds, f := hostRunFixture(t, "news.example.com")
	small := hostProbe{ok: true, small: true, bytes: 2048}
	big := hostProbe{ok: true, bytes: hostReadCap}
	urls := func(host string) []string {
		return []string{"https://" + host + "/small.png", "https://" + host + "/big.js"}
	}
	f.without["https://grows.example.net/small.png"] = []hostProbe{small}
	f.without["https://grows.example.net/big.js"] = []hostProbe{big}
	f.without["https://tiny.example.net/small.png"] = []hostProbe{small}
	f.without["https://tiny.example.net/big.js"] = []hostProbe{small}
	f.without["https://stalls.example.net/small.png"] = []hostProbe{small}
	f.without["https://stalls.example.net/big.js"] = []hostProbe{{failure: hostFailBlocked, err: "stalled after 16384 bytes", bytes: 16384}}

	var cands []HostCandidate
	for _, host := range []string{"grows.example.net", "tiny.example.net", "stalls.example.net"} {
		cands = append(cands, HostCandidate{Host: host, URLs: urls(host), Context: HostContextImage})
	}
	got := verdicts(ds.CheckHosts(context.Background(), HostTarget{
		Preset: "combo", Set: strategySet("combo"), Domains: []string{"news.example.com"}, Candidates: cands,
	}))

	if r := got["grows.example.net"]; r.Verdict != HostLoadsWithout || r.Small || r.URL != "https://grows.example.net/big.js" {
		t.Errorf("a small reply is followed by the next sample, got %+v", r)
	}
	if r := got["tiny.example.net"]; r.Verdict != HostLoadsWithout || !r.Small || r.BytesRead != 2048 {
		t.Errorf("only small replies still load, flagged small, got %+v", r)
	}
	if r := got["stalls.example.net"]; r.Verdict != HostNeeded || r.URL != "https://stalls.example.net/big.js" {
		t.Errorf("a larger sample that stalls makes the host blocked, and it is tested with the strategy on that sample, got %+v", r)
	}
}

func TestCheckHostsCapsTheCandidatesBlockingFirst(t *testing.T) {
	ds, f := hostRunFixture(t, "news.example.com")
	var cands []HostCandidate
	for i := 0; i < 40; i++ {
		cands = append(cands, cand("img"+string(rune('a'+i%26))+string(rune('a'+i/26))+".example.net", HostContextImage, false))
	}
	for i := 0; i < 5; i++ {
		cands = append(cands, cand("css"+string(rune('a'+i))+".example.net", HostContextStylesheet, true))
	}

	results := ds.CheckHosts(context.Background(), HostTarget{Preset: "combo", Set: strategySet("combo"), Domains: []string{"news.example.com"}, Candidates: cands})

	var untested []string
	for _, r := range results {
		if r.Verdict == HostUntested {
			untested = append(untested, r.Host)
			if !strings.Contains(r.Error, "over the limit of 40 hosts") {
				t.Errorf("%s: error %q", r.Host, r.Error)
			}
		}
	}
	want := []string{cands[35].Host, cands[36].Host, cands[37].Host, cands[38].Host, cands[39].Host}
	if !reflect.DeepEqual(untested, want) {
		t.Fatalf("untested %v, want the last images: blocking hosts go first", untested)
	}
	if f.count("without ") != maxHostCandidates {
		t.Fatalf("probed %d hosts, want %d", f.count("without "), maxHostCandidates)
	}
}

func TestCheckHostsTestsInTheDeployedShape(t *testing.T) {
	ds, f := hostRunFixture(t, "news.example.com")
	stored := strategySet("combo")
	stored.Targets.IPs = []string{"203.0.113.7/32"}
	stored.Targets.ASNs = []string{"AS64500"}
	for _, host := range []string{"cdn.example.net", "www.youtube.com"} {
		f.without[sample(host)] = []hostProbe{blockedProbe, blockedProbe}
	}

	ds.CheckHosts(context.Background(), HostTarget{
		Preset: "combo", Set: stored, Domains: []string{"news.example.com"},
		Candidates: []HostCandidate{cand("cdn.example.net", HostContextScript, true), cand("www.youtube.com", HostContextScript, false)},
	})

	if len(f.configs) != 2 {
		t.Fatalf("configurations applied %d, want b4 off then the strategy", len(f.configs))
	}
	if off := f.configs[0].Sets[0]; off.Enabled || off.DNS.Enabled {
		t.Errorf("the first pass runs with b4 off: %+v", off.Targets)
	}
	on := f.configs[1].Sets[0]
	if !on.Enabled || !reflect.DeepEqual(on.Targets.SNIDomains, []string{"news.example.com", "cdn.example.net", "www.youtube.com"}) {
		t.Errorf("the strategy pass adds the hosts by name to the deployed set, got %v", on.Targets.SNIDomains)
	}
	if len(on.Targets.IPs) != 0 || len(on.Targets.IpsToMatch) != 0 || len(on.Targets.ASNs) != 0 {
		t.Errorf("the deployed shape carries no addresses: %v %v %v", on.Targets.IPs, on.Targets.IpsToMatch, on.Targets.ASNs)
	}
	if len(on.Targets.GeoSiteCategories) != 0 || len(on.Targets.GeoIpCategories) != 0 {
		t.Errorf("a host of a known service never pulls its category into the set, got %v %v", on.Targets.GeoSiteCategories, on.Targets.GeoIpCategories)
	}
	if !reflect.DeepEqual(stored.Targets.IPs, []string{"203.0.113.7/32"}) {
		t.Errorf("the group's own set must stay untouched, got %v", stored.Targets.IPs)
	}
}

func TestDeployedConfigLeavesTheStoredSetAlone(t *testing.T) {
	ds, _ := hostRunFixture(t, "news.example.com")
	stored := strategySet("combo")
	backing := make([]string, 1, 4)
	backing[0] = "news.example.com"
	stored.Targets.SNIDomains = backing
	stored.Targets.DomainsToMatch = backing

	cfg := ds.deployedConfig(stored, nil, []string{"cdn.example.net"})

	if got := cfg.Sets[0].Targets.SNIDomains; !reflect.DeepEqual(got, []string{"news.example.com", "cdn.example.net"}) {
		t.Fatalf("deployed SNI domains %v", got)
	}
	if extra := backing[:2][1]; extra != "" || len(stored.Targets.SNIDomains) != 1 {
		t.Fatalf("appending the hosts wrote into the stored set's array: %q, %v", extra, stored.Targets.SNIDomains)
	}
	if plain := ds.deployedConfig(stored, []string{"news.example.com"}, nil); !reflect.DeepEqual(plain.Sets[0].Targets.SNIDomains, []string{"news.example.com"}) {
		t.Fatalf("without hosts the deployed shape is the confirmation's, got %v", plain.Sets[0].Targets.SNIDomains)
	}
}

func TestCheckHostsSharesTheWithoutPassAcrossTargets(t *testing.T) {
	ds, f := hostRunFixture(t, "a.example.com", "b.example.org")
	f.without[sample("shared.example.net")] = []hostProbe{blockedProbe, blockedProbe}
	run := ds.newHostRun([]*HostTarget{
		{Preset: "one", Set: strategySet("one"), Domains: []string{"a.example.com"}, Candidates: []HostCandidate{cand("shared.example.net", HostContextScript, true)}},
		{Preset: "two", Set: strategySet("two"), Domains: []string{"b.example.org"}, Candidates: []HostCandidate{cand("shared.example.net", HostContextImage, false)}},
	})
	ctx := context.Background()
	run.withoutPass(ctx)
	run.withPass(ctx, 0)
	run.withPass(ctx, 1)

	if f.count("without ") != 2 || f.count("with ") != 2*hostWithTries {
		t.Fatalf("a host named by two groups is tested once without b4 and with each strategy: %v", f.calls)
	}
	if len(f.configs) != 3 {
		t.Fatalf("configurations %d, want b4 off once and each strategy once", len(f.configs))
	}
	for i := range run.targets {
		if r := run.results(i); len(r) != 1 || r[0].Verdict != HostNeeded {
			t.Fatalf("target %d: %+v", i, r)
		}
	}
}

func TestCheckHostsStopsWhenTheContextEnds(t *testing.T) {
	defer goleak.VerifyNone(t, leaktest.Options()...)
	ds, f := hostRunFixture(t, "news.example.com")
	var cands []HostCandidate
	for _, host := range []string{"a.example.net", "b.example.net", "c.example.net"} {
		f.without[sample(host)] = []hostProbe{blockedProbe, blockedProbe}
		cands = append(cands, cand(host, HostContextScript, true))
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	run := ds.newHostRun([]*HostTarget{{Preset: "combo", Set: strategySet("combo"), Domains: []string{"news.example.com"}, Candidates: cands}})
	run.withoutPass(ctx)
	cancel()
	run.withPass(ctx, 0)

	for _, r := range run.results(0) {
		if r.Verdict != HostUntested {
			t.Errorf("%s: %q, want untested once the step is stopped", r.Host, r.Verdict)
		}
	}
	if f.count("with ") != 0 || len(f.configs) != 1 {
		t.Fatalf("no strategy is applied after the stop: calls %v, configs %d", f.calls, len(f.configs))
	}
	ds.CheckSuite.mu.RLock()
	done, total := ds.HostsDone, ds.HostsTotal
	ds.CheckSuite.mu.RUnlock()
	if done != total {
		t.Fatalf("progress %d of %d after the stop", done, total)
	}
}

func TestCheckHostsProgressNeverMovesBack(t *testing.T) {
	ds, f := hostRunFixture(t, "news.example.com")
	var cands []HostCandidate
	for i, host := range []string{"a.example.net", "b.example.net", "c.example.net", "d.example.net"} {
		if i%2 == 0 {
			f.without[sample(host)] = []hostProbe{blockedProbe, blockedProbe}
		}
		cands = append(cands, cand(host, HostContextScript, true))
	}
	ds.CheckHosts(context.Background(), HostTarget{Preset: "combo", Set: strategySet("combo"), Domains: []string{"news.example.com"}, Candidates: cands})

	total := f.progress[0][1]
	if total != 8 {
		t.Fatalf("total %d, want one step without b4 and one with the strategy per host, set before the first probe", total)
	}
	last := 0
	for _, p := range f.progress {
		if p[1] != total || p[0] < last || p[0] > total {
			t.Fatalf("progress went %v", f.progress)
		}
		last = p[0]
	}
	if ds.HostsDone != total {
		t.Fatalf("done %d of %d at the end", ds.HostsDone, total)
	}
}

func hostServer(t *testing.T, handler http.HandlerFunc) (*DiscoverySuite, string) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	ds := newProbeOnlySuite(srv.URL)
	return ds, srv.URL
}

func TestProbeHostReadsSubresources(t *testing.T) {
	var referer string
	var mu sync.Mutex
	ds, base := hostServer(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		referer = r.Header.Get("Referer")
		mu.Unlock()
		switch r.URL.Path {
		case "/small.js":
			_, _ = w.Write([]byte("var a = 1;"))
		case "/missing.png":
			http.NotFound(w, r)
		case "/forbidden.png":
			w.WriteHeader(http.StatusForbidden)
		case "/legal":
			w.WriteHeader(http.StatusUnavailableForLegalReasons)
		case "/bad":
			w.WriteHeader(http.StatusBadRequest)
		case "/broken":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "/blocked.html":
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte("<html><body>Доступ ограничен по решению суда, роскомнадзор</body></html>"))
		case "/news.js":
			w.Header().Set("Content-Type", "application/javascript")
			_, _ = w.Write([]byte(`var title = "Сайт заблокирован роскомнадзор";`))
		case "/to-block-page":
			http.Redirect(w, r, "http://warning.rt.ru/?id=17", http.StatusFound)
		case "/moved":
			http.Redirect(w, r, "/elsewhere", http.StatusMovedPermanently)
		case "/huge.bin":
			_, _ = w.Write(make([]byte, 1<<20))
		}
	})
	ctx := context.Background()

	for _, tc := range []struct {
		path    string
		with    bool
		ok      bool
		small   bool
		failure hostFailure
		detail  string
	}{
		{path: "/small.js", ok: true, small: true},
		{path: "/missing.png", ok: true, small: true},
		{path: "/forbidden.png", ok: true, small: true},
		{path: "/legal", failure: hostFailBlocked, detail: "HTTP 451"},
		{path: "/legal", with: true, failure: hostFailBlocked, detail: "HTTP 451"},
		{path: "/bad", ok: true, small: true},
		{path: "/bad", with: true, failure: hostFailBlocked, detail: "HTTP 400"},
		{path: "/broken", ok: true, small: true},
		{path: "/broken", with: true, failure: hostFailUnreachable, detail: "HTTP 503"},
		{path: "/blocked.html", failure: hostFailBlocked, detail: "block page"},
		{path: "/news.js", ok: true, small: true},
		{path: "/to-block-page", failure: hostFailBlocked, detail: "warning.rt.ru"},
		{path: "/moved", ok: true, small: true},
		{path: "/huge.bin", ok: true},
	} {
		p := ds.probeHost(ctx, base+tc.path, "https://news.example.com/", tc.with)
		if p.ok != tc.ok || p.small != tc.small || p.failure != tc.failure || !strings.Contains(p.err, tc.detail) {
			t.Errorf("%s with=%v: %+v, want ok=%v small=%v failure=%v %q", tc.path, tc.with, p, tc.ok, tc.small, tc.failure, tc.detail)
		}
	}
	lastReferer := func() string {
		mu.Lock()
		defer mu.Unlock()
		return referer
	}
	if p := ds.probeHost(ctx, base+"/huge.bin", "", false); p.bytes != hostReadCap {
		t.Errorf("a large body is read up to %d bytes, read %d", hostReadCap, p.bytes)
	}
	if got := lastReferer(); got != "" {
		t.Errorf("no origin, no Referer, got %q", got)
	}
	ds.probeHost(ctx, base+"/small.js", "https://news.example.com/", false)
	if got := lastReferer(); got != "https://news.example.com/" {
		t.Errorf("Referer %q, want the page's origin", got)
	}
}

func TestProbeHostFailsABodyThatStalls(t *testing.T) {
	ds, base := hostServer(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(make([]byte, 16<<10))
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(6 * time.Second):
		}
	})
	p := ds.probeHost(context.Background(), base+"/big.js", "", false)
	if p.ok || p.failure != hostFailBlocked || !strings.Contains(p.err, "stalled after 16384 bytes") {
		t.Fatalf("a body that freezes after the headers is a block, got %+v", p)
	}
}

func TestProbeHostAbortsWithTheStep(t *testing.T) {
	ds, base := hostServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(6 * time.Second):
		}
	})
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if p := ds.probeHost(ctx, base+"/slow.js", "", false); !p.aborted {
		t.Fatalf("a probe cut by the step's own stop is no verdict, got %+v", p)
	}
}

func TestCheckHostsSeesASizeDependentStall(t *testing.T) {
	small, script, big := make([]byte, 2<<10), make([]byte, 40<<10), make([]byte, 16<<10)
	var gzipped atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/small.png":
			_, _ = w.Write(small)
			return
		case "/app.js":
			if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
				_, _ = w.Write(script)
				return
			}
			gzipped.Add(1)
			w.Header().Set("Content-Encoding", "gzip")
			zw := gzip.NewWriter(w)
			_, _ = zw.Write(script)
			_ = zw.Close()
			return
		}
		w.Header().Set("Content-Length", "204800")
		_, _ = w.Write(big)
		w.(http.Flusher).Flush()
		select {
		case <-r.Context().Done():
		case <-time.After(6 * time.Second):
		}
	}))
	defer srv.Close()

	ds, f := hostRunFixture(t, "news.example.com")
	ds.hostProbeFn = nil
	results := ds.CheckHosts(context.Background(), HostTarget{
		Preset: "combo", Set: strategySet("combo"), Domains: []string{"news.example.com"},
		Candidates: []HostCandidate{
			{Host: "cdn.example.net", URLs: []string{srv.URL + "/small.png", srv.URL + "/big.js"}, Context: HostContextImage},
			{Host: "static.example.net", URLs: []string{srv.URL + "/app.js", srv.URL + "/big.js"}, Context: HostContextScript},
			{Host: "lib.example.net", URLs: []string{srv.URL + "/app.js"}, Context: HostContextScript},
		},
	})

	if gzipped.Load() == 0 {
		t.Fatal("the probe asks for gzip like a browser does, so the script must come back compressed")
	}
	got := verdicts(results)
	for _, host := range []string{"cdn.example.net", "static.example.net"} {
		if r := got[host]; r.Verdict != HostNotFixed || r.URL != srv.URL+"/big.js" || !strings.Contains(r.Error, "stalled after 16384 bytes") {
			t.Errorf("%s: a reply small on the wire, plain or 40 KiB inflated from a few hundred bytes of gzip, hides a block that stalls the 200 KiB one at 16 KiB, got %+v", host, r)
		}
	}
	if r := got["lib.example.net"]; r.Verdict != HostLoadsWithout || !r.Small || r.BytesRead != int64(len(script)) {
		t.Errorf("a gzip reply is small by the bytes that crossed the wire, not by the inflated body, got %+v", r)
	}
	if len(f.configs) != 2 {
		t.Fatalf("configurations %d", len(f.configs))
	}
}

func TestCheckHostsTriesAnUnresolvedHostWithTheSetsDNSFix(t *testing.T) {
	ds, f := hostRunFixture(t, "news.example.com")
	ds.dnsResults["news.example.com"] = &DNSDiscoveryResult{IsPoisoned: true}
	set := strategySet("combo")
	set.DNS.Enabled = true
	set.DNS.DoHURL = "https://1.1.1.1/dns-query"
	f.without[sample("docs.example.org")] = []hostProbe{unresolvedProbe, unresolvedProbe}
	f.with[sample("docs.example.org")] = []hostProbe{passedProbe, passedProbe}
	f.without[sample("gone.example.org")] = []hostProbe{unresolvedProbe, unresolvedProbe}
	f.with[sample("gone.example.org")] = []hostProbe{unresolvedProbe}
	privateProbe := hostProbe{failure: hostFailName, reserved: true, err: "0.0.0.0 is a private or local address, it is not probed"}
	f.without[sample("ads.example.org")] = []hostProbe{privateProbe, privateProbe}

	results := ds.CheckHosts(context.Background(), HostTarget{
		Preset: "combo", Set: set, Domains: []string{"news.example.com"},
		Candidates: []HostCandidate{
			cand("docs.example.org", HostContextStylesheet, true),
			cand("gone.example.org", HostContextImage, false),
			cand("ads.example.org", HostContextScript, false),
		},
	})

	got := verdicts(results)
	if got["docs.example.org"].Verdict != HostNeeded {
		t.Errorf("a name the resolver does not answer without b4 is tried through the set's DNS fix: %+v, calls %v", got["docs.example.org"], f.calls)
	}
	if got["gone.example.org"].Verdict != HostUnresolved || got["gone.example.org"].Error != unresolvedProbe.err {
		t.Errorf("a name the DNS fix does not resolve either stays unresolved: %+v", got["gone.example.org"])
	}
	if f.count("with "+sample("gone.example.org")) != 1 {
		t.Errorf("the with pass stops at the first name failure: %v", f.calls)
	}
	if got["ads.example.org"].Verdict != HostUnresolved || f.count("with "+sample("ads.example.org")) != 0 {
		t.Errorf("a private address answer is a local blocker's, never tried through the set's DNS fix: %+v, calls %v", got["ads.example.org"], f.calls)
	}
	if on := f.configs[len(f.configs)-1].Sets[0]; !on.DNS.Enabled || on.DNS.DoHURL == "" {
		t.Errorf("the strategy pass keeps the set's DNS fix: %+v", on.DNS)
	}
	ds.CheckSuite.mu.RLock()
	done, total := ds.HostsDone, ds.HostsTotal
	ds.CheckSuite.mu.RUnlock()
	if done != total {
		t.Errorf("progress ends at %d of %d", done, total)
	}
}

func TestProbeHostMarksAReservedAddress(t *testing.T) {
	previous := probeRefusesAddr
	probeRefusesAddr = utils.IsReservedAddr
	t.Cleanup(func() { probeRefusesAddr = previous })
	ds, base := hostServer(t, func(w http.ResponseWriter, r *http.Request) {})

	if p := ds.probeHost(context.Background(), base+"/a.js", "", false); p.failure != hostFailName || !p.reserved {
		t.Fatalf("a private address is a name failure the set's DNS fix must not retry, got %+v", p)
	}
}

func TestClassifyHostError(t *testing.T) {
	for _, tc := range []struct {
		err  error
		want hostFailure
	}{
		{&net.DNSError{Err: "no such host", Name: "gone.example.net", IsNotFound: true}, hostFailName},
		{&netprobe.ReservedAddrError{Addr: "10.0.0.1"}, hostFailName},
		{os.ErrDeadlineExceeded, hostFailBlocked},
		{syscall.ECONNRESET, hostFailBlocked},
		{errors.New("tls: first record does not look like a TLS handshake"), hostFailBlocked},
		{syscall.ECONNREFUSED, hostFailUnreachable},
		{&net.AddrError{Err: "no suitable address found", Addr: "v6only.example.net"}, hostFailUnreachable},
		{errors.New("remote error: tls: handshake failure"), hostFailUnreachable},
	} {
		if got, detail := classifyHostError(tc.err); got != tc.want {
			t.Errorf("%v: %v (%s), want %v", tc.err, got, detail, tc.want)
		}
	}
}

func TestHostWorkersFollowTheCPUs(t *testing.T) {
	if n := hostWorkers(); n < 1 || n > maxHostWorkers {
		t.Fatalf("workers %d", n)
	}
	var mu sync.Mutex
	var seen []int
	runHostWorkers(10, func(i int) {
		mu.Lock()
		seen = append(seen, i)
		mu.Unlock()
	})
	slices.Sort(seen)
	if !reflect.DeepEqual(seen, []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}) {
		t.Fatalf("every job runs once, got %v", seen)
	}
}
