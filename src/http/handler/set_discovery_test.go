package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/hub/hubtest"
	"github.com/daniellavrushin/b4/tables"
)

func legacyReplaceStrategy(dst, strategy *config.SetConfig) {
	tcp := strategy.TCP
	tcp.DPortFilter = dst.TCP.DPortFilter
	tcp.RSTProtection = dst.TCP.RSTProtection
	if !strategy.TCP.IPBlockDetect.Enabled {
		tcp.IPBlockDetect = dst.TCP.IPBlockDetect
	}
	udp := strategy.UDP
	udp.DPortFilter = dst.UDP.DPortFilter

	pins := dst.DNS.Pins
	dst.TCP = tcp
	dst.UDP = udp
	dst.Fragmentation = strategy.Fragmentation
	dst.Faking = strategy.Faking
	dst.DNS = strategy.DNS
	dst.DNS.Pins = pins
	dst.Targets.TLSVersion = strategy.Targets.TLSVersion
	dst.Targets.IPVersion = strategy.Targets.IPVersion
}

func replaceLegacyDst(pins map[string][]string) config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name = "set-a", "meduza"
	set.Targets.SNIDomains = []string{"meduza.io"}
	set.Targets.TLSVersion = "1.3"
	set.TCP.DPortFilter = "443,8443"
	set.TCP.RSTProtection = config.RSTProtectionConfig{Enabled: true, TTLTolerance: 4}
	set.TCP.IPBlockDetect = config.IPBlockDetectConfig{Enabled: true, RetransmitThreshold: 8}
	set.UDP.DPortFilter = "443"
	set.UDP.Mode = "drop"
	set.DNS = config.DNSConfig{Enabled: true, TargetDNS: "1.1.1.1", Pins: pins}
	set.Discovery.URLs = []string{"https://meduza.io/"}
	return set
}

func replaceLegacyStrategy(mutate func(*config.SetConfig)) config.SetConfig {
	set := config.NewSetConfig()
	set.TCP.ConnBytesLimit = 7
	set.TCP.Win = config.WinConfig{Mode: "oscillate", Values: []int{1, 2}}
	set.UDP.Mode = "fake"
	set.UDP.DPortFilter = "53"
	set.UDP.FakePayloadData = []byte{1, 2, 3}
	set.Fragmentation.Strategy = "tcp"
	set.Fragmentation.SeqOverlapBytes = []byte{0x16}
	set.Faking.Strategy = "pastseq"
	set.Faking.PayloadData = []byte{7}
	set.Targets.TLSVersion = "1.2"
	set.Targets.IPVersion = "6"
	set.DNS = config.DNSConfig{DoHURL: "https://dns.example/dns-query"}
	if mutate != nil {
		mutate(&set)
	}
	return set
}

func TestReplaceStrategyMatchesTheLegacyCopy(t *testing.T) {
	cases := []struct {
		name   string
		pins   map[string][]string
		mutate func(*config.SetConfig)
	}{
		{name: "defaults"},
		{name: "own pins", pins: map[string][]string{"meduza.io": {"9.9.9.9"}}},
		{name: "enabled ip block detection", mutate: func(s *config.SetConfig) {
			s.TCP.IPBlockDetect = config.IPBlockDetectConfig{Enabled: true, SynDetect: true}
		}},
		{name: "enabled DNS", pins: map[string][]string{"x.io": {"1.2.3.4"}}, mutate: func(s *config.SetConfig) {
			s.DNS = config.DNSConfig{Enabled: true, FragmentQuery: true, Pins: map[string][]string{"meduza.io": {"5.5.5.5"}}}
		}},
		{name: "nil slices", mutate: func(s *config.SetConfig) {
			s.TCP.Win.Values = nil
			s.Faking.TLSMod = nil
			s.Fragmentation.StrategyPool = nil
			s.UDP.FakePayloadData = nil
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			strategy := replaceLegacyStrategy(c.mutate)
			want := replaceLegacyDst(c.pins)
			legacyReplaceStrategy(&want, &strategy)

			got := replaceLegacyDst(c.pins)
			replaceStrategy(&got, &strategy)

			if !reflect.DeepEqual(got, want) {
				t.Fatalf("the rewritten replace must match the legacy one\n got: %+v\nwant: %+v", got, want)
			}
			gotJSON, _ := json.Marshal(got)
			wantJSON, _ := json.Marshal(want)
			if string(gotJSON) != string(wantJSON) {
				t.Fatalf("JSON differs\n got: %s\nwant: %s", gotJSON, wantJSON)
			}
		})
	}
}

func replaceWith(t *testing.T, mux *http.ServeMux, req DiscoveryReplaceRequest) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/discovery/replace", strings.NewReader(string(body))))
	return rec
}

func discoveredStrategy() config.SetConfig {
	strategy := config.NewSetConfig()
	strategy.Faking.Strategy = "pastseq"
	strategy.TCP.ConnBytesLimit = 7
	strategy.UDP.Mode = "drop"
	strategy.Targets.TLSVersion = "1.2"
	strategy.Targets.SNIDomains = []string{"www.youtube.com"}
	return strategy
}

