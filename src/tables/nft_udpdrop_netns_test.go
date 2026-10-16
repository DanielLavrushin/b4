package tables

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/mdlayher/netlink"
)

const (
	udpDropLink   = "b4ud0"
	udpDropPeer   = "b4ud0p"
	udpDropIP4    = "10.204.0.1"
	udpDropPeer4  = "10.204.0.2"
	udpDropIP6    = "2001:db8:204::1"
	udpDropPeer6  = "2001:db8:204::2"
	udpDropSport  = 41500
	udpDropTarget = 443
)

func netnsUDPDropLinks(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { _, _ = run("ip", "link", "del", udpDropLink) })
	netnsRun(t, "ip", "link", "set", "lo", "up")
	netnsRun(t, "ip", "link", "add", udpDropLink, "type", "veth", "peer", "name", udpDropPeer)
	for _, dev := range []string{udpDropLink, udpDropPeer} {
		_, _ = run("sysctl", "-w", "net.ipv6.conf."+dev+".disable_ipv6=0")
		netnsRun(t, "ip", "link", "set", dev, "up")
	}
	netnsRun(t, "ip", "addr", "add", udpDropIP4+"/24", "dev", udpDropLink)
	netnsRun(t, "ip", "-6", "addr", "add", udpDropIP6+"/64", "dev", udpDropLink, "nodad")
	mac := netnsLinkMAC(t, udpDropPeer)
	netnsRun(t, "ip", "neigh", "add", udpDropPeer4, "lladdr", mac, "dev", udpDropLink, "nud", "permanent")
	netnsRun(t, "ip", "-6", "neigh", "add", udpDropPeer6, "lladdr", mac, "dev", udpDropLink, "nud", "permanent")
}

func netnsSendUDPFrom(src, dst netip.AddrPort) error {
	network := "udp4"
	if src.Addr().Is6() {
		network = "udp6"
	}
	conn, err := net.DialUDP(network, net.UDPAddrFromAddrPort(src), net.UDPAddrFromAddrPort(dst))
	if err != nil {
		return err
	}
	defer conn.Close()
	_, err = conn.Write([]byte("b4-quic-drop-probe"))
	return err
}

func netnsDropRulePackets(t *testing.T, set string) int {
	t.Helper()
	out := netnsRun(t, "nft", "list", "chain", "inet", nftTableName, nftChainName)
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(line, "@"+set) {
			continue
		}
		f := strings.Fields(line)
		if i := slices.Index(f, "packets"); i >= 0 && i+1 < len(f) {
			if n, err := strconv.Atoi(f[i+1]); err == nil {
				return n
			}
		}
	}
	t.Fatalf("%s holds no counted drop rule for %s:\n%s", nftChainName, set, out)
	return 0
}

func netnsDropSetElement(listing, tuple string) (string, bool) {
	at := strings.Index(listing, tuple)
	if at < 0 {
		return "", false
	}
	rest := listing[at:]
	if end := strings.IndexAny(rest, ",}"); end >= 0 {
		rest = rest[:end]
	}
	return strings.TrimSpace(rest), true
}

type capturedDropWriters struct {
	v4, v6    func(src, dst netip.AddrPort) error
	published int
}

func netnsCaptureUDPDropWriters(t *testing.T) *capturedDropWriters {
	t.Helper()
	w := &capturedDropWriters{}
	udpDropMu.Lock()
	orig, origV4, origV6 := UDPDropWritersFunc, udpDropPublishedV4, udpDropPublishedV6
	UDPDropWritersFunc = func(v4, v6 func(src, dst netip.AddrPort) error) {
		w.v4, w.v6 = v4, v6
		w.published++
	}
	udpDropMu.Unlock()
	t.Cleanup(func() {
		udpDropMu.Lock()
		UDPDropWritersFunc, udpDropPublishedV4, udpDropPublishedV6 = orig, origV4, origV6
		udpDropMu.Unlock()
	})
	return w
}

