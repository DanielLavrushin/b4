package nfq

import (
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
)

func newSharedNameSet(id string, domains ...string) config.SetConfig {
	set := newHintSet()
	set.Id, set.Name = id, id
	set.Targets.DomainsToMatch = domains
	return set
}

func hintReloadPool(t *testing.T, sets ...*config.SetConfig) (*Worker, func(sets ...*config.SetConfig)) {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Queue.IsDiscovery = true
	cfg.Sets = sets
	w := newTestWorker(t, &cfg)
	pool := &Pool{Workers: []*Worker{w}, state: &runtimeState{hostHints: w.hostHints, destState: w.destState, tlsCache: w.tlsCache}}
	reload := func(sets ...*config.SetConfig) {
		t.Helper()
		next := cfg.Clone()
		next.Sets = sets
		if err := pool.UpdateConfig(next); err != nil {
			t.Fatal(err)
		}
	}
	return w, reload
}

func resolvesTo(w *Worker, host string) {
	resp := buildDNSResponse(0x1234, host, []net.IP{net.ParseIP("1.2.3.4")})
	w.ProcessPacket(makeV4UDPPacket(resp, net.ParseIP("8.8.8.8"), net.ParseIP("10.0.0.1"), 53, 5353))
}

func storedHint(w *Worker, client, dest string) string {
	id, _, _ := w.hostHints.Lookup(client, dest)
	return id
}

func hintedSet(w *Worker) string {
	set, _, _, _ := w.lookupHostHint(w.getConfig(), "10.0.0.1", "1.2.3.4", w.getMacByIp("10.0.0.1"))
	if set == nil {
		return ""
	}
	return set.Id
}

func TestAHostHintFollowsTheSetsThatListItsNameNow(t *testing.T) {
	first := func() *config.SetConfig {
		s := newSharedNameSet("first", "shared.example")
		return &s
	}
	second := func() *config.SetConfig {
		s := newSharedNameSet("second", "shared.example")
		return &s
	}
	for _, tc := range []struct {
		name   string
		reload func() []*config.SetConfig
		want   string
	}{
		{"after the other set is moved above", func() []*config.SetConfig { return []*config.SetConfig{second(), first()} }, "second"},
		{"after its set is turned off", func() []*config.SetConfig {
			off := first()
			off.Enabled = false
			return []*config.SetConfig{off, second()}
		}, "second"},
		{"after its set is deleted", func() []*config.SetConfig { return []*config.SetConfig{second()} }, "second"},
		{"after the name left its set", func() []*config.SetConfig {
			emptied := first()
			emptied.Targets.DomainsToMatch = []string{"else.example"}
			return []*config.SetConfig{emptied, second()}
		}, "second"},
		{"after a more specific set was added", func() []*config.SetConfig {
			exact := newSharedNameSet("exact", "www.shared.example")
			return []*config.SetConfig{first(), second(), &exact}
		}, "exact"},
		{"after a set for this device was added", func() []*config.SetConfig {
			mine := newSharedNameSet("mine", "shared.example")
			mine.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:01"}
			return []*config.SetConfig{first(), second(), &mine}
		}, "mine"},
		{"after a set for another device was added", func() []*config.SetConfig {
			other := newSharedNameSet("other", "shared.example")
			other.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:02"}
			return []*config.SetConfig{&other, first(), second()}
		}, "first"},
		{"after the name left every set", func() []*config.SetConfig {
			a, b := first(), second()
			a.Targets.DomainsToMatch, b.Targets.DomainsToMatch = []string{"a.example"}, []string{"b.example"}
			return []*config.SetConfig{a, b}
		}, ""},
		{"after an unrelated set changed", func() []*config.SetConfig {
			unrelated := newSharedNameSet("unrelated", "unrelated.example")
			return []*config.SetConfig{first(), second(), &unrelated}
		}, "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, reload := hintReloadPool(t, first(), second())
			w.ipToMac.Store(map[string]string{"10.0.0.1": "aa:bb:cc:dd:ee:01"})
			resolvesTo(w, "www.shared.example")
			if got := hintedSet(w); got != "first" {
				t.Fatalf("before the change the hint names %q, want the set listed first", got)
			}

			reload(tc.reload()...)
			if got := hintedSet(w); got != tc.want {
				t.Errorf("the hint names %q, want %q", got, tc.want)
			}
			if tc.want == "" {
				return
			}
			resolvesTo(w, "www.shared.example")
			if got := hintedSet(w); got != tc.want {
				t.Errorf("after the device resolved the name again the hint names %q, want %q", got, tc.want)
			}
		})
	}
}

