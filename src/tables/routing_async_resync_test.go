package tables

import (
	"net"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

type resyncAddLog struct {
	mu   sync.Mutex
	adds []string
}

func (l *resyncAddLog) has(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, a := range l.adds {
		if a == ip {
			return true
		}
	}
	return false
}

func recordResyncAdds(be *mockRouteBackend) *resyncAddLog {
	l := &resyncAddLog{}
	be.addElementsFn = func(_ string, ips []string, _ int) {
		l.mu.Lock()
		l.adds = append(l.adds, ips...)
		l.mu.Unlock()
	}
	return l
}

func resyncCachedState(setID string) (routeState, bool) {
	routeMu.Lock()
	defer routeMu.Unlock()
	st, ok := routeRuleCache[setID]
	return st, ok
}

func resyncSetCopy(set *config.SetConfig) *config.SetConfig {
	c := *set
	return &c
}

func resyncOne(t *testing.T, waits []<-chan struct{}) <-chan struct{} {
	t.Helper()
	if len(waits) != 1 {
		t.Fatalf("got %d waits, want 1", len(waits))
	}
	return waits[0]
}

func resyncSeedState(cfg *config.Config, set *config.SetConfig) {
	routeMu.Lock()
	st := buildRouteState(cfg, set)
	st.set = set
	routeRuleCache[set.Id] = st
	routeMu.Unlock()
}

func resyncSealed(setID string) func() bool {
	return func() bool {
		routeAsyncSeenMu.Lock()
		defer routeAsyncSeenMu.Unlock()
		return routeAsyncOpen[setID] == nil
	}
}

func TestResyncForcedResyncKeepsTheAddressOfABatchInFlight(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	adds := recordResyncAdds(be)
	resyncSeedState(cfg, set)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.106")}))
	routeMu.Lock()
	forced := make(chan struct{})
	go func() {
		RoutingForceResync(cfg)
		close(forced)
	}()
	time.Sleep(30 * time.Millisecond)
	release()
	waitUntil(t, "the batch to be sealed", resyncSealed(set.Id))
	time.Sleep(30 * time.Millisecond)
	routeAsyncSeenMu.Lock()
	routeMu.Unlock()
	time.Sleep(30 * time.Millisecond)
	routeAsyncSeenMu.Unlock()

	awaitClosed(t, wait, "the batch in flight")
	awaitClosed(t, forced, "the forced resync")
	drainAsync(t)
	if _, ok := resyncCachedState(set.Id); !ok {
		t.Fatal("the forced resync did not reinstall the set")
	}
	if !adds.has("198.51.100.106") {
		t.Fatal("the forced resync reinstalled the set from the same config, but the batch in flight during it was released without its address")
	}
}

func TestResyncForcedResyncKeepsTheAddressOfABatchAnEarlierSyncMadeStale(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	adds := recordResyncAdds(be)
	resyncSeedState(cfg, set)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.110")}))
	RoutingSyncConfig(cfg)
	routeMu.Lock()
	forced := make(chan struct{})
	go func() {
		RoutingForceResync(cfg)
		close(forced)
	}()
	time.Sleep(30 * time.Millisecond)
	release()
	waitUntil(t, "the batch to be sealed", resyncSealed(set.Id))
	time.Sleep(30 * time.Millisecond)
	routeMu.Unlock()

	awaitClosed(t, wait, "the batch")
	awaitClosed(t, forced, "the forced resync")
	drainAsync(t)
	if _, ok := resyncCachedState(set.Id); !ok {
		t.Fatal("the forced resync did not reinstall the set")
	}
	if !adds.has("198.51.100.110") {
		t.Fatal("a batch an earlier sync had made stale was released without its address while a forced resync rebuilt the set from the same config")
	}
}

func TestResyncBatchRacingASyncAddsUnderTheNewState(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	resyncSeedState(cfg, set)
	moved := resyncSetCopy(set)
	moved.Routing.EgressInterface = "b4new0"
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{moved}
	var mu sync.Mutex
	var addedUnder []string
	be.addElementsFn = func(_ string, ips []string, _ int) {
		for _, ip := range ips {
			if ip == "198.51.100.109" {
				mu.Lock()
				addedUnder = append(addedUnder, routeRuleCache[set.Id].iface)
				mu.Unlock()
			}
		}
	}
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(next, moved, []net.IP{net.ParseIP("198.51.100.109")}))
	routeMu.Lock()
	release()
	waitUntil(t, "the batch to be sealed", resyncSealed(set.Id))
	time.Sleep(30 * time.Millisecond)
	synced := make(chan struct{})
	go func() {
		RoutingSyncConfig(next)
		close(synced)
	}()
	time.Sleep(30 * time.Millisecond)
	routeMu.Unlock()

	awaitClosed(t, wait, "the batch")
	awaitClosed(t, synced, "the resync")
	drainAsync(t)
	mu.Lock()
	defer mu.Unlock()
	if len(addedUnder) == 0 {
		t.Fatal("the batch's address was never added")
	}
	for _, iface := range addedUnder {
		if iface != "b4new0" {
			t.Fatalf("the batch added its address while the set still had the state the resync was about to replace (%q)", iface)
		}
	}
}

