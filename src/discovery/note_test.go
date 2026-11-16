package discovery

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestNormalizeNoteFoldsWhitespaceAndControlCharacters(t *testing.T) {
	cases := map[string]string{
		"":                                    "",
		"   ":                                 "",
		"  YouTube cache  ":                   "YouTube cache",
		"GGC node\non\tRostelecom\r\nAS12389": "GGC node on Rostelecom AS12389",
		"bell\x07and\x00null":                 "bell and null",
		"кэш YouTube у провайдера":            "кэш YouTube у провайдера",
	}
	for raw, want := range cases {
		got, ok := NormalizeNote(raw)
		if !ok || got != want {
			t.Errorf("NormalizeNote(%q) = %q, %v; want %q", raw, got, ok, want)
		}
	}
}

func TestNormalizeNoteCountsCharactersNotBytes(t *testing.T) {
	if _, ok := NormalizeNote(strings.Repeat("ж", MaxNoteRunes)); !ok {
		t.Errorf("a note of %d Cyrillic letters is within the limit, though it takes twice as many bytes", MaxNoteRunes)
	}
	if _, ok := NormalizeNote(strings.Repeat("a", MaxNoteRunes+1)); ok {
		t.Errorf("a note of %d characters must be refused", MaxNoteRunes+1)
	}
	if got, ok := NormalizeNote(strings.Repeat(" ", 500) + "short" + strings.Repeat("\n", 500)); !ok || got != "short" {
		t.Errorf("the limit applies after trimming, got %q, %v", got, ok)
	}
}

func TestAddressesRecordEveryProbedIPAndWhichOnesLoaded(t *testing.T) {
	dr := &DomainDiscoveryResult{Domain: "rr1---sn-abc.googlevideo.com"}

	dr.noteAddresses(CheckResult{Status: CheckStatusFailed, UsedIP: "203.0.113.20", triedIPs: []string{"203.0.113.10"}})
	dr.noteAddresses(CheckResult{Status: CheckStatusComplete, UsedIP: "203.0.113.20", triedIPs: []string{"203.0.113.10"}})
	dr.noteAddresses(CheckResult{Status: CheckStatusFailed, UsedIP: "203.0.113.20"})
	dr.noteAddresses(CheckResult{Status: CheckStatusFailed, untried: true, UsedIP: "198.51.100.1"})
	dr.noteAddresses(CheckResult{Status: CheckStatusComplete})

	want := []ProbeAddress{{IP: "203.0.113.10"}, {IP: "203.0.113.20", Loaded: true}}
	if !reflect.DeepEqual(dr.Addresses, want) {
		t.Errorf("addresses = %+v, want %+v: each probed IP once in first-seen order, loaded once any fetch from it completed, a later failure never clears that, and an untried fetch or one without a pinned IP adds nothing", dr.Addresses, want)
	}
}

func TestAFetchReportsTheAddressesItFellBackFrom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("<html><body>ok</body></html>"))
	}))
	defer srv.Close()

	ds := newProbeOnlySuite(srv.URL + "/")
	ds.Domains[0].Domain = "127.0.0.1"
	ds.dnsResults = map[string]*DNSDiscoveryResult{
		"127.0.0.1": {ExpectedIPs: []string{"127.0.0.3", "127.0.0.1"}},
	}
	res := ds.fetchForDomain(ds.Domains[0], 5*time.Second)
	if res.Status != CheckStatusComplete || res.UsedIP != "127.0.0.1" {
		t.Fatalf("the second address serves the page, got %q from %q (%s)", res.Status, res.UsedIP, res.Error)
	}
	if !slices.Equal(res.triedIPs, []string{"127.0.0.3"}) {
		t.Errorf("the address that refused the connection first must be reported too, got %v", res.triedIPs)
	}

	dr := &DomainDiscoveryResult{}
	dr.noteAddresses(res)
	if want := []ProbeAddress{{IP: "127.0.0.3"}, {IP: "127.0.0.1", Loaded: true}}; !reflect.DeepEqual(dr.Addresses, want) {
		t.Errorf("addresses = %+v, want %+v", dr.Addresses, want)
	}
}

