package tun

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
)

const (
	steerTail     = " -j MARK --set-xmark 0x40000000/0x40000000"
	ctdirOriginal = " -m conntrack --ctdir ORIGINAL"
	ctdirReply    = " -m conntrack --ctdir REPLY"
	connbytes19   = " -m connbytes --connbytes-dir original --connbytes-mode packets --connbytes 0:19"
	tcpRST        = 0x04
)

func directionCaptureManager(multiport, noConnbytes, ctdir bool) *routeManager {
	r := dupCaptureManager(true, multiport)
	r.noConnbytes = noConnbytes
	r.ctdirOK = ctdir
	r.replyCapture = true
	return r
}

func isForwardTCPSteer(spec string) bool {
	return strings.HasPrefix(spec, "-p tcp -m multiport --dports ") || strings.HasPrefix(spec, "-p tcp --dport ")
}

func TestSteerSpecsSelectTCPByConntrackDirection(t *testing.T) {
	cases := []struct {
		multiport, noConnbytes bool
		want                   []string
	}{
		{true, false, []string{"-p tcp -m multiport --dports 443,8443" + connbytes19 + ctdirOriginal + steerTail}},
		{true, true, []string{"-p tcp -m multiport --dports 443,8443" + ctdirOriginal + steerTail}},
		{false, false, []string{
			"-p tcp --dport 443" + connbytes19 + ctdirOriginal + steerTail,
			"-p tcp --dport 8443" + connbytes19 + ctdirOriginal + steerTail,
		}},
		{false, true, []string{
			"-p tcp --dport 443" + ctdirOriginal + steerTail,
			"-p tcp --dport 8443" + ctdirOriginal + steerTail,
		}},
	}
	for _, c := range cases {
		name := fmt.Sprintf("multiport=%v noConnbytes=%v", c.multiport, c.noConnbytes)
		with := joinSpecs(directionCaptureManager(c.multiport, c.noConnbytes, true).steerSpecs())
		without := joinSpecs(directionCaptureManager(c.multiport, c.noConnbytes, false).steerSpecs())

		var forward []string
		for _, s := range with {
			if isForwardTCPSteer(s) {
				forward = append(forward, s)
			}
		}
		if !equalStringSet(forward, c.want) {
			t.Errorf("%s: forward TCP steer rules\n got %q\nwant %q", name, forward, c.want)
		}
		if len(with) != len(without) {
			t.Fatalf("%s: the conntrack match changes the number of steer rules: %d with, %d without", name, len(with), len(without))
		}
		for i := range with {
			if strings.Contains(without[i], "conntrack") {
				t.Errorf("%s: without the conntrack match a steer rule carries it: %q", name, without[i])
			}
			if isForwardTCPSteer(with[i]) {
				if strings.Replace(with[i], ctdirOriginal, "", 1) != without[i] {
					t.Errorf("%s: rule %d without the probe must be today's rule minus the direction match:\n with    %q\n without %q", name, i, with[i], without[i])
				}
				continue
			}
			if with[i] != without[i] {
				t.Errorf("%s: the UDP and duplication steer rules must not change, rule %d: %q became %q", name, i, without[i], with[i])
			}
		}
	}
}

func TestSteerSpecsWithoutTheConntrackMatchAreTodaysRules(t *testing.T) {
	got := joinSpecs(directionCaptureManager(true, false, false).steerSpecs())
	want := []string{
		"-p tcp -m multiport --dports 443,8443" + connbytes19 + steerTail,
		"-p udp -m multiport --dports 443 -m connbytes --connbytes-dir original --connbytes-mode packets --connbytes 0:8" + steerTail,
		"-p tcp -m set --match-set b4_tun_dup_v4 dst -m multiport --dports 443,8443" + steerTail,
	}
	if !equalStringSet(got, want) {
		t.Fatalf("a kernel without xt_conntrack must get exactly today's steer rules\n got %q\nwant %q", got, want)
	}
}

