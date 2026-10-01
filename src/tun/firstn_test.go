package tun

import (
	"net"
	"strings"
	"testing"
	"time"
)

func testPacket(proto uint8, src, dst string, sport, dport uint16, tcpFlags byte) []byte {
	ihl := 20
	l4 := 8
	if proto == 6 {
		l4 = 20
	}
	raw := make([]byte, ihl+l4)
	raw[0] = 0x45
	raw[9] = proto
	copy(raw[12:16], net.ParseIP(src).To4())
	copy(raw[16:20], net.ParseIP(dst).To4())
	raw[ihl], raw[ihl+1] = byte(sport>>8), byte(sport)
	raw[ihl+2], raw[ihl+3] = byte(dport>>8), byte(dport)
	if proto == 6 {
		raw[ihl+13] = tcpFlags
	}
	return raw
}

const (
	tcpSYN = 0x02
	tcpACK = 0x10
	tcpFIN = 0x01
)

func admitted(l *firstNLimiter, raw []byte, n int, now time.Time) int {
	got := 0
	for i := 0; i < n; i++ {
		if l.admit(raw, now) {
			got++
		}
	}
	return got
}

func TestFirstNLimiterProcessesOnlyTheFirstPacketsOfAFlow(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 3, udpLimit: 2})
	now := time.Now()

	tcp := testPacket(6, "192.168.0.10", "203.0.113.10", 40000, 443, tcpACK)
	if got := admitted(l, tcp, 10, now); got != 3 {
		t.Fatalf("tcp: %d of 10 packets processed, want 3", got)
	}
	other := testPacket(6, "192.168.0.10", "203.0.113.10", 40001, 443, tcpACK)
	if !l.admit(other, now) {
		t.Fatal("a second connection must start its own count")
	}
	udp := testPacket(17, "192.168.0.10", "203.0.113.10", 50000, 443, 0)
	if got := admitted(l, udp, 5, now); got != 2 {
		t.Fatalf("udp: %d of 5 packets processed, want 2", got)
	}
	if got := l.passed.Load(); got != 7+3 {
		t.Fatalf("passed = %d, want 10", got)
	}
}

func withSeq(raw []byte, seq uint32) []byte {
	raw[24], raw[25], raw[26], raw[27] = byte(seq>>24), byte(seq>>16), byte(seq>>8), byte(seq)
	return raw
}

func TestFirstNLimiterCountsRetransmittedSYNs(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 2, udpLimit: 2})
	now := time.Now()
	syn := withSeq(testPacket(6, "192.168.0.10", "203.0.113.10", 40000, 443, tcpSYN), 1000)
	if got := admitted(l, syn, 4, now); got != 2 {
		t.Fatalf("retransmitted SYNs belong to the same connection and count against its limit, got %d of 4 processed", got)
	}
}

func TestFirstNLimiterRestartsTheCountForANewConnectionOnTheSameTuple(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 2, udpLimit: 2})
	now := time.Now()
	data := testPacket(6, "192.168.0.10", "203.0.113.10", 40000, 443, tcpACK)
	first := withSeq(testPacket(6, "192.168.0.10", "203.0.113.10", 40000, 443, tcpSYN), 1000)
	second := withSeq(testPacket(6, "192.168.0.10", "203.0.113.10", 40000, 443, tcpSYN), 777777)

	l.admit(first, now)
	admitted(l, data, 5, now)
	if !l.admit(second, now) {
		t.Fatal("a SYN with a new initial sequence number opens a new connection and must be processed")
	}
	if !l.admit(data, now) || l.admit(data, now) {
		t.Fatal("the new connection gets its own first packets")
	}
}

func TestFirstNLimiterKeepsCountingAfterFINAndExpiresClosedFlowsSoon(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 2, udpLimit: 2})
	start := time.Now()
	data := testPacket(6, "192.168.0.10", "203.0.113.10", 40000, 443, tcpACK)
	fin := testPacket(6, "192.168.0.10", "203.0.113.10", 40000, 443, tcpFIN|tcpACK)
	live := testPacket(6, "192.168.0.10", "203.0.113.10", 40001, 443, tcpACK)

	admitted(l, data, 2, start)
	l.admit(live, start)
	if l.admit(fin, start) || l.admit(data, start) {
		t.Fatal("packets after a FIN belong to the same connection and must not restart its count")
	}
	l.sweep(start.Add(firstNClosedIdle + time.Second))
	if len(l.flows) != 1 {
		t.Fatalf("a closed flow must expire after %s while a live one stays, %d left", firstNClosedIdle, len(l.flows))
	}
}

