package nfq

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/florianl/go-nfqueue"
)

const pinnedConnKey = "10.0.0.1:12345->1.2.3.4:443"

func publishCollectedMatcher(t *testing.T, w *Worker) {
	t.Helper()
	next := buildMatcher(w.getConfig())
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
	publishCollectedMatcher(t, w)
	if v := queuedVerdict(t, w, pinnedHello("kws2.web.telegram.org"), &mark, idx["eth7"], 0); v != nfqueue.NfDrop {
		t.Errorf("a resent ClientHello of b4's own Telegram connection got verdict %d after a collected publish, want the Telegram set's strategy; the mark picks that set whatever the addresses, so a publish has nothing to re-match", v)
	}
}
