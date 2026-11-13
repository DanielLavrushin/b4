package nfq

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

const wsKernelWindow = 64240

var wsKernelSynOptions = []byte{2, 4, 5, 0xb4, 4, 2, 8, 10, 0, 0, 0, 1, 0, 0, 0, 0, 1, 3, 3, 7}

var wsSynCopyTimes = []time.Duration{0, time.Second, 3 * time.Second, 7 * time.Second, 15 * time.Second, 31 * time.Second, 63 * time.Second}

func wsTCPAt(pkt []byte) int {
	if pkt[0]>>4 == IPv6 {
		return IPv6HeaderLen
	}
	return int(pkt[0]&0x0f) * 4
}

func wsPacket(ver byte, sport uint16, seq uint32, flags byte, opts, payload []byte) []byte {
	pkt := synWithOptions(ver, opts, payload, seq)
	at := wsTCPAt(pkt)
	binary.BigEndian.PutUint16(pkt[at:], sport)
	pkt[at+13] = flags
	binary.BigEndian.PutUint16(pkt[at+14:], wsKernelWindow)
	if ver == IPv4 {
		sock.FixIPv4Checksum(pkt[:at])
		sock.FixTCPChecksum(pkt)
	} else {
		sock.FixTCPChecksumV6(pkt)
	}
	return pkt
}

func wsWindow(pkt []byte) int {
	return int(binary.BigEndian.Uint16(pkt[wsTCPAt(pkt)+14:]))
}

func wsScale(pkt []byte) int {
	tcp := pkt[wsTCPAt(pkt):]
	off := int(tcp[12]>>4) * 4
	for i := 20; i < off; {
		if tcp[i] == 0 {
			break
		}
		if tcp[i] == 1 {
			i++
			continue
		}
		if i+1 >= off || tcp[i+1] < 2 {
			break
		}
		if tcp[i] == 3 {
			return int(tcp[i+2])
		}
		i += int(tcp[i+1])
	}
	return -1
}

func wsChecksumsValid(pkt []byte) bool {
	if pkt[0]>>4 == IPv6 {
		return sock.TCPChecksumValidV6(pkt)
	}
	return sock.TCPChecksumValid(pkt) && ipv4HeaderChecksumValid(pkt[:wsTCPAt(pkt)])
}

func wsShow(pkt []byte) string {
	tcp := pkt[wsTCPAt(pkt):]
	return fmt.Sprintf("flags=%02x seq=%d win=%d ws=%d len=%d checksums_ok=%v", tcp[13], binary.BigEndian.Uint32(tcp[4:8]), wsWindow(pkt), wsScale(pkt), len(tcp)-int(tcp[12]>>4)*4, wsChecksumsValid(pkt))
}

type wsConn struct {
	t          *testing.T
	c          *wsSizeCache
	cfg        *config.Config
	set        *config.SetConfig
	start      time.Time
	now        time.Time
	port       uint16
	isn        uint32
	unselected bool
}

func newWSConn(t *testing.T, cutoff string) *wsConn {
	cfg, set := wsFixture()
	set.TCP.WSSize.Cutoff = cutoff
	now := time.Now()
	return &wsConn{t: t, c: newWSSizeCache(), cfg: cfg, set: set, start: now, now: now, port: 12345, isn: 5000}
}

func (p *wsConn) at(d time.Duration) *wsConn {
	p.now = p.start.Add(d)
	return p
}

func (p *wsConn) send(flags byte, off int, opts, payload []byte) bool {
	p.t.Helper()
	seq, selected := p.isn+1+uint32(int32(off)), (*config.SetConfig)(nil)
	if flags&0x02 != 0 {
		seq = p.isn
		if !p.unselected {
			selected = p.set
		}
	}
	pkt := wsPacket(IPv4, p.port, seq, flags, opts, payload)
	before := append([]byte(nil), pkt...)
	if !p.c.rewrite(pkt, 20, selected, p.cfg, p.now) {
		if !bytes.Equal(pkt, before) {
			p.t.Fatalf("a packet that was not rewritten left changed: %s", wsShow(pkt))
		}
		return false
	}
	if bytes.Equal(pkt, before) {
		p.t.Fatalf("a packet was reported as rewritten although none of its bytes changed: %s", wsShow(pkt))
	}
	ws := p.set.TCP.WSSize
	if wsWindow(pkt) != wsWindowWanted(ws, before) || !wsChecksumsValid(pkt) || !bytes.Equal(pkt[20+20+len(opts):], payload) {
		p.t.Fatalf("a rewritten packet left as %s", wsShow(pkt))
	}
	if wsScale(pkt) != wsScaleWanted(ws, before) {
		p.t.Fatalf("a rewritten SYN left as %s", wsShow(pkt))
	}
	return true
}

func wsWindowWanted(ws config.WSSizeConfig, original []byte) int {
	if ws.Window > 0 {
		return ws.Window
	}
	return wsWindow(original)
}

func wsScaleWanted(ws config.WSSizeConfig, original []byte) int {
	had := wsScale(original)
	if ws.Scale > 0 && had > ws.Scale {
		return ws.Scale
	}
	return had
}

func (p *wsConn) syn(payload []byte) bool {
	p.t.Helper()
	return p.send(0x02, 0, wsKernelSynOptions, payload)
}

func (p *wsConn) ack() bool {
	p.t.Helper()
	return p.send(0x10, 0, nil, nil)
}

func (p *wsConn) data(off int, payload []byte) bool {
	p.t.Helper()
	return p.send(0x18, off, nil, payload)
}

func (p *wsConn) flow() (wsSizeFlow, bool) {
	key, _ := streamKeyOf(wsPacket(IPv4, p.port, p.isn, 0x02, nil, nil), 20)
	f, ok := p.c.flows[key]
	return f, ok
}

