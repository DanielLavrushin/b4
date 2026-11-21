package nfq

import (
	"net"
	"net/netip"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/selfdial"
)

func TestTheTUNEngineGivesB4sOwnTelegramConnectionsTheTelegramSet(t *testing.T) {
	for i, c := range []struct {
		name     string
		recorded bool
		chosen   bool
		filter   string
		verdict  engine.PacketVerdict
		set      string
		diverted int
		plain    int
		why      string
	}{
		{"a connection b4 opened for the Telegram set", true, true, "", engine.VerdictDrop, "Telegram", 2, 0, "the TUN engine sees no socket mark, so b4 knows its own connection by the port it dialled from and the address it dialled; the set's strategy applies as in NFQUEUE mode, and what b4 sends into the connection carries its dial mark so a routing set does not take it away from the connection"},
		{"a connection b4 did not record", false, true, "", engine.VerdictAccept, "YT images", 0, 0, "only the connections b4 opened for the Telegram set are pinned to it; everything else is matched by the sets' targets"},
		{"a recorded connection while no Telegram set is chosen", true, false, "", engine.VerdictAccept, "YT images", 0, 0, "without a chosen set there is nothing to pin the connection to"},
		{"a recorded connection to a port the Telegram set's filter leaves out", true, true, "5222", engine.VerdictAccept, "", 0, 0, "the port filter is the one target filter that applies to b4's own connections, and no other set takes them"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sport := uint16(47100 + i)
			if c.recorded {
				selfdial.Note(sport, netip.MustParseAddrPort("1.2.3.4:443"))
			}
			other := newPassiveSet()
			telegram := newTelegramSet()
			telegram.TCP.DPortFilter = c.filter
			telegram.TCPPortRanges = config.ParsePortRanges(c.filter)
			w := tunScopeWorker(t, nil, &other, &telegram)
			if c.chosen {
				w.getConfig().System.MTProto.UpstreamSet = telegram.Id
			}
			ordinary, viaDialMark := captureTelegramInjected(t, w)
			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			hello := makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, sport, 0x18)
			if got := w.ProcessPacketDir(hello, engine.DirOriginal); got != c.verdict {
				t.Fatalf("verdict %d, want %d: %s", got, c.verdict, c.why)
			}
			if got := loggedSet(t, ch); got != c.set {
				t.Fatalf("connection logged for set %q, want %q: %s", got, c.set, c.why)
			}
			if plain, marked := ordinary(), viaDialMark(); len(marked) != c.diverted || len(plain) != c.plain {
				t.Fatalf("%d packets sent by the socket with the dial mark and %d by the ordinary one, want %d and %d: %s", len(marked), len(plain), c.diverted, c.plain, c.why)
			}
		})
	}
}

func tunWANHello(sport uint16) []byte {
	pkt := makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, sport, 0x18)
	copy(pkt[12:16], net.ParseIP(tunTestWAN).To4())
	copy(pkt[16:20], net.ParseIP("104.18.8.37").To4())
	return pkt
}

func TestTheTUNEngineKnowsB4sOwnTelegramConnectionsByTheirPortBeforeTheNAT(t *testing.T) {
	thisHost(t)
	const conntrack = "" +
		"ipv4 2 tcp 6 120 ESTABLISHED src=94.189.76.227 dst=104.18.8.37 sport=47201 dport=443 src=104.18.8.37 dst=94.189.76.227 sport=443 dport=47301 [ASSURED] use=1\n" +
		"ipv4 2 tcp 6 120 ESTABLISHED src=192.168.1.50 dst=104.18.8.37 sport=50000 dport=443 src=104.18.8.37 dst=94.189.76.227 sport=443 dport=47202 [ASSURED] use=1\n"
	for _, c := range []struct {
		name     string
		recorded uint16
		wire     uint16
		set      string
		diverted int
		why      string
	}{
		{"b4's connection whose port the NAT changed", 47201, 47301, "Telegram", 2, "the TUN engine sees the port after the NAT, so it looks the connection up by the port b4's socket dialled from, which conntrack gives, and what b4 sends into it is told apart by the tuple on the wire"},
		{"a device's connection the NAT moved onto b4's recorded port", 47202, 47202, "YT images", 0, "the port on the wire matches a connection b4 opened, but conntrack says a device opened this one from another port, and a device's connection is matched by the sets' targets"},
		{"a device's connection from the port b4 recorded", 50000, 47202, "YT images", 0, "the device dialled from the port b4 dialled from, to the same address, so only the origin conntrack gives keeps the device's connection out of the Telegram set"},
		{"a connection conntrack does not know", 47203, 47203, "Telegram", 2, "without a conntrack row the origin is unknown, and the port the packet carries is the best the engine has"},
	} {
		t.Run(c.name, func(t *testing.T) {
			selfdial.Note(c.recorded, netip.MustParseAddrPort("104.18.8.37:443"))
			other := newPassiveSet()
			telegram := newTelegramSet()
			w := tunScopeWorker(t, newResolverWith(t, tunTestWAN, conntrack), &other, &telegram)
			w.getConfig().System.MTProto.UpstreamSet = telegram.Id
			ordinary, viaDialMark := captureTelegramInjected(t, w)
			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			w.ProcessPacketDir(tunWANHello(c.wire), engine.DirOriginal)
			if got := loggedSet(t, ch); got != c.set {
				t.Fatalf("connection logged for set %q, want %q: %s", got, c.set, c.why)
			}
			if plain, marked := ordinary(), viaDialMark(); len(marked) != c.diverted || len(plain) != 0 {
				t.Fatalf("%d packets sent by the socket with the dial mark and %d by the ordinary one, want %d and 0: %s", len(marked), len(plain), c.diverted, c.why)
			}
		})
	}
}