func TestAReloadKeepsTheHintsOfSeveralNamesApart(t *testing.T) {
	one := newSharedNameSet("one", "one.example")
	two := newSharedNameSet("two", "two.example")
	w, reload := hintReloadPool(t, &one, &two)
	resolvesTo(w, "www.one.example")
	resolvesTo(w, "www.two.example")
	if got := hintedSet(w); got != "" {
		t.Fatalf("two names on one address are ambiguous, got %q", got)
	}

	merged := newSharedNameSet("one", "one.example", "two.example")
	reload(&merged)
	if got := hintedSet(w); got != "one" {
		t.Errorf("one set lists both names now, got %q", got)
	}

	again, split := newSharedNameSet("one", "one.example"), newSharedNameSet("two", "two.example")
	reload(&again, &split)
	if got := hintedSet(w); got != "" {
		t.Errorf("the names are in two sets again, got %q", got)
	}
}

func TestAReloadDoesNotExtendAHostHint(t *testing.T) {
	set := newSharedNameSet("first", "shared.example")
	w, reload := hintReloadPool(t, &set)
	resolvesTo(w, "www.shared.example")
	w.hostHints.mu.Lock()
	expires := w.hostHints.keys[hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}].candidates[0].expires
	w.hostHints.mu.Unlock()

	moved := newSharedNameSet("second", "shared.example")
	reload(&moved)
	w.hostHints.mu.Lock()
	defer w.hostHints.mu.Unlock()
	entry := w.hostHints.keys[hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}]
	if entry == nil || len(entry.candidates) != 1 || entry.candidates[0].setId != "second" || !entry.candidates[0].expires.Equal(expires) {
		t.Fatalf("want one hint for the new set with its first lifetime, got %+v", entry)
	}
}

func TestAReloadKeepsTheHintOfTheBackupThatAnswers(t *testing.T) {
	const host = "www.shared.example"
	sets := func(domainOnly, pinned bool, extra ...*config.SetConfig) []*config.SetConfig {
		primary, backup := newSharedNameSet("primary", "shared.example"), newSharedNameSet("backup", "backup.example")
		primary.Targets.DomainOnly, primary.Escalate.To = domainOnly, "backup"
		backup.TCP.SynFake = true
		if pinned {
			backup.DNS.Pins = map[string][]string{host: {"1.2.3.4"}}
		}
		return append([]*config.SetConfig{&primary, &backup}, extra...)
	}
	for name, domainOnly := range map[string]bool{"the primary learns addresses": false, "the primary is domain only": true} {
		t.Run(name, func(t *testing.T) {
			w, reload := hintReloadPool(t, sets(domainOnly, true)...)
			w.sock = &sock.Sender{}
			w.destState.SetEscalation(host, "backup", escalateReasonStall, time.Minute)
			resolve := func() {
				t.Helper()
				w.ProcessPacket(makeV4UDPPacket(dns.BuildQuery(host, 7, dnsTypeA), net.ParseIP("10.0.0.1"), net.ParseIP("8.8.8.8"), 5353, 53))
			}
			check := func(when string) {
				t.Helper()
				if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "backup" {
					t.Errorf("%s: the hint names %q, the name is switched to the backup", when, got)
				}
				if v := w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02)); v != engine.VerdictDrop {
					t.Errorf("%s: the SYN left without the backup's fake SYN, got %v", when, v)
				}
			}
			resolve()
			check("before the change")
			extra := newSharedNameSet("extra", "extra.example")
			reload(sets(domainOnly, true, &extra)...)
			check("after an unrelated set was added")
			resolve()
			check("after the device resolved the name again")
		})
	}

	t.Run("not past the escalation", func(t *testing.T) {
		w, reload := hintReloadPool(t, sets(false, true)...)
		w.destState.SetEscalation(host, "backup", escalateReasonStall, time.Minute)
		w.storeHostHint("10.0.0.1", "1.2.3.4", w.getConfig().GetSetById("backup"), host, "dns", 0, 0)
		w.destState.ClearEscalation(host)
		extra := newSharedNameSet("extra", "extra.example")
		reload(sets(false, true, &extra)...)
		if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "primary" {
			t.Errorf("the name is no longer switched, the hint names %q", got)
		}
	})

	t.Run("not a hint of the primary", func(t *testing.T) {
		w, reload := hintReloadPool(t, sets(false, false)...)
		resolvesTo(w, host)
		w.destState.SetEscalation(host, "backup", escalateReasonStall, time.Minute)
		extra := newSharedNameSet("extra", "extra.example")
		reload(sets(false, false, &extra)...)
		if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "primary" {
			t.Errorf("the answer came from the primary, the hint names %q", got)
		}
		resolvesTo(w, host)
		if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "primary" {
			t.Errorf("the device resolved the name again, the hint names %q", got)
		}
	})

	t.Run("not a backup that was turned off", func(t *testing.T) {
		w, reload := hintReloadPool(t, sets(false, true)...)
		w.destState.SetEscalation(host, "backup", escalateReasonStall, time.Minute)
		w.storeHostHint("10.0.0.1", "1.2.3.4", w.getConfig().GetSetById("backup"), host, "dns", 0, 0)
		off := sets(false, true)
		off[1].Enabled = false
		reload(off...)
		if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "primary" {
			t.Errorf("the backup is off, the hint names %q", got)
		}
	})
}

