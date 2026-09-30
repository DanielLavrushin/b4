package tables

import (
	"net"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type awaitAdd struct {
	setName string
	ips     []string
}

type awaitGate struct {
	release chan struct{}
	once    sync.Once
	adds    chan awaitAdd
	count   atomic.Int32
}

func (g *awaitGate) open() { g.once.Do(func() { close(g.release) }) }

func awaitSetup(t *testing.T, ipv6 bool) (*config.Config, *config.SetConfig, *mockRouteBackend) {
	t.Helper()
	familyResetGlobals(t)
	stubBinaries(t, "ip")
	origRun := run
	t.Cleanup(func() { run = origRun })
	run = func(args ...string) (string, error) { return "", nil }

	routeAsyncSeenMu.Lock()
	routeAsyncSeen = make(map[string]time.Time)
	routeAsyncPending = make(map[string]chan struct{})
	routeAsyncOpen = make(map[string]*routeAsyncBatch)
	routeAsyncSeenMu.Unlock()
	routeInstallFailedAt.Range(func(k, _ any) bool {
		routeInstallFailedAt.Delete(k)
		return true
	})

	cfg := familyTestConfig(true, ipv6)
	set := familyTestSet()
	cfg.Sets = []*config.SetConfig{set}
	be := &mockRouteBackend{}
	routeEngine = be
	routeRuleCache[set.Id] = buildRouteState(cfg, set)
	return cfg, set, be
}

func gateAdds(t *testing.T, be *mockRouteBackend) *awaitGate {
	t.Helper()
	g := &awaitGate{release: make(chan struct{}), adds: make(chan awaitAdd, 16)}
	be.addElementsFn = func(setName string, ips []string, ttlSec int) {
		g.count.Add(1)
		g.adds <- awaitAdd{setName: setName, ips: append([]string(nil), ips...)}
		<-g.release
	}
	t.Cleanup(g.open)
	return g
}

func awaitClosed(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatalf("%s: never signalled", what)
	}
}

func awaitOpen(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
		t.Fatalf("%s: signalled before the address was added", what)
	case <-time.After(50 * time.Millisecond):
	}
}

func nextAdd(t *testing.T, g *awaitGate) awaitAdd {
	t.Helper()
	select {
	case a := <-g.adds:
		return a
	case <-time.After(5 * time.Second):
		t.Fatal("the address never reached the firewall")
		return awaitAdd{}
	}
}

func TestRoutingHandleDNSAwaitSignalsOnceTheAddressIsInTheSet(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	g := gateAdds(t, be)

	waits := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("198.51.100.20")})
	if len(waits) != 1 {
		t.Fatalf("a new address must give the answer one thing to wait for, got %d", len(waits))
	}
	add := nextAdd(t, g)
	if add.setName != routeRuleCache[set.Id].setV4 || !reflect.DeepEqual(add.ips, []string{"198.51.100.20"}) {
		t.Fatalf("added %v to %q, want 198.51.100.20 in %q", add.ips, add.setName, routeRuleCache[set.Id].setV4)
	}
	awaitOpen(t, waits[0], "the wait")
	g.open()
	awaitClosed(t, waits[0], "the wait")
}

func TestRoutingHandleDNSAwaitJoinsAnUpdateAlreadyUnderWay(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	g := gateAdds(t, be)
	ip := []net.IP{net.ParseIP("198.51.100.21")}

	first := RoutingHandleDNSAwait(cfg, set, ip)
	nextAdd(t, g)
	second := RoutingHandleDNSAwait(cfg, set, ip)
	if len(first) != 1 || len(second) != 1 || first[0] != second[0] {
		t.Fatalf("a second answer for an address still being added must wait for that same update: first %v, second %v", first, second)
	}
	awaitOpen(t, second[0], "the second answer")
	g.open()
	awaitClosed(t, second[0], "the second answer")
	if n := g.count.Load(); n != 1 {
		t.Fatalf("the address was added %d times, want once", n)
	}
}

func TestRoutingHandleDNSAwaitDoesNotHoldAnAddressAlreadyInTheSet(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	g := gateAdds(t, be)
	g.open()
	ip := []net.IP{net.ParseIP("198.51.100.22")}

	first := RoutingHandleDNSAwait(cfg, set, ip)
	if len(first) != 1 {
		t.Fatalf("got %d waits for a new address, want 1", len(first))
	}
	awaitClosed(t, first[0], "the first answer")
	if again := RoutingHandleDNSAwait(cfg, set, ip); len(again) != 0 {
		t.Fatalf("an address added moments ago made the next answer wait: %v", again)
	}
}

func TestRoutingHandleDNSAwaitSkipsAddressesOfADisabledFamily(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	g := gateAdds(t, be)
	g.open()

	if waits := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("2001:db8::20")}); len(waits) != 0 {
		t.Fatalf("an IPv6-only answer with IPv6 off must queue nothing, got %d waits", len(waits))
	}
	routeAsyncSeenMu.Lock()
	_, claimed := routeAsyncSeen[set.Id+"|2001:db8::20"]
	routeAsyncSeenMu.Unlock()
	if claimed {
		t.Fatal("an IPv6 address was claimed while IPv6 is off")
	}

	waits := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("2001:db8::21"), net.ParseIP("198.51.100.23")})
	if len(waits) != 1 {
		t.Fatalf("a mixed answer must wait for its IPv4 address only, got %d waits", len(waits))
	}
	add := nextAdd(t, g)
	awaitClosed(t, waits[0], "the mixed answer")
	if !reflect.DeepEqual(add.ips, []string{"198.51.100.23"}) {
		t.Fatalf("added %v, want only 198.51.100.23", add.ips)
	}
}

