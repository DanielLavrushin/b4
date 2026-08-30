package tables

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
	"golang.org/x/sys/unix"
)

const (
	netnsPrimary   = "b4t0"
	netnsSecondary = "b4t1"
	netnsPrimaryIP = "10.201.1.1"
	netnsSecondIP  = "10.201.2.1"
	netnsPrimaryGW = "10.201.1.2"
	netnsSecondGW  = "10.201.2.2"
	netnsTarget    = "198.51.100.7"
)

func netnsRequire(t *testing.T) {
	t.Helper()
	if os.Getenv("B4_NETNS_TEST") != "1" {
		t.Skip("set B4_NETNS_TEST=1 and run inside a network namespace (make test-netns)")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root inside the namespace")
	}
	self, err1 := os.Readlink("/proc/self/ns/net")
	init, err2 := os.Readlink("/proc/1/ns/net")
	if err1 == nil && err2 == nil && self == init {
		t.Fatal("refusing to run: this is the host network namespace, and the test writes firewall rules")
	}
	for _, bin := range []string{"ip", "iptables", "ipset"} {
		if !hasBinary(bin) {
			t.Skipf("%s is not installed", bin)
		}
	}
}

func netnsRun(t *testing.T, args ...string) string {
	t.Helper()
	out, err := run(args...)
	if err != nil {
		t.Fatalf("%s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(out))
	}
	return out
}

func netnsLinkMAC(t *testing.T, name string) string {
	t.Helper()
	iface, err := net.InterfaceByName(name)
	if err != nil {
		t.Fatalf("interface %s: %v", name, err)
	}
	return iface.HardwareAddr.String()
}

func netnsSetupLinks(t *testing.T) {
	t.Helper()
	if _, err := net.InterfaceByName(netnsPrimary); err == nil {
		return
	}
	netnsRun(t, "ip", "link", "set", "lo", "up")
	_, _ = run("sysctl", "-w", "net.ipv6.conf.all.disable_ipv6=1")

	for _, l := range []struct{ dev, peer, addr string }{
		{netnsPrimary, netnsPrimary + "p", netnsPrimaryIP},
		{netnsSecondary, netnsSecondary + "p", netnsSecondIP},
	} {
		netnsRun(t, "ip", "link", "add", l.dev, "type", "veth", "peer", "name", l.peer)
		netnsRun(t, "ip", "link", "set", l.dev, "up")
		netnsRun(t, "ip", "link", "set", l.peer, "up")
		netnsRun(t, "ip", "addr", "add", l.addr+"/24", "dev", l.dev)
	}

	netnsRun(t, "ip", "neigh", "add", netnsPrimaryGW, "lladdr", netnsLinkMAC(t, netnsPrimary+"p"), "dev", netnsPrimary, "nud", "permanent")
	netnsRun(t, "ip", "neigh", "add", netnsSecondGW, "lladdr", netnsLinkMAC(t, netnsSecondary+"p"), "dev", netnsSecondary, "nud", "permanent")
	netnsRun(t, "ip", "route", "add", "default", "via", netnsPrimaryGW, "dev", netnsPrimary)
}

func netnsStartQueueListener(t *testing.T, qnum uint16) func() {
	t.Helper()
	nf, err := nfqueue.Open(&nfqueue.Config{
		NfQueue:      qnum,
		MaxPacketLen: 0xffff,
		MaxQueueLen:  1024,
		Copymode:     nfqueue.NfQnlCopyPacket,
	})
	if err != nil {
		t.Fatalf("bind queue %d: %v", qnum, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	err = nf.RegisterWithErrorFunc(ctx,
		func(a nfqueue.Attribute) int {
			if a.PacketID != nil {
				_ = nf.SetVerdict(*a.PacketID, nfqueue.NfAccept)
			}
			return 0
		},
		func(error) int { return 0 },
	)
	if err != nil {
		cancel()
		_ = nf.Close()
		t.Fatalf("register queue %d: %v", qnum, err)
	}
	return func() {
		cancel()
		_ = nf.Close()
	}
}

func netnsConfig(engine string) *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.Queue.Threads = 1
	cfg.Queue.Mark = 0x8000
	cfg.System.Tables.Engine = engine
	cfg.System.Tables.SkipSetup = false

	set := config.NewSetConfig()
	set.Id = "netns-egress-set"
	set.Name = "netns"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeInterface
	set.Routing.EgressInterface = netnsSecondary
	set.Targets.IPs = []string{netnsTarget}
	set.Targets.IpsToMatch = []string{netnsTarget}

	cfg.Sets = []*config.SetConfig{&set}
	return &cfg
}

func netnsRoutingSetName(t *testing.T, engine string) string {
	t.Helper()
	if engine == backendNFTables {
		out := netnsRun(t, "nft", "list", "sets", "table", "inet", routeNftTable)
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "set b4r_") && strings.HasSuffix(line, "_v4 {") {
				return strings.TrimSuffix(strings.TrimPrefix(line, "set "), " {")
			}
		}
		t.Fatalf("no b4 routing set was created:\n%s", out)
	}
	out := netnsRun(t, "ipset", "list", "-n")
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "b4r_") && strings.HasSuffix(line, "_v4") {
			return line
		}
	}
	t.Fatalf("no b4 routing ipset was created:\n%s", out)
	return ""
}

