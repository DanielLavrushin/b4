package tables

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type bridgeNetfilterFixture struct {
	root string
	proc string
	net  string
}

func newBridgeNetfilterFixture(t *testing.T, loaded bool) bridgeNetfilterFixture {
	t.Helper()
	root := t.TempDir()
	f := bridgeNetfilterFixture{root: root, proc: filepath.Join(root, "proc"), net: filepath.Join(root, "net")}
	bnfMkdir(t, f.net)
	if loaded {
		bnfMkdir(t, f.proc)
	}
	oldProc, oldNet, oldConf := bridgeNetfilterProcDir, bridgeNetfilterNetDir, bridgeNetfilterDockerdConf
	bridgeNetfilterProcDir = f.proc
	bridgeNetfilterNetDir = f.net
	bridgeNetfilterDockerdConf = filepath.Join(root, "12-br-netfilter-ip.conf")
	t.Cleanup(func() {
		bridgeNetfilterProcDir, bridgeNetfilterNetDir, bridgeNetfilterDockerdConf = oldProc, oldNet, oldConf
	})
	return f
}

func (f bridgeNetfilterFixture) global(t *testing.T, v4, v6 string) {
	t.Helper()
	bnfWrite(t, filepath.Join(f.proc, "bridge-nf-call-iptables"), v4)
	bnfWrite(t, filepath.Join(f.proc, "bridge-nf-call-ip6tables"), v6)
}

func (f bridgeNetfilterFixture) bridge(t *testing.T, name, v4, v6 string) {
	t.Helper()
	dir := filepath.Join(f.net, name, "bridge")
	bnfMkdir(t, dir)
	bnfWrite(t, filepath.Join(dir, "nf_call_iptables"), v4)
	bnfWrite(t, filepath.Join(dir, "nf_call_ip6tables"), v6)
}

func (f bridgeNetfilterFixture) ports(t *testing.T, bridge string, ports ...string) {
	t.Helper()
	dir := filepath.Join(f.net, bridge, "brif")
	bnfMkdir(t, dir)
	for _, p := range ports {
		bnfMkdir(t, filepath.Join(dir, p))
	}
}

func (f bridgeNetfilterFixture) iface(t *testing.T, name string) {
	t.Helper()
	bnfMkdir(t, filepath.Join(f.net, name))
}

func bnfMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func bnfWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestReadBridgeNetfilterModuleNotLoaded(t *testing.T) {
	f := newBridgeNetfilterFixture(t, false)
	f.bridge(t, "br-lan", "1", "1")

	bn := ReadBridgeNetfilter()
	if bn.GlobalV4 || bn.GlobalV6 || len(bn.BridgesV4) > 0 || len(bn.BridgesV6) > 0 {
		t.Fatalf("without br_netfilter no bridge runs the IP hooks, got %+v", bn)
	}
}

func TestReadBridgeNetfilterGlobalCoversEveryBridge(t *testing.T) {
	f := newBridgeNetfilterFixture(t, true)
	f.global(t, "1", "0")
	f.bridge(t, "docker0", "0", "0")
	f.bridge(t, "br-lan", "0", "0")
	f.iface(t, "eth0")

	bn := ReadBridgeNetfilter()
	if !bn.GlobalV4 || bn.GlobalV6 {
		t.Fatalf("global flags = v4 %v v6 %v, want true false", bn.GlobalV4, bn.GlobalV6)
	}
	if want := []string{"br-lan", "docker0"}; !slices.Equal(bn.BridgesV4, want) {
		t.Fatalf("BridgesV4 = %v, want %v", bn.BridgesV4, want)
	}
	if len(bn.BridgesV6) != 0 {
		t.Fatalf("BridgesV6 = %v, want none", bn.BridgesV6)
	}
}