func TestResyncAddingADomainKeepsQueuedAnswersForTheOthers(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	looked := make(chan struct{}, 16)
	stubSetLookup(t, func(dns.Server, string) ([]net.IP, error) {
		select {
		case looked <- struct{}{}:
		default:
		}
		return nil, dns.ErrNoAddress
	})
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "192.0.2.53"
	set.Targets.SNIDomains = []string{"a.example"}
	resyncSeedState(cfg, set)
	adds := recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.107")}))
	grown := resyncSetCopy(set)
	grown.Targets.SNIDomains = []string{"a.example", "b.example"}
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{grown}
	RoutingSyncConfig(next)
	awaitClosed(t, looked, "the pre-resolution of the first target")
	awaitClosed(t, looked, "the pre-resolution of the second target")
	release()
	awaitClosed(t, wait, "the queued answer")
	drainAsync(t)
	if !adds.has("198.51.100.107") {
		t.Fatal("adding b.example to the set dropped a held answer for a.example, which the set still targets")
	}
}

func TestResyncRemovedGeositeCategoryDropsItsQueuedAnswer(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	set.Targets.GeoSiteCategories = []string{"cat-a", "cat-b"}
	set.Targets.DomainsToMatch = []string{"a.example", "b.example"}
	resyncSeedState(cfg, set)
	adds := recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.108")}))
	narrowed := resyncSetCopy(set)
	narrowed.Targets.GeoSiteCategories = []string{"cat-b"}
	narrowed.Targets.DomainsToMatch = []string{"b.example"}
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{narrowed}
	RoutingSyncConfig(next)
	release()
	awaitClosed(t, wait, "the queued answer")
	drainAsync(t)
	if adds.has("198.51.100.108") {
		t.Fatal("an answer queued for a geosite category the resync removed was still added to the set")
	}
}

func TestResyncQueuedBatchDoesNotReinstallARemovedSet(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	adds := recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.90")}))
	RoutingSyncConfig(familyTestConfig(true, false))
	if _, ok := resyncCachedState(set.Id); ok {
		t.Fatal("the resync did not remove the set")
	}

	release()
	awaitClosed(t, wait, "the queued batch")
	drainAsync(t)
	if _, ok := resyncCachedState(set.Id); ok {
		t.Fatal("a batch queued before the resync reinstalled the set the resync removed")
	}
	if adds.has("198.51.100.90") {
		t.Fatal("a batch queued before the resync added its address to the set the resync removed")
	}
}

func TestResyncQueuedBatchDoesNotRevertAChangedSet(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.91")}))
	moved := resyncSetCopy(set)
	moved.Routing.EgressInterface = "b4new0"
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{moved}
	RoutingSyncConfig(next)
	if st, ok := resyncCachedState(set.Id); !ok || st.iface != "b4new0" {
		t.Fatalf("the resync left the set on %q, want b4new0", st.iface)
	}

	release()
	awaitClosed(t, wait, "the queued batch")
	drainAsync(t)
	if st, ok := resyncCachedState(set.Id); !ok || st.iface != "b4new0" {
		t.Fatalf("a batch queued before the resync rebuilt the set back onto %q, want it to stay on b4new0", st.iface)
	}
}

func TestResyncQueuedBatchDoesNotRevertASetANewerAnswerRebuilt(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	adds := recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.104")}))
	moved := resyncSetCopy(set)
	moved.Routing.EgressInterface = "b4new0"
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{moved}
	RoutingHandleDNS(next, moved, []net.IP{net.ParseIP("198.51.100.105")})
	if st, ok := resyncCachedState(set.Id); !ok || st.iface != "b4new0" {
		t.Fatalf("the newer answer left the set on %q, want b4new0", st.iface)
	}

	release()
	awaitClosed(t, wait, "the queued batch")
	drainAsync(t)
	if st, ok := resyncCachedState(set.Id); !ok || st.iface != "b4new0" {
		t.Fatalf("a batch queued before a newer answer rebuilt the set moved it back onto %q, want b4new0", st.iface)
	}
	if !adds.has("198.51.100.104") {
		t.Fatal("the queued batch was released without its address although the set still exists with the same targets")
	}
}

func TestResyncAnswerAfterTheResyncDoesNotJoinAPreResyncBatch(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	recordResyncAdds(be)
	release := holdAsyncWorker(t)

	before := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.92")}))
	same := resyncSetCopy(set)
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{same}
	RoutingSyncConfig(next)
	after := resyncOne(t, RoutingHandleDNSAwait(next, same, []net.IP{net.ParseIP("198.51.100.93")}))
	release()
	awaitClosed(t, before, "the pre-resync batch")
	awaitClosed(t, after, "the post-resync answer")
	if before == after {
		t.Fatal("an answer after the resync joined the batch queued before it")
	}
}

func TestResyncThatChangesNothingStillAddsTheQueuedAddress(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	adds := recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.94")}))
	RoutingSyncConfig(cfg)
	release()
	awaitClosed(t, wait, "the queued batch")
	drainAsync(t)
	if !adds.has("198.51.100.94") {
		t.Fatal("a resync that changed nothing released the held answer without adding its address")
	}
}