func netnsAddCounters(t *testing.T) {
	t.Helper()
	for _, dev := range []string{netnsPrimary, netnsSecondary} {
		spec := []string{"iptables", "-w", "-t", "mangle", "-C", "POSTROUTING",
			"-o", dev, "-d", netnsTarget, "-p", "tcp", "-j", "ACCEPT"}
		if _, err := run(spec...); err == nil {
			continue
		}
		spec[4] = "-I"
		netnsRun(t, spec...)
	}
}

func netnsEgressCounts(t *testing.T) map[string]int {
	t.Helper()
	out := netnsRun(t, "iptables", "-w", "-t", "mangle", "-L", "POSTROUTING", "-v", "-x", "-n")
	counts := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, netnsTarget) {
			continue
		}
		f := strings.Fields(line)
		if len(f) < 7 {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			continue
		}
		counts[f[6]] = n
	}
	return counts
}

func netnsLogState(t *testing.T, engine string) {
	t.Helper()
	if engine == backendNFTables {
		out, _ := run("nft", "list", "table", "inet", routeNftTable)
		t.Logf("inet %s:\n%s", routeNftTable, out)
	} else {
		t.Logf("mangle OUTPUT:\n%s", netnsRun(t, "iptables", "-w", "-t", "mangle", "-S", "OUTPUT"))
		out, _ := run("sh", "-c", "iptables -t mangle -S | grep b4r_")
		t.Logf("routing chains:\n%s", out)
	}
	t.Logf("ip rule:\n%s", netnsRun(t, "ip", "rule", "show"))
	out, _ := run("sh", "-c", "ip route show table all | grep -v '^broadcast\\|^local\\|^multicast'")
	t.Logf("routes:\n%s", out)
}

func netnsZeroCounters(t *testing.T) {
	t.Helper()
	netnsRun(t, "iptables", "-w", "-t", "mangle", "-Z", "POSTROUTING")
}

func netnsTCPPacket(src, dst net.IP, sport, dport uint16) []byte {
	pkt := make([]byte, 40)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:], 40)
	binary.BigEndian.PutUint16(pkt[4:], 0x4242)
	pkt[8] = 64
	pkt[9] = 6
	copy(pkt[12:16], src.To4())
	copy(pkt[16:20], dst.To4())

	tcp := pkt[20:]
	binary.BigEndian.PutUint16(tcp[0:], sport)
	binary.BigEndian.PutUint16(tcp[2:], dport)
	binary.BigEndian.PutUint32(tcp[4:], 0x1000)
	tcp[12] = 5 << 4
	tcp[13] = 0x02
	binary.BigEndian.PutUint16(tcp[14:], 64240)

	sum := uint32(0)
	for i := 12; i < 20; i += 2 {
		sum += uint32(binary.BigEndian.Uint16(pkt[i:]))
	}
	sum += uint32(6) + uint32(len(tcp))
	for i := 0; i+1 < len(tcp); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(tcp[i:]))
	}
	for sum>>16 != 0 {
		sum = (sum & 0xFFFF) + (sum >> 16)
	}
	binary.BigEndian.PutUint16(tcp[16:], ^uint16(sum))
	return pkt
}