func TestReadBridgeNetfilterPerBridgeFlag(t *testing.T) {
	f := newBridgeNetfilterFixture(t, true)
	f.global(t, "0", "0")
	f.bridge(t, "br-lan", "1", "0")
	f.bridge(t, "docker0", "0", "1")

	bn := ReadBridgeNetfilter()
	if want := []string{"br-lan"}; !slices.Equal(bn.BridgesV4, want) {
		t.Fatalf("BridgesV4 = %v, want %v", bn.BridgesV4, want)
	}
	if want := []string{"docker0"}; !slices.Equal(bn.BridgesV6, want) {
		t.Fatalf("BridgesV6 = %v, want %v", bn.BridgesV6, want)
	}
}

func TestReadBridgeNetfilterSkipsBridgesWithoutPorts(t *testing.T) {
	f := newBridgeNetfilterFixture(t, true)
	f.global(t, "1", "1")
	f.bridge(t, "br-lan", "0", "0")
	f.ports(t, "br-lan", "eth0", "eth2")
	f.bridge(t, "docker0", "0", "0")
	f.ports(t, "docker0")
	f.bridge(t, "br-guest", "0", "0")

	bn := ReadBridgeNetfilter()
	want := []string{"br-guest", "br-lan"}
	if !slices.Equal(bn.BridgesV4, want) || !slices.Equal(bn.BridgesV6, want) {
		t.Fatalf("got v4 %v v6 %v, want %v for both: a bridge with an empty brif carries no traffic, an unreadable brif is kept", bn.BridgesV4, bn.BridgesV6, want)
	}
}

func TestBridgeNetfilterSwitches(t *testing.T) {
	cases := []struct {
		name          string
		bn            BridgeNetfilter
		ipv4, ipv6    bool
		sysctls, attr []string
	}{
		{
			name:    "global IPv4",
			bn:      BridgeNetfilter{GlobalV4: true, GlobalV6: true, BridgesV4: []string{"br-lan"}, BridgesV6: []string{"br-lan"}},
			ipv4:    true,
			sysctls: []string{"net.bridge.bridge-nf-call-iptables"},
		},
		{
			name:    "global both families",
			bn:      BridgeNetfilter{GlobalV4: true, GlobalV6: true, BridgesV4: []string{"br-lan"}, BridgesV6: []string{"br-lan"}},
			ipv4:    true,
			ipv6:    true,
			sysctls: []string{"net.bridge.bridge-nf-call-iptables", "net.bridge.bridge-nf-call-ip6tables"},
		},
		{
			name: "per-bridge IPv4 only",
			bn:   BridgeNetfilter{BridgesV4: []string{"br-lan"}},
			ipv4: true,
			ipv6: true,
			attr: []string{"nf_call_iptables"},
		},
		{
			name:    "global IPv4 and per-bridge IPv6",
			bn:      BridgeNetfilter{GlobalV4: true, BridgesV4: []string{"br-lan"}, BridgesV6: []string{"docker0"}},
			ipv4:    true,
			ipv6:    true,
			sysctls: []string{"net.bridge.bridge-nf-call-iptables"},
			attr:    []string{"nf_call_ip6tables"},
		},
		{
			name: "global flag without a bridge that has ports",
			bn:   BridgeNetfilter{GlobalV4: true},
			ipv4: true,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sysctls, attrs := tc.bn.Switches(tc.ipv4, tc.ipv6)
			if !slices.Equal(sysctls, tc.sysctls) || !slices.Equal(attrs, tc.attr) {
				t.Fatalf("got sysctls %v attrs %v, want %v and %v", sysctls, attrs, tc.sysctls, tc.attr)
			}
		})
	}
}

