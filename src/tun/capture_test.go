package tun

import (
	"strings"
	"testing"
)

func TestChunkPorts(t *testing.T) {
	got := chunkPorts([]string{"1", "2", "3", "4", "5"}, 2)
	if len(got) != 3 {
		t.Fatalf("expected 3 chunks, got %d (%v)", len(got), got)
	}
	if got[0][0] != "1" || got[1][0] != "3" || got[2][0] != "5" {
		t.Errorf("unexpected chunk boundaries: %v", got)
	}
	if len(chunkPorts(nil, 15)) != 0 {
		t.Errorf("nil ports should yield no chunks")
	}
}

func TestNormalizePorts(t *testing.T) {
	got := normalizePorts([]string{"443", "8000-8100", "53"})
	want := []string{"443", "8000:8100", "53"}
	if !equalStringSet(got, want) {
		t.Errorf("normalizePorts = %v, want %v", got, want)
	}
}

func TestEqualStringSet(t *testing.T) {
	if !equalStringSet([]string{"a", "b"}, []string{"a", "b"}) {
		t.Error("equal slices reported unequal")
	}
	if equalStringSet([]string{"a"}, []string{"a", "b"}) {
		t.Error("different lengths reported equal")
	}
	if equalStringSet([]string{"a", "b"}, []string{"a", "c"}) {
		t.Error("different contents reported equal")
	}
}

func TestSteerMarkDefault(t *testing.T) {
	r := &routeManager{}
	if r.steerMarkStr() != "0x40000000/0x40000000" {
		t.Errorf("steerMarkStr = %q, want 0x40000000/0x40000000", r.steerMarkStr())
	}
}

func TestSteerSpecsMultiport(t *testing.T) {
	r := &routeManager{
		multiport: true,
		tcpPorts:  []string{"443", "8443"},
		udpPorts:  []string{"443"},
		tcpLimit:  19,
		udpLimit:  8,
	}
	specs := r.steerSpecs()
	joined := make([]string, len(specs))
	for i, s := range specs {
		joined[i] = strings.Join(s, " ")
	}
	all := strings.Join(joined, "\n")

	if !strings.Contains(all, "-p tcp -m multiport --dports 443,8443") {
		t.Errorf("missing tcp multiport rule:\n%s", all)
	}
	if !strings.Contains(all, "--connbytes 0:19") {
		t.Errorf("missing tcp connbytes 0:19:\n%s", all)
	}
	if !strings.Contains(all, "-p udp -m multiport --dports 443") || !strings.Contains(all, "--connbytes 0:8") {
		t.Errorf("missing udp multiport/connbytes:\n%s", all)
	}
	dns := make([]string, 0, 2)
	for _, spec := range r.dnsSteerSpecs() {
		dns = append(dns, strings.Join(spec, " "))
	}
	if !strings.Contains(strings.Join(dns, "\n"), "-p udp --dport 53 -j MARK --set-xmark 0x40000000/0x40000000") {
		t.Errorf("missing DNS steer rule:\n%s", strings.Join(dns, "\n"))
	}
	for _, s := range joined {
		if !strings.HasSuffix(s, "-j MARK --set-xmark 0x40000000/0x40000000") {
			t.Errorf("steer spec does not end in MARK: %q", s)
		}
	}
}

func TestSteerSpecsPerPortFallback(t *testing.T) {
	r := &routeManager{
		multiport: false,
		tcpPorts:  []string{"443", "8443"},
		udpPorts:  []string{"443"},
		tcpLimit:  19,
		udpLimit:  8,
	}
	specs := r.steerSpecs()
	var tcpRules int
	for _, s := range specs {
		j := strings.Join(s, " ")
		if strings.Contains(j, "multiport") {
			t.Errorf("fallback should not use multiport: %q", j)
		}
		if strings.HasPrefix(j, "-p tcp --dport") {
			tcpRules++
		}
	}
	if tcpRules != 2 {
		t.Errorf("expected 2 per-port tcp rules, got %d", tcpRules)
	}
}

