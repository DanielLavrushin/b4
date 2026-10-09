package discovery

import (
	"net/http"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func answered400(domain, ip string) CheckResult {
	return CheckResult{Domain: domain, Status: CheckStatusFailed, StatusCode: http.StatusBadRequest, UsedIP: ip, Error: "server answered HTTP 400, the strategy corrupted the request"}
}

func TestALinkThatAnswers400WithoutAStrategyLeavesNothingToTest(t *testing.T) {
	ds := unresolvedSuite(t, true, "yt3.example")
	ds.dnsResults["yt3.example"] = &DNSDiscoveryResult{ExpectedIPs: []string{"203.0.113.7"}}

	ds.markBadLinks(map[string]CheckResult{"yt3.example": answered400("yt3.example", "203.0.113.7")})

	dr := ds.domainResults["yt3.example"]
	if dr.LinkStatus != http.StatusBadRequest || dr.Outcome != OutcomeBadLink {
		t.Fatalf("a clean request answered with 400 means the link is wrong, got status=%d outcome=%q", dr.LinkStatus, dr.Outcome)
	}
	if ds.needsBypass("yt3.example") {
		t.Fatal("a link that refuses a clean request cannot need a strategy, or the run goes on to the extended search")
	}
	if r := ds.fetchForDomain(ds.Domains[0], time.Second); !r.untried {
		t.Fatalf("no strategy may be fetched against it, every one would get the same 400: %+v", r)
	}
	if !ds.nothingLeftToTest() {
		t.Fatal("with no other domain, the run must stop after the baseline")
	}
}

func TestOnlyAnHonestHTTPSAnswerMarksABadLink(t *testing.T) {
	poisoned := &DNSDiscoveryResult{IsPoisoned: true, ExpectedIPs: []string{"203.0.113.7"}}
	cases := map[string]struct {
		checkURL string
		dns      *DNSDiscoveryResult
		result   CheckResult
		want     bool
	}{
		"no DNS check":                  {"https://yt3.example/a", nil, answered400("yt3.example", ""), true},
		"honest name":                   {"https://yt3.example/a", &DNSDiscoveryResult{ExpectedIPs: []string{"203.0.113.7"}}, answered400("yt3.example", "198.51.100.9"), true},
		"poisoned name, honest address": {"https://yt3.example/a", poisoned, answered400("yt3.example", "203.0.113.7"), true},
		"poisoned name, its sinkhole":   {"https://yt3.example/a", poisoned, answered400("yt3.example", "198.51.100.9"), false},
		"plain http":                    {"http://yt3.example/a", nil, answered400("yt3.example", ""), false},
		"another status":                {"https://yt3.example/a", nil, CheckResult{Domain: "yt3.example", Status: CheckStatusFailed, StatusCode: http.StatusNotFound}, false},
		"no answer":                     {"https://yt3.example/a", nil, CheckResult{Domain: "yt3.example", Status: CheckStatusFailed, Error: "connection reset"}, false},
	}
	for name, tc := range cases {
		ds := unresolvedSuite(t, true, "yt3.example")
		ds.Domains[0].CheckURL = tc.checkURL
		if tc.dns != nil {
			ds.dnsResults["yt3.example"] = tc.dns
		}
		ds.markBadLinks(map[string]CheckResult{"yt3.example": tc.result})
		if got := ds.badLink("yt3.example"); got != tc.want {
			t.Errorf("%s: bad link = %v, want %v", name, got, tc.want)
		}
	}
}

func TestABadLinkStopsOnlyItsOwnDomain(t *testing.T) {
	ds := unresolvedSuite(t, true, "yt3.example", "rutracker.example")

	ds.markBadLinks(map[string]CheckResult{
		"yt3.example":       answered400("yt3.example", ""),
		"rutracker.example": {Domain: "rutracker.example", Status: CheckStatusFailed, Error: "connection reset"},
	})

	if ds.nothingLeftToTest() {
		t.Fatal("the other domain still needs a strategy, so the run goes on")
	}
	if ds.Domain != "rutracker.example" {
		t.Fatalf("the single-domain steps must move to a domain they can test, primary is %q", ds.Domain)
	}
	if !ds.needsBypass("rutracker.example") || ds.allDomainsTransportBlocked() {
		t.Fatal("the domain the DPI blocks keeps its search")
	}
}

func TestSetVerdictNamesALinkThatAnswers400(t *testing.T) {
	ds := setRunFixture(t, "a.example", "yt3.example", "b.example")
	for _, d := range []string{"a.example", "yt3.example", "b.example"} {
		ds.put(d, presetNoBypass, CheckStatusFailed, PhaseBaseline, 0)
	}
	ds.domainResults["yt3.example"].LinkStatus = http.StatusBadRequest
	ds.put("a.example", "pair", CheckStatusComplete, PhaseStrategy, 0)
	ds.put("b.example", "pair", CheckStatusComplete, PhaseStrategy, 0)
	ds.determineBest()
	ds.buildStrategyGroups()

	v := ds.buildSetVerdict(ds.runDomains(), "")
	if v.Status != SetVerdictPartial || !reflect.DeepEqual(v.Uncovered, []string{"yt3.example"}) || !reflect.DeepEqual(v.BadLinks, []string{"yt3.example"}) || len(v.Unresolved) != 0 {
		t.Fatalf("the address with a bad link stays uncovered and is named as such, verdict = %+v", v)
	}
}

func TestHistoryKeepsWhyALinkWasNotTested(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	dr := &DomainDiscoveryResult{Domain: "yt3.example", LinkStatus: http.StatusBadRequest}
	dr.refreshOutcome(true)
	SaveToHistory(&CheckSuite{
		Id: "run-400", Status: CheckStatusComplete, EndTime: time.Now(),
		DomainDiscoveryResults: map[string]*DomainDiscoveryResult{"yt3.example": dr},
	}, cfgPath)

	e := LoadDiscoveryHistory(cfgPath).Entries[0]
	if e.LinkStatus != http.StatusBadRequest || e.EffectiveOutcome() != OutcomeBadLink {
		t.Fatalf("the entry must say the link answered 400 rather than that no strategy worked, got status=%d outcome=%q", e.LinkStatus, e.EffectiveOutcome())
	}
}
