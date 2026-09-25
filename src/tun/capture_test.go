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
