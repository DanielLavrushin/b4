package nfq

import (
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netif"
	"github.com/florianl/go-nfqueue"
)

const egressRouteHeader = "Iface\tDestination\tGateway \tFlags\tRefCnt\tUse\tMetric\tMask\t\tMTU\tWindow\tIRTT\n"

func egressRoutes(lines ...string) string {
	return egressRouteHeader + strings.Join(lines, "\n") + "\n"
}

func fakeEgressIfaces(t *testing.T, routes string) map[string]uint32 {
	t.Helper()
	up, down := "0x1003\n", "0x1002\n"
	ifaces := map[string]map[string]string{
		"wgt0":   {"uevent": "DEVTYPE=wireguard\nINTERFACE=wgt0\n", "flags": up},
		"tunt0":  {"tun_flags": "0x1001\n", "flags": up},
		"wgdn0":  {"uevent": "DEVTYPE=wireguard\nINTERFACE=wgdn0\n", "flags": down},
		"wgdef0": {"uevent": "DEVTYPE=wireguard\nINTERFACE=wgdef0\n", "flags": up},
		"wgbak0": {"uevent": "DEVTYPE=wireguard\nINTERFACE=wgbak0\n", "flags": up},
		"awgt0":  {"uevent": "DEVTYPE=amneziawg\nINTERFACE=awgt0\n", "flags": up},
		"eth7":   {"uevent": "INTERFACE=eth7\n", "flags": up},
	}
	root := t.TempDir() + string(os.PathSeparator)
	for name, files := range ifaces {
		dir := filepath.Join(root, name)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		for f, body := range files {
			if err := os.WriteFile(filepath.Join(dir, f), []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	routeFile := filepath.Join(t.TempDir(), "route")
	if err := os.WriteFile(routeFile, []byte(routes), 0o644); err != nil {
		t.Fatal(err)
	}

	prevRoot, prevRoutes := netif.Root, netif.RouteFile
	netif.Root, netif.RouteFile = root, routeFile
	netif.Forget()

	idx := make(map[string]uint32, len(ifaces))
	next := uint32(64000)
	for _, name := range []string{"wgt0", "tunt0", "wgdn0", "wgdef0", "wgbak0", "awgt0", "eth7"} {
		ifaceCache.Store(next, name)
		idx[name] = next
		next++
	}
	t.Cleanup(func() {
		netif.Root, netif.RouteFile = prevRoot, prevRoutes
		netif.Forget()
		for _, i := range idx {
			ifaceCache.Delete(i)
		}
		ResetIfaceTraffic()
	})
	return idx
}

func queuedVerdict(t *testing.T, w *Worker, raw []byte, mark *uint32, out, in uint32) int {
	t.Helper()
	id := uint32(1)
	a := nfqueue.Attribute{PacketID: &id, Payload: &raw, Mark: mark}
	if out != 0 {
		a.OutDev = &out
	}
	if in != 0 {
		a.InDev = &in
	}
	q := newFakeQueue()
	w.handleQueued(&verdictCtx{id: id, q: q}, a, uint(w.getConfig().Queue.Mark))
	return q.now(t)
}

func ytimgHello() []byte {
	return makeV4TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000)
}

const egressUplink = "eth7\t00000000\t0101A8C0\t0003\t0\t0\t0\t00000000\t0\t0\t0"

func TestAClientHelloLeavingThroughATunnelIsHandedOff(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(
		"eth7\t00000000\t0101A8C0\t0003\t0\t0\t20\t00000000\t0\t0\t0",
		"wgt0\t0000000A\t00000000\t0001\t0\t0\t0\t000000FF\t0\t0\t0",
		"wgdef0\t00000000\t00000000\t0001\t0\t0\t10\t00000000\t0\t0\t0",
		"wgbak0\t00000000\t00000000\t0001\t0\t0\t50\t00000000\t0\t0\t0",
	))

	for _, c := range []struct {
		name    string
		out, in uint32
		want    int
		why     string
	}{
		{"wireguard", idx["wgt0"], 0, nfqueue.NfAccept, "a ClientHello leaving through a WireGuard tunnel never meets the DPI as b4 would write it"},
		{"userspace tunnel", idx["tunt0"], 0, nfqueue.NfAccept, "a ClientHello leaving through a TUN device never meets the DPI as b4 would write it"},
		{"amneziawg", idx["awgt0"], 0, nfqueue.NfAccept, "a ClientHello leaving through an AmneziaWG tunnel never meets the DPI as b4 would write it"},
		{"wireguard that is down", idx["wgdn0"], 0, nfqueue.NfDrop, "a tunnel that is down carries nothing, so it must not change how b4 treats the packet"},
		{"tunnel carrying the default route", idx["wgdef0"], 0, nfqueue.NfDrop, "b4's own packets follow the main default route, so a tunnel that carries it is the router's uplink and keeps the strategy"},
		{"tunnel with a backup default route", idx["wgbak0"], 0, nfqueue.NfAccept, "a default route with a higher metric carries none of b4's own packets while the first one is there"},
		{"plain uplink", idx["eth7"], 0, nfqueue.NfDrop, "a ClientHello leaving through the uplink must get the set's strategy"},
		{"arriving from a tunnel", 0, idx["wgt0"], nfqueue.NfDrop, "only the interface a packet leaves through decides the hand-off"},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newFragmentingYTImagesSet()
			w := newInjectingWorker(t, &set)
			if got := queuedVerdict(t, w, ytimgHello(), nil, c.out, c.in); got != c.want {
				t.Fatalf("verdict %d, want %d: %s", got, c.want, c.why)
			}
		})
	}
}

