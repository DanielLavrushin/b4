package nfq

import (
	"encoding/binary"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/florianl/go-nfqueue"
)

func captureTelegramInjected(t *testing.T, w *Worker) (ordinary, telegram func() [][]byte) {
	t.Helper()
	telegram = captureInjected(t, w)
	viaTelegram := w.sock
	ordinary = captureInjected(t, w)
	w.divertTelegramFlows(viaTelegram)
	return ordinary, telegram
}

func laterFragment(pkt []byte) bool {
	switch pkt[0] >> 4 {
	case IPv4:
		return binary.BigEndian.Uint16(pkt[6:8])&0x1fff != 0
	case IPv6:
		return len(pkt) >= IPv6HeaderLen+8 && pkt[6] == 44 && binary.BigEndian.Uint16(pkt[IPv6HeaderLen+2:IPv6HeaderLen+4])&^7 != 0
	}
	return false
}

func ytimgHelloV6() []byte {
	return makeV6TCPPacket(buildClientHello("i.ytimg.com", 16, 0xAB), 1000)
}

func TestThePacketsB4SendsIntoItsOwnConnectionsCarryTheirDialMark(t *testing.T) {
	for _, c := range []struct {
		name     string
		tune     func(*config.SetConfig)
		hello    func() []byte
		sent     int
		fragment bool
	}{
		{"split segments", func(*config.SetConfig) {}, ytimgHello, 2, false},
		{"split segments sent after a delay", func(s *config.SetConfig) { s.TCP.Seg2Delay = 40 }, ytimgHello, 2, false},
		{"fakes ahead of the segments", func(s *config.SetConfig) { s.Faking.SNI, s.Faking.SNISeqLength = true, 2 }, ytimgHello, 4, false},
		{"IP fragments", func(s *config.SetConfig) { s.Fragmentation.Strategy = "ip" }, ytimgHello, 2, true},
		{"IP fragments in reverse order", func(s *config.SetConfig) {
			s.Fragmentation.Strategy, s.Fragmentation.ReverseOrder = "ip", true
		}, ytimgHello, 2, true},
		{"IPv6 fragments", func(s *config.SetConfig) { s.Fragmentation.Strategy = "ip" }, ytimgHelloV6, 2, true},
		{"IPv6 fragments in reverse order", func(s *config.SetConfig) {
			s.Fragmentation.Strategy, s.Fragmentation.ReverseOrder = "ip", true
		}, ytimgHelloV6, 2, true},
	} {
		for _, flow := range []struct {
			name     string
			mark     *uint32
			owner    *uint32
			telegram bool
		}{
			{"b4's own Telegram connection", markOf(config.TelegramSetDialMark), nil, true},
			{"another connection b4 opens itself", markOf(config.SelfDialMark), routerSocket(), true},
			{"a forwarded connection whose mark has bit 0x40000", markOf(config.SelfDialMark), nil, false},
			{"a device's connection", nil, nil, false},
		} {
			t.Run(c.name+"/"+flow.name, func(t *testing.T) {
				other := newFragmentingYTImagesSet()
				telegram := newTelegramSet()
				c.tune(&other)
				c.tune(&telegram)
				w := newInjectingWorker(t, &other, &telegram)
				w.getConfig().System.MTProto.UpstreamSet = telegram.Id
				w.getConfig().Queue.IPv6Enabled = true
				ordinary, viaTelegram := captureTelegramInjected(t, w)
				idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
				stubInjectRoute(t, idx["eth7"], true)

				if got := queuedVerdictFrom(t, w, c.hello(), flow.mark, flow.owner, idx["eth7"], 0); got != nfqueue.NfDrop {
					t.Fatalf("verdict %d, want the drop of a packet b4 sends on itself", got)
				}
				plain, marked := ordinary(), viaTelegram()
				want, wrong := plain, marked
				if flow.telegram {
					want, wrong = marked, plain
				}
				if len(want) != c.sent || len(wrong) != 0 {
					t.Fatalf("%d packets left by the socket with the dial mark and %d by the ordinary injector, want %d by the %s one: %s", len(marked), len(plain), c.sent, map[bool]string{true: "dial mark", false: "ordinary"}[flow.telegram],
						map[bool]string{true: "the routing sets skip the connection's packets on bit 0x40000, so a routing set that lists the address would take these while the connection leaves by the uplink", false: "a forwarded connection already took its route before it reached the queue, whatever its mark"}[flow.telegram])
				}
				if c.fragment {
					later := 0
					for _, p := range want {
						if laterFragment(p) {
							later++
						}
					}
					if later != 1 {
						t.Fatalf("%d of the fragments carry an offset, want exactly one: a fragment without ports must follow the connection's first fragment", later)
					}
				}
			})
		}
	}
}

