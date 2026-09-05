package tables

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

const (
	netnsDevLink   = "b4t2"
	netnsDevPeer   = "b4t2p"
	netnsDevRouter = "10.201.3.1"
	netnsDevIP     = "10.201.3.100"
	netnsDevInet   = "198.51.100.7"
	netnsDevOther  = "198.51.100.8"
	netnsDevWeb    = 8080
)

type netnsDevice struct {
	pid int
	cmd *exec.Cmd
	mac string
}

func netnsStartDevice(t *testing.T) *netnsDevice {
	t.Helper()
	for _, bin := range []string{"unshare", "nsenter"} {
		if !hasBinary(bin) {
			t.Skipf("%s is not installed", bin)
		}
	}
	_, _ = run("ip", "link", "del", netnsDevLink)
	netnsRun(t, "ip", "link", "add", netnsDevLink, "type", "veth", "peer", "name", netnsDevPeer)
	netnsRun(t, "ip", "link", "set", netnsDevLink, "up")
	netnsRun(t, "ip", "addr", "add", netnsDevRouter+"/24", "dev", netnsDevLink)
	mac := netnsLinkMAC(t, netnsDevPeer)

	cmd := exec.Command("unshare", "-n", "sleep", "600")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot open a second network namespace: %v", err)
	}
	dev := &netnsDevice{pid: cmd.Process.Pid, cmd: cmd, mac: mac}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		_, _ = run("ip", "link", "del", netnsDevLink)
	})

	self, _ := os.Readlink("/proc/self/ns/net")
	deadline := time.Now().Add(3 * time.Second)
	for {
		other, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/net", dev.pid))
		if err == nil && other != self {
			break
		}
		if time.Now().After(deadline) {
			t.Skipf("the child never entered its own network namespace")
		}
		time.Sleep(20 * time.Millisecond)
	}

	netnsRun(t, "ip", "link", "set", netnsDevPeer, "netns", strconv.Itoa(dev.pid))
	dev.in(t, "ip", "link", "set", "lo", "up")
	dev.in(t, "ip", "link", "set", netnsDevPeer, "up")
	dev.in(t, "ip", "addr", "add", netnsDevIP+"/24", "dev", netnsDevPeer)
	dev.in(t, "ip", "route", "add", "default", "via", netnsDevRouter)
	return dev
}

func (d *netnsDevice) in(t *testing.T, args ...string) string {
	t.Helper()
	return netnsRun(t, append([]string{"nsenter", "-t", strconv.Itoa(d.pid), "-n"}, args...)...)
}

func (d *netnsDevice) probe(t *testing.T, kind, host string, port int, msg string) string {
	t.Helper()
	cmd := exec.Command("nsenter", "-t", strconv.Itoa(d.pid), "-n", os.Args[0], "-test.run", "^TestNetnsDeviceProbe$", "-test.v")
	cmd.Env = append(os.Environ(),
		"B4_NETNS_PROBE=1",
		"B4_PROBE_KIND="+kind,
		"B4_PROBE_HOST="+host,
		"B4_PROBE_PORT="+strconv.Itoa(port),
		"B4_PROBE_MSG="+msg,
	)
	out, _ := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "PROBE ") {
			return strings.TrimPrefix(line, "PROBE ")
		}
	}
	return "no probe output: " + strings.TrimSpace(string(out))
}

func TestNetnsDeviceProbe(t *testing.T) {
	if os.Getenv("B4_NETNS_PROBE") == "" {
		t.Skip("helper for the device side of the netns tests")
	}
	host := os.Getenv("B4_PROBE_HOST")
	port, _ := strconv.Atoi(os.Getenv("B4_PROBE_PORT"))
	msg := os.Getenv("B4_PROBE_MSG")
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	switch os.Getenv("B4_PROBE_KIND") {
	case "tcp":
		conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
		if err != nil {
			fmt.Printf("PROBE failed: %v\n", err)
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		if _, err := conn.Write([]byte(msg)); err != nil {
			fmt.Printf("PROBE failed: %v\n", err)
			return
		}
		buf := make([]byte, 256)
		n, err := conn.Read(buf)
		if err != nil {
			fmt.Printf("PROBE failed: %v\n", err)
			return
		}
		fmt.Printf("PROBE reply=%s\n", string(buf[:n]))
	case "udp":
		conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: 0})
		if err != nil {
			fmt.Printf("PROBE failed: %v\n", err)
			return
		}
		defer conn.Close()
		raw, _ := conn.SyscallConn()
		_ = raw.Control(func(fd uintptr) {
			_ = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_BROADCAST, 1)
		})
		dst := &net.UDPAddr{IP: net.ParseIP(host), Port: port}
		if _, err := conn.WriteToUDP([]byte(msg), dst); err != nil {
			fmt.Printf("PROBE failed: %v\n", err)
			return
		}
		fmt.Printf("PROBE sent\n")
	default:
		fmt.Printf("PROBE unknown kind\n")
	}
}