func netnsSendMarked(t *testing.T, mark uint32, sport, dport uint16) {
	t.Helper()
	s, err := sock.NewSenderWithMark(int(mark))
	if err != nil {
		t.Fatalf("raw socket with mark 0x%x: %v", mark, err)
	}
	defer s.Close()

	dst := net.ParseIP(netnsTarget)
	pkt := netnsTCPPacket(net.ParseIP(netnsPrimaryIP), dst, sport, dport)
	if err := s.SendIPv4(pkt, dst); err != nil {
		t.Fatalf("send with mark 0x%x: %v", mark, err)
	}
	time.Sleep(150 * time.Millisecond)
}

func TestNetnsInjectedPacketFollowsTheSetsInterface(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) { netnsEgressMatrix(t, engine) })
	}
}

func netnsEgressMatrix(t *testing.T, engine string) {
	t.Helper()
	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(engine)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	setName := netnsRoutingSetName(t, engine)
	if engine == backendNFTables {
		netnsRun(t, "nft", "add", "element", "inet", routeNftTable, setName, "{", netnsTarget, "}")
	} else {
		netnsRun(t, "ipset", "add", setName, netnsTarget, "-exist")
	}
	netnsAddCounters(t)

	stopQueue := netnsStartQueueListener(t, uint16(cfg.Queue.StartNum))
	defer stopQueue()

	netnsLogState(t, engine)

	for _, tc := range []struct {
		name  string
		mark  uint32
		sport uint16
		dport uint16
		want  string
		why   string
	}{
		{
			name:  "packet the engine injected",
			mark:  0x8000,
			sport: 41001,
			dport: 443,
			want:  netnsSecondary,
			why:   "every fake, split and desync packet carries the queue mark; sending it out the main table splits one connection across two uplinks",
		},
		{
			name:  "router traffic to a routed destination",
			mark:  0,
			sport: 41002,
			dport: 8443,
			want:  netnsSecondary,
			why:   "unmarked router traffic to a set's destination follows the set",
		},
		{
			name:  "connection b4 opened for itself",
			mark:  config.SelfDialMark,
			sport: 41003,
			dport: 443,
			want:  netnsPrimary,
			why:   "b4's own dials stay on the main table so they are not pulled into a set's egress",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			netnsZeroCounters(t)
			netnsSendMarked(t, tc.mark, tc.sport, tc.dport)

			counts := netnsEgressCounts(t)
			if counts[tc.want] != 1 {
				t.Errorf("mark 0x%x: expected the packet on %s, counters were %v - %s", tc.mark, tc.want, counts, tc.why)
			}
			for dev, n := range counts {
				if dev != tc.want && n != 0 {
					t.Errorf("mark 0x%x: %d packet(s) left over %s instead", tc.mark, n, dev)
				}
			}
		})
	}
}

func TestNetnsOutputChainSitsAboveTheQueueAccept(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(backendIPTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	out := netnsRun(t, "iptables", "-w", "-t", "mangle", "-S", "OUTPUT")
	jump, accept := -1, -1
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		switch {
		case strings.Contains(line, "-j b4r_") && strings.HasSuffix(line, "_out"):
			jump = i
		case strings.Contains(line, fmt.Sprintf("--mark 0x%x/0x%x -j ACCEPT", cfg.Queue.Mark, cfg.Queue.Mark)):
			accept = i
		}
	}
	if jump < 0 {
		t.Fatalf("the set's OUTPUT chain is not hung off mangle OUTPUT:\n%s", out)
	}
	if accept < 0 {
		t.Fatalf("b4's queue-mark ACCEPT is missing from mangle OUTPUT:\n%s", out)
	}
	if jump > accept {
		t.Errorf("the set's chain is below the queue-mark ACCEPT, which ends the mangle table, so injected packets never reach it:\n%s", out)
	}
}