func markOf(m uint32) *uint32 {
	return &m
}

func routerSocket() *uint32 {
	return markOf(0)
}

func queuedVerdictFrom(t *testing.T, w *Worker, raw []byte, mark, owner *uint32, out, in uint32) int {
	t.Helper()
	id := uint32(1)
	a := nfqueue.Attribute{PacketID: &id, Payload: &raw, Mark: mark, UID: owner}
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

func TestAnInterfaceSetThatLearnedTheTelegramAddressLeavesItsPacketsToTheTelegramSet(t *testing.T) {
	vpn := newPassiveSet()
	vpn.Id, vpn.Name = "vpn", "VPN"
	vpn.Targets.DomainOnly = false
	vpn.Targets.DomainsToMatch = []string{"example.org"}
	vpn.Routing.Enabled = true
	vpn.Routing.Mode = config.RoutingModeInterface
	vpn.Routing.EgressInterface = "wgt0"
	telegram := newTelegramSet()
	w := newInjectingWorker(t, &vpn, &telegram)
	w.getConfig().System.MTProto.UpstreamSet = telegram.Id
	w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", &vpn)
	ordinary, viaTelegram := captureTelegramInjected(t, w)
	idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
	stubInjectRoute(t, idx["eth7"], true)

	if got := queuedVerdict(t, w, ytimgHello(), markOf(config.TelegramSetDialMark), idx["eth7"], 0); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: the Telegram set's strategy applies to its connections whatever another set learned about the address", got)
	}
	if plain, marked := ordinary(), viaTelegram(); len(plain) != 0 || len(marked) != 2 {
		t.Fatalf("%d split segments left by the socket with the Telegram set's dial mark and %d by the ordinary injector, want both by the first: with the injector mark alone the set's routing chain sends them through wgt0 while the connection leaves by the uplink", len(marked), len(plain))
	}
}

func TestThePacketsB4SendsIntoARelayConnectionLeaveWithItWhateverInterfaceSetHoldsTheAddress(t *testing.T) {
	for _, c := range []struct {
		name    string
		iface   string
		learned bool
	}{
		{"the set routed through a second uplink applies its strategy", "eth8", false},
		{"a strategy set applies while the set routed through a second uplink learned the address", "eth8", true},
		{"a strategy set applies while the set routed through a WireGuard tunnel learned the address", "wgt0", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			routed := newFragmentingYTImagesSet()
			sets := []*config.SetConfig{&routed}
			if c.learned {
				strategy := newFragmentingYTImagesSet()
				routed = newPassiveSet()
				routed.Targets.DomainsToMatch = []string{"example.org"}
				sets = []*config.SetConfig{&strategy, &routed}
			}
			routed.Id, routed.Name = "routed", "Routed"
			routed.Targets.DomainOnly = false
			routed.Routing.Enabled = true
			routed.Routing.Mode = config.RoutingModeInterface
			routed.Routing.EgressInterface = c.iface
			w := newInjectingWorker(t, sets...)
			if c.learned {
				w.getMatcher().LearnIPToDomain(net.ParseIP("1.2.3.4"), "i.ytimg.com", &routed)
			}
			ordinary, viaDialMark := captureTelegramInjected(t, w)
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stubInjectRoute(t, idx["eth7"], true)

			if got := queuedVerdictFrom(t, w, ytimgHello(), markOf(config.SelfDialMark), routerSocket(), idx["eth7"], 0); got != nfqueue.NfDrop {
				t.Fatalf("verdict %d, want the drop of a ClientHello b4 splits itself", got)
			}
			if plain, marked := ordinary(), viaDialMark(); len(plain) != 0 || len(marked) != 2 {
				t.Fatalf("%d split segments left by the socket with the dial mark and %d by the ordinary injector, want both by the first: the relay connection carries 0x40000 and leaves by the uplink, while with the injector mark alone the routing chain sends its segments through %s", len(marked), len(plain), c.iface)
			}
		})
	}
}