type netnsSockets struct {
	mu       sync.Mutex
	tcp      map[string][]string
	udp      map[string][]string
	accepted []string
}

func (s *netnsSockets) note(kind, key, what string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if kind == "tcp" {
		s.tcp[key] = append(s.tcp[key], what)
		return
	}
	s.udp[key] = append(s.udp[key], what)
}

func (s *netnsSockets) got(kind, key, what string) bool {
	deadline := time.Now().Add(1500 * time.Millisecond)
	for {
		s.mu.Lock()
		var list []string
		if kind == "tcp" {
			list = s.tcp[key]
		} else {
			list = s.udp[key]
		}
		for _, item := range list {
			if item == what {
				s.mu.Unlock()
				return true
			}
		}
		s.mu.Unlock()
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(30 * time.Millisecond)
	}
}

func netnsServeTCP(t *testing.T, ln net.Listener, key, prefix string, s *netnsSockets) {
	t.Helper()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				buf := make([]byte, 256)
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				msg := string(buf[:n])
				s.note("tcp", key, msg)
				if key == "tproxy" {
					s.mu.Lock()
					s.accepted = append(s.accepted, c.LocalAddr().String())
					s.mu.Unlock()
				}
				_, _ = c.Write([]byte(prefix + msg))
			}(conn)
		}
	}()
}

func netnsServeUDP(t *testing.T, uc *net.UDPConn, key string, s *netnsSockets, origDst bool) {
	t.Helper()
	go func() {
		buf := make([]byte, 2048)
		oob := make([]byte, 512)
		for {
			n, oobn, _, _, err := uc.ReadMsgUDP(buf, oob)
			if err != nil {
				return
			}
			what := string(buf[:n])
			if origDst {
				if dst, perr := parseNetnsOrigDst(oob[:oobn]); perr == nil {
					what += "@" + dst
				}
			}
			s.note("udp", key, what)
		}
	}()
}

func parseNetnsOrigDst(oob []byte) (string, error) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return "", err
	}
	for _, m := range msgs {
		if m.Header.Level == unix.IPPROTO_IP && m.Header.Type == unix.IP_ORIGDSTADDR && len(m.Data) >= unix.SizeofSockaddrInet4 {
			port := int(m.Data[2])<<8 | int(m.Data[3])
			ip := net.IPv4(m.Data[4], m.Data[5], m.Data[6], m.Data[7])
			return net.JoinHostPort(ip.String(), strconv.Itoa(port)), nil
		}
	}
	return "", fmt.Errorf("no original destination")
}

func netnsTransparentUDP(t *testing.T, port int) *net.UDPConn {
	t.Helper()
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var ctlErr error
			if err := c.Control(func(fd uintptr) {
				if e := unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_TRANSPARENT, 1); e != nil {
					ctlErr = e
					return
				}
				if e := unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_RECVORIGDSTADDR, 1); e != nil {
					ctlErr = e
				}
			}); err != nil {
				return err
			}
			return ctlErr
		},
	}
	pc, err := lc.ListenPacket(context.Background(), "udp4", fmt.Sprintf("0.0.0.0:%d", port))
	if err != nil {
		t.Fatalf("transparent udp listen on %d: %v", port, err)
	}
	t.Cleanup(func() { _ = pc.Close() })
	return pc.(*net.UDPConn)
}

func netnsMarkedTransparentListener(t *testing.T, port int, mark uint32) net.Listener {
	t.Helper()
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var ctlErr error
			if err := c.Control(func(fd uintptr) {
				if e := unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_TRANSPARENT, 1); e != nil {
					ctlErr = e
					return
				}
				if mark != 0 {
					if e := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark)); e != nil {
						ctlErr = e
					}
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

func netnsPlainUDP(t *testing.T, port int) *net.UDPConn {
	t.Helper()
	uc, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4zero, Port: port})
	if err != nil {
		t.Fatalf("plain udp listen on %d: %v", port, err)
	}
	t.Cleanup(func() { _ = uc.Close() })
	return uc
}

