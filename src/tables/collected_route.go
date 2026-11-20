package tables

import (
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
)

type CollectedRoute struct {
	IPv4, IPv6 bool
	Router     bool
	Port       uint16
}

var routeCollectedRoutes atomic.Pointer[map[string]CollectedRoute]

func CollectedRouteOf(setID string) (CollectedRoute, bool) {
	if p := routeCollectedRoutes.Load(); p != nil {
		route, ok := (*p)[setID]
		return route, ok
	}
	return CollectedRoute{}, false
}

func routeCollectedRouteOf(st routeState) (CollectedRoute, bool) {
	route := CollectedRoute{IPv4: st.ipv4, IPv6: st.ipv6}
	switch {
	case !st.collected || config.RoutingIsBlock(st.mode):
		return route, false
	case config.RoutingUsesTProxy(st.mode):
		if st.mode == config.RoutingModeWSReuse && !st.diverts {
			return route, false
		}
		route.Router = routeWantsOutputJump(st)
		if ports := proxyModePorts(st.mode); len(ports) > 0 {
			route.Port = uint16(ports[0])
		}
		return route, true
	case st.iface == "" || !routeIfaceWasSeen(st.iface):
		return route, false
	}
	route.Router = st.routerOut
	return route, true
}

func routePublishCollectedRoutesLocked() {
	routes := make(map[string]CollectedRoute)
	for id, st := range routeRuleCache {
		if route, ok := routeCollectedRouteOf(st); ok {
			routes[id] = route
		}
	}
	routeCollectedRoutes.Store(&routes)
}