func TestHistoryKeepsTheRunsNoteAndTheProbedAddresses(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.json")
	addrs := []ProbeAddress{{IP: "203.0.113.10"}, {IP: "203.0.113.20", Loaded: true}}
	SaveToHistory(&CheckSuite{
		Id: "run-note", Status: CheckStatusComplete, EndTime: time.Now(), Note: "YouTube cache on the ISP", SetId: "yt",
		SetVerdict: &SetVerdict{Status: SetVerdictNone},
		DomainDiscoveryResults: map[string]*DomainDiscoveryResult{
			"rr1---sn-abc.googlevideo.com": {Domain: "rr1---sn-abc.googlevideo.com", Addresses: addrs},
			"youtube.com":                  {Domain: "youtube.com"},
		},
	}, cfgPath)

	hist := LoadDiscoveryHistory(cfgPath)
	if len(hist.Entries) != 2 {
		t.Fatalf("entries = %d", len(hist.Entries))
	}
	for _, e := range hist.Entries {
		if e.Note != "YouTube cache on the ISP" {
			t.Errorf("every site of the run must keep its note, %s has %q", e.Domain, e.Note)
		}
		switch e.Domain {
		case "rr1---sn-abc.googlevideo.com":
			if !reflect.DeepEqual(e.Addresses, addrs) {
				t.Errorf("the probed addresses must survive the save, got %+v", e.Addresses)
			}
		case "youtube.com":
			if e.Addresses != nil {
				t.Errorf("a site probed through no pinned address records none, got %+v", e.Addresses)
			}
		}
	}
	if rec := hist.SetRuns["yt"]; rec.Note != "YouTube cache on the ISP" {
		t.Errorf("the saved set run must keep the note too, got %q", rec.Note)
	}

	noteOf := func(domain string) string {
		for _, e := range LoadDiscoveryHistory(cfgPath).Entries {
			if e.Domain == domain {
				return e.Note
			}
		}
		t.Fatalf("no history entry for %s", domain)
		return ""
	}
	rerun := func(id, source, note string) {
		SaveToHistory(&CheckSuite{
			Id: id, Status: CheckStatusComplete, EndTime: time.Now(), Source: source, Note: note, SetId: "yt",
			SetVerdict: &SetVerdict{Status: SetVerdictNone},
			DomainDiscoveryResults: map[string]*DomainDiscoveryResult{
				"youtube.com": {Domain: "youtube.com"},
			},
		}, cfgPath)
	}

	for _, source := range []string{SourceWatchdog, SourceMCP} {
		rerun("run-"+source, source, "")
		if got := noteOf("youtube.com"); got != "YouTube cache on the ISP" {
			t.Errorf("a %s run cannot take a note, so it must keep the one the user wrote, got %q", source, got)
		}
		if got := LoadDiscoveryHistory(cfgPath).SetRuns["yt"].Note; got != "YouTube cache on the ISP" {
			t.Errorf("a %s run of the set must keep the set's last note too, got %q", source, got)
		}
	}

	rerun("run-mcp-noted", SourceMCP, "asked by the assistant")
	if got := noteOf("youtube.com"); got != "asked by the assistant" {
		t.Errorf("a run with its own note replaces the old one, got %q", got)
	}

	rerun("run-web", SourceWeb, "")
	if got := noteOf("youtube.com"); got != "" {
		t.Errorf("a search started from the page without a note replaces the entry with its own, got %q", got)
	}
	if got := LoadDiscoveryHistory(cfgPath).SetRuns["yt"].Note; got != "" {
		t.Errorf("a set run started from the page without a note clears the set's note, got %q", got)
	}
}
