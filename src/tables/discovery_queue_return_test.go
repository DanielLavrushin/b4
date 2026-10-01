package tables

import (
	"errors"
	"strings"
	"testing"
)

func discoveryRecordRun(t *testing.T, listings map[string]string) *[]string {
	t.Helper()
	origRun := run
	t.Cleanup(func() { run = origRun })
	calls := []string{}
	run = func(args ...string) (string, error) {
		line := strings.Join(args, " ")
		calls = append(calls, line)
		if out, ok := listings[line]; ok {
			return out, nil
		}
		for _, arg := range args {
			if arg == "-D" {
				return "iptables: Bad rule (does a matching rule exist in that chain?).", errors.New("exit status 1")
			}
		}
		return "", nil
	}
	return &calls
}

func discoveryCallIndex(calls []string, want string) int {
	for i, c := range calls {
		if c == want {
			return i
		}
	}
	return -1
}

func TestDiscoverySteeringKeepsItsMarksOutOfTheIptablesQueueChain(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true})
	calls := discoveryRecordRun(t, nil)

	if err := (&discoveryIptBackend{}).apply(0x8001, 0x8002, 541, 1); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		for _, mark := range []string{"0x8001", "0x8002"} {
			want := bin + " -w -t mangle -I B4 1 -m mark --mark " + mark + "/0xffffffff -j RETURN"
			if discoveryCallIndex(*calls, want) < 0 {
				t.Errorf("mangle POSTROUTING jumps to B4, whose queue rules have no return for Discovery's marks, so the main queue gets Discovery's probes and applies the live set's strategy on top of the one under test; missing %q", want)
			}
		}
	}

	*calls = nil
	(&discoveryIptBackend{}).clear(0x8001, 0x8002)
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		for _, mark := range []string{"0x8001", "0x8002"} {
			want := bin + " -w -t mangle -D B4 -m mark --mark " + mark + "/0xffffffff -j RETURN"
			if discoveryCallIndex(*calls, want) < 0 {
				t.Errorf("the end of a run must take its return out of B4 again; missing %q", want)
			}
		}
	}
}

func TestDiscoverySteeringKeepsItsMarksOutOfTheNftQueueChain(t *testing.T) {
	calls := discoveryRecordRun(t, nil)

	if err := (&discoveryNftBackend{}).apply(0x8001, 0x8002, 541, 1); err != nil {
		t.Fatalf("apply: %v", err)
	}
	for _, mark := range []string{"0x8001", "0x8002"} {
		want := "nft insert rule inet b4_mangle b4_chain meta mark " + mark + " return"
		if discoveryCallIndex(*calls, want) < 0 {
			t.Errorf("postrouting jumps to b4_chain, which returns only packets with every bit of the queue mark; Discovery's own marks need a return of their own, missing %q", want)
		}
	}
}

func TestDiscoveryClearTakesOnlyItsReturnsOutOfTheNftQueueChain(t *testing.T) {
	listing := `table inet b4_mangle {
	chain b4_chain {
		meta mark 0x00008001 return # handle 30
		meta mark 0x00008002 return # handle 31
		meta mark & 0x00008000 == 0x00008000 return # handle 4
		meta mark & 0x00027fff == 0x00024bab return # handle 5
		tcp dport 443 ct original packets < 20 counter packets 0 bytes 0 queue flags bypass to 537-540 # handle 7
	}
}`
	calls := discoveryRecordRun(t, map[string]string{
		"nft -a list chain inet b4_mangle b4_chain": listing,
	})

	(&discoveryNftBackend{}).clear(0x8001, 0x8002)

	for _, handle := range []string{"30", "31"} {
		if discoveryCallIndex(*calls, "nft delete rule inet b4_mangle b4_chain handle "+handle) < 0 {
			t.Errorf("Discovery's return at handle %s stayed in b4_chain after the run", handle)
		}
	}
	for _, handle := range []string{"4", "5", "7"} {
		if discoveryCallIndex(*calls, "nft delete rule inet b4_mangle b4_chain handle "+handle) >= 0 {
			t.Errorf("clearing Discovery deleted b4_chain's own rule at handle %s", handle)
		}
	}
}
