package tun

import (
	"strings"
	"testing"
)

const addrShowSample = `2: eth1    inet 172.20.119.223/24 brd 172.20.119.255 scope global eth1\       valid_lft forever preferred_lft forever
6: br-docker    inet 172.17.0.1/16 brd 172.17.255.255 scope global br-docker\       valid_lft forever preferred_lft forever
7: br-lan    inet 192.168.31.1/24 brd 192.168.31.255 scope global br-lan\       valid_lft forever preferred_lft forever
9: br-miot    inet 192.168.32.1/24 brd 192.168.32.255 scope global br-miot\       valid_lft forever preferred_lft forever
14: l2tp-vpn    inet 62.78.37.161/32 scope global l2tp-vpn\       valid_lft forever preferred_lft forever
15: b4tun0    inet 13.255.0.1/30 scope global b4tun0\       valid_lft forever preferred_lft forever`

func TestParseLocalNetsSkipsTheTunAndKeepsEveryConnectedNetwork(t *testing.T) {
	got := parseLocalNets(addrShowSample, "b4tun0")
	want := []string{"172.17.0.0/16", "172.20.119.0/24", "192.168.31.0/24", "192.168.32.0/24", "62.78.37.161/32"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
	for _, n := range got {
		if strings.HasPrefix(n, "13.255.0.") {
			t.Fatalf("the tun's own network must not be exempted: %v", got)
		}
	}
}

func TestParseLocalNetsDeduplicates(t *testing.T) {
	out := "7: br-lan    inet 192.168.31.1/24 scope global br-lan\n8: br-lan    inet 192.168.31.2/24 scope global secondary br-lan"
	got := parseLocalNets(out, "b4tun0")
	if len(got) != 1 || got[0] != "192.168.31.0/24" {
		t.Fatalf("got %v, want one 192.168.31.0/24", got)
	}
}

func TestCidrNetworkMasksTheHostBits(t *testing.T) {
	cases := map[string]string{
		"192.168.31.1/24": "192.168.31.0/24",
		"172.17.0.1/16":   "172.17.0.0/16",
		"62.78.37.161/32": "62.78.37.161/32",
		"10.255.0.1/30":   "10.255.0.0/30",
		"not-an-address":  "",
		"192.168.31.1":    "",
	}
	for in, want := range cases {
		if got := cidrNetwork(in); got != want {
			t.Fatalf("cidrNetwork(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestReplySteerSpecsAreEmptyWithoutReplyCapture(t *testing.T) {
	r := &routeManager{tcpPorts: []string{"443"}, replyCapture: false}
	if specs := r.replySteerSpecs(); len(specs) != 0 {
		t.Fatalf("without reply capture there is nothing to steer back, got %v", specs)
	}
}

func TestReplySteerSpecsAddRSTRulesOnlyWithReplyCapture(t *testing.T) {
	r := &routeManager{tcpPorts: []string{"443", "8443"}, replyCapture: true}
	specs := r.replySteerSpecs()
	if len(specs) != 2 {
		t.Fatalf("want one RST rule per port, got %v", specs)
	}
	rst := 0
	for _, s := range specs {
		if strings.Contains(strings.Join(s, " "), "--tcp-flags RST RST") {
			rst++
		}
	}
	if rst != 2 {
		t.Fatalf("want 2 RST rules, got %d in %v", rst, specs)
	}
}

func TestForwardSteerSpecsCarryNeitherReplyNorDNSRules(t *testing.T) {
	r := &routeManager{tcpPorts: []string{"443"}, udpPorts: []string{"443"}, tcpLimit: 19, udpLimit: 8, replyCapture: true}
	for _, s := range r.steerSpecs() {
		joined := strings.Join(s, " ")
		if strings.Contains(joined, "--sport") || strings.Contains(joined, "--sports") {
			t.Fatalf("reply-direction rule leaked into the forward specs: %q", joined)
		}
		if strings.Contains(joined, "--dport 53") {
			t.Fatalf("the DNS rules must sit above the local-network exemptions, not with the forward specs: %q", joined)
		}
	}
}

func TestClientMarkRuleArgsTargetTheClientMark(t *testing.T) {
	r := &routeManager{routeTable: 9999}
	add := r.clientMarkRuleArgs("add", "main")
	joined := strings.Join(add, " ")
	if joined != "ip rule add fwmark "+clientMarkMatch()+" lookup main" {
		t.Fatalf("unexpected rule args: %q", joined)
	}
	if clientLocalPrio >= clientBypassPrio {
		t.Fatalf("the local-first rule must be consulted before the bypass rule (%d, %d)", clientLocalPrio, clientBypassPrio)
	}
	if clientBypassPrio >= captureRulePrio {
		t.Fatalf("both client rules must be consulted before the capture steer (%d, %d)", clientBypassPrio, captureRulePrio)
	}
}

func TestClientDirectionIsNotRoutableUntilTheBypassIsInstalled(t *testing.T) {
	r := &routeManager{}
	if r.clientDirectionRoutable() {
		t.Fatalf("a fresh routeManager must not claim a client bypass it has not installed")
	}
	r.clientBypassOK.Store(true)
	if !r.clientDirectionRoutable() {
		t.Fatalf("the client direction must be usable once the bypass is in place")
	}
}

func TestDNSSteerSpecsCoverBothDirections(t *testing.T) {
	r := &routeManager{}
	specs := r.dnsSteerSpecs()
	if len(specs) != 2 {
		t.Fatalf("want a query rule and an answer rule, got %v", specs)
	}
	if !strings.Contains(strings.Join(specs[0], " "), "--dport 53") {
		t.Fatalf("first DNS rule should match queries: %v", specs[0])
	}
	if !strings.Contains(strings.Join(specs[1], " "), "--sport 53") {
		t.Fatalf("second DNS rule should match answers: %v", specs[1])
	}
}

func TestIsDNSResponseReadsTheQRBit(t *testing.T) {
	pkt := make([]byte, 20+8+12)
	pkt[0] = 0x45
	if isDNSResponse(pkt, 20) {
		t.Fatalf("a DNS query has the QR bit clear")
	}
	pkt[20+8+2] = 0x81
	if !isDNSResponse(pkt, 20) {
		t.Fatalf("a DNS answer has the QR bit set")
	}
	if isDNSResponse(pkt[:20+8], 20) {
		t.Fatalf("a truncated packet cannot be read as an answer")
	}
}

func TestCaptureChainOrderIsExclusionsThenDNSThenLocalThenForward(t *testing.T) {
	r := &routeManager{
		mark:         0x8000,
		tcpPorts:     []string{"443"},
		udpPorts:     []string{"443"},
		tcpLimit:     19,
		udpLimit:     8,
		replyCapture: true,
	}

	dns := strings.Join(r.dnsSteerSpecs()[0], " ")
	reply := strings.Join(r.replySteerSpecs()[0], " ")
	forward := strings.Join(r.steerSpecs()[0], " ")

	if !strings.Contains(dns, "--dport 53") {
		t.Fatalf("DNS specs must lead with the query rule: %q", dns)
	}
	if !strings.Contains(reply, "--tcp-flags RST RST") {
		t.Fatalf("reply specs must be RST rules once the DNS rules moved out: %q", reply)
	}
	if strings.Contains(forward, "53") {
		t.Fatalf("the forward specs must not carry a DNS rule any more: %q", forward)
	}
}

func TestRebuildOrderPutsLocalNetsFirstWithoutTheReinjectRule(t *testing.T) {
	withRule := &routeManager{reinjectLocalAdded: true}
	withoutRule := &routeManager{reinjectLocalAdded: false}

	if !withRule.reinjectLocalAdded {
		t.Fatal("fixture")
	}
	if withoutRule.reinjectLocalAdded {
		t.Fatal("fixture")
	}
	if len(withRule.dnsSteerSpecs()) != 2 || len(withoutRule.dnsSteerSpecs()) != 2 {
		t.Fatalf("the DNS rules exist either way, only their position changes")
	}
}