func telegramServerReply(payload []byte) []byte {
	pkt := makeV4TCPPacket(payload, 5000)
	copy(pkt[12:16], []byte{1, 2, 3, 4})
	copy(pkt[16:20], []byte{10, 0, 0, 1})
	binary.BigEndian.PutUint16(pkt[20:22], 443)
	binary.BigEndian.PutUint16(pkt[22:24], 12345)
	binary.BigEndian.PutUint32(pkt[28:32], 2000)
	return pkt
}

func TestWhatAReplyMakesB4SendIntoItsOwnConnectionCarriesTheDialMark(t *testing.T) {
	for _, c := range []struct {
		name     string
		mode     string
		mark     *uint32
		owner    *uint32
		telegram bool
	}{
		{"fakes toward the server of b4's own Telegram connection", "fake", markOf(config.TelegramSetDialMark), nil, true},
		{"resets toward the server of b4's own Telegram connection", "reset", markOf(config.TelegramSetDialMark), nil, true},
		{"fakes toward the server of another connection b4 opens itself", "fake", markOf(config.SelfDialMark), routerSocket(), true},
		{"resets toward the server of another connection b4 opens itself", "reset", markOf(config.SelfDialMark), routerSocket(), true},
		{"fakes toward the server of a forwarded connection whose mark has bit 0x40000", "fake", markOf(config.SelfDialMark), nil, false},
		{"fakes toward the server of a device's connection in the Telegram set", "fake", nil, nil, false},
	} {
		t.Run(c.name, func(t *testing.T) {
			telegram := newTelegramSet()
			telegram.Targets.DomainsToMatch = []string{"i.ytimg.com"}
			telegram.TCP.Incoming.Mode = c.mode
			telegram.TCP.Incoming.FakeCount = 1
			telegram.TCP.Incoming.Min = 1
			telegram.TCP.Incoming.Max = 1
			w := newInjectingWorker(t, &telegram)
			w.getConfig().System.MTProto.UpstreamSet = telegram.Id
			ordinary, viaTelegram := captureTelegramInjected(t, w)
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stubInjectRoute(t, idx["eth7"], true)

			queuedVerdictFrom(t, w, ytimgHello(), c.mark, c.owner, idx["eth7"], 0)
			_, _ = ordinary(), viaTelegram()
			if bound := w.connTracker.GetSetForIncoming("10.0.0.1", 12345, "1.2.3.4", 443); bound == nil || bound.Id != telegram.Id {
				t.Fatalf("the connection is bound to %v, want the Telegram set, or its replies trigger nothing", bound)
			}
			w.telegramFlows.sweep(time.Now().Add(telegramFlowTTL))
			if live := w.telegramFlows.live.Load(); live != 0 {
				t.Fatalf("%d entries survive a minute without a queued packet of the connection", live)
			}

			queuedVerdict(t, w, telegramServerReply(tlsAppData(1400)), nil, 0, idx["eth7"])
			plain, marked := ordinary(), viaTelegram()
			want, wrong := plain, marked
			if c.telegram {
				want, wrong = marked, plain
			}
			if len(want) != 1 || len(wrong) != 0 {
				t.Fatalf("%d packets left by the socket with the dial mark and %d by the ordinary injector, want 1 by the %s one: %s", len(marked), len(plain), map[bool]string{true: "dial mark", false: "ordinary"}[c.telegram],
					map[bool]string{true: "a reply arrived after the outgoing side went quiet, and with the injector mark alone a routing set that lists the server's address takes the packet away from the connection", false: "a forwarded connection already took its route before it reached the queue, whatever its mark or set"}[c.telegram])
			}
		})
	}
}

