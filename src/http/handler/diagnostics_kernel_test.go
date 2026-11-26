package handler

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

var tunPortCaptureGroups = []DiagRuleGroup{
	{Title: "iptables mangle B4_TUN", Rules: []string{"-N B4_TUN", "-A B4_TUN -m mark --mark 0x8000/0x8000 -j RETURN"}},
	{Title: "iptables mangle (b4)", Rules: []string{"-A PREROUTING -j B4_TUN", "-A OUTPUT -j B4_TUN"}},
	{Title: "iptables nat (b4)", Rules: []string{"-A POSTROUTING -o b4tun0 -j SNAT --to-source 172.17.0.2"}},
	{Title: "iptables filter (b4)", Rules: []string{"-A FORWARD -o b4tun0 -j ACCEPT", "-A FORWARD -i b4tun0 -j ACCEPT"}},
}

var tunDefaultCaptureGroups = []DiagRuleGroup{
	{Title: "iptables nat (b4)", Rules: []string{"-A POSTROUTING -o b4tun0 -j SNAT --to-source 172.17.0.2"}},
	{Title: "iptables raw (b4)", Rules: []string{"-A OUTPUT -m mark --mark 0x10000000/0x10000000 -j CT --notrack"}},
	{Title: "iptables filter (b4)", Rules: []string{"-A FORWARD -o b4tun0 -j ACCEPT", "-A FORWARD -i b4tun0 -j ACCEPT"}},
}

var defaultTUNRules = diagTUNRules{device: "b4tun0"}

func TestTUNFirewallBackendNamesTheIptablesThatHoldItsRules(t *testing.T) {
	for name, groups := range map[string][]DiagRuleGroup{"ports": tunPortCaptureGroups, "default": tunDefaultCaptureGroups} {
		if got := tunFirewallBackend("none", true, defaultTUNRules, groups); got != "iptables" {
			t.Errorf("TUN with %s capture and its rules in iptables: backend = %q, want iptables", name, got)
		}
	}
}