func TestReplySteerSpecsSelectRSTsByReplyDirection(t *testing.T) {
	cases := []struct {
		multiport, ctdir bool
		want             []string
	}{
		{true, true, []string{"-p tcp -m multiport --sports 443,8443 --tcp-flags RST RST" + ctdirReply + steerTail}},
		{true, false, []string{"-p tcp -m multiport --sports 443,8443 --tcp-flags RST RST" + steerTail}},
		{false, true, []string{
			"-p tcp --sport 443 --tcp-flags RST RST" + ctdirReply + steerTail,
			"-p tcp --sport 8443 --tcp-flags RST RST" + ctdirReply + steerTail,
		}},
		{false, false, []string{
			"-p tcp --sport 443 --tcp-flags RST RST" + steerTail,
			"-p tcp --sport 8443 --tcp-flags RST RST" + steerTail,
		}},
	}
	for _, c := range cases {
		r := directionCaptureManager(c.multiport, false, c.ctdir)
		if got := joinSpecs(r.replySteerSpecs()); !equalStringSet(got, c.want) {
			t.Errorf("multiport=%v ctdir=%v: RST capture\n got %q\nwant %q", c.multiport, c.ctdir, got, c.want)
		}
		r.replyCapture = false
		if got := r.replySteerSpecs(); len(got) != 0 {
			t.Errorf("multiport=%v ctdir=%v: without reply capture there is no RST rule, got %q", c.multiport, c.ctdir, got)
		}
	}
}

func TestDNSSteerSpecsIgnoreTheConntrackMatch(t *testing.T) {
	with := joinSpecs(directionCaptureManager(true, false, true).dnsSteerSpecs())
	without := joinSpecs(directionCaptureManager(true, false, false).dnsSteerSpecs())
	if !equalStringSet(with, without) {
		t.Fatalf("DNS steer rules must not change with the conntrack match: %q vs %q", with, without)
	}
}

func portCaptureSetupManager() *routeManager {
	return &routeManager{
		tunName:      "b4tun0",
		mark:         0x8000,
		captureTable: 96,
		tcpPorts:     []string{"443"},
		udpPorts:     []string{"443"},
		tcpLimit:     19,
		udpLimit:     8,
		replyCapture: true,
	}
}

func isCtdirProbe(cmd string) bool {
	return strings.HasPrefix(cmd, "iptables -t filter -A "+tunProbeChain+" ") && strings.Contains(cmd, "conntrack")
}

func TestTheConntrackProbeIsSeparateFromTheConnbytesProbe(t *testing.T) {
	for _, ctdirLoads := range []bool{true, false} {
		f := stubCapture(t, false, nil)
		f.fail = func(c string) bool {
			if isCtdirProbe(c) {
				return !ctdirLoads
			}
			return strings.Contains(c, " -D ") || strings.HasPrefix(c, "ip rule del")
		}
		r := portCaptureSetupManager()
		if mode := r.resolveCaptureMode(); mode != "ports" || r.noConnbytes {
			t.Fatalf("ctdir loads=%v: the connbytes probe must decide the capture mode alone, got mode %q noConnbytes=%v", ctdirLoads, mode, r.noConnbytes)
		}
		if err := r.setupPortCapture("198.51.100.1"); err != nil {
			t.Fatalf("ctdir loads=%v: setup: %v", ctdirLoads, err)
		}

		probes := 0
		for _, c := range f.calls {
			if strings.HasPrefix(c, "iptables -t filter -A "+tunProbeChain+" ") && strings.Contains(c, "connbytes") && strings.Contains(c, "conntrack") {
				t.Fatalf("the connbytes probe must not carry the conntrack match, or a kernel without xt_conntrack captures the whole default route: %q", c)
			}
			if c == "iptables -t filter -A "+tunProbeChain+" -p tcp -m conntrack --ctdir ORIGINAL -j ACCEPT" {
				probes++
			}
		}
		if probes != 1 {
			t.Fatalf("ctdir loads=%v: want one conntrack probe of its own, got %d", ctdirLoads, probes)
		}
		if r.ctdirOK != ctdirLoads {
			t.Fatalf("ctdirOK = %v, want %v", r.ctdirOK, ctdirLoads)
		}

		var forward, rst, conntrack int
		for _, s := range f.appended() {
			if strings.Contains(s, "conntrack") {
				conntrack++
			}
			switch {
			case strings.HasPrefix(s, "-p tcp -m multiport --dports 443") && strings.Contains(s, "connbytes"):
				forward++
				if strings.Contains(s, ctdirOriginal) != ctdirLoads {
					t.Errorf("ctdir loads=%v: forward steer %q", ctdirLoads, s)
				}
			case strings.Contains(s, "--tcp-flags RST RST"):
				rst++
				if strings.Contains(s, ctdirReply) != ctdirLoads {
					t.Errorf("ctdir loads=%v: RST capture %q", ctdirLoads, s)
				}
			}
		}
		if forward != 1 || rst != 1 {
			t.Fatalf("ctdir loads=%v: want one first-N forward steer and one RST capture, got %d and %d in %q", ctdirLoads, forward, rst, f.appended())
		}
		if want := map[bool]int{true: 2, false: 0}[ctdirLoads]; conntrack != want {
			t.Fatalf("ctdir loads=%v: %d rules carry the conntrack match, want %d: %q", ctdirLoads, conntrack, want, f.appended())
		}
	}
}

