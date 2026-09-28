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
	want := routeSetMarkRule(st.mark) + " iif lo lookup main"
	if !strings.Contains(rules, want) {
		t.Errorf("no source-check rule for the set's mark (%q) in:\n%s", want, rules)
	}

	port, _ := portFromState(st)
	s := netnsRouterSockets(t, port, SelfDialMark)
	got := dev.probe(t, "tcp", netnsDevInet, 443, "inet")
	t.Logf("device -> internet with src_valid_mark=1: %s", got)
	if got != "reply=tproxy:inet" {
		netnsLogState(t, engine)
		t.Fatalf("device -> internet through a proxy set with net.ipv4.conf.all.src_valid_mark=1: %s; the kernel checked the device's address in the set's local-delivery table and dropped the SYN as a martian source", got)
	}
	if !s.got("tcp", "tproxy", "inet") {
		t.Errorf("the transparent listener never saw the device's connection: tcp=%v", s.tcp)
	}
}

func TestNetnsProxySetAnswersALanClientUnderSrcValidMark(t *testing.T) {
	netnsProxySetAnswersALanClientUnderSrcValidMark(t, backendIPTables)
}

func TestNetnsProxySetAnswersALanClientUnderSrcValidMarkNft(t *testing.T) {
	netnsProxySetAnswersALanClientUnderSrcValidMark(t, backendNFTables)
}

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
	port, _ := portFromState(st)
	mark := routeSetMarkRule(st.mark)

	rules := netnsRun(t, "ip", "rule", "show")
	for _, clientNet := range proxySourceCheckClientNets {
		if !strings.Contains(rules, "to "+clientNet+" fwmark "+mark+" iif lo lookup main") {
			t.Errorf("no source-check rule for clients in %s with the set's mark %s in:\n%s", clientNet, mark, rules)
		}
	}

	for _, route := range []string{"198.0.0.0/8", "198.51.100.0/24", netnsProxyTarget + "/32"} {
		netnsRun(t, "ip", "route", "add", route, "via", netnsSecondGW, "dev", netnsSecondary)
		t.Cleanup(func() { _, _ = run("ip", "route", "del", route, "via", netnsSecondGW, "dev", netnsSecondary) })
		reached := netnsRouterDialReachesListener(t, port, 3*time.Second)
		_, _ = run("ip", "route", "del", route, "via", netnsSecondGW, "dev", netnsSecondary)
		if !reached {
			netnsLogState(t, engine)
			t.Errorf("with %s via %s in the main table, the router's own dial to %s never reached the set's listener; the source-check rule sent it out through main instead of the local-delivery table", route, netnsSecondary, netnsProxyTarget)
		}
	}

	netnsRun(t, "ip", "route", "add", "198.51.100.0/24", "via", netnsSecondGW, "dev", netnsSecondary)
	t.Cleanup(func() {
		_, _ = run("ip", "route", "del", "198.51.100.0/24", "via", netnsSecondGW, "dev", netnsSecondary)
	})
	netnsRun(t, "ip", "rule", "add", "fwmark", mark, "iif", "lo", "lookup", "main", "suppress_prefixlength", "7", "priority", "1")
	t.Cleanup(func() {
		_, _ = run("ip", "rule", "del", "fwmark", mark, "iif", "lo", "lookup", "main", "priority", "1")
	})
	unscoped := netnsRouterDialReachesListener(t, port, time.Second)
	if unscoped {
		t.Error("the dial reached the listener even with a source-check rule that has no destination scope, so the test proves nothing about the scope")
	}
}

func TestNetnsProxySetKeepsTheRoutersOwnDialOverASpecificRoute(t *testing.T) {
	netnsProxySetKeepsTheRoutersOwnDialOverASpecificRoute(t, backendIPTables)
}

func TestNetnsProxySetKeepsTheRoutersOwnDialOverASpecificRouteNft(t *testing.T) {
	netnsProxySetKeepsTheRoutersOwnDialOverASpecificRoute(t, backendNFTables)
}

func netnsSourceCheckRuleCounts(t *testing.T, mark string) (scoped, unscoped int) {
	t.Helper()
	for _, line := range strings.Split(netnsRun(t, "ip", "rule", "show"), "\n") {
		if !routeRuleIsSourceCheck(line) || routeRuleField(line, "fwmark") != mark {
			continue
		}
		if routeRuleField(line, "to") == "" {
			unscoped++
		} else {
			scoped++
		}
	}
	return scoped, unscoped
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
	mark := routeSetMarkRule(st.mark)

	routeDelSourceCheckRule(mark)
	netnsRun(t, "ip", "rule", "add", "fwmark", mark, "iif", "lo", "lookup", "main", "suppress_prefixlength", "7", "priority", "2")
	routeEnsureLocalDelivery(st.mark, st.table, true, false)
	routeEnsureLocalDelivery(st.mark, st.table, true, false)
	if scoped, unscoped := netnsSourceCheckRuleCounts(t, mark); scoped != len(proxySourceCheckClientNets) || unscoped != 0 {
		t.Errorf("after two ensures over an unscoped rule from an earlier build: %d scoped and %d unscoped source-check rules, want %d and 0; an unscoped one takes the router's own dials away from the set, and extra copies pile up at every rebuild:\n%s",
			scoped, unscoped, len(proxySourceCheckClientNets), netnsRun(t, "ip", "rule", "show"))
	}

	RoutingClearAll()
	cleared = true
	if scoped, unscoped := netnsSourceCheckRuleCounts(t, mark); scoped+unscoped != 0 {
		t.Errorf("removing the set left %d source-check rules behind:\n%s", scoped+unscoped, netnsRun(t, "ip", "rule", "show"))
	}
}
