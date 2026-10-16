package tables

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

type gwNode struct {
	pid int
}

func gwStartNode(t *testing.T) *gwNode {
	t.Helper()
	for _, bin := range []string{"unshare", "nsenter"} {
		if !hasBinary(bin) {
			t.Skipf("%s is not installed", bin)
		}
	}
	cmd := exec.Command("unshare", "-n", "sleep", "600")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot open a network namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	self, _ := os.Readlink("/proc/thread-self/ns/net")
	deadline := time.Now().Add(3 * time.Second)
	for {
		other, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/net", cmd.Process.Pid))
		if err == nil && other != self {
			break
		}
		if time.Now().After(deadline) {
			t.Skip("the child never entered its own network namespace")
		}
		time.Sleep(20 * time.Millisecond)
	}
	n := &gwNode{pid: cmd.Process.Pid}
	n.run(t, "ip", "link", "set", "lo", "up")
	return n
}

func (n *gwNode) run(t *testing.T, args ...string) string {
	t.Helper()
	return netnsRun(t, append([]string{"nsenter", "-t", strconv.Itoa(n.pid), "-n"}, args...)...)
}

func (n *gwNode) attach(t *testing.T, routerSide, nodeSide string) string {
	t.Helper()
	_, _ = run("ip", "link", "del", routerSide)
	netnsRun(t, "ip", "link", "add", routerSide, "type", "veth", "peer", "name", nodeSide)
	t.Cleanup(func() { _, _ = run("ip", "link", "del", routerSide) })
	mac := netnsLinkMAC(t, nodeSide)
	netnsRun(t, "ip", "link", "set", nodeSide, "netns", strconv.Itoa(n.pid))
	n.run(t, "ip", "link", "set", nodeSide, "up")
	return mac
}

func (n *gwNode) inside(fn func() error) error {
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		home, err := os.Open("/proc/thread-self/ns/net")
		if err != nil {
			done <- err
			return
		}
		defer home.Close()
		ns, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", n.pid))
		if err != nil {
			done <- err
			return
		}
		defer ns.Close()
		if err := unix.Setns(int(ns.Fd()), unix.CLONE_NEWNET); err != nil {
			done <- err
			return
		}
		err = fn()
		if unix.Setns(int(home.Fd()), unix.CLONE_NEWNET) == nil {
			runtime.UnlockOSThread()
		}
		done <- err
	}()
	return <-done
}

func gwKeepSysctl(t *testing.T, key, value string) {
	t.Helper()
	t.Cleanup(setDSCPKeepSysctl(key))
	if err := os.WriteFile("/proc/sys/"+key, []byte(value), 0o644); err != nil {
		t.Fatalf("set %s: %v", key, err)
	}
}

func gwNftPackets(listing string) int {
	total := 0
	for _, line := range strings.Split(listing, "\n") {
		i := strings.Index(line, "counter packets ")
		if i < 0 {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(line[i:], "counter packets %d", &n); err == nil {
			total += n
		}
	}
	return total
}

type gwServer struct {
	accepted atomic.Int32
	mu       sync.Mutex
	peers    []string
	wg       sync.WaitGroup
}

func (s *gwServer) note(c net.Conn) int32 {
	s.mu.Lock()
	if addr, ok := c.RemoteAddr().(*net.TCPAddr); ok {
		s.peers = append(s.peers, addr.IP.String())
	}
	s.mu.Unlock()
	return s.accepted.Add(1)
}

func (s *gwServer) seen() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.peers)
}

func gwServe(t *testing.T, n *gwNode, addr string, handle func(c net.Conn, nth int32)) *gwServer {
	t.Helper()
	var ln net.Listener
	if err := n.inside(func() (err error) {
		ln, err = net.Listen("tcp4", addr)
		return err
	}); err != nil {
		t.Fatalf("listen on %s: %v", addr, err)
	}
	s := &gwServer{}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			nth := s.note(c)
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(4 * time.Second))
				handle(c, nth)
			}()
		}
	}()
	t.Cleanup(func() {
		_ = ln.Close()
		s.wg.Wait()
	})
	return s
}