func TestOnlyConnectionsB4OpensItselfAreNoted(t *testing.T) {
	telegramInjected := uint32(0x8000) | engine.InjectedMarkBit | config.TelegramSetDialMark
	const anyVerdict = -1
	for _, c := range []struct {
		name     string
		chosen   bool
		mark     uint32
		owner    *uint32
		arriving bool
		verdict  int
		noted    bool
		explain  string
	}{
		{"b4's own Telegram connection", true, config.TelegramSetDialMark, nil, false, nfqueue.NfDrop, true, "its injected packets must leave with its dial mark"},
		{"the Telegram bit without a Telegram set, from a router socket", false, config.TelegramSetDialMark, routerSocket(), false, nfqueue.NfDrop, true, "the routing sets skip its packets on bit 0x40000, so they must skip the ones b4 sends into it"},
		{"another connection b4 opens itself", true, config.SelfDialMark, routerSocket(), false, nfqueue.NfDrop, true, "the routing sets skip its packets on bit 0x40000, so they must skip the ones b4 sends into it"},
		{"a forwarded connection whose mark has bit 0x40000", true, config.SelfDialMark, nil, false, nfqueue.NfDrop, false, "another service marked it after it took its route, so the ones b4 sends into it must take the same route"},
		{"a KeeneticOS hotspot device's connection", false, 0x0ffffaaa, nil, false, anyVerdict, false, "the hotspot mark has bit 0x40000, but the connection is a device's"},
		{"a KeeneticOS hotspot device's connection with a Telegram set chosen", true, 0x0ffffaaa, nil, false, anyVerdict, false, "the hotspot mark holds the Telegram set's bits among many others, but the connection is a device's"},
		{"a packet arriving for a router socket, with bit 0x40000", true, config.SelfDialMark, routerSocket(), true, anyVerdict, false, "only a packet a router socket sends opens a connection of b4's own"},
		{"a device's connection", true, 0, nil, false, nfqueue.NfDrop, false, "its packets carry no dial mark, so the ones b4 sends into it follow the same rules"},
		{"a packet b4 sent into its Telegram connection, queued again", true, telegramInjected, nil, false, nfqueue.NfAccept, false, "b4's own output is not a new connection of its own, or every injected packet is injected again"},
	} {
		t.Run(c.name, func(t *testing.T) {
			other := newFragmentingYTImagesSet()
			telegram := newTelegramSet()
			w := newInjectingWorker(t, &other, &telegram)
			w.getConfig().Queue.Mark = 0x8000
			if c.chosen {
				w.getConfig().System.MTProto.UpstreamSet = telegram.Id
			}
			idx := fakeEgressIfaces(t, egressRoutes(egressUplink))
			stubInjectRoute(t, idx["eth7"], true)
			hello := ytimgHello()
			out, in := idx["eth7"], uint32(0)
			if c.arriving {
				out, in = 0, idx["eth7"]
			}
			if got := queuedVerdictFrom(t, w, hello, markOf(c.mark), c.owner, out, in); c.verdict != anyVerdict && got != c.verdict {
				t.Fatalf("verdict %d, want %d: %s", got, c.verdict, c.explain)
			}
			if got := w.telegramFlows.carries(hello); got != c.noted {
				t.Fatalf("flow noted for the dial-mark injector: %v, want %v: %s", got, c.noted, c.explain)
			}
		})
	}
}

func telegramFlowPacketV4(src, dst string, sport, dport uint16, proto byte, fragOffset uint16) []byte {
	pkt := makeV4TCPPacket([]byte("payload!"), 1)
	copy(pkt[12:16], net.ParseIP(src).To4())
	copy(pkt[16:20], net.ParseIP(dst).To4())
	pkt[9] = proto
	binary.BigEndian.PutUint16(pkt[6:8], fragOffset)
	binary.BigEndian.PutUint16(pkt[20:22], sport)
	binary.BigEndian.PutUint16(pkt[22:24], dport)
	return pkt
}

func telegramFragmentV6(base []byte, offset uint16) []byte {
	frag := make([]byte, 0, len(base)+8)
	frag = append(frag, base[:IPv6HeaderLen]...)
	frag[6] = 44
	hdr := make([]byte, 8)
	hdr[0] = base[6]
	binary.BigEndian.PutUint16(hdr[2:4], offset<<3|1)
	frag = append(frag, hdr...)
	frag = append(frag, base[IPv6HeaderLen:]...)
	binary.BigEndian.PutUint16(frag[4:6], uint16(len(frag)-IPv6HeaderLen))
	return frag
}