func TestAReloadMovesTheHintOfABackupThatNowHandlesOnlyCalls(t *testing.T) {
	const host = "www.shared.example"
	sets := func(calls bool) []*config.SetConfig {
		primary, backup := newSharedNameSet("primary", "shared.example"), newSharedNameSet("backup")
		primary.Escalate.To = "backup"
		backup.DNS.Pins = map[string][]string{host: {"1.2.3.4"}}
		backup.TCP.SynFake = true
		if calls {
			primary.Escalate.To = ""
			backup.UDP.FilterL7, backup.UDP.DPortFilter = []string{config.UDPL7STUN}, "50000-50100"
		}
		return []*config.SetConfig{&primary, &backup}
	}
	w, reload := hintReloadPool(t, sets(false)...)
	w.sock = &sock.Sender{}
	w.destState.SetEscalation(host, "backup", escalateReasonStall, time.Minute)
	w.ProcessPacket(makeV4UDPPacket(dns.BuildQuery(host, 7, dnsTypeA), net.ParseIP("10.0.0.1"), net.ParseIP("8.8.8.8"), 5353, 53))
	if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "backup" {
		t.Fatalf("before the change the hint names %q, the backup answered", got)
	}
	reload(sets(true)...)
	if _, _, live := w.destState.GetEscalation(host); live {
		t.Fatal("a set that handles only calls is no backup, the escalation is over")
	}
	if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "primary" {
		t.Errorf("the name is no longer switched, the hint names %q", got)
	}
	if v := w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02)); v != engine.VerdictAccept {
		t.Errorf("the SYN left with the fake SYN of a set that handles only calls, got %v", v)
	}
}

func TestAReloadKeepsTheHintsOfTwoDevicesApart(t *testing.T) {
	const phone, laptop = "aa:bb:cc:dd:ee:01", "aa:bb:cc:dd:ee:02"
	first := newSharedNameSet("first", "shared.example")
	w, reload := hintReloadPool(t, &first)
	w.ipToMac.Store(map[string]string{"10.0.0.1": phone, "10.0.0.2": laptop})
	for _, client := range []string{"10.0.0.1", "10.0.0.2"} {
		w.storeHostHint(client, "1.2.3.4", &first, "www.shared.example", "dns", 0, 0)
	}
	again, mine := newSharedNameSet("first", "shared.example"), newSharedNameSet("mine", "shared.example")
	mine.Targets.SourceDevices = []string{phone}
	reload(&again, &mine)
	if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "mine" {
		t.Errorf("the phone's hint names %q, want mine", got)
	}
	if got := storedHint(w, "10.0.0.2", "1.2.3.4"); got != "first" {
		t.Errorf("the laptop's hint names %q, want first", got)
	}
}

func TestAReloadMatchesAHintWithItsTLSVersion(t *testing.T) {
	first := newSharedNameSet("first", "shared.example")
	w, reload := hintReloadPool(t, &first)
	w.storeHostHint("10.0.0.1", "1.2.3.4", &first, "www.shared.example", "quic", 0x0304, IPv4)
	w.storeHostHint("10.0.0.1", "1.2.3.5", &first, "www.shared.example", "dns", 0, 0)
	tls12, tls13 := newSharedNameSet("tls12", "shared.example"), newSharedNameSet("tls13", "shared.example")
	tls12.Targets.TLSVersion, tls13.Targets.TLSVersion = "1.2", "1.3"
	reload(&tls12, &tls13)
	if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "tls13" {
		t.Errorf("the hint of a QUIC ClientHello names %q, want tls13", got)
	}
	if got := storedHint(w, "10.0.0.1", "1.2.3.5"); got != "tls12" {
		t.Errorf("the hint of a DNS answer names %q, want tls12, the first set of the name", got)
	}
}

func TestAReloadMatchesAHintWithTheFamilyOfItsAddress(t *testing.T) {
	first := familySet("first", "")
	w, reload := hintReloadPool(t, first)
	w.storeHostHint(familyDevice, familyV4Addr, first, familyName, "dns", 0, 0)
	w.storeHostHint(familyDevice, familyV6Addr, first, familyName, "dns", 0, 0)
	reload(familySet("v6", "6"), familySet("v4", "4"))
	if got := familyHint(w, familyV4Addr); got != "v4" {
		t.Errorf("the hint for %s names %q, want v4", familyV4Addr, got)
	}
	if got := familyHint(w, familyV6Addr); got != "v6" {
		t.Errorf("the hint for %s names %q, want v6", familyV6Addr, got)
	}
}

