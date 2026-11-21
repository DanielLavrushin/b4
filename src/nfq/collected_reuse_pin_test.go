package nfq

import (
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

func newReuseConsumer() config.SetConfig {
	set := newFragmentingYTImagesSet()
	set.Id, set.Name = "reuse-consumer", "reuse-consumer"
	set.Targets.DomainOnly = false
	set.Targets.DomainsToMatch = nil
	set.Targets.UseCollectedIPs = true
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, Scale: 1}
	set.TCP.WSSizeReuse.Enabled = true
	return set
}

func stubReuseDiverts(t *testing.T, id string) *bool {
	t.Helper()
	diverted := new(bool)
	saved := wsReuseDiverts
	t.Cleanup(func() { wsReuseDiverts = saved })
	wsReuseDiverts = func(set string) bool { return *diverted && set == id }
	return diverted
}

func TestAPinnedConnectionIsNotPassedToAReuseConsumerThatTookItsAddress(t *testing.T) {
	for _, c := range []struct {
		name      string
		host      string
		collected []string
		published []string
		divertNow bool
		decided   string
	}{
		{"a set its ClientHello named, then the consumer collects the address", "i.ytimg.com", []string{}, []string{"1.2.3.4"}, true, "sni-a"},
		{"the consumer itself, decided before its diversion came up", "unlisted.example.net", []string{"1.2.3.4"}, []string{"1.2.3.4", "5.6.7.8"}, false, "reuse-consumer"},
	} {
		t.Run(c.name, func(t *testing.T) {
			named := newFragmentingYTImagesSet()
			named.Id, named.Name = "sni-a", "sni-a"
			consumer := newReuseConsumer()
			diverted := stubReuseDiverts(t, consumer.Id)
			*diverted = c.divertNow
			w := newInjectingWorker(t, &named, &consumer)
			w.matcher.Store(buildMatcher(storeCollectedView(w, c.collected)))
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stubInjectRoute(t, idx["eth7"], true)
			if v := queuedVerdict(t, w, pinnedHello(c.host), nil, idx["eth7"], 0); v != nfqueue.NfDrop {
				t.Fatalf("setup: the first ClientHello got verdict %d, want a set's strategy", v)
			}
			d, ok := w.tlsCache.Decision(pinnedConnKey)
			decided := d.priorSet
			if ok && d.set != nil {
				decided = d.set.Id
			}
			if !ok || decided != c.decided {
				t.Fatalf("setup: the connection was decided for %q (decided=%v), want %q", decided, ok, c.decided)
			}
			publishCollectedMatcher(t, w, c.published)
			*diverted = false
			if v := queuedVerdict(t, w, makeV4TCPPacketFromPort(buildClientHello("unlisted.example.net", 16, 0xAB), 1000, 12346, 0x18), nil, idx["eth7"], 0); v != nfqueue.NfDrop {
				t.Fatalf("setup: a new connection to the collected address got verdict %d while the consumer is not diverted, want the consumer's strategy", v)
			}
			*diverted = true
			if v := queuedVerdict(t, w, makeV4TCPPacketFromPort(buildClientHello("unlisted.example.net", 16, 0xAB), 1000, 12347, 0x18), nil, idx["eth7"], 0); v != nfqueue.NfAccept {
				t.Fatalf("setup: a new connection to the collected address got verdict %d while the consumer is diverted, want it passed untouched, so this test proves nothing", v)
			}
			if v := queuedVerdict(t, w, pinnedHello(c.host), nil, idx["eth7"], 0); v != nfqueue.NfDrop {
				t.Errorf("%s: a resent ClientHello of a connection the queue decided for %q before a collected publish got verdict %d while the reuse consumer is diverted, want that set's strategy; the relay never had the connection, and a publish leaves an open connection the set it had", c.name, decided, v)
			}
		})
	}
}

func TestASplitHelloSecondSegmentStaysBehindTheFirstAcrossAReuseConsumerPublish(t *testing.T) {
	named := newFakingSet(false)
	named.Id, named.Name = "sni-a", "sni-a"
	named.Targets.DomainOnly = true
	consumer := newReuseConsumer()
	diverted := stubReuseDiverts(t, consumer.Id)
	*diverted = true
	w, sent := heldHelloWorker(t, &named, &consumer)
	w.matcher.Store(buildMatcher(storeCollectedView(w, []string{})))
	fake := sock.GetPayload(&named.Faking)
	hello := helloWithEarlySNI("i.ytimg.com")
	const cut = 600
	seg1, seg2 := hello[:cut], hello[cut:]
	order := &orderLog{}
	parked, release := parkFirstSend(t, w, order, func(pkt []byte) string {
		return labelSent(pkt, splitSeq, fake, namedPiece{"1", seg1}, namedPiece{"2", seg2})
	})
	q := orderedVerdicts{order}

	queueTo(w, q, 1, makeV4TCPPacket(seg1, splitSeq))
	waitParked(t, parked)
	publishCollectedMatcher(t, w, []string{"1.2.3.4"})
	queueTo(w, q, 2, makeV4TCPPacket(seg2, splitSeq+cut))
	if got := order.list(); !slices.Equal(got, []string{"drop 1", "drop 2"}) {
		t.Fatalf("a collected publish gave the address to a diverted reuse consumer while segment 1 was on its way: %v, want [drop 1 drop 2]; segment 2 must wait for segment 1, not leave ahead of it", got)
	}

	release()
	sent()
	if got, want := order.sends(), []string{"F@0", "1", "2"}; !slices.Equal(got, want) {
		t.Fatalf("sent %v, want %v: the fake, segment 1, then segment 2 with no fake of its own", got, want)
	}
}