func TestNetnsTheDropSetDropsTheWrittenFlowAndNoOther(t *testing.T) {
	netnsRequireNft(t)
	netnsUDPDropLinks(t)
	routeEngine = nil
	defer func() { routeEngine = nil }()
	writers := netnsCaptureUDPDropWriters(t)

	cfg := netnsConfig(backendNFTables)
	cfg.Sets = nil
	cfg.Queue.IPv6Enabled = true
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = ClearRules(cfg) })
	if writers.v4 == nil || writers.v6 == nil {
		t.Fatalf("after the apply the published writers are v4 %v, v6 %v; want both", writers.v4 != nil, writers.v6 != nil)
	}

	for _, fam := range []struct {
		name      string
		set       string
		write     func(src, dst netip.AddrPort) error
		src, peer string
	}{
		{"IPv4", nftUDPDropSetV4, writers.v4, udpDropIP4, udpDropPeer4},
		{"IPv6", nftUDPDropSetV6, writers.v6, udpDropIP6, udpDropPeer6},
	} {
		t.Run(fam.name, func(t *testing.T) {
			src := netip.AddrPortFrom(netip.MustParseAddr(fam.src), udpDropSport)
			dst := netip.AddrPortFrom(netip.MustParseAddr(fam.peer), udpDropTarget)
			if err := netnsSendUDPFrom(src, dst); err != nil {
				t.Fatalf("before the write the flow could not be sent, so this test proves nothing: %v", err)
			}
			before := netnsDropRulePackets(t, fam.set)

			for i := 0; i < 2; i++ {
				if err := fam.write(src, dst); err != nil {
					t.Fatalf("write %d of %s -> %s into %s: %v", i+1, src, dst, fam.set, err)
				}
			}
			if err := netnsSendUDPFrom(src, dst); !errors.Is(err, syscall.EPERM) {
				t.Errorf("a datagram of the written flow %s -> %s was sent (%v); the drop rule must stop it", src, dst, err)
			}
			for _, n := range []struct{ src, dst netip.AddrPort }{
				{netip.AddrPortFrom(src.Addr(), udpDropSport+1), dst},
				{src, netip.AddrPortFrom(dst.Addr(), udpDropTarget+1)},
			} {
				if err := netnsSendUDPFrom(n.src, n.dst); err != nil {
					t.Errorf("the neighbouring flow %s -> %s was refused: %v", n.src, n.dst, err)
				}
			}
			if got := netnsDropRulePackets(t, fam.set) - before; got != 1 {
				t.Errorf("the drop rule counted %d packets, want 1: the written flow's datagram and none of its neighbours", got)
			}

			listing := netnsRun(t, "nft", "list", "set", "inet", nftTableName, fam.set)
			tuple := fam.src + " . " + strconv.Itoa(udpDropSport) + " . " + fam.peer + " . " + strconv.Itoa(udpDropTarget)
			elem, ok := netnsDropSetElement(listing, tuple)
			switch {
			case !ok:
				t.Errorf("nft does not list the written flow as %q, so the key layout differs from the set's type:\n%s", tuple, listing)
			case !strings.Contains(elem, "expires"):
				t.Errorf("the written element %q has no expiry:\n%s", elem, listing)
			}
			if !strings.Contains(listing, "timeout 2m") || !strings.Contains(listing, "size 65536") {
				t.Errorf("the set lost its default timeout or its size:\n%s", listing)
			}
			if got := strings.Count(listing, "expires"); got != 1 {
				t.Errorf("the set holds %d elements, want only the written flow:\n%s", got, listing)
			}
		})
	}

	const flows = 100
	start := time.Now()
	for i := 0; i < flows; i++ {
		src := netip.AddrPortFrom(netip.MustParseAddr(udpDropIP4), uint16(50000+i))
		if err := writers.v4(src, netip.AddrPortFrom(netip.MustParseAddr(udpDropPeer4), udpDropTarget)); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	t.Logf("%d writes into %s took %v, %v each", flows, nftUDPDropSetV4, time.Since(start), time.Since(start)/flows)
	if got := strings.Count(netnsRun(t, "nft", "list", "set", "inet", nftTableName, nftUDPDropSetV4), "expires"); got != flows+1 {
		t.Errorf("the set holds %d elements after %d more writes, want %d", got, flows, flows+1)
	}

	stale := writers.v4
	if err := AddRules(cfg); err != nil {
		t.Fatalf("rebuilding the rules: %v", err)
	}
	if got := strings.Count(netnsRun(t, "nft", "list", "set", "inet", nftTableName, nftUDPDropSetV4), "expires"); got != 0 {
		t.Errorf("the rebuilt set still holds %d elements", got)
	}
	if writers.v4 == nil || writers.v6 == nil || writers.published < 3 {
		t.Errorf("the rebuild left the writers v4 %v, v6 %v after %d publications; want them withdrawn and published again", writers.v4 != nil, writers.v6 != nil, writers.published)
	}

	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if writers.v4 != nil || writers.v6 != nil {
		t.Errorf("ClearRules left the writers published")
	}
	if err := stale(netip.MustParseAddrPort(udpDropIP4+":41600"), netip.MustParseAddrPort(udpDropPeer4+":443")); err == nil {
		t.Errorf("a write into the set of a cleared table was reported as done, so the caller would not fall back to conntrack")
	}
}

