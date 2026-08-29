package tun

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/tables"
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
	if clientBypassPrio >= reinjectLocalPrio {
		t.Fatalf("the client rules must be consulted before the re-inject rules (%d, %d)", clientBypassPrio, reinjectLocalPrio)
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

func captureRuleIndex(rules []captureRule, match func(string) bool) int {
	for i, rule := range rules {
		if match(strings.Join(rule.spec, " ")) {
			return i
		}
	}
	return -1
}

func orderedCaptureManager(reinjectLocal bool) *routeManager {
	return &routeManager{
		mark:               0x8000,
		outIface:           "l2tp-vpn",
		tcpPorts:           []string{"443"},
		udpPorts:           []string{"443"},
		tcpLimit:           19,
		udpLimit:           8,
		replyCapture:       true,
		reinjectLocalAdded: reinjectLocal,
	}
}

func TestCaptureChainOrderWithTheReinjectRuleKeepsLANResolversIntercepted(t *testing.T) {
	r := orderedCaptureManager(true)
	rules := r.captureChainRules([]string{"b4r_abc_v4"}, []string{"192.168.31.0/24"})

	guard := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, "0x8000/0x8000") })
	excl := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, "--match-set b4r_abc_v4") })
	dns := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, "--dport 53") })
	local := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, "-d 192.168.31.0/24") })
	forward := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, "connbytes") })

	for name, idx := range map[string]int{"guard": guard, "exclusion": excl, "dns": dns, "local": local, "forward": forward} {
		if idx < 0 {
			t.Fatalf("%s rule missing from the chain: %v", name, rules)
		}
	}
	if !(guard < excl && excl < dns && dns < local && local < forward) {
		t.Fatalf("want guard < exclusion < dns < local < forward, got %d %d %d %d %d", guard, excl, dns, local, forward)
	}
}

func TestCaptureChainOrderWithoutTheReinjectRuleKeepsLANTrafficLocal(t *testing.T) {
	r := orderedCaptureManager(false)
	rules := r.captureChainRules(nil, []string{"192.168.31.0/24"})

	dns := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, "--dport 53") })
	rst := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, "--tcp-flags RST RST") })
	local := captureRuleIndex(rules, func(s string) bool { return strings.Contains(s, "-d 192.168.31.0/24") })

	if local < 0 || dns < 0 || rst < 0 {
		t.Fatalf("chain is missing a rule: %v", rules)
	}
	if local > dns || local > rst {
		t.Fatalf("without the re-inject rule a local destination must be exempted before anything steers it, got local=%d dns=%d rst=%d", local, dns, rst)
	}
}

func TestCaptureChainMarksExclusionsAsSoftFailures(t *testing.T) {
	r := orderedCaptureManager(true)
	rules := r.captureChainRules([]string{"b4r_abc_v4"}, []string{"192.168.31.0/24"})

	for _, rule := range rules {
		joined := strings.Join(rule.spec, " ")
		switch {
		case strings.Contains(joined, "--match-set"):
			if !rule.soft {
				t.Fatalf("a missing ipset is expected and must not warn: %q", joined)
			}
		case strings.Contains(joined, "-d 192.168.31.0/24"):
			if rule.local != "192.168.31.0/24" {
				t.Fatalf("a local exemption must carry its network so a failure is not recorded as applied: %q", joined)
			}
		default:
			if rule.soft || rule.local != "" {
				t.Fatalf("unexpected classification on %q", joined)
			}
		}
	}
}

func TestCapturePriorityStaysBelowTheProxyDivertAndAboveMain(t *testing.T) {
	if capturePrioFloor <= tables.ProxyRulePriority() {
		t.Fatalf("a proxy set must divert to b4's listener before capture steals the packet (floor %d, tproxy %d)", capturePrioFloor, tables.ProxyRulePriority())
	}
	if defaultCapturePrio >= 32766 {
		t.Fatalf("capture must be consulted before the main table (%d)", defaultCapturePrio)
	}
	if defaultCapturePrio <= capturePrioFloor {
		t.Fatalf("the default must leave room to move down (default %d, floor %d)", defaultCapturePrio, capturePrioFloor)
	}
}