func wsFill(t testing.TB, c *wsSizeCache, cfg *config.Config, set *config.SetConfig, now time.Time) {
	t.Helper()
	for i := 0; i < wsSizeCapacity; i++ {
		if !c.rewrite(wsPacket(IPv4, uint16(i), 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, now) {
			t.Fatalf("SYN %d refused below capacity", i)
		}
	}
}

func wsCaptureLog(t testing.TB, level log.Level, to io.Writer) {
	t.Helper()
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, level, true)
	log.StartCapture(to)
	t.Cleanup(func() {
		log.StopCapture(to)
		log.Init(os.Stderr, previous, true)
	})
}

func wsLogged(buf *syncBuffer, what string) int {
	log.Flush()
	return strings.Count(buf.String(), what)
}

func TestWSSizeFirstDataRecordEndsWithItsCutoff(t *testing.T) {
	p := newWSConn(t, wsFirstData)
	if !p.syn(nil) || !p.ack() || !p.data(0, []byte("first")) {
		t.Fatal("the opening through the first data packet must be rewritten")
	}
	if n := len(p.c.flows); n != 0 {
		t.Fatalf("%d record(s) kept after the first data packet, although nothing can be rewritten any more", n)
	}
	if p.data(5, []byte("more")) || p.data(0, []byte("first")) || p.ack() {
		t.Fatal("a packet after the cutoff was rewritten")
	}
}

func TestWSSizeHelloRecordEndsWithItsFallbackOrDeadline(t *testing.T) {
	hello := buildClientHello("record.example", 300, 0xab)
	t.Run("fallback", func(t *testing.T) {
		p := newWSConn(t, wsHello)
		if !p.syn(nil) || !p.ack() || !p.data(0, []byte("GET / HTTP/1.1\r\n")) {
			t.Fatal("the packet that triggers the fallback is still rewritten")
		}
		if n := len(p.c.flows); n != 0 || p.c.helloBytes != 0 {
			t.Fatalf("%d record(s) and %d buffered bytes kept after the fallback", n, p.c.helloBytes)
		}
		if p.data(16, []byte("Host: record.example\r\n")) {
			t.Fatal("a packet after the fallback was rewritten")
		}
	})
	t.Run("complete ClientHello", func(t *testing.T) {
		p := newWSConn(t, wsHello)
		if !p.syn(nil) || !p.ack() || !p.data(0, hello) {
			t.Fatal("the opening through the ClientHello must be rewritten")
		}
		if f, kept := p.flow(); !kept || f.helloEnd != len(hello) {
			t.Fatalf("a complete ClientHello keeps its record for retransmissions, got %+v kept=%v", f, kept)
		}
		if !p.at(time.Second).data(0, hello) || p.data(len(hello), []byte("application data")) {
			t.Fatal("inside the deadline a retransmitted ClientHello is rewritten and later data is not")
		}
		p.c.cleanup(p.start.Add(wsSizeHelloTimeout))
		if n := len(p.c.flows); n != 0 {
			t.Fatalf("%d record(s) kept past the ClientHello deadline", n)
		}
	})
	t.Run("stalled ClientHello, cleanup", func(t *testing.T) {
		p := newWSConn(t, wsHello)
		if !p.syn(nil) || !p.ack() || !p.data(0, hello[:40]) || p.c.helloBytes == 0 {
			t.Fatal("the first part of the ClientHello must be rewritten and buffered")
		}
		p.c.cleanup(p.start.Add(wsSizeHelloTimeout))
		if n := len(p.c.flows); n != 0 || p.c.helloBytes != 0 {
			t.Fatalf("%d record(s) and %d buffered bytes kept past the ClientHello deadline", n, p.c.helloBytes)
		}
	})
	t.Run("stalled ClientHello, late packet", func(t *testing.T) {
		p := newWSConn(t, wsHello)
		if !p.syn(nil) || !p.ack() || !p.data(0, hello[:40]) {
			t.Fatal("the first part of the ClientHello must be rewritten")
		}
		if p.at(wsSizeHelloTimeout).data(40, hello[40:]) {
			t.Fatal("a packet at the deadline was rewritten")
		}
		if n := len(p.c.flows); n != 0 || p.c.helloBytes != 0 {
			t.Fatalf("%d record(s) and %d buffered bytes kept past the ClientHello deadline", n, p.c.helloBytes)
		}
	})
}

func TestWSSizeRecordsOfUnansweredSynsExpireEarly(t *testing.T) {
	cfg, set := wsFixture()
	c, now := newWSSizeCache(), time.Now()
	wsFill(t, c, cfg, set, now)
	c.cleanup(now.Add(wsSizeSynTTL))
	if n := len(c.flows); n != 0 {
		t.Fatalf("%d records of SYNs that were never answered are still held after %s", n, wsSizeSynTTL)
	}
	if !c.rewrite(wsPacket(IPv4, 60000, 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, now.Add(wsSizeSynTTL)) {
		t.Fatalf("%s after a burst of unanswered SYNs a new connection is still refused", wsSizeSynTTL)
	}
}

func TestWSSizeRecordLifetimeFollowsTheHandshake(t *testing.T) {
	for _, cutoff := range wsCutoffs {
		t.Run(wsCutoffName(cutoff), func(t *testing.T) {
			p := newWSConn(t, cutoff)
			if !p.syn(nil) || !p.at(25*time.Second).syn(nil) {
				t.Fatal("both SYN copies must be rewritten")
			}
			p.c.cleanup(p.start.Add(50 * time.Second))
			if len(p.c.flows) != 1 || !p.at(50*time.Second).ack() {
				t.Fatal("a SYN copy refreshes the record, so the handshake ACK 25 s later still finds it")
			}
			p.c.cleanup(p.now.Add(wsSizeSynTTL))
			if len(p.c.flows) != 1 || !p.at(50*time.Second+wsSizeTTL-time.Second).ack() {
				t.Fatal("an established record keeps the long lifetime")
			}
			p.c.cleanup(p.now.Add(wsSizeTTL))
			if n := len(p.c.flows); n != 0 {
				t.Fatalf("%d record(s) kept past the lifetime of an established record", n)
			}
		})
	}
}

func TestWSSizeEverySynCopyCarriesTheWindow(t *testing.T) {
	hello := buildClientHello("syn.example", 300, 0xab)
	for _, cutoff := range wsCutoffs {
		for _, carried := range [][]byte{nil, hello[:1], hello} {
			for _, swept := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s, %d bytes in the SYN, cleanup %v", wsCutoffName(cutoff), len(carried), swept), func(t *testing.T) {
					p := newWSConn(t, cutoff)
					for _, at := range wsSynCopyTimes {
						if !p.at(at).syn(carried) || !p.syn(nil) {
							t.Fatalf("a SYN copy sent at +%s left with another window or scale than the first one", at)
						}
						if swept {
							p.c.cleanup(p.now)
						}
					}
				})
			}
		}
	}
}

