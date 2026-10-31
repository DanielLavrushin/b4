package nfq

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

type gatedReader struct {
	gate <-chan struct{}
	r    io.Reader
}

func (g *gatedReader) Read(p []byte) (int, error) {
	<-g.gate
	return g.r.Read(p)
}

func countConntrackOpens(t *testing.T, holdFirst <-chan struct{}) *atomic.Int32 {
	t.Helper()
	prev := openConntrack
	var opens atomic.Int32
	openConntrack = func(path string) (io.ReadCloser, error) {
		n := opens.Add(1)
		f, err := prev(path)
		if err != nil || n != 1 || holdFirst == nil {
			return f, err
		}
		return struct {
			io.Reader
			io.Closer
		}{&gatedReader{gate: holdFirst, r: f}, f}, nil
	}
	t.Cleanup(func() { openConntrack = prev })
	return &opens
}

func procSocketLine(t *testing.T, local string, lport uint16, remote string, rport uint16, state string) string {
	t.Helper()
	l, ok := procHexAddr(net.ParseIP(local))
	if !ok {
		t.Fatalf("bad local address %s", local)
	}
	r, ok := procHexAddr(net.ParseIP(remote))
	if !ok {
		t.Fatalf("bad remote address %s", remote)
	}
	return fmt.Sprintf("   7: %s:%04X %s:%04X %s 00000000:00000000 00:00000000 00000000     0        0 31337 2 0000000000000000 0\n", l, lport, r, rport, state)
}