func provenanceManager() *routeManager {
	return &routeManager{resolvedCapture: "ports", ctdirOK: true}
}

func TestTUNPortsModeReadsTheDirectionFromTheRuleThatSteeredThePacket(t *testing.T) {
	firstFragment := testPacket(6, "62.78.52.126", "203.0.113.10", 40000, 443, tcpACK)
	firstFragment[6] = 0x20
	laterFragment := testPacket(6, "62.78.52.126", "203.0.113.10", 40000, 443, tcpACK)
	laterFragment[7] = 0xb9
	cases := []struct {
		name string
		raw  []byte
		want engine.Direction
	}{
		{"a forwarded client's packet after the SNAT", testPacket(6, "62.78.52.126", "203.0.113.10", 40000, 443, tcpACK), engine.DirOriginal},
		{"a client's SYN from a port a wide filter covers", testPacket(6, "62.78.52.126", "203.0.113.10", 50000, 443, tcpSYN), engine.DirOriginal},
		{"the router's own connection", testPacket(6, "192.168.31.1", "203.0.113.10", 40000, 443, tcpACK), engine.DirOriginal},
		{"an inbound connection to a forwarded port on a WAN address the chain does not exempt", testPacket(6, "203.0.113.50", "192.168.31.20", 51000, 443, tcpACK), engine.DirOriginal},
		{"a server's RST, which the RST capture steers", testPacket(6, "203.0.113.10", "192.168.31.20", 443, 40000, tcpRST|tcpACK), engine.DirUnknown},
		{"the router's own RST answering a closed port", testPacket(6, "62.78.52.126", "203.0.113.50", 443, 51000, tcpRST|tcpACK), engine.DirUnknown},
		{"a client's RST, which the forward steer can carry too", testPacket(6, "62.78.52.126", "203.0.113.10", 40000, 443, tcpRST), engine.DirUnknown},
		{"UDP with a payload", append(testPacket(17, "62.78.52.126", "203.0.113.10", 40000, 443, 0), make([]byte, 40)...), engine.DirUnknown},
		{"ICMP with a payload", append(testPacket(1, "62.78.52.126", "203.0.113.10", 0, 0, 0), make([]byte, 40)...), engine.DirUnknown},
		{"the first fragment of a TCP packet", firstFragment, engine.DirUnknown},
		{"a later fragment", laterFragment, engine.DirUnknown},
		{"a TCP header cut before its flags", testPacket(6, "62.78.52.126", "203.0.113.10", 40000, 443, tcpACK)[:33], engine.DirUnknown},
		{"a header length below the minimum", append([]byte{0x44}, testPacket(6, "62.78.52.126", "203.0.113.10", 40000, 443, tcpACK)[1:]...), engine.DirUnknown},
		{"IPv6", []byte{0x60, 0, 0, 0, 0, 20, 6, 64, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, engine.DirUnknown},
	}
	r := provenanceManager()
	for _, c := range cases {
		if got := r.tcpDirection(c.raw); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}

	noCtdir := provenanceManager()
	noCtdir.ctdirOK = false
	wholeRoute := provenanceManager()
	wholeRoute.resolvedCapture = "default"
	for _, c := range cases {
		if got := noCtdir.tcpDirection(c.raw); got != engine.DirUnknown {
			t.Errorf("without --ctdir the steer rules also carry the router's replies, so nothing may be read from them: %s got %d", c.name, got)
		}
		if got := wholeRoute.tcpDirection(c.raw); got != engine.DirUnknown {
			t.Errorf("the whole default route carries the replies of inbound connections, so no steer rule speaks for it: %s got %d", c.name, got)
		}
	}
}

func TestEveryTCPSteerRuleButTheRSTCaptureSelectsTheOriginalDirection(t *testing.T) {
	for _, multiport := range []bool{true, false} {
		for _, noConnbytes := range []bool{true, false} {
			for _, localOnly := range []bool{true, false} {
				for _, dup := range []string{"set", "addresses", "none"} {
					name := fmt.Sprintf("multiport=%v noConnbytes=%v localOnly=%v dup=%s", multiport, noConnbytes, localOnly, dup)
					r := directionCaptureManager(multiport, noConnbytes, true)
					r.localOnly = localOnly
					r.outGateway = "192.168.0.1"
					r.dupSetActive = dup == "set"
					if dup == "none" {
						r.dupIPs = nil
					}
					steers := 0
					for _, rule := range r.captureChainRules([]string{"b4r_abc_v4"}, []string{"192.168.31.0/24"}) {
						s := strings.Join(rule.spec, " ")
						if !strings.Contains(s, "-j MARK") {
							continue
						}
						if strings.Contains(s, ctdirReply) && !strings.Contains(s, "--tcp-flags RST RST") {
							t.Errorf("%s: only the RST capture may steer replies, or the TUN reads a reply as a client's packet: %q", name, s)
						}
						switch {
						case strings.HasPrefix(s, "-p udp ") || strings.Contains(s, " -p udp "):
						case !strings.HasPrefix(s, "-p tcp "):
							t.Errorf("%s: a steer rule that names no protocol can carry TCP replies: %q", name, s)
						case strings.Contains(s, "--tcp-flags RST RST"):
							if !strings.Contains(s, ctdirReply) {
								t.Errorf("%s: the RST capture must select replies: %q", name, s)
							}
						case isDupSpec(s):
							if strings.Contains(s, "conntrack") {
								t.Errorf("%s: the duplication rules keep their destination match alone: %q", name, s)
							}
						default:
							steers++
							if !strings.Contains(s, ctdirOriginal) {
								t.Errorf("%s: a TCP steer rule that carries packets without RST must select the original direction: %q", name, s)
							}
						}
					}
					if steers == 0 {
						t.Fatalf("%s: no forward steer rule in the chain", name)
					}
				}
			}
		}
	}
}

func flowPacket(src, dst string, sport, dport uint16, flags byte, seq uint32) []byte {
	return withSeq(testPacket(6, src, dst, sport, dport, flags), seq)
}

func TestFirstNRecordsTheDirectionAFlowStartedWith(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 2, udpLimit: 2, dupIPs: []string{"198.51.100.7"}})
	now := time.Now()
	dirOf := func(raw []byte) engine.Direction {
		_, d := l.admit(raw, now)
		return d
	}

	syn := flowPacket("62.78.52.126", "203.0.113.10", 40000, 443, tcpSYN, 1000)
	data := flowPacket("62.78.52.126", "203.0.113.10", 40000, 443, tcpACK, 1001)
	if got := dirOf(syn); got != engine.DirOriginal {
		t.Fatalf("a flow that starts with a SYN is the client's, got %d", got)
	}
	for i := 0; i < 4; i++ {
		if pass, got := l.admit(data, now); got != engine.DirOriginal {
			t.Fatalf("packet %d of the flow (pass=%v) must keep the record, got %d", i, pass, got)
		}
	}

	synAck := flowPacket("62.78.52.126", "198.51.100.200", 22, 50000, tcpSYN|tcpACK, 7000)
	reply := flowPacket("62.78.52.126", "198.51.100.200", 22, 50000, tcpACK, 7001)
	if got := dirOf(synAck); got != engine.DirReply {
		t.Fatalf("a flow that starts with a SYN-ACK answers an inbound connection, got %d", got)
	}
	if got := dirOf(reply); got != engine.DirReply {
		t.Fatalf("the rest of an inbound connection's answer keeps the record, got %d", got)
	}

	mid := flowPacket("62.78.52.126", "203.0.113.11", 40001, 443, tcpACK, 5000)
	if got := dirOf(mid); got != engine.DirUnknown {
		t.Fatalf("a flow first seen mid-stream has no record, got %d", got)
	}
	if got := dirOf(flowPacket("62.78.52.126", "203.0.113.11", 40001, 443, tcpSYN|tcpACK, 5001)); got != engine.DirUnknown {
		t.Fatalf("a SYN-ACK on a flow that already exists records nothing, got %d", got)
	}
	if got := dirOf(flowPacket("62.78.52.126", "203.0.113.11", 40001, 443, tcpSYN, 9000)); got != engine.DirOriginal {
		t.Fatalf("a new connection on the same tuple starts with a SYN and is the client's, got %d", got)
	}

	frag := flowPacket("62.78.52.126", "203.0.113.12", 40003, 443, tcpSYN, 1)
	frag[6] = 0x20
	unrecorded := map[string][]byte{
		"udp":                            testPacket(17, "62.78.52.126", "203.0.113.10", 50000, 443, 0),
		"dns":                            testPacket(17, "62.78.52.126", "203.0.113.10", 50000, 53, 0),
		"icmp":                           testPacket(1, "62.78.52.126", "203.0.113.10", 0, 0, 0),
		"ipv6":                           {0x60, 0, 0, 0},
		"fragment of a TCP flow":         frag,
		"truncated before the ports":     testPacket(6, "62.78.52.126", "203.0.113.13", 40004, 443, tcpSYN)[:22],
		"truncated before the TCP flags": testPacket(6, "62.78.52.126", "203.0.113.14", 40005, 443, tcpSYN)[:24],
	}
	for name, raw := range unrecorded {
		if got := dirOf(raw); got != engine.DirUnknown {
			t.Errorf("%s: got %d, want no record", name, got)
		}
	}
}

