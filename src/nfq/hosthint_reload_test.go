package nfq

import (
	"maps"
	"net"
	"slices"
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

func TestAReloadSetsAHintOfNoSetAsideAndLeavesItsNeighbourUsable(t *testing.T) {
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
	if got := hintedSet(w); got != "" {
		t.Errorf("no set lists either name, the hint names %q", got)
	}
	next := newSharedNameSet("two", "two.example")
	reload(&next)
	if got := hintedSet(w); got != "two" {
		t.Errorf("only the other name is listed again, the hint names %q, want two", got)
	}
	back, both := newSharedNameSet("one", "one.example"), newSharedNameSet("two", "two.example")
	reload(&back, &both)
	if got := hintedSet(w); got != "" {
		t.Errorf("both names are in a set again, two sets claim the address, got %q", got)
	}
}

func TestAHostHintComesBackWithItsSet(t *testing.T) {
	const host = "www.shared.example"
	set := func(id, domain string, enabled bool) *config.SetConfig {
		s := newSharedNameSet(id, domain)
		s.Enabled, s.TCP.SynFake = enabled, true
		return &s
	}
	listed := func() *config.SetConfig { return set("first", "shared.example", true) }
	for _, tc := range []struct {
		name  string
		steps [][]*config.SetConfig
		want  string
	}{
		{"the set is turned off and on", [][]*config.SetConfig{{set("first", "shared.example", false)}, {listed()}}, "first"},
		{"the name is removed and added again", [][]*config.SetConfig{{set("first", "else.example", true)}, {listed()}}, "first"},
		{"the set is deleted and restored", [][]*config.SetConfig{{}, {listed()}}, "first"},
		{"another change comes in between", [][]*config.SetConfig{
			{set("first", "shared.example", false)},
			{set("first", "shared.example", false), set("other", "other.example", true)},
			{listed(), set("other", "other.example", true)},
		}, "first"},
		{"another set takes the name", [][]*config.SetConfig{{set("first", "shared.example", false)}, {set("second", "shared.example", true)}}, "second"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			w, reload := hintReloadPool(t, listed())
			w.sock = &sock.Sender{}
			resolvesTo(w, host)
			key := hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}
			expires := w.hostHints.keys[key].candidates[0].expires
			for _, sets := range tc.steps[:len(tc.steps)-1] {
				reload(sets...)
				if got := hintedSet(w); got != "" {
					t.Errorf("no set lists the name, the hint names %q", got)
				}
				if v := w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02)); v != engine.VerdictAccept {
					t.Errorf("no set lists the name, the SYN got %v", v)
				}
			}
			reload(tc.steps[len(tc.steps)-1]...)
			if got := hintedSet(w); got != tc.want {
				t.Errorf("the hint names %q, want %q", got, tc.want)
			}
			if v := w.ProcessPacket(makeV4TCPPacketFlags(nil, 999, 0x02)); v != engine.VerdictDrop {
				t.Errorf("the SYN left without the fake SYN of the set, got %v", v)
			}
			if entry := w.hostHints.keys[key]; entry == nil || len(entry.candidates) != 1 || !entry.candidates[0].expires.Equal(expires) {
				t.Errorf("want one hint with its first lifetime, got %+v", entry)
			}
		})
	}
}

func TestTheHintOfABackupComesBackWithThePrimary(t *testing.T) {
	const host = "www.shared.example"
	for _, domainOnly := range []bool{false, true} {
		for name, without := range map[string]func(primary *config.SetConfig){
			"the primary is turned off and on":    func(primary *config.SetConfig) { primary.Enabled = false },
			"the name is removed and added again": func(primary *config.SetConfig) { primary.Targets.DomainsToMatch = []string{"else.example"} },
		} {
			if domainOnly {
				name += ", domain only"
			}
			t.Run(name, func(t *testing.T) {
				sets := func(listed bool) []*config.SetConfig {
					primary, backup := newSharedNameSet("primary", "shared.example"), newSharedNameSet("backup", "backup.example")
					primary.Targets.DomainOnly, primary.Escalate.To, backup.TCP.SynFake = domainOnly, "backup", true
					backup.DNS.Pins = map[string][]string{host: {"1.2.3.4"}}
					if !listed {
						without(&primary)
					}
					return []*config.SetConfig{&primary, &backup}
				}
				w, reload := hintReloadPool(t, sets(true)...)
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
				reload(sets(false)...)
				if got := storedHint(w, "10.0.0.1", "1.2.3.4"); got != "" {
					t.Errorf("no set lists the name, the hint names %q", got)
				}
				reload(sets(true)...)
				check("after the change")
				resolve()
				check("after the device resolved the name again")
			})
		}
	}
}

