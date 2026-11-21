package nfq

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tables"
	"github.com/daniellavrushin/b4/tproxy"
	"golang.org/x/sys/unix"
)

const (
	takesUplink     = "b4k0"
	takesUplinkIP   = "10.211.0.1"
	takesUplinkGW   = "10.211.0.2"
	takesUplinkIP6  = "fd00:211::1"
	takesUplinkGW6  = "fd00:211::2"
	takesEgress     = "b4k1"
	takesEgressIP   = "10.211.1.1"
	takesEgressIP6  = "fd00:211:1::1"
	takesLAN        = "b4k2"
	takesLANPeer    = "b4k2p"
	takesRouterLAN  = "10.211.2.1"
	takesRouterLAN6 = "fd00:211:2::1"
	takesDevice     = "10.211.2.100"
	takesDevice6    = "fd00:211:2::100"
	takesTarget     = "198.51.100.77"
	takesTarget6    = "2001:db8:77::7"
	takesMissing    = "b4knone"
	takesCountTable = "b4ktakes"
	takesDialEnv    = "B4_NETNS_TAKES_DIAL"
)

type takesKind string

const (
	takesInterface takesKind = "interface"
	takesProxy     takesKind = "proxy"
	takesMTProtoWS takesKind = "mtproto-ws"
	takesReuse     takesKind = "wssize-reuse"
)

type takesCell struct {
	name       string
	kind       takesKind
	shape      func(cfg *config.Config, set *config.SetConfig, mac string)
	router     bool
	v6         bool
	port       int
	noListener bool
}

type takesRig struct {
	devicePID int
	mac       string
}

func takesRequire(t *testing.T, engine string) {
	t.Helper()
	netnsHoldRequire(t)
	for _, bin := range []string{"nft", "ipset"} {
		if _, err := exec.LookPath(bin); err != nil {
			t.Skipf("%s is not installed", bin)
		}
	}
	if engine == "iptables" {
		if out, err := exec.Command("iptables", "-w", "-t", "mangle", "-L", "PREROUTING", "-n").CombinedOutput(); err != nil {
			t.Skipf("iptables cannot read mangle here: %s", strings.TrimSpace(string(out)))
		}
	}
}

func takesRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func takesSysctl(t *testing.T, key, value string) {
	t.Helper()
	path := "/proc/sys/" + strings.ReplaceAll(key, ".", "/")
	prev, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("%s is not readable here: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Skipf("cannot write %s here: %v", path, err)
	}
	t.Cleanup(func() { _ = os.WriteFile(path, prev, 0o644) })
}

func takesLinkMAC(t *testing.T, args ...string) string {
	t.Helper()
	out := takesRun(t, args...)
	m := regexp.MustCompile(`link/ether ([0-9a-f:]{17})`).FindStringSubmatch(out)
	if m == nil {
		t.Fatalf("no MAC in %q", out)
	}
	return m[1]
}