func TestBridgeNetfilterBridgesByFamily(t *testing.T) {
	bn := BridgeNetfilter{BridgesV4: []string{"br-lan", "docker0"}, BridgesV6: []string{"br-guest", "br-lan"}}

	if got, want := bn.Bridges(true, true), []string{"br-guest", "br-lan", "docker0"}; !slices.Equal(got, want) {
		t.Fatalf("Bridges(v4, v6) = %v, want %v", got, want)
	}
	if got, want := bn.Bridges(false, true), []string{"br-guest", "br-lan"}; !slices.Equal(got, want) {
		t.Fatalf("Bridges(v6) = %v, want %v", got, want)
	}
	if got := bn.Bridges(false, false); len(got) != 0 {
		t.Fatalf("Bridges() = %v, want none", got)
	}
	if want := []string{"br-lan", "docker0"}; !slices.Equal(bn.BridgesV4, want) {
		t.Fatalf("Bridges changed BridgesV4 to %v", bn.BridgesV4)
	}
}

func TestBridgeNetfilterWarning(t *testing.T) {
	newBridgeNetfilterFixture(t, true)

	t.Run("no bridge runs the hooks", func(t *testing.T) {
		if msg := bridgeNetfilterWarning(BridgeNetfilter{}, true, true); msg != "" {
			t.Fatalf("got %q, want no warning", msg)
		}
	})

	t.Run("only the family that is not in use", func(t *testing.T) {
		bn := BridgeNetfilter{GlobalV6: true, BridgesV6: []string{"br-lan"}}
		if msg := bridgeNetfilterWarning(bn, true, false); msg != "" {
			t.Fatalf("got %q, want no warning for IPv6 bridges with IPv6 off", msg)
		}
	})

	t.Run("global IPv4", func(t *testing.T) {
		bn := BridgeNetfilter{GlobalV4: true, BridgesV4: []string{"br-lan", "docker0"}}
		msg := bridgeNetfilterWarning(bn, true, false)
		for _, want := range []string{"br-lan, docker0", "sysctl -w net.bridge.bridge-nf-call-iptables=0", "/etc/sysctl.conf"} {
			if !strings.Contains(msg, want) {
				t.Errorf("warning %q does not contain %q", msg, want)
			}
		}
		for _, unwanted := range []string{"ip6tables", "12-br-netfilter-ip.conf", "/sys/class/net"} {
			if strings.Contains(msg, unwanted) {
				t.Errorf("warning %q should not contain %q", msg, unwanted)
			}
		}
	})

	t.Run("global both families", func(t *testing.T) {
		bn := BridgeNetfilter{GlobalV4: true, GlobalV6: true, BridgesV4: []string{"br-lan"}, BridgesV6: []string{"br-lan"}}
		msg := bridgeNetfilterWarning(bn, true, true)
		for _, want := range []string{"sysctl -w net.bridge.bridge-nf-call-iptables=0", "sysctl -w net.bridge.bridge-nf-call-ip6tables=0"} {
			if !strings.Contains(msg, want) {
				t.Errorf("warning %q does not contain %q", msg, want)
			}
		}
	})

	t.Run("per-bridge flag only", func(t *testing.T) {
		bn := BridgeNetfilter{BridgesV4: []string{"br-lan"}}
		msg := bridgeNetfilterWarning(bn, true, false)
		if !strings.Contains(msg, "nf_call_iptables under /sys/class/net/<bridge>/bridge/") {
			t.Errorf("warning %q does not point at the per-bridge flag", msg)
		}
		if strings.Contains(msg, "sysctl -w") {
			t.Errorf("warning %q suggests the global sysctl, which is already off", msg)
		}
	})

	t.Run("dockerd sysctl file named when present", func(t *testing.T) {
		bnfWrite(t, bridgeNetfilterDockerdConf, "net.bridge.bridge-nf-call-iptables=1")
		bn := BridgeNetfilter{GlobalV4: true, BridgesV4: []string{"br-lan"}}
		if msg := bridgeNetfilterWarning(bn, true, false); !strings.Contains(msg, bridgeNetfilterDockerdConf) {
			t.Errorf("warning %q does not name %s", msg, bridgeNetfilterDockerdConf)
		}
	})
}