func TestReplaceStrategyOnlyKeepsTargetsWhenAsked(t *testing.T) {
	api, mux := replaceFixture(t, func(target *config.SetConfig) {
		target.UDP.Mode = "fake"
	})
	rec := replaceWith(t, mux, DiscoveryReplaceRequest{
		SetId:        "set-a",
		Set:          discoveredStrategy(),
		StrategyOnly: true,
		KeepTargets:  true,
		ProbeURLs:    []string{"meduza.io", "https://MEDUZA.io/en", "http://10.0.0.1/", "www.dw.com/ru"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d (%s)", rec.Code, rec.Body.String())
	}

	got, other := replaceSets(api)
	if got.Faking.Strategy != "pastseq" || got.TCP.ConnBytesLimit != 7 {
		t.Errorf("the discovered strategy must be adopted, got faking=%q conn=%d", got.Faking.Strategy, got.TCP.ConnBytesLimit)
	}
	if got.UDP.Mode != "fake" || got.Targets.TLSVersion != "" {
		t.Errorf("strategy-only must leave UDP and the target filters alone, got udp=%q tls=%q", got.UDP.Mode, got.Targets.TLSVersion)
	}
	if !reflect.DeepEqual(got.Targets.SNIDomains, []string{"meduza.io"}) {
		t.Errorf("keep_targets must not touch the set's domains, got %v", got.Targets.SNIDomains)
	}
	if !domainInList(other.Targets.SNIDomains, "cdn.meduza.io") {
		t.Errorf("keep_targets must not take domains from other sets, got %v", other.Targets.SNIDomains)
	}
	if pins := got.DNS.Pins["meduza.io"]; len(pins) != 1 || pins[0] != "1.1.1.1" {
		t.Errorf("without pins in the request the set's pins stay, got %v", got.DNS.Pins)
	}
	want := []string{"https://meduza.io/", "https://www.dw.com/ru"}
	if !reflect.DeepEqual(got.Discovery.URLs, want) {
		t.Errorf("probe URLs must be stored normalised, got %v want %v", got.Discovery.URLs, want)
	}
	if !strings.Contains(rec.Body.String(), `"moved":null`) {
		t.Errorf("nothing moves with keep_targets, got %s", rec.Body.String())
	}
}

func TestReplaceKeepTargetsReplacesPinsOnlyWhenGiven(t *testing.T) {
	api, mux := replaceFixture(t, nil)
	rec := replaceWith(t, mux, DiscoveryReplaceRequest{
		SetId:        "set-a",
		Set:          discoveredStrategy(),
		StrategyOnly: true,
		KeepTargets:  true,
		Pins:         map[string][]string{"meduza.io": {"9.9.9.9"}},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d (%s)", rec.Code, rec.Body.String())
	}
	got, _ := replaceSets(api)
	if pins := got.DNS.Pins["meduza.io"]; len(pins) != 1 || pins[0] != "9.9.9.9" {
		t.Errorf("given pins replace the set's pins for that site, got %v", got.DNS.Pins["meduza.io"])
	}
	if len(got.DNS.Pins["other.io"]) != 1 {
		t.Errorf("pins of other sites stay, got %v", got.DNS.Pins)
	}
	if !reflect.DeepEqual(got.Discovery.URLs, []string{}) {
		t.Errorf("no probe_urls leaves the stored list alone, got %#v", got.Discovery.URLs)
	}
}

func TestReplaceStrategyOnlyStillMovesDomainsByDefault(t *testing.T) {
	api, mux := replaceFixture(t, func(target *config.SetConfig) {
		target.UDP.Mode = "fake"
	})
	rec := replaceWith(t, mux, DiscoveryReplaceRequest{
		SetId:        "set-a",
		Set:          discoveredStrategy(),
		StrategyOnly: true,
		Domains:      []string{"cdn.meduza.io"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d (%s)", rec.Code, rec.Body.String())
	}
	got, other := replaceSets(api)
	if got.UDP.Mode != "fake" {
		t.Errorf("strategy-only must leave UDP alone, got %q", got.UDP.Mode)
	}
	if !domainInList(got.Targets.SNIDomains, "cdn.meduza.io") || domainInList(other.Targets.SNIDomains, "cdn.meduza.io") {
		t.Errorf("without keep_targets the domain moves into the set, target=%v other=%v", got.Targets.SNIDomains, other.Targets.SNIDomains)
	}
}

func TestReplaceNeedsDomainsUnlessTargetsAreKept(t *testing.T) {
	_, mux := replaceFixture(t, nil)
	if rec := replaceWith(t, mux, DiscoveryReplaceRequest{SetId: "set-a", Set: discoveredStrategy(), StrategyOnly: true}); rec.Code != http.StatusBadRequest {
		t.Errorf("domains are required without keep_targets, got %d", rec.Code)
	}
	if rec := replaceWith(t, mux, DiscoveryReplaceRequest{SetId: "missing", Set: discoveredStrategy(), KeepTargets: true}); rec.Code != http.StatusNotFound {
		t.Errorf("an unknown set is 404, got %d", rec.Code)
	}
}

func stubDiscoveryDSCPStamp(t *testing.T, stamp func(dscp *config.DSCPProbe) (*int, error)) {
	t.Helper()
	orig := discoveryDSCPStamp
	t.Cleanup(func() { discoveryDSCPStamp = orig })
	discoveryDSCPStamp = func(_ *config.Config, dscp *config.DSCPProbe) (*int, error) {
		return stamp(dscp)
	}
}

func stampsTheSetsValue(dscp *config.DSCPProbe) (*int, error) {
	if dscp == nil {
		return nil, nil
	}
	value := dscp.Value
	return &value, nil
}

func stampsNoSetValue(*config.DSCPProbe) (*int, error) {
	return nil, nil
}

func strategyTestedOn(dscp int, ipVersion string) config.SetConfig {
	strategy := discoveredStrategy()
	strategy.Targets.IPVersion = ipVersion
	if dscp >= 0 {
		strategy.DSCP = config.SetDSCPConfig{Enabled: true, Value: dscp}
	}
	return strategy
}

func TestReplaceRefusesAStrategyTestedOnAnotherDSCPPath(t *testing.T) {
	stampsIPv4Only := func(dscp *config.DSCPProbe) (*int, error) {
		if dscp == nil || !dscp.IPv4 {
			return nil, nil
		}
		return stampsTheSetsValue(dscp)
	}
	for _, tc := range []struct {
		name            string
		setDSCP         int
		stamp           func(*config.DSCPProbe) (*int, error)
		tested          int
		ipVersion       string
		refusal, params string
	}{
		{"tested on 25, the set's traffic carries 7", 7, stampsTheSetsValue, 25, "", "dscp_path_changed", "map[now:7 set:meduza tested:25]"},
		{"tested without a value, the set's traffic carries 7", 7, stampsTheSetsValue, -1, "", "dscp_path_tested_without", "map[now:7 set:meduza]"},
		{"tested on 25, the set carries no value", -1, stampsTheSetsValue, 25, "", "dscp_path_now_without", "map[set:meduza tested:25]"},
		{"tested on 7, the firewall no longer stamps the set's value", 7, stampsNoSetValue, 7, "", "dscp_path_now_without", "map[set:meduza tested:7]"},
		{"tested over IPv4 without a value, the set's 7 reaches IPv4", 7, stampsIPv4Only, -1, "4", "dscp_path_tested_without", "map[now:7 set:meduza]"},
		{"tested on 7, the set's traffic carries 7", 7, stampsTheSetsValue, 7, "", "", ""},
		{"tested without a value, the set carries none", -1, stampsTheSetsValue, -1, "", "", ""},
		{"tested without a value, the firewall cannot stamp the set's 7", 7, stampsNoSetValue, -1, "", "", ""},
		{"tested over IPv6 without a value, the set's 7 reaches only IPv4", 7, stampsIPv4Only, -1, "6", "", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stubDiscoveryDSCPStamp(t, tc.stamp)
			api, mux := replaceFixture(t, func(target *config.SetConfig) {
				if tc.setDSCP >= 0 {
					target.DSCP = config.SetDSCPConfig{Enabled: true, Value: tc.setDSCP}
				}
			})
			rec := replaceWith(t, mux, DiscoveryReplaceRequest{SetId: "set-a", Set: strategyTestedOn(tc.tested, tc.ipVersion), StrategyOnly: true, KeepTargets: true})
			got, _ := replaceSets(api)
			if tc.refusal == "" {
				if rec.Code != http.StatusOK || got.Faking.Strategy != "pastseq" {
					t.Errorf("the strategy was tested on the path the set's traffic takes, so it is written: %d (%s)", rec.Code, rec.Body.String())
				}
				return
			}
			var body APIError
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode %q: %v", rec.Body.String(), err)
			}
			if rec.Code != http.StatusConflict || len(body.Fields) != 1 || body.Fields[0].Code != tc.refusal || fmt.Sprint(body.Fields[0].Params) != tc.params {
				t.Errorf("want a %s refusal with %s, got %d (%s)", tc.refusal, tc.params, rec.Code, rec.Body.String())
			}
			if got.Faking.Strategy == "pastseq" {
				t.Error("nothing may be written from a test on another network path")
			}
		})
	}
}

func TestReplaceWaitsWhileTheDSCPRulesAreStillChanging(t *testing.T) {
	stubDiscoveryDSCPStamp(t, func(*config.DSCPProbe) (*int, error) { return nil, tables.ErrDiscoveryDSCPPending })
	api, mux := replaceFixture(t, func(target *config.SetConfig) {
		target.DSCP = config.SetDSCPConfig{Enabled: true, Value: 7}
	})
	rec := replaceWith(t, mux, DiscoveryReplaceRequest{SetId: "set-a", Set: strategyTestedOn(7, ""), StrategyOnly: true, KeepTargets: true})
	var body APIError
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode %q: %v", rec.Body.String(), err)
	}
	if rec.Code != http.StatusConflict || len(body.Fields) != 1 || body.Fields[0].Code != "dscp_rules_pending" {
		t.Errorf("while b4 is still applying its DSCP rules the set's network path is unknown, so the write must wait: %d (%s)", rec.Code, rec.Body.String())
	}
	if got, _ := replaceSets(api); got.Faking.Strategy == "pastseq" {
		t.Error("nothing may be written while the set's network path is unknown")
	}
}

func TestReplaceStrategyKeepsTheSetsUDPProtocolSelection(t *testing.T) {
	const voicePorts = "19294-19344,50000-50100"
	voice := func(target *config.SetConfig) {
		target.Targets.SNIDomains = nil
		target.TCP.DPortFilter = ""
		target.UDP.Mode = config.UDPModeFake
		target.UDP.DPortFilter = voicePorts
		target.UDP.FilterL7 = []string{config.UDPL7Discord, config.UDPL7STUN}
	}
	wantL7 := []string{config.UDPL7Discord, config.UDPL7STUN}

	api, mux := replaceFixture(t, voice)
	rec := replaceWith(t, mux, DiscoveryReplaceRequest{SetId: "set-a", Set: discoveredStrategy(), KeepTargets: true})
	if rec.Code != http.StatusOK {
		t.Fatalf("replace: %d (%s)", rec.Code, rec.Body.String())
	}
	got, _ := replaceSets(api)
	if got.UDP.Mode != "drop" || got.Faking.Strategy != "pastseq" {
		t.Errorf("the discovered strategy must be adopted, got udp=%q faking=%q", got.UDP.Mode, got.Faking.Strategy)
	}
	if !reflect.DeepEqual(got.UDP.FilterL7, wantL7) || got.UDP.DPortFilter != voicePorts {
		t.Errorf("the protocols a set selects and its UDP ports are not part of the strategy and must survive: %+v", got.UDP)
	}
	if got.DeclaresDestinationTargets() || got.TCP.DPortFilter != "" {
		t.Errorf("keep_targets must leave the set without targets: %+v tcp=%q", got.Targets, got.TCP.DPortFilter)
	}

	api, mux = replaceFixture(t, voice)
	rec = replaceWith(t, mux, DiscoveryReplaceRequest{SetId: "set-a", Set: discoveredStrategy(), Domains: []string{"discord.media"}})
	expectCode(t, rec, http.StatusBadRequest, "validation_failed")
	var refusal APIError
	decodeInto(t, rec, &refusal)
	if len(refusal.Fields) != 1 || refusal.Fields[0].Path != "sets[0].udp.filter_l7" || refusal.Fields[0].Code != "conflict" {
		t.Errorf("moving domains into a set that selects UDP protocols must be refused, not turn it into a domain set: %+v", refusal.Fields)
	}
	if got, _ := replaceSets(api); !reflect.DeepEqual(got.UDP.FilterL7, wantL7) || got.DeclaresDestinationTargets() {
		t.Errorf("a refused replace must leave the set as it was: %+v %+v", got.UDP, got.Targets)
	}
}

func TestReplaceStrategyKeepsHowTheSetHandlesCalls(t *testing.T) {
	voice := func(target *config.SetConfig) {
		target.Targets.SNIDomains = nil
		target.TCP.DPortFilter = ""
		target.UDP.DPortFilter = "19294-19344,50000-50100"
		target.UDP.FilterL7 = []string{config.UDPL7Discord, config.UDPL7STUN}
	}
	for _, c := range []struct {
		name     string
		l7       bool
		strategy string
		want     string
	}{
		{"a zero fill keeps the STUN fake of a set that handles only calls", true, "", config.FakePayloadAutoSTUN},
		{"a generated QUIC Initial keeps it too", true, config.FakePayloadAutoQUIC, config.FakePayloadAutoSTUN},
		{"a payload the strategy chose replaces it", true, config.FakePayloadPreset1, config.FakePayloadPreset1},
		{"a set that handles calls like other UDP takes the zero fill", false, "", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			api, mux := replaceFixture(t, func(target *config.SetConfig) {
				if c.l7 {
					voice(target)
				}
				target.UDP.FilterSTUN = false
				target.UDP.FakePayloadFile = config.FakePayloadAutoSTUN
			})
			strategy := discoveredStrategy()
			strategy.UDP.Mode = config.UDPModeFake
			strategy.UDP.FakePayloadFile = c.strategy
			rec := replaceWith(t, mux, DiscoveryReplaceRequest{SetId: "set-a", Set: strategy, KeepTargets: true})
			if rec.Code != http.StatusOK {
				t.Fatalf("replace: %d (%s)", rec.Code, rec.Body.String())
			}
			got, _ := replaceSets(api)
			if got.UDP.FilterSTUN {
				t.Error("whether the set passes call setup belongs to the traffic it matches, not to the strategy, and must survive")
			}
			if got.UDP.FakePayloadFile != c.want {
				t.Errorf("fake payload %q, want %q", got.UDP.FakePayloadFile, c.want)
			}
		})
	}
}

func TestAPlainFixSetSavedWithCallsLikeOtherUDPHasTheSameStrategy(t *testing.T) {
	plainFix := func(filterSTUN bool) config.SetConfig {
		set := config.SetConfig{
			TCP:           config.TCPConfig{ConnBytesLimit: 19},
			UDP:           config.UDPConfig{Mode: config.ConfigOff, FilterSTUN: filterSTUN},
			Fragmentation: config.FragmentationConfig{Strategy: config.ConfigNone},
			Faking:        config.FakingConfig{SNI: false},
		}
		config.ApplySetDefaults(&set)
		return set
	}
	existing := plainFix(false)
	existing.Id, existing.Name, existing.Enabled = "old", "dns-redirect", true
	existing.Targets.SNIDomains = []string{"example.com"}
	existing.Targets.DomainsToMatch = []string{"example.com"}
	api, _ := discoveryAPI(t, &existing)

	found := plainFix(true)
	if !setsHaveSimilarConfig(&existing, &found) {
		t.Error("whether the set passes call setup belongs to the traffic it matches, so a plain-fix set from 1.87, saved with it off, must still match the same plain fix found now")
	}
	if got := api.setCoveringDomainWith("example.com", &found); got == nil || got.Id != "old" {
		t.Error("the plain-fix set that already covers example.com must be found, or applying the result adds a second set with the same strategy")
	}
}

func discoveryAPI(t *testing.T, sets ...*config.SetConfig) (*API, *http.ServeMux) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Sets = sets
	api := &API{cfgPtr: testCfgPtr(&cfg), geodataManager: geodat.NewGeodataManager("", "")}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterDiscoveryApi()
	api.RegisterSetsApi()
	return api, mux
}

