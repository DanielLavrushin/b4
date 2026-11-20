package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
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
