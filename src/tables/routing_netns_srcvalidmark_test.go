package tables

import (
	"os"
	"strings"
	"testing"
	"time"
)

func netnsTurnOnSrcValidMark(t *testing.T) {
	t.Helper()
	prev, err := os.ReadFile(srcValidMarkSysctl)
	if err != nil {
		t.Skipf("%s is not readable in this namespace: %v", srcValidMarkSysctl, err)
	}
	if err := os.WriteFile(srcValidMarkSysctl, []byte("1"), 0644); err != nil {
		t.Skipf("cannot turn on src_valid_mark in this namespace: %v", err)
	}
	t.Cleanup(func() { _ = os.WriteFile(srcValidMarkSysctl, prev, 0644) })
}

func netnsProxySetAnswersALanClientUnderSrcValidMark(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	if engine == backendNFTables && !hasBinary("nft") {
		t.Skip("nft is not installed")
	}
	netnsTurnOnSrcValidMark(t)
	dev := netnsStartDevice(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsLocalGuardConfig(engine, "", []string{netnsDevInet})
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()
	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	st, ok := routeRuleCache["netns-localdst-proxy"]
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	rules := netnsRun(t, "ip", "rule", "show")
	want := routeSourceCheckMarkRule(st.mark) + " iif lo lookup main"
	if !strings.Contains(rules, want) {
		t.Errorf("no source-check rule for the set's mark (%q) in:\n%s", want, rules)
	}

	port, _ := portFromState(st)
	s := netnsRouterSockets(t, port, SelfDialMark)
	got := dev.probe(t, "tcp", netnsDevInet, 443, "inet")
	t.Logf("device %s -> internet with src_valid_mark=1: %s", netnsDevIP, got)
	if got != "reply=tproxy:inet" {
		netnsLogState(t, engine)
		t.Fatalf("device -> internet through a proxy set with net.ipv4.conf.all.src_valid_mark=1: %s; the kernel checked the device's address in the set's local-delivery table and dropped the SYN as a martian source", got)
	}
	if !s.got("tcp", "tproxy", "inet") {
		t.Errorf("the transparent listener never saw the device's connection: tcp=%v", s.tcp)
	}

	const publicRouter, publicDev = "203.0.113.1", "203.0.113.100"
	netnsRun(t, "ip", "addr", "add", publicRouter+"/24", "dev", netnsDevLink)
	dev.in(t, "ip", "addr", "flush", "dev", netnsDevPeer)
	dev.in(t, "ip", "addr", "add", publicDev+"/24", "dev", netnsDevPeer)
	dev.in(t, "ip", "route", "replace", "default", "via", publicRouter)
	got = dev.probe(t, "tcp", netnsDevInet, 443, "inet")
	t.Logf("device %s -> internet with src_valid_mark=1: %s", publicDev, got)
	if got != "reply=tproxy:inet" {
		netnsLogState(t, engine)
		t.Errorf("a device on a public LAN subnet -> internet through a proxy set with net.ipv4.conf.all.src_valid_mark=1: %s; its source check never reached the main table and the SYN was dropped as a martian source", got)
	}
}

func TestNetnsProxySetAnswersALanClientUnderSrcValidMark(t *testing.T) {
	netnsProxySetAnswersALanClientUnderSrcValidMark(t, backendIPTables)
}

func TestNetnsProxySetAnswersALanClientUnderSrcValidMarkNft(t *testing.T) {
	netnsProxySetAnswersALanClientUnderSrcValidMark(t, backendNFTables)
}

const netnsPrivateProxyTarget = "10.8.0.2"

func netnsProxySetKeepsTheRoutersOwnDialOverASpecificRoute(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsProxyMixConfig()
	cfg.System.Tables.Engine = engine
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()
	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	st, ok := routeRuleCache["netns-proxy-set"]
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	netnsAddProxyTarget(t, engine, st)
	netnsAddProxyTargetIP(t, engine, st, netnsPrivateProxyTarget)
	port, _ := portFromState(st)

	rules := netnsRun(t, "ip", "rule", "show")
	if want := "fwmark " + routeSourceCheckMarkRule(st.mark) + " iif lo lookup main"; !strings.Contains(rules, want) {
		t.Errorf("no source-check rule %q in:\n%s", want, rules)
	}

	for _, tc := range []struct{ route, target string }{
		{"198.0.0.0/8", netnsProxyTarget},
		{"198.51.100.0/24", netnsProxyTarget},
		{netnsProxyTarget + "/32", netnsProxyTarget},
		{"10.8.0.0/24", netnsPrivateProxyTarget},
	} {
		netnsRun(t, "ip", "route", "add", tc.route, "via", netnsSecondGW, "dev", netnsSecondary)
		t.Cleanup(func() { _, _ = run("ip", "route", "del", tc.route, "via", netnsSecondGW, "dev", netnsSecondary) })
		reached := netnsRouterDialToReachesListener(t, port, tc.target, 3*time.Second)
		_, _ = run("ip", "route", "del", tc.route, "via", netnsSecondGW, "dev", netnsSecondary)
		if !reached {
			netnsLogState(t, engine)
			t.Errorf("with %s via %s in the main table, the router's own dial to %s never reached the set's listener; the source-check rule sent it out through main instead of the local-delivery table", tc.route, netnsSecondary, tc.target)
		}
	}

	netnsRun(t, "ip", "route", "add", "198.51.100.0/24", "via", netnsSecondGW, "dev", netnsSecondary)
	t.Cleanup(func() {
		_, _ = run("ip", "route", "del", "198.51.100.0/24", "via", netnsSecondGW, "dev", netnsSecondary)
	})
	perSet := routeSetMarkRule(st.mark)
	netnsRun(t, "ip", "rule", "add", "fwmark", perSet, "iif", "lo", "lookup", "main", "priority", "1")
	t.Cleanup(func() {
		_, _ = run("ip", "rule", "del", "fwmark", perSet, "iif", "lo", "lookup", "main", "priority", "1")
	})
	blind := netnsRouterDialReachesListener(t, port, time.Second)
	if blind {
		t.Error("the dial reached the listener even with a source-check rule that ignores the router's own mark bit, so the test proves nothing about that bit")
	}
}

func TestNetnsProxySetKeepsTheRoutersOwnDialOverASpecificRoute(t *testing.T) {
	netnsProxySetKeepsTheRoutersOwnDialOverASpecificRoute(t, backendIPTables)
}

func TestNetnsProxySetKeepsTheRoutersOwnDialOverASpecificRouteNft(t *testing.T) {
	netnsProxySetKeepsTheRoutersOwnDialOverASpecificRoute(t, backendNFTables)
}

func netnsSourceCheckRuleCounts(t *testing.T, mark uint32) (current, earlier int) {
	t.Helper()
	for _, line := range strings.Split(netnsRun(t, "ip", "rule", "show"), "\n") {
		if !routeRuleIsSourceCheck(line) {
			continue
		}
		switch routeRuleField(line, "fwmark") {
		case routeSourceCheckMarkRule(mark):
			current++
		case routeSetMarkRule(mark):
			earlier++
		}
	}
	return current, earlier
}

func TestNetnsSourceCheckRulesAreReplacedAndRemovedWithTheSet(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, backendNFTables)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsProxyMixConfig()
	RoutingSyncConfig(cfg)
	cleared := false
	defer func() {
		if !cleared {
			RoutingClearAll()
		}
	}()

	st, ok := routeRuleCache["netns-proxy-set"]
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	perSet := routeSetMarkRule(st.mark)

	routeDelSourceCheckRules(st.mark)
	netnsRun(t, "ip", "rule", "add", "fwmark", perSet, "iif", "lo", "lookup", "main", "suppress_prefixlength", "7", "priority", "2")
	netnsRun(t, "ip", "rule", "add", "fwmark", perSet, "to", "10.0.0.0/8", "iif", "lo", "lookup", "main", "suppress_prefixlength", "7", "priority", "2")
	routeEnsureLocalDelivery(st.mark, st.table, true, false)
	routeEnsureLocalDelivery(st.mark, st.table, true, false)
	if current, earlier := netnsSourceCheckRuleCounts(t, st.mark); current != 1 || earlier != 0 {
		t.Errorf("after two ensures over the rules earlier builds added: %d current and %d earlier source-check rules, want 1 and 0; an earlier one ignores the router's own mark bit and takes its connections past the set, and extra copies pile up at every rebuild:\n%s",
			current, earlier, netnsRun(t, "ip", "rule", "show"))
	}

	RoutingClearAll()
	cleared = true
	if current, earlier := netnsSourceCheckRuleCounts(t, st.mark); current+earlier != 0 {
		t.Errorf("removing the set left %d source-check rules behind:\n%s", current+earlier, netnsRun(t, "ip", "rule", "show"))
	}
}
