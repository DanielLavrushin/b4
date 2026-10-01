package tables

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
)

func holdAsyncWorker(t *testing.T) func() {
	t.Helper()
	routeAsyncStart()
	block := make(chan struct{})
	started := make(chan struct{})
	routeAsyncCh <- func() {
		close(started)
		<-block
	}
	var once sync.Once
	release := func() { once.Do(func() { close(block) }) }
	t.Cleanup(release)
	awaitClosed(t, started, "the blocking job")
	return release
}

func drainAsync(t *testing.T) {
	t.Helper()
	routeAsyncStart()
	done := make(chan struct{})
	routeAsyncCh <- func() { close(done) }
	awaitClosed(t, done, "the async queue")
}

func TestRoutingHandleDNSAwaitCoalescesABurstIntoOneUpdate(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	g := gateAdds(t, be)
	g.open()
	release := holdAsyncWorker(t)

	var waits [][]<-chan struct{}
	for i := 0; i < 20; i++ {
		w := RoutingHandleDNSAwait(cfg, set, []net.IP{net.IPv4(198, 51, 100, byte(100+i))})
		if len(w) != 1 {
			t.Fatalf("answer %d got %d waits, want 1", i, len(w))
		}
		waits = append(waits, w)
	}
	for i := 1; i < len(waits); i++ {
		if waits[i][0] != waits[0][0] {
			t.Fatalf("answer %d waits on an update of its own, want the burst to share one", i)
		}
	}
	release()
	for _, w := range waits {
		awaitClosed(t, w[0], "the burst")
	}
	if n := g.count.Load(); n != 1 {
		t.Fatalf("the burst took %d firewall updates, want 1", n)
	}
	if add := nextAdd(t, g); len(add.ips) != 20 {
		t.Fatalf("the update carried %d addresses, want all 20", len(add.ips))
	}
}

func TestRoutingHandleDNSAwaitSplitsAHugeBurstIntoChunks(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	g := gateAdds(t, be)
	g.open()
	release := holdAsyncWorker(t)

	var waits []<-chan struct{}
	for i := 0; i < 50; i++ {
		ips := make([]net.IP, 0, 4)
		for j := 0; j < 4; j++ {
			ips = append(ips, net.IPv4(10, 77, byte(i), byte(j+1)))
		}
		w := RoutingHandleDNSAwait(cfg, set, ips)
		if len(w) != 1 {
			t.Fatalf("answer %d got %d waits, want 1", i, len(w))
		}
		waits = append(waits, w[0])
	}
	if waits[0] != waits[31] || waits[31] == waits[32] || waits[32] != waits[49] {
		t.Fatal("a burst must fill one update up to a chunk of addresses and then start the next")
	}
	release()
	for _, w := range waits {
		awaitClosed(t, w, "the burst")
	}
	if n := g.count.Load(); n != 2 {
		t.Fatalf("200 addresses took %d firewall updates, want 2", n)
	}
	first, second := nextAdd(t, g), nextAdd(t, g)
	if len(first.ips)+len(second.ips) != 200 || len(first.ips) > routeNftChunkSize || len(second.ips) > routeNftChunkSize {
		t.Fatalf("updates carried %d and %d addresses, want 200 in chunks of at most %d", len(first.ips), len(second.ips), routeNftChunkSize)
	}
}

func TestRoutingHandleDNSAwaitStartsANewUpdateOnceTheBatchIsRunning(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	g := gateAdds(t, be)

	first := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.50")})
	nextAdd(t, g)
	second := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.51")})
	if len(first) != 1 || len(second) != 1 || first[0] == second[0] {
		t.Fatal("an address that arrived while an update was running joined it and would be released before it was added")
	}
	g.open()
	awaitClosed(t, first[0], "the first update")
	awaitClosed(t, second[0], "the second update")
	if n := g.count.Load(); n != 2 {
		t.Fatalf("%d firewall updates, want 2", n)
	}
}

func TestRoutingHandleDNSAwaitDoesNotHoldForAnInterfaceSetWithoutEgress(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	set.Routing.EgressInterface = ""
	var adds atomic.Int32
	be.addElementsFn = func(string, []string, int) { adds.Add(1) }

	if waits := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.52")}); len(waits) != 0 {
		t.Fatalf("a set with no egress interface can add nothing, yet the answer got %d waits", len(waits))
	}
	drainAsync(t)
	if n := adds.Load(); n != 0 {
		t.Fatalf("a set with no egress interface was updated %d times", n)
	}
}

