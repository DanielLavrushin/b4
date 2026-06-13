package mtproto

import (
	"strconv"
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
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, -4)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := wsSNIs(plans)
	want := []string{"kws4-1.web.telegram.org", "kws4.web.telegram.org"}
	if len(got) != 2 || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("media DC -4 order: got %v want %v", got, want)
	}
}

func TestPlanTransports_DC203_RemapsToDC2SNIOnOwnIP(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "auto"}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 203)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var ws *transportPlan
	for i := range plans {
		if plans[i].kind == transportWS {
			ws = &plans[i]
			break
		}
	}
	if ws == nil {
		t.Fatal("DC 203 should have a native WS plan dialing its own IP")
	}
	if ws.sni != "kws2.web.telegram.org" {
		t.Fatalf("DC 203 WS SNI must remap to kws2 for cert validity, got %q", ws.sni)
	}
	if ws.dialHost != "91.105.192.100" {
		t.Fatalf("DC 203 WS must dial its own DC IP, got %q", ws.dialHost)
	}
	if !hasTCP(plans) {
		t.Fatalf("DC 203 should still fall back to TCP in auto mode, got %+v", plans)
	}
}

func TestPlanTransports_DefaultConfig_DialsPerDCNotSharedEdge(t *testing.T) {
	cfg := config.DefaultConfig.System.MTProto
	cfg.UpstreamMode = "ws"
	for dc, ip := range map[int]string{1: "149.154.175.50", 2: "149.154.167.51"} {
		plans, err := planTransports(&cfg, config.QueueConfig{IPv4Enabled: true}, dc)
		if err != nil {
			t.Fatalf("DC %d: unexpected error: %v", dc, err)
		}
		var ws *transportPlan
		for i := range plans {
			if plans[i].kind == transportWS {
				ws = &plans[i]
				break
			}
		}
		if ws == nil {
			t.Fatalf("DC %d: default config produced no native WS plan", dc)
		}
		if ws.dialHost != ip {
			t.Fatalf("DC %d: default config must dial the DC's own IP %s, got %q (a non-empty WSEndpointHost default would re-pin every DC to one shared edge and break media)", dc, ip, ws.dialHost)
		}
	}
}

func TestPlanTransports_StandardDCs_DialOwnIP(t *testing.T) {
	want := map[int]string{
		1: "149.154.175.50",
		3: "149.154.175.100",
		5: "149.154.171.5",
	}
	for dc, ip := range want {
		cfg := &config.MTProtoConfig{UpstreamMode: "ws"}
		plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, dc)
		if err != nil {
			t.Fatalf("DC %d: unexpected error: %v", dc, err)
		}
		var ws *transportPlan
		for i := range plans {
			if plans[i].kind == transportWS {
				ws = &plans[i]
				break
			}
		}
		if ws == nil {
			t.Fatalf("DC %d must have a native WS plan (media DCs need WS too)", dc)
		}
		if ws.sni != "kws"+strconv.Itoa(dc)+".web.telegram.org" {
			t.Fatalf("DC %d WS SNI wrong: %q", dc, ws.sni)
		}
		if ws.dialHost != ip {
			t.Fatalf("DC %d WS must dial its own IP %s, got %q", dc, ip, ws.dialHost)
		}
	}
}

func TestPlanTransports_UnknownDC_NoKwsPlans(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "ws"}
	plans, _ := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 99)
	if len(wsSNIs(plans)) != 0 {
		t.Fatalf("unknown DC must not generate kws{N}.web.telegram.org plans (cert-spam risk)")
	}
}

func TestPlanTransports_AutoMode_AlwaysIncludesTCPFallback(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "auto"}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wsSNIs(plans)) != 2 {
		t.Fatalf("auto mode for DC 2 should include both kws plans")
	}
	if !hasTCP(plans) {
		t.Fatalf("auto mode must always include TCP fallback")
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

func TestPlanTransports_DCRelay_AutoMode_WSPlansPlusRelayTCP(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode: "auto",
		DCRelay:      "127.0.0.1:4443",
	}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(wsSNIs(plans)) == 0 {
		t.Fatalf("auto: expected WS plans first, got none")
	}
	if !hasTCP(plans) {
		t.Fatalf("auto: expected TCP plan as fallback")
	}
	for _, p := range plans {
		if p.kind == transportTCP && !strings.HasPrefix(p.addr, "127.0.0.1:") {
			t.Fatalf("TCP fallback should target relay, got %s", p.addr)
		}
	}
}

