package nfq

import (
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
	"github.com/florianl/go-nfqueue"
)

const pinnedConnKey = "10.0.0.1:12345->1.2.3.4:443"

func storeCollectedView(w *Worker, ips []string) *config.Config {
	view := w.getConfig()
	if ips != nil {
		view = view.WithCollectedIPs(ips)
		w.cfg.Store(view)
	}
	return view
}

func publishCollectedMatcher(t *testing.T, w *Worker, ips []string) {
	t.Helper()
	next := buildMatcher(storeCollectedView(w, ips))
	next.TransferLearnedIPs(w.getMatcher())
	next.KeepConnectionPolicy(w.getMatcher())
	w.tlsCache.PublishCollected(next)
	w.matcher.Store(next)
	if d, ok := w.tlsCache.Decision(pinnedConnKey); !ok || !d.collectedPinned || !w.tlsCache.HasPinned() {
		t.Fatalf("setup: the publish did not pin the connection's decision (decided=%v pinned=%v), so this test proves nothing", ok, d.collectedPinned)
	}
}

func pinnedHello(host string) []byte {
	return makeV4TCPPacket(buildClientHello(host, 16, 0xAB), 1000)
}

func saveSets(w *Worker, edit func(sets []*config.SetConfig)) {
	current := w.getConfig()
	n := *current
	next := &n
	next.Sets = make([]*config.SetConfig, len(current.Sets))
	for i, set := range current.Sets {
		copySet := *set
		next.Sets[i] = &copySet
	}
	if edit != nil {
		edit(next.Sets)
	}
	matcher := buildMatcher(next)
	matcher.TransferLearnedIPs(w.getMatcher())
	w.cfg.Store(next)
	w.matcher.Store(matcher)
	w.tlsCache.ForgetDecisions(matcher)
}

func onPort(set *config.SetConfig, ports string) {
	set.TCP.DPortFilter = ports
	set.TCPPortRanges = config.ParsePortRanges(ports)
}

func TestATelegramSetConnectionKeepsItsSetAfterACollectedPublish(t *testing.T) {
	mark := config.TelegramSetDialMark
	telegram := newTelegramSet()
	w := newInjectingWorker(t, &telegram)
	w.getConfig().System.MTProto.UpstreamSet = telegram.Id
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stubInjectRoute(t, idx["eth7"], true)
	if v := queuedVerdict(t, w, pinnedHello("kws2.web.telegram.org"), &mark, idx["eth7"], 0); v != nfqueue.NfDrop {
		t.Fatalf("setup: the first ClientHello got verdict %d, want the Telegram set's strategy", v)
	}
	if d, ok := w.tlsCache.Decision(pinnedConnKey); !ok || d.set != nil {
		t.Fatalf("setup: want a decision that names no set, the target-less Telegram set's usual case; decided=%v", ok)
	}
	publishCollectedMatcher(t, w, nil)
	if v := queuedVerdict(t, w, pinnedHello("kws2.web.telegram.org"), &mark, idx["eth7"], 0); v != nfqueue.NfDrop {
		t.Errorf("a resent ClientHello of b4's own Telegram connection got verdict %d after a collected publish, want the Telegram set's strategy; the mark picks that set whatever the addresses, so a publish has nothing to re-match", v)
	}
}