func TestATunnelHandOffIsLoggedWithItsInterface(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	set := newFragmentingYTImagesSet()
	w := newInjectingWorker(t, &set)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	if got := queuedVerdict(t, w, ytimgHello(), nil, idx["wgt0"], 0); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d, want the ClientHello accepted unchanged", got)
	}
	f := nextConnectionLine(t, ch)
	if flags := f[len(f)-1]; flags != "tunnel->wgt0" {
		t.Fatalf("connection line flags %q, want %q so the Traffic page shows why the set's strategy did not run", flags, "tunnel->wgt0")
	}
	if got := loggedSetFromLine(f); got != set.Name {
		t.Fatalf("connection line set %q, want %q: the set still matches, only its strategy is skipped", got, set.Name)
	}
}

func loggedSetFromLine(f []string) string {
	if len(f) < 7 {
		return ""
	}
	if f[5] != "" {
		return f[5]
	}
	return f[2]
}

func TestABlockSetStillBlocksAConnectionLeavingThroughATunnel(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	set := newFragmentingYTImagesSet()
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeBlock
	set.Routing.BlockAction = config.BlockActionDrop
	w := newInjectingWorker(t, &set)

	if got := queuedVerdict(t, w, ytimgHello(), nil, idx["wgt0"], 0); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: a block set blocks by destination, whatever interface the connection leaves through", got)
	}
}

func TestAQUICInitialLeavingThroughATunnelIsHandedOff(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))

	for _, c := range []struct {
		name string
		out  uint32
		want int
	}{
		{"wireguard", idx["wgt0"], nfqueue.NfAccept},
		{"plain uplink", idx["eth7"], nfqueue.NfDrop},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newDomainUDPSet(config.QUICFilterSNI)
			cfg := newUDPGateConfig(t, &set)
			w := newTestWorker(t, &cfg)
			initial := buildQUICInitialWithSNI(t, []byte{6, 6, 6, 6, 9, 9, 9, 9}, "rr1.googlevideo.com")
			pkt := makeV4UDPPacket(initial, net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 443)
			if got := queuedVerdict(t, w, pkt, nil, c.out, 0); got != c.want {
				t.Fatalf("verdict %d, want %d: udp.mode=drop applies to QUIC leaving through the uplink and never to QUIC leaving through a tunnel", got, c.want)
			}
		})
	}
}

