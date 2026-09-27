package tables

import (
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

const netnsAllowListedMAC = "02:00:00:00:00:01"

func netnsAllowListConfig(engine string, mode string) *config.Config {
	cfg := netnsProxyMixConfig()
	cfg.System.Tables.Engine = engine
	switch mode {
	case "allow", "deny":
		cfg.Queue.Devices.Enabled = true
		cfg.Queue.Devices.WhiteIsBlack = mode == "deny"
		cfg.Queue.Devices.Devices = []config.Device{{MAC: netnsAllowListedMAC, Selected: true}}
	}
	return cfg
}

func netnsRequireEngine(t *testing.T, engine string) {
	t.Helper()
	switch engine {
	case backendNFTables:
		if !hasBinary("nft") {
			t.Skip("nft is not installed")
		}
	default:
		if out, err := run(engine, "-w", "-t", "mangle", "-L", "PREROUTING", "-n"); err != nil {
			t.Skipf("%s cannot read mangle here: %s", engine, strings.TrimSpace(out))
		}
	}
}

func netnsAddProxyTarget(t *testing.T, engine string, st routeState) {
	t.Helper()
	if engine == backendNFTables {
		netnsRun(t, "nft", "add", "element", "inet", routeNftTable, st.setV4, "{", netnsProxyTarget, "}")
		return
	}
	netnsRun(t, "ipset", "add", st.setV4, netnsProxyTarget, "-exist")
}

func netnsPreroutingRules(t *testing.T, engine string) []string {
	t.Helper()
	if engine == backendNFTables {
		return strings.Split(netnsRun(t, "nft", "-a", "list", "chain", "inet", routeNftTable, routeNftPrerouting), "\n")
	}
	return strings.Split(netnsRun(t, engine, "-w", "-t", "mangle", "-S", "PREROUTING"), "\n")
}

func netnsLoopJumps(t *testing.T, engine, chain string) (loop, other []string) {
	t.Helper()
	for _, line := range netnsPreroutingRules(t, engine) {
		if !strings.Contains(line, chain) {
			continue
		}
		if strings.Contains(line, `iifname "lo"`) || strings.Contains(line, "-i lo ") {
			loop = append(loop, line)
			continue
		}
		other = append(other, line)
	}
	return loop, other
}

func netnsRouterDialReachesListener(t *testing.T, port int, timeout time.Duration) bool {
	t.Helper()
	ln := netnsTransparentListener(t, port)
	defer ln.Close()
	accepted := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn.LocalAddr().String()
		_ = conn.Close()
	}()
	conn, err := net.DialTimeout("tcp", netnsProxyTarget+":443", timeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	select {
	case local := <-accepted:
		return strings.HasPrefix(local, netnsProxyTarget+":")
	case <-time.After(timeout):
		return false
	}
}

func TestNetnsProxySetTakesTheRoutersOwnDialBehindAnAllowList(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	for _, tc := range []struct {
		engine  string
		capture bool
	}{
		{backendNFTables, true},
		{backendIPTablesLegacy, true},
		{backendIPTables, false},
	} {
		t.Run(tc.engine, func(t *testing.T) {
			netnsRequireEngine(t, tc.engine)
			routeEngine = nil
			defer func() { routeEngine = nil }()

			cfg := netnsAllowListConfig(tc.engine, "allow")
			if tc.capture {
				if err := AddRules(cfg); err != nil {
					t.Fatalf("AddRules: %v", err)
				}
				defer func() { _ = ClearRules(cfg) }()
			}
			RoutingSyncConfig(cfg)
			defer RoutingClearAll()

			st, ok := routeRuleCache["netns-proxy-set"]
			if !ok {
				t.Fatal("the proxy set built no rules")
			}
			netnsAddProxyTarget(t, tc.engine, st)
			port, _ := portFromState(st)

			if !netnsRouterDialReachesListener(t, port, 3*time.Second) {
				t.Fatalf("with an allow list the router's own dial to a proxied address never reached the listener; this is the dial b4's built-in SOCKS5 server makes:\n%s",
					strings.Join(netnsPreroutingRules(t, tc.engine), "\n"))
			}

			loop, _ := netnsLoopJumps(t, tc.engine, st.chainPre)
			if len(loop) != 1 {
				t.Fatalf("want one loopback jump into %s, got %v", st.chainPre, loop)
			}
			if tc.engine == backendNFTables {
				netnsRun(t, "nft", "delete", "rule", "inet", routeNftTable, routeNftPrerouting, "handle", nftHandleFromLine(loop[0]))
			} else {
				netnsRun(t, tc.engine, "-w", "-t", "mangle", "-D", "PREROUTING", "-i", "lo", "-m", "mark", "--mark", routeSetMarkRule(st.mark), "-j", st.chainPre)
			}
			if netnsRouterDialReachesListener(t, port, time.Second) {
				t.Error("the dial got through without the loopback jump, so the test proves nothing about it")
			}
		})
	}
}

func TestNetnsLoopbackJumpFollowsTheDeviceFilter(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequireEngine(t, engine)
			routeEngine = nil
			defer func() { routeEngine = nil }()
			defer RoutingClearAll()

			scoped := netnsAllowListConfig(engine, "allow")
			scoped.Sets[1].Targets.SourceDevices = []string{netnsAllowListedMAC}
			removed := netnsAllowListConfig(engine, "allow")
			removed.Sets = removed.Sets[:1]

			for _, step := range []struct {
				name        string
				cfg         *config.Config
				loop, other int
			}{
				{"allow list", netnsAllowListConfig(engine, "allow"), 1, 1},
				{"same allow list again", netnsAllowListConfig(engine, "allow"), 1, 1},
				{"filter off", netnsAllowListConfig(engine, "off"), 0, 1},
				{"deny list", netnsAllowListConfig(engine, "deny"), 0, 1},
				{"allow list back", netnsAllowListConfig(engine, "allow"), 1, 1},
				{"set limited to the device", scoped, 0, 1},
				{"set removed", removed, 0, 0},
			} {
				RoutingSyncConfig(step.cfg)
				chain, _, _ := routeBuildChainNames("netns-proxy-set")
				loop, other := netnsLoopJumps(t, engine, chain)
				if len(loop) != step.loop || len(other) != step.other {
					t.Errorf("%s: %d loopback and %d other jumps into %s, want %d and %d:\n%s",
						step.name, len(loop), len(other), chain, step.loop, step.other,
						strings.Join(netnsPreroutingRules(t, engine), "\n"))
				}
			}
		})
	}
}