func netnsAddrPresent(t *testing.T, iface, ip string) bool {
	t.Helper()
	out, err := run("ip", "-o", "addr", "show", "dev", iface)
	if err != nil {
		t.Fatalf("ip addr show %s: %v", iface, err)
	}
	for _, line := range strings.Split(out, "\n") {
		for _, f := range strings.Fields(line) {
			if f == ip || strings.HasPrefix(f, ip+"/") {
				return true
			}
		}
	}
	return false
}

func TestNetnsEgressIPIsPutOnTheInterfaceAndTakenBack(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	const egressIP = "10.201.2.77"

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(backendIPTables)
	cfg.Sets[0].Routing.EgressIP = egressIP

	if netnsAddrPresent(t, netnsSecondary, egressIP) {
		t.Fatalf("%s already sits on %s before the test runs", egressIP, netnsSecondary)
	}

	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)

	if !netnsAddrPresent(t, netnsSecondary, egressIP) {
		t.Fatalf("b4 did not put %s on %s, so the address has to be added by hand and does not survive a reboot", egressIP, netnsSecondary)
	}

	out := netnsRun(t, "iptables", "-w", "-t", "nat", "-S")
	if !strings.Contains(out, "--to-source "+egressIP) {
		t.Errorf("the set fell back to masquerade instead of rewriting the source to %s:\n%s", egressIP, out)
	}

	RoutingClearAll()

	if netnsAddrPresent(t, netnsSecondary, egressIP) {
		t.Errorf("b4 left %s behind on %s after the set stopped using it", egressIP, netnsSecondary)
	}
}

func TestNetnsEgressIPAddedByHandIsNotTakenAway(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	const egressIP = "10.201.2.78"

	routeEngine = nil
	defer func() { routeEngine = nil }()

	netnsRun(t, "ip", "addr", "add", egressIP+"/32", "dev", netnsSecondary)
	defer func() { _, _ = run("ip", "addr", "del", egressIP+"/32", "dev", netnsSecondary) }()

	cfg := netnsConfig(backendIPTables)
	cfg.Sets[0].Routing.EgressIP = egressIP

	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	RoutingClearAll()

	if !netnsAddrPresent(t, netnsSecondary, egressIP) {
		t.Errorf("b4 removed %s from %s, but it was configured by hand and b4 must only take back what it added itself", egressIP, netnsSecondary)
	}
}

const netnsProxyTarget = "198.51.100.9"

func netnsProxyMixConfig() *config.Config {
	cfg := netnsConfig(backendNFTables)

	proxy := config.NewSetConfig()
	proxy.Id = "netns-proxy-set"
	proxy.Name = "netnsproxy"
	proxy.Enabled = true
	proxy.Routing.Enabled = true
	proxy.Routing.Mode = config.RoutingModeProxy
	proxy.Routing.Upstream.Host = "127.0.0.1"
	proxy.Routing.Upstream.Port = 1080
	proxy.Targets.IPs = []string{netnsProxyTarget}
	proxy.Targets.IpsToMatch = []string{netnsProxyTarget}

	cfg.Sets = append(cfg.Sets, &proxy)
	return cfg
}

