package nfq

import (
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tables"
)

func installedCollectedRoute(cfg *config.Config, set *config.SetConfig) (tables.CollectedRoute, bool) {
	if cfg == nil || set == nil || !set.Enabled || !set.Targets.UseCollectedIPs {
		return tables.CollectedRoute{}, false
	}
	route := tables.CollectedRoute{IPv4: cfg.Queue.IPv4Enabled, IPv6: cfg.Queue.IPv6Enabled}
	switch {
	case set.ReusesWSSize():
		view := *set
		view.Routing = config.RoutingConfig{}
		route.Router, route.Port = !view.RoutingSourceScoped(), 443
	case !set.Routing.Enabled || config.RoutingIsBlock(set.Routing.Mode):
		return route, false
	case config.RoutingUsesTProxy(set.RoutingModeOrDefault()):
		route.Router = !set.RoutingSourceScoped()
	default:
		route.Router = set.RoutingIncludesRouterTraffic()
	}
	return route, true
}

func routesAsInstalled(t *testing.T, w *Worker) {
	t.Helper()
	stubCollectedRoutes(t, func(id string) (tables.CollectedRoute, bool) {
		cfg := w.getConfig()
		return installedCollectedRoute(cfg, cfg.GetSetById(id))
	})
}

func stubCollectedRoutes(t *testing.T, of func(string) (tables.CollectedRoute, bool)) {
	t.Helper()
	prev := collectedRouteOf
	collectedRouteOf = of
	t.Cleanup(func() { collectedRouteOf = prev })
}

func TestARouterConnectionCountsAsTakenOnlyWhereItsConsumerRoutesTheRouter(t *testing.T) {
	for _, routerTraffic := range []string{config.RouterTrafficExclude, config.RouterTrafficInclude} {
		t.Run(routerTraffic, func(t *testing.T) {
			idx := scopeIfaces(t)
			consumer := interfaceConsumer()
			consumer.Routing.RouterTraffic = routerTraffic
			r := newCollectReadyRig(t, consumer, readyProducer(), blockWatchSet())
			for i, sport := range []uint16{42000, 42001} {
				queuedFrom(t, r.w, silentHelloFrom(sport), idx["br0"], nil, nil)
				r.serverSends(t, sport, 0x10, nil)
				r.sweep(time.Duration(4+i) * time.Second)
			}
			if r.collected() != 1 {
				t.Fatalf("setup: candidates=%d installs=%d", r.collected(), r.installs)
			}
			blockHello := buildClientHello("www.block.example", 16, 0xAB)
			queuedFrom(t, r.w, clientPacket(43000, silentHelloSeq, 0x18, blockHello), 0, hookAt(nfHookLocalOut), nil)
			if !r.watched(43000) {
				t.Fatal("setup: the router's own Block connection is not watched")
			}
			r.w.ProcessPacket(clientPacketAcking(43000, silentHelloSeq+uint32(len(blockHello)), silentServerAck+1400, 0x10, nil))
			r.sweep(10 * time.Second)
			kept := r.collected() == 1
			if want := routerTraffic == config.RouterTrafficInclude; kept != want {
				t.Errorf("router_traffic=%s: the server answered the router's own connection and the address was kept=%v, want %v; the consumer routes the router's own traffic only when router_traffic includes it, otherwise that answer came over the uplink", routerTraffic, kept, want)
			}
		})
	}
}