func TestResyncWithAnEqualNewConfigStillAddsTheQueuedAddress(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	adds := recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.100")}))
	same := resyncSetCopy(set)
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{same}
	RoutingSyncConfig(next)
	release()
	awaitClosed(t, wait, "the queued batch")
	drainAsync(t)
	if !adds.has("198.51.100.100") {
		t.Fatal("a resync to an equal config released the held answer without adding its address")
	}
}

func TestResyncRebuildByOneBatchDoesNotDropTheNextBatchForTheSameSet(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	adds := recordResyncAdds(be)
	old := routeRuleCache[set.Id]
	old.iface = "b4old0"
	routeRuleCache[set.Id] = old
	release := holdAsyncWorker(t)

	first := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.95")}))
	routeMu.Lock()
	release()
	waitUntil(t, "the first batch to be sealed", func() bool {
		routeAsyncSeenMu.Lock()
		defer routeAsyncSeenMu.Unlock()
		return routeAsyncOpen[set.Id] == nil
	})
	second := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.96")}))
	routeMu.Unlock()
	awaitClosed(t, first, "the first batch")
	awaitClosed(t, second, "the second batch")
	drainAsync(t)
	if !adds.has("198.51.100.95") {
		t.Fatal("the rebuilding batch did not add its own address")
	}
	if !adds.has("198.51.100.96") {
		t.Fatal("a batch queued while another batch rebuilt the same set was released without its address")
	}
}

func TestResyncQueuedBatchDoesNotReinstallAfterClearAll(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.101")}))
	RoutingClearAll()
	routeMu.Lock()
	routeEngine = be
	routeMu.Unlock()
	release()
	awaitClosed(t, wait, "the queued batch")
	drainAsync(t)
	if _, ok := resyncCachedState(set.Id); ok {
		t.Fatal("a batch queued before RoutingClearAll reinstalled the set after everything was torn down")
	}
}

func TestResyncDroppedTargetsAreNotAddedToTheNewSet(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	looked := make(chan struct{}, 8)
	stubSetLookup(t, func(dns.Server, string) ([]net.IP, error) {
		looked <- struct{}{}
		return nil, dns.ErrNoAddress
	})
	set.DNS.Enabled = true
	set.DNS.TargetDNS = "192.0.2.53"
	set.Targets.SNIDomains = []string{"old.example"}
	adds := recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.102")}))
	retargeted := resyncSetCopy(set)
	retargeted.Targets.SNIDomains = []string{"new.example"}
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{retargeted}
	RoutingSyncConfig(next)
	awaitClosed(t, looked, "the pre-resolution of the new target")
	release()
	awaitClosed(t, wait, "the queued batch")
	drainAsync(t)
	if adds.has("198.51.100.102") {
		t.Fatal("an address answered for the old targets was added after the resync changed them")
	}
}

func resyncGeositeReload(t *testing.T, ip string, before, after []string) bool {
	t.Helper()
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	set.Targets.GeoSiteCategories = []string{"cat-a"}
	set.Targets.DomainsToMatch = before
	resyncSeedState(cfg, set)
	adds := recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP(ip)}))
	reloaded := resyncSetCopy(set)
	reloaded.Targets.DomainsToMatch = after
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{reloaded}
	RoutingSyncConfig(next)
	release()
	awaitClosed(t, wait, "the queued answer")
	drainAsync(t)
	return adds.has(ip)
}

func TestResyncGeositeReloadThatDroppedADomainDropsTheQueuedAnswer(t *testing.T) {
	if resyncGeositeReload(t, "198.51.100.111", []string{"a.example", "b.example"}, []string{"b.example"}) {
		t.Fatal("an answer queued before a geosite reload took a domain out of the category was still added to the set")
	}
}

func TestResyncGeositeReloadThatOnlyAddedDomainsKeepsTheQueuedAnswer(t *testing.T) {
	if !resyncGeositeReload(t, "198.51.100.112", []string{"a.example", "b.example"}, []string{"a.example", "b.example", "c.example"}) {
		t.Fatal("an answer queued before a geosite reload that only added domains was released without its address")
	}
}

func TestResyncChangedSetStillGetsTheQueuedAddress(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stubRetryState(t)
	adds := recordResyncAdds(be)
	release := holdAsyncWorker(t)

	wait := resyncOne(t, RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.103")}))
	moved := resyncSetCopy(set)
	moved.Routing.EgressInterface = "b4new0"
	next := familyTestConfig(true, false)
	next.Sets = []*config.SetConfig{moved}
	RoutingSyncConfig(next)
	release()
	awaitClosed(t, wait, "the queued batch")
	drainAsync(t)
	if st, ok := resyncCachedState(set.Id); !ok || st.iface != "b4new0" {
		t.Fatalf("the set ended on %q, want b4new0", st.iface)
	}
	if !adds.has("198.51.100.103") {
		t.Fatal("the held answer was released without its address although the set still exists with the same targets")
	}
}