func TestAConnectionWhoseHelloNamesNoSetKeepsItsSetAfterACollectedPublish(t *testing.T) {
	const unlisted = "unlisted.example.net"
	for _, c := range []struct {
		name      string
		edit      func(*config.SetConfig)
		collected []string
		learn     bool
		addLater  bool
		want      int
	}{
		{"an address the set lists", func(s *config.SetConfig) { s.Targets.IpsToMatch = []string{"1.2.3.4"} }, nil, false, false, nfqueue.NfDrop},
		{"an address the set lists with IP priority", func(s *config.SetConfig) {
			s.Targets.IpsToMatch = []string{"1.2.3.4"}
			s.Targets.IPPriority = true
		}, nil, false, false, nfqueue.NfDrop},
		{"a collected address of a consumer", func(s *config.SetConfig) { s.Targets.UseCollectedIPs = true }, []string{"1.2.3.4"}, false, false, nfqueue.NfDrop},
		{"a port the set matches without destination targets", func(s *config.SetConfig) {
			s.TCP.DPortFilter = "443"
			s.TCPPortRanges = config.ParsePortRanges(s.TCP.DPortFilter)
		}, nil, false, false, nfqueue.NfDrop},
		{"an address the set learned from an earlier ClientHello", func(s *config.SetConfig) { s.Targets.DomainsToMatch = []string{"ytimg.com"} }, nil, true, false, nfqueue.NfDrop},
		{"no set, then a consumer collects the address", func(s *config.SetConfig) { s.Targets.UseCollectedIPs = true }, []string{}, false, true, nfqueue.NfAccept},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newFragmentingYTImagesSet()
			set.Id, set.Name = "plain", "plain"
			set.Targets.DomainOnly = false
			set.Targets.DomainsToMatch = nil
			c.edit(&set)
			w := newInjectingWorker(t, &set)
			if c.collected != nil {
				w.matcher.Store(buildMatcher(storeCollectedView(w, c.collected)))
			}
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stubInjectRoute(t, idx["eth7"], true)
			if c.learn {
				queuedVerdict(t, w, makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, 12346, 0x18), nil, idx["eth7"], 0)
			}
			if v := queuedVerdict(t, w, pinnedHello(unlisted), nil, idx["eth7"], 0); v != c.want {
				t.Fatalf("setup: the first ClientHello got verdict %d, want %d", v, c.want)
			}
			if d, ok := w.tlsCache.Decision(pinnedConnKey); !ok || d.set != nil {
				t.Fatalf("setup: want a decision that names no set; decided=%v", ok)
			}
			ips := c.collected
			if c.addLater {
				ips = []string{"1.2.3.4"}
			}
			publishCollectedMatcher(t, w, ips)
			if v := queuedVerdict(t, w, pinnedHello(unlisted), nil, idx["eth7"], 0); v != c.want {
				t.Errorf("%s: a resent ClientHello naming no set got verdict %d after a collected publish, want %d as before it; a publish pins an open connection to what it had, neither dropping its set nor handing it to a set that took its address since", c.name, v, c.want)
			}
		})
	}
}

func TestAConnectionKeepsItsSetAcrossASaveAndALaterCollectedPublish(t *testing.T) {
	const unlisted = "unlisted.example.net"
	for _, c := range []struct {
		name      string
		edit      func(*config.SetConfig)
		collected []string
		learn     bool
		addLater  bool
		save      func(sets []*config.SetConfig)
		want      int
	}{
		{"an address the set lists", func(s *config.SetConfig) { s.Targets.IpsToMatch = []string{"1.2.3.4"} }, nil, false, false, nil, nfqueue.NfDrop},
		{"a collected address of a consumer", func(s *config.SetConfig) { s.Targets.UseCollectedIPs = true }, []string{"1.2.3.4"}, false, false, nil, nfqueue.NfDrop},
		{"a port the set matches without destination targets", func(s *config.SetConfig) { onPort(s, "443") }, nil, false, false, nil, nfqueue.NfDrop},
		{"an address the set learned from an earlier ClientHello", func(s *config.SetConfig) { s.Targets.DomainsToMatch = []string{"ytimg.com"} }, nil, true, false, nil, nfqueue.NfDrop},
		{"no set, then a consumer collects the address", func(s *config.SetConfig) { s.Targets.UseCollectedIPs = true }, []string{}, false, true, nil, nfqueue.NfAccept},
		{"a port the save moves to another set", func(s *config.SetConfig) { onPort(s, "443") }, nil, false, false, func(sets []*config.SetConfig) {
			onPort(sets[0], "8443")
			onPort(sets[1], "443")
		}, nfqueue.NfDrop},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newFragmentingYTImagesSet()
			set.Id, set.Name = "plain", "plain"
			set.Targets.DomainOnly = false
			set.Targets.DomainsToMatch = nil
			c.edit(&set)
			other := newFragmentingYTImagesSet()
			other.Id, other.Name = "other", "other"
			other.Targets.DomainOnly = false
			other.Targets.DomainsToMatch = nil
			onPort(&other, "8443")
			w := newInjectingWorker(t, &set, &other)
			if c.collected != nil {
				w.matcher.Store(buildMatcher(storeCollectedView(w, c.collected)))
			}
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stubInjectRoute(t, idx["eth7"], true)
			if c.learn {
				queuedVerdict(t, w, makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, 12346, 0x18), nil, idx["eth7"], 0)
			}
			hello := buildClientHello(unlisted, 16, 0xAB)
			if v := queuedVerdict(t, w, pinnedHello(unlisted), nil, idx["eth7"], 0); v != c.want {
				t.Fatalf("setup: the first ClientHello got verdict %d, want %d", v, c.want)
			}
			if d, ok := w.tlsCache.Decision(pinnedConnKey); !ok || d.set != nil {
				t.Fatalf("setup: want a decision that names no set; decided=%v", ok)
			}
			saveSets(w, c.save)
			data := makeV4TCPPacket(tlsAppData(100), 1000+uint32(len(hello)))
			if v := queuedVerdict(t, w, data, nil, idx["eth7"], 0); v != c.want {
				t.Fatalf("setup: the first data packet after the save got verdict %d, want %d, so the save alone already changed what the connection gets", v, c.want)
			}
			ips := c.collected
			if c.addLater {
				ips = []string{"1.2.3.4"}
			}
			publishCollectedMatcher(t, w, ips)
			if v := queuedVerdict(t, w, pinnedHello(unlisted), nil, idx["eth7"], 0); v != c.want {
				d, _ := w.tlsCache.Decision(pinnedConnKey)
				t.Errorf("%s: a resent ClientHello naming no set got verdict %d after a save, a data packet and a collected publish, want %d as on its data packet before the publish (recorded set %q); a save must not leave the publish pinning the connection to no set", c.name, v, c.want, d.priorSet)
			}
		})
	}
}

