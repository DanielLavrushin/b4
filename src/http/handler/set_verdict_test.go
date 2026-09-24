package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
)

func TestSetRunStrategyCopiesOnlyTheStrategy(t *testing.T) {
	set := config.NewSetConfig()
	set.Id, set.Name = "yt", "YouTube"
	set.TCP.ConnBytesLimit = 9
	set.TCP.DPortFilter = "443"
	set.TCP.Win = config.WinConfig{Mode: "oscillate", Values: []int{1, 2}}
	set.UDP.Mode = "fake"
	set.UDP.FakePayloadData = []byte{9}
	set.Fragmentation.Strategy = "tls"
	set.Fragmentation.StrategyPool = []string{"tcp", "tls"}
	set.Faking.PayloadData = []byte{1, 2}
	set.Faking.TLSMod = []string{"rnd"}
	set.Faking.SNIMutation.FakeSNIs = []string{"a.example"}
	set.DNS = config.DNSConfig{Enabled: true, TargetDNS: "9.9.9.9"}
	set.Targets.SNIDomains = []string{"youtube.com"}

	got := discovery.SetRunStrategy(&set)
	if got.TCP.ConnBytesLimit != 9 || got.TCP.DPortFilter != "443" || got.UDP.Mode != "fake" || got.Fragmentation.Strategy != "tls" {
		t.Fatalf("the strategy must be the set's own: %+v", got)
	}
	if !got.DNS.Enabled || got.DNS.TargetDNS != "9.9.9.9" {
		t.Fatalf("the set's DNS is carried so the verdict can tell whether a DNS fix is missing: %+v", got.DNS)
	}
	if len(got.Targets.SNIDomains) > 0 || got.Id != "" || got.Name != "" {
		t.Fatalf("targets and identity stay out: targets=%+v id=%q name=%q", got.Targets, got.Id, got.Name)
	}

	set.TCP.Win.Values[0] = 7
	set.UDP.FakePayloadData[0] = 7
	set.Fragmentation.StrategyPool[0] = "changed"
	set.Faking.PayloadData[0] = 7
	set.Faking.TLSMod[0] = "changed"
	set.Faking.SNIMutation.FakeSNIs[0] = "changed"
	if got.TCP.Win.Values[0] != 1 || got.UDP.FakePayloadData[0] != 9 || got.Fragmentation.StrategyPool[0] != "tcp" ||
		got.Faking.PayloadData[0] != 1 || got.Faking.TLSMod[0] != "rnd" || got.Faking.SNIMutation.FakeSNIs[0] != "a.example" {
		t.Fatalf("the run must own its copy, a later edit of the set leaked in: %+v", got)
	}
}

func TestSetRunVersionsFollowTheSetTargets(t *testing.T) {
	cases := []struct {
		setTLS, setIP string
		reqTLS, reqIP string
		wantTLS       string
		wantIP        string
	}{
		{"1.3", "6", "", "", "tls13", "ipv6"},
		{"1.2", "4", "auto", "auto", "tls12", "ipv4"},
		{"1.3", "4", "tls12", "ipv6", "tls12", "ipv6"},
		{"", "", "", "auto", "", "auto"},
	}
	for _, c := range cases {
		set := config.NewSetConfig()
		set.Targets.TLSVersion, set.Targets.IPVersion = c.setTLS, c.setIP
		tlsVersion, ipVersion := discovery.SetRunVersions(&set, c.reqTLS, c.reqIP)
		if tlsVersion != c.wantTLS || ipVersion != c.wantIP {
			t.Errorf("set %q/%q, request %q/%q: got %q/%q, want %q/%q",
				c.setTLS, c.setIP, c.reqTLS, c.reqIP, tlsVersion, ipVersion, c.wantTLS, c.wantIP)
		}
	}
}

func getSetRuns(t *testing.T, mux *http.ServeMux, query string) []discovery.SetRunRecord {
	t.Helper()
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/discovery/set-runs"+query, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("set-runs%s: %d %s", query, rec.Code, rec.Body.String())
	}
	if !strings.HasPrefix(strings.TrimSpace(rec.Body.String()), "[") {
		t.Fatalf("set-runs must always answer a list, got %s", rec.Body.String())
	}
	var runs []discovery.SetRunRecord
	if err := json.Unmarshal(rec.Body.Bytes(), &runs); err != nil {
		t.Fatalf("decode: %v", err)
	}
	return runs
}