func TestAReloadGivesAHintToAScopedBlockSetOnlyOnItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
	first := newSharedNameSet("first", "shared.example")
	w, reload := hintReloadPool(t, &first)
	for _, client := range []string{scopeLAN, scopeGuest} {
		w.storeHostHint(client, "1.2.3.4", &first, "www.shared.example", "dns", 0, 0)
	}
	again, block := newSharedNameSet("first", "shared.example"), newSharedNameSet("block", "shared.example")
	reload(scopeToGuest(&block, config.BlockActionDrop), &again)
	if got := storedHint(w, scopeLAN, "1.2.3.4"); got != "first" {
		t.Errorf("a client on br0: the hint names %q, want first", got)
	}
	if got := storedHint(w, scopeGuest, "1.2.3.4"); got != "block" {
		t.Errorf("a client on br-guest: the hint names %q, want block", got)
	}
}

func TestAReloadDropsAHintOfNoSetAndLeavesItsNeighbourUsable(t *testing.T) {
	one, two := newSharedNameSet("one", "one.example"), newSharedNameSet("two", "two.example")
	w, reload := hintReloadPool(t, &one, &two)
	resolvesTo(w, "www.one.example")
	resolvesTo(w, "www.two.example")
	again := newSharedNameSet("one", "one.example")
	reload(&again)
	if got := hintedSet(w); got != "one" {
		t.Errorf("only one name is still listed, the hint names %q, want one", got)
	}
	other := newSharedNameSet("other", "other.example")
	reload(&other)
	if n := w.hostHints.Len(); n != 0 {
		t.Errorf("no set lists either name, %d hints are left", n)
	}
}

func TestASettingsOnlyReloadLeavesEveryHintAlone(t *testing.T) {
	set := newSharedNameSet("first", "shared.example")
	w, reload := hintReloadPool(t, &set)
	w.storeHostHint("10.0.0.1", "1.2.3.4", &set, "www.elsewhere.example", "dns", 0, 0)
	same := newSharedNameSet("first", "shared.example")
	reload(&same)
	if got := hintedSet(w); got != "first" {
		t.Errorf("no set changed, the hint names %q, want first", got)
	}
}

func TestAHintStoredWhileOthersAreMatchedAgainStays(t *testing.T) {
	c := newHostHintCache()
	c.Store("10.0.0.1", "1.2.3.4", "first", "www.shared.example")
	c.Rematch(func(client, dest string, hint hostHintCandidate) string {
		c.Store("10.0.0.9", "1.2.3.9", "fresh", "www.fresh.example")
		return "second"
	})
	if id, _, ok := c.Lookup("10.0.0.9", "1.2.3.9"); !ok || id != "fresh" {
		t.Errorf("the hint stored meanwhile: %q, %v", id, ok)
	}
	if id, _, _ := c.Lookup("10.0.0.1", "1.2.3.4"); id != "second" {
		t.Errorf("the matched hint names %q", id)
	}
}

func TestTwoHintsThatMeetInOneSetKeepTheLongerLife(t *testing.T) {
	now := time.Now()
	for name, lives := range map[string][2]time.Duration{
		"the longer one first": {90 * time.Second, 30 * time.Second},
		"the longer one last":  {30 * time.Second, 90 * time.Second},
	} {
		t.Run(name, func(t *testing.T) {
			c := newHostHintCache()
			key := hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}
			c.keys[key] = &hostHintEntry{candidates: []hostHintCandidate{
				{setId: "first", host: "www.shared.example", expires: now.Add(lives[0])},
				{setId: "second", host: "www.shared.example", expires: now.Add(lives[1])},
			}}
			c.Rematch(func(client, dest string, hint hostHintCandidate) string { return "merged" })
			got := c.keys[key].candidates
			if len(got) != 1 || got[0].setId != "merged" || !got[0].expires.Equal(now.Add(90*time.Second)) {
				t.Errorf("want one hint with the longer life, got %+v", got)
			}
		})
	}
}

func TestTwoHintsOfOneNameInTwoSetsStayAmbiguous(t *testing.T) {
	c := newHostHintCache()
	c.StoreTLS("10.0.0.1", "1.2.3.4", "tls12", "www.shared.example", 0, 0)
	c.StoreTLS("10.0.0.1", "1.2.3.4", "tls13", "www.shared.example", 0x0304, IPv4)
	c.Rematch(func(client, dest string, hint hostHintCandidate) string {
		if hint.tlsVersion == 0x0304 {
			return "new13"
		}
		return "new12"
	})
	if id, _, ok := c.Lookup("10.0.0.1", "1.2.3.4"); ok {
		t.Errorf("two sets claim the address, got %q", id)
	}
}
