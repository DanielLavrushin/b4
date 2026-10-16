package tables

import (
	"errors"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestTheRulesReportTheBackendTheyWereAppliedWith(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendIPTablesLegacy, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			var got []string
			prev := BackendAppliedFunc
			BackendAppliedFunc = func(b string) { got = append(got, b) }
			t.Cleanup(func() { BackendAppliedFunc = prev })

			cfg := goldenConfig(backend, config.DSCPConfig{})
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			if len(got) != 1 || got[0] != backend {
				t.Fatalf("reported %q, want [%s]: the packet engine decides which marks to check for another route by the backend in use", got, backend)
			}
		})
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
	prev, had := xtVariantCache.Load(backendIPTables)
	t.Cleanup(func() {
		if had {
			xtVariantCache.Store(backendIPTables, prev)
		} else {
			xtVariantCache.Delete(backendIPTables)
		}
	})
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
	prevSnap := routeMarksByIface.Load()
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

func TestSkippedRulesReportNoBackend(t *testing.T) {
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	called := false
	prev := BackendAppliedFunc
	BackendAppliedFunc = func(string) { called = true }
	t.Cleanup(func() { BackendAppliedFunc = prev })

	cfg := goldenConfig(backendIPTables, config.DSCPConfig{})
	cfg.System.Tables.SkipSetup = true
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	if called {
		t.Fatal("with firewall setup skipped no backend was applied, so none may be reported")
	}
}
