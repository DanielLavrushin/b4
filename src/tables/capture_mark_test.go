package tables

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
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

func TestTheOwnPacketReturnLeavesAnotherServicesMarkToTheQueue(t *testing.T) {
	spec := iptMarkReturn(iptOwnPacketMark(0x8000))
	for _, c := range []struct {
		name string
		mark uint64
		want bool
	}{
		{"the queue mark", 0x8000, true},
		{"the queue mark with a set's route bits", 0x8000 | 0x2bd8, true},
		{"a packet toward a device", 0x8000 | engine.ClientMark, true},
		{"a packet TUN mode writes back", 0x8000 | engine.ReinjectMarkBit, true},
		{"a packet b4 sends into a client's connection", 0x8000 | engine.InjectedMarkBit, true},
		{"wg-quick's socket mark", 0xca6c, true},
		{"a KeeneticOS hotspot mark", 0x0ffffaaa, false},
		{"the queue mark with bit 16", 0x8000 | 0x10000, false},
		{"the queue mark with bit 31", 0x80008000, false},
		{"a MagiTrickle mark", 0x4d616769, false},
		{"no mark", 0, false},
	} {
		if got := markMatches(t, spec, c.mark); got != c.want {
			t.Errorf("%s (0x%x): returned as b4's own = %v, want %v", c.name, c.mark, got, c.want)
		}
	}
}

func TestTheOwnPacketReturnTakesWhatB4SendsIntoItsTelegramConnections(t *testing.T) {
	inj, dial := uint64(engine.InjectedMarkBit), uint64(config.TelegramSetDialMark)
	for _, queue := range []uint{0, 0x400000, 0x8001, 0x48000, 0x4008000, 0x4000000, 0x4040000} {
		cfg := config.NewConfig()
		cfg.Queue.Mark = queue
		q := uint64(queue)
		if q == 0 {
			q = 0x8000
		}
		ipt := iptMarkReturn(iptOwnPacketMark(queue))
		nftMask, nftValue := nftOwnPacketMark(queue)
		mask, err1 := strconv.ParseUint(nftMask, 0, 32)
		value, err2 := strconv.ParseUint(nftValue, 0, 32)
		if err1 != nil || err2 != nil {
			t.Fatalf("queue mark %#x: nft match %s == %s does not parse", queue, nftMask, nftValue)
		}
		for _, p := range []struct {
			name string
			mark uint64
			want bool
			moot bool
		}{
			{"a packet b4 sends into one of its Telegram connections", q | inj | dial, true, false},
			{"a packet b4 sends into another connection", q | inj, true, false},
			{"b4's own lookup", q, true, false},
			{"b4's own Telegram connection", dial, false, cfg.TelegramSetMarkConflict()},
			{"another connection b4 opens itself", uint64(config.SelfDialMark), false, false},
			{"a KeeneticOS hotspot mark", 0x0ffffaaa, false, false},
			{"no mark", 0, false, false},
		} {
			if p.moot {
				continue
			}
			if got := markMatches(t, ipt, p.mark); got != p.want {
				t.Errorf("queue mark %#x, iptables %q: %s (%#x) returned as b4's own = %v, want %v", queue, ipt, p.name, p.mark, got, p.want)
			}
			if got := p.mark&mask == value; got != p.want {
				t.Errorf("queue mark %#x, nftables mark & %s == %s: %s (%#x) returned as b4's own = %v, want %v", queue, nftMask, nftValue, p.name, p.mark, got, p.want)
			}
		}
	}
}

func TestAFullMaskOwnPacketMarkIsWrittenTheWayIptablesListsIt(t *testing.T) {
	own := iptOwnPacketMark(0x77067fff)
	if own != "0x77067fff" {
		t.Fatalf("own-packet match %q, want %q: iptables lists a match whose mask is full without the mask", own, "0x77067fff")
	}
	listing := "-N B4\n-A B4 -m mark --mark 0x77067fff -j RETURN\n"
	if !iptListingHasMarkReturn(listing, iptChainName, own) {
		t.Fatal("the monitor does not find the return as iptables lists it, so it would restore the rules on every check")
	}
}

func TestCaptureChainLetsB4sOwnPacketsThroughFirst(t *testing.T) {
	for _, tc := range []struct {
		name string
		mark uint
		want string
	}{
		{"default mark", 0, "-m mark --mark 0x8000/0x88f98000 -j RETURN"},
		{"configured mark", 0x400000, "-m mark --mark 0x400000/0x88f98000 -j RETURN"},
		{"configured mark with a route bit", 0x8001, "-m mark --mark 0x8001/0x88f98001 -j RETURN"},
		{"configured mark holding every bit b4 adds", 0x77067fff, "-m mark --mark 0x77067fff -j RETURN"},
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