func netnsLocalGuardConfig(engine, mac string, ips []string) *config.Config {
	cfg := netnsConfig(engine)
	cfg.Sets = nil

	proxy := config.NewSetConfig()
	proxy.Id = "netns-localdst-proxy"
	proxy.Name = "netnslocaldst"
	proxy.Enabled = true
	proxy.Routing.Enabled = true
	proxy.Routing.Mode = config.RoutingModeProxy
	proxy.Routing.Upstream.Host = "127.0.0.1"
	proxy.Routing.Upstream.Port = 31901
	proxy.Routing.Upstream.UDP = true
	proxy.Routing.Upstream.FailOpen = true
	proxy.Targets.IPs = append([]string{}, ips...)
	proxy.Targets.IpsToMatch = append([]string{}, ips...)
	if mac != "" {
		proxy.Targets.SourceDevices = []string{mac}
	}
	cfg.Sets = []*config.SetConfig{&proxy}
	return cfg
}

func netnsRouterSockets(t *testing.T, port int, mark uint32) *netnsSockets {
	t.Helper()
	s := &netnsSockets{tcp: map[string][]string{}, udp: map[string][]string{}}

	web, err := net.Listen("tcp4", net.JoinHostPort(netnsDevRouter, strconv.Itoa(netnsDevWeb)))
	if err != nil {
		t.Fatalf("plain listen: %v", err)
	}
	t.Cleanup(func() { _ = web.Close() })
	netnsServeTCP(t, web, "plain", "plain:", s)
	netnsServeUDP(t, netnsPlainUDP(t, 53), "dns", s, false)
	netnsServeUDP(t, netnsPlainUDP(t, 67), "dhcp", s, false)

	netnsServeTCP(t, netnsMarkedTransparentListener(t, port, mark), "tproxy", "tproxy:", s)
	netnsServeUDP(t, netnsTransparentUDP(t, port), "tproxy", s, true)
	return s
}

func netnsSetMembers(t *testing.T, engine, setName string) string {
	t.Helper()
	if engine == backendNFTables {
		return netnsRun(t, "nft", "list", "set", "inet", routeNftTable, setName)
	}
	return netnsRun(t, "ipset", "list", setName)
}

func netnsPreChain(t *testing.T, engine, chain string) string {
	t.Helper()
	if engine == backendNFTables {
		return netnsRun(t, "nft", "list", "chain", "inet", routeNftTable, chain)
	}
	return netnsRun(t, "iptables", "-w", "-t", "mangle", "-S", chain)
}