func TestCountChainRules(t *testing.T) {
	full := "-N B4_TUN\n" +
		"-A B4_TUN -m mark --mark 0x8000/0x8000 -j RETURN\n" +
		"-A B4_TUN -m mark --mark 0x20000000/0x20000000 -j RETURN\n" +
		"-A B4_TUN -p udp -m udp --dport 53 -j MARK --set-xmark 0x40000000/0x40000000\n" +
		"-A B4_TUN -d 192.168.31.0/24 -j RETURN\n"
	cases := []struct {
		name string
		dump string
		want int
	}{
		{"flushed chain", "-N B4_TUN\n", 0},
		{"empty output", "", 0},
		{"populated chain", full, 4},
		{"gate chain lines are not capture rules", "-N B4_TUN_GATE\n-A B4_TUN_GATE -m mac --mac-source 02:42:AC:11:00:03 -j RETURN\n-A B4_TUN_GATE -j B4_TUN\n", 0},
		{"surrounding whitespace", "  -A B4_TUN -j RETURN  \r\n", 1},
	}
	for _, c := range cases {
		if got := countChainRules(c.dump, tunCaptureChain); got != c.want {
			t.Errorf("%s: countChainRules = %d, want %d", c.name, got, c.want)
		}
	}
}

func TestGateRulesFromDumpMatchesMACsPrintedInLowercase(t *testing.T) {
	for _, whiteIsBlack := range []bool{true, false} {
		r := &routeManager{whiteIsBlack: whiteIsBlack, selectedMACs: []string{"02:07:15:be:63:58", "00:0C:29:87:6C:85"}}
		var dump strings.Builder
		dump.WriteString("-N B4_TUN_GATE\n")
		target := tunCaptureChain
		if whiteIsBlack {
			target = "RETURN"
		}
		dump.WriteString("-A B4_TUN_GATE -m mac --mac-source 02:07:15:be:63:58 -j " + target + "\n")
		dump.WriteString("-A B4_TUN_GATE -m mac --mac-source 00:0c:29:87:6c:85 -j " + target + "\n")
		if whiteIsBlack {
			dump.WriteString("-A B4_TUN_GATE -j B4_TUN\n")
		}
		got, want := gateRulesFromDump(dump.String()), r.desiredGateRules()
		if !equalStringSet(got, want) {
			t.Errorf("whiteIsBlack=%v: gate dump %q does not match desired %q, so the gate would be rebuilt every reconcile", whiteIsBlack, got, want)
		}
	}
}

func dupCaptureManager(viaSet, multiport bool) *routeManager {
	return &routeManager{
		mark:         0x8000,
		multiport:    multiport,
		tcpPorts:     []string{"443", "8443"},
		udpPorts:     []string{"443"},
		tcpLimit:     19,
		udpLimit:     8,
		dupIPs:       []string{"20.33.25.0/24", "52.146.136.33", "140.82.112.0/20"},
		dupSetActive: viaSet,
	}
}

func joinSpecs(specs [][]string) []string {
	out := make([]string, len(specs))
	for i, s := range specs {
		out[i] = strings.Join(s, " ")
	}
	return out
}

func isDupSpec(s string) bool {
	return strings.Contains(s, " -d ") || strings.Contains(s, "--match-set "+tunDupSet+" ")
}

func TestDupAddressesShareOneSetRulePerPortChunk(t *testing.T) {
	var dup []string
	for _, s := range joinSpecs(dupCaptureManager(true, true).steerSpecs()) {
		if strings.Contains(s, " -d ") {
			t.Fatalf("with the ipset in place no address may get a rule of its own: %q", s)
		}
		if isDupSpec(s) {
			dup = append(dup, s)
		}
	}
	want := "-p tcp -m set --match-set b4_tun_dup_v4 dst -m multiport --dports 443,8443 -j MARK --set-xmark 0x40000000/0x40000000"
	if len(dup) != 1 || dup[0] != want {
		t.Fatalf("want only %q, got %q", want, dup)
	}
}