func TestTheTelegramFlowRegistryFindsEveryPacketOfANotedConnection(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	f := newTelegramFlows()
	if f.carriesAt(ytimgHello(), now) {
		t.Fatal("an empty registry diverts a packet")
	}
	f.note(telegramFlowPacketV4("10.0.0.1", "1.2.3.4", 12345, 443, ipProtoTCP, 0), now)
	v6 := ytimgHelloV6()
	f.note(v6, now)
	f.note(telegramFlowPacketV4("10.0.0.1", "9.9.9.9", 12345, 443, ipProtoUDP, 0), now)

	for _, c := range []struct {
		name string
		pkt  []byte
		want bool
	}{
		{"a segment of the connection", telegramFlowPacketV4("10.0.0.1", "1.2.3.4", 12345, 443, ipProtoTCP, 0), true},
		{"the first IPv4 fragment", telegramFlowPacketV4("10.0.0.1", "1.2.3.4", 12345, 443, ipProtoTCP, 0x2000), true},
		{"a later IPv4 fragment", telegramFlowPacketV4("10.0.0.1", "1.2.3.4", 0, 0, ipProtoTCP, 3), true},
		{"another connection to the same address", telegramFlowPacketV4("10.0.0.1", "1.2.3.4", 12346, 443, ipProtoTCP, 0), false},
		{"the reply direction", telegramFlowPacketV4("1.2.3.4", "10.0.0.1", 443, 12345, ipProtoTCP, 0), false},
		{"a later fragment toward another address", telegramFlowPacketV4("10.0.0.1", "1.2.3.5", 0, 0, ipProtoTCP, 3), false},
		{"a UDP datagram", telegramFlowPacketV4("10.0.0.1", "9.9.9.9", 12345, 443, ipProtoUDP, 0), false},
		{"a segment of the IPv6 connection", v6, true},
		{"the first IPv6 fragment", telegramFragmentV6(v6, 0), true},
		{"a later IPv6 fragment", telegramFragmentV6(v6, 3), true},
		{"an IPv4 packet with the IPv6 connection's ports", telegramFlowPacketV4("0.0.0.1", "0.0.0.2", 12345, 443, ipProtoTCP, 0), false},
		{"a truncated packet", []byte{0x45, 0, 0}, false},
	} {
		if got := f.carriesAt(c.pkt, now.Add(time.Second)); got != c.want {
			t.Errorf("%s: diverted %v, want %v", c.name, got, c.want)
		}
	}

	if !f.carriesAt(v6, now.Add(telegramFlowTTL-time.Millisecond)) {
		t.Fatal("a connection noted just under the expiry ago no longer counts, so a delayed send leaves with the ordinary mark")
	}
	if f.carriesAt(v6, now.Add(telegramFlowTTL)) {
		t.Fatal("a connection nobody noted for the whole expiry still counts")
	}
	f.sweep(now.Add(telegramFlowTTL))
	if f.live.Load() != 0 || len(f.flows) != 0 || len(f.pairs) != 0 {
		t.Fatalf("after the expiry the registry keeps %d flows and %d address pairs (live %d), so every send pays for the lookup", len(f.flows), len(f.pairs), f.live.Load())
	}
	var none *telegramFlows
	none.note(v6, now)
	none.sweep(now)
	if none.carries(v6) {
		t.Fatal("a worker without a registry diverts a packet")
	}
}

func TestTheTUNEnginesLookupKeepsALongLivedTelegramConnectionDiverted(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	f := newTelegramFlows()
	segment := telegramFlowPacketV4("10.0.0.1", "1.2.3.4", 12345, 443, ipProtoTCP, 0)
	fragment := telegramFlowPacketV4("10.0.0.1", "1.2.3.4", 0, 0, ipProtoTCP, 3)
	f.note(segment, now)

	at := now
	for i := range 5 {
		at = at.Add(telegramFlowTTL - time.Second)
		if !f.carriesOnAt(segment, at) {
			t.Fatalf("lookup %d, %v after the connection was noted: the TUN engine stops diverting it, although without xt_connbytes every packet of the connection passes through the engine and only the first ones reach the worker that notes it", i+1, at.Sub(now))
		}
	}
	if !f.carriesAt(fragment, at.Add(telegramFlowTTL-time.Second)) {
		t.Fatal("a later fragment of the connection lost the address pair the segments kept alive")
	}
	if f.carriesOnAt(segment, at.Add(2*telegramFlowTTL)) {
		t.Fatal("a connection idle for longer than the expiry is still diverted, so a later connection on the same addresses and ports would be taken for b4's own")
	}

	f.note(segment, now)
	if !f.carriesAt(segment, now.Add(telegramFlowTTL-time.Second)) || f.carriesAt(segment, now.Add(telegramFlowTTL)) {
		t.Fatal("the NFQUEUE lookup extends the expiry, but there only the worker sends into the connection and it notes every packet it handles")
	}
}

