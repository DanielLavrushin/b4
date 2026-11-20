package tables

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestTheCollectedRoutesFollowWhatTheRulesTake(t *testing.T) {
	familyResetGlobals(t)
	seen := routeIfaceSeen
	routeIfaceSeen = map[string]bool{"wg0": true}
	t.Cleanup(func() { routeIfaceSeen = seen })
	both := func(st routeState) routeState {
		st.collected, st.ipv4, st.ipv6 = true, true, true
		return st
	}
	routeMu.Lock()
	routeRuleCache = map[string]routeState{
		"proxy":          both(routeState{mode: config.RoutingModeProxy}),
		"proxy-scoped":   both(routeState{mode: config.RoutingModeProxy, srcScoped: true}),
		"mtproto":        both(routeState{mode: config.RoutingModeMTProtoWS}),
		"reuse":          both(routeState{mode: config.RoutingModeWSReuse, diverts: true, tproxied: true}),
		"reuse-refused":  both(routeState{mode: config.RoutingModeWSReuse, diverts: true}),
		"reuse-idle":     both(routeState{mode: config.RoutingModeWSReuse}),
		"iface":          both(routeState{mode: config.RoutingModeInterface, iface: "wg0", routerOut: true}),
		"iface-lan-only": both(routeState{mode: config.RoutingModeInterface, iface: "wg0"}),
		"iface-unseen":   both(routeState{mode: config.RoutingModeInterface, iface: "wg9", routerOut: true}),
		"block":          both(routeState{mode: config.RoutingModeBlock}),
		"plain":          {mode: config.RoutingModeProxy, ipv4: true},
		"v4-only":        {mode: config.RoutingModeProxy, collected: true, ipv4: true},
	}
	routePublishMarksLocked()
	routeMu.Unlock()

	for id, want := range map[string]*CollectedRoute{
		"proxy":          {IPv4: true, IPv6: true, Router: true},
		"proxy-scoped":   {IPv4: true, IPv6: true},
		"mtproto":        {IPv4: true, IPv6: true, Router: true},
		"reuse":          {IPv4: true, IPv6: true, Router: true, Port: 443},
		"reuse-refused":  nil,
		"reuse-idle":     nil,
		"iface":          {IPv4: true, IPv6: true, Router: true},
		"iface-lan-only": {IPv4: true, IPv6: true},
		"iface-unseen":   nil,
		"block":          nil,
		"plain":          nil,
		"v4-only":        {IPv4: true, Router: true},
		"missing":        nil,
	} {
		got, ok := CollectedRouteOf(id)
		switch {
		case want == nil && ok:
			t.Errorf("%s: published %+v; its rules take no connection (not a collected consumer, a block set, a reuse set whose diversion is off or whose TPROXY rule was refused, or an interface that never appeared and so has no ip rule)", id, got)
		case want != nil && (!ok || got != *want):
			t.Errorf("%s: published %+v (published=%v), want %+v", id, got, ok, *want)
		}
	}
}

func TestACollectedPublishNeverMarksAnInterfaceSeenWithoutItsRule(t *testing.T) {
	familyResetGlobals(t)
	seen, routes := routeIfaceSeen, routeSetRoutes.Load()
	t.Cleanup(func() {
		routeMu.Lock()
		routeIfaceSeen = seen
		routeMu.Unlock()
		routeSetRoutes.Store(routes)
	})
	routeMu.Lock()
	routeIfaceSeen = map[string]bool{}
	routeRuleCache = map[string]routeState{"late": {setID: "late", mode: config.RoutingModeInterface, iface: "lo", mark: 0x2a, table: 200, collected: true, ipv4: true, routerOut: true}}
	routeMu.Unlock()
	for publish := 1; publish <= 2; publish++ {
		routeMu.Lock()
		routePublishMarksLocked()
		marked := routeIfaceSeen["lo"]
		routeMu.Unlock()
		route, _ := RoutingSetRoute("late")
		_, taken := CollectedRouteOf("late")
		if marked || !route.RuleMissing || taken {
			t.Errorf("publish %d: interface marked seen=%v, RuleMissing=%v, collected route taken=%v; want false, true, false: no sync has added the set's ip rule for lo, so its connections leave by the uplink and b4's Telegram path must not trust a rule that is not there", publish, marked, route.RuleMissing, taken)
		}
	}
}