func TestFirstNRecordsFlowsToDuplicationAddressesWithoutLimitingThem(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 1, udpLimit: 1, dupIPs: []string{"198.51.100.7", "198.51.100.64/26"}})
	now := time.Now()
	for _, dst := range []string{"198.51.100.7", "198.51.100.70"} {
		if pass, dir := l.admit(flowPacket("62.78.52.126", dst, 40002, 443, tcpSYN, 1), now); !pass || dir != engine.DirOriginal {
			t.Fatalf("%s: a duplicated connection's SYN must pass and record the client's side, got pass=%v dir=%d", dst, pass, dir)
		}
		for i := 0; i < 5; i++ {
			if pass, dir := l.admit(flowPacket("62.78.52.126", dst, 40002, 443, tcpACK, 2), now); !pass || dir != engine.DirOriginal {
				t.Fatalf("%s: packet %d of a duplicated connection must pass with its record, got pass=%v dir=%d", dst, i, pass, dir)
			}
		}
	}
	if got := l.passed.Load(); got != 0 {
		t.Fatalf("duplicated connections are never cut off, yet %d packets were counted as past the limit", got)
	}
}

func TestFirstNRecordSurvivesASettingsChange(t *testing.T) {
	l := newFirstNLimiter(captureParams{tcpLimit: 2, udpLimit: 2})
	now := time.Now()
	l.admit(flowPacket("62.78.52.126", "203.0.113.10", 40000, 443, tcpSYN, 1000), now)
	l.setParams(captureParams{tcpLimit: 10, udpLimit: 10})
	if _, got := l.admit(flowPacket("62.78.52.126", "203.0.113.10", 40000, 443, tcpACK, 1001), now); got != engine.DirOriginal {
		t.Fatalf("which side opened a connection does not depend on the settings, got %d", got)
	}
}