func namedSet(id, name string, enabled bool, domains ...string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = id, name, enabled
	set.Targets.SNIDomains = append([]string{}, domains...)
	set.Targets.DomainsToMatch = append([]string{}, domains...)
	return &set
}

func TestStartDiscoveryForASet(t *testing.T) {
	stored := namedSet("yt", "YouTube", true, "youtube.com")
	stored.Discovery.URLs = []string{"https://www.youtube.com/"}
	empty := namedSet("empty", "Empty", true, "example.com")
	_, mux := discoveryAPI(t, stored, empty)

	start := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/discovery/start", strings.NewReader(body)))
		return rec
	}

	expectCode(t, start(`{"set_id":"nope"}`), http.StatusNotFound, "not_found")
	expectCode(t, start(`{"set_id":"empty"}`), http.StatusBadRequest, "no_urls")
	expectCode(t, start(`{"set_id":"empty","check_urls":["ftp://example.com/"]}`), http.StatusBadRequest, "no_urls")
	expectCode(t, start(`{"set_id":"empty","check_urls":["a.example","b.example","c.example","d.example","e.example","f.example"]}`), http.StatusBadRequest, "too_many_urls")
	expectCode(t, start(`{"set_id":"yt","check_urls":["https://www.youtube.com/","http://192.168.1.1/"]}`), http.StatusBadRequest, "reserved_host")

	for _, body := range []string{
		`{"check_urls":["http://127.0.0.1:8080/admin"]}`,
		`{"check_url":"localhost"}`,
		`{"check_url":"LOCALHOST:8080/x"}`,
		`{"check_urls":["[::1]/"]}`,
		`{"check_urls":["http://10.0.0.1:bad/"]}`,
		`{"check_urls":["youtube.com","100.64.0.1"]}`,
	} {
		expectCode(t, start(body), http.StatusBadRequest, "reserved_host")
	}

	for _, body := range []string{`{"set_id":"yt"}`, `{"set_id":"empty","check_urls":["example.com"]}`, `{"check_url":"youtube.com"}`} {
		if rec := start(body); rec.Code != http.StatusInternalServerError || !strings.Contains(rec.Body.String(), "runtime") {
			t.Errorf("%s must pass validation and reach the runtime, got %d (%s)", body, rec.Code, rec.Body.String())
		}
	}
}

