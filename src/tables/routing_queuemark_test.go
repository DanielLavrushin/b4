package tables

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func queueMarkTestConfig(mark uint) *config.Config {
	cfg := familyTestConfig(true, false)
	cfg.Queue.Mark = mark
	return cfg
}

func queueMarkPinProxyTable(t *testing.T) {
	t.Helper()
	proxyTableMu.Lock()
	chosen, resolved := proxyTableChosen, proxyTableResolved
	proxyTableChosen, proxyTableResolved = proxyLocalDeliveryTable, true
	proxyTableMu.Unlock()
	t.Cleanup(func() {
		proxyTableMu.Lock()
		proxyTableChosen, proxyTableResolved = chosen, resolved
		proxyTableMu.Unlock()
	})
}

func TestRouteStateEqual_TheQueueMarkIsPartOfTheState(t *testing.T) {
	familyResetGlobals(t)
	queueMarkPinProxyTable(t)

	for _, mode := range []string{config.RoutingModeInterface, config.RoutingModeProxy, config.RoutingModeMTProtoWS} {
		t.Run(mode, func(t *testing.T) {
			set := familyTestSet()
			set.Routing.Mode = mode
			set.Routing.Upstream.Host = "127.0.0.1"
			set.Routing.Upstream.Port = 1080
			before := buildRouteState(queueMarkTestConfig(0x8000), set)
			after := buildRouteState(queueMarkTestConfig(0x4000000), set)
			if routeStateEqual(before, after) {
				t.Errorf("the set's chains return on the queue mark, so a changed mark must rebuild them; with equal states the sync keeps returning on 0x8000 until the next firewall check")
			}
		})
	}

	t.Run(config.RoutingModeBlock, func(t *testing.T) {
		set := familyTestSet()
		set.Routing.Mode = config.RoutingModeBlock
		before := buildRouteState(queueMarkTestConfig(0x8000), set)
		after := buildRouteState(queueMarkTestConfig(0x4000000), set)
		if !routeStateEqual(before, after) {
			t.Errorf("a block set tests no mark, so a changed queue mark must leave it alone")
		}
	})
}

func TestRoutingSyncConfig_AChangedQueueMarkRebuildsTheSetRules(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("RoutingSyncConfig gives up before it touches a set when the ip binary is missing")
	}
	familyResetGlobals(t)

	set := familyTestSet()
	chainPre, chainOut, _ := routeBuildChainNames(set.Id)

	sync := func(mark uint) *mockRouteBackend {
		be := &mockRouteBackend{}
		routeEngine = be
		cfg := queueMarkTestConfig(mark)
		cfg.Sets = []*config.SetConfig{set}
		RoutingSyncConfig(cfg)
		return be
	}
	returnsOn := func(be *mockRouteBackend, chain string, mark uint32) bool {
		for _, m := range be.bypass[chain] {
			if m == mark {
				return true
			}
		}
		return false
	}

	first := sync(0x8000)
	if !returnsOn(first, chainPre, 0x8000) {
		t.Fatalf("the first sync must return on the queue mark in %s: %v", chainPre, first.bypass[chainPre])
	}

	moved := sync(0x4000000)
	for _, chain := range []string{chainPre, chainOut} {
		if !returnsOn(moved, chain, 0x4000000) {
			t.Errorf("after the queue mark moved to 0x4000000, %s was not rebuilt, so it keeps returning on 0x8000 and routes b4's own packets that carry the new mark: %v", chain, moved.bypass[chain])
		}
	}
	if routeRuleCache[set.Id].bypass != 0x4000000 {
		t.Errorf("the cached state still holds bypass 0x%x, so the next sync compares against the old mark", routeRuleCache[set.Id].bypass)
	}
}

func TestRoutingHandleDNS_AStaleConfigKeepsTheSyncedQueueMark(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("RoutingHandleDNS gives up before it touches a set when the ip binary is missing")
	}
	familyResetGlobals(t)

	set := familyTestSet()
	before := queueMarkTestConfig(0x8000)
	before.Sets = []*config.SetConfig{set}
	after := queueMarkTestConfig(0x4000000)
	after.Sets = []*config.SetConfig{set}

	routeEngine = &mockRouteBackend{}
	RoutingSyncConfig(before)
	routeEngine = &mockRouteBackend{}
	RoutingSyncConfig(after)

	setV4, _ := routeBuildSetNames(set.Id)
	var learned []string
	stale := &mockRouteBackend{addElementsFn: func(name string, ips []string, ttlSec int) {
		if name == setV4 {
			learned = append(learned, ips...)
		}
	}}
	routeEngine = stale
	RoutingHandleDNS(before, set, []net.IP{net.ParseIP("198.51.100.9")})

	if got := routeRuleCache[set.Id].bypass; got != 0x4000000 {
		t.Errorf("a DNS answer that still carried the configuration from before the save rebuilt the set back onto queue mark 0x%x", got)
	}
	chainPre, _, _ := routeBuildChainNames(set.Id)
	if len(stale.bypass[chainPre]) != 0 {
		t.Errorf("the stale answer rebuilt %s: %v", chainPre, stale.bypass[chainPre])
	}
	if len(learned) != 1 || learned[0] != "198.51.100.9" {
		t.Errorf("the stale answer's address must still reach %s, got %v", setV4, learned)
	}
}

