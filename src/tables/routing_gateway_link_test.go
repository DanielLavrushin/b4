package tables

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/netif"
)

const (
	gwLinkTun  = 65534
	gwLinkPPP  = 512
	gwLinkGRE  = 778
	gwLinkLoop = 772
)

func TestOnlyAnEthernetEgressNarrowsTheIPv4Guard(t *testing.T) {
	for _, c := range []struct {
		name   string
		typ    int
		known  bool
		narrow bool
	}{
		{"ethernet", routeArphrdEther, true, true},
		{"tun or wireguard", gwLinkTun, true, false},
		{"ppp", gwLinkPPP, true, false},
		{"gre", gwLinkGRE, true, false},
		{"loopback", gwLinkLoop, true, false},
		{"missing", 0, false, false},
	} {
		for _, engine := range []string{"mock", backendNFTables, backendIPTables} {
			t.Run(c.name+"/"+engine, func(t *testing.T) {
				gwIsolate(t)
				cmds := gwStubCommands(t)
				stubBinaries(t, "ip", backendIPTables, backendIP6Tables, "ipset")
				gwStubHost(t, map[string][]string{"lo": {"192.0.2.10/24"}})
				types := map[string]int{}
				if c.known {
					types["lo"] = c.typ
				}
				gwStubLinks(t, types)
				cfg := familyTestConfig(true, true)
				set := gwTestSet("linkset", "lo", "192.0.2.1")
				cfg.Sets = []*config.SetConfig{set}
				st := gwResolvedState(cfg, set)
				if st.gwV4 != "192.0.2.1" {
					t.Fatalf("the gateway is usable on any link type, got %q", st.gwV4)
				}
				if routeGuardNarrowed(st) != c.narrow {
					t.Fatalf("narrowed guard = %v (peers %v), want %v", routeGuardNarrowed(st), st.gwPeersV4, c.narrow)
				}

				mock := &mockRouteBackend{}
				var be routeBackend = mock
				switch engine {
				case backendNFTables:
					be = &routeNftBackend{}
				case backendIPTables:
					be = &routeIptBackend{}
				}
				if err := routeEnsureRule(be, cfg, set, st, nil); err != nil {
					t.Fatalf("routeEnsureRule: %v", err)
				}

				if got := cmds.with("route replace default via 192.0.2.1 dev lo"); len(got) != 1 {
					t.Errorf("the usable gateway must be the next hop on every link type, routes %v", cmds.with("route replace default"))
				}
				pre := chainRuleLines(cmds.lines, st.chainPre)
				out := chainRuleLines(cmds.lines, st.chainOut)
				switch engine {
				case "mock":
					narrowed := indexOfPrefix(mock.chainOps[st.chainPre], "gateway-guard") >= 0
					full := indexOfOp(mock.chainOps[st.chainPre], "loop-guard lo") >= 0
					dropped := indexOfPrefix(mock.chainOps[st.chainOut], "redirect-drop") >= 0
					if narrowed != c.narrow || full == c.narrow || dropped != c.narrow {
						t.Errorf("narrowed=%v full=%v redirect-drop=%v, want the narrowed guard and the drop only on Ethernet: %v %v", narrowed, full, dropped, mock.chainOps[st.chainPre], mock.chainOps[st.chainOut])
					}
					return
				case backendNFTables:
					if c.narrow {
						return
					}
					if got := linesWith(pre, `iifname "lo"`); !slices.Equal(got, []string{`nft add rule inet b4_route ` + st.chainPre + ` iifname "lo" return`}) {
						t.Errorf("a gateway on a non-Ethernet link keeps the bare guard of a set without one, got %v", got)
					}
				case backendIPTables:
					if c.narrow {
						return
					}
					want := []string{
						"iptables -w -t mangle -A " + st.chainPre + " -i lo -j RETURN",
						"ip6tables -w -t mangle -A " + st.chainPre + " -i lo -j RETURN",
					}
					if got := linesWith(pre, "-i lo"); !slices.Equal(got, want) {
						t.Errorf("a gateway on a non-Ethernet link keeps the bare per-family guard, got %v", got)
					}
				}
				for _, needle := range []string{"saddr", " -s ", "fib daddr", "addrtype"} {
					if got := linesWith(pre, needle); len(got) != 0 {
						t.Errorf("a gateway on a non-Ethernet link takes no gateway or local-destination rule, got %v", got)
					}
				}
				if got := linesWith(out, "redirect"); len(got) != 0 {
					t.Errorf("a gateway on a non-Ethernet link drops no redirects, got %v", got)
				}
			})
		}
	}
}