func netnsDropSetConn() (*netlink.Conn, bool) {
	udpDropConn.mu.Lock()
	defer udpDropConn.mu.Unlock()
	return udpDropConn.conn, udpDropConn.open
}

func netnsDropSetRules(t *testing.T) (*capturedDropWriters, *config.Config) {
	t.Helper()
	netnsRequireNft(t)
	netnsUDPDropLinks(t)
	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })
	writers := netnsCaptureUDPDropWriters(t)
	cfg := netnsConfig(backendNFTables)
	cfg.Sets = nil
	cfg.Queue.IPv6Enabled = true
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = ClearRules(cfg) })
	return writers, cfg
}

func TestNetnsAFlushEmptiesTheDropSetsOverTheWritersConnection(t *testing.T) {
	writers, cfg := netnsDropSetRules(t)
	flows := []struct {
		set      string
		write    func(src, dst netip.AddrPort) error
		src, dst netip.AddrPort
	}{
		{nftUDPDropSetV4, writers.v4, netip.AddrPortFrom(netip.MustParseAddr(udpDropIP4), udpDropSport), netip.AddrPortFrom(netip.MustParseAddr(udpDropPeer4), udpDropTarget)},
		{nftUDPDropSetV6, writers.v6, netip.AddrPortFrom(netip.MustParseAddr(udpDropIP6), udpDropSport), netip.AddrPortFrom(netip.MustParseAddr(udpDropPeer6), udpDropTarget)},
	}

	var shared *netlink.Conn
	for i, f := range flows {
		if err := f.write(f.src, f.dst); err != nil {
			t.Fatalf("write %s -> %s into %s: %v", f.src, f.dst, f.set, err)
		}
		conn, _ := netnsDropSetConn()
		if conn == nil || (i > 0 && conn != shared) {
			t.Fatalf("write %d left connection %p after %p: the writes share one long-lived connection", i+1, conn, shared)
		}
		shared = conn
		if err := netnsSendUDPFrom(f.src, f.dst); !errors.Is(err, syscall.EPERM) {
			t.Fatalf("the written flow %s -> %s was sent (%v), so the flush below proves nothing", f.src, f.dst, err)
		}
	}

	if err := FlushUDPDropSets(); err != nil {
		t.Fatalf("FlushUDPDropSets: %v", err)
	}
	for _, f := range flows {
		if got := strings.Count(netnsRun(t, "nft", "list", "set", "inet", nftTableName, f.set), "expires"); got != 0 {
			t.Errorf("%s holds %d elements after the flush", f.set, got)
		}
		if err := netnsSendUDPFrom(f.src, f.dst); err != nil {
			t.Errorf("after the flush the flow %s -> %s must be let through to be decided again, got %v", f.src, f.dst, err)
		}
	}
	if conn, _ := netnsDropSetConn(); conn != shared {
		t.Errorf("the flush dialled connection %p instead of using the writers' %p", conn, shared)
	}

	if err := nftWriteUDPDropNetlink("b4_udp_drop_missing", make([]byte, 16)); err == nil {
		t.Fatal("a write into a set that does not exist must fail")
	}
	if conn, _ := netnsDropSetConn(); conn != nil {
		t.Fatal("a failed exchange must close the connection, so none of its late replies is read as the answer to the next request")
	}
	if err := flows[0].write(flows[0].src, flows[0].dst); err != nil {
		t.Fatalf("the write after a failed one must dial again: %v", err)
	}
	if conn, _ := netnsDropSetConn(); conn == nil || conn == shared {
		t.Fatalf("connection %p after the redial, want a new one", conn)
	}

	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if conn, open := netnsDropSetConn(); conn != nil || open {
		t.Errorf("ClearRules left the drop sets' connection %p open=%v", conn, open)
	}
	if err := FlushUDPDropSets(); err != nil {
		t.Errorf("with no drop set installed the flush has nothing to do, got %v", err)
	}
}