func writeProcTable(t *testing.T, name string, lines ...string) string {
	t.Helper()
	header := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(header+strings.Join(lines, "")), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestTunSrcMissesWaitingTogetherShareOneConntrackRead(t *testing.T) {
	thisHost(t)
	const waiting = 3
	var rows strings.Builder
	for i := 0; i <= waiting; i++ {
		rows.WriteString(snatFlow(fmt.Sprintf("192.168.1.%d", 100+i), tunTestWAN, 41000+i))
	}
	release := make(chan struct{})
	opens := countConntrackOpens(t, release)
	r := newResolverWith(t, tunTestWAN, rows.String())
	wan, dst := net.ParseIP(tunTestWAN), net.ParseIP("1.2.3.4")

	got := make([]string, waiting+1)
	var wg sync.WaitGroup
	attribute := func(i int) {
		defer wg.Done()
		if ip, origin := r.attribute(6, wan, uint16(41000+i), dst, 443); origin == tunSrcLAN {
			got[i] = ip.String()
		}
	}

	wg.Add(1)
	go attribute(0)
	if !waitFor(t, func() bool {
		r.scanMu.Lock()
		defer r.scanMu.Unlock()
		return r.scanActive != nil
	}) {
		t.Fatal("the first miss never started a conntrack read")
	}
	for i := 1; i <= waiting; i++ {
		wg.Add(1)
		go attribute(i)
	}
	if !waitFor(t, func() bool {
		r.scanMu.Lock()
		defer r.scanMu.Unlock()
		return r.scanQueued != nil && len(r.scanQueued.want) == waiting
	}) {
		close(release)
		wg.Wait()
		t.Fatal("misses that arrived during a read did not queue for the next one")
	}
	close(release)
	wg.Wait()

	for i, ip := range got {
		if want := fmt.Sprintf("192.168.1.%d", 100+i); ip != want {
			t.Errorf("miss %d attributed to %q, want %s", i, ip, want)
		}
	}
	if n := opens.Load(); n != 2 {
		t.Fatalf("%d conntrack reads for %d misses, want 2: the one in progress when the others arrived and one shared by all of them", n, waiting+1)
	}
}

func TestTunSrcNeverReadsConntrackForADNSAnswer(t *testing.T) {
	const routerLAN, client = "192.168.1.1", "192.168.1.20"
	thisHost(t, routerLAN)
	opens := countConntrackOpens(t, nil)
	matching := "ipv4 2 udp 17 29 src=192.168.1.77 dst=" + client + " sport=40000 dport=40001 src=" + client + " dst=" + routerLAN + " sport=40001 dport=53 use=1\n"
	r := newResolverWith(t, tunTestWAN, matching)

	for _, src := range []string{routerLAN, tunTestWAN} {
		ip, origin := r.attributePacket(tunSrcPacket{ttl: 64, answer: true}, 17, net.ParseIP(src), 53, net.ParseIP(client), 40001)
		if origin != tunSrcUnknown || ip != nil {
			t.Fatalf("answer from %s: attribute = %v, %v; an answer the router sends carries no SNAT mapping, so it stays unknown as before", src, ip, origin)
		}
	}
	if ip, origin := r.attributePacket(tunSrcPacket{answer: true}, 17, net.ParseIP("8.8.8.8"), 53, net.ParseIP(client), 40001); origin != tunSrcWire || ip != nil {
		t.Fatalf("answer from a public resolver: attribute = %v, %v; want the wire source", ip, origin)
	}
	if n := opens.Load(); n != 0 {
		t.Fatalf("conntrack read %d times for DNS answers", n)
	}

	if ip, origin := r.attributePacket(tunSrcPacket{}, 17, net.ParseIP(routerLAN), 53, net.ParseIP(client), 40001); origin != tunSrcLAN || ip.String() != "192.168.1.77" {
		t.Fatalf("the same tuple outside a DNS answer: attribute = %v, %v; the row must still be found by a read", ip, origin)
	}
	if n := opens.Load(); n != 1 {
		t.Fatalf("%d conntrack reads, want 1 for the packet that is not a DNS answer", n)
	}
}

func TestTunSrcTakesTheRoutersOwnUDPFlowFromItsSocketTable(t *testing.T) {
	thisHost(t)
	opens := countConntrackOpens(t, nil)
	r := newResolverWith(t, tunTestWAN, "")
	r.own = localSockets{
		udp: writeProcTable(t, "udp",
			procSocketLine(t, "0.0.0.0", 45002, "0.0.0.0", 0, "07"),
			procSocketLine(t, "0.0.0.0", 123, "0.0.0.0", 0, "07"),
		),
		ttl: 64,
	}
	wan, upstream := net.ParseIP(tunTestWAN), net.ParseIP("1.1.1.1")

	if ip, origin := r.attributePacket(tunSrcPacket{ttl: 64}, 17, wan, 45002, upstream, 53); origin != tunSrcRouter || ip != nil {
		t.Fatalf("dnsmasq-style query: attribute = %v, %v; want the router", ip, origin)
	}
	if n := opens.Load(); n != 0 {
		t.Fatalf("conntrack read %d times for a flow one of the router's sockets holds", n)
	}
	if !r.addressable(17, wan, 45002, upstream, 53) {
		t.Fatal("a router flow found in the socket table must stay addressable, or set DNS redirect stops answering dnsmasq")
	}

	if _, origin := r.attributePacket(tunSrcPacket{ttl: 63}, 17, wan, 45002, net.ParseIP("8.8.8.8"), 53); origin != tunSrcUnknown {
		t.Fatalf("a forwarded packet (TTL decremented) on the same port: origin %v, want it left to conntrack", origin)
	}
	if _, origin := r.attributePacket(tunSrcPacket{ttl: 64}, 17, wan, 123, upstream, 123); origin != tunSrcUnknown {
		t.Fatalf("an unconnected socket on a privileged port: origin %v, want it left to conntrack", origin)
	}
	if n := opens.Load(); n != 2 {
		t.Fatalf("%d conntrack reads, want 2 for the two packets the socket table does not settle", n)
	}
}

func TestTunSrcKeepsAnUnconnectedRouterUDPPortFromClaimingANonDNSLANFlow(t *testing.T) {
	thisHost(t)
	quic := "ipv4 2 udp 17 29 src=192.168.1.50 dst=8.8.8.8 sport=45002 dport=443 src=8.8.8.8 dst=" + tunTestWAN + " sport=443 dport=45002 use=1\n"
	r := newResolverWith(t, tunTestWAN, quic)
	r.own = localSockets{udp: writeProcTable(t, "udp", procSocketLine(t, "0.0.0.0", 45002, "0.0.0.0", 0, "07")), ttl: 64}
	wan := net.ParseIP(tunTestWAN)

	if ip, origin := r.attributePacket(tunSrcPacket{ttl: 64}, 17, wan, 45002, net.ParseIP("8.8.8.8"), 443); origin != tunSrcLAN || ip.String() != "192.168.1.50" {
		t.Fatalf("LAN QUIC flow sent with TTL 65 on a port an unconnected router socket holds: attribute = %v, %v; want the LAN client from conntrack", ip, origin)
	}
	if ip, origin := r.attributePacket(tunSrcPacket{ttl: 64}, 17, wan, 45002, net.ParseIP("1.1.1.1"), 53); origin != tunSrcRouter || ip != nil {
		t.Fatalf("DNS query from the same router port: attribute = %v, %v; dnsmasq's upstream queries still come from the socket table", ip, origin)
	}
}

func TestTunSrcTakesTheRoutersOwnTCPFlowOnlyFromItsExactConnection(t *testing.T) {
	thisHost(t)
	opens := countConntrackOpens(t, nil)
	r := newResolverWith(t, tunTestWAN, snatFlow("192.168.1.50", tunTestWAN, 40300))
	r.own = localSockets{
		tcp: writeProcTable(t, "tcp",
			procSocketLine(t, tunTestWAN, 40100, "1.2.3.4", 443, "01"),
			procSocketLine(t, "0.0.0.0", 40300, "0.0.0.0", 0, "0A"),
		),
		ttl: 64,
	}
	wan, dst := net.ParseIP(tunTestWAN), net.ParseIP("1.2.3.4")

	if ip, origin := r.attributePacket(tunSrcPacket{ttl: 64}, 6, wan, 40100, dst, 443); origin != tunSrcRouter || ip != nil {
		t.Fatalf("the router's own connection: attribute = %v, %v; want the router", ip, origin)
	}
	if n := opens.Load(); n != 0 {
		t.Fatalf("conntrack read %d times for the router's own connection", n)
	}
	if ip, origin := r.attributePacket(tunSrcPacket{ttl: 64}, 6, wan, 40300, dst, 443); origin != tunSrcLAN || ip.String() != "192.168.1.50" {
		t.Fatalf("a LAN flow on the port of a listening socket: attribute = %v, %v; a listener must not claim it", ip, origin)
	}
}

func TestProcHexAddrMatchesTheKernelsSocketTables(t *testing.T) {
	want := "0100007F"
	if binary.NativeEndian.Uint16([]byte{0, 1}) == 1 {
		want = "7F000001"
	}
	if got, ok := procHexAddr(net.ParseIP("127.0.0.1")); !ok || got != want {
		t.Fatalf("procHexAddr(127.0.0.1) = %q, want %q as /proc/net/udp prints it on this byte order", got, want)
	}
	if _, ok := procHexAddr(net.ParseIP("2001:db8::1")); ok {
		t.Fatal("an IPv6 address has no entry in the IPv4 socket tables")
	}
}

func TestSocketTableHoldsFindsRealSocketsInTheKernelsTables(t *testing.T) {
	for _, p := range []string{procNetUDP, procNetTCP} {
		if _, err := os.Stat(p); err != nil {
			t.Skipf("no %s here: %v", p, err)
		}
	}
	loopback, other := net.ParseIP("127.0.0.1"), net.ParseIP("192.0.2.9")

	bound, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	boundPort := uint16(bound.LocalAddr().(*net.UDPAddr).Port)
	if !socketTableHolds(procNetUDP, loopback, boundPort, other, 53, true) {
		t.Errorf("unconnected udp4 socket on 127.0.0.1:%d not found in %s", boundPort, procNetUDP)
	}

	wild, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer wild.Close()
	wildPort := uint16(wild.LocalAddr().(*net.UDPAddr).Port)
	if !socketTableHolds(procNetUDP, other, wildPort, loopback, 53, true) {
		t.Errorf("unconnected udp4 socket on 0.0.0.0:%d not found for an arbitrary source address", wildPort)
	}
	if socketTableHolds(procNetUDP, loopback, boundPort, other, 53, false) {
		t.Error("an unconnected socket matched where only a connected one may")
	}

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()
	conn, err := net.Dial("tcp4", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if c, ok := <-accepted; ok {
		defer c.Close()
	}
	local, remote := conn.LocalAddr().(*net.TCPAddr), conn.RemoteAddr().(*net.TCPAddr)
	if !socketTableHolds(procNetTCP, local.IP, uint16(local.Port), remote.IP, uint16(remote.Port), false) {
		t.Errorf("connected tcp4 socket %v -> %v not found in %s", local, remote, procNetTCP)
	}
	listenPort := uint16(ln.Addr().(*net.TCPAddr).Port)
	if socketTableHolds(procNetTCP, loopback, listenPort, other, 9, false) {
		t.Error("the listening socket's port matched a peer it has no connection to")
	}
}

func TestReadHostTTLFallsBackToTheLinuxDefault(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	for path, want := range map[string]uint8{
		write("set", "65\n"):           65,
		write("garbage", "ttl\n"):      fallbackHostTTL,
		write("out-of-range", "300\n"): fallbackHostTTL,
		filepath.Join(dir, "missing"):  fallbackHostTTL,
	} {
		if got := readHostTTL(path); got != want {
			t.Errorf("readHostTTL(%s) = %d, want %d", filepath.Base(path), got, want)
		}
	}
}

func TestTunWorkerReadsNoConntrackForADnsmasqAnswer(t *testing.T) {
	const routerLAN = "192.168.1.1"
	thisHost(t, routerLAN)
	opens := countConntrackOpens(t, nil)
	cfg := config.NewConfig()
	cfg.Queue.Mode = "tun"
	w := newTestWorker(t, &cfg)
	w.srcResolver = newResolverWith(t, tunTestWAN, "")

	answer := dns.BuildQuery("example.com", 0x4242, dnsTypeA)
	answer[2] |= 0x80
	if pkt, ok := w.parseIPHeaders(makeV4UDPPacket(answer, net.ParseIP(routerLAN), net.ParseIP("192.168.1.20"), 53, 40001)); !ok || pkt.srcStr != routerLAN || !pkt.srcUnattributed {
		t.Fatalf("dnsmasq answer parsed as %+v, ok=%v; want the router source kept and marked unattributed as before", pkt, ok)
	}
	if n := opens.Load(); n != 0 {
		t.Fatalf("conntrack read %d times for a dnsmasq answer", n)
	}

	query := dns.BuildQuery("example.com", 0x4243, dnsTypeA)
	if _, ok := w.parseIPHeaders(makeV4UDPPacket(query, net.ParseIP(tunTestWAN), net.ParseIP("1.1.1.1"), 40002, 53)); !ok {
		t.Fatal("query from the uplink address did not parse")
	}
	if n := opens.Load(); n != 1 {
		t.Fatalf("%d conntrack reads, want 1 for a query from the uplink address", n)
	}
}
