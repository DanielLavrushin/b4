package handler

import (
	"strings"
	"testing"
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

func TestTUNFirewallBackendNamesTheIptablesThatHoldItsRules(t *testing.T) {
	for name, groups := range map[string][]DiagRuleGroup{"ports": tunPortCaptureGroups, "default": tunDefaultCaptureGroups} {
		if got := tunFirewallBackend("none", true, groups); got != "iptables" {
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
		if got := tunFirewallBackend(c.backend, c.tun, c.groups); got != c.want {
			t.Errorf("%s: backend = %q, want %q", c.name, got, c.want)
		}
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
	if strings.Contains(tun.Detail, "switch to TUN") || !strings.Contains(tun.Detail, "TUN engine in use does not need it") {
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