func directionEngine(skip bool) *Engine {
	cfg := config.NewConfig()
	cfg.System.Tables.SkipSetup = skip
	e := NewEngine(&cfg, nil)
	e.routes = provenanceManager()
	e.routes.skipTables = skip
	return e
}

var allDirections = []engine.Direction{engine.DirUnknown, engine.DirOriginal, engine.DirReply}

func TestTheEngineChoosesTheDirectionSignalByCaptureMode(t *testing.T) {
	data := testPacket(6, "62.78.52.126", "203.0.113.10", 40000, 443, tcpACK)
	rst := testPacket(6, "203.0.113.10", "192.168.31.20", 443, 40000, tcpRST)

	e := directionEngine(false)
	for _, flow := range allDirections {
		if got := e.packetDirection(data, flow); got != engine.DirOriginal {
			t.Fatalf("ports mode with direction rules: a packet without RST came through an original-direction rule, flow record %d, got %d", flow, got)
		}
		if got := e.packetDirection(rst, flow); got != flow {
			t.Fatalf("ports mode with direction rules: an RST falls back to the flow record, want %d, got %d", flow, got)
		}
	}

	e.limiter = newFirstNLimiter(captureParams{tcpLimit: 1, udpLimit: 1})
	e.routes.resolvedCapture = "default"
	for _, flow := range allDirections {
		if got := e.packetDirection(data, flow); got != flow {
			t.Fatalf("whole default route: the flow record decides, want %d, got %d", flow, got)
		}
	}
	e.routes.resolvedCapture = "ports"
	e.routes.ctdirOK = false
	for _, flow := range allDirections {
		if got := e.packetDirection(data, flow); got != flow {
			t.Fatalf("ports mode without xt_conntrack: the flow record decides, want %d, got %d", flow, got)
		}
	}

	for _, skipAtStart := range []bool{true, false} {
		e := directionEngine(true)
		e.routes.skipTables = skipAtStart
		for _, limiter := range []bool{false, true} {
			if limiter {
				e.limiter = newFirstNLimiter(captureParams{tcpLimit: 1, udpLimit: 1})
			}
			for _, flow := range allDirections {
				if got := e.packetDirection(data, flow); got != engine.DirUnknown {
					t.Fatalf("skip_setup (at start %v, firstn %v) keeps the port test, got %d", skipAtStart, limiter, got)
				}
			}
		}
	}

	e = directionEngine(false)
	e.routes.skipTables = true
	if got := e.packetDirection(data, engine.DirOriginal); got != engine.DirUnknown {
		t.Fatalf("a run that started with skip_setup keeps the port test after skip_setup is turned off, got %d", got)
	}

	if got := (&Engine{}).packetDirection(data, engine.DirOriginal); got != engine.DirUnknown {
		t.Fatalf("an engine without routes has no direction signal, got %d", got)
	}
}