const (
	gwArmBridge   = "b4gbr0"
	gwArmRouter   = "10.201.5.1"
	gwArmGateway  = "10.201.5.2"
	gwArmInet     = "b4gi0"
	gwArmInetIP   = "10.201.6.1"
	gwArmTarget   = "10.201.6.7"
	gwArmPort     = 18091
	gwArmProxy    = 18090
	gwArmObserver = "b4gwobs"
	gwArmLoopCap  = 4
)

func TestNetnsOneArmedGatewayCarriesALANClientWithoutALoop(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	if !hasBinary("nft") {
		t.Skip("the gateway's redirect and the counters use nft")
	}
	for i, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			for j, fwmarkReflect := range []string{"0", "1"} {
				client := fmt.Sprintf("10.201.5.%d", 100+i+2*j)
				t.Run("fwmark_reflect="+fwmarkReflect, func(t *testing.T) { gwOneArmed(t, engine, client, fwmarkReflect) })
			}
		})
	}
}

func gwOneArmed(t *testing.T, engine, client, fwmarkReflect string) {
	gwKeepSysctl(t, "net/ipv4/ip_forward", "1")
	gwKeepSysctl(t, "net/ipv4/conf/all/send_redirects", "1")
	gwKeepSysctl(t, "net/ipv4/fwmark_reflect", fwmarkReflect)

	_, _ = run("ip", "link", "del", gwArmBridge)
	netnsRun(t, "ip", "link", "add", gwArmBridge, "type", "bridge")
	t.Cleanup(func() { _, _ = run("ip", "link", "del", gwArmBridge) })
	netnsRun(t, "ip", "addr", "add", gwArmRouter+"/24", "dev", gwArmBridge)
	netnsRun(t, "ip", "link", "set", gwArmBridge, "up")

	gw, cl, inet := gwStartNode(t), gwStartNode(t), gwStartNode(t)
	for _, port := range []struct {
		node                 *gwNode
		router, inside, addr string
	}{
		{gw, "b4gpg", "b4ggw", gwArmGateway},
		{cl, "b4gpc", "b4gcl", client},
	} {
		port.node.attach(t, port.router, port.inside)
		netnsRun(t, "ip", "link", "set", port.router, "master", gwArmBridge)
		netnsRun(t, "ip", "link", "set", port.router, "up")
		port.node.run(t, "ip", "addr", "add", port.addr+"/24", "dev", port.inside)
		port.node.run(t, "ip", "route", "add", "default", "via", gwArmRouter)
	}
	inet.attach(t, gwArmInet, "b4gin")
	netnsRun(t, "ip", "addr", "add", gwArmInetIP+"/24", "dev", gwArmInet)
	netnsRun(t, "ip", "link", "set", gwArmInet, "up")
	inet.run(t, "ip", "addr", "add", gwArmTarget+"/24", "dev", "b4gin")
	inet.run(t, "ip", "route", "add", "default", "via", gwArmInetIP)

	gw.run(t, "nft", "add", "table", "ip", "b4gwarm")
	gw.run(t, "nft", "add", "chain", "ip", "b4gwarm", "pre", "{ type nat hook prerouting priority -100 ; }")
	gw.run(t, "nft", "add", "rule", "ip", "b4gwarm", "pre", "iifname", `"b4ggw"`, "ip", "daddr", gwArmTarget,
		"tcp", "dport", strconv.Itoa(gwArmPort), "counter", "redirect", "to", ":"+strconv.Itoa(gwArmProxy))
	cl.run(t, "nft", "add", "table", "inet", gwArmObserver)
	cl.run(t, "nft", "add", "chain", "inet", gwArmObserver, "in", "{ type filter hook prerouting priority -400 ; }")
	cl.run(t, "nft", "add", "rule", "inet", gwArmObserver, "in", "icmp", "type", "redirect", "counter")
	_, _ = run("nft", "delete", "table", "inet", gwArmObserver)
	netnsRun(t, "nft", "add", "table", "inet", gwArmObserver)
	t.Cleanup(func() { _, _ = run("nft", "delete", "table", "inet", gwArmObserver) })
	for _, hook := range []struct{ chain, priority string }{{"out", "-400"}, {"kept", "-125"}} {
		netnsRun(t, "nft", "add", "chain", "inet", gwArmObserver, hook.chain, "{ type filter hook output priority "+hook.priority+" ; }")
		netnsRun(t, "nft", "add", "rule", "inet", gwArmObserver, hook.chain, "oifname", `"`+gwArmBridge+`"`, "icmp", "type", "redirect", "counter")
	}

	targetAddr := net.JoinHostPort(gwArmTarget, strconv.Itoa(gwArmPort))
	target := gwServe(t, inet, targetAddr, func(c net.Conn, _ int32) {
		buf := make([]byte, 256)
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		_, _ = c.Write(append([]byte("target:"), buf[:n]...))
	})
	proxy := gwServe(t, gw, net.JoinHostPort("0.0.0.0", strconv.Itoa(gwArmProxy)), func(c net.Conn, nth int32) {
		buf := make([]byte, 256)
		n, err := c.Read(buf)
		if err != nil || nth > gwArmLoopCap {
			return
		}
		var up net.Conn
		if err := gw.inside(func() (err error) {
			up, err = net.DialTimeout("tcp4", targetAddr, 2*time.Second)
			return err
		}); err != nil {
			return
		}
		defer up.Close()
		_ = up.SetDeadline(time.Now().Add(3 * time.Second))
		if _, err := up.Write(buf[:n]); err != nil {
			return
		}
		if n, err = up.Read(buf); err != nil {
			return
		}
		_, _ = c.Write(buf[:n])
	})

	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })
	set := netnsGatewaySet("netns-gateway-onearm", gwArmBridge, gwArmGateway)
	set.Targets.IPs = []string{gwArmTarget}
	set.Targets.IpsToMatch = []string{gwArmTarget}
	netnsGatewayInstall(t, netnsGatewayConfig(engine, set))
	st := netnsGatewayState(t, set.Id)
	if !slices.Equal(st.gwPeersV4, []string{gwArmGateway}) {
		t.Fatalf("the gateway on the bridge must narrow the guard to itself, got gwV4=%q peers=%v", st.gwV4, st.gwPeersV4)
	}
	netnsRun(t, "nft", "add", "chain", "inet", gwArmObserver, "marked", "{ type filter hook output priority -400 ; }")
	netnsRun(t, "nft", "add", "rule", "inet", gwArmObserver, "marked", "oifname", `"`+gwArmBridge+`"`, "icmp", "type", "redirect",
		"meta", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "==", fmt.Sprintf("0x%x", st.mark), "counter")

	var reply string
	err := cl.inside(func() error {
		c, err := net.DialTimeout("tcp4", targetAddr, 3*time.Second)
		if err != nil {
			return err
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		if _, err := c.Write([]byte("hello")); err != nil {
			return err
		}
		buf := make([]byte, 256)
		n, err := c.Read(buf)
		reply = string(buf[:n])
		return err
	})
	redirected := gwNftPackets(gw.run(t, "nft", "list", "chain", "ip", "b4gwarm", "pre"))
	report := fmt.Sprintf("client error %v reply %q; connections at the gateway %d from %v, at the target %d from %v; the gateway redirected %d",
		err, reply, proxy.accepted.Load(), proxy.seen(), target.accepted.Load(), target.seen(), redirected)
	if err != nil || reply != "target:hello" {
		netnsLogState(t, engine)
		t.Fatalf("the client on the gateway's segment did not reach the target through the gateway: %s", report)
	}
	if redirected != 1 || proxy.accepted.Load() != 1 {
		t.Errorf("the client's connection must reach the gateway exactly once: %s", report)
	}
	if peers := target.seen(); !slices.Equal(peers, []string{gwArmGateway}) {
		t.Errorf("the target must see one connection, the gateway's own, which b4 left unmarked: %s", report)
	}

	sent := gwNftPackets(netnsRun(t, "nft", "list", "chain", "inet", gwArmObserver, "out"))
	kept := gwNftPackets(netnsRun(t, "nft", "list", "chain", "inet", gwArmObserver, "kept"))
	got := gwNftPackets(cl.run(t, "nft", "list", "chain", "inet", gwArmObserver, "in"))
	marked := gwNftPackets(netnsRun(t, "nft", "list", "chain", "inet", gwArmObserver, "marked"))
	if sent == 0 {
		t.Errorf("the router sent the client's packets back out the interface they came in on without generating a redirect, so this run cannot show the drop")
	}
	if fwmarkReflect == "1" && marked != sent {
		t.Errorf("with net.ipv4.fwmark_reflect=1 every redirect the router generates for the hairpin carries the set's mark 0x%x, got %d of %d", st.mark, marked, sent)
	}
	if fwmarkReflect == "0" && marked != 0 {
		t.Errorf("without mark reflection a redirect carries no mark, yet %d of %d carried the set's mark 0x%x", marked, sent, st.mark)
	}
	if kept != 0 {
		t.Errorf("%d of the %d ICMP redirect(s) the router generated for the hairpin got past the set's out chain, which must drop them", kept, sent)
	}
	if got != 0 {
		t.Errorf("the client received %d ICMP redirect(s) naming the gateway, so it would stop sending the set's traffic through the router", got)
	}
}