func TestRouteNoteBridgeNetfilterWarnsOnChange(t *testing.T) {
	f := newBridgeNetfilterFixture(t, true)
	f.bridge(t, "br-lan", "0", "0")
	bridgeNetfilterMu.Lock()
	saved := bridgeNetfilterLogged
	bridgeNetfilterLogged = ""
	bridgeNetfilterMu.Unlock()
	t.Cleanup(func() {
		bridgeNetfilterMu.Lock()
		bridgeNetfilterLogged = saved
		bridgeNetfilterMu.Unlock()
	})
	logged := func() string {
		bridgeNetfilterMu.Lock()
		defer bridgeNetfilterMu.Unlock()
		return bridgeNetfilterLogged
	}

	f.global(t, "1", "0")
	routeNoteBridgeNetfilter(true, false)
	first := logged()
	if !strings.Contains(first, "br-lan") {
		t.Fatalf("expected a warning naming br-lan, got %q", first)
	}
	routeNoteBridgeNetfilter(true, false)
	if logged() != first {
		t.Fatalf("an unchanged state must not produce a new warning")
	}

	f.global(t, "0", "0")
	routeNoteBridgeNetfilter(true, false)
	if got := logged(); got != "" {
		t.Fatalf("with bridge netfilter off the warning must clear, got %q", got)
	}

	f.global(t, "1", "0")
	routeNoteBridgeNetfilter(false, false)
	if got := logged(); got != "" {
		t.Fatalf("without transparent-proxy sets there is nothing to warn about, got %q", got)
	}
}

func TestRoutingInstalledFamilies(t *testing.T) {
	routeMu.Lock()
	saved := routeRuleCache
	routeRuleCache = map[string]routeState{
		config.TelegramBridgeSetID: {mode: config.RoutingModeMTProtoWS, ipv4: true},
		"iface":                    {mode: config.RoutingModeInterface, ipv4: true, ipv6: true},
	}
	routeMu.Unlock()
	t.Cleanup(func() {
		routeMu.Lock()
		routeRuleCache = saved
		routeMu.Unlock()
	})

	if ipv4, ipv6, ok := RoutingSetFamilies(config.TelegramBridgeSetID); !ok || !ipv4 || ipv6 {
		t.Errorf("bridge set: got v4 %v v6 %v installed %v, want true false true", ipv4, ipv6, ok)
	}
	if _, _, ok := RoutingSetFamilies("missing"); ok {
		t.Error("a set that is not installed reported as installed")
	}
	if ipv4, ipv6 := RoutingTProxyFamilies(); !ipv4 || ipv6 {
		t.Errorf("only the IPv4 mtproto-ws rule uses TPROXY: got v4 %v v6 %v", ipv4, ipv6)
	}
}

func TestRouteTProxyFamiliesLocked(t *testing.T) {
	routeMu.Lock()
	saved := routeRuleCache
	routeRuleCache = map[string]routeState{
		"iface": {mode: config.RoutingModeInterface, ipv4: true, ipv6: true},
		"proxy": {mode: config.RoutingModeProxy, ipv4: true},
	}
	v4, v6 := routeTProxyFamiliesLocked()
	routeRuleCache["tg"] = routeState{mode: config.RoutingModeMTProtoWS, ipv6: true}
	bothV4, bothV6 := routeTProxyFamiliesLocked()
	routeRuleCache = map[string]routeState{"iface": {mode: config.RoutingModeInterface, ipv4: true, ipv6: true}}
	noneV4, noneV6 := routeTProxyFamiliesLocked()
	routeRuleCache = saved
	routeMu.Unlock()

	if !v4 || v6 {
		t.Errorf("proxy set on IPv4 only: got v4 %v v6 %v", v4, v6)
	}
	if !bothV4 || !bothV6 {
		t.Errorf("proxy on IPv4 and mtproto-ws on IPv6: got v4 %v v6 %v", bothV4, bothV6)
	}
	if noneV4 || noneV6 {
		t.Errorf("interface sets do not use TPROXY: got v4 %v v6 %v", noneV4, noneV6)
	}
}