func TestTheTUNEngineLeavesANewConnectionOnAFailedDialsTupleToTheSets(t *testing.T) {
	for i, c := range []struct {
		name     string
		syns     []uint32
		hello    uint32
		set      string
		diverted int
		why      string
	}{
		{"b4's connection after its SYN", []uint32{999}, 1000, "Telegram", 2, "the SYN claims b4's registration and the connection that follows is b4's own"},
		{"b4's connection after its SYN went out again", []uint32{999, 999}, 1000, "Telegram", 2, "a SYN sent again carries the same sequence number, so it is the same connection"},
		{"another connection on the tuple of b4's failed dial", []uint32{999, 4999}, 5000, "YT images", 0, "b4's dial sent its SYN and failed, the kernel gave the port to a connection b4 did not dial, and its SYN with another sequence number shows a new connection, which the sets' targets match and which keeps the ordinary re-send mark"},
	} {
		t.Run(c.name, func(t *testing.T) {
			sport := uint16(47160 + i)
			selfdial.Note(sport, netip.MustParseAddrPort("1.2.3.4:443"))
			other := newPassiveSet()
			telegram := newTelegramSet()
			w := tunScopeWorker(t, nil, &other, &telegram)
			w.getConfig().System.MTProto.UpstreamSet = telegram.Id
			ordinary, viaDialMark := captureTelegramInjected(t, w)
			for _, isn := range c.syns {
				w.ProcessPacketDir(makeV4TCPPacketFromPort(nil, isn, sport, 0x02), engine.DirOriginal)
			}
			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			hello := makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), c.hello, sport, 0x18)
			w.ProcessPacketDir(hello, engine.DirOriginal)
			if got := loggedSet(t, ch); got != c.set {
				t.Fatalf("connection logged for set %q, want %q: %s", got, c.set, c.why)
			}
			if plain, marked := ordinary(), viaDialMark(); len(marked) != c.diverted || len(plain) != 0 {
				t.Fatalf("%d packets sent by the socket with the dial mark and %d by the ordinary one, want %d and 0: %s", len(marked), len(plain), c.diverted, c.why)
			}
			if got := w.telegramFlows.carries(hello); got != (c.diverted > 0) {
				t.Fatalf("the engine's re-sends of the connection carry the dial mark: %v, want %v: %s", got, c.diverted > 0, c.why)
			}
		})
	}
}

func TestDiscoveryInTUNModeDoesNotTakeB4sOwnConnectionsForTheTelegramSet(t *testing.T) {
	sport := uint16(47150)
	selfdial.Note(sport, netip.MustParseAddrPort("1.2.3.4:443"))
	telegram := newTelegramSet()
	w := tunScopeWorker(t, nil, &telegram)
	w.getConfig().System.MTProto.UpstreamSet = telegram.Id
	w.getConfig().Queue.IsDiscovery = true
	pkt, ok := w.parseIPHeaders(makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, sport, 0x18))
	if !ok {
		t.Fatal("the test packet does not parse")
	}
	if set := w.ownTelegramSet(pkt); set != nil {
		t.Fatalf("a Discovery worker pinned a packet to the Telegram set %s: Discovery tests its own strategies, never the Telegram set's", set.Name)
	}
}