func TestTheDirectionExactnessTheTUNPublishes(t *testing.T) {
	cases := []struct {
		name    string
		capture string
		ctdir   bool
		firstn  bool
		skip    bool
		want    bool
	}{
		{"ports with direction rules", "ports", true, false, false, true},
		{"ports without xt_conntrack", "ports", false, false, false, false},
		{"ports without connbytes, b4 counts", "ports", false, true, false, true},
		{"whole default route, b4 counts", "default", false, true, false, true},
		{"skip_setup", "ports", true, false, true, false},
		{"skip_setup with b4 counting", "default", false, true, true, false},
	}
	for _, c := range cases {
		e := &Engine{routes: &routeManager{resolvedCapture: c.capture, ctdirOK: c.ctdir, skipTables: c.skip}}
		if c.firstn {
			e.limiter = newFirstNLimiter(captureParams{tcpLimit: 1, udpLimit: 1})
		}
		if got := e.tcpDirectionExact(); got != c.want {
			t.Errorf("%s: exact = %v, want %v", c.name, got, c.want)
		}
	}
	if (&Engine{}).tcpDirectionExact() {
		t.Error("an engine without routes cannot be exact")
	}
}

func senderEngine(ports ...string) *Engine {
	r := &routeManager{}
	r.setCaptureParams(captureParams{tcpPorts: ports})
	r.clientBypassOK.Store(true)
	return &Engine{routes: r, sender: &sock.Sender{}, clientSender: &sock.Sender{}}
}