func TestFirstNLimiterAlwaysProcessesDNSDuplicatesAndWhatItCannotKey(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 1, udpLimit: 1, dupIPs: []string{"198.51.100.7", "198.51.100.64/26"}})
	now := time.Now()
	cases := map[string][]byte{
		"dns query":        testPacket(17, "192.168.0.10", "192.168.0.1", 50000, 53, 0),
		"dns answer":       testPacket(17, "192.168.0.10", "192.168.0.20", 53, 50000, 0),
		"duplicated host":  testPacket(6, "192.168.0.10", "198.51.100.7", 40000, 443, tcpACK),
		"duplicated range": testPacket(6, "192.168.0.10", "198.51.100.70", 40000, 443, tcpACK),
		"icmp":             testPacket(1, "192.168.0.10", "203.0.113.10", 0, 0, 0),
	}
	for name, raw := range cases {
		if got := admitted(l, raw, 5, now); got != 5 {
			t.Errorf("%s: %d of 5 packets processed, want all", name, got)
		}
	}

	frag := testPacket(6, "192.168.0.10", "203.0.113.10", 40000, 443, tcpACK)
	frag[6] = 0x20
	if got := admitted(l, frag, 3, now); got != 3 {
		t.Errorf("fragments cannot be keyed and must all be processed, got %d of 3", got)
	}
	if !l.admit([]byte{0x60, 0, 0, 0}, now) {
		t.Error("IPv6 is left to the engine")
	}

	dupUDP := testPacket(17, "192.168.0.10", "198.51.100.7", 50000, 443, 0)
	if got := admitted(l, dupUDP, 3, now); got != 1 {
		t.Errorf("duplication covers tcp only, udp to a duplicated host keeps the udp limit: got %d of 3", got)
	}
}

func TestFirstNLimiterSweepsIdleFlowsAndStaysBounded(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 1, udpLimit: 1})
	start := time.Now()
	l.admit(testPacket(17, "192.168.0.10", "203.0.113.10", 50000, 443, 0), start)
	l.admit(testPacket(6, "192.168.0.10", "203.0.113.10", 40000, 443, tcpACK), start)

	l.sweep(start.Add(firstNUDPIdle + time.Second))
	if len(l.flows) != 1 {
		t.Fatalf("an idle udp flow must go and a tcp flow stay, %d left", len(l.flows))
	}
	l.sweep(start.Add(firstNTCPIdle + time.Second))
	if len(l.flows) != 0 {
		t.Fatalf("an idle tcp flow must go too, %d left", len(l.flows))
	}

	for i := 0; i < firstNMaxFlows; i++ {
		l.flows[flowKey{proto: 6, sport: uint16(i), dport: uint16(i >> 16)}] = &flowCount{packets: 9, seen: start}
	}
	for i := 0; i < 50; i++ {
		fresh := testPacket(6, "192.168.0.10", "203.0.113.10", uint16(41000+i), 443, tcpACK)
		if got := admitted(l, fresh, 3, start); got != 1 {
			t.Fatalf("flow %d: with the table full of live flows a new flow must still get a counter, got %d of 3 processed", i, got)
		}
	}
	if len(l.flows) != firstNMaxFlows {
		t.Fatalf("the table must stay at its cap by evicting, has %d entries", len(l.flows))
	}
}

func TestFirstNLimiterEvictsTheStalestOfItsSample(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 1, udpLimit: 1})
	start := time.Now()
	stale := flowKey{proto: 6, sport: 1}
	l.flows[stale] = &flowCount{packets: 1, seen: start.Add(-time.Hour)}
	for i := 2; i <= firstNEvictSample-1; i++ {
		l.flows[flowKey{proto: 6, sport: uint16(i)}] = &flowCount{packets: 1, seen: start}
	}
	l.evictStalestLocked()
	if _, ok := l.flows[stale]; ok {
		t.Fatal("with fewer flows than the sample size the stalest one must be the one evicted")
	}
	if len(l.flows) != firstNEvictSample-2 {
		t.Fatalf("exactly one flow must go, %d left", len(l.flows))
	}
}

func TestLocalOnlyChainExemptsLocalDNSButKeepsTheGateway(t *testing.T) {
	r := &routeManager{
		mark:        0x8000,
		localOnly:   true,
		outGateway:  "192.168.0.1",
		tcpPorts:    []string{"443"},
		udpPorts:    []string{"443"},
		tcpLimit:    19,
		udpLimit:    8,
		noConnbytes: true,
	}
	rules := r.captureChainRules(nil, []string{"172.17.0.0/16", "192.168.0.0/24"})

	const steer = " -j MARK --set-xmark 0x40000000/0x40000000"
	is := func(want string) func(string) bool { return func(s string) bool { return s == want } }
	gateway := captureRuleIndex(rules, is("-d 192.168.0.1 -p udp --dport 53"+steer))
	bridge := captureRuleIndex(rules, is("-d 172.17.0.0/16 -j RETURN"))
	lan := captureRuleIndex(rules, is("-d 192.168.0.0/24 -j RETURN"))
	dns := captureRuleIndex(rules, is("-p udp --dport 53"+steer))
	forward := captureRuleIndex(rules, is("-p tcp --dport 443"+steer))

	for name, idx := range map[string]int{"gateway dns": gateway, "bridge": bridge, "lan": lan, "dns": dns, "forward": forward} {
		if idx < 0 {
			t.Fatalf("%s rule missing from the chain: %v", name, rules)
		}
	}
	if !(gateway < bridge && gateway < lan && bridge < dns && lan < dns && dns < forward) {
		t.Fatalf("want gateway dns < local exemptions < dns < forward, got %d %d %d %d %d", gateway, bridge, lan, dns, forward)
	}
	for _, rule := range rules {
		s := strings.Join(rule.spec, " ")
		port := s == "-p tcp --dport 443"+steer || s == "-p udp --dport 443"+steer
		if rule.steer != port {
			t.Fatalf("only the port rules count as steer rules: %q steer=%v", s, rule.steer)
		}
	}
}
