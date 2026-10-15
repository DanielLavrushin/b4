package tables

import (
	"errors"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type localAddrStub struct {
	mu    sync.Mutex
	addrs []net.Addr
	dumps atomic.Int32
}

func (s *localAddrStub) set(addrs ...net.Addr) {
	s.mu.Lock()
	s.addrs = addrs
	s.mu.Unlock()
}

func stubLocalAddrs(t *testing.T, addrs ...net.Addr) *localAddrStub {
	t.Helper()
	s := &localAddrStub{addrs: addrs}
	prev := routeHostAddrs
	t.Cleanup(func() { routeHostAddrs = prev })
	routeHostAddrs = func() ([]net.Addr, error) {
		s.dumps.Add(1)
		s.mu.Lock()
		defer s.mu.Unlock()
		return append([]net.Addr(nil), s.addrs...), nil
	}
	return s
}

func refuseAddrTypeMatch(t *testing.T) *[]string {
	t.Helper()
	var lines []string
	prevRun, prevLogged := run, runLogged
	t.Cleanup(func() { run, runLogged = prevRun, prevLogged })
	run = func(args ...string) (string, error) {
		line := strings.Join(args, " ")
		lines = append(lines, line)
		if strings.Contains(line, "addrtype") || strings.Contains(line, "fib daddr") {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		return "", nil
	}
	runLogged = func(op string, args ...string) bool {
		_, err := run(args...)
		return err == nil
	}
	return &lines
}

func TestTheLocalAddressListIsWatchedOnlyWhileItIsInUse(t *testing.T) {
	localGuardReset(t)
	stubBinaries(t, backendIPTables)
	addrs := stubLocalAddrs(t, testCIDR(t, "192.168.1.1/24"))
	prevRun := run
	t.Cleanup(func() { run = prevRun })
	run = func(args ...string) (string, error) { return "", nil }

	routeAddLocalDestinationGuard(&routeIptBackend{}, "b4r_lw_pre", true, false)
	addrs.dumps.Store(0)
	if routeLocalGuardListsMoved() || addrs.dumps.Load() != 0 {
		t.Fatalf("the address-type match loaded, so no address list is in use and a monitor tick must not read the addresses (%d dumps)", addrs.dumps.Load())
	}

	lines := refuseAddrTypeMatch(t)
	routeAddLocalDestinationGuard(&routeIptBackend{}, "b4r_lw2_pre", true, false)
	if firstLineWith(*lines, "-A b4r_lw2_pre -d 192.168.1.1/32 -j RETURN") < 0 {
		t.Fatalf("the rejected match must fall back to the address list:\n%s", strings.Join(*lines, "\n"))
	}
	if routeLocalGuardListsMoved() {
		t.Fatal("the addresses did not change, yet the list counts as moved")
	}
	addrs.set(testCIDR(t, "192.168.1.7/24"))
	if !routeLocalGuardListsMoved() {
		t.Fatal("the router's address changed while a chain still lists the old one, and nothing noticed")
	}
	routeLocalGuardForgetLists()
	if routeLocalGuardListsMoved() {
		t.Fatal("a forced resync rebuilds every list, so the old ones must be forgotten")
	}
}

func TestTheMonitorRebuildsTheSetsWhenTheListedAddressesChange(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	gwIsolate(t)
	gwStubCommands(t)
	stubBinaries(t, "ip", backendIPTables)
	gwStubHost(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	addrs := stubLocalAddrs(t, testCIDR(t, "192.168.1.1/24"))
	lines := refuseAddrTypeMatch(t)
	routeEngine = &mockRouteBackend{}
	cfg := familyTestConfig(true, false)
	set := gwTestSet("listed", "lo", "192.0.2.1")
	cfg.Sets = []*config.SetConfig{set}
	RoutingSyncConfig(cfg)
	st := gwCachedState(t, set.Id)
	if !routeGuardNarrowed(st) {
		t.Fatalf("the gateway set must carry the narrowed guard, which leaves router-addressed traffic alone, peers %v", st.gwPeersV4)
	}
	old := "-A " + st.chainPre + " -d 192.168.1.1/32 -j RETURN"
	fresh := "-A " + st.chainPre + " -d 192.168.1.7/32 -j RETURN"
	if countLinesWith(*lines, old) != 1 {
		t.Fatalf("the set must list the router's address while the address-type match is refused:\n%s", strings.Join(*lines, "\n"))
	}

	m, _ := newLockTestMonitor(t)
	if m.reconcileRouting(false) {
		t.Fatal("nothing changed, yet the monitor rebuilt routing")
	}
	addrs.set(testCIDR(t, "192.168.1.7/24"))
	if !m.reconcileRouting(false) {
		t.Fatal("the router's address changed under a set that lists it, and the monitor left the stale list in place")
	}
	if countLinesWith(*lines, fresh) != 1 {
		t.Errorf("the rebuilt set must list the new address:\n%s", strings.Join(*lines, "\n"))
	}
	if m.reconcileRouting(false) {
		t.Error("the list was rebuilt, yet the next tick rebuilt routing again")
	}
	if countLinesWith(*lines, fresh) != 1 {
		t.Errorf("an unchanged address was listed again:\n%s", strings.Join(*lines, "\n"))
	}
}

func TestARemovedSetsAddressListDoesNotKeepTheMonitorRebuilding(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	gwIsolate(t)
	gwStubCommands(t)
	stubBinaries(t, "ip", backendIPTables)
	gwStubHost(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	addrs := stubLocalAddrs(t, testCIDR(t, "192.168.1.1/24"))
	refuseAddrTypeMatch(t)
	routeEngine = &mockRouteBackend{}
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{gwTestSet("gone", "lo", "192.0.2.1")}
	RoutingSyncConfig(cfg)

	without := familyTestConfig(true, false)
	plain := gwTestSet("stays", "lo", "")
	plain.Routing.FWMark, plain.Routing.Table = 0x7e20, 233
	without.Sets = []*config.SetConfig{plain}
	RoutingSyncConfig(without)

	m, _ := newLockTestMonitor(t)
	m.reconcileRouting(false)
	addrs.set(testCIDR(t, "192.168.1.7/24"))
	if m.reconcileRouting(false) {
		t.Fatal("the first tick after the address change rebuilt routing for a list only the removed set carried")
	}
	for tick := 1; tick <= 3; tick++ {
		if m.reconcileRouting(false) {
			t.Fatalf("tick %d rebuilt routing for an address list no installed set carries any more", tick)
		}
	}
}

func TestTheAddressListWarningIsNotAboutProxySetsOnly(t *testing.T) {
	localGuardReset(t)
	logs := captureTablesLog(t)
	routeLocalGuardReject("nft", "nft_fib_inet", "Error: Could not process rule: No such file or directory")
	got := logs.String()
	if !strings.Contains(got, "rejected the address-type match") {
		t.Fatalf("the rejection was not reported:\n%s", got)
	}
	if strings.Contains(got, "proxy set") {
		t.Errorf("gateway sets use the same list, so the warning must not speak of proxy sets alone:\n%s", got)
	}
	if !strings.Contains(got, "while the tables monitor runs") {
		t.Errorf("with monitor_interval at 0 nothing watches the addresses, so the warning must not promise a rebuild on every address change:\n%s", got)
	}
}

func TestTheAddressListIsWatchedForTheRulesTheChainStillCarries(t *testing.T) {
	familyResetGlobals(t)
	gwIsolate(t)
	fw := stubRevertFirewall(t)
	stubBinaries(t, "ip", "nft")
	gwStubHost(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	addrs := stubLocalAddrs(t, testCIDR(t, "192.168.1.1/24"))
	firewall := run
	run = func(args ...string) (string, error) {
		if strings.Contains(strings.Join(args, " "), "fib daddr") {
			return "Error: Could not process rule: No such file or directory", errors.New("exit status 1")
		}
		return firewall(args...)
	}
	be := &routeNftBackend{}
	cfg := familyTestConfig(true, false)
	set := gwTestSet("relisted", "lo", "192.0.2.1")
	cfg.Sets = []*config.SetConfig{set}
	st := gwResolvedState(cfg, set)
	if !routeGuardNarrowed(st) {
		t.Fatalf("the gateway set must carry the narrowed guard, peers %v", st.gwPeersV4)
	}
	if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureRule: %v", err)
	}

	plain := gwTestSet("relisted", "lo", "")
	cfg.Sets = []*config.SetConfig{plain}
	without := gwResolvedState(cfg, plain)
	fw.refuseRules(`iifname "lo" return`)
	if err := routeEnsureRule(be, cfg, plain, without, nil); err == nil {
		t.Fatal("the guard was refused, yet the rebuild reported success")
	}
	addrs.set(testCIDR(t, "192.168.1.7/24"))
	if !routeLocalGuardListsMoved() {
		t.Fatal("the failed rebuild left the previous rules and their list of router addresses in place, yet an address change no longer counts")
	}

	addrs.set(testCIDR(t, "192.168.1.1/24"))
	fw.refuseRules("")
	if err := routeEnsureRule(be, cfg, plain, without, nil); err != nil {
		t.Fatalf("routeEnsureRule: %v", err)
	}
	addrs.set(testCIDR(t, "192.168.1.7/24"))
	if routeLocalGuardListsMoved() {
		t.Fatal("the set was rebuilt without the address list, yet a change of the router's addresses still rebuilds all routing")
	}
}

func TestAProxySetsAddressListFollowsTheRulesItsChainCarries(t *testing.T) {
	familyResetGlobals(t)
	localGuardReset(t)
	stubRevertFirewall(t)
	stubBinaries(t, "ip", "nft")
	prevSysctl := writeSysctl
	t.Cleanup(func() { writeSysctl = prevSysctl })
	writeSysctl = func(path, value string) {}
	proxyTableMu.Lock()
	chosen, resolved := proxyTableChosen, proxyTableResolved
	proxyTableMu.Unlock()
	t.Cleanup(func() {
		proxyTableMu.Lock()
		proxyTableChosen, proxyTableResolved = chosen, resolved
		proxyTableMu.Unlock()
	})
	addrs := stubLocalAddrs(t, testCIDR(t, "192.168.1.1/24"))
	cfg := familyTestConfig(true, false)
	set := orderTestSet("plisted", config.RoutingModeProxy, nil)
	cfg.Sets = []*config.SetConfig{set}
	st := proxyGuardState(0x5400)
	st.setID = set.Id
	st.setV4, st.setV6 = routeBuildSetNames(set.Id)
	st.chainPre, st.chainOut, st.chainSNAT = routeBuildChainNames(set.Id)
	st.chainQUIC = routeBuildQUICChainName(set.Id)
	st.quicReject = true
	refuseQUIC := false
	firewall := run
	run = func(args ...string) (string, error) {
		line := strings.Join(args, " ")
		if strings.Contains(line, "fib daddr") || (refuseQUIC && strings.Contains(line, "add chain inet "+routeNftTable+" "+st.chainQUIC)) {
			return "Error: Could not process rule: No such file or directory", errors.New("exit status 1")
		}
		return firewall(args...)
	}
	be := &routeNftBackend{}

	if err := routeEnsureProxyRule(be, cfg, set, st, nil); err != nil {
		t.Fatalf("routeEnsureProxyRule: %v", err)
	}
	addrs.set(testCIDR(t, "192.168.1.7/24"))
	refuseQUIC = true
	if err := routeEnsureProxyRule(be, cfg, set, st, nil); err == nil {
		t.Fatal("the QUIC chain was refused, yet the rebuild reported success")
	}
	if !routeLocalGuardListsMoved() {
		t.Fatal("the failed rebuild took its own rules back and left the previous ones, which list 192.168.1.1, yet the list counts as current")
	}

	routeCleanupProxyRule(be, st, false)
	if routeLocalGuardListsMoved() {
		t.Fatal("the proxy set's chain is gone, yet its address list still makes an address change rebuild all routing")
	}
}
