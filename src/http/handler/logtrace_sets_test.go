package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
)

func routedTestSet() config.SetConfig {
	set := config.DefaultSetConfig
	set.Id = "aaaa1111"
	set.Name = "Meta"
	set.Enabled = true
	set.Targets.SNIDomains = []string{"a.com", "b.com", "c.com", "d.com", "e.com", "f.com", "g.com", "h.com", "i.com"}
	set.Targets.DomainsToMatch = set.Targets.SNIDomains
	set.Targets.GeoSiteCategories = []string{"geosite:meta"}
	set.Targets.IPs = []string{"1.2.3.4"}
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	set.DNS = config.DNSConfig{Enabled: true, TargetDNS: "1.1.1.1", Pins: map[string][]string{"a.com": {"1.2.3.4"}}}
	set.Routing = config.RoutingConfig{
		Enabled: true,
		Mode:    config.RoutingModeProxy,
		Upstream: config.UpstreamProxyConfig{
			Host:     "192.168.1.50",
			Port:     1080,
			Username: "socksuser",
			Password: "hunter2",
			UDP:      true,
			FailOpen: true,
		},
		FWMark: 0x1000,
		Table:  201,
	}
	set.Escalate = config.EscalateConfig{To: "bbbb2222"}
	set.MSSClamp = config.MSSClampConfig{Enabled: true, Size: 1200}
	return set
}

func TestCollectTraceSetsReportsPinsWithoutRedirect(t *testing.T) {
	cfg := config.NewConfig()
	set := config.DefaultSetConfig
	set.Id = "cccc3333"
	set.Name = "Pinned"
	set.Enabled = true
	set.DNS = config.DNSConfig{Pins: map[string][]string{"ntc.party": {"130.255.77.28"}}}
	cfg.Sets = []*config.SetConfig{&set}

	sets := collectTraceSets(&cfg)
	if len(sets) != 1 {
		t.Fatalf("expected one set, got %d", len(sets))
	}
	got := sets[0].DNS
	if got == nil {
		t.Fatal("pins were configured but the trace has no dns section")
	}
	if got.Enabled || got.Pins != 1 {
		t.Errorf("unexpected dns section: %+v", got)
	}
}

func TestCollectTraceSets(t *testing.T) {
	cfg := config.NewConfig()
	routed := routedTestSet()
	disabled := config.DefaultSetConfig
	disabled.Id = "bbbb2222"
	disabled.Name = "Fallback"
	disabled.Enabled = false
	cfg.Sets = []*config.SetConfig{&routed, &disabled}

	sets := collectTraceSets(&cfg)
	if len(sets) != 1 {
		t.Fatalf("expected only the enabled set, got %d", len(sets))
	}

	got := sets[0]
	if got.ID != "aaaa1111" || got.Name != "Meta" {
		t.Errorf("unexpected set identity: %+v", got)
	}
	if got.Targets.SNIDomains != 9 || got.Targets.ResolvedDomains != 9 {
		t.Errorf("expected 9 domains, got %d listed / %d resolved", got.Targets.SNIDomains, got.Targets.ResolvedDomains)
	}
	if len(got.Targets.DomainSample) != traceDomainSampleLimit {
		t.Errorf("expected sample capped at %d, got %d", traceDomainSampleLimit, len(got.Targets.DomainSample))
	}
	if got.Targets.IPs != 1 || got.Targets.ResolvedIPs != 1 {
		t.Errorf("expected 1 ip target, got %d / %d", got.Targets.IPs, got.Targets.ResolvedIPs)
	}
	if got.DNS == nil || !got.DNS.Enabled || got.DNS.TargetDNS != "1.1.1.1" || got.DNS.Pins != 1 {
		t.Errorf("unexpected dns section: %+v", got.DNS)
	}
	if got.Routing == nil {
		t.Fatal("expected a routing section for a routed set")
	}
	if got.Routing.Upstream != "192.168.1.50:1080" {
		t.Errorf("expected upstream host:port, got %q", got.Routing.Upstream)
	}
	if !got.Routing.UpstreamAuth || !got.Routing.UpstreamUDP || !got.Routing.FailOpen {
		t.Errorf("unexpected upstream flags: %+v", got.Routing)
	}
	if got.Routing.FWMark != "0x1000" || got.Routing.Table != 201 {
		t.Errorf("unexpected mark/table: %q / %d", got.Routing.FWMark, got.Routing.Table)
	}
	if got.Escalate == nil || got.Escalate.To != "bbbb2222" || got.Escalate.ToName != "Fallback" {
		t.Errorf("unexpected escalation: %+v", got.Escalate)
	}
	if got.MSSClamp != 1200 {
		t.Errorf("expected mss clamp 1200, got %d", got.MSSClamp)
	}
}