func TestRoutingHandleDNSAwaitDoesNotHoldASetThatFailedToInstall(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	delete(routeRuleCache, set.Id)
	var attempts atomic.Int32
	be.ensureChainFn = func(string, bool) error {
		attempts.Add(1)
		return errors.New("module missing")
	}

	first := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.53")})
	if len(first) != 1 {
		t.Fatalf("the first answer got %d waits, want 1", len(first))
	}
	awaitClosed(t, first[0], "the failed install")
	if !routeInstallFailedRecently(set.Id) {
		t.Fatal("a failed install was not remembered")
	}
	if waits := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.54")}); len(waits) != 0 {
		t.Fatalf("an answer for a set whose rules just failed to install got %d waits", len(waits))
	}
	drainAsync(t)
	if attempts.Load() < 2 {
		t.Fatal("b4 stopped trying to install the set")
	}

	routeNoteInstalled(set.Id)
	be.ensureChainFn = nil
	if waits := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.55")}); len(waits) != 1 {
		t.Fatalf("an answer for an installed set got %d waits, want 1", len(waits))
	} else {
		awaitClosed(t, waits[0], "the update")
	}
}

func failingChains(t *testing.T, be *mockRouteBackend) *atomic.Bool {
	t.Helper()
	fail := &atomic.Bool{}
	fail.Store(true)
	be.ensureChainFn = func(string, bool) error {
		if fail.Load() {
			return errors.New("module missing")
		}
		return nil
	}
	return fail
}

func TestASuccessfulInstallClearsTheFailureMemo(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	delete(routeRuleCache, set.Id)
	fail := failingChains(t, be)

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.73")})
	if !routeInstallFailedRecently(set.Id) {
		t.Fatal("a failed install was not remembered")
	}
	fail.Store(false)
	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.74")})
	if routeInstallFailedRecently(set.Id) {
		t.Fatal("a set whose rules installed is still remembered as failed, so its answers go unheld for up to a minute")
	}
}

func TestASuccessfulSyncClearsTheFailureMemo(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	delete(routeRuleCache, set.Id)
	fail := failingChains(t, be)

	RoutingSyncConfig(cfg)
	if !routeInstallFailedRecently(set.Id) {
		t.Fatal("a sync that could not install the set was not remembered")
	}
	fail.Store(false)
	RoutingSyncConfig(cfg)
	if routeInstallFailedRecently(set.Id) {
		t.Fatal("a set the sync installed is still remembered as failed")
	}
}

func TestAnAnswerForASetWithRulesInPlaceClearsTheFailureMemo(t *testing.T) {
	cfg, set, _ := awaitSetup(t, false)
	routeNoteInstallFailed(set.Id)

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.75")})
	if routeInstallFailedRecently(set.Id) {
		t.Fatal("a set whose rules are in place is still remembered as failed")
	}
}

func TestRoutingHandleDNSAwaitAfterForgetAllQueuesItsOwnUpdate(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	g := gateAdds(t, be)
	ip := []net.IP{net.ParseIP("198.51.100.56")}

	first := RoutingHandleDNSAwait(cfg, set, ip)
	nextAdd(t, g)
	routeAsyncForgetAll()
	second := RoutingHandleDNSAwait(cfg, set, ip)
	if len(first) != 1 || len(second) != 1 || first[0] == second[0] {
		t.Fatal("after a resync an answer joined the update the resync may have undone")
	}
	g.open()
	awaitClosed(t, first[0], "the first update")
	awaitClosed(t, second[0], "the second update")
	routeAsyncSeenMu.Lock()
	_, pending := routeAsyncPending[set.Id+"|198.51.100.56"]
	routeAsyncSeenMu.Unlock()
	if pending {
		t.Fatal("the address is still marked as under way after both updates finished")
	}
}

func TestSynchronousAddLetsTheRelayedAnswerPassAtOnce(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	ips := []net.IP{net.ParseIP("198.51.100.57")}

	RoutingHandleDNS(cfg, set, ips)
	if waits := RoutingHandleDNSAwait(cfg, set, ips); len(waits) != 0 {
		t.Fatalf("an address b4 has just added itself made the relayed answer wait: %d waits", len(waits))
	}
	if len(be.setOps) != 1 {
		t.Fatalf("set operations %v, want one add", be.setOps)
	}
}

func TestRoutingHandleDNSAwaitSkipsIPv4WhenItIsOff(t *testing.T) {
	cfg, set, _ := awaitSetup(t, true)
	cfg.Queue.IPv4Enabled = false

	if waits := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.58")}); len(waits) != 0 {
		t.Fatalf("an IPv4 answer with IPv4 off must queue nothing, got %d waits", len(waits))
	}
}

func TestRoutingHandleDNSAddsLearnedAddressesWithTheDefaultTTL(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	set.Routing.IPTTLSeconds = 0
	var ttls []int
	be.addElementsFn = func(_ string, _ []string, ttl int) { ttls = append(ttls, ttl) }

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.59")})

	if len(ttls) != 1 || ttls[0] != 3600 {
		t.Fatalf("learned addresses were added with TTLs %v, want [3600]", ttls)
	}
}