func takesSetUp(t *testing.T) *takesRig {
	t.Helper()
	takesSysctl(t, "net.ipv4.ip_forward", "1")
	takesSysctl(t, "net.ipv6.conf.all.disable_ipv6", "0")
	takesSysctl(t, "net.ipv6.conf.all.forwarding", "1")
	takesRun(t, "ip", "link", "set", "lo", "up")
	for _, l := range []struct{ dev, v4, v6 string }{
		{takesUplink, takesUplinkIP, takesUplinkIP6},
		{takesEgress, takesEgressIP, takesEgressIP6},
	} {
		_ = exec.Command("ip", "link", "del", l.dev).Run()
		takesRun(t, "ip", "link", "add", l.dev, "type", "veth", "peer", "name", l.dev+"p")
		dev := l.dev
		t.Cleanup(func() { _ = exec.Command("ip", "link", "del", dev).Run() })
		takesRun(t, "ip", "link", "set", l.dev, "up")
		takesRun(t, "ip", "link", "set", l.dev+"p", "up")
		takesRun(t, "ip", "addr", "add", l.v4+"/24", "dev", l.dev)
		takesRun(t, "ip", "-6", "addr", "add", l.v6+"/64", "dev", l.dev, "nodad")
	}
	peer := takesLinkMAC(t, "ip", "link", "show", takesUplink+"p")
	takesRun(t, "ip", "neigh", "replace", takesUplinkGW, "lladdr", peer, "dev", takesUplink, "nud", "permanent")
	takesRun(t, "ip", "-6", "neigh", "replace", takesUplinkGW6, "lladdr", peer, "dev", takesUplink, "nud", "permanent")
	takesRun(t, "ip", "route", "replace", "default", "via", takesUplinkGW, "dev", takesUplink)
	takesRun(t, "ip", "-6", "route", "replace", "default", "via", takesUplinkGW6, "dev", takesUplink)

	_ = exec.Command("ip", "link", "del", takesLAN).Run()
	takesRun(t, "ip", "link", "add", takesLAN, "type", "veth", "peer", "name", takesLANPeer)
	t.Cleanup(func() { _ = exec.Command("ip", "link", "del", takesLAN).Run() })
	takesRun(t, "ip", "link", "set", takesLAN, "up")
	takesRun(t, "ip", "addr", "add", takesRouterLAN+"/24", "dev", takesLAN)
	takesRun(t, "ip", "-6", "addr", "add", takesRouterLAN6+"/64", "dev", takesLAN, "nodad")
	rig := &takesRig{mac: takesLinkMAC(t, "ip", "link", "show", takesLANPeer)}
	cmd := exec.Command("unshare", "-n", "sleep", "900")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot open a device network namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	rig.devicePID = cmd.Process.Pid
	self, _ := os.Readlink("/proc/self/ns/net")
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		if other, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/net", rig.devicePID)); err == nil && other != self {
			break
		}
		if time.Now().After(deadline) {
			t.Skip("the device never entered its own network namespace")
		}
	}
	takesRun(t, "ip", "link", "set", takesLANPeer, "netns", strconv.Itoa(rig.devicePID))
	in := func(args ...string) {
		takesRun(t, append([]string{"nsenter", "-t", strconv.Itoa(rig.devicePID), "-n"}, args...)...)
	}
	in("ip", "link", "set", "lo", "up")
	in("ip", "link", "set", takesLANPeer, "up")
	in("ip", "addr", "add", takesDevice+"/24", "dev", takesLANPeer)
	in("ip", "-6", "addr", "add", takesDevice6+"/64", "dev", takesLANPeer, "nodad")
	in("ip", "route", "add", "default", "via", takesRouterLAN)
	in("ip", "-6", "route", "add", "default", "via", takesRouterLAN6)

	_ = exec.Command("nft", "delete", "table", "inet", takesCountTable).Run()
	takesRun(t, "nft", "add", "table", "inet", takesCountTable)
	t.Cleanup(func() { _ = exec.Command("nft", "delete", "table", "inet", takesCountTable).Run() })
	takesRun(t, "nft", "add", "chain", "inet", takesCountTable, "post", "{ type filter hook postrouting priority 300 ; policy accept ; }")
	takesRun(t, "nft", "add", "rule", "inet", takesCountTable, "post", "oifname", takesUplink, "ip", "daddr", takesTarget, "counter")
	takesRun(t, "nft", "add", "rule", "inet", takesCountTable, "post", "oifname", takesUplink, "ip6", "daddr", takesTarget6, "counter")
	return rig
}

var takesCounter = regexp.MustCompile(`counter packets (\d+)`)

func takesUplinkPackets(t *testing.T) int {
	t.Helper()
	total := 0
	for _, m := range takesCounter.FindAllStringSubmatch(takesRun(t, "nft", "list", "chain", "inet", takesCountTable, "post"), -1) {
		n, _ := strconv.Atoi(m[1])
		total += n
	}
	return total
}

func TestNetnsCollectedTakesDial(t *testing.T) {
	addr := os.Getenv(takesDialEnv)
	if addr == "" {
		t.Skip("helper for the device side of the collected takes oracle")
	}
	if conn, err := net.DialTimeout("tcp", addr, 400*time.Millisecond); err == nil {
		_ = conn.Close()
	}
}

func (r *takesRig) dial(t *testing.T, router bool, addr string) {
	t.Helper()
	if router {
		if conn, err := net.DialTimeout("tcp", addr, 400*time.Millisecond); err == nil {
			_ = conn.Close()
		}
		return
	}
	cmd := exec.Command("nsenter", "-t", strconv.Itoa(r.devicePID), "-n", os.Args[0], "-test.run", "^TestNetnsCollectedTakesDial$")
	cmd.Env = append(os.Environ(), takesDialEnv+"="+addr)
	_ = cmd.Run()
}