func TestRulePriorityParsesTheLeadingField(t *testing.T) {
	cases := map[string]int{
		"0:\tfrom all lookup local":                     0,
		"51:\tfrom 192.168.1.0/24 lookup 250":           51,
		"32766:\tfrom all lookup main":                  32766,
		"4:\tfrom all fwmark 0x10000/0x10000 lookup 77": 4,
	}
	for line, want := range cases {
		got, ok := rulePriority(line)
		if !ok || got != want {
			t.Fatalf("rulePriority(%q) = %d,%v; want %d", line, got, ok, want)
		}
	}
	if _, ok := rulePriority(""); ok {
		t.Fatalf("an empty line has no priority")
	}
	if _, ok := rulePriority("from all lookup main"); ok {
		t.Fatalf("a line with no leading priority must be skipped")
	}
}

func TestParseSteerProbe(t *testing.T) {
	toTun := "1.1.1.1 from 192.168.1.100 dev b4tun0 table 9998 mark 0x40000000 \n    cache iif br0"
	if dev, reached := parseSteerProbe(toTun, "b4tun0"); !reached || dev != "b4tun0 table 9998" {
		t.Fatalf("got %q,%v; want the tun and reached", dev, reached)
	}
	stolen := "1.1.1.1 from 192.168.1.100 dev xray0 table 250 mark 0x40000000 \n    cache iif br0"
	if dev, reached := parseSteerProbe(stolen, "b4tun0"); reached || dev != "xray0 table 250" {
		t.Fatalf("got %q,%v; want xray0 and not reached", dev, reached)
	}
	main := "1.1.1.1 via 10.0.0.1 dev eth0 src 10.0.0.2 mark 0x40000000"
	if dev, reached := parseSteerProbe(main, "b4tun0"); reached || dev != "eth0" {
		t.Fatalf("got %q,%v; want eth0 and not reached", dev, reached)
	}
	if dev, reached := parseSteerProbe("", "b4tun0"); reached || dev != "" {
		t.Fatalf("got %q,%v; empty output means no answer", dev, reached)
	}
}

func TestProbeSourceForPicksAHostInsideTheNetwork(t *testing.T) {
	cases := map[string]string{
		"192.168.1.1/24":   "192.168.1.2",
		"172.17.0.1/16":    "172.17.0.2",
		"10.8.0.1/24":      "10.8.0.2",
		"94.189.76.227/32": "94.189.76.227",
		"nonsense":         "",
	}
	for in, want := range cases {
		if got := probeSourceFor(in); got != want {
			t.Fatalf("probeSourceFor(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSteerProbeMarkHasNoMaskBecauseIpRouteGetRejectsOne(t *testing.T) {
	if strings.Contains(steerProbeMark(), "/") {
		t.Fatalf("ip route get refuses a masked mark with 'invalid mark value', got %q", steerProbeMark())
	}
	if steerProbeMark() != "0x40000000" {
		t.Fatalf("the probe must carry the steer mark, got %q", steerProbeMark())
	}
	r := &routeManager{}
	if !strings.Contains(r.steerMarkStr(), "/") {
		t.Fatalf("the iptables form still needs its mask, got %q", r.steerMarkStr())
	}
}

func TestSteerConflictReadsAsAnExplanation(t *testing.T) {
	c := steerConflict{Iface: "br0", Source: "192.168.1.2", Went: "xray0 table 250"}
	got := c.String()
	for _, want := range []string{"br0", "192.168.1.2", "xray0 table 250"} {
		if !strings.Contains(got, want) {
			t.Fatalf("%q should name %q so the log points at the conflicting rule", got, want)
		}
	}
}

func TestConflictsEqual(t *testing.T) {
	a := []steerConflict{{Iface: "br0", Source: "192.168.1.2", Went: "xray0 table 250"}}
	if !conflictsEqual(a, []steerConflict{{Iface: "br0", Source: "192.168.1.2", Went: "xray0 table 250"}}) {
		t.Fatalf("the same conflict must not be re-logged on every reconcile")
	}
	if conflictsEqual(a, nil) {
		t.Fatalf("a conflict clearing must be noticed")
	}
	if conflictsEqual(a, []steerConflict{{Iface: "br0", Source: "192.168.1.2", Went: "eth0"}}) {
		t.Fatalf("a conflict that changed target must be re-logged")
	}
}
