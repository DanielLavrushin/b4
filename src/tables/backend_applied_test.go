package tables

import (
	"errors"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func recordOwnPacketsShield(t *testing.T) *[]bool {
	t.Helper()
	var got []bool
	prev := OwnPacketsShieldFunc
	OwnPacketsShieldFunc = func(shielded bool) { got = append(got, shielded) }
	t.Cleanup(func() { OwnPacketsShieldFunc = prev })
	return &got
}

func stubNftTableListing(t *testing.T, listing *string) {
	t.Helper()
	hostRun := run
	run = func(args ...string) (string, error) {
		if slices.Equal(args, []string{"nft", "list", "tables"}) {
			return *listing, nil
		}
		return hostRun(args...)
	}
	t.Cleanup(func() { run = hostRun })
}

func TestAppliedRulesPublishWhetherB4sOwnPacketsAreShielded(t *testing.T) {
	for _, c := range []struct {
		backend string
		want    bool
	}{
		{backendIPTablesLegacy, true},
		{backendNFTables, false},
	} {
		t.Run(c.backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			listing := ""
			stubNftTableListing(t, &listing)
			got := recordOwnPacketsShield(t)

			if err := AddRules(goldenConfig(c.backend, config.DSCPConfig{})); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			if !slices.Equal(*got, []bool{c.want}) {
				t.Fatalf("published %v, want [%v]: the packet engine checks every mark for another route only while b4's OUTPUT accept shields its own packets", *got, c.want)
			}
		})
	}
}

func TestEveryMonitorCheckLooksAgainForNftablesTablesOfOtherServices(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	listing := ""
	stubNftTableListing(t, &listing)
	cfg := goldenConfig(backendIPTablesLegacy, config.DSCPConfig{})
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	got := recordOwnPacketsShield(t)
	var ptr atomic.Pointer[config.Config]
	ptr.Store(cfg)
	m := &Monitor{cfgPtr: &ptr, backend: backendNFTables}

	for _, listed := range []string{"table inet fw4\n", ""} {
		listing = listed
		if _, restored := m.ensureRules(false); restored {
			t.Fatalf("the monitor rebuilt rules that were in place")
		}
	}
	if want := []bool{false, true}; !slices.Equal(*got, want) {
		t.Fatalf("published %v, want %v: another service can add or drop its nftables tables while b4's legacy rules stay in place", *got, want)
	}
}

func TestAMonitorCheckThatLeavesTheRebuildToARefreshStillLooksForNftablesTables(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	listing := ""
	stubNftTableListing(t, &listing)
	applied := goldenConfig(backendIPTablesLegacy, config.DSCPConfig{})
	if err := AddRules(applied); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	got := recordOwnPacketsShield(t)
	saved := goldenConfig(backendIPTablesLegacy, config.DSCPConfig{})
	saved.Queue.Mark = 0x4000000
	var ptr atomic.Pointer[config.Config]
	ptr.Store(saved)
	m := &Monitor{cfgPtr: &ptr, backend: backendNFTables}
	activeSteering = &discoverySteering{}

	for _, listed := range []string{"table inet fw4\n", ""} {
		listing = listed
		if _, acted := m.ensureRules(false); !acted || m.pendingApply != saved {
			t.Fatalf("the check did not leave the new queue mark to the refresh that waits for the Discovery run")
		}
	}
	if want := []bool{false, true}; !slices.Equal(*got, want) {
		t.Fatalf("published %v, want %v: b4's applied rules stay in place for the whole Discovery run, and another service can add or drop its nftables tables meanwhile", *got, want)
	}
}

func TestATUNMonitorCheckPublishesNothing(t *testing.T) {
	stubTUNFirewall(t, nil)
	got := recordOwnPacketsShield(t)
	newTUNTestMonitor(newTUNTestConfig()).ensureRules(false)
	if len(*got) != 0 {
		t.Fatalf("published %v: in TUN mode b4 queues no packet whose foreign mark it could check", *got)
	}
}

func TestSkippedRulesPublishNothing(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	got := recordOwnPacketsShield(t)

	cfg := goldenConfig(backendIPTables, config.DSCPConfig{})
	cfg.System.Tables.SkipSetup = true
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	if len(*got) != 0 {
		t.Fatalf("published %v: with firewall setup skipped b4 applied no rules", *got)
	}
}