func TestNetnsADropSetWriteWaitingBehindAnotherTransactionIsNotAFailure(t *testing.T) {
	writers, _ := netnsDropSetRules(t)

	const elements = 100000
	var b strings.Builder
	b.WriteString("table inet b4_busy {\n set big { type ipv4_addr . inet_service . ipv4_addr . inet_service; flags interval; elements = {\n")
	for n := 0; n < elements; n++ {
		if n > 0 {
			b.WriteString(",\n")
		}
		fmt.Fprintf(&b, "10.%d.%d.%d . 1000-2000 . 172.16.%d.%d . 443", n/65536%256, n/256%256, n%256, n/256%256, n%256)
	}
	b.WriteString("\n } }\n}\n")
	big := filepath.Join(t.TempDir(), "busy.nft")
	if err := os.WriteFile(big, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = run("nft", "delete", "table", "inet", "b4_busy") })

	load := exec.Command("nft", "-f", big)
	if err := load.Start(); err != nil {
		t.Fatalf("nft -f: %v", err)
	}
	loaded := make(chan struct{})
	var loadErr error
	go func() {
		loadErr = load.Wait()
		close(loaded)
	}()
	t.Cleanup(func() {
		_ = load.Process.Kill()
		<-loaded
	})

	dst := netip.AddrPortFrom(netip.MustParseAddr(udpDropPeer4), udpDropTarget)
	var src netip.AddrPort
	var took time.Duration
	var err error
	for port := udpDropSport + 7; took <= nftUDPDropDeadline; port++ {
		select {
		case <-loaded:
			if loadErr != nil {
				t.Fatalf("loading the other table failed: %v", loadErr)
			}
			t.Skipf("no write waited past the %v reply deadline while the other table loaded, so the case was not reproduced", nftUDPDropDeadline)
		default:
		}
		src = netip.AddrPortFrom(netip.MustParseAddr(udpDropIP4), uint16(port))
		start := time.Now()
		err = writers.v4(src, dst)
		took = time.Since(start)
		time.Sleep(time.Millisecond)
	}
	<-loaded
	if loadErr != nil {
		t.Fatalf("loading the other table failed: %v", loadErr)
	}
	if err != nil {
		t.Fatalf("the write waited %v behind another nftables transaction and was reported as failed: %v", took, err)
	}
	tuple := udpDropIP4 + " . " + strconv.Itoa(int(src.Port())) + " . " + udpDropPeer4 + " . " + strconv.Itoa(udpDropTarget)
	if _, ok := netnsDropSetElement(netnsRun(t, "nft", "list", "set", "inet", nftTableName, nftUDPDropSetV4), tuple); !ok {
		t.Fatalf("the write was reported as done after %v, yet the set does not hold %q", took, tuple)
	}
	t.Logf("the write waited %v behind the other transaction and succeeded", took)
}