func TestNetnsProxySetDoesNotSwallowTheInjectedPacket(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsProxyMixConfig()
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	st, ok := routeRuleCache["netns-egress-set"]
	if !ok {
		t.Fatal("the interface set built no rules")
	}
	netnsRun(t, "nft", "add", "element", "inet", routeNftTable, st.setV4, "{", netnsTarget, "}")
	netnsAddCounters(t)

	stopQueue := netnsStartQueueListener(t, uint16(cfg.Queue.StartNum))
	defer stopQueue()

	out := netnsRun(t, "nft", "-a", "list", "chain", "inet", routeNftTable, routeNftOutput)
	t.Logf("base output chain with a proxy set present:\n%s", out)

	firstReturn, jump := -1, -1
	for i, line := range strings.Split(strings.TrimSpace(out), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case firstReturn < 0 && strings.Contains(line, "meta mark") && strings.HasSuffix(strings.Split(line, "#")[0], "return "):
			firstReturn = i
		case jump < 0 && strings.Contains(line, "jump "+st.chainOut):
			jump = i
		}
	}
	if jump < 0 {
		t.Fatalf("the interface set's out chain is not hung off the base output chain:\n%s", out)
	}
	if firstReturn >= 0 && firstReturn < jump {
		t.Errorf("a return sits at line %d, above the jump to %s at line %d; the base chain is a hook, so returning there ends it for every set below:\n%s",
			firstReturn, st.chainOut, jump, out)
	}

	proxySt, ok := routeRuleCache["netns-proxy-set"]
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	proxyChain := netnsRun(t, "nft", "list", "chain", "inet", routeNftTable, proxySt.chainOut)
	for _, want := range []string{
		fmt.Sprintf("0x%08x", cfg.Queue.Mark),
		fmt.Sprintf("0x%08x", SelfDialMark),
	} {
		if !strings.Contains(proxyChain, want) {
			t.Errorf("the proxy set's own chain lost its %s return, so a connection b4 opens to a proxied address loops into b4's listener:\n%s", want, proxyChain)
		}
	}

	netnsZeroCounters(t)
	netnsSendMarked(t, uint32(cfg.Queue.Mark), 40001, 443)
	counts := netnsEgressCounts(t)
	if counts[netnsSecondary] == 0 {
		t.Errorf("a packet b4 injected for the interface set left by %v, not by %s; with a proxy set configured the injected-packet rule is never reached",
			counts, netnsSecondary)
	}
}

func TestNetnsLegacyBaseOutputBypassIsSweptOnSync(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsProxyMixConfig()
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	for _, m := range []uint32{uint32(cfg.Queue.Mark), SelfDialMark} {
		hex := fmt.Sprintf("0x%x", m)
		netnsRun(t, "nft", "insert", "rule", "inet", routeNftTable, routeNftOutput,
			"meta", "mark", "&", hex, "==", hex, "return")
	}

	before := netnsRun(t, "nft", "list", "chain", "inet", routeNftTable, routeNftOutput)
	if !strings.Contains(before, "return") {
		t.Fatalf("the legacy rules were not planted:\n%s", before)
	}

	RoutingSyncConfig(cfg)

	after := netnsRun(t, "nft", "list", "chain", "inet", routeNftTable, routeNftOutput)
	if strings.Contains(after, "return") {
		t.Errorf("an upgrade leaves these behind, and they end the output hook before any set's chain runs:\n%s", after)
	}
	if !strings.Contains(after, "jump b4r_") {
		t.Errorf("the sweep took the jumps with it:\n%s", after)
	}
}

func netnsTransparentListener(t *testing.T, port int) net.Listener {
	t.Helper()
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var ctlErr error
			if err := c.Control(func(fd uintptr) {
				if e := unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_TRANSPARENT, 1); e != nil {
					ctlErr = e
				}
			}); err != nil {
				return err
			}
			return ctlErr
		},
	}
	ln, err := lc.Listen(context.Background(), "tcp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		t.Fatalf("transparent listen on %d: %v", port, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func TestNetnsProxySetTProxiesTheRoutersOwnDial(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsProxyMixConfig()
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	st, ok := routeRuleCache["netns-proxy-set"]
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	_, _ = run("nft", "add", "element", "inet", routeNftTable, st.setV4, "{", netnsProxyTarget, "}")

	port, _ := portFromState(st)
	ln := netnsTransparentListener(t, port)

	accepted := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn.LocalAddr().String()
		_ = conn.Close()
	}()

	conn, err := net.DialTimeout("tcp", netnsProxyTarget+":443", 3*time.Second)
	if err != nil {
		chain := netnsRun(t, "nft", "list", "chain", "inet", routeNftTable, st.chainPre)
		t.Fatalf("the router's own dial to a proxied address never completed (%v); this is the path b4's built-in SOCKS5 server takes, and the chain guard returns it above the tproxy rule unless the guard exempts the set's own mark 0x%x:\n%s", err, st.mark, chain)
	}
	defer conn.Close()

	select {
	case local := <-accepted:
		if !strings.HasPrefix(local, netnsProxyTarget+":") {
			t.Errorf("the tproxy listener accepted %s, not the original destination %s; the connection was not transparently diverted", local, netnsProxyTarget)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the dial connected but the transparent listener never accepted it")
	}

	chain := netnsRun(t, "nft", "list", "chain", "inet", routeNftTable, st.chainPre)
	if !strings.Contains(chain, fmt.Sprintf("!= 0x%08x", st.mark)) && !strings.Contains(chain, fmt.Sprintf("!= 0x%x", st.mark)) {
		t.Errorf("the pre chain guard carries no exemption for the set's own mark 0x%x, so the dial above succeeded for some other reason:\n%s", st.mark, chain)
	}
}