func TestSenderForFollowsTheDirectionForTCP(t *testing.T) {
	e := senderEngine("443")
	serverRST := testPacket(6, "203.0.113.10", "192.168.31.20", 443, 40000, 0x04)
	client := testPacket(6, "62.78.52.126", "203.0.113.10", 40000, 443, tcpACK)

	cases := []struct {
		name string
		raw  []byte
		dir  engine.Direction
		want *sock.Sender
	}{
		{"a reply goes back toward the client", serverRST, engine.DirReply, e.clientSender},
		{"a reply from a port outside the filter still goes toward the client", client, engine.DirReply, e.clientSender},
		{"an original goes on to the server", client, engine.DirOriginal, e.sender},
		{"an original from a filtered port still goes on to the server", serverRST, engine.DirOriginal, e.sender},
		{"no direction: a source port in the filter means a reply", serverRST, engine.DirUnknown, e.clientSender},
		{"no direction: any other source port means an original", client, engine.DirUnknown, e.sender},
	}
	for _, c := range cases {
		if got := e.senderFor(c.raw, c.dir); got != c.want {
			t.Errorf("%s: wrong sender", c.name)
		}
	}
}

func TestSenderForWithAFilterOverClientSourcePorts(t *testing.T) {
	for _, filter := range [][]string{{"443"}, {"443", "10000:20000"}, {"1:65535"}} {
		e := senderEngine(filter...)
		for _, port := range []uint16{12345, 40000} {
			raw := testPacket(6, "62.78.52.126", "203.0.113.10", port, 443, tcpACK)
			if got := e.senderFor(raw, engine.DirOriginal); got != e.sender {
				t.Errorf("filter %v, client port %d: a client's packet must be sent on to the server", filter, port)
			}
			legacy := e.clientSender
			if !portMatches(port, filter) {
				legacy = e.sender
			}
			if got := e.senderFor(raw, engine.DirUnknown); got != legacy {
				t.Errorf("filter %v, client port %d: without a direction the port test must stay as it was", filter, port)
			}
		}
	}
}

func TestSenderForKeepsUDPFragmentsAndTheBypassCheck(t *testing.T) {
	e := senderEngine("1:65535")
	answer := testPacket(17, "192.168.31.1", "192.168.31.20", 53, 40000, 0)
	answer = append(answer, make([]byte, 12)...)
	answer[20+8+2] = 0x81
	query := testPacket(17, "192.168.31.20", "192.168.31.1", 40000, 53, 0)
	quic := testPacket(17, "203.0.113.10", "62.78.52.126", 443, 40000, 0)
	frag := testPacket(6, "203.0.113.10", "192.168.31.20", 443, 40000, 0x04)
	frag[6] = 0x20

	for _, dir := range []engine.Direction{engine.DirUnknown, engine.DirOriginal, engine.DirReply} {
		if e.senderFor(answer, dir) != e.clientSender {
			t.Errorf("dir %d: a DNS answer goes toward the client whatever the TCP direction says", dir)
		}
		if e.senderFor(query, dir) != e.sender || e.senderFor(quic, dir) != e.sender {
			t.Errorf("dir %d: other UDP keeps the uplink sender", dir)
		}
		if e.senderFor(frag, dir) != e.sender {
			t.Errorf("dir %d: every fragment takes the same sender", dir)
		}
	}

	e.routes.clientBypassOK.Store(false)
	frag[6] = 0
	if e.senderFor(frag, engine.DirReply) != e.sender || e.senderFor(answer, engine.DirReply) != e.sender {
		t.Error("without the client bypass rule everything leaves by the uplink sender")
	}
}

