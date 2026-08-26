package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func orderTestSet(id, mode string, devices []string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Name = id
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = mode
	set.Targets.SourceDevices = devices
	if mode == config.RoutingModeInterface {
		set.Routing.EgressInterface = "wg0"
	} else {
		set.Routing.Upstream.Host = "127.0.0.1"
		set.Routing.Upstream.Port = 2001
	}
	return &set
}

func withOrderedCache(t *testing.T, sets ...*config.SetConfig) *config.Config {
	t.Helper()

	origCache := routeRuleCache
	origKey := routeJumpOrderKey
	t.Cleanup(func() {
		routeRuleCache = origCache
		routeJumpOrderKey = origKey
	})
	routeRuleCache = make(map[string]routeState)
	routeJumpOrderKey = ""

	cfg := config.NewConfig()
	cfg.Sets = sets
	for _, set := range sets {
		mode := set.Routing.Mode
		routeRuleCache[set.Id] = routeState{
			mode:      mode,
			srcScoped: routeSetIsSourceScoped(set),
			iface:     set.Routing.EgressInterface,
			chainPre:  "b4r_" + set.Id + "_pre",
			chainOut:  "b4r_" + set.Id + "_out",
			chainSNAT: "b4r_" + set.Id + "_snat",
		}
	}
	return &cfg
}

func outJumpTargets(be *mockRouteBackend) []string {
	var out []string
	for _, j := range be.jumps {
		if j.baseChain == "OUTPUT" {
			out = append(out, j.targetChain)
		}
	}
	return out
}

func TestProxySetsKeepTheirConfiguredOrderInPrerouting(t *testing.T) {
	cfg := withOrderedCache(t,
		orderTestSet("static", config.RoutingModeProxy, nil),
		orderTestSet("telegram", config.RoutingModeProxy, nil),
		orderTestSet("direct", config.RoutingModeProxy, nil),
		orderTestSet("rudirect", config.RoutingModeProxy, nil),
	)

	be := &mockRouteBackend{}
	routeReestablishJumpOrder(be, cfg, true)

	got := strings.Join(preJumpTargets(be), ",")
	want := "b4r_static_pre,b4r_telegram_pre,b4r_direct_pre,b4r_rudirect_pre"
	if got != want {
		t.Errorf("prerouting jumps are %q; the first set in the config has to be tried first, and the chain used to come out reversed (issue #324)", got)
	}
}

func TestProxyJumpsAreAppendedNotPrepended(t *testing.T) {
	cfg := withOrderedCache(t,
		orderTestSet("first", config.RoutingModeProxy, nil),
		orderTestSet("second", config.RoutingModeProxy, nil),
	)

	be := &mockRouteBackend{}
	routeReestablishJumpOrder(be, cfg, true)

	for _, j := range be.jumps {
		if j.baseChain == "PREROUTING" && j.atTop {
			t.Errorf("jump to %s was inserted at the top; prepending each set in turn is what reverses the chain", j.targetChain)
		}
	}
}

func TestProxyAndInterfaceSetsShareOnePrecedenceOrder(t *testing.T) {
	cfg := withOrderedCache(t,
		orderTestSet("wide-proxy", config.RoutingModeProxy, nil),
		orderTestSet("phone-iface", config.RoutingModeInterface, []string{"AA:BB:CC:DD:EE:01"}),
		orderTestSet("wide-iface", config.RoutingModeInterface, nil),
		orderTestSet("phone-proxy", config.RoutingModeProxy, []string{"AA:BB:CC:DD:EE:02"}),
	)

	be := &mockRouteBackend{}
	routeReestablishJumpOrder(be, cfg, true)

	got := strings.Join(preJumpTargets(be), ",")
	want := "b4r_phone-iface_pre,b4r_phone-proxy_pre,b4r_wide-proxy_pre,b4r_wide-iface_pre"
	if got != want {
		t.Errorf("prerouting jumps are %q; device-scoped sets rank ahead of catch-all ones in both modes, and config order breaks the tie", got)
	}
}

func TestProxySetsKeepTheirConfiguredOrderInOutput(t *testing.T) {
	cfg := withOrderedCache(t,
		orderTestSet("static", config.RoutingModeProxy, nil),
		orderTestSet("telegram", config.RoutingModeProxy, nil),
		orderTestSet("rudirect", config.RoutingModeProxy, nil),
	)

	be := &mockRouteBackend{}
	routeReestablishJumpOrder(be, cfg, true)

	got := strings.Join(outJumpTargets(be), ",")
	want := "b4r_static_out,b4r_telegram_out,b4r_rudirect_out"
	if got != want {
		t.Errorf("output jumps are %q; traffic the router itself originates has to follow the same precedence as forwarded traffic", got)
	}
}

func TestReorderingSetsAloneRewritesTheJumps(t *testing.T) {
	first := orderTestSet("static", config.RoutingModeProxy, nil)
	second := orderTestSet("rudirect", config.RoutingModeProxy, nil)
	cfg := withOrderedCache(t, first, second)

	routeReestablishJumpOrder(&mockRouteBackend{}, cfg, true)

	cfg.Sets = []*config.SetConfig{second, first}
	be := &mockRouteBackend{}
	routeReestablishJumpOrder(be, cfg, false)

	got := strings.Join(preJumpTargets(be), ",")
	want := "b4r_rudirect_pre,b4r_static_pre"
	if got != want {
		t.Errorf("prerouting jumps are %q; dragging a set in the UI changes nothing else, so the reorder alone has to rewrite the chain", got)
	}
}

func TestProxyOutputChainIsWatchedByTheMonitor(t *testing.T) {
	st := routeState{
		mode:     config.RoutingModeProxy,
		chainPre: "pre",
		chainOut: "out",
	}
	var found bool
	for _, c := range routeStateChains(st) {
		if c.chain == "out" {
			found = true
			if !c.wantBypass {
				t.Error("the proxy output chain carries the self-dial bypass, so the monitor has to require it")
			}
		}
	}
	if !found {
		t.Error("the proxy output chain holds the mark rules for locally-originated traffic; the monitor has to restore it when it disappears")
	}

	scoped := routeState{mode: config.RoutingModeProxy, chainPre: "pre", chainOut: "out", srcScoped: true}
	for _, c := range routeStateChains(scoped) {
		if c.chain == "out" {
			t.Error("a source-scoped set never gets an output chain, so requiring it would make the monitor resync forever")
		}
	}
}