func netnsRuleLines(t *testing.T) string {
	t.Helper()
	return netnsRun(t, "ip", "rule", "show")
}

func TestNetnsTheStaleSweepLeavesTheLiveRuleStanding(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(backendIPTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)

	routeMu.Lock()
	st, ok := routeRuleCache[cfg.Sets[0].Id]
	routeMu.Unlock()
	if !ok {
		t.Fatal("the set built no routing state")
	}

	want := routeSetMarkRule(st.mark)
	table := strconv.Itoa(st.table)
	prio := strconv.Itoa(routePolicyRuleBase + st.table)

	legacy := []string{fmt.Sprintf("0x%x", st.mark), fmt.Sprintf("0x%x/0x%x", st.mark, st.mark)}
	for _, m := range legacy {
		netnsRun(t, "ip", "rule", "add", "fwmark", m, "lookup", table, "priority", prio)
	}
	defer func() {
		for _, m := range legacy {
			_, _ = run("ip", "rule", "del", "fwmark", m, "lookup", table)
		}
	}()

	routeDelStaleRuleForms(st.mark, table)

	lines := netnsRuleLines(t)
	live := 0
	for _, line := range strings.Split(lines, "\n") {
		if routeRuleField(line, "fwmark") == want && routeRuleField(line, "lookup") == table {
			live++
		}
	}
	if live != 1 {
		t.Fatalf("the kernel holds %d rule(s) sending %s to table %s after the stale sweep, want exactly one. A "+
			"shape asked for without a mask reaches the kernel with no FRA_FWMASK, and before 4.18 the delete "+
			"matched on the mark alone and took the live rule with it, leaving the set marked with nothing "+
			"pointing at its table:\n%s", live, want, table, lines)
	}

	for _, m := range legacy {
		for _, line := range strings.Split(lines, "\n") {
			if routeRuleField(line, "fwmark") == m && routeRuleField(line, "lookup") == table {
				t.Errorf("the rule an older b4 wrote as %q is still in the kernel and still steers traffic into "+
					"table %s:\n%s", m, table, lines)
			}
		}
	}
}

func TestNetnsAPolicyRuleTakenAwayIsPutBack(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(backendIPTables)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)

	routeMu.Lock()
	st, ok := routeRuleCache[cfg.Sets[0].Id]
	routeMu.Unlock()
	if !ok {
		t.Fatal("the set built no routing state")
	}

	want := routeSetMarkRule(st.mark)
	table := strconv.Itoa(st.table)
	netnsRun(t, "ip", "rule", "del", "fwmark", want, "lookup", table)

	RoutingReconcilePolicyRules(cfg)

	lines := netnsRuleLines(t)
	found := false
	for _, line := range strings.Split(lines, "\n") {
		if routeRuleField(line, "fwmark") == want && routeRuleField(line, "lookup") == table {
			found = true
		}
	}
	if !found {
		t.Fatalf("the firmware rebuilding its own rules takes b4's policy rule with it, and nothing read it back, "+
			"so the set kept marking traffic that then left by the ordinary uplink:\n%s", lines)
	}
}