func TestWSSizeASynCopyNeedsNoSecondSelection(t *testing.T) {
	hello := buildClientHello("copy.example", 300, 0xab)
	for _, cutoff := range wsCutoffs {
		for _, carried := range [][]byte{nil, []byte("GET / HTTP/1.1\r\n"), hello} {
			t.Run(fmt.Sprintf("%s, %d bytes in the SYN", wsCutoffName(cutoff), len(carried)), func(t *testing.T) {
				p := newWSConn(t, cutoff)
				if !p.syn(carried) {
					t.Fatal("SYN not rewritten")
				}
				p.unselected = true
				for i, at := range wsSynCopyTimes[1:] {
					payload := carried
					if i%2 == 1 {
						payload = nil
					}
					if !p.at(at).syn(payload) {
						t.Fatalf("the SYN copy sent at +%s of a recorded connection was left alone when its set was not selected a second time", at)
					}
				}
			})
		}
	}
}

func TestWSSizeFastOpenPayloadDoesNotUseUpTheCutoff(t *testing.T) {
	hello := buildClientHello("tfo.example", 300, 0xab)
	for _, copies := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d SYN copies", copies), func(t *testing.T) {
			p := newWSConn(t, wsFirstData)
			for i := 0; i < copies; i++ {
				if !p.syn(hello) {
					t.Fatal("every copy of a Fast Open SYN must be rewritten")
				}
			}
			if !p.ack() || !p.data(0, hello) {
				t.Fatal("the handshake ACK and the data resent after a Fast Open SYN lost the window")
			}
			if n := len(p.c.flows); n != 0 || p.data(len(hello), []byte("later")) {
				t.Fatalf("the first data packet after the handshake is the cutoff, %d record(s) kept", n)
			}
		})
	}
}

func TestWSSizeHelloClockStartsAtTheFirstPayload(t *testing.T) {
	hello := buildClientHello("clock.example", 1500, 0xab)
	const cut = 1200
	t.Run("ClientHello sent seconds after the handshake", func(t *testing.T) {
		p := newWSConn(t, wsHello)
		if !p.syn(nil) || !p.at(100*time.Millisecond).ack() {
			t.Fatal("the handshake must be rewritten")
		}
		if !p.at(6*time.Second).data(0, hello[:cut]) {
			t.Fatal("a ClientHello sent 6 s after the SYN on an idle socket was not rewritten")
		}
		if !p.at(6*time.Second+wsSizeHelloTimeout-time.Millisecond).data(cut, hello[cut:]) {
			t.Fatal("the rest of the ClientHello, inside the deadline counted from its first byte, was not rewritten")
		}
		if f, _ := p.flow(); f.helloEnd != len(hello) {
			t.Fatalf("the ClientHello was not followed to its end: %+v", f)
		}
	})
	t.Run("the deadline still bounds a ClientHello that stalls", func(t *testing.T) {
		p := newWSConn(t, wsHello)
		if !p.syn(nil) || !p.ack() || !p.at(6*time.Second).data(0, hello[:cut]) {
			t.Fatal("the opening through the first part of the ClientHello must be rewritten")
		}
		if p.at(6*time.Second+wsSizeHelloTimeout).data(cut, hello[cut:]) {
			t.Fatalf("a packet %s after the first payload byte was rewritten", wsSizeHelloTimeout)
		}
	})
	t.Run("handshake that needs four SYNs", func(t *testing.T) {
		p := newWSConn(t, wsHello)
		for _, at := range []time.Duration{0, time.Second, 3 * time.Second, 7 * time.Second} {
			if !p.at(at).syn(nil) {
				t.Fatalf("the SYN copy sent at +%s left with another window or scale than the first one", at)
			}
		}
		if !p.at(7100*time.Millisecond).ack() || !p.at(7200*time.Millisecond).data(0, hello[:cut]) || !p.data(cut, hello[cut:]) {
			t.Fatal("the handshake ACK and the ClientHello that follow a late SYN copy lost the window")
		}
		if f, _ := p.flow(); f.helloEnd != len(hello) || p.data(len(hello), []byte("application data")) {
			t.Fatalf("the ClientHello was not followed to its end: %+v", f)
		}
	})
}

