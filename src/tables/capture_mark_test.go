package tables

import (
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/engine"
)

func markMatches(t *testing.T, spec []string, mark uint64) bool {
	t.Helper()
	for i := 0; i+1 < len(spec); i++ {
		if spec[i] != "--mark" {
			continue
		}
		value, mask, _ := strings.Cut(spec[i+1], "/")
		v, err1 := strconv.ParseUint(value, 0, 32)
		m, err2 := strconv.ParseUint(mask, 0, 32)
		if err1 != nil || err2 != nil {
			t.Fatalf("mark match %q does not parse", spec[i+1])
		}
		return mark&m == v
	}
	t.Fatalf("no mark match in %q", spec)
	return false
}

func TestTheConnmarkSaveLeavesB4sAnswersToDevicesAlone(t *testing.T) {
	for _, mark := range []uint{0x8000, 0x400000} {
		manager := dnsQueryTestManager(t, nil)
		manager.cfg.Queue.Mark = mark
		manager.connmarkSupport = map[string]bool{backendIPTables: true, backendIP6Tables: true}
		m, err := manager.buildManifest()
		if err != nil {
			t.Fatalf("buildManifest: %v", err)
		}
		saves := 0
		for _, r := range m.Rules {
			if r.Table != "mangle" || r.Chain != "OUTPUT" || !strings.Contains(strings.Join(r.Spec, " "), "CONNMARK") {
				continue
			}
			saves++
			if !markMatches(t, r.Spec, uint64(mark)) {
				t.Errorf("mark %#x: %q skips b4's own connections, so the replies to them reach b4's queue", mark, r.Spec)
			}
			if markMatches(t, r.Spec, uint64(mark|engine.ClientMark)) {
				t.Errorf("mark %#x: %q also saves the mark of b4's answers to devices, so b4 then skips the later lookups the device sends from that socket", mark, r.Spec)
			}
		}
		if saves != 2 {
			t.Fatalf("mark %#x: %d CONNMARK rules in mangle OUTPUT, want one per binary", mark, saves)
		}

		teardown, err := manager.buildTeardownManifest()
		if err != nil {
			t.Fatalf("buildTeardownManifest: %v", err)
		}
		legacy := iptMarkAccept(mark)
		for _, ipt := range []string{backendIPTables, backendIP6Tables} {
			found := false
			for _, s := range manifestSpecs(teardown, ipt, "mangle", "OUTPUT") {
				found = found || strings.HasPrefix(s, "-m mark --mark "+legacy+" -j CONNMARK")
			}
			if !found {
				t.Errorf("mark %#x, %s: the teardown leaves the CONNMARK rule an earlier version installed, which keeps marking the connections of b4's answers", mark, ipt)
			}
		}
	}
}

func TestCaptureChainLetsB4sOwnPacketsThroughFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		mark uint
		want string
	}{
		{"default mark", 0, "-m mark --mark 0x8000/0x8000 -j RETURN"},
		{"configured mark", 0x400000, "-m mark --mark 0x400000/0x400000 -j RETURN"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			manager := dnsQueryTestManager(t, nil)
			manager.cfg.Queue.Mark = tc.mark
			m, err := manager.buildManifest()
			if err != nil {
				t.Fatalf("buildManifest: %v", err)
			}
			for _, ipt := range []string{backendIPTables, backendIP6Tables} {
				returns, firstQueue := -1, -1
				for i, r := range m.Rules {
					if r.IPT != ipt || r.Table != "mangle" || r.Chain != iptChainName {
						continue
					}
					spec := strings.Join(r.Spec, " ")
					if spec == tc.want {
						if returns >= 0 {
							t.Errorf("%s: %s holds the queue-mark RETURN twice", ipt, iptChainName)
						}
						returns = i
						if r.Action != "I" {
							t.Errorf("%s: the queue-mark RETURN uses action %q; it must be inserted, so a restore that adds only this rule still puts it ahead of the queue rules", ipt, r.Action)
						}
					}
					if !strings.Contains(spec, "NFQUEUE") {
						continue
					}
					if firstQueue < 0 {
						firstQueue = i
					}
					if r.Action != "A" {
						t.Errorf("%s: %s inserts the queue rule %q; a restore that adds only this rule would put it above the queue-mark RETURN, and the monitor checks that rules are present, not their order", ipt, iptChainName, spec)
					}
				}
				if returns < 0 {
					t.Fatalf("%s: %s has no %q, so packets b4 injects itself are queued a second time at POSTROUTING and clash in conntrack on kernels before 4.18; rules: %q", ipt, iptChainName, tc.want, manifestSpecs(m, ipt, "mangle", iptChainName))
				}
				if firstQueue >= 0 && returns > firstQueue {
					t.Errorf("%s: the queue-mark RETURN comes after a queue rule in %s", ipt, iptChainName)
				}
			}

			teardown, err := manager.buildTeardownManifest()
			if err != nil {
				t.Fatalf("buildTeardownManifest: %v", err)
			}
			for _, ipt := range []string{backendIPTables, backendIP6Tables} {
				found := false
				for _, s := range manifestSpecs(teardown, ipt, "mangle", iptChainName) {
					found = found || s == tc.want
				}
				if !found {
					t.Errorf("%s: the teardown manifest misses %q, so a clear would leave it behind", ipt, tc.want)
				}
			}
		})
	}
}
