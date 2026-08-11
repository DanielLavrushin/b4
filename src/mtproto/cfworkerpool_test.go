package mtproto

import (
	"crypto/tls"
	"net"
	"sort"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestPlanTransports_BlacklistSuppressesOnlyNativeEdge(t *testing.T) {
	wsResetState()
	tcpResetState()
	defer func() {
		wsResetState()
		tcpResetState()
	}()

	wsRecordFailure(2, true)

	cfg := &config.MTProtoConfig{
		UpstreamMode:   "ws",
		CFWorkerDomain: "w.user.workers.dev",
		CFProxyEnabled: false,
		WSCustomDomain: "example.com",
	}
	plans, err := planTransports(cfg, config.QueueConfig{}, 2, dialTarget{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	worker, custom := false, false
	for _, p := range plans {
		if p.native {
			t.Errorf("blacklisted DC must not plan the native WS edge, got %q", p.sni)
		}
		if p.isWorker {
			worker = true
		}
		if p.cfBase != "" {
			custom = true
		}
	}
	if !worker {
		t.Error("a 302 from Telegram's edge must not disable the user's own Worker")
	}
	if !custom {
		t.Error("a 302 from Telegram's edge must not disable the user's own CF-proxied domain")
	}
}

func TestPlanTransports_EverythingCoolingStillYieldsAPlan(t *testing.T) {
	wsResetState()
	tcpResetState()
	defer func() {
		wsResetState()
		tcpResetState()
	}()

	wsRecordFailure(2, true)
	tcpRecordFailure(dcAddressesV4[2])

	cfg := &config.MTProtoConfig{UpstreamMode: "auto"}
	plans, err := planTransports(cfg, config.QueueConfig{IPv4Enabled: true}, 2, dialTarget{})
	if err != nil {
		t.Fatalf("a fully cooling DC must still yield a retryable plan, got %v", err)
	}
	if !hasTCP(plans) {
		t.Fatalf("expected the cooling TCP address to be retried as a last resort, got %+v", plans)
	}
}

func TestPlanTransports_WorkerDialTimeoutNotShortenedByNativeCooldown(t *testing.T) {
	wsResetState()
	defer wsResetState()

	wsRecordFailure(2, false)
	if !wsCooldownActive(2) {
		t.Fatal("precondition: DC2 native WS should be cooling")
	}

	cfg := &config.MTProtoConfig{UpstreamMode: "ws", CFWorkerDomain: "w.user.workers.dev"}
	plans, err := planTransports(cfg, config.QueueConfig{}, 2, dialTarget{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, p := range plans {
		if p.isWorker && p.native {
			t.Error("worker plans must never be flagged native, or they inherit the edge's short dial timeout")
		}
	}
}

func TestShuffledWorkerDomains_PreservesSet(t *testing.T) {
	cfg := &config.MTProtoConfig{CFWorkerDomain: "a.workers.dev, b.workers.dev ,c.workers.dev"}
	got := shuffledWorkerDomains(cfg)
	sort.Strings(got)
	want := []string{"a.workers.dev", "b.workers.dev", "c.workers.dev"}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestNewSplitterFor(t *testing.T) {
	wsBacked := &ObfuscatedConn{Conn: &wsConn{}}

	if s := newSplitterFor(wsBacked, dialInfo{isWorker: true}, connectionTagAbridged); s != nil {
		t.Error("a Worker relays raw TCP, so it must not get per-packet WS framing")
	}
	if s := newSplitterFor(wsBacked, dialInfo{}, connectionTagAbridged); s == nil {
		t.Error("Telegram's own WS edge needs one transport packet per frame")
	}
}

func TestCFWorkerPool_MediaDCSharesBucketWithMainDC(t *testing.T) {
	main := transportPlan{sni: "w.workers.dev", dc: 2, isWorker: true}
	media := transportPlan{sni: "w.workers.dev", dc: -2, isWorker: true}
	if planWorkerKey(main) != planWorkerKey(media) {
		t.Error("the Worker URL is identical for DC 2 and DC -2, so both must share one bucket")
	}
}

func TestCFWorkerPool_NilAndEmptyAreSafe(t *testing.T) {
	var nilPool *cfWorkerPool
	pl := transportPlan{sni: "w.workers.dev", dc: 2, isWorker: true}

	if nilPool.get(pl) != nil {
		t.Error("nil pool must report a miss")
	}
	nilPool.warm(pl)
	nilPool.close()

	p := newCFWorkerPool(0)
	if p.get(pl) != nil {
		t.Error("empty pool must report a miss")
	}
	p.close()
	if p.get(pl) != nil {
		t.Error("closed pool must report a miss")
	}
}

func TestCFWorkerPool_ExpireRetiresEntry(t *testing.T) {
	p := newCFWorkerPool(0)
	defer p.close()

	pl := transportPlan{sni: "w.workers.dev", dc: 2, isWorker: true}
	k := planWorkerKey(pl)

	local, remote := net.Pipe()
	defer remote.Close()
	conn := &wsConn{tls: tls.Client(local, &tls.Config{InsecureSkipVerify: true})}
	conn.closed.Store(true)

	p.mu.Lock()
	p.idle[k] = []cfWorkerEntry{{conn: conn}}
	p.mu.Unlock()

	p.expire(k, conn)

	p.mu.Lock()
	n := len(p.idle[k])
	p.mu.Unlock()
	if n != 0 {
		t.Fatalf("expired conn should be dropped from the bucket, %d left", n)
	}
}
