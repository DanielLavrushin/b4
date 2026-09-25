package tproxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func procHex(ip net.IP, port int) string {
	return procHexWidth(ip, port, ip.To4() == nil)
}

func procHexWidth(ip net.IP, port int, wide bool) string {
	raw := ip.To4()
	if wide || raw == nil {
		raw = ip.To16()
	}
	var b strings.Builder
	for i := 0; i < len(raw); i += 4 {
		fmt.Fprintf(&b, "%08X", binary.NativeEndian.Uint32(raw[i:i+4]))
	}
	fmt.Fprintf(&b, ":%04X", port)
	return b.String()
}

type fixtureSocket struct {
	local, remote string
	state         int
	inode         int
}

func procLine(i int, s fixtureSocket, wide bool) string {
	lh, lp, _ := net.SplitHostPort(s.local)
	rh, rp, _ := net.SplitHostPort(s.remote)
	var lport, rport int
	fmt.Sscan(lp, &lport)
	fmt.Sscan(rp, &rport)
	return fmt.Sprintf("%4d: %s %s %02X 00000000:00000000 00:00000000 00000000     0        0 %d 1 0000000000000000 100 0 0 10 0",
		i, procHexWidth(net.ParseIP(lh), lport, wide), procHexWidth(net.ParseIP(rh), rport, wide), s.state, s.inode)
}