const (
	gw6Egress     = "b4g6"
	gw6Upstream   = "b4g6p"
	gw6Lan        = "b4g6l"
	gw6LanPeer    = "b4g6lp"
	gw6Router     = "2001:db8:e6::1"
	gw6Gateway    = "2001:db8:e6::2"
	gw6Router4    = "10.201.8.1"
	gw6Gateway4   = "10.201.8.2"
	gw6Remote     = "2001:db8:99::7"
	gw6RemoteNet  = "2001:db8:99::/64"
	gw6LanRouter  = "2001:db8:1a::1"
	gw6LanHost    = "2001:db8:1a::100"
	gw6LanNet     = "2001:db8:1a::/64"
	gw6Observer   = "b4gw6obs"
	gw6SetGateway = "netns-gateway-v6"
	gw6SetNarrow  = "netns-gateway-v6-narrow"
)

func gw6Links(t *testing.T) (*gwNode, *gwNode) {
	t.Helper()
	for _, key := range []string{"net/ipv6/conf/all/disable_ipv6", "net/ipv6/conf/default/disable_ipv6"} {
		gwKeepSysctl(t, key, "0")
	}
	gwKeepSysctl(t, "net/ipv6/conf/all/forwarding", "1")

	up, lan := gwStartNode(t), gwStartNode(t)
	upMAC := up.attach(t, gw6Egress, gw6Upstream)
	netnsRun(t, "ip", "link", "set", gw6Egress, "up")
	netnsRun(t, "ip", "-6", "addr", "add", gw6Router+"/64", "dev", gw6Egress, "nodad")
	netnsRun(t, "ip", "addr", "add", gw6Router4+"/24", "dev", gw6Egress)
	netnsRun(t, "ip", "-6", "neigh", "replace", gw6Gateway, "lladdr", upMAC, "dev", gw6Egress, "nud", "permanent")
	netnsRun(t, "ip", "-6", "route", "add", gw6RemoteNet, "via", gw6Gateway, "dev", gw6Egress)
	up.run(t, "ip", "-6", "addr", "add", gw6Gateway+"/64", "dev", gw6Upstream, "nodad")
	up.run(t, "ip", "addr", "add", gw6Gateway4+"/24", "dev", gw6Upstream)
	up.run(t, "ip", "-6", "addr", "add", gw6Remote+"/128", "dev", "lo")
	up.run(t, "ip", "-6", "neigh", "replace", gw6Router, "lladdr", netnsLinkMAC(t, gw6Egress), "dev", gw6Upstream, "nud", "permanent")
	up.run(t, "ip", "-6", "route", "add", "default", "via", gw6Router, "dev", gw6Upstream)

	lanMAC := lan.attach(t, gw6Lan, gw6LanPeer)
	netnsRun(t, "ip", "link", "set", gw6Lan, "up")
	netnsRun(t, "ip", "-6", "addr", "add", gw6LanRouter+"/64", "dev", gw6Lan, "nodad")
	netnsRun(t, "ip", "-6", "neigh", "replace", gw6LanHost, "lladdr", lanMAC, "dev", gw6Lan, "nud", "permanent")
	lan.run(t, "ip", "-6", "addr", "add", gw6LanHost+"/64", "dev", gw6LanPeer, "nodad")
	lan.run(t, "ip", "-6", "neigh", "replace", gw6LanRouter, "lladdr", netnsLinkMAC(t, gw6Lan), "dev", gw6LanPeer, "nud", "permanent")
	lan.run(t, "ip", "-6", "route", "add", "default", "via", gw6LanRouter, "dev", gw6LanPeer)
	return up, lan
}

