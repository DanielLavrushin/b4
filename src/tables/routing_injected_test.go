package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func injectedTestState() routeState {
	return routeState{
		mode:      config.RoutingModeInterface,
		mark:      0x2bd8,
		table:     136,
		iface:     "eth4",
		chainPre:  "b4r_test_pre",
		chainOut:  "b4r_test_out",
		chainSNAT: "b4r_test_snat",
		setV4:     "b4r_test_v4",
		setV6:     "b4r_test_v6",
	}
}

func injectedTestConfig() *config.Config {
	cfg := &config.Config{}
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = true
	return cfg
}

func TestRouteOutChain_MarksInjectedPacketsBeforeTheQueueBypass(t *testing.T) {
	be := &mockRouteBackend{}
	cfg := injectedTestConfig()
	st := injectedTestState()

	routeAddOutChainRules(be, cfg, st)

	ops := be.chainOps[st.chainOut]
	queueBypass := -1
	lastInjected := -1
	for i, op := range ops {
		switch op {
		case "injected 0x2bd8":
			lastInjected = i
		case "bypass 0x8000":
			if queueBypass < 0 {
				queueBypass = i
			}
		}
	}
	if lastInjected < 0 {
		t.Fatalf("no rule marks the packets the engine injects: %v", ops)
	}
	if queueBypass < 0 {
		t.Fatalf("the queue-mark bypass is gone, so injected packets would be routed into b4's own listener: %v", ops)
	}
	if lastInjected > queueBypass {
		t.Errorf("the injected-packet rule sits below the queue-mark RETURN, so it never runs: %v", ops)
	}
}

func TestRouteOutChain_InjectedRuleCoversBothFamilies(t *testing.T) {
	cfg := injectedTestConfig()
	st := injectedTestState()

	t.Run("both families", func(t *testing.T) {
		be := &mockRouteBackend{}
		routeAddOutChainRules(be, cfg, st)
		if len(be.injected) != 2 {
			t.Fatalf("expected an IPv4 and an IPv6 rule, got %d", len(be.injected))
		}
		want := map[string]bool{st.setV4: false, st.setV6: false}
		for _, r := range be.injected {
			if r.chain != st.chainOut {
				t.Errorf("injected rule went to chain %s, want %s", r.chain, st.chainOut)
			}
			if r.mark != st.mark {
				t.Errorf("injected rule marks 0x%x, want the set's routing mark 0x%x", r.mark, st.mark)
			}
			if r.queueMark != routeQueueBypassMark(cfg) {
				t.Errorf("injected rule matches mark 0x%x, want the queue mark 0x%x", r.queueMark, routeQueueBypassMark(cfg))
			}
			if _, ok := want[r.setName]; !ok {
				t.Errorf("unexpected set %s", r.setName)
				continue
			}
			want[r.setName] = true
		}
		for name, seen := range want {
			if !seen {
				t.Errorf("no injected-packet rule for set %s", name)
			}
		}
	})

	t.Run("ipv4 only", func(t *testing.T) {
		be := &mockRouteBackend{}
		v4 := injectedTestConfig()
		v4.Queue.IPv6Enabled = false
		routeAddOutChainRules(be, v4, st)
		if len(be.injected) != 1 || be.injected[0].v6 {
			t.Fatalf("expected a single IPv4 rule, got %+v", be.injected)
		}
	})
}

func TestRouteOutChain_InjectedRuleFollowsACustomQueueMark(t *testing.T) {
	be := &mockRouteBackend{}
	cfg := injectedTestConfig()
	cfg.Queue.Mark = 0x1234
	st := injectedTestState()

	routeAddOutChainRules(be, cfg, st)
	if len(be.injected) == 0 {
		t.Fatal("no injected-packet rule emitted")
	}
	for _, r := range be.injected {
		if r.queueMark != 0x1234 {
			t.Errorf("injected rule matches mark 0x%x, want the configured queue mark 0x1234", r.queueMark)
		}
	}
}

func TestRouteChainJumps_OutputJumpGoesFirst(t *testing.T) {
	be := &mockRouteBackend{}
	st := injectedTestState()

	routeEnsureChainJumps(be, st, routeDeviceGate{}, false)

	var out *mockRouteJump
	for i := range be.jumps {
		if be.jumps[i].baseChain == "OUTPUT" {
			out = &be.jumps[i]
		}
	}
	if out == nil {
		t.Fatal("the set's OUTPUT chain is never hung off mangle OUTPUT")
	}
	if !out.atTop {
		t.Error("the OUTPUT jump is appended; b4's own queue-mark ACCEPT ends the mangle table above it, so the chain never sees an injected packet")
	}
	for _, j := range be.jumps {
		if j.baseChain == "POSTROUTING" && j.atTop {
			t.Error("the SNAT jump must stay appended, ahead of nothing the firmware put in nat POSTROUTING")
		}
	}
}