func TestRoutingHandleDNSAwaitKeepsIPv6WhenItIsOn(t *testing.T) {
	cfg, set, be := awaitSetup(t, true)
	g := gateAdds(t, be)
	g.open()

	waits := RoutingHandleDNSAwait(cfg, set, []net.IP{net.ParseIP("2001:db8::22")})
	if len(waits) != 1 {
		t.Fatalf("an IPv6 address with IPv6 on must be added, got %d waits", len(waits))
	}
	add := nextAdd(t, g)
	awaitClosed(t, waits[0], "the IPv6 answer")
	if add.setName != routeRuleCache[set.Id].setV6 || !reflect.DeepEqual(add.ips, []string{"2001:db8::22"}) {
		t.Fatalf("added %v to %q, want 2001:db8::22 in %q", add.ips, add.setName, routeRuleCache[set.Id].setV6)
	}
}

func TestRoutingHandleDNSAwaitSettlesWhenTheBacklogIsFull(t *testing.T) {
	cfg, set, _ := awaitSetup(t, false)
	routeAsyncStart()
	block := make(chan struct{})
	started := make(chan struct{})
	routeAsyncCh <- func() {
		close(started)
		<-block
	}
	t.Cleanup(func() {
		close(block)
		drained := make(chan struct{})
		routeAsyncCh <- func() { close(drained) }
		awaitClosed(t, drained, "the backlog")
	})
	awaitClosed(t, started, "the blocking job")
	for len(routeAsyncCh) < cap(routeAsyncCh) {
		routeAsyncCh <- func() {}
	}

	ip := []net.IP{net.ParseIP("198.51.100.24")}
	waits := RoutingHandleDNSAwait(cfg, set, ip)
	if len(waits) != 1 {
		t.Fatalf("got %d waits, want 1", len(waits))
	}
	select {
	case <-waits[0]:
	default:
		t.Fatal("a dropped update must release the answer at once instead of leaving it to the time limit")
	}
	routeAsyncSeenMu.Lock()
	_, pending := routeAsyncPending[set.Id+"|198.51.100.24"]
	routeAsyncSeenMu.Unlock()
	if pending {
		t.Fatal("a dropped update left its address marked as under way")
	}
	if fresh := routeAsyncClaim(set, ip); len(fresh) != 1 {
		t.Fatal("a dropped update must let the next answer claim the address again")
	}
}

func TestRoutingHandleDNSAsyncStillQueuesTheUpdate(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	g := gateAdds(t, be)
	g.open()

	ip := []net.IP{net.ParseIP("198.51.100.25")}
	RoutingHandleDNSAsync(cfg, set, ip)
	if add := nextAdd(t, g); !reflect.DeepEqual(add.ips, []string{"198.51.100.25"}) {
		t.Fatalf("added %v, want 198.51.100.25", add.ips)
	}
	for _, w := range RoutingHandleDNSAwait(cfg, set, ip) {
		awaitClosed(t, w, "the queued update")
	}
}

func TestRoutingLearnIPAsyncMakesAnswersForTheSameAddressWait(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	g := gateAdds(t, be)
	ip := net.ParseIP("198.51.100.26")

	RoutingLearnIPAsync(cfg, set, ip)
	nextAdd(t, g)
	waits := RoutingHandleDNSAwait(cfg, set, []net.IP{ip})
	if len(waits) != 1 {
		t.Fatalf("an answer for an address a ClientHello is teaching must wait for it, got %d waits", len(waits))
	}
	awaitOpen(t, waits[0], "the answer")
	g.open()
	awaitClosed(t, waits[0], "the answer")
	if n := g.count.Load(); n != 1 {
		t.Fatalf("the address was added %d times, want once", n)
	}
}

func TestRoutingHandleDNSAddsWithoutRebuildingWhenTheRulesAreInPlace(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	bases, chains := 0, 0
	be.ensureBaseFn = func() error { bases++; return nil }
	be.ensureChainFn = func(string, bool) error { chains++; return nil }

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.30")})

	if bases != 0 || chains != 0 {
		t.Fatalf("a set whose rules are in place re-ran its base (%d) or chain (%d) setup for one address", bases, chains)
	}
	if want := []string{"add " + routeRuleCache[set.Id].setV4}; !reflect.DeepEqual(be.setOps, want) {
		t.Fatalf("set operations %v, want %v", be.setOps, want)
	}
}

func TestRoutingHandleDNSStillBuildsTheRulesOfANewSet(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	delete(routeRuleCache, set.Id)
	bases := 0
	be.ensureBaseFn = func() error { bases++; return nil }

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.31")})

	if bases != 1 {
		t.Fatalf("the first answer for a set without rules ran its base setup %d times, want 1", bases)
	}
	if _, ok := routeRuleCache[set.Id]; !ok {
		t.Fatal("the first answer for a set without rules did not install them")
	}
}

func TestRoutingHandleDNSStillRebuildsASetWhoseStateChanged(t *testing.T) {
	cfg, set, be := awaitSetup(t, false)
	stale := routeRuleCache[set.Id]
	stale.iface = "b4old0"
	routeRuleCache[set.Id] = stale
	bases := 0
	be.ensureBaseFn = func() error { bases++; return nil }

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP("198.51.100.32")})

	if bases != 1 {
		t.Fatalf("a set whose state changed ran its base setup %d times, want 1", bases)
	}
	if got := routeRuleCache[set.Id].iface; got != set.Routing.EgressInterface {
		t.Fatalf("the rebuilt state points at %q, want %q", got, set.Routing.EgressInterface)
	}
}