func TestDiscoverySetRunsListsTheLastRunOfExistingSets(t *testing.T) {
	yt := namedSet("yt", "YouTube", true, "youtube.com")
	tg := namedSet("tg", "Telegram", true, "telegram.org")
	api, mux := discoveryAPI(t, yt, tg)
	cfgPath := api.getCfg().ConfigPath

	now := time.Now()
	for _, s := range []*discovery.CheckSuite{
		{Id: "old-yt", Status: discovery.CheckStatusComplete, EndTime: now.Add(-3 * time.Hour), SetId: "yt", SetVerdict: &discovery.SetVerdict{Status: discovery.SetVerdictNone}},
		{Id: "tg-run", Status: discovery.CheckStatusComplete, EndTime: now.Add(-2 * time.Hour), SetId: "tg", SetVerdict: &discovery.SetVerdict{Status: discovery.SetVerdictPartial}},
		{Id: "gone-run", Status: discovery.CheckStatusComplete, EndTime: now, SetId: "deleted", SetVerdict: &discovery.SetVerdict{Status: discovery.SetVerdictCovered}},
		{
			Id: "new-yt", Status: discovery.CheckStatusComplete, EndTime: now.Add(-time.Hour), SetId: "yt",
			Domains:    []discovery.DomainInput{{Domain: "www.youtube.com", CheckURL: "https://www.youtube.com/"}},
			SetVerdict: &discovery.SetVerdict{Status: discovery.SetVerdictCovered, WinnerPreset: "combo", Covered: []string{"www.youtube.com"}, Confirmed: true},
		},
	} {
		discovery.SaveToHistory(s, cfgPath)
	}

	runs := getSetRuns(t, mux, "")
	if len(runs) != 2 || runs[0].SetId != "yt" || runs[1].SetId != "tg" {
		t.Fatalf("want the last run of each existing set, newest first, got %+v", runs)
	}
	if runs[0].SuiteId != "new-yt" || runs[0].Verdict.Status != discovery.SetVerdictCovered || !reflect.DeepEqual(runs[0].URLs, []string{"https://www.youtube.com/"}) {
		t.Errorf("the record is the set's last run with its URLs and verdict, got %+v", runs[0])
	}

	if only := getSetRuns(t, mux, "?set_id=tg"); len(only) != 1 || only[0].SuiteId != "tg-run" {
		t.Errorf("set_id narrows the list to that set, got %+v", only)
	}
	if none := getSetRuns(t, mux, "?set_id=deleted"); len(none) != 0 {
		t.Errorf("a deleted set has no record to show, got %+v", none)
	}

	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/discovery/history/clear", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("clear: %d", rec.Code)
	}
	if left := getSetRuns(t, mux, ""); len(left) != 0 {
		t.Errorf("clearing the history clears the set runs, got %+v", left)
	}
}

func TestMCPDiscoverySetResolution(t *testing.T) {
	cfg := config.NewConfig()
	routed := namedSet("r1", "Routed", true, "r.example")
	routed.Routing.Enabled = true
	cfg.Sets = []*config.SetConfig{
		namedSet("id-1", "YouTube", true, "youtube.com"),
		namedSet("id-2", "youtube", true, "m.youtube.com"),
		namedSet("id-3", "Telegram", true, "telegram.org"),
		namedSet("id-4", "Dup", true, "a.example"),
		namedSet("id-5", "Dup", true, "b.example"),
		routed,
	}

	for ref, want := range map[string]string{
		"id-3":     "id-3",
		"ID-3":     "id-3",
		"YouTube":  "id-1",
		"youtube":  "id-2",
		"telegram": "id-3",
	} {
		set, err := mcpDiscoverySet(&cfg, ref)
		if err != nil || set.Id != want {
			t.Errorf("%q resolved to %+v (%v), want %s", ref, set, err, want)
		}
	}
	for ref, fragment := range map[string]string{
		"Dup":     "pass the set id",
		"nothing": "no set with id or name",
		"Routed":  "routing enabled",
	} {
		if _, err := mcpDiscoverySet(&cfg, ref); err == nil || !strings.Contains(err.Error(), fragment) {
			t.Errorf("%q: got %v, want an error mentioning %q", ref, err, fragment)
		}
	}
	if set, err := mcpFindDiscoverySet(&cfg, "Routed"); err != nil || set.Id != "r1" {
		t.Errorf("reading a routed set's status is allowed, got %+v %v", set, err)
	}
}