func TestTakenFollowsThePortsAndFamiliesTheConsumersRulesMatch(t *testing.T) {
	const v4, v6 = "203.0.113.7", "2001:db8::7"
	shape := func(mode string) *config.SetConfig {
		set := readyConsumer(false)
		set.Id, set.Enabled = "consumer-"+mode, true
		switch mode {
		case "reuse":
			set.TCP.WSSizeReuse.Enabled = true
		case config.RoutingModeInterface:
			set.Routing.Enabled, set.Routing.Mode, set.Routing.EgressInterface = true, mode, "eth-collected"
		default:
			set.Routing.Enabled, set.Routing.Mode = true, mode
		}
		return set
	}
	for _, c := range []struct {
		name  string
		mode  string
		edit  func(*config.Config, *config.SetConfig)
		dst   string
		port  uint16
		taken bool
	}{
		{"WSSIZE reuse on 443", "reuse", nil, v4, 443, true},
		{"WSSIZE reuse on another port", "reuse", nil, v4, 8443, false},
		{"an interface consumer on another port", config.RoutingModeInterface, nil, v4, 8443, true},
		{"an interface consumer whose dport_filter leaves the port out", config.RoutingModeInterface, func(_ *config.Config, s *config.SetConfig) {
			s.TCP.DPortFilter = "443"
			s.TCPPortRanges = config.ParsePortRanges(s.TCP.DPortFilter)
		}, v4, 8443, true},
		{"a proxy consumer whose dport_filter leaves the port out", config.RoutingModeProxy, func(_ *config.Config, s *config.SetConfig) {
			s.TCP.DPortFilter = "443"
			s.TCPPortRanges = config.ParsePortRanges(s.TCP.DPortFilter)
		}, v4, 8443, true},
		{"a proxy consumer for IPv6 while the IPv6 queue is off", config.RoutingModeProxy, func(c *config.Config, s *config.SetConfig) {
			c.Queue.IPv6Enabled = false
			s.Targets.IpsToMatch = []string{v6 + "/128"}
		}, v6, 443, false},
		{"an IPv4-only proxy consumer listing an IPv6 address", config.RoutingModeProxy, func(_ *config.Config, s *config.SetConfig) {
			s.Targets.IPVersion = "4"
			s.Targets.IpsToMatch = []string{v6 + "/128"}
		}, v6, 443, true},
		{"an IPv4-only WSSIZE reuse consumer listing an IPv6 address", "reuse", func(_ *config.Config, s *config.SetConfig) {
			s.Targets.IPVersion = "4"
			s.Targets.IpsToMatch = []string{v6 + "/128"}
		}, v6, 443, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled = true, true
			set := shape(c.mode)
			if c.edit != nil {
				c.edit(&cfg, set)
			}
			cfg.Sets = []*config.SetConfig{set}
			view := cfg.WithCollectedIPs([]string{v4, v6})
			stubCollectedRoutes(t, func(id string) (tables.CollectedRoute, bool) {
				return installedCollectedRoute(view, view.GetSetById(id))
			})
			ver := uint8(IPv4)
			if net.ParseIP(c.dst).To4() == nil {
				ver = IPv6
			}
			h := helloWatch{dst: net.ParseIP(c.dst), port: c.port, ver: ver, client: net.IPv4(192, 168, 1, 50), scope: &ingressScope{resolved: true, name: "br0"}}
			if got := collectedTakenAway(view, buildMatcher(view), h); got != c.taken {
				t.Errorf("%s: taken=%v, want %v; the consumer's firewall rules match only their own families, and only WSSIZE reuse has a port term (443)", c.name, got, c.taken)
			}
		})
	}
}

func TestOnlyAnInstalledRouteTakesACollectedConnection(t *testing.T) {
	reuse := readyConsumer(false)
	reuse.Id = "reuse-consumer"
	reuse.TCP.WSSizeReuse.Enabled = true
	routed := interfaceConsumer()
	routed.Id = "routed-consumer"
	proxy := readyConsumer(false)
	proxy.Id = "proxy-consumer"
	proxy.Routing.Enabled, proxy.Routing.Mode = true, config.RoutingModeProxy
	device := helloWatch{dst: net.IPv4(203, 0, 113, 7), port: 443, ver: IPv4, client: net.IPv4(192, 168, 1, 50), scope: &ingressScope{resolved: true, name: "br0"}}
	for _, c := range []struct {
		name      string
		consumer  *config.SetConfig
		published bool
		want      bool
	}{
		{"a WSSIZE reuse consumer whose diversion is active", reuse, true, true},
		{"a WSSIZE reuse consumer whose diversion is not active", reuse, false, false},
		{"an interface consumer whose route is installed", routed, true, true},
		{"an interface consumer whose route is not installed", routed, false, false},
		{"a proxy consumer whose rules are installed", proxy, true, true},
		{"a proxy consumer whose rules failed to install", proxy, false, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			cfg := config.NewConfig()
			cfg.Queue.IPv4Enabled = true
			set := *c.consumer
			set.Enabled = true
			cfg.Sets = []*config.SetConfig{&set}
			view := cfg.WithCollectedIPs([]string{"203.0.113.7"})
			stubCollectedRoutes(t, func(id string) (tables.CollectedRoute, bool) {
				if !c.published {
					return tables.CollectedRoute{}, false
				}
				return installedCollectedRoute(view, view.GetSetById(id))
			})
			if got := collectedTakenAway(view, buildMatcher(view), device); got != c.want {
				t.Errorf("%s: taken=%v, want %v; an answer counts as the consumer's only while the firewall sends the connection to it", c.name, got, c.want)
			}
		})
	}
}