func gw6Observe(t *testing.T) func() (lan, back, marked int) {
	t.Helper()
	_, _ = run("nft", "delete", "table", "inet", gw6Observer)
	netnsRun(t, "nft", "add", "table", "inet", gw6Observer)
	t.Cleanup(func() { _, _ = run("nft", "delete", "table", "inet", gw6Observer) })
	netnsRun(t, "nft", "add", "chain", "inet", gw6Observer, "lan", "{ type filter hook postrouting priority 400 ; }")
	netnsRun(t, "nft", "add", "chain", "inet", gw6Observer, "back", "{ type filter hook postrouting priority 401 ; }")
	netnsRun(t, "nft", "add", "chain", "inet", gw6Observer, "marked", "{ type filter hook postrouting priority 402 ; }")
	netnsRun(t, "nft", "add", "rule", "inet", gw6Observer, "lan", "oifname", `"`+gw6Lan+`"`, "ip6", "daddr", gw6LanHost, "counter")
	netnsRun(t, "nft", "add", "rule", "inet", gw6Observer, "back", "oifname", `"`+gw6Egress+`"`, "ip6", "daddr", gw6LanHost, "counter")
	netnsRun(t, "nft", "add", "rule", "inet", gw6Observer, "marked", "ip6", "daddr", gw6LanHost,
		"meta", "mark", "&", fmt.Sprintf("0x%x", routeSetMarkMask), "!=", "0", "counter")
	return func() (int, int, int) {
		read := func(chain string) int {
			return gwNftPackets(netnsRun(t, "nft", "list", "chain", "inet", gw6Observer, chain))
		}
		return read("lan"), read("back"), read("marked")
	}
}