func TestCollectTraceSetsRedactsCredentials(t *testing.T) {
	cfg := config.NewConfig()
	routed := routedTestSet()
	cfg.Sets = []*config.SetConfig{&routed}

	data, err := json.Marshal(collectTraceSets(&cfg))
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if strings.Contains(string(data), "hunter2") || strings.Contains(string(data), "socksuser") {
		t.Errorf("upstream credentials leaked into the trace: %s", data)
	}
}

func TestCollectTraceSetsOffModesOmitted(t *testing.T) {
	cfg := config.NewConfig()
	set := config.DefaultSetConfig
	set.Id = "cccc3333"
	set.Enabled = true
	cfg.Sets = []*config.SetConfig{&set}

	got := collectTraceSets(&cfg)[0]
	if got.TCP.Desync != "" || got.TCP.Win != "" || got.TCP.Incoming != "" {
		t.Errorf("expected off modes to be dropped, got %+v", got.TCP)
	}
	if got.Faking.SNIMutation != "" {
		t.Errorf("expected off sni mutation to be dropped, got %q", got.Faking.SNIMutation)
	}
	if got.TCP.Duplicate != 0 {
		t.Errorf("expected no duplicate count when disabled, got %d", got.TCP.Duplicate)
	}
	if got.Frag.Strategy != "combo" {
		t.Errorf("expected active strategy to survive, got %q", got.Frag.Strategy)
	}
	if got.DNS != nil || got.Routing != nil || got.Escalate != nil {
		t.Errorf("expected disabled features to be omitted: %+v", got)
	}
}

func TestCollectTraceSetsReportsSetDSCP(t *testing.T) {
	cfg := config.NewConfig()
	zero := config.DefaultSetConfig
	zero.Id = "zero"
	zero.DSCP = config.SetDSCPConfig{Enabled: true, Value: 0}
	stamped := config.DefaultSetConfig
	stamped.Id = "stamped"
	stamped.DSCP = config.SetDSCPConfig{Enabled: true, Value: 31}
	off := config.DefaultSetConfig
	off.Id = "off"
	off.DSCP = config.SetDSCPConfig{Value: 12}
	cfg.Sets = []*config.SetConfig{&zero, &stamped, &off}

	sets := collectTraceSets(&cfg)
	if len(sets) != 3 {
		t.Fatalf("expected three sets, got %d", len(sets))
	}
	if sets[0].DSCP == nil || *sets[0].DSCP != 0 {
		t.Errorf("an enabled DSCP value of 0 clears the field and must be reported, got %v", sets[0].DSCP)
	}
	if sets[1].DSCP == nil || *sets[1].DSCP != 31 {
		t.Errorf("expected DSCP 31, got %v", sets[1].DSCP)
	}
	if sets[2].DSCP != nil {
		t.Errorf("a switched-off DSCP value must be omitted, got %d", *sets[2].DSCP)
	}

	data, err := json.Marshal(sets)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}
	if text := string(data); strings.Count(text, `"dscp":`) != 2 || !strings.Contains(text, `"dscp":0`) {
		t.Errorf("the trace must print the value 0 and skip the switched-off one: %s", text)
	}
}

func TestCollectTraceSetsNilConfig(t *testing.T) {
	if collectTraceSets(nil) != nil {
		t.Error("expected nil for nil config")
	}
}

func TestTraceHeaderCarriesEnabledSets(t *testing.T) {
	cfg := config.NewConfig()
	cfg.ConfigPath = "/etc/b4/b4.json"
	routed := routedTestSet()
	routed.Escalate = config.EscalateConfig{}
	disabled := config.DefaultSetConfig
	disabled.Id = "bbbb2222"
	disabled.Name = "Fallback"
	disabled.Enabled = false
	cfg.Sets = []*config.SetConfig{&routed, &disabled}

	api := &API{
		cfgPtr:                 testCfgPtr(&cfg),
		overrideServiceManager: func() string { return "standalone" },
		geodataManager:         geodat.NewGeodataManager("", ""),
	}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterLogTraceApi()
	t.Cleanup(func() { resetTraceState() })
	resetTraceState()

	doTrace(t, mux, http.MethodPost, "/api/logs/trace/start", "")
	doTrace(t, mux, http.MethodPost, "/api/logs/trace/stop", "")

	req := httptest.NewRequest(http.MethodGet, "/api/logs/trace/download", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, "--- enabled sets (1) ---") {
		t.Fatalf("trace file should carry the enabled sets section:\n%s", body)
	}
	if !strings.Contains(body, `"name": "Meta"`) {
		t.Error("trace file should name the enabled set")
	}
	if strings.Contains(body, "Fallback") {
		t.Error("trace file should skip disabled sets")
	}
	if strings.Contains(body, "hunter2") || strings.Contains(body, "socksuser") {
		t.Error("trace file should not carry upstream credentials")
	}
}
