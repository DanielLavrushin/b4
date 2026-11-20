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
		"reuse":          both(routeState{mode: config.RoutingModeWSReuse, diverts: true}),
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
			t.Errorf("%s: published %+v; its rules take no connection (not a collected consumer, a block set, a reuse set whose diversion is off, or an interface that never appeared and so has no ip rule)", id, got)
		case want != nil && (!ok || got != *want):
			t.Errorf("%s: published %+v (published=%v), want %+v", id, got, ok, *want)
		}
	}
}