func TestNetnsIPv6GatewayRoutesTheSetAndLeavesInboundIPv6Alone(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) {
		if !hasBinary("ping") {
			t.Skip("ping is not installed")
		}
		if !hasBinary("nft") {
			t.Skip("the observer's counters use nft")
		}
		up, _ := gw6Links(t)
		counts := gw6Observe(t)

		withV6 := netnsGatewaySet(gw6SetGateway, gw6Egress, gw6Gateway)
		withV4 := netnsGatewaySet(gw6SetNarrow, gw6Egress, gw6Gateway4)
		for _, set := range []*config.SetConfig{withV6, withV4} {
			set.Targets.IPs = []string{netnsTarget, gw6LanNet}
			set.Targets.IpsToMatch = []string{netnsTarget, gw6LanNet}
		}
		cfg := netnsGatewayConfig(engine, withV6, withV4)
		cfg.Queue.IPv6Enabled = true
		netnsGatewayInstall(t, cfg)

		v6 := netnsGatewayState(t, gw6SetGateway)
		if v6.gwV6 != gw6Gateway || v6.gwV4 != "" {
			t.Fatalf("the IPv6 gateway must be the set's IPv6 next hop only, got gwV4=%q gwV6=%q", v6.gwV4, v6.gwV6)
		}
		narrowed := netnsGatewayState(t, gw6SetNarrow)
		if !slices.Equal(narrowed.gwPeersV4, []string{gw6Gateway4}) {
			t.Fatalf("the IPv4 gateway on the veth must narrow the IPv4 guard, got peers %v", narrowed.gwPeersV4)
		}
		want := fmt.Sprintf("default via %s dev %s", gw6Gateway, gw6Egress)
		assertRoute := func(when string) {
			t.Helper()
			if routes := netnsGatewayRoutes(t, netnsGatewayState(t, gw6SetGateway), true); !strings.Contains(routes, want) {
				t.Errorf("%s the set's IPv6 table must route through the IPv6 gateway, want %q in:\n%s", when, want, routes)
			}
		}
		assertRoute("after the sync")
		if routes := netnsGatewayRoutes(t, v6, false); strings.Contains(routes, "via") {
			t.Errorf("an IPv6 gateway must leave the set's IPv4 route on the interface's own route:\n%s", routes)
		}
		want4 := fmt.Sprintf("default via %s dev %s", gw6Gateway4, gw6Egress)
		if routes := netnsGatewayRoutes(t, narrowed, false); !strings.Contains(routes, want4) {
			t.Errorf("the IPv4 gateway set must route through %s, want %q in:\n%s", gw6Gateway4, want4, routes)
		}

		ipv6Guards := map[string]string{gw6SetGateway: netnsGatewayFullGuard(engine, gw6Egress), gw6SetNarrow: netnsGatewayIPv6Guard(engine, gw6Egress)}
		for id, guard := range ipv6Guards {
			st := netnsGatewayState(t, id)
			rules := netnsGatewayRules(t, engine, st.chainPre, engine != backendNFTables)
			if !slices.Contains(rules, guard) {
				t.Errorf("set %s must return every IPv6 packet arriving on %s, want %q in:\n%s", id, gw6Egress, guard, strings.Join(rules, "\n"))
			}
		}

		ping := func(when string) {
			t.Helper()
			lan0, back0, marked0 := counts()
			out, err := run("nsenter", "-t", strconv.Itoa(up.pid), "-n", "ping", "-6", "-c", "1", "-W", "2", "-I", gw6Remote, gw6LanHost)
			lan, back, marked := counts()
			if err != nil || lan == lan0 || back != back0 || marked != marked0 {
				netnsLogState(t, engine)
				t.Errorf("%s an inbound IPv6 packet from a host behind the gateway to a LAN host in the sets' targets must pass unmarked: ping %v, toward the LAN %d, back out %s %d, marked %d\n%s",
					when, err, lan-lan0, gw6Egress, back-back0, marked-marked0, strings.TrimSpace(out))
			}
		}
		ping("after the sync")

		RoutingForceResync(cfg)
		assertRoute("after a forced resync")
		mark := routeSetMarkRule(v6.mark)
		table := fmt.Sprint(v6.table)
		found := false
		for _, line := range strings.Split(netnsRun(t, "ip", "-6", "rule", "show"), "\n") {
			if routeRuleField(line, "fwmark") == mark && routeRuleField(line, "lookup") == table {
				found = true
			}
		}
		if !found {
			t.Errorf("after a forced resync the IPv6 policy rule for mark %s and table %s is gone", mark, table)
		}
		ping("after a forced resync")
	})
}