func TestNetnsAnAllowListThatLeavesASetNoDeviceTakesItsJumpAway(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	for _, engine := range []string{backendNFTables, backendIPTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequireEngine(t, engine)
			routeEngine = nil
			defer func() { routeEngine = nil }()
			defer RoutingClearAll()

			RoutingSyncConfig(netnsAllowListConfig(engine, "off"))
			chain, _, _ := routeBuildChainNames("netns-egress-set")
			if _, other := netnsLoopJumps(t, engine, chain); len(other) != 1 {
				t.Fatalf("with the filter off the interface set wants one jump, got %v", other)
			}

			cfg := netnsAllowListConfig(engine, "allow")
			cfg.Sets[0].Targets.SourceDevices = []string{netnsAllowListedMAC}
			cfg.Sets[0].Targets.SourceDevicesExclude = true
			RoutingSyncConfig(cfg)

			if loop, other := netnsLoopJumps(t, engine, chain); len(loop)+len(other) != 0 {
				t.Errorf("the set excludes the only allowed device, yet %s still has jumps %v %v, so every device keeps its route:\n%s",
					chain, loop, other, strings.Join(netnsPreroutingRules(t, engine), "\n"))
			}
			if !RoutingRulesPresent(cfg) {
				t.Error("right after a clean sync the presence check reports rules missing, so the firewall monitor rebuilds routing on every tick")
			}
		})
	}
}