func issueEngine(filter, capture string, ctdir bool) *Engine {
	cfg := config.NewConfig()
	e := NewEngine(&cfg, nil)
	e.routes = &routeManager{resolvedCapture: capture, ctdirOK: ctdir}
	e.routes.setCaptureParams(captureParams{tcpPorts: normalizePorts(strings.Split(filter, ","))})
	e.routes.clientBypassOK.Store(true)
	e.sender, e.clientSender = &sock.Sender{}, &sock.Sender{}
	if capture == "default" {
		e.limiter = newFirstNLimiter(captureParams{tcpLimit: 19, udpLimit: 8})
	}
	return e
}

func TestAFilterOverClientSourcePortsNoLongerTurnsOutgoingTCPAroundInTUN(t *testing.T) {
	now := time.Now()
	for _, filter := range []string{"443", "443,10000-20000", "1-65535"} {
		for _, port := range []uint16{12345, 40000} {
			syn := flowPacket("62.78.52.126", "198.51.100.10", port, 443, tcpSYN, 1000)
			hello := flowPacket("62.78.52.126", "198.51.100.10", port, 443, tcpACK|0x08, 1001)
			legacyReply := portMatches(port, normalizePorts(strings.Split(filter, ",")))

			for _, capture := range []string{"ports", "default"} {
				name := fmt.Sprintf("%s capture, filter %s, client port %d", capture, filter, port)
				e := issueEngine(filter, capture, true)
				flow := engine.DirUnknown
				if e.limiter != nil {
					e.limiter.admit(syn, now)
					_, flow = e.limiter.admit(hello, now)
				}
				dir := e.packetDirection(hello, flow)
				if dir != engine.DirOriginal {
					t.Errorf("%s: the client's ClientHello must be read as the client's, got %d", name, dir)
				}
				if e.senderFor(hello, dir) != e.sender {
					t.Errorf("%s: the client's ClientHello must be sent on to the server", name)
				}
			}

			e := issueEngine(filter, "ports", false)
			dir := e.packetDirection(hello, engine.DirUnknown)
			if dir != engine.DirUnknown {
				t.Errorf("filter %s, client port %d: without xt_conntrack the port test must stay, got %d", filter, port, dir)
			}
			if (e.senderFor(hello, dir) == e.clientSender) != legacyReply {
				t.Errorf("filter %s, client port %d: without xt_conntrack the sender must follow today's port test", filter, port)
			}
		}
	}
}

func TestAReplyRSTTheRouterSendsItselfKeepsThePortTest(t *testing.T) {
	for _, filter := range []string{"443", "1-65535"} {
		e := issueEngine(filter, "ports", true)
		rst := testPacket(6, "62.78.52.126", "203.0.113.50", 443, 51000, tcpRST|tcpACK)
		dir := e.packetDirection(rst, engine.DirUnknown)
		if dir != engine.DirUnknown {
			t.Fatalf("filter %s: the RST capture also takes the RSTs the router sends as a server, so an RST keeps the port test, got %d", filter, dir)
		}
		if e.senderFor(rst, dir) != e.clientSender {
			t.Fatalf("filter %s: a reply RST from a capture port goes back toward the client as before", filter)
		}
	}
}