func TestRouteResolveIDs_RejectsAnFWMarkThatLooksInjected(t *testing.T) {
	routeMu.Lock()
	routeIfaceAuto = make(map[string]routeState)
	routeMu.Unlock()

	cfg := injectedTestConfig()
	set := config.NewSetConfig()
	set.Name = "collides"
	set.Routing.EgressInterface = "wan9"
	set.Routing.FWMark = 0x8100
	set.Routing.Table = 199

	mark, table := routeResolveIDs(cfg, &set)
	if mark == 0x8100 {
		t.Error("a set mark carrying the queue mark makes every packet of that set read as one b4 injected")
	}
	if table == 0 || mark == 0 {
		t.Fatalf("expected an assigned mark and table, got 0x%x / %d", mark, table)
	}

	routeMu.Lock()
	routeIfaceAuto = make(map[string]routeState)
	routeMu.Unlock()
}

func TestRouteResolveIDs_KeepsAnUnrelatedFWMark(t *testing.T) {
	routeMu.Lock()
	routeIfaceAuto = make(map[string]routeState)
	routeMu.Unlock()

	cfg := injectedTestConfig()
	set := config.NewSetConfig()
	set.Name = "manual"
	set.Routing.EgressInterface = "wan8"
	set.Routing.FWMark = 0x1234
	set.Routing.Table = 198

	mark, table := routeResolveIDs(cfg, &set)
	if mark != 0x1234 || table != 198 {
		t.Errorf("a hand-picked mark and table must be honoured, got 0x%x / %d", mark, table)
	}

	routeMu.Lock()
	routeIfaceAuto = make(map[string]routeState)
	routeMu.Unlock()
}

func TestRouteIptJumpArgs(t *testing.T) {
	top := strings.Join(routeIptJumpArgs("mangle", "OUTPUT", "b4r_test_out", true), " ")
	if top != "-w -t mangle -I OUTPUT 1 -j b4r_test_out" {
		t.Errorf("atTop jump = %q", top)
	}
	end := strings.Join(routeIptJumpArgs("nat", "POSTROUTING", "b4r_test_snat", false), " ")
	if end != "-w -t nat -A POSTROUTING -j b4r_test_snat" {
		t.Errorf("appended jump = %q", end)
	}
}

func TestRouteIptInjectedMarkArgs_KeepsTheQueueMarkOnThePacket(t *testing.T) {
	got := strings.Join(routeIptInjectedMarkArgs("b4r_test_out", "b4r_test_v4", 0x2bd8, 0x8000), " ")
	want := "-w -t mangle -A b4r_test_out -m mark --mark 0x8000/0x8000 -m set --match-set b4r_test_v4 dst -j MARK --set-mark 0x2bd8/0x2bd8"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}
	if !strings.Contains(got, "--set-mark 0x2bd8/0x2bd8") {
		t.Error("the mark must be applied through a mask; setting it whole would clear the queue mark and the packet would be queued again")
	}
}

func TestRouteNftInjectedMarkArgs_KeepsTheQueueMarkOnThePacket(t *testing.T) {
	got := strings.Join(routeNftInjectedMarkArgs("b4r_test_out", false, "b4r_test_v4", 0x2bd8, 0x8000), " ")
	want := "add rule inet b4_route b4r_test_out meta mark & 0x8000 == 0x8000 ip daddr @b4r_test_v4 meta mark set meta mark or 0x2bd8"
	if got != want {
		t.Errorf("got  %q\nwant %q", got, want)
	}

	v6 := strings.Join(routeNftInjectedMarkArgs("b4r_test_out", true, "b4r_test_v6", 0x2bd8, 0x8000), " ")
	if !strings.Contains(v6, "ip6 daddr @b4r_test_v6") {
		t.Errorf("IPv6 rule = %q", v6)
	}
}

func TestRouteNftInjectedMarkRule_IsNotReadAsABypass(t *testing.T) {
	line := strings.Join(routeNftInjectedMarkArgs("b4r_test_out", false, "b4r_test_v4", 0x2bd8, 0x8000)[5:], " ")
	if _, _, ok := nftParseMarkRule(line); ok {
		t.Errorf("the monitor reads %q as a bypass rule, so a chain that lost its RETURN would look healthy", line)
	}
}