func TestThePoolsLookupForTheTUNEngineExtendsTheExpiry(t *testing.T) {
	flows := newTelegramFlows()
	p := &Pool{state: &runtimeState{telegram: flows}}
	segment := telegramFlowPacketV4("10.0.0.1", "1.2.3.4", 12345, 443, ipProtoTCP, 0)
	flows.note(segment, time.Now().Add(-telegramFlowTTL+10*time.Second))

	if !p.CarriesOwnTelegramFlow(segment) {
		t.Fatal("a connection noted less than the expiry ago is not diverted")
	}
	if !flows.carriesAt(segment, time.Now().Add(30*time.Second)) {
		t.Fatal("the pool's lookup, which the TUN engine diverts its re-sends by, did not extend the expiry, so a long-lived connection falls back to the ordinary re-send mark a minute after its first packets")
	}
}

func TestTheTelegramFlowRegistryStaysBounded(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	f := newTelegramFlows()
	for i := range telegramFlowsMax + 100 {
		dst := net.IPv4(1, byte(i>>16), byte(i>>8), byte(i)).String()
		f.note(telegramFlowPacketV4("10.0.0.1", dst, uint16(i), 443, ipProtoTCP, 0), now)
	}
	if len(f.flows) > telegramFlowsMax || len(f.pairs) > telegramFlowsMax {
		t.Fatalf("%d flows and %d address pairs, want at most %d of each", len(f.flows), len(f.pairs), telegramFlowsMax)
	}
	last := telegramFlowsMax + 99
	newest := telegramFlowPacketV4("10.0.0.1", net.IPv4(1, byte(last>>16), byte(last>>8), byte(last)).String(), uint16(last), 443, ipProtoTCP, 0)
	if !f.carriesAt(newest, now) {
		t.Fatal("the newest connection was evicted to make room")
	}
	later := now.Add(telegramFlowTTL)
	f.note(telegramFlowPacketV4("10.0.0.2", "1.1.1.1", 1, 443, ipProtoTCP, 0), later)
	if len(f.flows) != 1 || len(f.pairs) != 1 {
		t.Fatalf("a full registry of expired connections kept %d flows and %d pairs after a new one, want only the new one", len(f.flows), len(f.pairs))
	}
}

func TestEveryWorkerOfAPoolSharesTheTelegramFlowRegistry(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.IsDiscovery = true
	cfg.Queue.Threads = 3
	cfg.ConfigPath = t.TempDir() + "/b4.json"
	pool := NewPool(&cfg)
	t.Cleanup(pool.Stop)
	if pool.state.telegram == nil {
		t.Fatal("the pool has no Telegram flow registry")
	}
	for i, w := range pool.Workers {
		if w.telegramFlows != pool.state.telegram {
			t.Fatalf("worker %d of %d has its own Telegram flow registry or none: a connection noted on one queue thread must divert the packets every thread sends into it, and without a registry Start opens no Telegram injector at all", i, len(pool.Workers))
		}
	}
}

func TestTheTelegramInjectorMarkAddsTheDialMark(t *testing.T) {
	cfg := config.NewConfig()
	if got, want := telegramInjectorMark(&cfg), injectorMark(&cfg)|uint(config.TelegramSetDialMark); got != want {
		t.Errorf("the Telegram injector mark is %#x, want %#x: the routing chains return on the dial mark, and the capture chain on the injector mark", got, want)
	}
	if got := uint32(telegramInjectorMark(&cfg)); got&engine.InjectedMarkBit == 0 || !config.IsTelegramSetMark(got) {
		t.Errorf("the Telegram injector mark %#x lacks the injected bit or the dial mark", got)
	}

	zero := config.NewConfig()
	zero.Queue.Mark = 0
	if got := telegramInjectorMark(&zero); got != 0 {
		t.Errorf("with queue mark 0 the Telegram injector mark is %#x, want none: its packets would carry exactly the dial mark and be taken for a new Telegram connection", got)
	}

	discovery := config.NewConfig()
	discovery.Queue.IsDiscovery = true
	discovery.Queue.Mark = discovery.DiscoveryInjectedMark()
	if got := telegramInjectorMark(&discovery); got != 0 {
		t.Errorf("Discovery's Telegram injector mark is %#x, want none: no Telegram connection reaches Discovery's queue", got)
	}
}
