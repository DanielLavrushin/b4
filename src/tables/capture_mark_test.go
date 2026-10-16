package tables

import (
	"slices"
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

func connmarkSaveTarget(t *testing.T, spec []string) uint64 {
	t.Helper()
	i := slices.Index(spec, "--nfmask")
	if i < 0 || i+3 >= len(spec) || spec[i+2] != "--ctmask" || spec[i+1] != spec[i+3] {
		t.Fatalf("CONNMARK rule %q does not save one bit set under the same packet and connection mask", spec)
	}
	v, err := strconv.ParseUint(spec[i+1], 0, 32)
	if err != nil {
		t.Fatalf("CONNMARK rule %q: %v", spec, err)
	}
	return v
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
		own, injected := uint64(mark), uint64(mark|engine.InjectedMarkBit)
		saves := map[string]map[uint64]int{}
		for _, r := range m.Rules {
			if r.Table != "mangle" || r.Chain != "OUTPUT" || !strings.Contains(strings.Join(r.Spec, " "), "CONNMARK") {
				continue
			}
			bit := connmarkSaveTarget(t, r.Spec)
			if saves[r.IPT] == nil {
				saves[r.IPT] = map[uint64]int{}
			}
			saves[r.IPT][bit]++
			if r.Action != "I" {
				t.Errorf("mark %#x: %q is appended; it has to sit above the ACCEPT for b4's own packets in mangle OUTPUT", mark, r.Spec)
			}
			switch bit {
			case own:
				if !markMatches(t, r.Spec, own) {
					t.Errorf("mark %#x: %q skips b4's own connections, so the replies to them reach b4's queue", mark, r.Spec)
				}
				if markMatches(t, r.Spec, injected) {
					t.Errorf("mark %#x: %q also marks the connections b4 sends fakes or split segments into as b4's own, so the RSTs and SYN-ACKs on them never reach b4", mark, r.Spec)
				}
			case engine.InjectedMarkBit:
				if !markMatches(t, r.Spec, injected) {
					t.Errorf("mark %#x: %q misses the packets b4 sends into connections it does not own", mark, r.Spec)
				}
				if markMatches(t, r.Spec, own) {
					t.Errorf("mark %#x: %q marks b4's own connections as ones b4 sent into", mark, r.Spec)
				}
			default:
				t.Errorf("mark %#x: %q saves bit %#x, neither the queue mark nor %#x", mark, r.Spec, bit, engine.InjectedMarkBit)
				continue
			}
			for _, toDevice := range []uint64{own | engine.ClientMark, injected | engine.ClientMark} {
				if markMatches(t, r.Spec, toDevice) {
					t.Errorf("mark %#x: %q also saves the mark of b4's answers to devices (%#x), so b4 then skips the later lookups the device sends from that socket", mark, r.Spec, toDevice)
				}
			}
		}
		for _, ipt := range []string{backendIPTables, backendIP6Tables} {
			if got := saves[ipt]; len(got) != 2 || got[own] != 1 || got[engine.InjectedMarkBit] != 1 {
				t.Errorf("mark %#x, %s: CONNMARK rules in mangle OUTPUT by saved bit %v, want one for b4's own connections and one for the connections it sends into", mark, ipt, got)
			}
		}

		teardown, err := manager.buildTeardownManifest()
		if err != nil {
			t.Fatalf("buildTeardownManifest: %v", err)
		}
		for _, ipt := range []string{backendIPTables, backendIP6Tables} {
			specs := manifestSpecs(teardown, ipt, "mangle", "OUTPUT")
			for _, earlier := range []struct {
				mask uint
				what string
			}{
				{mark, "before b4 left its answers to devices alone"},
				{mark | engine.ClientMark, "before b4 told the connections it sends into from its own"},
			} {
				want := strings.Join(iptConnmarkSave(mark, earlier.mask, mark), " ")
				if !slices.Contains(specs, want) {
					t.Errorf("mark %#x, %s: the teardown leaves %q, the CONNMARK rule of the versions %s, which keeps setting the queue mark on the connections b4 sends into", mark, ipt, want, earlier.what)
				}
			}
			for _, current := range [][]string{iptOwnConnmarkSave(mark), iptInjectedConnmarkSave(mark)} {
				if want := strings.Join(current, " "); !slices.Contains(specs, want) {
					t.Errorf("mark %#x, %s: the teardown misses %q", mark, ipt, want)
				}
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