func TestDupSetGetsOneRulePerPortWithoutMultiport(t *testing.T) {
	var dup []string
	for _, s := range joinSpecs(dupCaptureManager(true, false).steerSpecs()) {
		if isDupSpec(s) {
			dup = append(dup, s)
		}
	}
	want := []string{
		"-p tcp -m set --match-set b4_tun_dup_v4 dst --dport 443 -j MARK --set-xmark 0x40000000/0x40000000",
		"-p tcp -m set --match-set b4_tun_dup_v4 dst --dport 8443 -j MARK --set-xmark 0x40000000/0x40000000",
	}
	if !equalStringSet(dup, want) {
		t.Fatalf("want %q, got %q", want, dup)
	}
}

func TestDupAddressesFallBackToARuleEachWithoutTheSet(t *testing.T) {
	r := dupCaptureManager(false, true)
	all := strings.Join(joinSpecs(r.steerSpecs()), "\n")
	for _, ip := range r.dupIPs {
		want := "-p tcp -d " + ip + " -m multiport --dports 443,8443 -j MARK --set-xmark 0x40000000/0x40000000"
		if !strings.Contains(all, want) {
			t.Fatalf("missing %q in\n%s", want, all)
		}
	}
	if strings.Contains(all, "--match-set") {
		t.Fatalf("the fallback must not reference the ipset:\n%s", all)
	}
}

func TestFirstPacketCaptureComesBeforeTheDuplicationRules(t *testing.T) {
	for _, viaSet := range []bool{true, false} {
		lastFirstN, firstDup := -1, -1
		for i, s := range joinSpecs(dupCaptureManager(viaSet, true).steerSpecs()) {
			switch {
			case strings.Contains(s, "connbytes"):
				lastFirstN = i
			case isDupSpec(s) && firstDup < 0:
				firstDup = i
			}
		}
		if lastFirstN < 0 || firstDup < 0 || lastFirstN > firstDup {
			t.Fatalf("viaSet=%v: a rebuild must restore first-packet capture before it spends time on duplication rules, got last first-N %d, first dup %d", viaSet, lastFirstN, firstDup)
		}
	}
}

func TestNoDuplicationRulesWithoutDuplicationAddresses(t *testing.T) {
	r := dupCaptureManager(true, true)
	r.dupIPs = nil
	for _, s := range joinSpecs(r.steerSpecs()) {
		if isDupSpec(s) {
			t.Fatalf("no duplication address, yet a duplication rule: %q", s)
		}
	}
}

func TestDupSetRuleIsARequiredCaptureRule(t *testing.T) {
	rules := dupCaptureManager(true, true).captureChainRules([]string{"b4r_abc_v4"}, []string{"192.168.31.0/24"})
	found := 0
	for _, rule := range rules {
		joined := strings.Join(rule.spec, " ")
		if !strings.Contains(joined, "--match-set "+tunDupSet+" ") {
			continue
		}
		found++
		if rule.soft || rule.local != "" {
			t.Fatalf("a failed duplication rule must count as missing, not pass as an optional exclusion: %q", joined)
		}
	}
	if found != 1 {
		t.Fatalf("want one duplication set rule in the chain, got %d", found)
	}
}

func TestRebuildStopsOnlyWhenTheEngineQuits(t *testing.T) {
	if (&routeManager{}).stopping() {
		t.Fatalf("a manager without a quit channel must never cut a rebuild short")
	}
	quit := make(chan struct{})
	r := &routeManager{quit: quit}
	if r.stopping() {
		t.Fatalf("stopping before the engine quits")
	}
	close(quit)
	if !r.stopping() {
		t.Fatalf("a closed quit channel must stop the rebuild")
	}
}