func TestForeignMarksSharingTheNoDPIBitStillGetTheSetsStrategy(t *testing.T) {
	for _, c := range []struct {
		name string
		mark uint32
		want int
	}{
		{"Keenetic hotspot mark", 0x0ffffaaa, nfqueue.NfDrop},
		{"MagiTrickle mark", 0x4d616769, nfqueue.NfDrop},
		{"the no-DPI bit alone", config.SelfDialNoDPIBit, nfqueue.NfDrop},
		{"b4's own no-DPI dial", config.SelfDialRelayMark, nfqueue.NfAccept},
		{"b4's own no-DPI dial with routing bits", config.SelfDialRelayMark | 0x3f00, nfqueue.NfAccept},
		{"b4's own no-DPI dial with an engine bit", config.SelfDialRelayMark | 0x40000000, nfqueue.NfAccept},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newFragmentingYTImagesSet()
			w := newInjectingWorker(t, &set)
			mark := c.mark
			if got := queuedVerdict(t, w, ytimgHello(), &mark, 0, 0); got != c.want {
				t.Fatalf("mark 0x%x: verdict %d, want %d", c.mark, got, c.want)
			}
		})
	}
}

func escalationBackupSet() config.SetConfig {
	b := newFragmentingYTImagesSet()
	b.Id = "backup"
	b.Name = "Backup"
	return b
}

func routedIntoWgt0(set config.SetConfig) config.SetConfig {
	set.Targets.DomainOnly = false
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeInterface
	set.Routing.EgressInterface = "wgt0"
	return set
}

func TestASYNLeavingThroughATunnelGetsNoFakeSYN(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	for _, c := range []struct {
		name string
		out  uint32
		want int
	}{
		{"wireguard", idx["wgt0"], nfqueue.NfAccept},
		{"plain uplink", idx["eth7"], nfqueue.NfDrop},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newStaticAddressSet(false)
			set.TCP.SynFake = true
			w := newInjectingWorker(t, &set)
			if got := queuedVerdict(t, w, makeV4TCPPacketFlags(nil, 1000, 0x02), nil, c.out, 0); got != c.want {
				t.Fatalf("verdict %d, want %d: SYN Fake Packets apply to a SYN leaving through the uplink and never to one leaving through a tunnel", got, c.want)
			}
		})
	}
}

func TestAStallThroughAnotherServicesTunnelDoesNotEscalate(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	set := newFragmentingYTImagesSet()
	set.Escalate.To = "backup"
	backup := escalationBackupSet()
	w := newInjectingWorker(t, &set, &backup)

	for i := 0; i < 3; i++ {
		if got := queuedVerdict(t, w, ytimgHello(), nil, idx["wgt0"], 0); got != nfqueue.NfAccept {
			t.Fatalf("ClientHello %d through the tunnel: verdict %d, want it accepted unchanged", i+1, got)
		}
	}
	if _, _, escalated := w.destState.GetEscalation("i.ytimg.com"); escalated {
		t.Fatal("a ClientHello resent through another service's tunnel escalated the host for every device")
	}
}

func TestASetRoutedIntoATunnelStillEscalatesOnStall(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	for _, c := range []struct {
		name string
		out  uint32
	}{
		{"no egress interface", 0},
		{"leaving through its tunnel", idx["wgt0"]},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := routedIntoWgt0(newFragmentingYTImagesSet())
			set.Escalate.To = "backup"
			backup := escalationBackupSet()
			w := newInjectingWorker(t, &set, &backup)
			if !set.RoutingHandsOffPackets() {
				t.Fatal("the set must hand its packets to wgt0")
			}
			for i := 0; i < 3; i++ {
				queuedVerdict(t, w, ytimgHello(), nil, c.out, 0)
			}
			if _, _, escalated := w.destState.GetEscalation("i.ytimg.com"); !escalated {
				t.Fatal("a set b4 routes into a tunnel stalled three times and did not escalate to its backup")
			}
		})
	}
}