func TestTUNFirewallBackendLeavesOtherAnswersAlone(t *testing.T) {
	cases := []struct {
		name    string
		backend string
		tun     bool
		groups  []DiagRuleGroup
		want    string
	}{
		{"NFQUEUE rules found under TUN", "nftables", true, tunPortCaptureGroups, "nftables"},
		{"legacy NFQUEUE rules found under TUN", "iptables-legacy", true, tunPortCaptureGroups, "iptables-legacy"},
		{"TUN without any b4 iptables rules", "none", true, nil, "none"},
		{"NFQUEUE engine without a B4 chain", "none", false, tunDefaultCaptureGroups, "none"},
	}
	for _, c := range cases {
		if got := tunFirewallBackend(c.backend, c.tun, defaultTUNRules, c.groups); got != c.want {
			t.Errorf("%s: backend = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestTUNFirewallBackendCountsOnlyTheTUNEnginesOwnRules(t *testing.T) {
	custom := diagTUNRules{device: "tun0"}
	customDevice := []DiagRuleGroup{
		{Title: "iptables nat (b4)", Rules: []string{"-A POSTROUTING -o tun0 -j SNAT --to-source 10.0.0.2"}},
		{Title: "iptables filter (b4)", Rules: []string{"-A FORWARD -o tun0 -j ACCEPT", "-A FORWARD -i tun0 -j ACCEPT"}},
	}
	foreignQueue := []DiagRuleGroup{
		{Title: "iptables mangle (b4)", Rules: []string{"-A PREROUTING -p tcp -m tcp --dport 443 -j NFQUEUE --queue-num 200 --queue-bypass"}},
	}
	userNAT := []DiagRuleGroup{
		{Title: "iptables nat (b4)", Rules: []string{"-A POSTROUTING -o b4tun0 -j MASQUERADE"}},
	}
	cases := []struct {
		name   string
		rules  diagTUNRules
		groups []DiagRuleGroup
		want   string
	}{
		{"default capture on a custom device", custom, customDevice, "iptables"},
		{"another program's NFQUEUE rule alone", defaultTUNRules, foreignQueue, "none"},
		{"skip_setup with the user's own NAT on the TUN device", diagTUNRules{device: "b4tun0", skipSetup: true}, userNAT, "none"},
		{"skip_setup with b4's port capture chain", diagTUNRules{device: "b4tun0", skipSetup: true}, tunPortCaptureGroups, "iptables"},
	}
	for _, c := range cases {
		if got := tunFirewallBackend("none", true, c.rules, c.groups); got != c.want {
			t.Errorf("%s: backend = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestDiagTUNRulesFollowTheRunningEngine(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.Mode = "tun"
	cfg.Queue.TUN.DeviceName = "ppp0"
	cfg.System.Tables.SkipSetup = true
	for name, running := range map[string]*DiagTUN{
		"no TUN section":          nil,
		"the engine refused ppp0": {DeviceName: "ppp0"},
	} {
		if got := diagTUNRulesOf(&cfg, running); got != (diagTUNRules{skipSetup: true}) {
			t.Errorf("%s: %+v, want no device: without a running engine no interface is known to be b4's", name, got)
		}
	}

	running := &DiagTUN{Running: true, DeviceName: "tun7", skipTables: false}
	if got := diagTUNRulesOf(&cfg, running); got != (diagTUNRules{device: "tun7"}) {
		t.Errorf("running engine: %+v, want its device and the skip_setup it started with, not the saved one", got)
	}
	cfg.System.Tables.SkipSetup = false
	running.skipTables = true
	if got := diagTUNRulesOf(&cfg, running); got != (diagTUNRules{device: "tun7", skipSetup: true}) {
		t.Errorf("engine started with skip_setup, switched off since: %+v", got)
	}
}

func TestTUNFirewallBackendIgnoresRulesOnAnInterfaceTheEngineRefused(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.Mode = "tun"
	cfg.Queue.TUN.DeviceName = "ppp0"
	rules := diagTUNRulesOf(&cfg, &DiagTUN{DeviceName: "ppp0"})
	firmware := []string{
		"-A POSTROUTING -o ppp0 -j MASQUERADE",
		"-A FORWARD -i ppp0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT",
	}
	for _, l := range firmware {
		if isB4IptablesRule(l, rules.device) {
			t.Errorf("a firmware rule on ppp0 is listed as b4's: %q", l)
		}
	}
	groups := []DiagRuleGroup{{Title: "iptables nat (b4)", Rules: firmware}}
	if got := tunFirewallBackend("none", true, rules, groups); got != "none" {
		t.Errorf("backend = %q, want none: the TUN engine never started on ppp0", got)
	}
}

func TestTUNFirewallBackendUsesTheSkipSetupTheEngineStartedWith(t *testing.T) {
	defaultCapture := []DiagRuleGroup{
		{Title: "iptables nat (b4)", Rules: []string{"-A POSTROUTING -o b4tun0 -j SNAT --to-source 172.17.0.2"}},
		{Title: "iptables filter (b4)", Rules: []string{"-A FORWARD -o b4tun0 -j ACCEPT", "-A FORWARD -i b4tun0 -j ACCEPT"}},
	}
	cfg := config.NewConfig()
	cfg.System.Tables.SkipSetup = true
	rules := diagTUNRulesOf(&cfg, &DiagTUN{Running: true, DeviceName: "b4tun0"})
	if got := tunFirewallBackend("none", true, rules, defaultCapture); got != "iptables" {
		t.Errorf("engine started without skip_setup, which was switched on since, restart pending: backend = %q, want iptables", got)
	}
}

func TestNFQueueWorksIsThePacketQueueProbe(t *testing.T) {
	for _, available := range []bool{true, false} {
		k := DiagKernel{Capabilities: []DiagCapability{
			{Name: "transparent_proxy", Available: !available},
			packetQueueCapabilityRow(available, nil, nil, nil, true),
		}}
		if got := packetQueueWorks(k); got != available {
			t.Fatalf("packet_queue available=%v: nfqueue_works = %v", available, got)
		}
	}
	if packetQueueWorks(DiagKernel{}) {
		t.Fatal("nfqueue_works is true without a packet_queue probe")
	}
}

func TestPacketQueueDetailUnderTUNDoesNotAskToSwitchToTUN(t *testing.T) {
	missing := []string{"xt_NFQUEUE"}
	tun := packetQueueCapabilityRow(false, missing, nil, nil, true)
	if strings.Contains(tun.Detail, "switch to TUN") || strings.Contains(tun.Detail, "in use") || !strings.Contains(tun.Detail, "TUN mode does not use it") {
		t.Fatalf("TUN detail = %q", tun.Detail)
	}
	nfq := packetQueueCapabilityRow(false, missing, nil, nil, false)
	if !strings.Contains(nfq.Detail, "switch to TUN mode") {
		t.Fatalf("NFQUEUE detail = %q", nfq.Detail)
	}
	if tun.Name != "packet_queue" || tun.Available || len(tun.Missing) != 1 {
		t.Fatalf("row = %+v", tun)
	}
}