func TestMCPDiscoveryStartForASetValidates(t *testing.T) {
	cfg := probeCfg(t)
	cfg.Sets[0].Discovery.URLs = []string{"https://www.youtube.com/", "https://m.youtube.com/"}
	private := namedSet("lan", "LAN", true, "router.lan")
	private.Discovery.URLs = []string{"http://192.168.1.1/"}
	routed := namedSet("routed", "Routed", true, "r.example")
	routed.Routing.Enabled = true
	cfg.Sets = append(cfg.Sets, private, routed)
	srv := newMCPTestServer(t, cfg)
	session, ctx := connectMCP(t, srv)

	for args, fragment := range map[string]string{
		`{"action":"start","set":"nope"}`:                                            "no set with id or name",
		`{"action":"start","set":"Routed"}`:                                          "routing enabled",
		`{"action":"start","set":"disabled-set"}`:                                    "stores no discovery URLs",
		`{"action":"start","set":"LAN"}`:                                             "private or local address",
		`{"action":"start","set":"video","domains":"a.io,b.io,c.io,d.io,e.io,f.io"}`: "at most",
		`{"action":"start","set":"video"}`:                                           "runtime",
		`{"action":"start","set":"disabled-set","domains":"example.org"}`:            "runtime",
	} {
		var m map[string]any
		if err := json.Unmarshal([]byte(args), &m); err != nil {
			t.Fatal(err)
		}
		res := callDiscovery(t, session, ctx, m)
		if !res.IsError || !strings.Contains(mcpErrorText(res), fragment) {
			t.Errorf("%s: got %q, want an error mentioning %q", args, mcpErrorText(res), fragment)
		}
	}
}

func coveredVerdict() *discovery.SetVerdict {
	set := config.NewSetConfig()
	set.Name = "combo-pastseq"
	set.Fragmentation.Strategy = "tls"
	set.Faking.TTL = 7
	set.Targets.SNIDomains = []string{"youtube.com", "www.youtube.com"}
	set.DNS.Pins = map[string][]string{"www.youtube.com": {"203.0.113.9"}, "elsewhere.example": {"203.0.113.10"}}
	return &discovery.SetVerdict{
		Status:       discovery.SetVerdictCovered,
		WinnerPreset: "combo-pastseq",
		Family:       discovery.FamilyCombo,
		Set:          &set,
		Covered:      []string{"youtube.com", "www.youtube.com"},
		NoBypass:     []string{"youtube.com"},
		Confirmed:    true,
	}
}

func TestMCPDiscoveryApplyWritesACoveredVerdictIntoTheSet(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	cfg.Sets[0].DNS.Pins = map[string][]string{"keep.example": {"198.51.100.1"}}
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)
	mcpResetHistory()

	suite := &discovery.CheckSuite{
		Id: "set-run-covered", Status: discovery.CheckStatusComplete, SetId: "set-1",
		StoppedCovered: true, SetVerdict: coveredVerdict(),
	}
	discovery.RegisterSuite(suite)
	mcpRememberSuite(suite.Id)
	t.Cleanup(func() { mcpRememberSuite("") })

	status := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status"}))
	if status.SetVerdict == nil || status.SetVerdict.Status != "covered" || status.SetVerdict.SetName != "video" || !status.SetVerdict.StoppedCovered {
		t.Fatalf("status must carry the set verdict, got %+v", status.SetVerdict)
	}

	before := len(api.getCfg().Sets)
	out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "apply", "set": "video"}))
	if !out.Changed || out.Applied == nil || out.Applied.Id != "set-1" {
		t.Fatalf("a covered verdict is written into the set: %+v", out)
	}
	if got := len(api.getCfg().Sets); got != before {
		t.Fatalf("writing into a set must not create one, %d sets from %d", got, before)
	}
	live := api.getCfg().GetSetById("set-1")
	if live.Fragmentation.Strategy != "tls" || live.Faking.TTL != 7 {
		t.Errorf("the strategy was not adopted: %+v", live.Fragmentation)
	}
	if !reflect.DeepEqual(live.Targets.SNIDomains, []string{"youtube.com"}) {
		t.Errorf("the set's domains are untouched, got %v", live.Targets.SNIDomains)
	}
	if !reflect.DeepEqual(live.DNS.Pins["www.youtube.com"], []string{"203.0.113.9"}) || live.DNS.Pins["elsewhere.example"] != nil {
		t.Errorf("only pins of covered addresses are written, got %v", live.DNS.Pins)
	}
	if !reflect.DeepEqual(live.DNS.Pins["keep.example"], []string{"198.51.100.1"}) {
		t.Errorf("unrelated pins of the set stay, got %v", live.DNS.Pins)
	}

	decodeRevert(t, session, ctx)
	if got := api.getCfg().GetSetById("set-1"); got.Fragmentation.Strategy == "tls" {
		t.Error("the write must be revertable like any other MCP write")
	}
}