func TestALearnedSetThatTakesAListedAddressKeepsTheConnectionAcrossASaveAndAPublish(t *testing.T) {
	const unlisted = "unlisted.example.net"
	for _, save := range []bool{false, true} {
		name := "without a save"
		if save {
			name = "with a save"
		}
		t.Run(name, func(t *testing.T) {
			listing := newPassiveSet()
			listing.Id, listing.Name = "listing", "listing"
			listing.Targets.DomainOnly = false
			listing.Targets.DomainsToMatch = nil
			listing.Targets.IpsToMatch = []string{"1.2.3.4"}
			learner := newFragmentingYTImagesSet()
			learner.Id, learner.Name = "learner", "learner"
			learner.Targets.DomainOnly = false
			w := newInjectingWorker(t, &listing, &learner)
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stubInjectRoute(t, idx["eth7"], true)
			queuedVerdict(t, w, makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, 12346, 0x18), nil, idx["eth7"], 0)
			hello := buildClientHello(unlisted, 16, 0xAB)
			if v := queuedVerdict(t, w, pinnedHello(unlisted), nil, idx["eth7"], 0); v != nfqueue.NfDrop {
				t.Fatalf("setup: the first ClientHello got verdict %d, want the learned set's drop", v)
			}
			if save {
				saveSets(w, nil)
			}
			data := makeV4TCPPacket(tlsAppData(100), 1000+uint32(len(hello)))
			if v := queuedVerdict(t, w, data, nil, idx["eth7"], 0); v != nfqueue.NfDrop {
				t.Fatalf("setup: the data packet got verdict %d, want the learned set's drop", v)
			}
			publishCollectedMatcher(t, w, nil)
			if v := queuedVerdict(t, w, pinnedHello(unlisted), nil, idx["eth7"], 0); v != nfqueue.NfDrop {
				d, _ := w.tlsCache.Decision(pinnedConnKey)
				t.Errorf("%s: a resent ClientHello got verdict %d after a collected publish, want the learned set's drop as before it (recorded set %q); the set that lists the address without IP priority never had the connection", name, v, d.priorSet)
			}
		})
	}
}