func netnsCatchAllLeavesRouterLocalDestinationsAlone(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	if engine == backendNFTables && !hasBinary("nft") {
		t.Skip("nft is not installed")
	}
	dev := netnsStartDevice(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsLocalGuardConfig(engine, dev.mac, []string{"0.0.0.0/0", netnsDevInet})
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()
	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	st, ok := routeRuleCache["netns-localdst-proxy"]
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	port, _ := portFromState(st)
	s := netnsRouterSockets(t, port, 0)

	chain := netnsPreChain(t, engine, st.chainPre)
	guard := "-m addrtype --dst-type LOCAL,BROADCAST,MULTICAST -j RETURN"
	if engine == backendNFTables {
		guard = "fib daddr type { local, broadcast, multicast } return"
	}
	if !strings.Contains(chain, guard) {
		t.Fatalf("the pre chain carries no local destination guard:\n%s", chain)
	}
	if strings.Index(chain, guard) > strings.Index(chain, "socket") {
		t.Errorf("the guard sits below the divert rule:\n%s", chain)
	}

	if got := dev.probe(t, "tcp", netnsDevRouter, netnsDevWeb, "web"); got != "reply=plain:web" {
		t.Errorf("device -> router web: %s; the router's own service must answer, not the transparent listener:\n%s", got, chain)
	}
	dev.probe(t, "udp", netnsDevRouter, 53, "dns")
	if !s.got("udp", "dns", "dns") {
		t.Errorf("device -> router:53 never reached the router's DNS socket; with the catch-all in the set it was diverted to the upstream")
	}
	dev.probe(t, "udp", "255.255.255.255", 67, "dhcp")
	if !s.got("udp", "dhcp", "dhcp") {
		t.Errorf("device -> 255.255.255.255:67 never reached the DHCP socket")
	}
	if got := dev.probe(t, "tcp", netnsDevInet, 443, "inet"); got != "reply=tproxy:inet" {
		t.Errorf("device -> internet: %s; the set must still divert internet destinations", got)
	}
	dev.probe(t, "udp", netnsDevInet, 53, "inetdns")
	if !s.got("udp", "tproxy", "inetdns@"+netnsDevInet+":53") {
		t.Errorf("device -> internet UDP never reached the transparent socket with its original destination: %v", s.udp)
	}
	s.mu.Lock()
	for _, local := range s.accepted {
		if strings.HasPrefix(local, netnsDevRouter+":") {
			t.Errorf("the transparent listener accepted a connection to the router itself: %s", local)
		}
	}
	s.mu.Unlock()

	set := cfg.Sets[0]
	set.Targets.IPs = []string{netnsDevInet}
	set.Targets.IpsToMatch = []string{netnsDevInet}
	before := netnsPreChain(t, engine, st.chainPre)
	RoutingSyncConfig(cfg)
	if after := netnsPreChain(t, engine, st.chainPre); after != before {
		t.Errorf("removing an address rebuilt the chain:\n%s\n---\n%s", before, after)
	}
	members := netnsSetMembers(t, engine, st.setV4)
	for _, half := range []string{"0.0.0.0/1", "128.0.0.0/1", "0.0.0.0/0"} {
		if strings.Contains(members, half) {
			t.Errorf("%s is still in the kernel set after it was removed from the targets:\n%s", half, members)
		}
	}
	if !strings.Contains(members, netnsDevInet) {
		t.Errorf("the surviving static entry %s is gone:\n%s", netnsDevInet, members)
	}
	if got := dev.probe(t, "tcp", netnsDevInet, 443, "still"); got != "reply=tproxy:still" {
		t.Errorf("device -> %s after the removal: %s; the listed address must stay diverted", netnsDevInet, got)
	}
	if got := dev.probe(t, "tcp", netnsDevOther, 443, "gone"); got == "reply=tproxy:gone" {
		t.Errorf("device -> %s is still diverted after the catch-all was removed", netnsDevOther)
	}
}

func TestNetnsProxyCatchAllLeavesRouterLocalDestinationsAlone(t *testing.T) {
	netnsCatchAllLeavesRouterLocalDestinationsAlone(t, backendIPTables)
}

func TestNetnsProxyCatchAllLeavesRouterLocalDestinationsAloneNft(t *testing.T) {
	netnsCatchAllLeavesRouterLocalDestinationsAlone(t, backendNFTables)
}

func netnsUnscopedCatchAllAnswersALanClient(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	if engine == backendNFTables && !hasBinary("nft") {
		t.Skip("nft is not installed")
	}
	dev := netnsStartDevice(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsLocalGuardConfig(engine, "", []string{"0.0.0.0/0"})
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()
	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	st, ok := routeRuleCache["netns-localdst-proxy"]
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	port, _ := portFromState(st)

	for _, tc := range []struct {
		name string
		mark uint32
	}{
		{"marked listener", SelfDialMark},
		{"unmarked listener", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := netnsRouterSockets(t, port, tc.mark)
			got := dev.probe(t, "tcp", netnsDevInet, 443, "inet")
			t.Logf("device -> internet: %s", got)
			if got != "reply=tproxy:inet" {
				t.Errorf("device -> internet through an unscoped catch-all set: %s; the listener's reply was marked into the local delivery table by the out chain", got)
			}
			got = dev.probe(t, "tcp", netnsDevRouter, netnsDevWeb, "web")
			t.Logf("device -> router web: %s", got)
			if got != "reply=plain:web" {
				t.Errorf("device -> router web through an unscoped catch-all set: %s; the router's own reply to a set member was marked into the local delivery table", got)
			}
			if !s.got("tcp", "tproxy", "inet") || !s.got("tcp", "plain", "web") {
				t.Errorf("router sockets saw tcp=%v", s.tcp)
			}
		})
	}

	if engine != backendNFTables {
		return
	}

	accepted := make(chan string, 1)
	ln := netnsMarkedTransparentListener(t, port, SelfDialMark)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		accepted <- conn.LocalAddr().String()
		_ = conn.Close()
	}()
	conn, err := net.DialTimeout("tcp", netnsDevOther+":443", 3*time.Second)
	if err != nil {
		netnsLogState(t, engine)
		t.Fatalf("the router's own dial to a set address never completed: %v", err)
	}
	defer conn.Close()
	select {
	case local := <-accepted:
		if !strings.HasPrefix(local, netnsDevOther+":") {
			t.Errorf("the router's own dial was accepted as %s, not the original destination", local)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the router's own dial connected but the transparent listener never accepted it")
	}
}

func TestNetnsUnscopedCatchAllProxySetAnswersALanClient(t *testing.T) {
	netnsUnscopedCatchAllAnswersALanClient(t, backendIPTables)
}

func TestNetnsUnscopedCatchAllProxySetAnswersALanClientNft(t *testing.T) {
	netnsUnscopedCatchAllAnswersALanClient(t, backendNFTables)
}