func stubSyncMapEntry(t *testing.T, m interface {
	Load(any) (any, bool)
	Store(any, any)
	Delete(any)
}, key string, value any) {
	t.Helper()
	prev, had := m.Load(key)
	m.Store(key, value)
	t.Cleanup(func() {
		if had {
			m.Store(key, prev)
		} else {
			m.Delete(key)
		}
	})
}

func TestNftablesTablesOfOtherServicesLeaveB4sOwnPacketsUnshielded(t *testing.T) {
	stubSyncMapEntry(t, &hasBinaryCache, "nft", true)
	prevRun := run
	t.Cleanup(func() { run = prevRun })
	for _, c := range []struct {
		name    string
		listing string
		err     error
		want    bool
	}{
		{"only b4's own tables", "table inet b4_mangle\ntable inet b4_route\ntable ip _b4_queue_probe\n", nil, true},
		{"tables outside the IP families", "table bridge filter\ntable netdev ingress\ntable arp filter\n", nil, true},
		{"OpenWrt's fw4", "table inet fw4\ntable inet b4_mangle\n", nil, false},
		{"another service on iptables-nft", "table ip mangle\n", nil, false},
		{"an IPv6 table", "table ip6 b4r\n", nil, false},
		{"nft that cannot list tables", "", errors.New("no nf_tables"), true},
	} {
		t.Run(c.name, func(t *testing.T) {
			run = func(args ...string) (string, error) { return c.listing, c.err }
			if got := OwnPacketsShielded(backendIPTablesLegacy); got != c.want {
				t.Fatalf("%v, want %v: an nftables chain of another service sees b4's packets whatever b4's legacy OUTPUT accept does", got, c.want)
			}
		})
	}
}

func TestOnlyLegacyIPTablesShieldsB4sOwnPackets(t *testing.T) {
	stubSyncMapEntry(t, &hasBinaryCache, "nft", false)
	stubSyncMapEntry(t, &xtVariantCache, backendIPTables, false)
	for _, c := range []struct {
		backend string
		nft     bool
		want    bool
	}{
		{backendIPTablesLegacy, false, true},
		{backendIPTables, false, true},
		{backendIPTables, true, false},
		{backendNFTables, false, false},
		{"external", false, false},
		{"", false, false},
	} {
		xtVariantCache.Store(backendIPTables, c.nft)
		if got := OwnPacketsShielded(c.backend); got != c.want {
			t.Errorf("%q (nft variant %v): %v, want %v: b4's OUTPUT accept stops other services' rules only when they share its legacy chain", c.backend, c.nft, got, c.want)
		}
	}
}

func TestTheRouteMarksOfInterfaceSetsArePublished(t *testing.T) {
	prevSnap := routeMarksByIface.Load()
	routeMu.Lock()
	prevCache := routeRuleCache
	routeRuleCache = map[string]routeState{
		"a": {mode: config.RoutingModeInterface, iface: "ppp9", mark: 0x2bd8},
		"b": {mode: config.RoutingModeInterface, iface: "ppp9", mark: 0x5aef},
		"c": {mode: config.RoutingModeInterface, iface: "wg0", mark: 0x1234},
		"d": {mode: config.RoutingModeProxy, iface: "ppp9", mark: 0x4321},
		"e": {mode: config.RoutingModeBlock, iface: "ppp9"},
	}
	routePublishMarksLocked()
	routeMu.Unlock()
	t.Cleanup(func() {
		routeMu.Lock()
		routeRuleCache = prevCache
		routeMu.Unlock()
		routeMarksByIface.Store(prevSnap)
	})

	got := RoutedIfaceMarks("ppp9")
	if len(got) != 2 || !((got[0] == 0x2bd8 && got[1] == 0x5aef) || (got[0] == 0x5aef && got[1] == 0x2bd8)) {
		t.Fatalf("ppp9 marks %#x, want 0x2bd8 and 0x5aef", got)
	}
	if got := RoutedIfaceMarks("wg0"); len(got) != 1 || got[0] != 0x1234 {
		t.Fatalf("wg0 marks %#x, want 0x1234", got)
	}
	if got := RoutedIfaceMarks("eth7"); len(got) != 0 {
		t.Fatalf("eth7 marks %#x, want none", got)
	}
}