func TestTheLinkTypeIsReadOnlyForAUsableIPv4GatewayAndOncePerSync(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	gwIsolate(t)
	gwStubCommands(t)
	stubBinaries(t, "ip")
	gwStubHost(t, map[string][]string{"eth1": {"192.0.2.10/24", "2001:db8:1::10/64"}})
	links := gwStubLinks(t, map[string]int{"eth1": routeArphrdEther})
	routeEngine = &mockRouteBackend{}
	plain := gwTestSet("readplain", "eth1", "")
	v6 := gwTestSet("readv6", "eth1", "2001:db8:1::1")
	far := gwTestSet("readfar", "eth1", "198.51.100.1")
	v6.Routing.FWMark, v6.Routing.Table = 0x7e20, 233
	far.Routing.FWMark, far.Routing.Table = 0x7e30, 234
	cfg := familyTestConfig(true, true)
	cfg.Sets = []*config.SetConfig{plain, v6, far}
	RoutingSyncConfig(cfg)
	if n := links.reads.Load(); n != 0 {
		t.Fatalf("no set has a usable IPv4 gateway, yet the link type was read %d times", n)
	}

	a := gwTestSet("reada", "eth1", "192.0.2.1")
	b := gwTestSet("readb", "eth1", "192.0.2.2")
	b.Routing.FWMark, b.Routing.Table = 0x7e40, 235
	next := familyTestConfig(true, true)
	next.Sets = []*config.SetConfig{plain, v6, far, a, b}
	RoutingSyncConfig(next)
	if n := links.reads.Load(); n != 1 {
		t.Errorf("a sync of two IPv4 gateway sets on one egress read its link type %d times, want once", n)
	}
}

func TestALinkTypeChangeRebuildsTheGatewaySet(t *testing.T) {
	familyResetGlobals(t)
	stubRetryState(t)
	gwIsolate(t)
	cmds := gwStubCommands(t)
	stubBinaries(t, "ip")
	gwStubHost(t, map[string][]string{"lo": {"192.0.2.10/24"}})
	links := gwStubLinks(t, map[string]int{"lo": gwLinkTun})
	be := &mockRouteBackend{}
	routeEngine = be
	cfg := familyTestConfig(true, false)
	cfg.Sets = []*config.SetConfig{gwTestSet("relink", "lo", "192.0.2.1")}
	RoutingSyncConfig(cfg)
	tun := gwCachedState(t, "relink")
	if routeGuardNarrowed(tun) || gwLastOp(be.chainOps[tun.chainPre], "loop-guard") != "loop-guard lo" {
		t.Fatalf("a tun egress must start on the full guard, got peers %v ops %v", tun.gwPeersV4, be.chainOps[tun.chainPre])
	}
	if got := cmds.with("route replace default via 192.0.2.1 dev lo"); len(got) != 1 {
		t.Fatalf("a tun egress still routes through the usable gateway, routes %v", cmds.with("route replace default"))
	}

	links.set("lo", routeArphrdEther)
	be.chainOps = nil
	RoutingReinstallForInterface(cfg, "lo")
	ether := gwCachedState(t, "relink")
	if routeStateEqual(tun, ether) || !routeGuardNarrowed(ether) {
		t.Fatalf("the egress came back as an Ethernet link, so the set must be rebuilt with the narrowed guard, peers %v", ether.gwPeersV4)
	}
	if got := gwLastOp(be.chainOps[ether.chainPre], "gateway-guard"); got != "gateway-guard lo gw4=192.0.2.1 v6=false" {
		t.Errorf("the rebuilt chain must narrow the guard to the gateway, got %v", be.chainOps[ether.chainPre])
	}

	links.set("lo", gwLinkPPP)
	be.chainOps = nil
	RoutingReinstallForInterface(cfg, "lo")
	ppp := gwCachedState(t, "relink")
	if routeGuardNarrowed(ppp) || gwLastOp(be.chainOps[ppp.chainPre], "loop-guard") != "loop-guard lo" {
		t.Errorf("the egress came back as a PPP link, so the set must return to the full guard, peers %v ops %v", ppp.gwPeersV4, be.chainOps[ppp.chainPre])
	}
}

func TestRouteLinkTypeReadsTheInterfaceTypeFromSysfs(t *testing.T) {
	root := t.TempDir() + string(os.PathSeparator)
	prev := netif.Root
	netif.Root = root
	t.Cleanup(func() { netif.Root = prev })
	for name, body := range map[string]string{"eth9": "1\n", "tun9": "65534\n", "ppp9": "512\n", "bad9": "ether\n"} {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "type"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, c := range []struct {
		iface string
		typ   int
		ok    bool
	}{
		{"eth9", routeArphrdEther, true},
		{"tun9", gwLinkTun, true},
		{"ppp9", gwLinkPPP, true},
		{"bad9", 0, false},
		{"gone9", 0, false},
	} {
		typ, ok := routeLinkTypeSysfs(c.iface)
		if typ != c.typ || ok != c.ok {
			t.Errorf("routeLinkTypeSysfs(%q) = %d %v, want %d %v", c.iface, typ, ok, c.typ, c.ok)
		}
	}
	probe := newRouteGatewayProbe()
	prevType := routeLinkType
	t.Cleanup(func() { routeLinkType = prevType })
	routeLinkType = routeLinkTypeSysfs
	for iface, want := range map[string]bool{"eth9": true, "tun9": false, "ppp9": false, "bad9": false, "gone9": false} {
		if got := probe.ethernet(iface); got != want {
			t.Errorf("probe.ethernet(%q) = %v, want %v", iface, got, want)
		}
	}
}