func TestPlanTransports_DCRelay_AutoMode_RelayBeforeWS(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode: "auto",
		DCRelay:      "127.0.0.1:4443",
	}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plans) == 0 || plans[0].kind != transportTCP {
		t.Fatalf("auto + DCRelay: relay TCP must be the first plan, got %+v", plans)
	}
	if !strings.HasPrefix(plans[0].addr, "127.0.0.1:") {
		t.Fatalf("auto + DCRelay: first plan must target relay, got %s", plans[0].addr)
	}
	if len(wsSNIs(plans)) == 0 {
		t.Fatalf("auto + DCRelay: WS plans must still exist as fallback")
	}
}

func TestPlanTransports_DCRelay_DC203_CollapsesToDC2Port(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode: "tcp",
		DCRelay:      "127.0.0.1:4443",
	}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 203)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(plans) != 1 || plans[0].kind != transportTCP {
		t.Fatalf("expected single TCP plan, got %+v", plans)
	}
	if plans[0].addr != "127.0.0.1:4444" {
		t.Fatalf("DC 203 + relay base 4443 must collapse to port 4444 (DC2 slot), got %s", plans[0].addr)
	}
}

func TestPlanTransports_DC203_DirectTCP_HasDefaultIP(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "tcp"}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 203)
	if err != nil {
		t.Fatalf("DC 203 must have a default TCP address: %v", err)
	}
	if len(plans) == 0 || plans[0].kind != transportTCP {
		t.Fatalf("expected TCP plan for DC 203, got %+v", plans)
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

func TestPlanTransports_WorkerForDC2BeforeCFPool(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode:   "ws",
		CFWorkerDomain: "my-worker-123.user.workers.dev",
		CFProxyEnabled: true,
	}
	plans, err := planTransports(cfg, config.QueueConfig{}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var workerIdx, cfIdx, edgeIdx = -1, -1, -1
	for i, p := range plans {
		switch {
		case p.isWorker && workerIdx == -1:
			workerIdx = i
		case p.cfBase != "" && cfIdx == -1:
			cfIdx = i
		case !p.isWorker && p.cfBase == "" && edgeIdx == -1:
			edgeIdx = i
		}
	}
	if workerIdx == -1 {
		t.Fatal("expected a worker plan for DC2")
	}
	if edgeIdx == -1 || workerIdx < edgeIdx {
		t.Errorf("worker (%d) should come after native edge (%d)", workerIdx, edgeIdx)
	}
	if cfIdx != -1 && workerIdx > cfIdx {
		t.Errorf("worker (%d) should come before shared CF pool (%d)", workerIdx, cfIdx)
	}
	wp := plans[workerIdx]
	if wp.wsPath != "/apiws?dst=149.154.167.51&dc=2" {
		t.Errorf("unexpected worker path %q", wp.wsPath)
	}
	if wp.sni != "my-worker-123.user.workers.dev" || wp.dialHost != wp.sni {
		t.Errorf("worker sni/dialHost wrong: sni=%q dialHost=%q", wp.sni, wp.dialHost)
	}
}

func TestPlanTransports_WorkerForDC1AlongsideNativeEdge(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode:   "ws",
		CFWorkerDomain: "w.user.workers.dev",
	}
	plans, err := planTransports(cfg, config.QueueConfig{}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	foundWorker, foundEdge := false, false
	for _, p := range plans {
		if p.isWorker {
			foundWorker = true
			if p.wsPath != "/apiws?dst=149.154.175.50&dc=1" {
				t.Errorf("unexpected DC1 worker path %q", p.wsPath)
			}
		}
		if p.kind == transportWS && !p.isWorker && p.cfBase == "" && p.sni == "kws1.web.telegram.org" {
			foundEdge = true
		}
	}
	if !foundWorker {
		t.Fatal("expected a worker plan for DC1")
	}
	if !foundEdge {
		t.Fatal("DC1 should now also have a native WS edge plan (per-DC IP routing)")
	}
}

func TestPlanTransports_MultipleWorkerDomains(t *testing.T) {
	cfg := &config.MTProtoConfig{
		UpstreamMode:   "ws",
		CFWorkerDomain: " a.workers.dev , b.workers.dev ",
	}
	plans, err := planTransports(cfg, config.QueueConfig{}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	n := 0
	for _, p := range plans {
		if p.isWorker {
			n++
		}
	}
	if n != 2 {
		t.Errorf("expected 2 worker plans (trimmed), got %d", n)
	}
}

func TestPlanTransports_NoWorkerWhenUnset(t *testing.T) {
	cfg := &config.MTProtoConfig{UpstreamMode: "ws"}
	plans, err := planTransports(cfg, config.QueueConfig{}, 2)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range plans {
		if p.isWorker {
			t.Error("did not expect worker plans when CFWorkerDomain is empty")
		}
	}
}