func takesTransparentListener(t *testing.T, port int, v6 bool) net.Listener {
	t.Helper()
	network, level, opt := "tcp4", unix.SOL_IP, unix.IP_TRANSPARENT
	if v6 {
		network, level, opt = "tcp6", unix.SOL_IPV6, unix.IPV6_TRANSPARENT
	}
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var ctlErr error
		if err := c.Control(func(fd uintptr) { ctlErr = unix.SetsockoptInt(int(fd), level, opt, 1) }); err != nil {
			return err
		}
		return ctlErr
	}}
	ln, err := lc.Listen(context.Background(), network, net.JoinHostPort("", strconv.Itoa(port)))
	if err != nil {
		t.Fatalf("transparent listen on %s %d: %v", network, port, err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()
	return ln
}

func takesConsumer(kind takesKind) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "takes-consumer", "takes consumer", true
	set.Targets.UseCollectedIPs = true
	switch kind {
	case takesInterface:
		set.Routing.Enabled, set.Routing.Mode, set.Routing.EgressInterface = true, config.RoutingModeInterface, takesEgress
	case takesProxy:
		set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeProxy
		set.Routing.Upstream.Host, set.Routing.Upstream.Port = "127.0.0.1", 31911
	case takesMTProtoWS:
		set.Routing.Enabled, set.Routing.Mode = true, config.RoutingModeMTProtoWS
	case takesReuse:
		set.TCP.WSSizeReuse.Enabled = true
	}
	return &set
}

func takesConfig(engine string, kind takesKind) *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled, cfg.Queue.IPv6Enabled = true, true
	cfg.Queue.Threads = 1
	cfg.Queue.Mark = 0x8000
	cfg.System.Tables.Engine = engine
	cfg.Sets = []*config.SetConfig{takesConsumer(kind)}
	return &cfg
}

func takesCells() []takesCell {
	kinds := []takesKind{takesInterface, takesProxy, takesMTProtoWS, takesReuse}
	var cells []takesCell
	add := func(name string, kind takesKind, shape func(*config.Config, *config.SetConfig, string), router, v6 bool, port int) {
		cells = append(cells, takesCell{name: name, kind: kind, shape: shape, router: router, v6: v6, port: port})
	}
	none := func(*config.Config, *config.SetConfig, string) {}
	for _, kind := range kinds {
		add("device 443", kind, none, false, false, 443)
		add("device 8443", kind, none, false, false, 8443)
		add("device 8443, consumer dport_filter 443", kind, func(_ *config.Config, s *config.SetConfig, _ string) {
			s.TCP.DPortFilter = "443"
			s.TCPPortRanges = config.ParsePortRanges(s.TCP.DPortFilter)
		}, false, false, 8443)
		add("router 443", kind, func(_ *config.Config, s *config.SetConfig, _ string) {
			s.Routing.RouterTraffic = config.RouterTrafficInclude
		}, true, false, 443)
		add("router 443, router_traffic exclude", kind, func(_ *config.Config, s *config.SetConfig, _ string) {
			s.Routing.RouterTraffic = config.RouterTrafficExclude
		}, true, false, 443)
		add("router 443, consumer scoped to the device", kind, func(_ *config.Config, s *config.SetConfig, mac string) { s.Targets.SourceDevices = []string{mac} }, true, false, 443)
		add("device 443, consumer scoped to the device", kind, func(_ *config.Config, s *config.SetConfig, mac string) { s.Targets.SourceDevices = []string{mac} }, false, false, 443)
		add("device 443, consumer scoped to another device", kind, func(_ *config.Config, s *config.SetConfig, _ string) {
			s.Targets.SourceDevices = []string{"02:00:00:00:00:99"}
		}, false, false, 443)
		add("device 443, consumer excludes the device", kind, func(_ *config.Config, s *config.SetConfig, mac string) {
			s.Targets.SourceDevices, s.Targets.SourceDevicesExclude = []string{mac}, true
		}, false, false, 443)
		add("router 443, consumer excludes a device", kind, func(_ *config.Config, s *config.SetConfig, _ string) {
			s.Targets.SourceDevices, s.Targets.SourceDevicesExclude = []string{"02:00:00:00:00:99"}, true
		}, true, false, 443)
		add("device 443, consumer on the device's interface", kind, func(_ *config.Config, s *config.SetConfig, _ string) { s.Routing.SourceInterfaces = []string{takesLAN} }, false, false, 443)
		add("device 443, consumer on the device's interface and scoped to the device", kind, func(_ *config.Config, s *config.SetConfig, mac string) {
			s.Routing.SourceInterfaces, s.Targets.SourceDevices = []string{takesLAN}, []string{mac}
		}, false, false, 443)
		add("router 443, consumer on the LAN interface", kind, func(_ *config.Config, s *config.SetConfig, _ string) {
			s.Routing.SourceInterfaces, s.Routing.RouterTraffic = []string{takesLAN}, config.RouterTrafficInclude
		}, true, false, 443)
		add("router IPv6 443", kind, func(_ *config.Config, s *config.SetConfig, _ string) {
			s.Targets.IpsToMatch, s.Routing.RouterTraffic = []string{takesTarget6 + "/128"}, config.RouterTrafficInclude
		}, true, true, 443)
		add("device 443, consumer on another interface", kind, func(_ *config.Config, s *config.SetConfig, _ string) { s.Routing.SourceInterfaces = []string{"b4kz"} }, false, false, 443)
		add("device IPv6 443", kind, func(_ *config.Config, s *config.SetConfig, _ string) {
			s.Targets.IpsToMatch = []string{takesTarget6 + "/128"}
		}, false, true, 443)
		add("device IPv6 443, IPv6 queue off", kind, func(c *config.Config, s *config.SetConfig, _ string) {
			c.Queue.IPv6Enabled = false
			s.Targets.IpsToMatch = []string{takesTarget6 + "/128"}
		}, false, true, 443)
		add("device IPv6 443, IPv4-only consumer listing the address", kind, func(_ *config.Config, s *config.SetConfig, _ string) {
			s.Targets.IPVersion = "4"
			s.Targets.IpsToMatch = []string{takesTarget6 + "/128"}
		}, false, true, 443)
	}
	add("device 443, egress interface never appeared", takesInterface, func(_ *config.Config, s *config.SetConfig, _ string) { s.Routing.EgressInterface = takesMissing }, false, false, 443)
	add("router 443, egress interface never appeared", takesInterface, func(_ *config.Config, s *config.SetConfig, _ string) {
		s.Routing.EgressInterface, s.Routing.RouterTraffic = takesMissing, config.RouterTrafficInclude
	}, true, false, 443)
	for _, kind := range []takesKind{takesProxy, takesMTProtoWS} {
		cells = append(cells, takesCell{name: "device 443, no listener", kind: kind, shape: none, port: 443, noListener: true})
		cells = append(cells, takesCell{name: "router 443, no listener", kind: kind, shape: none, router: true, port: 443, noListener: true})
	}
	return cells
}

