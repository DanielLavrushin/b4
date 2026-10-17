package nfq

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
)

const earlyConnKey = "10.0.0.1:12345->1.2.3.4:443"

func TestAConnectionRegisteredAtItsClientHelloJudgesAResetByItsSynAck(t *testing.T) {
	for _, c := range []struct {
		name      string
		ttl       uint8
		opts, ack bool
		drop      bool
		reason    string
	}{
		{"the server's own reset", 52, true, true, false, ""},
		{"a reset with another TTL", 60, true, true, true, "TTL mismatch"},
		{"a reset without the server's TCP options", 52, false, true, true, "TCP options stripped"},
	} {
		t.Run(c.name, func(t *testing.T) {
			tracker := &connStateTracker{conns: make(map[string]*connInfo)}
			tracker.RecordEarlySynAck("10.0.0.1", 12345, "1.2.3.4", 443, 52, true)
			tracker.RegisterOutgoing(earlyConnKey, &config.SetConfig{})
			drop, reason := tracker.CheckRST("10.0.0.1", 12345, "1.2.3.4", 443, c.ttl, c.opts, c.ack, 3)
			if drop != c.drop || !strings.Contains(reason, c.reason) {
				t.Fatalf("drop=%v reason %q, want drop=%v with %q", drop, reason, c.drop, c.reason)
			}
			if len(tracker.synAcks) != 0 {
				t.Fatalf("the registration must use up the SYN-ACK it was seeded from, %d left", len(tracker.synAcks))
			}
		})
	}
}

func TestAnEarlySynAckOnlySeedsAFreshUnregisteredConnection(t *testing.T) {
	stale := &connStateTracker{conns: make(map[string]*connInfo)}
	stale.RecordEarlySynAck("10.0.0.1", 12345, "1.2.3.4", 443, 52, true)
	seen := stale.synAcks[earlyConnKey]
	seen.at = seen.at.Add(-earlySynAckTTL)
	stale.synAcks[earlyConnKey] = seen
	stale.RegisterOutgoing(earlyConnKey, &config.SetConfig{})
	if _, reason := stale.CheckRST("10.0.0.1", 12345, "1.2.3.4", 443, 52, true, true, 3); reason != "RST before any server response" {
		t.Fatalf("a SYN-ACK older than %v belongs to another connection on that port, yet the reset was judged by it: %q", earlySynAckTTL, reason)
	}

	registered := newTrackerWithConn()
	registered.RecordEarlySynAck("10.0.0.1", 12345, "1.2.3.4", 443, 52, true)
	if len(registered.synAcks) != 0 {
		t.Fatal("a registered connection records its SYN-ACK as a server response, not as an early one")
	}

	unused := &connStateTracker{conns: make(map[string]*connInfo)}
	unused.RecordEarlySynAck("10.0.0.1", 12345, "1.2.3.4", 443, 0, true)
	if len(unused.synAcks) != 0 {
		t.Fatal("a SYN-ACK without a TTL tells nothing")
	}
	unused.RecordEarlySynAck("10.0.0.1", 12345, "1.2.3.4", 443, 52, true)
	old := unused.synAcks[earlyConnKey]
	old.at = old.at.Add(-earlySynAckTTL)
	unused.synAcks[earlyConnKey] = old
	unused.Cleanup()
	if len(unused.synAcks) != 0 {
		t.Fatal("Cleanup must forget the SYN-ACKs no ClientHello came for")
	}
}

func TestEarlySynAcksStayBounded(t *testing.T) {
	tracker := &connStateTracker{conns: make(map[string]*connInfo)}
	for i := 0; i < maxEarlySynAcks+50; i++ {
		tracker.RecordEarlySynAck("10.0.0.1", uint16(10000+i), "1.2.3.4", 443, 52, true)
	}
	if n := len(tracker.synAcks); n != maxEarlySynAcks {
		t.Fatalf("%d SYN-ACKs remembered, want at most %d", n, maxEarlySynAcks)
	}
	last := fmt.Sprintf(connKeyFormat, "10.0.0.1", 10000+maxEarlySynAcks+49, "1.2.3.4", 443)
	if _, ok := tracker.synAcks[last]; !ok {
		t.Fatal("the newest SYN-ACK must be kept when an older one makes room")
	}
}

func serverSegment(clientPort uint16, flags byte, ttl uint8) []byte {
	const ipHL, tcpHL = 20, 24
	pkt := make([]byte, ipHL+tcpHL)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	pkt[8] = ttl
	pkt[9] = 6
	copy(pkt[12:16], []byte{1, 2, 3, 4})
	copy(pkt[16:20], []byte{10, 0, 0, 1})
	binary.BigEndian.PutUint16(pkt[ipHL:ipHL+2], 443)
	binary.BigEndian.PutUint16(pkt[ipHL+2:ipHL+4], clientPort)
	binary.BigEndian.PutUint32(pkt[ipHL+4:ipHL+8], 7000)
	pkt[ipHL+12] = byte(tcpHL/4) << 4
	pkt[ipHL+13] = flags
	copy(pkt[ipHL+20:], []byte{2, 4, 0x05, 0xb4})
	return pkt
}

func TestRSTProtectionJudgesTheFirstConnectionToANewAddressByItsSynAck(t *testing.T) {
	buf := &syncBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})

	cfg := directionConfig("443")
	w := directionWorker(t, &cfg)
	const synAck, rstAck = 0x12, 0x14

	for _, c := range []struct {
		name    string
		port    uint16
		synAck  bool
		rstTTL  uint8
		verdict engine.PacketVerdict
		reason  string
	}{
		{"the server's own reset passes", 40001, true, 52, engine.VerdictAccept, ""},
		{"a reset with another TTL is dropped", 40002, true, 60, engine.VerdictDrop, "TTL mismatch"},
		{"without the SYN-ACK nothing is known of the server", 40003, false, 52, engine.VerdictDrop, "RST before any server response"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if c.synAck {
				if v := w.ProcessPacket(serverSegment(c.port, synAck, 52)); v != engine.VerdictAccept {
					t.Fatalf("the SYN-ACK passes, got %v", v)
				}
			}
			if v := w.ProcessPacket(clientHelloFrom(c.port)); v != engine.VerdictDrop {
				t.Fatalf("the fragmenting set takes the ClientHello and registers the connection, got %v", v)
			}
			before := strings.Count(buf.String(), "RST protection: dropped")
			if v := w.ProcessPacket(serverSegment(c.port, rstAck, c.rstTTL)); v != c.verdict {
				t.Fatalf("reset verdict %v, want %v:\n%s", v, c.verdict, buf.String())
			}
			drops := strings.Count(buf.String(), "RST protection: dropped")
			if c.reason == "" {
				if drops != before {
					t.Fatalf("the server's own reset was dropped:\n%s", buf.String())
				}
				return
			}
			if drops != before+1 || !strings.Contains(lastLine(buf.String()), c.reason) {
				t.Fatalf("want one drop for %q:\n%s", c.reason, buf.String())
			}
		})
	}
	if n := len(w.connTracker.synAcks); n != 0 {
		t.Fatalf("%d SYN-ACKs left behind after their connections registered", n)
	}
}

func lastLine(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	return lines[len(lines)-1]
}