func TestWSSizeTheHelloClockIsNotRestartedByLaterPayload(t *testing.T) {
	hello := buildClientHello("dribble.example", 1500, 0xab)
	p := newWSConn(t, wsHello)
	if !p.syn(nil) || !p.ack() || !p.data(0, hello[:400]) {
		t.Fatal("the opening through the first part of the ClientHello must be rewritten")
	}
	if !p.at(4*time.Second).data(400, hello[400:800]) {
		t.Fatalf("a part of the ClientHello sent 4 s after its first byte, inside the %s deadline, was not rewritten", wsSizeHelloTimeout)
	}
	if p.at(6*time.Second).data(800, hello[800:]) {
		t.Fatalf("a part of the ClientHello sent 6 s after its first byte was rewritten, so the %s deadline moved with a later packet", wsSizeHelloTimeout)
	}
	if n := len(p.c.flows); n != 0 || p.c.helloBytes != 0 {
		t.Fatalf("%d record(s) and %d buffered bytes kept past the ClientHello deadline", n, p.c.helloBytes)
	}
}

func TestWSSizeAnExpiredRecordIsNotUsedBeforeTheCleanupRuns(t *testing.T) {
	stages := []struct {
		name     string
		lifetime time.Duration
		open     func(p *wsConn) bool
	}{
		{"a record that has seen only its SYN", wsSizeSynTTL, func(p *wsConn) bool { return p.syn(nil) }},
		{"a record of an established connection", wsSizeTTL, func(p *wsConn) bool { return p.syn(nil) && p.ack() }},
	}
	for _, cutoff := range wsCutoffs {
		for _, stage := range stages {
			t.Run(fmt.Sprintf("%s, %s", wsCutoffName(cutoff), stage.name), func(t *testing.T) {
				p := newWSConn(t, cutoff)
				if !stage.open(p) {
					t.Fatal("the opening must be rewritten")
				}
				if !p.at(stage.lifetime - time.Millisecond).ack() {
					t.Fatalf("a packet sent just inside the %s lifetime was not rewritten", stage.lifetime)
				}
				p = newWSConn(t, cutoff)
				if !stage.open(p) {
					t.Fatal("the opening must be rewritten")
				}
				if p.at(stage.lifetime).ack() {
					t.Fatalf("a packet sent when the %s lifetime had run out, before any cleanup, was rewritten", stage.lifetime)
				}
				if n := len(p.c.flows); n != 0 {
					t.Fatalf("%d record(s) kept after a packet found the %s lifetime run out", n, stage.lifetime)
				}
			})
		}
	}
}

func TestWSSizeARecordLivesOnThroughPacketsThatNeedNoChange(t *testing.T) {
	hello := buildClientHello("kept.example", 300, 0xab)
	for _, cutoff := range wsCutoffs {
		t.Run(wsCutoffName(cutoff), func(t *testing.T) {
			p := newWSConn(t, cutoff)
			p.set.TCP.WSSize.Window, p.set.TCP.WSSize.Scale = 0, 6
			if !p.syn(nil) {
				t.Fatal("the SYN carries Window Scale 7, above the configured 6, and must be rewritten")
			}
			p.unselected = true
			if !p.at(time.Second).syn(nil) {
				t.Fatal("a SYN copy of the recorded connection left with Window Scale 7")
			}
			if p.ack() {
				t.Fatal("with window 0 the handshake ACK has nothing to change, but it was reported as rewritten")
			}
			if _, kept := p.flow(); !kept {
				t.Fatal("the record ended at the handshake ACK, a packet that needed no change")
			}
			if p.data(0, hello) {
				t.Fatal("with window 0 the first data packet has nothing to change, but it was reported as rewritten")
			}
			f, kept := p.flow()
			if cutoff == wsFirstData && kept {
				t.Fatal("the record was kept past the first data packet")
			}
			if cutoff == wsHello && (!kept || f.helloEnd != len(hello)) {
				t.Fatalf("the ClientHello was not followed to its end: %+v kept=%v", f, kept)
			}
		})
	}
}

func wsFillUp(t testing.TB, c *wsSizeCache, cfg *config.Config, set *config.SetConfig, now time.Time) {
	t.Helper()
	for port := 0; len(c.flows) < wsSizeCapacity; port++ {
		if port > wsSizeCapacity {
			t.Fatalf("the table holds %d records after %d SYNs and cannot be filled", len(c.flows), port)
		}
		c.rewrite(wsPacket(IPv4, uint16(port), 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, now)
	}
}

func TestWSSizeAFastOpenSynKeepsItsRecordForItsCopies(t *testing.T) {
	hello := buildClientHello("tfo-copies.example", 300, 0xab)
	carried := map[string][]byte{
		"the start of a ClientHello": hello[:40],
		"a whole ClientHello":        hello,
		"bytes that are not TLS":     []byte("GET / HTTP/1.1\r\n"),
	}
	for name, payload := range carried {
		for _, full := range []bool{false, true} {
			for _, unselected := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s, table full %v, copies unselected %v", name, full, unselected), func(t *testing.T) {
					p := newWSConn(t, wsHello)
					p.port = 40000
					if !p.syn(payload) {
						t.Fatal("the Fast Open SYN must be rewritten")
					}
					fill := func() {
						if full {
							wsFillUp(t, p.c, p.cfg, p.set, p.now)
						}
					}
					fill()
					p.at(6 * time.Second)
					p.c.cleanup(p.now)
					fill()
					if p.c.helloBytes != 0 {
						t.Fatalf("the cleanup at +6 s, past the ClientHello deadline, left %d buffered bytes for a connection whose ClientHello is not followed any more", p.c.helloBytes)
					}
					p.unselected = unselected
					if !p.at(7 * time.Second).syn(nil) {
						t.Fatal("the SYN copy sent at +7 s left with another window or scale than the Fast Open SYN")
					}
					for _, tick := range []time.Duration{8 * time.Second, 10 * time.Second, 12 * time.Second, 14 * time.Second} {
						p.c.cleanup(p.start.Add(tick))
					}
					if !p.at(15 * time.Second).syn(nil) {
						t.Fatal("the SYN copy sent at +15 s left with another window or scale than the Fast Open SYN")
					}
					if p.at(15100 * time.Millisecond).ack() {
						t.Fatal("the handshake ACK sent after the ClientHello deadline was rewritten")
					}
					if _, kept := p.flow(); kept {
						t.Fatal("the record was kept after the first packet that is not a SYN, although the ClientHello deadline has passed")
					}
					if p.data(0, hello) {
						t.Fatal("data sent after the ClientHello deadline was rewritten")
					}
					if p.c.helloBytes != 0 {
						t.Fatalf("the ClientHello buffer counter ended at %d bytes although no record holds a buffer", p.c.helloBytes)
					}
				})
			}
		}
	}
}