func TestAHintOfNoSetDoesNotOutliveItsLifetime(t *testing.T) {
	c := newHostHintCache()
	key := hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}
	c.Store(key.client, key.dest, "first", "www.shared.example")
	aside := func() *hostHintEntry {
		t.Helper()
		c.Rematch(nil, func(client, dest string, hint hostHintCandidate) string { return "" })
		entry := c.keys[key]
		if entry == nil || len(entry.candidates) != 1 {
			t.Fatalf("the hint of no set is not kept: %+v", entry)
		}
		if id, _, ok := c.Lookup(key.client, key.dest); ok {
			t.Fatalf("the hint of no set names %q", id)
		}
		return entry
	}
	aside()
	c.Store(key.client, key.dest, "first", "www.shared.example")
	if id, _, _ := c.Lookup(key.client, key.dest); id != "first" {
		t.Errorf("the set answered for the name again, the hint names %q", id)
	}
	aside().candidates[0].expires = time.Now().Add(-time.Second)
	c.Rematch(nil, func(client, dest string, hint hostHintCandidate) string { return "first" })
	if n := c.Len(); n != 0 {
		t.Errorf("the lifetime is over, %d hints are left", n)
	}
}

func TestAFullKeyLosesAHintOfNoSetFirst(t *testing.T) {
	c := newHostHintCache()
	c.Store("10.0.0.1", "1.2.3.4", "kept", "kept.example")
	for _, host := range []string{"a.example", "b.example", "c.example"} {
		c.Store("10.0.0.1", "1.2.3.4", "off", host)
	}
	c.Rematch(nil, func(client, dest string, hint hostHintCandidate) string {
		if hint.setId == "off" {
			return ""
		}
		return hint.setId
	})
	c.Store("10.0.0.1", "1.2.3.4", "kept", "new.example")
	if id, _, ok := c.Lookup("10.0.0.1", "1.2.3.4"); !ok || id != "kept" {
		t.Errorf("the set of two names is on, the hint names %q, %v", id, ok)
	}
	var hosts []string
	for _, hint := range c.keys[hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}].candidates {
		hosts = append(hosts, hint.host)
	}
	if want := []string{"kept.example", "b.example", "c.example", "new.example"}; !slices.Equal(hosts, want) {
		t.Errorf("the key holds %v, want %v", hosts, want)
	}
}

func TestAHintWithASetWinsOverOneWithoutItInTheSameSet(t *testing.T) {
	now := time.Now()
	aside := hostHintCandidate{setId: "merged", host: "www.shared.example", tlsVersion: 0x0304, expires: now.Add(90 * time.Second)}
	live := hostHintCandidate{setId: "other", host: "www.shared.example", expires: now.Add(30 * time.Second)}
	for name, order := range map[string][]hostHintCandidate{"first": {live, aside}, "last": {aside, live}} {
		t.Run(name, func(t *testing.T) {
			c := newHostHintCache()
			key := hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}
			c.keys[key] = &hostHintEntry{candidates: slices.Clone(order)}
			c.Rematch(nil, func(client, dest string, hint hostHintCandidate) string {
				if hint.tlsVersion == 0x0304 {
					return ""
				}
				return "merged"
			})
			if id, _, ok := c.Lookup(key.client, key.dest); !ok || id != "merged" {
				t.Errorf("the hint names %q, %v, want the set that lists the name", id, ok)
			}
		})
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
	c.Rematch(nil, func(client, dest string, hint hostHintCandidate) string {
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

func TestAHintRefreshedWhileItIsMatchedAgainGetsTheSetOfItsNewTLSVersion(t *testing.T) {
	const host = "www.shared.example"
	key, other := hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}, hostHintKey{client: "10.0.0.9", dest: "1.2.3.9"}
	for _, tc := range []struct {
		name   string
		only   []hostHintKey
		before uint16
		after  []uint16
		want   string
	}{
		{"by a QUIC ClientHello", nil, 0, []uint16{0x0304}, "tls13"},
		{"by a QUIC ClientHello, one address", []hostHintKey{key}, 0, []uint16{0x0304}, "tls13"},
		{"by a DNS answer", nil, 0x0304, []uint16{0}, "tls12"},
		{"by the same answer again", nil, 0, []uint16{0}, "tls12"},
		{"twice", nil, 0, []uint16{0x0304, 0}, "tls12"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newHostHintCache()
			c.StoreTLS(key.client, key.dest, "first", host, tc.before, 0)
			c.Store(other.client, other.dest, "first", host)
			matched, last := 0, tc.after[len(tc.after)-1]
			c.Rematch(tc.only, func(client, dest string, hint hostHintCandidate) string {
				if (hostHintKey{client, dest}) == other {
					matched++
				} else if len(tc.after) > 0 {
					c.StoreTLS(client, dest, "first", host, tc.after[0], 0)
					tc.after = tc.after[1:]
				}
				if hint.tlsVersion == 0x0304 {
					return "tls13"
				}
				return "tls12"
			})
			entry := c.keys[key]
			if entry == nil || len(entry.candidates) != 1 || entry.candidates[0].setId != tc.want || entry.candidates[0].tlsVersion != last {
				t.Errorf("want one hint of %s for version %#x, got %+v", tc.want, last, entry)
			}
			if len(tc.after) > 0 {
				t.Errorf("the hint was matched before it was refreshed %d more time(s)", len(tc.after))
			}
			if tc.only == nil && matched != 1 {
				t.Errorf("a hint that did not change was matched %d times", matched)
			}
		})
	}
}

func TestAHintRefreshedWhileItIsMatchedAgainLeavesItsNeighbourItsSet(t *testing.T) {
	c := newHostHintCache()
	key := hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}
	c.Store(key.client, key.dest, "old", "www.shared.example")
	c.Store(key.client, key.dest, "tls12", "www.shared.example")
	refreshed := false
	c.Rematch(nil, func(client, dest string, hint hostHintCandidate) string {
		if !refreshed {
			refreshed = true
			c.StoreTLS(client, dest, "tls12", "www.shared.example", 0x0304, 0)
		}
		if hint.tlsVersion == 0x0304 {
			return "tls13"
		}
		return "tls12"
	})
	got := map[uint16]string{}
	if entry := c.keys[key]; entry != nil {
		for _, hint := range entry.candidates {
			got[hint.tlsVersion] = hint.setId
		}
	}
	if want := (map[uint16]string{0: "tls12", 0x0304: "tls13"}); !maps.Equal(got, want) {
		t.Errorf("the hints are in %v, want %v", got, want)
	}
}

