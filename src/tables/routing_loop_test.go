package tables

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/netif"
)

func loopTestState(iface string, routerOut bool) routeState {
	return routeState{
		mode:      config.RoutingModeInterface,
		mark:      0x4d05,
		table:     169,
		iface:     iface,
		chainOut:  "b4r_x_out",
		setV4:     "b4r_x_v4",
		setV6:     "b4r_x_v6",
		routerOut: routerOut,
	}
}

func loopTestSysfs(t *testing.T) {
	t.Helper()
	root := t.TempDir() + string(os.PathSeparator)
	prev := netif.Root
	netif.Root = root
	netif.Forget()
	t.Cleanup(func() {
		netif.Root = prev
		netif.Forget()
	})

	for name, files := range map[string]map[string]string{
		"xray0": {"tun_flags": "0x1001\n", "flags": "0x1003\n"},
		"eth1":  {"flags": "0x1003\n"},
	} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f, body := range files {
			if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestRouterTrafficGuardOnlyGuardsWhatItMarks(t *testing.T) {
	loopTestSysfs(t)
	cfg := config.NewConfig()

	open := &mockRouteBackend{}
	routeAddOutChainRules(open, &cfg, loopTestState("xray0", true), routeDeviceGate{})
	ops := strings.Join(open.chainOps["b4r_x_out"], "|")
	if !strings.Contains(ops, "router-traffic-guard") {
		t.Errorf("a set that marks the router's own traffic must cap the rate, or a loop through it is unbounded: %s", ops)
	}
	guard := indexOfOp(open.chainOps["b4r_x_out"], "router-traffic-guard")
	firstMark := indexOfPrefix(open.chainOps["b4r_x_out"], "mark ")
	if guard < 0 || firstMark < 0 || guard > firstMark {
		t.Errorf("the guard has to sit ahead of the marking it caps, got %v", open.chainOps["b4r_x_out"])
	}

	quiet := &mockRouteBackend{}
	routeAddOutChainRules(quiet, &cfg, loopTestState("xray0", false), routeDeviceGate{})
	for _, op := range quiet.chainOps["b4r_x_out"] {
		if op == "router-traffic-guard" {
			t.Error("with no marking of router traffic there is nothing to rate-limit and no rule to spend")
		}
		if strings.HasPrefix(op, "mark ") {
			t.Errorf("router traffic is excluded for this set, so its out chain must not mark by destination alone: %q", op)
		}
	}
	if len(quiet.injected) == 0 {
		t.Error("excluding router traffic must still leave the rule that re-marks the packets b4 injects itself")
	}
}

func TestRouterTrafficGuardIsScopedToTheSetAndToTunnels(t *testing.T) {
	loopTestSysfs(t)
	cfg := config.NewConfig()

	plain := &mockRouteBackend{}
	routeAddOutChainRules(plain, &cfg, loopTestState("eth1", true), routeDeviceGate{})
	if indexOfOp(plain.chainOps["b4r_x_out"], "router-traffic-guard") < 0 {
		t.Error("the guard has to go in even when the egress is missing or plain, or a tunnel that appears after b4 started marks router traffic with nothing capping it")
	}
	if indexOfPrefix(plain.chainOps["b4r_x_out"], "mark ") < 0 {
		t.Error("a plain egress still routes the router's own traffic")
	}

	tunnel := &mockRouteBackend{}
	routeAddOutChainRules(tunnel, &cfg, loopTestState("xray0", true), routeDeviceGate{})
	if len(tunnel.guards) == 0 {
		t.Fatal("no guard emitted for a userspace tunnel")
	}
	for _, g := range tunnel.guards {
		want := "b4r_x_v4"
		if g.v6 {
			want = "b4r_x_v6"
		}
		if g.setName != want {
			t.Errorf("guard matches %q, want %q: without the set's own destinations in the match, unrelated router traffic spends the budget and the set's real connections go out unmarked", g.setName, want)
		}
	}
}

func indexOfOp(ops []string, want string) int {
	for i, op := range ops {
		if op == want {
			return i
		}
	}
	return -1
}

func indexOfPrefix(ops []string, prefix string) int {
	for i, op := range ops {
		if strings.HasPrefix(op, prefix) {
			return i
		}
	}
	return -1
}

func TestRouteLineBelongsToIface(t *testing.T) {
	for _, c := range []struct {
		line, iface string
		ours        bool
	}{
		{"default dev xray0 scope link", "xray0", true},
		{"default via 10.8.0.1 dev xray0 src 10.8.0.2", "xray0", true},
		{"blackhole default metric 4096", "xray0", true},
		{"default via 192.168.2.1 dev tun13", "tun0", false},
		{"192.168.1.0/24 dev br0 scope link", "tun0", false},
		{"unreachable default metric 1", "tun0", false},
		{"default", "tun0", false},
	} {
		if got := routeLineBelongsToIface(c.line, c.iface); got != c.ours {
			t.Errorf("routeLineBelongsToIface(%q, %q) = %v, want %v", c.line, c.iface, got, c.ours)
		}
	}
}

func TestRouteResolveIDsSkipsATableSomebodyElseOwns(t *testing.T) {
	prev := routeTableForeignRoutes
	t.Cleanup(func() {
		routeTableForeignRoutes = prev
		routeIfaceAuto = make(map[string]routeState)
		routeRuleCache = make(map[string]routeState)
	})

	routeIfaceAuto = make(map[string]routeState)
	routeRuleCache = make(map[string]routeState)

	cfg := config.NewConfig()
	set := config.NewSetConfig()
	set.Routing.EgressInterface = "tun0"

	routeTableForeignRoutes = func(table int, iface string) bool { return false }
	_, natural := routeResolveIDs(&cfg, &set)

	routeIfaceAuto = make(map[string]routeState)
	routeTableForeignRoutes = func(table int, iface string) bool { return table == natural }
	_, moved := routeResolveIDs(&cfg, &set)

	if moved == natural {
		t.Fatalf("table %d already holds someone else's routes; b4 flushes the table it picks, so it must pick another", natural)
	}
	if moved < 100 || moved > 249 {
		t.Errorf("replacement table %d is outside the range b4 assigns from", moved)
	}
}

func TestKillSwitchHoldsTheTableShut(t *testing.T) {
	var cmds []string
	prev := runLogged
	runLogged = func(op string, args ...string) { cmds = append(cmds, strings.Join(args, " ")) }
	t.Cleanup(func() { runLogged = prev })

	st := loopTestState("xray0", true)
	st.killSwitch = true
	routeApplyKillSwitch(st, true, true, true)

	joined := strings.Join(cmds, "\n")
	for _, want := range []string{
		"ip route replace blackhole default metric 4096 table 169",
		"ip -6 route replace blackhole default metric 4096 table 169",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q, so a set whose tunnel drops leaks out the normal uplink instead of stopping; got:\n%s", want, joined)
		}
	}

	cmds = nil
	st.killSwitch = false
	routeApplyKillSwitch(st, false, true, false)
	joined = strings.Join(cmds, "\n")
	if !strings.Contains(joined, "ip route del blackhole default metric 4096 table 169") {
		t.Errorf("turning the kill switch off has to take the blackhole back out, got:\n%s", joined)
	}
	if strings.Contains(joined, "ip -6 ") {
		t.Errorf("IPv6 is off here, so nothing should touch the v6 table: %s", joined)
	}
}

func TestKillSwitchMetricLosesToTheRealDefault(t *testing.T) {
	if routeKillSwitchMetric == "0" || routeKillSwitchMetric == "" {
		t.Fatal("the blackhole must carry a metric worse than the interface route, or it wins while the tunnel is up")
	}
}
