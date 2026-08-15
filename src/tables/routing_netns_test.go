package tables

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
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