func TestRoutingHandleDNS_AnAnswerAheadOfTheSyncStillReachesTheSet(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("RoutingHandleDNS gives up before it touches a set when the ip binary is missing")
	}
	familyResetGlobals(t)

	set := familyTestSet()
	synced := queueMarkTestConfig(0x8000)
	synced.Sets = []*config.SetConfig{set}
	routeEngine = &mockRouteBackend{}
	RoutingSyncConfig(synced)

	saved := familyTestSet()
	saved.DNS.Enabled = true
	saved.DNS.TargetDNS = "9.9.9.9"
	next := queueMarkTestConfig(0x4000000)
	next.Sets = []*config.SetConfig{saved}

	setV4, _ := routeBuildSetNames(set.Id)
	var learned []string
	be := &mockRouteBackend{addElementsFn: func(name string, ips []string, ttlSec int) {
		if name == setV4 {
			learned = append(learned, ips...)
		}
	}}
	routeEngine = be
	RoutingHandleDNS(next, saved, []net.IP{net.ParseIP("198.51.100.10")})

	if len(learned) != 1 || learned[0] != "198.51.100.10" {
		t.Errorf("an answer handled with the saved configuration before its routing sync lost its address, got %v", learned)
	}
	if got := routeRuleCache[set.Id].bypass; got != 0x8000 {
		t.Errorf("the answer rebuilt the set onto queue mark 0x%x ahead of the sync", got)
	}
	chainPre, _, _ := routeBuildChainNames(set.Id)
	if len(be.bypass[chainPre]) != 0 {
		t.Errorf("the answer rebuilt %s ahead of the sync: %v", chainPre, be.bypass[chainPre])
	}
}

func TestRoutingSyncConfig_AHandSetMarkInsideTheQueueMarkLeavesTheAutoSetAlone(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("RoutingSyncConfig gives up before it touches a set when the ip binary is missing")
	}
	familyResetGlobals(t)

	manual := familyTestSet()
	manual.Id = "famtest-manual"
	manual.Name = "famtestmanual"
	manual.Routing.FWMark = 0x100
	manual.Routing.Table = 232
	auto := familyTestSet()
	auto.Id = "famtest-auto"
	auto.Name = "famtestauto"
	auto.Routing.FWMark = 0
	auto.Routing.Table = 0
	auto.Targets.IpsToMatch = []string{"198.51.100.17"}

	cfg := queueMarkTestConfig(0x8100)
	cfg.Sets = []*config.SetConfig{manual, auto}
	marks := map[uint32]int{}
	for i := 0; i < 30; i++ {
		routeEngine = &mockRouteBackend{}
		RoutingSyncConfig(cfg)
		marks[routeRuleCache[auto.Id].mark]++
	}
	if len(marks) != 1 {
		t.Errorf("the automatic set kept changing its mark between syncs, rebuilding its rules each time: %v", marks)
	}
}

func TestRoutingSyncConfig_AQueueMarkCoveringAnAutoMarkMovesTheSet(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("RoutingSyncConfig gives up before it touches a set when the ip binary is missing")
	}
	familyResetGlobals(t)

	set := familyTestSet()
	set.Routing.FWMark = 0
	set.Routing.Table = 0

	first := queueMarkTestConfig(0x8000)
	first.Sets = []*config.SetConfig{set}
	routeEngine = &mockRouteBackend{}
	RoutingSyncConfig(first)
	auto := routeRuleCache[set.Id].mark
	if auto == 0 {
		t.Skip("no routing table was free for an automatic mark on this host")
	}

	moved := queueMarkTestConfig(uint(auto) | 0x10000)
	if err := moved.Validate(); err != nil {
		t.Fatalf("queue mark 0x%x should be accepted: %v", uint(auto)|0x10000, err)
	}
	moved.Sets = []*config.SetConfig{set}
	routeEngine = &mockRouteBackend{}
	RoutingSyncConfig(moved)

	if routeRuleCache[set.Id].mark == auto {
		t.Errorf("the set kept route mark 0x%x, which queue mark 0x%x carries in its per-set bits, so every packet b4 injects follows the set's ip rule", auto, uint32(moved.Queue.Mark))
	}
}

func TestRouteResolveIDs_AHandSetMarkEqualToTheQueueMarksSetBitsIsReplaced(t *testing.T) {
	if !hasBinary("ip") {
		t.Skip("routeResolveIDs looks up routing tables with the ip binary")
	}
	familyResetGlobals(t)

	set := familyTestSet()
	set.Routing.FWMark = 0x100
	if mark, _ := routeResolveIDs(queueMarkTestConfig(0x8100), set); mark == 0x100 {
		t.Errorf("hand-set fwmark 0x100 equals queue mark 0x8100 under 0x%x, so every packet b4 injects follows the set's ip rule; the mark must be replaced", routeSetMarkMask)
	}
	if mark, table := routeResolveIDs(queueMarkTestConfig(0x8000), set); mark != 0x100 || table != 232 {
		t.Errorf("a hand-set mark clear of the queue mark must be kept, got 0x%x and table %d", mark, table)
	}
}
