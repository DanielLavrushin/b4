package nfq

import (
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
		sent     int
		why      string
	}{
		{"a connection b4 opened for the Telegram set", true, true, "", engine.VerdictDrop, "Telegram", 2, "the TUN engine sees no socket mark, so b4 knows its own connection by the port it dialled from and the address it dialled, and the Telegram set's strategy applies as in NFQUEUE mode"},
		{"a connection b4 did not record", false, true, "", engine.VerdictAccept, "YT images", 0, "only the connections b4 opened for the Telegram set are pinned to it; everything else is matched by the sets' targets"},
		{"a recorded connection while no Telegram set is chosen", true, false, "", engine.VerdictAccept, "YT images", 0, "without a chosen set there is nothing to pin the connection to"},
		{"a recorded connection to a port the Telegram set's filter leaves out", true, true, "5222", engine.VerdictAccept, "", 0, "the port filter is the one target filter that applies to b4's own connections, and no other set takes them"},
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
			sent := captureInjected(t, w)
			ch, _ := log.GetConnectionHub().Subscribe()
			defer log.GetConnectionHub().Unsubscribe(ch)

			hello := makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, sport, 0x18)
			if got := w.ProcessPacketDir(hello, engine.DirOriginal); got != c.verdict {
				t.Fatalf("verdict %d, want %d: %s", got, c.verdict, c.why)
			}
			if got := loggedSet(t, ch); got != c.set {
				t.Fatalf("connection logged for set %q, want %q: %s", got, c.set, c.why)
			}
			if got := len(sent()); got != c.sent {
				t.Fatalf("%d packets sent in its place, want %d: %s", got, c.sent, c.why)
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
	hello := makeV4TCPPacketFromPort(buildClientHello("i.ytimg.com", 16, 0xAB), 1000, sport, 0x18)
	if set := w.ownTelegramSet(hello); set != nil {
		t.Fatalf("a Discovery worker pinned a packet to the Telegram set %s: Discovery tests its own strategies, never the Telegram set's", set.Name)
	}
}