func writeProcFixture(t *testing.T, root string, v4, v6 []fixtureSocket, pids map[int][]int, names map[int]string) {
	t.Helper()
	header := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode"
	write := func(name string, socks []fixtureSocket) {
		lines := []string{header}
		for i, s := range socks {
			lines = append(lines, procLine(i, s, name == "tcp6"))
		}
		if err := os.MkdirAll(filepath.Join(root, "net"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, "net", name), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tcp", v4)
	write("tcp6", v6)
	for pid, inodes := range pids {
		fd := filepath.Join(root, fmt.Sprint(pid), "fd")
		if err := os.MkdirAll(fd, 0o755); err != nil {
			t.Fatal(err)
		}
		for i, inode := range inodes {
			if err := os.Symlink(fmt.Sprintf("socket:[%d]", inode), filepath.Join(fd, fmt.Sprint(i+3))); err != nil {
				t.Fatal(err)
			}
		}
		if err := os.WriteFile(filepath.Join(root, fmt.Sprint(pid), "comm"), []byte(names[pid]+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func fixedAddrs(cidrs ...string) *localAddrs {
	return &localAddrs{list: func() ([]net.Addr, error) {
		out := make([]net.Addr, 0, len(cidrs))
		for _, c := range cidrs {
			ip, n, err := net.ParseCIDR(c)
			if err != nil {
				return nil, err
			}
			n.IP = ip
			out = append(out, n)
		}
		return out, nil
	}}
}

func tcpAddr(s string) *net.TCPAddr {
	a, err := net.ResolveTCPAddr("tcp", s)
	if err != nil {
		panic(err)
	}
	return a
}

const (
	listenState      = 0x0A
	establishedState = 0x01
)

func TestDecodeProcAddr(t *testing.T) {
	ip, port, ok := decodeProcAddr(procHex(net.ParseIP("94.189.76.227"), 58990))
	if !ok || !ip.Equal(net.ParseIP("94.189.76.227")) || port != 58990 {
		t.Fatalf("got %v:%d ok=%v", ip, port, ok)
	}
	ip, port, ok = decodeProcAddr(procHex(net.ParseIP("2001:db8::1"), 443))
	if !ok || !ip.Equal(net.ParseIP("2001:db8::1")) || port != 443 {
		t.Fatalf("got %v:%d ok=%v", ip, port, ok)
	}
	if _, _, ok := decodeProcAddr("zz:0050"); ok {
		t.Fatal("garbage decoded")
	}
}

func TestLoopGuardFindsTheUpstreamsOwnConnection(t *testing.T) {
	root := t.TempDir()
	writeProcFixture(t, root,
		[]fixtureSocket{
			{local: "0.0.0.0:55028", remote: "0.0.0.0:0", state: listenState, inode: 1111},
			{local: "94.189.76.227:40000", remote: "20.215.224.201:4455", state: establishedState, inode: 2222},
			{local: "94.189.76.227:40001", remote: "1.1.1.1:443", state: establishedState, inode: 3333},
		},
		nil,
		map[int][]int{100: {1111, 2222}, 200: {3333}},
		map[int]string{100: "xray", 200: "tor"},
	)
	g := &loopGuard{
		upstreamIPs:  []net.IP{net.ParseIP("192.168.1.1")},
		upstreamPort: 55028,
		procRoot:     root,
		locals:       fixedAddrs("192.168.1.1/24", "94.189.76.227/32"),
	}

	name, pid, ok := g.fromUpstreamProcess(tcpAddr("94.189.76.227:40000"), tcpAddr("20.215.224.201:4455"))
	if !ok || name != "xray" || pid != 100 {
		t.Fatalf("the upstream's own connection: got (%q, %d, %v)", name, pid, ok)
	}
	if _, _, ok := g.fromUpstreamProcess(tcpAddr("94.189.76.227:40001"), tcpAddr("1.1.1.1:443")); ok {
		t.Fatal("another local process's connection was taken for the upstream's own")
	}
	if _, _, ok := g.fromUpstreamProcess(tcpAddr("192.168.1.50:40000"), tcpAddr("20.215.224.201:4455")); ok {
		t.Fatal("a LAN client's connection was taken for the upstream's own")
	}
	if g.fromUpstreamHost(net.ParseIP("192.168.1.1")) {
		t.Fatal("an upstream on the router is matched by process, not by address")
	}
}

func TestLoopGuardFollowsARestartedUpstream(t *testing.T) {
	root := t.TempDir()
	writeProcFixture(t, root,
		[]fixtureSocket{
			{local: "0.0.0.0:1080", remote: "0.0.0.0:0", state: listenState, inode: 10},
			{local: "94.189.76.227:40000", remote: "20.215.224.201:4455", state: establishedState, inode: 20},
		},
		nil,
		map[int][]int{100: {10, 20}},
		map[int]string{100: "sing-box"},
	)
	g := &loopGuard{upstreamIPs: []net.IP{net.ParseIP("127.0.0.1")}, upstreamPort: 1080, procRoot: root, locals: fixedAddrs("94.189.76.227/32")}
	if _, pid, ok := g.fromUpstreamProcess(tcpAddr("94.189.76.227:40000"), tcpAddr("20.215.224.201:4455")); !ok || pid != 100 {
		t.Fatalf("first run: pid %d ok=%v", pid, ok)
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	writeProcFixture(t, root,
		[]fixtureSocket{
			{local: "127.0.0.1:1080", remote: "0.0.0.0:0", state: listenState, inode: 30},
			{local: "94.189.76.227:40002", remote: "20.215.224.201:4455", state: establishedState, inode: 40},
		},
		nil,
		map[int][]int{300: {30, 40}},
		map[int]string{300: "sing-box"},
	)
	if _, pid, ok := g.fromUpstreamProcess(tcpAddr("94.189.76.227:40002"), tcpAddr("20.215.224.201:4455")); !ok || pid != 300 {
		t.Fatalf("after the restart: pid %d ok=%v", pid, ok)
	}
}

func TestLoopGuardMatchesDualStackSockets(t *testing.T) {
	root := t.TempDir()
	writeProcFixture(t, root, nil,
		[]fixtureSocket{
			{local: "[::]:9150", remote: "[::]:0", state: listenState, inode: 7},
			{local: "[::ffff:94.189.76.227]:40000", remote: "[::ffff:185.220.101.1]:9001", state: establishedState, inode: 8},
		},
		map[int][]int{55: {7, 8}},
		map[int]string{55: "tor"},
	)
	g := &loopGuard{upstreamIPs: []net.IP{net.ParseIP("192.168.1.1")}, upstreamPort: 9150, procRoot: root, locals: fixedAddrs("192.168.1.1/24", "94.189.76.227/32")}
	if name, _, ok := g.fromUpstreamProcess(tcpAddr("94.189.76.227:40000"), tcpAddr("185.220.101.1:9001")); !ok || name != "tor" {
		t.Fatalf("got (%q, %v)", name, ok)
	}
}

func TestLoopGuardMatchesAnUpstreamOnAnotherHostByAddress(t *testing.T) {
	g := &loopGuard{upstreamIPs: []net.IP{net.ParseIP("192.168.1.50")}, upstreamPort: 1080, procRoot: t.TempDir(), locals: fixedAddrs("192.168.1.1/24")}
	if !g.fromUpstreamHost(net.ParseIP("192.168.1.50")) {
		t.Fatal("the upstream host's own traffic was not recognised")
	}
	if g.fromUpstreamHost(net.ParseIP("192.168.1.51")) {
		t.Fatal("another LAN host was taken for the upstream")
	}
	if _, _, ok := g.fromUpstreamProcess(tcpAddr("192.168.1.1:40000"), tcpAddr("1.1.1.1:443")); ok {
		t.Fatal("an upstream off the router cannot own a router socket")
	}
	var nilGuard *loopGuard
	if nilGuard.fromUpstreamHost(net.ParseIP("192.168.1.50")) {
		t.Fatal("a nil guard matched")
	}
}

func TestLoopGuardOnTheLiveProcTable(t *testing.T) {
	if _, err := os.Stat("/proc/net/tcp"); err != nil {
		t.Skip("no /proc/net/tcp")
	}
	upstream, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	dst, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer dst.Close()
	conn, err := net.Dial("tcp4", dst.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	accepted, err := dst.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer accepted.Close()

	g := newLoopGuard("127.0.0.1", upstream.Addr().(*net.TCPAddr).Port)
	_, pid, ok := g.fromUpstreamProcess(conn.LocalAddr().(*net.TCPAddr), dst.Addr().(*net.TCPAddr))
	if !ok || pid != os.Getpid() {
		t.Fatalf("this process owns both the upstream listener and the connection: got pid %d ok=%v", pid, ok)
	}
}

func TestLoopGuardRecognisesForkedWorkers(t *testing.T) {
	root := t.TempDir()
	writeProcFixture(t, root,
		[]fixtureSocket{
			{local: "0.0.0.0:1080", remote: "0.0.0.0:0", state: listenState, inode: 10},
			{local: "94.189.76.227:40000", remote: "20.215.224.201:443", state: establishedState, inode: 20},
		},
		nil,
		map[int][]int{1: {10}, 100: {10}, 101: {10, 20}},
		map[int]string{1: "init", 100: "danted", 101: "danted"},
	)
	g := &loopGuard{upstreamIPs: []net.IP{net.ParseIP("127.0.0.1")}, upstreamPort: 1080, procRoot: root, locals: fixedAddrs("94.189.76.227/32")}
	if _, pid, ok := g.fromUpstreamProcess(tcpAddr("94.189.76.227:40000"), tcpAddr("20.215.224.201:443")); !ok || pid != 101 {
		t.Fatalf("a worker holding the listener and the connection: pid %d ok=%v", pid, ok)
	}
	for _, pid := range g.owner.pids {
		if pid == 1 {
			t.Fatal("pid 1 holds every inherited socket and must never count as the upstream")
		}
	}
}

func TestLoopGuardRemembersThatNothingListens(t *testing.T) {
	root := t.TempDir()
	writeProcFixture(t, root,
		[]fixtureSocket{{local: "94.189.76.227:40000", remote: "20.215.224.201:443", state: establishedState, inode: 20}},
		nil, map[int][]int{100: {20}}, map[int]string{100: "xray"},
	)
	g := &loopGuard{upstreamIPs: []net.IP{net.ParseIP("127.0.0.1")}, upstreamPort: 1080, procRoot: root, locals: fixedAddrs("94.189.76.227/32")}
	client, dst := tcpAddr("94.189.76.227:40000"), tcpAddr("20.215.224.201:443")
	if _, _, ok := g.fromUpstreamProcess(client, dst); ok {
		t.Fatal("matched with nothing listening on the upstream port")
	}

	if err := os.RemoveAll(root); err != nil {
		t.Fatal(err)
	}
	writeProcFixture(t, root,
		[]fixtureSocket{
			{local: "0.0.0.0:1080", remote: "0.0.0.0:0", state: listenState, inode: 10},
			{local: "94.189.76.227:40000", remote: "20.215.224.201:443", state: establishedState, inode: 20},
		},
		nil, map[int][]int{100: {10, 20}}, map[int]string{100: "xray"},
	)
	if _, _, ok := g.fromUpstreamProcess(client, dst); ok {
		t.Fatal("the missing owner was looked up again inside the retry interval")
	}
	g.checkedAt = g.checkedAt.Add(-2 * ownerRetry)
	if _, _, ok := g.fromUpstreamProcess(client, dst); !ok {
		t.Fatal("the owner was not found once the retry interval passed")
	}
}

func TestLoopGuardResolvesHostnameAndUnspecifiedUpstreams(t *testing.T) {
	if got := newLoopGuard("0.0.0.0", 1080).addresses(); len(got) != 1 || !got[0].IsLoopback() {
		t.Fatalf("0.0.0.0 reaches the router itself: got %v", got)
	}
	if got := newLoopGuard("::", 1080).addresses(); len(got) != 1 || !got[0].Equal(net.IPv6loopback) {
		t.Fatalf(":: reaches the router itself: got %v", got)
	}

	g := newLoopGuard("proxy.lan", 1080)
	g.locals = fixedAddrs("192.168.1.1/24")
	g.lookup = func(ctx context.Context, host string) ([]net.IP, error) {
		if host != "proxy.lan" {
			return nil, fmt.Errorf("unexpected host %q", host)
		}
		return []net.IP{net.ParseIP("192.168.1.50")}, nil
	}
	deadline := time.Now().Add(2 * time.Second)
	for !g.fromUpstreamHost(net.ParseIP("192.168.1.50")) {
		if time.Now().After(deadline) {
			t.Fatal("a hostname upstream was never resolved")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for g.resolving.Load() {
		time.Sleep(time.Millisecond)
	}
}

func TestLocalAddrsPicksUpANewAddressOnAMiss(t *testing.T) {
	current := []string{"192.168.1.1/24"}
	calls := 0
	a := &localAddrs{list: func() ([]net.Addr, error) {
		calls++
		return fixedAddrs(current...).list()
	}}
	if !a.has(net.ParseIP("192.168.1.1")) || calls != 1 {
		t.Fatalf("first lookup: calls=%d", calls)
	}
	current = append(current, "94.189.76.228/32")
	if a.has(net.ParseIP("94.189.76.228")) {
		t.Fatal("a miss inside the retry interval refreshed anyway")
	}
	a.fetched = a.fetched.Add(-2 * localAddrsRetry)
	if !a.has(net.ParseIP("94.189.76.228")) {
		t.Fatal("a new WAN address was not picked up on a miss")
	}
}