func TestProbeInputHost(t *testing.T) {
	cases := map[string]string{
		"youtube.com":                 "youtube.com",
		"https://www.youtube.com/x":   "www.youtube.com",
		"localhost:8080/x":            "localhost",
		"[::1]/":                      "::1",
		"http://10.0.0.1:bad/":        "10.0.0.1",
		"http://u:p@[fe80::1]:x/path": "fe80::1",
		"\"meduza.io\"":               "meduza.io",
		"":                            "",
		"::1":                         "::1",
		"::":                          "::",
		"fe80::1":                     "fe80::1",
		"fd00::1/path":                "fd00::1",
		"https://::1/":                "::1",
	}
	for in, want := range cases {
		if got := probeInputHost(in); got != want {
			t.Errorf("probeInputHost(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCheckDomainNamesTheSetThatHandlesIt(t *testing.T) {
	broad := namedSet("broad", "Broad", true, "youtube.com")
	exact := namedSet("exact", "Exact", true, "www.youtube.com")
	off := namedSet("off", "Off", false, "www.youtube.com")
	_, mux := discoveryAPI(t, off, broad, exact)

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/sets/check-domain?domain=www.youtube.com,m.youtube.com,example.org", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("check-domain: %d (%s)", rec.Code, rec.Body.String())
	}
	var matches []SetDomainMatch
	decodeInto(t, rec, &matches)

	handles := map[string]string{}
	for _, m := range matches {
		if m.Handles {
			if prev, dup := handles[m.Domain]; dup {
				t.Errorf("%s is handled by %s and %s", m.Domain, prev, m.SetId)
			}
			handles[m.Domain] = m.SetId
		}
	}
	if handles["www.youtube.com"] != "exact" {
		t.Errorf("the engine picks the most specific enabled entry for www.youtube.com, got %q", handles["www.youtube.com"])
	}
	if handles["m.youtube.com"] != "broad" {
		t.Errorf("m.youtube.com falls to the broad set, got %q", handles["m.youtube.com"])
	}
	if _, ok := handles["example.org"]; ok {
		t.Error("nothing handles example.org")
	}
	if !strings.Contains(rec.Body.String(), `"handles":true`) || strings.Contains(rec.Body.String(), `"handles":false`) {
		t.Errorf("handles is omitted when false, got %s", rec.Body.String())
	}
}

func suggest(t *testing.T, mux *http.ServeMux, setID string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/discovery/suggest?set_id="+setID, nil))
	return rec
}

func TestSuggestDiscoveryURLsForASet(t *testing.T) {
	shadow := namedSet("shadow", "Shadow", true, "www.youtube.com")
	yt := namedSet("yt", "YouTube", true, "youtube.com", "googlevideo.com")
	yt.Targets.SNIDomains = append(yt.Targets.SNIDomains, "regexp:.*\\.ytimg\\.com$", "*.bad", "1.1.1.1", "M.YouTube.com")
	yt.Targets.GeoSiteCategories = []string{"youtube"}
	yt.Discovery.URLs = []string{"https://m.youtube.com/"}
	api, mux := discoveryAPI(t, shadow, yt)

	now := time.Now()
	err := discovery.UpdateHistory(api.getCfg().ConfigPath, func(h *discovery.DiscoveryHistory) bool {
		h.Entries = []discovery.HistoryEntry{
			{Domain: "tv.youtube.com", Url: "https://tv.youtube.com/", EndTime: now.Add(-time.Hour), Applied: map[string]discovery.AppliedMark{"p": {SetId: "yt"}}},
			{Domain: "music.youtube.com", Url: "https://music.youtube.com/", EndTime: now, SetId: "yt"},
			{Domain: "m.youtube.com", Url: "https://m.youtube.com/feed", EndTime: now, SetId: "yt"},
			{Domain: "meduza.io", Url: "https://meduza.io/", EndTime: now, SetId: "other"},
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}

	rec := suggest(t, mux, "yt")
	if rec.Code != http.StatusOK {
		t.Fatalf("suggest: %d (%s)", rec.Code, rec.Body.String())
	}
	var resp DiscoverySuggestResponse
	decodeInto(t, rec, &resp)
	if resp.SetId != "yt" {
		t.Errorf("set_id = %q", resp.SetId)
	}

	want := []DiscoverySuggestion{
		{URL: "https://m.youtube.com/", Host: "m.youtube.com", Source: "stored", OwnerSetId: "yt", OwnerSetName: "YouTube"},
		{URL: "https://music.youtube.com/", Host: "music.youtube.com", Source: "history", OwnerSetId: "yt", OwnerSetName: "YouTube"},
		{URL: "https://tv.youtube.com/", Host: "tv.youtube.com", Source: "history", OwnerSetId: "yt", OwnerSetName: "YouTube"},
		{URL: "https://www.youtube.com/", Host: "www.youtube.com", Source: "detector", OwnerSetId: "shadow", OwnerSetName: "Shadow"},
		{URL: "https://youtube.com/", Host: "youtube.com", Source: "domain", OwnerSetId: "yt", OwnerSetName: "YouTube"},
		{URL: "https://googlevideo.com/", Host: "googlevideo.com", Source: "domain", OwnerSetId: "yt", OwnerSetName: "YouTube"},
	}
	if !reflect.DeepEqual(resp.URLs, want) {
		t.Fatalf("suggestions differ\n got: %+v\nwant: %+v", resp.URLs, want)
	}
	if strings.Contains(rec.Body.String(), `"owner_set_id":""`) {
		t.Errorf("an empty owner must be omitted, got %s", rec.Body.String())
	}
}

func TestSuggestDiscoveryURLsFromServicesAndCap(t *testing.T) {
	geo := namedSet("geo", "Geo", false)
	geo.Targets.GeoSiteCategories = []string{"youtube", "meta"}
	many := namedSet("many", "Many", true)
	for i := 1; i <= 12; i++ {
		many.Targets.SNIDomains = append(many.Targets.SNIDomains, fmt.Sprintf("a%d.example", i))
	}
	_, mux := discoveryAPI(t, geo, many)

	var resp DiscoverySuggestResponse
	decodeInto(t, suggest(t, mux, "geo"), &resp)
	want := []DiscoverySuggestion{
		{URL: "https://instagram.com/", Host: "instagram.com", Source: "service"},
		{URL: "https://youtube.com/", Host: "youtube.com", Source: "service"},
	}
	if !reflect.DeepEqual(resp.URLs, want) {
		t.Errorf("a disabled geosite-only set still gets its services, got %+v", resp.URLs)
	}

	decodeInto(t, suggest(t, mux, "many"), &resp)
	if len(resp.URLs) != maxDiscoverySuggestions || resp.URLs[0].Host != "a1.example" || resp.URLs[9].Host != "a10.example" {
		t.Errorf("suggestions are capped at %d in order, got %+v", maxDiscoverySuggestions, resp.URLs)
	}

	empty := namedSet("none", "None", true)
	_, mux = discoveryAPI(t, empty)
	rec := suggest(t, mux, "none")
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"urls":[]`) {
		t.Errorf("a set with nothing to suggest returns an empty list, got %d %s", rec.Code, rec.Body.String())
	}
	expectCode(t, suggest(t, mux, "missing"), http.StatusNotFound, "not_found")
	expectCode(t, suggest(t, mux, ""), http.StatusBadRequest, "bad_request")
}

func TestSetsAlwaysCarryADiscoveryBlock(t *testing.T) {
	var set config.SetConfig
	api := &API{}
	api.initializeSetDefaults(&set)
	if set.Discovery.URLs == nil {
		t.Fatal("a set built from a partial body must still carry discovery.urls = []")
	}
}

func TestHubReplaceKeepsTheSetsDiscoveryURLs(t *testing.T) {
	env := newHubEnv(t)
	shared := hubStrategySet("Shared", "rutracker.org")
	shared.Discovery.URLs = []string{"https://leak.example/"}
	cs, _ := hubtest.CatalogueSet(t, "rt-1", 1, &shared, func(string) ([]byte, error) { return nil, nil })
	env.publish(t, cs)

	old := hubStrategySet("Old", "rutracker.org")
	old.Id = "old-1"
	old.Discovery.URLs = []string{"https://rutracker.org/forum/"}
	env.update(func(cfg *config.Config) { cfg.Sets = []*config.SetConfig{&old} })

	rec := postJSON(t, env.mux, "/api/hub/sets/rt-1/apply", map[string]interface{}{"replace": "old-1"})
	if rec.Code != http.StatusAccepted {
		t.Fatalf("expected 202, got %d (%s)", rec.Code, rec.Body.String())
	}
	got := env.localSet("old-1")
	if got == nil || got.Hub == nil || got.Hub.ID != "rt-1" {
		t.Fatalf("the hub set must replace the local one in place: %+v", got)
	}
	if !reflect.DeepEqual(got.Discovery.URLs, []string{"https://rutracker.org/forum/"}) {
		t.Errorf("a hub update must keep the set's local discovery URLs, got %v", got.Discovery.URLs)
	}
}

func TestMCPDiscoveryApplyStoresTheProbedURLs(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "trackers"
	set.Targets.SNIDomains = []string{"rutracker.org", "nnmclub.to"}
	suite := &discovery.CheckSuite{
		Id:     "urls-run",
		Status: discovery.CheckStatusComplete,
		Domains: []discovery.DomainInput{
			{Domain: "nnmclub.to", CheckURL: "https://nnmclub.to/forum/"},
			{Domain: "rutracker.org", CheckURL: "https://rutracker.org/forum/index.php"},
		},
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"rutracker.org": {Domain: "rutracker.org", Url: "https://rutracker.org/forum/index.php", BestPreset: "combo", BestSuccess: true},
			"nnmclub.to":    {Domain: "nnmclub.to", Url: "https://nnmclub.to/forum/", BestPreset: "combo", BestSuccess: true},
		},
		StrategyGroups: []discovery.StrategyGroup{
			{WinnerPreset: "combo", Family: "combo", Domains: []string{"nnmclub.to", "rutracker.org"}, Set: &set},
		},
	}
	discovery.RegisterSuite(suite)
	mcpRememberSuite(suite.Id)
	t.Cleanup(func() { mcpRememberSuite("") })

	applied := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "rutracker.org"}))
	if !applied.Changed || applied.Applied == nil {
		t.Fatalf("apply must create the set: %+v", applied)
	}
	created := api.getCfg().GetSetById(applied.Applied.Id)
	if created == nil {
		t.Fatal("the created set disappeared")
	}
	want := []string{"https://rutracker.org/forum/index.php", "https://nnmclub.to/forum/"}
	if !reflect.DeepEqual(created.Discovery.URLs, want) {
		t.Errorf("the new set must remember the URLs its strategy was found with, got %v want %v", created.Discovery.URLs, want)
	}
}

func TestMCPDiscoveryApplyFromHistoryStoresTheEntryURL(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)

	set := config.NewSetConfig()
	set.Name = "meduza"
	set.Targets.SNIDomains = []string{"meduza.io"}
	discovery.SaveToHistory(&discovery.CheckSuite{
		Id: "history-urls", Status: discovery.CheckStatusComplete, EndTime: time.Now(),
		DomainDiscoveryResults: map[string]*discovery.DomainDiscoveryResult{
			"meduza.io": {Domain: "meduza.io", Url: "https://meduza.io/en", BestPreset: "combo", BestSuccess: true},
		},
		StrategyGroups: []discovery.StrategyGroup{
			{WinnerPreset: "combo", Domains: []string{"meduza.io"}, Set: &set},
		},
	}, cfg.ConfigPath)
	mcpRememberSuite("")
	t.Cleanup(func() { mcpRememberSuite("") })

	res := callDiscovery(t, session, ctx, map[string]any{"action": "apply", "domain": "meduza.io"})
	if res.IsError {
		t.Fatalf("apply from history: %s", mcpErrorText(res))
	}
	applied := decodeDiscovery(t, res)
	created := api.getCfg().GetSetById(applied.Applied.Id)
	if created == nil || !reflect.DeepEqual(created.Discovery.URLs, []string{"https://meduza.io/en"}) {
		t.Errorf("a set applied from history must remember the entry's URL, got %+v", created)
	}
}

func TestStartDiscoveryChecksTheTrustedServerAndPins(t *testing.T) {
	_, mux := discoveryAPI(t)
	start := func(body string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/discovery/start", strings.NewReader(body)))
		return rec
	}

	for _, server := range []string{"dns.google", "tls://1.1.1.1", "http://dns.example/dns-query", "0.0.0.0"} {
		expectCode(t, start(`{"check_urls":["ntc.party"],"dns_server":"`+server+`"}`), http.StatusBadRequest, "bad_dns_server")
	}
	for _, pins := range []string{
		`{"ntc.party":["not-an-ip"]}`,
		`{"ntc.party":["192.168.1.10"]}`,
		`{"ntc.party":["127.0.0.1"]}`,
		`{"":["130.255.77.28"]}`,
	} {
		expectCode(t, start(`{"check_urls":["ntc.party"],"pins":`+pins+`}`), http.StatusBadRequest, "bad_pin")
	}
}

func TestRunPinsAreNormalized(t *testing.T) {
	pins, err := runPins(map[string][]string{
		"*.NTC.party.": {" 130.255.77.28 ", "130.255.77.28", "::ffff:130.255.77.29"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string][]string{"ntc.party": {"130.255.77.28", "130.255.77.29"}}; !reflect.DeepEqual(pins, want) {
		t.Fatalf("got %v, want %v", pins, want)
	}

	many := map[string][]string{}
	for i := 0; i <= maxRunPins; i++ {
		many[fmt.Sprintf("d%d.example", i)] = []string{"198.51.100.1"}
	}
	if _, err := runPins(many); err == nil {
		t.Fatal("too many pinned addresses must be refused")
	}
	repeated := []string{}
	for i := 0; i <= maxRunPins; i++ {
		repeated = append(repeated, "198.51.100.1", "::ffff:198.51.100.1")
	}
	if pins, err := runPins(map[string][]string{"ntc.party": repeated, "NTC.party.": {"198.51.100.1"}}); err != nil || !reflect.DeepEqual(pins, map[string][]string{"ntc.party": {"198.51.100.1"}}) {
		t.Fatalf("one address repeated is one pin and must not reach the limit, got %v %v", pins, err)
	}
	if pins, err := runPins(nil); err != nil || pins != nil {
		t.Fatalf("no pins is fine, got %v %v", pins, err)
	}
	for _, name := range []string{"#", "a..b", "site.example/path", "a b.example"} {
		if _, err := runPins(map[string][]string{name: {"198.51.100.1"}}); err == nil {
			t.Errorf("%q is not a name a pin can be for", name)
		}
	}
	if _, err := runPins(map[string][]string{"_dmarc.xn--e1afmkfd.xn--p1ai": {"198.51.100.1"}}); err != nil {
		t.Errorf("an xn-- name with an underscore label is a name: %v", err)
	}
	if _, err := runPins(map[string][]string{"пример.рф": {"198.51.100.1"}}); err == nil || !strings.Contains(err.Error(), "xn--") {
		t.Errorf("an internationalized name is refused with a pointer to its xn-- form, got %v", err)
	}
}