func TestARecordedSetSurvivesAPublishBetweenTheDecisionAndTheRecord(t *testing.T) {
	set := newFragmentingYTImagesSet()
	set.Id, set.Name = "plain", "plain"
	set.Targets.DomainOnly = false
	set.Targets.DomainsToMatch = nil
	onPort(&set, "443")
	for _, c := range []struct {
		name    string
		between func(w *Worker, old *sni.SuffixSet, gen uint64)
		pinned  bool
		want    string
	}{
		{"a collected publish", func(w *Worker, old *sni.SuffixSet, _ uint64) {
			next := buildMatcher(w.getConfig())
			next.KeepConnectionPolicy(old)
			w.tlsCache.PublishCollected(next)
			w.matcher.Store(next)
		}, true, "plain"},
		{"a save", func(w *Worker, _ *sni.SuffixSet, _ uint64) {
			w.tlsCache.ForgetDecisions(buildMatcher(w.getConfig()))
		}, false, ""},
		{"a save and a decision under its sets", func(w *Worker, _ *sni.SuffixSet, gen uint64) {
			next := buildMatcher(w.getConfig())
			w.tlsCache.ForgetDecisions(next)
			w.tlsCache.DecideHeld(pinnedConnKey, gen, next, "", nil, false, nil)
		}, false, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newInjectingWorker(t, &set)
			old := w.getMatcher()
			w.tlsCache.ForgetDecisions(old)
			gen := w.tlsCache.Store(pinnedConnKey, "unlisted.example.net", 0x0303, 1000, 7, 100)
			w.tlsCache.DecideHeld(pinnedConnKey, gen, old, "", nil, false, nil)
			c.between(w, old, gen)
			w.tlsCache.RecordPrior(pinnedConnKey, gen, old, &set)
			d, ok := w.tlsCache.Decision(pinnedConnKey)
			if !ok {
				t.Fatal("setup: the connection has no decision")
			}
			if d.collectedPinned != c.pinned || d.priorSet != c.want {
				t.Errorf("%s between the re-decision and its record: pinned=%v recorded set %q, want pinned=%v recorded set %q", c.name, d.collectedPinned, d.priorSet, c.pinned, c.want)
			}
		})
	}
}

func TestAnEscalatedConnectionKeepsItsEscalationAcrossACollectedPublish(t *testing.T) {
	const unlisted = "unlisted.example.net"
	for _, c := range []struct {
		name          string
		hello         string
		learn         bool
		save          bool
		escalateFirst bool
		escalateLater bool
		consumer      bool
		wantFirst     int
		want          int
	}{
		{"a set its ClientHello named", "i.ytimg.com", false, false, true, false, false, nfqueue.NfDrop, nfqueue.NfDrop},
		{"a learned address, its ClientHello naming no set", unlisted, true, false, true, false, false, nfqueue.NfDrop, nfqueue.NfDrop},
		{"a learned address, its ClientHello naming no set, after a save", unlisted, true, true, true, false, false, nfqueue.NfDrop, nfqueue.NfDrop},
		{"an escalation recorded after the publish", "i.ytimg.com", false, false, false, true, false, nfqueue.NfAccept, nfqueue.NfDrop},
		{"no escalation, a consumer collects the address", "i.ytimg.com", false, false, false, false, true, nfqueue.NfAccept, nfqueue.NfAccept},
		{"no escalation for its host, a consumer collects the address and is another host's escalation", "i.ytimg.com", false, false, false, true, true, nfqueue.NfAccept, nfqueue.NfAccept},
	} {
		t.Run(c.name, func(t *testing.T) {
			named := newPassiveSet()
			named.Id, named.Name = "named", "named"
			named.Targets.DomainOnly = false
			escalated := newFragmentingYTImagesSet()
			escalated.Id, escalated.Name = "escalated", "escalated"
			escalated.Targets.DomainOnly = false
			escalated.Targets.DomainsToMatch = []string{"never.example.org"}
			escalated.Targets.UseCollectedIPs = c.consumer
			w := newInjectingWorker(t, &named, &escalated)
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stubInjectRoute(t, idx["eth7"], true)
			if c.learn {
				queuedVerdict(t, w, makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, 12346, 0x18), nil, idx["eth7"], 0)
			}
			escalate := func() {
				host := "i.ytimg.com"
				if c.consumer {
					host = "other.example.org"
				}
				w.destState.SetEscalation(host, "escalated", "test", time.Hour)
			}
			if c.escalateFirst {
				escalate()
			}
			hello := buildClientHello(c.hello, 16, 0xAB)
			if v := queuedVerdict(t, w, pinnedHello(c.hello), nil, idx["eth7"], 0); v != c.wantFirst {
				t.Fatalf("setup: the first ClientHello got verdict %d, want %d", v, c.wantFirst)
			}
			if c.save {
				saveSets(w, nil)
			}
			data := makeV4TCPPacket(tlsAppData(100), 1000+uint32(len(hello)))
			if v := queuedVerdict(t, w, data, nil, idx["eth7"], 0); v != c.wantFirst {
				t.Fatalf("setup: the data packet got verdict %d, want %d", v, c.wantFirst)
			}
			var ips []string
			if c.consumer {
				ips = []string{"1.2.3.4"}
			}
			publishCollectedMatcher(t, w, ips)
			if c.escalateLater {
				escalate()
			}
			if v := queuedVerdict(t, w, pinnedHello(c.hello), nil, idx["eth7"], 0); v != c.want {
				d, _ := w.tlsCache.Decision(pinnedConnKey)
				t.Errorf("%s: a resent ClientHello got verdict %d after a collected publish, want %d (recorded set %q); a publish leaves an open connection its set and that set's escalation, and gives it to no set that took its address since", c.name, v, c.want, d.priorSet)
			}
		})
	}
}

