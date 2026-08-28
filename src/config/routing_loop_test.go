package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/daniellavrushin/b4/netif"
)

func fakeIfaces(t *testing.T, kinds map[string]string) {
	t.Helper()
	root := t.TempDir() + string(os.PathSeparator)
	prev := netif.Root
	netif.Root = root
	netif.Forget()
	t.Cleanup(func() {
		netif.Root = prev
		netif.Forget()
	})

	for name, kind := range kinds {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		switch kind {
		case "tun":
			if err := os.WriteFile(filepath.Join(dir, "tun_flags"), []byte("0x1001\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		case "wireguard":
			if err := os.WriteFile(filepath.Join(dir, "uevent"), []byte("DEVTYPE=wireguard\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func routedSet(iface string) *SetConfig {
	set := NewSetConfig()
	set.Id = "11111111-2222-3333-4444-555555555555"
	set.Name = "routed"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = RoutingModeInterface
	set.Routing.EgressInterface = iface
	set.Routing.RouterTraffic = RouterTrafficAuto
	set.Targets.IpsToMatch = []string{"198.51.100.7"}
	return &set
}

func TestRoutingIncludesRouterTraffic(t *testing.T) {
	fakeIfaces(t, map[string]string{"xray0": "tun", "wg0": "wireguard", "eth1": "plain"})

	t.Run("auto keeps the router out of a userspace tunnel", func(t *testing.T) {
		if routedSet("xray0").RoutingIncludesRouterTraffic() {
			t.Error("a proxy behind a TUN re-dials the same destination, so marking the router's own traffic loops the box out of memory")
		}
	})

	t.Run("auto still routes the router through a plain interface", func(t *testing.T) {
		for _, iface := range []string{"eth1", "wg0"} {
			if !routedSet(iface).RoutingIncludesRouterTraffic() {
				t.Errorf("%s cannot re-dial a connection, so the router's own traffic keeps being routed", iface)
			}
		}
	})

	t.Run("include overrides the tunnel detection", func(t *testing.T) {
		set := routedSet("xray0")
		set.Routing.RouterTraffic = RouterTrafficInclude
		if !set.RoutingIncludesRouterTraffic() {
			t.Error("an explicit include must win over the automatic decision")
		}
	})

	t.Run("exclude wins on a plain interface", func(t *testing.T) {
		set := routedSet("eth1")
		set.Routing.RouterTraffic = RouterTrafficExclude
		if set.RoutingIncludesRouterTraffic() {
			t.Error("an explicit exclude must win over the automatic decision")
		}
	})

	t.Run("a source filter already excludes the router", func(t *testing.T) {
		byIface := routedSet("eth1")
		byIface.Routing.SourceInterfaces = []string{"br0"}
		byIface.Routing.RouterTraffic = RouterTrafficInclude
		if byIface.RoutingIncludesRouterTraffic() {
			t.Error("traffic the router originates arrives on no interface, so a source-interface filter can never match it")
		}

		byDevice := routedSet("eth1")
		byDevice.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
		if byDevice.RoutingIncludesRouterTraffic() {
			t.Error("traffic the router originates comes from no device, so a source-device filter can never match it")
		}

		excluded := routedSet("eth1")
		excluded.Targets.SourceDevices = []string{"aa:bb:cc:dd:ee:ff"}
		excluded.Targets.SourceDevicesExclude = true
		if !excluded.RoutingIncludesRouterTraffic() {
			t.Error("an exclude list names who is left out, so it does not scope the set to a source")
		}
	})
}

func TestRoutingHandsOffPackets(t *testing.T) {
	fakeIfaces(t, map[string]string{"xray0": "tun", "wg0": "wireguard", "eth1": "plain"})

	t.Run("a tunnel egress takes the packet as it stands", func(t *testing.T) {
		for _, iface := range []string{"xray0", "wg0"} {
			if !routedSet(iface).RoutingHandsOffPackets() {
				t.Errorf("%s swallows the packet, so the segment b4 would mangle is one no censor ever sees", iface)
			}
		}
	})

	t.Run("a plain egress still gets the bypass", func(t *testing.T) {
		if routedSet("eth1").RoutingHandsOffPackets() {
			t.Error("a second uplink puts the packet on the wire as it stands, so the DPI bypass still has work to do")
		}
	})

	t.Run("a proxy set hands off whatever the interface is", func(t *testing.T) {
		set := routedSet("eth1")
		set.Routing.Mode = RoutingModeProxy
		if !set.RoutingHandsOffPackets() {
			t.Error("b4's own listener terminates the connection, so nothing downstream of it should be desynced")
		}
	})

	t.Run("a set with no destination steers nothing", func(t *testing.T) {
		set := routedSet("xray0")
		set.Targets.IpsToMatch = nil
		if set.RoutingHandsOffPackets() {
			t.Error("with no destination b4 installs no rule, so the packet leaves by the normal route and still needs the bypass")
		}
	})
}

func TestValidateRouterTraffic(t *testing.T) {
	fakeIfaces(t, map[string]string{"xray0": "tun"})

	t.Run("empty normalises to auto", func(t *testing.T) {
		cfg := NewConfig()
		set := routedSet("xray0")
		set.Routing.RouterTraffic = ""
		cfg.Sets = []*SetConfig{set}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("validate: %v", err)
		}
		if got := cfg.Sets[0].Routing.RouterTraffic; got != RouterTrafficAuto {
			t.Errorf("router_traffic = %q, want %q", got, RouterTrafficAuto)
		}
	})

	t.Run("case and padding are accepted", func(t *testing.T) {
		cfg := NewConfig()
		set := routedSet("xray0")
		set.Routing.RouterTraffic = "  Include "
		cfg.Sets = []*SetConfig{set}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("validate: %v", err)
		}
		if got := cfg.Sets[0].Routing.RouterTraffic; got != RouterTrafficInclude {
			t.Errorf("router_traffic = %q, want %q", got, RouterTrafficInclude)
		}
	})

	t.Run("an unknown value is rejected", func(t *testing.T) {
		cfg := NewConfig()
		set := routedSet("xray0")
		set.Routing.RouterTraffic = "sometimes"
		cfg.Sets = []*SetConfig{set}
		if err := cfg.Validate(); err == nil {
			t.Error("an unreadable router_traffic must fail validation instead of silently picking a side")
		}
	})

	t.Run("kill switch is dropped where there is no interface to lose", func(t *testing.T) {
		cfg := NewConfig()
		set := routedSet("xray0")
		set.Routing.Mode = RoutingModeProxy
		set.Routing.Upstream.Host = "127.0.0.1"
		set.Routing.Upstream.Port = 1080
		set.Routing.KillSwitch = true
		cfg.Sets = []*SetConfig{set}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("validate: %v", err)
		}
		if cfg.Sets[0].Routing.KillSwitch {
			t.Error("proxy mode terminates the connection, so there is no routing table to hold shut")
		}
	})
}

func TestRoutingHandsOffPacketsOnlyWhenTheTunnelActuallyCarriesThem(t *testing.T) {
	fakeIfaces(t, map[string]string{"xray0": "tun", "wg0": "wireguard", "eth1": "plain"})

	t.Run("a live tunnel takes the packets", func(t *testing.T) {
		for _, iface := range []string{"xray0", "wg0"} {
			if !routedSet(iface).RoutingHandsOffPackets() {
				t.Errorf("%s wraps the packet before the network sees it, so mangling the inner one buys nothing", iface)
			}
		}
	})

	t.Run("a plain uplink keeps its bypass", func(t *testing.T) {
		if routedSet("eth1").RoutingHandsOffPackets() {
			t.Error("a second uplink puts the packet on the wire as it stands, so the strategy still has to run")
		}
	})

	t.Run("a source-filtered set still hands off", func(t *testing.T) {
		scoped := routedSet("xray0")
		scoped.Routing.SourceInterfaces = []string{"br1"}
		if !scoped.RoutingHandsOffPackets() {
			t.Error("the engine cannot tell an in-scope client from an out-of-scope one, and mangling the in-scope traffic the set does route is the worse of the two mistakes")
		}
	})

	t.Run("a domain-only set with no IP targets keeps its bypass", func(t *testing.T) {
		domainOnly := routedSet("xray0")
		domainOnly.Targets.DomainOnly = true
		domainOnly.Targets.IpsToMatch = nil
		domainOnly.Targets.DomainsToMatch = []string{"example.com"}
		if domainOnly.RoutingHandsOffPackets() {
			t.Error("every routing writer bails on domain-only, so nothing is ever routed and the strategy is all the set has")
		}
	})

	t.Run("an interface that is down carries nothing", func(t *testing.T) {
		root := netif.Root
		if err := os.WriteFile(filepath.Join(root, "xray0", "flags"), []byte("0x1002\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		netif.Forget()
		if routedSet("xray0").RoutingHandsOffPackets() {
			t.Error("the kernel refuses a default route through a device that is not up, so the set leaks out the WAN and still needs its bypass")
		}
	})
}

func TestAnAbsentEgressInterfaceNeverClaimsTheRouterOwnTraffic(t *testing.T) {
	fakeIfaces(t, map[string]string{"eth1": "plain"})

	if routedSet("xray0").RoutingIncludesRouterTraffic() {
		t.Fatal("an egress interface that does not exist cannot be classified, so the router's own traffic must " +
			"stay on the normal route. Claiming it sends every connection b4 opens into a table that holds nothing " +
			"but the kill-switch blackhole, so b4's own dials fail instantly and retry in a hot loop that takes the box down")
	}

	if !routedSet("eth1").RoutingIncludesRouterTraffic() {
		t.Fatal("a plain interface that exists still carries the router's own traffic")
	}
}

func TestAnAbsentInterfaceIsStillOverridableByHand(t *testing.T) {
	fakeIfaces(t, map[string]string{"eth1": "plain"})

	set := routedSet("xray0")
	set.Routing.RouterTraffic = RouterTrafficInclude
	if !set.RoutingIncludesRouterTraffic() {
		t.Fatal("an explicit include must still win; the missing-interface rule only changes the automatic choice")
	}
}
