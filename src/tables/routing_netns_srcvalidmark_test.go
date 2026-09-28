package tables

import (
	"os"
	"strings"
	"testing"
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