func TestASetRoutedIntoAmneziaWGKeepsItsStrategy(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	set := newFragmentingYTImagesSet()
	set.Targets.DomainOnly = false
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeInterface
	set.Routing.EgressInterface = "awgt0"
	w := newInjectingWorker(t, &set)
	if set.RoutingHandsOffPackets() {
		t.Fatal("a set routed into AmneziaWG must not count as handing its packets to a tunnel")
	}
	if got := queuedVerdict(t, w, ytimgHello(), nil, idx["awgt0"], 0); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: a set b4 itself routes into AmneziaWG keeps its strategy, as before", got)
	}
}

func TestASetRoutedIntoATunnelKeepsItsUDPDrop(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	set := routedIntoWgt0(newDomainUDPSet(config.QUICFilterSNI))
	cfg := newUDPGateConfig(t, &set)
	w := newTestWorker(t, &cfg)
	if !set.RoutingHandsOffPackets() {
		t.Fatal("the set must hand its packets to wgt0")
	}
	initial := buildQUICInitialWithSNI(t, []byte{6, 6, 6, 6, 9, 9, 9, 9}, "rr1.googlevideo.com")
	pkt := makeV4UDPPacket(initial, net.ParseIP("10.0.0.1"), net.ParseIP("1.2.3.4"), 51000, 443)
	if got := queuedVerdict(t, w, pkt, nil, idx["wgt0"], 0); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: udp.mode=drop of a set b4 routes into a tunnel must still drop its QUIC", got)
	}
}

func serverReply(flags byte) []byte {
	pkt := makeV4TCPPacketFlags(nil, 5000, flags)
	copy(pkt[12:16], []byte{1, 2, 3, 4})
	copy(pkt[16:20], []byte{10, 0, 0, 1})
	binary.BigEndian.PutUint16(pkt[20:22], 443)
	binary.BigEndian.PutUint16(pkt[22:24], 12345)
	return pkt
}

func TestRepliesOfAConnectionThroughAnotherServicesTunnelAreLeftAlone(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	set := newFragmentingYTImagesSet()
	set.TCP.RSTProtection.Enabled = true
	set.Escalate.To = "backup"
	backup := escalationBackupSet()
	w := newInjectingWorker(t, &set, &backup)

	if got := queuedVerdict(t, w, ytimgHello(), nil, idx["wgt0"], 0); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: the ClientHello through the tunnel must be accepted unchanged", got)
	}
	for i := 0; i < 3; i++ {
		if got := queuedVerdict(t, w, serverReply(0x14), nil, 0, idx["wgt0"]); got != nfqueue.NfAccept {
			t.Fatalf("server RST %d: verdict %d, a reset of a connection that left through another service's tunnel must pass", i+1, got)
		}
	}
	if _, _, escalated := w.destState.GetEscalation("i.ytimg.com"); escalated {
		t.Fatal("server resets of a connection that left through another service's tunnel escalated the host for every device")
	}
}

func TestABlockSetThroughATunnelIsNotLoggedAsHandedOff(t *testing.T) {
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	set := newFragmentingYTImagesSet()
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeBlock
	set.Routing.BlockAction = config.BlockActionDrop
	w := newInjectingWorker(t, &set)

	ch, _ := log.GetConnectionHub().Subscribe()
	defer log.GetConnectionHub().Unsubscribe(ch)

	queuedVerdict(t, w, ytimgHello(), nil, idx["wgt0"], 0)
	for {
		select {
		case line := <-ch:
			if strings.Contains(line, "tunnel->") {
				t.Fatalf("connection line %q: a blocked connection is not handed off to the tunnel", line)
			}
		default:
			return
		}
	}
}