func TestWSSizeARecordKeptForSynCopiesStillExpires(t *testing.T) {
	hello := buildClientHello("tfo-expiry.example", 300, 0xab)
	carried := map[string][]byte{
		"the start of a ClientHello": hello[:40],
		"a whole ClientHello":        hello,
		"bytes that are not TLS":     []byte("GET / HTTP/1.1\r\n"),
	}
	for name, payload := range carried {
		t.Run(name, func(t *testing.T) {
			p := newWSConn(t, wsHello)
			if !p.syn(payload) {
				t.Fatal("the Fast Open SYN must be rewritten")
			}
			for tick := 2 * time.Second; tick <= 34*time.Second; tick += 2 * time.Second {
				p.c.cleanup(p.start.Add(tick))
				if _, kept := p.flow(); !kept {
					t.Fatalf("the cleanup at +%s dropped the record of a Fast Open SYN inside the 35 s it is kept for the copies of that SYN", tick)
				}
				if tick >= 6*time.Second && p.c.helloBytes != 0 {
					t.Fatalf("the cleanup at +%s, past the ClientHello deadline, left %d buffered bytes for a connection whose ClientHello is not followed any more", tick, p.c.helloBytes)
				}
			}
			p.c.cleanup(p.start.Add(36 * time.Second))
			if n := len(p.c.flows); n != 0 {
				t.Fatalf("36 s after the only SYN of a Fast Open connection %d record(s) are still held, so the cleanup tick extends the 35 s lifetime of a record that has seen only SYNs", n)
			}
			if p.c.helloBytes != 0 {
				t.Fatalf("the ClientHello buffer counter ended at %d bytes although no record is left", p.c.helloBytes)
			}
		})
	}
}

func TestWSSizeTheAckAfterAFastOpenClientHelloKeepsTheWindow(t *testing.T) {
	hello := buildClientHello("tfo-ack.example", 300, 0xab)
	t.Run("the SYN carried the whole ClientHello", func(t *testing.T) {
		p := newWSConn(t, wsHello)
		if !p.syn(hello) {
			t.Fatal("the Fast Open SYN must be rewritten")
		}
		if f, _ := p.flow(); f.helloEnd != len(hello) {
			t.Fatalf("fixture: the ClientHello in the SYN was not followed to its end: %+v", f)
		}
		if !p.ack() {
			t.Fatal("the handshake ACK left with the kernel's window, between a rewritten SYN and the rewritten ClientHello that is resent as data")
		}
		if !p.data(0, hello) {
			t.Fatal("the ClientHello resent as data lost the window")
		}
		if !p.send(0x10, len(hello)-1, nil, nil) {
			t.Fatal("an ACK whose sequence number is still inside the ClientHello lost the window")
		}
		if p.send(0x10, -1, nil, nil) || p.send(0x10, len(hello), nil, nil) || p.data(len(hello), []byte("application data")) {
			t.Fatal("a packet outside the ClientHello was rewritten after its end")
		}
	})
	t.Run("the ClientHello came as data", func(t *testing.T) {
		p := newWSConn(t, wsHello)
		if !p.syn(nil) || !p.ack() || !p.data(0, hello) {
			t.Fatal("the opening through the ClientHello must be rewritten")
		}
		if p.send(0x10, len(hello), nil, nil) {
			t.Fatal("the ACK that follows a complete ClientHello was rewritten")
		}
	})
	t.Run("the SYN carried bytes that are not TLS, so the fallback ends the rewrite at the SYN", func(t *testing.T) {
		p := newWSConn(t, wsHello)
		if !p.syn([]byte("GET / HTTP/1.1\r\n")) {
			t.Fatal("the Fast Open SYN that triggers the fallback is still rewritten")
		}
		if p.ack() {
			t.Fatal("the handshake ACK was rewritten after the fallback, which ends the rewrite with the packet that triggered it")
		}
		if n := len(p.c.flows); n != 0 || p.data(0, []byte("GET / HTTP/1.1\r\n")) {
			t.Fatalf("after the fallback %d record(s) were kept or the resent data was rewritten", n)
		}
	})
}

func TestWSSizeAnOutgoingResetIsLeftAloneAndKeepsTheRecord(t *testing.T) {
	for _, cutoff := range wsCutoffs {
		for _, flags := range []byte{0x04, 0x14} {
			p := newWSConn(t, cutoff)
			if !p.syn(nil) {
				t.Fatal("SYN not rewritten")
			}
			if p.send(flags, 0, nil, nil) {
				t.Fatalf("%s: an outgoing RST (flags %#02x) was rewritten", wsCutoffName(cutoff), flags)
			}
			if len(p.c.flows) != 1 || !p.ack() || !p.data(0, buildClientHello("rst.example", 0, 0)) {
				t.Fatalf("%s: an outgoing RST (flags %#02x) ended the record", wsCutoffName(cutoff), flags)
			}
		}
	}
}