func TestALearnerAheadOfAnIPPrioritySetKeepsTheConnectionAcrossASaveAndAPublish(t *testing.T) {
	const unlisted = "unlisted.example.net"
	for _, save := range []bool{false, true} {
		name := "without a save"
		if save {
			name = "with a save"
		}
		t.Run(name, func(t *testing.T) {
			learner := newFragmentingYTImagesSet()
			learner.Id, learner.Name = "learner", "learner"
			learner.Targets.DomainOnly = false
			listing := newPassiveSet()
			listing.Id, listing.Name = "listing", "listing"
			listing.Targets.DomainOnly = false
			listing.Targets.DomainsToMatch = nil
			listing.Targets.IpsToMatch = []string{"1.2.3.4"}
			listing.Targets.IPPriority = true
			w := newInjectingWorker(t, &learner, &listing)
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stubInjectRoute(t, idx["eth7"], true)
			queuedVerdict(t, w, makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, 12346, 0x18), nil, idx["eth7"], 0)
			hello := buildClientHello(unlisted, 16, 0xAB)
			if v := queuedVerdict(t, w, pinnedHello(unlisted), nil, idx["eth7"], 0); v != nfqueue.NfDrop {
				t.Fatalf("setup: the first ClientHello got verdict %d, want the learner's drop: IP priority holds only against the sets after the listing set", v)
			}
			if save {
				saveSets(w, nil)
			}
			data := makeV4TCPPacket(tlsAppData(100), 1000+uint32(len(hello)))
			if v := queuedVerdict(t, w, data, nil, idx["eth7"], 0); v != nfqueue.NfDrop {
				t.Fatalf("setup: the data packet got verdict %d, want the learner's drop", v)
			}
			publishCollectedMatcher(t, w, nil)
			if v := queuedVerdict(t, w, pinnedHello(unlisted), nil, idx["eth7"], 0); v != nfqueue.NfDrop {
				d, _ := w.tlsCache.Decision(pinnedConnKey)
				t.Errorf("%s: a resent ClientHello got verdict %d after a collected publish, want the learner's drop as before it (recorded set %q); a set with IP priority later in the order never had the connection", name, v, d.priorSet)
			}
		})
	}
}

func TestAnEscalationFoundAfterAHostHintDropSurvivesACollectedPublish(t *testing.T) {
	const unlisted = "unlisted.example.net"
	hinted := newPassiveSet()
	hinted.Id, hinted.Name = "hinted", "hinted"
	hinted.Targets.DomainOnly = false
	escalated := newFragmentingYTImagesSet()
	escalated.Id, escalated.Name = "escalated", "escalated"
	escalated.Targets.DomainOnly = false
	escalated.Targets.DomainsToMatch = []string{"never.example.org"}
	w := newInjectingWorker(t, &hinted, &escalated)
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stubInjectRoute(t, idx["eth7"], true)
	w.hostHints.Store("10.0.0.1", "1.2.3.4", "hinted", "i.ytimg.com")
	w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", &hinted)
	w.destState.SetEscalation("i.ytimg.com", "escalated", "test", time.Hour)
	if v := queuedVerdict(t, w, pinnedHello(unlisted), nil, idx["eth7"], 0); v != nfqueue.NfDrop {
		t.Fatalf("setup: the first ClientHello got verdict %d, want the escalated set's drop after the hint gave way to the learned address", v)
	}
	publishCollectedMatcher(t, w, nil)
	if v := queuedVerdict(t, w, pinnedHello(unlisted), nil, idx["eth7"], 0); v != nfqueue.NfDrop {
		d, _ := w.tlsCache.Decision(pinnedConnKey)
		t.Errorf("a resent ClientHello got verdict %d after a collected publish, want the escalated set's drop as before it (recorded set %q); the escalation lookup after a host hint drop must not take the pinned scope", v, d.priorSet)
	}
}
