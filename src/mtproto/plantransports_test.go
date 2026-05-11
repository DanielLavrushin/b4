package mtproto

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func wsSNIs(plans []transportPlan) []string {
	var out []string
	for _, p := range plans {
		if p.kind == transportWS {
			out = append(out, p.sni)
		}
	}
	return out
}

func hasTCP(plans []transportPlan) bool {
	for _, p := range plans {
		if p.kind == transportTCP {
			return true
		}
	}
	return false
}

func TestPlanTransports_WSOnly_DC2(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "ws"}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := wsSNIs(plans)
	want := []string{"kws2.web.telegram.org", "kws2-1.web.telegram.org"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("non-media DC 2 order: got %v want %v", got, want)
	}
	if hasTCP(plans) {
		t.Fatalf("ws-only mode should not include TCP for normal DC")
	}
}

func TestPlanTransports_MediaDC_ReversesOrdering(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "ws"}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, -3)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := wsSNIs(plans)
	want := []string{"kws3-1.web.telegram.org", "kws3.web.telegram.org"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("media DC -3 order: got %v want %v", got, want)
	}
}

func TestPlanTransports_DC203_RemapsToKws2(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "ws"}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 203)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := wsSNIs(plans)
	want := []string{"kws2.web.telegram.org", "kws2-1.web.telegram.org"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("DC 203 should remap to kws2: got %v want %v", got, want)
	}
}

func TestPlanTransports_UnknownDC_NoKwsPlans(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "ws"}
	plans, _ := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 99)
	if len(wsSNIs(plans)) != 0 {
		t.Fatalf("unknown DC must not generate kws{N}.web.telegram.org plans (cert-spam risk)")
	}
}

func TestPlanTransports_AutoMode_IncludesTCPWhenFallbackEnabled(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "auto", WSFallbackTCP: true}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wsSNIs(plans)) != 2 {
		t.Fatalf("auto mode for DC 2 should include both kws plans")
	}
	if !hasTCP(plans) {
		t.Fatalf("auto + fallback should include TCP")
	}
}

func TestPlanTransports_AutoMode_NoTCPWhenFallbackDisabled(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "auto", WSFallbackTCP: false}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasTCP(plans) {
		t.Fatalf("auto without fallback should not include TCP for DC 2")
	}
}

func TestPlanTransports_TCPOnly(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "tcp"}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wsSNIs(plans)) != 0 {
		t.Fatalf("tcp mode should produce no ws plans")
	}
	if !hasTCP(plans) {
		t.Fatalf("tcp mode should include TCP plan")
	}
}

func TestPlanTransports_CustomDomain_PrependsKwsPrefix(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode:   "ws",
		WSCustomDomain: "example.com",
	}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snis := wsSNIs(plans)
	found := false
	for _, s := range snis {
		if s == "kws4.example.com" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected kws4.example.com in plans, got %v", snis)
	}
}

func TestPlanTransports_CustomDomain_HighDCStillWorks(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode:   "ws",
		WSCustomDomain: "example.com",
	}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 99)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	snis := wsSNIs(plans)
	if len(snis) != 1 || snis[0] != "kws99.example.com" {
		t.Fatalf("custom domain should work for unknown DCs: got %v", snis)
	}
	if hasTCP(plans) {
		t.Fatalf("custom domain present means TCP fallback should not be forced")
	}
}

func TestPlanTransports_DCRelay_TCPMode_TargetsRelay(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode: "tcp",
		DCRelay:      "127.0.0.1:4443",
	}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plans) != 1 || plans[0].kind != transportTCP {
		t.Fatalf("tcp mode + DCRelay should yield single TCP plan, got %+v", plans)
	}
	if !strings.HasPrefix(plans[0].addr, "127.0.0.1:") {
		t.Fatalf("TCP plan should target relay address, got %s", plans[0].addr)
	}
}

func TestPlanTransports_DCRelay_AutoFallbackOn_WSPlansPlusRelayTCP(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode:  "auto",
		WSFallbackTCP: true,
		DCRelay:       "127.0.0.1:4443",
	}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wsSNIs(plans)) == 0 {
		t.Fatalf("auto + fallback: expected WS plans first, got none")
	}
	if !hasTCP(plans) {
		t.Fatalf("auto + fallback: expected TCP plan as fallback")
	}
	for _, p := range plans {
		if p.kind == transportTCP && !strings.HasPrefix(p.addr, "127.0.0.1:") {
			t.Fatalf("TCP fallback should target relay, got %s", p.addr)
		}
	}
}

func TestPlanTransports_DCRelay_AutoFallbackOff_IgnoredForKnownDC(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode:  "auto",
		WSFallbackTCP: false,
		DCRelay:       "127.0.0.1:4443",
	}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasTCP(plans) {
		t.Fatalf("auto + fallback off: DCRelay must NOT introduce a TCP plan; got %+v", plans)
	}
	if len(wsSNIs(plans)) == 0 {
		t.Fatalf("expected WS plans, got none")
	}
}

func TestPlanTransports_DCRelay_IgnoredInWSMode(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode: "ws",
		DCRelay:      "127.0.0.1:4443",
	}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hasTCP(plans) {
		t.Fatalf("DCRelay in ws mode must NOT yield TCP plans (user explicitly chose ws only); got %+v", plans)
	}
	if len(wsSNIs(plans)) == 0 {
		t.Fatalf("expected WS plans for DC 2 in ws mode, got none")
	}
}