func TestWSSizeAFullTableSweepsAtMostOncePerSecond(t *testing.T) {
	cfg, set := wsFixture()
	c, now := newWSSizeCache(), time.Now()
	wsFill(t, c, cfg, set, now)
	refuse := func(at time.Time, n int) {
		t.Helper()
		for i := 0; i < n; i++ {
			syn := wsPacket(IPv4, uint16(30000+i), 100, 0x02, wsKernelSynOptions, nil)
			before := append([]byte(nil), syn...)
			if c.rewrite(syn, 20, set, cfg, at) || !bytes.Equal(syn, before) {
				t.Fatal("a SYN was admitted or changed although the table is full")
			}
		}
	}
	refuse(now, 1000)
	if c.sweeps != 1 {
		t.Fatalf("1000 refused SYNs swept the full table %d times, want once", c.sweeps)
	}
	refuse(now.Add(wsSizeSweepEvery-time.Millisecond), 10)
	if c.sweeps != 1 {
		t.Fatalf("the table was swept again inside %s", wsSizeSweepEvery)
	}
	refuse(now.Add(wsSizeSweepEvery), 10)
	if c.sweeps != 2 {
		t.Fatalf("%d sweeps after %s, want 2", c.sweeps, wsSizeSweepEvery)
	}
	if !c.rewrite(wsPacket(IPv4, 60000, 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, now.Add(wsSizeSynTTL)) {
		t.Fatal("a SYN at capacity must evict expired records before it is refused")
	}
}

func TestWSSizeARefusedSynWarnsOncePerInterval(t *testing.T) {
	buf := &syncBuffer{}
	wsCaptureLog(t, log.LevelInfo, buf)
	cfg, set := wsFixture()
	c, now := newWSSizeCache(), time.Now()
	wsFill(t, c, cfg, set, now)
	const line = "WSSize: the connection table is full"
	refuse := func(at time.Time, n int) {
		for i := 0; i < n; i++ {
			c.rewrite(wsPacket(IPv4, uint16(30000+i), 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, at)
		}
	}
	refuse(now, 50)
	if n := wsLogged(buf, line); n != 1 {
		t.Fatalf("50 refused SYNs logged %d warnings, want 1: %q", n, buf.String())
	}
	refuse(now.Add(wsSizeWarnEvery-time.Second), 1)
	if n := wsLogged(buf, line); n != 1 {
		t.Fatalf("a second warning inside %s: %q", wsSizeWarnEvery, buf.String())
	}
	refuse(now.Add(wsSizeWarnEvery), 1)
	if n := wsLogged(buf, line); n != 2 || wsLogged(buf, "(52 so far)") != 1 {
		t.Fatalf("want a second warning that counts all 52 refusals after %s: %q", wsSizeWarnEvery, buf.String())
	}
}

func TestWSSizeAnExhaustedHelloBudgetWarnsOncePerInterval(t *testing.T) {
	buf := &syncBuffer{}
	wsCaptureLog(t, log.LevelInfo, buf)
	hello := buildClientHello("budget.example", 300, 0xab)
	cfg, set := wsFixture()
	set.TCP.WSSize.Cutoff = wsHello
	c, now := newWSSizeCache(), time.Now()
	c.helloBytes = wsSizeHelloBudget
	for port := uint16(1); port <= 5; port++ {
		c.rewrite(wsPacket(IPv4, port, 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, now)
		if !c.rewrite(wsPacket(IPv4, port, 101, 0x18, nil, hello[:40]), 20, nil, cfg, now) {
			t.Fatal("the packet that finds the budget exhausted is still rewritten")
		}
	}
	if n := wsLogged(buf, "WSSize: the ClientHello buffers are full"); n != 1 {
		t.Fatalf("5 connections that found the buffer budget exhausted logged %d warnings, want 1: %q", n, buf.String())
	}
	if len(c.flows) != 0 || c.helloBytes != wsSizeHelloBudget {
		t.Fatalf("%d record(s) kept, budget counter moved to %d", len(c.flows), c.helloBytes)
	}
}

func TestWSSizeTracesTheOpeningAndTheEnd(t *testing.T) {
	hello := buildClientHello("trace.example", 300, 0xab)
	const who = "10.0.0.1:12345 -> 1.2.3.4:443"
	cases := []struct {
		name, cutoff string
		drive        func(p *wsConn)
		want         []string
	}{
		{"first data", wsFirstData, func(p *wsConn) {
			p.syn(nil)
			p.ack()
			p.data(0, hello)
		}, []string{
			"WSSize: opened for " + who + " (set: YT images): window 64240 -> 1, scale 7 -> 1",
			"WSSize: ended for " + who + ": first data packet",
		}},
		{"SYN without a Window Scale option", wsFirstData, func(p *wsConn) {
			p.send(0x02, 0, nil, nil)
			p.send(0x11, 0, nil, nil)
		}, []string{
			"WSSize: opened for " + who + " (set: YT images): window 64240 -> 1, no Window Scale option",
			"WSSize: ended for " + who + ": FIN before the cutoff",
		}},
		{"complete ClientHello", wsHello, func(p *wsConn) {
			p.syn(nil)
			p.data(0, hello[:100])
			p.data(100, hello[100:])
			p.c.cleanup(p.start.Add(wsSizeHelloTimeout))
		}, []string{
			"WSSize: opened for " + who,
			fmt.Sprintf("WSSize: ended for %s: ClientHello complete at %d bytes", who, len(hello)),
		}},
		{"fallback", wsHello, func(p *wsConn) {
			p.syn(nil)
			p.data(0, []byte("GET / HTTP/1.1\r\n"))
		}, []string{
			"WSSize: opened for " + who,
			"WSSize: ended for " + who + ": fallback, the payload is not a TLS ClientHello",
		}},
		{"deadline seen by a packet", wsHello, func(p *wsConn) {
			p.syn(nil)
			p.data(0, hello[:100])
			p.at(wsSizeHelloTimeout).data(100, hello[100:])
		}, []string{
			"WSSize: opened for " + who,
			"WSSize: ended for " + who + ": deadline",
		}},
		{"deadline seen by the cleanup", wsHello, func(p *wsConn) {
			p.syn(nil)
			p.data(0, hello[:100])
			p.c.cleanup(p.start.Add(wsSizeHelloTimeout))
		}, []string{
			"WSSize: opened for " + who,
			"WSSize: ended for " + who + ": deadline",
		}},
		{"window 0 leaves the window as it is", wsFirstData, func(p *wsConn) {
			p.set.TCP.WSSize.Window, p.set.TCP.WSSize.Scale = 0, 6
			p.syn(nil)
			p.ack()
			p.data(0, hello)
		}, []string{
			"WSSize: opened for " + who + " (set: YT images): window 64240 kept, scale 7 -> 6",
			"WSSize: ended for " + who + ": first data packet",
		}},
		{"scale 0 leaves the scale as it is", wsFirstData, func(p *wsConn) {
			p.set.TCP.WSSize.Window, p.set.TCP.WSSize.Scale = 1, 0
			p.syn(nil)
			p.send(0x11, 0, nil, nil)
		}, []string{
			"WSSize: opened for " + who + " (set: YT images): window 64240 -> 1, scale 7 kept",
			"WSSize: ended for " + who + ": FIN before the cutoff",
		}},
		{"a scale that is not above the configured one is not raised", wsFirstData, func(p *wsConn) {
			p.set.TCP.WSSize.Window, p.set.TCP.WSSize.Scale = 0, 14
			p.syn(nil)
		}, []string{
			"WSSize: opened for " + who + " (set: YT images): window 64240 kept, scale 7 kept",
		}},
		{"window 0 and a SYN without a Window Scale option", wsFirstData, func(p *wsConn) {
			p.set.TCP.WSSize.Window, p.set.TCP.WSSize.Scale = 0, 6
			p.send(0x02, 0, nil, nil)
		}, []string{
			"WSSize: opened for " + who + " (set: YT images): window 64240 kept, no Window Scale option",
		}},
		{"a window above the SYN's own is written, not kept", wsFirstData, func(p *wsConn) {
			p.set.TCP.WSSize.Window, p.set.TCP.WSSize.Scale = 65535, 0
			p.syn(nil)
		}, []string{
			"WSSize: opened for " + who + " (set: YT images): window 64240 -> 65535, scale 7 kept",
		}},
		{"a SYN whose Window Scale option holds 0 still has the option", wsFirstData, func(p *wsConn) {
			p.send(0x02, 0, []byte{2, 4, 5, 0xb4, 1, 3, 3, 0}, nil)
		}, []string{
			"WSSize: opened for " + who + " (set: YT images): window 64240 -> 1, scale 0 kept",
		}},
		{"an established record that expires without payload is not a ClientHello deadline", wsHello, func(p *wsConn) {
			p.syn(nil)
			p.ack()
			p.c.cleanup(p.start.Add(wsSizeTTL))
		}, []string{
			"WSSize: opened for " + who + " (set: YT images): window 64240 -> 1, scale 7 -> 1",
		}},
		{"Fast Open SYN whose ClientHello stalls, record kept for its copies", wsHello, func(p *wsConn) {
			p.syn(hello[:100])
			for _, tick := range []time.Duration{6 * time.Second, 8 * time.Second, 10 * time.Second} {
				p.c.cleanup(p.start.Add(tick))
			}
			p.at(11 * time.Second).syn(nil)
			p.at(12 * time.Second).ack()
		}, []string{
			"WSSize: opened for " + who,
			"WSSize: ended for " + who + ": deadline",
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			buf := &syncBuffer{}
			wsCaptureLog(t, log.LevelTrace, buf)
			tc.drive(newWSConn(t, tc.cutoff))
			for _, want := range tc.want {
				if wsLogged(buf, want) != 1 {
					t.Errorf("want one line with %q, got %q", want, buf.String())
				}
			}
			if n := wsLogged(buf, "WSSize:"); n != len(tc.want) {
				t.Errorf("%d WSSize lines, want %d: %q", n, len(tc.want), buf.String())
			}
		})
		t.Run(tc.name+", trace off", func(t *testing.T) {
			buf := &syncBuffer{}
			wsCaptureLog(t, log.LevelInfo, buf)
			tc.drive(newWSConn(t, tc.cutoff))
			if n := wsLogged(buf, "WSSize:"); n != 0 {
				t.Errorf("%d WSSize lines below trace level: %q", n, buf.String())
			}
		})
	}
}

func TestWSSizeAllocatesNothingWhileTraceIsOff(t *testing.T) {
	wsCaptureLog(t, log.LevelInfo, io.Discard)
	for _, cutoff := range wsCutoffs {
		cfg, set := wsFixture()
		set.TCP.WSSize.Cutoff = cutoff
		c, now := newWSSizeCache(), time.Now()
		syn := wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil)
		ack := wsPacket(IPv4, 12345, 101, 0x10, nil, nil)
		last := wsPacket(IPv4, 12345, 101, 0x11, nil, nil)
		if cutoff == wsFirstData {
			last = wsPacket(IPv4, 12345, 101, 0x18, nil, []byte("first data"))
		}
		fromKernel := [][]byte{append([]byte(nil), syn...), append([]byte(nil), ack...), append([]byte(nil), last...)}
		n := testing.AllocsPerRun(100, func() {
			copy(syn, fromKernel[0])
			copy(ack, fromKernel[1])
			copy(last, fromKernel[2])
			if !c.rewrite(syn, 20, set, cfg, now) || !c.rewrite(ack, 20, nil, cfg, now) || !c.rewrite(last, 20, nil, cfg, now) {
				t.Fatal("the connection must be rewritten from its SYN to its end")
			}
		})
		if n != 0 || len(c.flows) != 0 {
			t.Fatalf("%s: a connection that opens and ends cost %v allocations with trace logging off and left %d record(s)", wsCutoffName(cutoff), n, len(c.flows))
		}
	}
}

type wsLockProbe struct {
	c            *wsSizeCache
	lines, under atomic.Int32
}

func (p *wsLockProbe) Write(b []byte) (int, error) {
	if !bytes.Contains(b, []byte("WSSize:")) {
		return len(b), nil
	}
	p.lines.Add(1)
	if p.c.mu.TryLock() {
		p.c.mu.Unlock()
	} else {
		p.under.Add(1)
	}
	return len(b), nil
}

func TestWSSizeLogsAfterTheCacheLockIsReleased(t *testing.T) {
	hello := buildClientHello("lock.example", 300, 0xab)
	cfg, set := wsFixture()
	set.TCP.WSSize.Cutoff = wsHello
	c, now := newWSSizeCache(), time.Now()
	probe := &wsLockProbe{c: c}
	wsCaptureLog(t, log.LevelTrace, probe)
	wsFill(t, c, cfg, set, now)
	c.rewrite(wsPacket(IPv4, 30000, 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, now)
	c.rewrite(wsPacket(IPv4, 1, 101, 0x18, nil, hello), 20, nil, cfg, now)
	c.rewrite(wsPacket(IPv4, 2, 101, 0x18, nil, hello[:40]), 20, nil, cfg, now)
	c.rewrite(wsPacket(IPv4, 3, 101, 0x18, nil, []byte("GET / HTTP/1.1\r\n")), 20, nil, cfg, now)
	buffered := c.helloBytes
	c.helloBytes = wsSizeHelloBudget
	c.rewrite(wsPacket(IPv4, 4, 101, 0x18, nil, hello[:40]), 20, nil, cfg, now)
	c.helloBytes = buffered
	c.cleanup(now.Add(wsSizeHelloTimeout))
	if n := probe.lines.Load(); n < wsSizeCapacity+6 {
		t.Fatalf("only %d WSSize lines were logged, the test did not reach every log site", n)
	}
	if n := probe.under.Load(); n != 0 {
		t.Fatalf("%d WSSize lines were written while the cache lock was held", n)
	}
}

func TestWSSizeAConfigUpdateEmptiesTheCache(t *testing.T) {
	hello := buildClientHello("reload.example", 300, 0xab)
	cfg, set := wsFixture()
	cfg.Queue.IsDiscovery = true
	set.TCP.WSSize.Cutoff = wsHello
	c, now := newWSSizeCache(), time.Now()
	for port := uint16(1); port <= 50; port++ {
		c.rewrite(wsPacket(IPv4, port, 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, now)
		c.rewrite(wsPacket(IPv4, port, 101, 0x18, nil, hello[:40]), 20, nil, cfg, now)
	}
	if len(c.flows) != 50 || c.helloBytes == 0 {
		t.Fatalf("fixture holds %d records and %d buffered bytes", len(c.flows), c.helloBytes)
	}
	w := &Worker{}
	w.cfg.Store(cfg)
	w.matcher.Store(buildMatcher(cfg))
	pool := &Pool{Workers: []*Worker{w}, state: &runtimeState{wssize: c}}
	if err := pool.UpdateConfig(cfg.Clone()); err != nil {
		t.Fatal(err)
	}
	if len(c.flows) != 0 || c.helloBytes != 0 {
		t.Fatalf("%d records and %d buffered bytes of the replaced config are still held", len(c.flows), c.helloBytes)
	}
}

func TestWSSizeACleanupThatEmptiesALargeTableReturnsItsMemory(t *testing.T) {
	cfg, set := wsFixture()
	c, now := newWSSizeCache(), time.Now()
	table := func() uintptr { return reflect.ValueOf(c.flows).Pointer() }
	first := table()
	for i := 0; i < 10; i++ {
		c.rewrite(wsPacket(IPv4, uint16(i), 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, now)
	}
	c.cleanup(now.Add(wsSizeTTL))
	if len(c.flows) != 0 || table() != first {
		t.Fatal("a table that stayed small is emptied in place")
	}
	wsFill(t, c, cfg, set, now)
	c.cleanup(now.Add(wsSizeTTL))
	if len(c.flows) != 0 {
		t.Fatalf("%d records kept past their lifetime", len(c.flows))
	}
	if table() == first {
		t.Fatalf("the table that grew to %d records was kept after it emptied, so its buckets are never returned", wsSizeCapacity)
	}
}

func TestWSSizeTheFastCleanupTickRemovesExpiredRecords(t *testing.T) {
	cfg, set := wsFixture()
	cfg.Queue.IsDiscovery = true
	cfg.ConfigPath = t.TempDir() + "/b4.json"
	pool := NewPool(cfg)
	t.Cleanup(pool.Stop)
	c := pool.state.wssize
	if !c.rewrite(wsPacket(IPv4, 12345, 100, 0x02, wsKernelSynOptions, nil), 20, set, cfg, time.Now().Add(-wsSizeTTL)) {
		t.Fatal("SYN not rewritten")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		c.mu.Lock()
		n := len(c.flows)
		c.mu.Unlock()
		if n == 0 {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("an expired record was still held 5 s later: the cleanup runs on the slow tick only")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func BenchmarkWSSizeSynAtCapacity(b *testing.B) {
	wsCaptureLog(b, log.Level(log.CurLevel.Load()), io.Discard)
	cfg, set := wsFixture()
	c, now := newWSSizeCache(), time.Now()
	wsFill(b, c, cfg, set, now)
	syn := wsPacket(IPv4, 60000, 100, 0x02, wsKernelSynOptions, nil)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		binary.BigEndian.PutUint16(syn[20:22], uint16(30000+i%30000))
		if c.rewrite(syn, 20, set, cfg, now) {
			b.Fatal("admitted at capacity")
		}
	}
}
