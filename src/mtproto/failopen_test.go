package mtproto

import (
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func failOpenTestBridge(t *testing.T, workers string) *TransparentBridge {
	t.Helper()
	workerResetStall()
	failOpenMu.Lock()
	failOpenDirect = map[string]time.Time{}
	failOpenMu.Unlock()
	t.Cleanup(func() {
		workerResetStall()
		failOpenMu.Lock()
		failOpenDirect = map[string]time.Time{}
		failOpenMu.Unlock()
	})
	cfg := config.NewConfig()
	cfg.System.MTProto.CFWorkerDomain = workers
	return NewTransparentBridge(&cfg)
}

func assertFailOpenOrder(t *testing.T, b *TransparentBridge, ip net.IP, port int, wantDirectFirst, wantWorkerFallback bool, why string) {
	t.Helper()
	directFirst, workerFallback := b.FailOpenOrder(ip, port)
	if directFirst != wantDirectFirst || workerFallback != wantWorkerFallback {
		t.Errorf("%s: got directFirst=%v workerFallback=%v, want %v %v", why, directFirst, workerFallback, wantDirectFirst, wantWorkerFallback)
	}
}

func TestFailOpenGoesDirectWhenTheWorkerCannotServeIt(t *testing.T) {
	ip := net.ParseIP("149.154.175.211")

	b := failOpenTestBridge(t, "")
	assertFailOpenOrder(t, b, ip, 443, true, false, "with no Worker configured the fail-open path goes direct only")

	b = failOpenTestBridge(t, "relay.example.workers.dev")
	assertFailOpenOrder(t, b, ip, 80, true, false, "the Worker always connects to port 443, so a port-80 connection goes direct only")
	if b.FailOpenViaWorker(nil, ip, 80) {
		t.Error("a port-80 connection was handed to the Worker")
	}
	assertFailOpenOrder(t, b, ip, 443, false, true, "a usable Worker with no history stays first for port 443")

	workerDemote("relay.example.workers.dev")
	assertFailOpenOrder(t, b, ip, 443, true, false, "a single Worker in cooldown must not keep taking fail-open sessions")
}

func TestFailOpenRemembersWhatCarriedData(t *testing.T) {
	ip := net.ParseIP("149.154.167.41")
	b := failOpenTestBridge(t, "relay.example.workers.dev")

	failOpenRemember(ip.String(), true)
	assertFailOpenOrder(t, b, ip, 443, true, true, "after the Worker stalled for this address, direct goes first with the Worker behind it")
	assertFailOpenOrder(t, b, net.ParseIP("149.154.167.42"), 443, false, true, "the memory is per destination")

	b.NoteFailOpenDirect(ip, false, 0)
	assertFailOpenOrder(t, b, ip, 443, false, true, "a failed direct dial hands the address back to the Worker")

	b.NoteFailOpenDirect(ip, true, 512)
	assertFailOpenOrder(t, b, ip, 443, true, true, "a direct connection that carried data is preferred next time")

	b.NoteFailOpenDirect(ip, true, 0)
	assertFailOpenOrder(t, b, ip, 443, false, true, "a direct connection that opened and carried nothing, as behind a filter that answers the handshake itself, hands the address back to the Worker")
}

func TestWorkerDialsFollowSetsOnlyWhenAllowed(t *testing.T) {
	base := uint(config.SelfDialMark)
	relay := uint(config.SelfDialRelayMark)
	worker := transportPlan{kind: transportWS, isWorker: true}
	proxied := transportPlan{kind: transportWS, cfBase: "example.co.uk"}

	cfg := config.NewConfig()
	setWorkerFollowsSets(&cfg)
	t.Cleanup(func() { setWorkerFollowsSets(nil) })
	if got := planDialMark(worker, base); got != relay {
		t.Errorf("by default a Worker dial must skip DPI sets, got 0x%x", got)
	}

	cfg.System.MTProto.CFWorkerDPI = true
	setWorkerFollowsSets(&cfg)
	if got := planDialMark(worker, base); got != base {
		t.Errorf("with the switch on a Worker dial must carry the plain self-dial mark, got 0x%x", got)
	}
	if got := workerDialMark(base); got != base {
		t.Errorf("pool and fail-open Worker dials must follow the switch too, got 0x%x", got)
	}
	if got := planDialMark(proxied, base); got != relay {
		t.Errorf("the switch covers the Worker only; Cloudflare-proxied domains must keep skipping DPI, got 0x%x", got)
	}
}