func (r *takesRig) firewallTakes(t *testing.T, engine string, c takesCell) (taken, predicate bool, route string) {
	t.Helper()
	cfg := takesConfig(engine, c.kind)
	c.shape(cfg, cfg.Sets[0], r.mac)
	view := cfg.WithCollectedIPs([]string{takesTarget, takesTarget6})
	if err := tables.AddRules(view); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	tables.RoutingSyncConfig(view)
	defer func() {
		tables.RoutingClearAll()
		_ = tables.ClearRules(view)
	}()
	set := view.Sets[0]
	published, ok := tables.CollectedRouteOf(set.Id)
	route = fmt.Sprintf("%+v published=%v", published, ok)
	if c.kind != takesInterface && !c.noListener {
		port := tproxy.PortFor(tproxy.MarkForConfig(view, set))
		for _, v6 := range []bool{false, true} {
			ln := takesTransparentListener(t, port, v6)
			defer ln.Close()
		}
	}
	dst, client, scope := net.ParseIP(takesTarget), net.ParseIP(takesDevice), &ingressScope{resolved: true, name: takesLAN}
	ver, mac := uint8(IPv4), r.mac
	if c.v6 {
		dst, client, ver = net.ParseIP(takesTarget6), net.ParseIP(takesDevice6), IPv6
	}
	if c.router {
		client, scope, mac = net.ParseIP(takesUplinkIP), &ingressScope{resolved: true, router: true}, ""
		if c.v6 {
			client = net.ParseIP(takesUplinkIP6)
		}
	}
	before := takesUplinkPackets(t)
	r.dial(t, c.router, net.JoinHostPort(dst.String(), strconv.Itoa(c.port)))
	taken = takesUplinkPackets(t) == before
	h := helloWatch{dst: dst, port: uint16(c.port), ver: ver, client: client, mac: mac, scope: scope}
	predicate = collectedTakenAway(view, buildMatcher(view), &h)
	return taken, predicate, route
}

func takesOracle(t *testing.T, engine string) {
	takesRequire(t, engine)
	r := takesSetUp(t)
	var disagree []string
	for _, c := range takesCells() {
		taken, predicate, route := r.firewallTakes(t, engine, c)
		t.Logf("%-12s %-58s firewall takes=%-5v predicate=%-5v route %s", c.kind, c.name, taken, predicate, route)
		if taken != predicate {
			disagree = append(disagree, fmt.Sprintf("%s, %s: the firewall takes it=%v, collectedTakenAway=%v", c.kind, c.name, taken, predicate))
		}
	}
	for _, d := range disagree {
		t.Errorf("%s", d)
	}
}

func TestNetnsCollectedTakesFollowsTheFirewallNft(t *testing.T) {
	takesOracle(t, "nftables")
}

func TestNetnsCollectedTakesFollowsTheFirewallIptables(t *testing.T) {
	takesOracle(t, "iptables")
}