func TestAnAddressWhoseHintsKeepChangingIsListedOnce(t *testing.T) {
	c := newHostHintCache()
	hosts := []string{"a.example", "b.example", "c.example", "d.example"}
	store := func(version uint16) {
		for _, host := range hosts {
			c.StoreTLS("10.0.0.1", "1.2.3.4", "first", host, version, 0)
		}
	}
	store(0)
	var only []hostHintKey
	for pass := range uint16(6) {
		only = c.rematchOnce(only, func(client, dest string, hint hostHintCandidate) string {
			store(pass%2*0x0304 + 0x0303)
			return "first"
		})
		if len(only) != 1 {
			t.Fatalf("pass %d lists the address %d time(s)", pass, len(only))
		}
	}
	if only = c.rematchOnce(only, func(client, dest string, hint hostHintCandidate) string { return "second" }); len(only) != 0 {
		t.Errorf("nothing changed, %d address(es) are matched again", len(only))
	}
	if id, _, _ := c.Lookup("10.0.0.1", "1.2.3.4"); id != "second" {
		t.Errorf("the hint names %q, want second", id)
	}
}

func TestAnAnswerThatListsAnAddressTwiceKeepsTheHintsOfBothTLSVersions(t *testing.T) {
	sets := func(tls12, tls13 string) []*config.SetConfig {
		a, b := newSharedNameSet(tls12, "shared.example"), newSharedNameSet(tls13, "shared.example")
		a.Targets.TLSVersion, b.Targets.TLSVersion = "1.2", "1.3"
		return []*config.SetConfig{&a, &b}
	}
	old := sets("a", "b")
	w, reload := hintReloadPool(t, old...)
	asked := w.getMatcher()
	reload(sets("b", "c")...)
	w.storeHostHint("10.0.0.1", "1.2.3.4", old[0], "www.shared.example", "dns", 0, 0)
	w.storeHostHint("10.0.0.1", "1.2.3.4", old[1], "www.shared.example", "quic", 0x0304, IPv4)
	twice := []net.IP{net.ParseIP("1.2.3.4"), net.ParseIP("1.2.3.4").To4()}
	if keys := hostHintKeys(net.ParseIP("10.0.0.1"), twice); len(keys) != 1 {
		t.Errorf("one address listed twice gives %d keys", len(keys))
	}
	w.settleHostHints(asked, net.ParseIP("10.0.0.1"), twice)
	got := map[uint16]string{}
	if entry := w.hostHints.keys[hostHintKey{client: "10.0.0.1", dest: "1.2.3.4"}]; entry != nil {
		for _, hint := range entry.candidates {
			got[hint.tlsVersion] = hint.setId
		}
	}
	if want := (map[uint16]string{0: "b", 0x0304: "c"}); !maps.Equal(got, want) {
		t.Errorf("the hints are in %v, want %v", got, want)
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
			c.Rematch(nil, func(client, dest string, hint hostHintCandidate) string { return "merged" })
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
	c.Rematch(nil, func(client, dest string, hint hostHintCandidate) string {
		if hint.tlsVersion == 0x0304 {
			return "new13"
		}
		return "new12"
	})
	if id, _, ok := c.Lookup("10.0.0.1", "1.2.3.4"); ok {
		t.Errorf("two sets claim the address, got %q", id)
	}
}