func TestMCPDiscoveryApplyForASetFollowsTheVerdict(t *testing.T) {
	cfg := probeCfg(t)
	cfg.System.WebServer.MCP.AllowWrites = true
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	srv, api := newMCPTestServerAPI(t, cfg)
	session, ctx := connectMCP(t, srv)
	mcpRememberSuite("")
	t.Cleanup(func() { mcpRememberSuite("") })

	current := coveredVerdict()
	current.Status = discovery.SetVerdictCurrentWorks
	current.WinnerPreset = "set-current"
	discovery.RegisterSuite(&discovery.CheckSuite{Id: "set-run-current", Status: discovery.CheckStatusComplete, SetId: "set-1", SetVerdict: current})

	out := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "apply", "id": "set-run-current"}))
	if out.Changed || !strings.Contains(out.Note, "nothing written") {
		t.Fatalf("the set's own strategy already works, nothing to write: %+v", out)
	}
	if got := api.getCfg().GetSetById("set-1"); got.Fragmentation.Strategy == "tls" {
		t.Fatal("current_works must not touch the set")
	}

	discovery.SaveToHistory(&discovery.CheckSuite{
		Id: "set-run-partial", Status: discovery.CheckStatusComplete, EndTime: time.Now(), SetId: "set-1",
		Domains: []discovery.DomainInput{{Domain: "youtube.com", CheckURL: "https://youtube.com/"}, {Domain: "googlevideo.com", CheckURL: "https://googlevideo.com/"}},
		SetVerdict: &discovery.SetVerdict{
			Status: discovery.SetVerdictPartial, WinnerPreset: "tcp-frag",
			Covered: []string{"youtube.com"}, Uncovered: []string{"googlevideo.com"},
		},
	}, cfg.ConfigPath)

	res := callDiscovery(t, session, ctx, map[string]any{"action": "apply", "set": "set-1"})
	if !res.IsError {
		t.Fatal("a partial verdict must not be written into the set")
	}
	msg := mcpErrorText(res)
	if !strings.Contains(msg, "Covered: youtube.com") || !strings.Contains(msg, "Uncovered: googlevideo.com") {
		t.Errorf("the refusal must list covered and uncovered addresses: %q", msg)
	}

	status := decodeDiscovery(t, callDiscovery(t, session, ctx, map[string]any{"action": "status", "set": "video"}))
	if status.SetVerdict == nil || status.SetVerdict.Status != "partial" || status.Source != "history" {
		t.Fatalf("status for the set must read its saved record, got %+v", status)
	}

	res = callDiscovery(t, session, ctx, map[string]any{"action": "apply", "id": "set-run-partial"})
	if !res.IsError || !strings.Contains(mcpErrorText(res), "Uncovered") {
		t.Errorf("a suite id of a saved set run follows the set path too: %q", mcpErrorText(res))
	}

	res = callDiscovery(t, session, ctx, map[string]any{"action": "apply", "set": "disabled-set"})
	if !res.IsError || !strings.Contains(mcpErrorText(res), "no discovery run for set") {
		t.Errorf("a set never probed has nothing to apply: %q", mcpErrorText(res))
	}
	if got := api.getCfg().GetSetById("set-1"); got.Fragmentation.Strategy == "tls" {
		t.Fatal("refused applies must leave the set alone")
	}
}
