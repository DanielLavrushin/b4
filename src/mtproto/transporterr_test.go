package mtproto

import (
	"bytes"
	"encoding/binary"
	"strings"
	"sync"
	"testing"
)

func interFrame(payload []byte) []byte {
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, uint32(len(payload)))
	return append(hdr, payload...)
}

func codePayload(code int32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, uint32(code))
	return b
}

func feedAll(s *dcFrameScanner, stream []byte, chunk int) (out []byte, code int32, found bool) {
	for len(stream) > 0 {
		n := chunk
		if n > len(stream) {
			n = len(stream)
		}
		got, _, c, f := s.feed(stream[:n])
		out = append(out, got...)
		stream = stream[n:]
		if f {
			return out, c, true
		}
	}
	return out, 0, false
}

// feedRest is feedAll for a caller that also needs the bytes that followed the
// error frame, which are the ones a passed-through error must be put back in
// front of.
func feedRest(s *dcFrameScanner, stream []byte) (out, rest []byte, code int32, found bool) {
	return s.feed(stream)
}

func TestDCFrameScannerFindsErrorCodes(t *testing.T) {
	cases := []struct {
		name  string
		proto uint32
		frame func([]byte) []byte
	}{
		{"abridged", connectionTagAbridged, abridgedFrame},
		{"intermediate", connectionTagInter, interFrame},
		{"padded", connectionTagPadded, interFrame},
	}
	for _, tc := range cases {
		for _, code := range []int32{tgErrInvalidDC, tgErrAuthKeyNotFound, tgErrFlood} {
			body := make([]byte, 96)
			for i := range body {
				body[i] = byte(i)
			}
			stream := append(tc.frame(body), tc.frame(codePayload(code))...)
			for _, chunk := range []int{len(stream), 7, 3, 1} {
				s := newDCFrameScanner(tc.proto)
				out, got, found := feedAll(s, append([]byte(nil), stream...), chunk)
				if !found {
					t.Errorf("%s code %d chunk %d: error frame not detected", tc.name, code, chunk)
					continue
				}
				if got != code {
					t.Errorf("%s chunk %d: got code %d want %d", tc.name, chunk, got, code)
				}
				if !bytes.Equal(out, tc.frame(body)) {
					t.Errorf("%s code %d chunk %d: forwarded %d bytes, want the %d-byte message that preceded the error",
						tc.name, code, chunk, len(out), len(tc.frame(body)))
				}
			}
		}
	}
}

// A quick ack is a four-byte value with the top bit set, so it reads as a large
// negative number. Only small negative values are error codes, and every
// documented one is far above this floor.
func TestDCFrameScannerIgnoresQuickAck(t *testing.T) {
	for _, ack := range []int32{-2000, -1 << 20, -1 << 30, -1} {
		if ack > transportErrFloor {
			continue
		}
		s := newDCFrameScanner(connectionTagPadded)
		stream := interFrame(codePayload(ack))
		out, code, found := feedAll(s, stream, len(stream))
		if found {
			t.Errorf("quick ack %d was read as transport error %d", ack, code)
		}
		if !bytes.Equal(out, stream) {
			t.Errorf("quick ack %d was not forwarded unchanged", ack)
		}
	}
}

func TestDCFrameScannerPassesTrafficThrough(t *testing.T) {
	s := newDCFrameScanner(connectionTagPadded)
	var stream []byte
	for _, n := range []int{20, 132, 4096, 88} {
		body := make([]byte, n)
		for i := range body {
			body[i] = byte(n + i)
		}
		stream = append(stream, interFrame(body)...)
	}
	for _, chunk := range []int{len(stream), 1000, 5, 1} {
		s = newDCFrameScanner(connectionTagPadded)
		out, _, found := feedAll(s, append([]byte(nil), stream...), chunk)
		if found {
			t.Fatalf("chunk %d: ordinary traffic read as a transport error", chunk)
		}
		if !bytes.Equal(out, stream) {
			t.Fatalf("chunk %d: forwarded %d bytes, want %d unchanged", chunk, len(out), len(stream))
		}
	}
}

// A framing the scanner cannot follow must cost the session nothing: it hands
// back everything it holds and stops looking.
func TestDCFrameScannerDisablesOnGarbage(t *testing.T) {
	s := newDCFrameScanner(connectionTagPadded)
	stream := append(interFrame(make([]byte, 0)), []byte("whatever follows")...)
	out, _, found := feedAll(s, stream, len(stream))
	if found {
		t.Fatal("a zero-length frame must not be reported as an error code")
	}
	if !bytes.Equal(out, stream) {
		t.Fatalf("forwarded %d bytes, want the whole %d-byte stream", len(out), len(stream))
	}
	if !s.disabled {
		t.Fatal("scanner must switch itself off after an unparseable frame")
	}
	more := []byte("and this too")
	out, _, _, found = s.feed(more)
	if found || !bytes.Equal(out, more) {
		t.Fatal("a disabled scanner must pass everything through untouched")
	}
}

func TestDCFrameScannerUnknownProtoIsInert(t *testing.T) {
	s := newDCFrameScanner(0x12345678)
	stream := interFrame(codePayload(tgErrInvalidDC))
	out, _, found := feedAll(s, stream, len(stream))
	if found {
		t.Fatal("an unknown transport tag must not be parsed")
	}
	if !bytes.Equal(out, stream) {
		t.Fatal("an unknown transport tag must pass everything through")
	}
}

// scriptedConn plays a fixed stream back. holdOpen keeps the side that has
// nothing to say blocked until the relay closes it, so it cannot end the relay
// before the side under test has been read.
type scriptedConn struct {
	read     *bytes.Reader
	holdOpen bool
	wrote    bytes.Buffer
	mu       sync.Mutex
	done     chan struct{}
	once     sync.Once
}

func newScriptedConn(b []byte, holdOpen bool) *scriptedConn {
	return &scriptedConn{read: bytes.NewReader(b), holdOpen: holdOpen, done: make(chan struct{})}
}

func (c *scriptedConn) Read(p []byte) (int, error) {
	n, err := c.read.Read(p)
	if err != nil && c.holdOpen {
		<-c.done
	}
	return n, err
}

func (c *scriptedConn) Write(p []byte) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.wrote.Write(p)
}

func (c *scriptedConn) Close() error {
	c.once.Do(func() { close(c.done) })
	return nil
}

func (c *scriptedConn) written() []byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]byte(nil), c.wrote.Bytes()...)
}

// -444 is b4's own routing fault and must never reach the client: one of them is
// enough for Telegram to switch the proxy off.
func TestRelaySwallowsInvalidDCAndPassesTheRest(t *testing.T) {
	pool := &sync.Pool{New: func() interface{} { b := make([]byte, relayBufSize); return &b }}
	body := make([]byte, 88)
	traffic := interFrame(body)

	for _, tc := range []struct {
		name      string
		code      int32
		swallowed bool
	}{
		{"invalid dc", tgErrInvalidDC, true},
		{"auth key not found", tgErrAuthKeyNotFound, false},
		{"flood", tgErrFlood, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dc := newScriptedConn(append(append([]byte(nil), traffic...), interFrame(codePayload(tc.code))...), false)
			client := newScriptedConn(nil, true)
			var seen int32
			relayConns(client, dc, relayOpts{
				label:   "test",
				bufPool: pool,
				scan:    newDCFrameScanner(connectionTagPadded),
				onTransportErr: func(code int32) bool {
					seen = code
					return code == tgErrInvalidDC
				},
			})
			if seen != tc.code {
				t.Fatalf("handler saw code %d, want %d", seen, tc.code)
			}
			got := client.written()
			if !bytes.HasPrefix(got, traffic) {
				t.Fatalf("the message before the error was not forwarded")
			}
			tail := got[len(traffic):]
			if tc.swallowed {
				if len(tail) != 0 {
					t.Fatalf("-444 reached the client: %x", tail)
				}
			} else if !bytes.Equal(tail, interFrame(codePayload(tc.code))) {
				t.Fatalf("code %d must be passed on unchanged, client got %x", tc.code, tail)
			}
		})
	}
}

func TestDemoteRejectedRouteMarksTheRouteItRodeOn(t *testing.T) {
	wsResetState()
	tcpResetState()
	defer func() {
		wsResetState()
		tcpResetState()
	}()

	native := transportPlan{kind: transportWS, dc: 4, sni: "kws4.web.telegram.org", dialHost: telegramWSEdgeIP, native: true}
	demoteRejectedRoute(dialInfo{transport: "ws-pool", plan: native}, 4)
	if !wsEndpointCooling(telegramWSEdgeIP, "kws4.web.telegram.org") {
		t.Error("a native edge that rejected the session must be stepped over afterwards")
	}
	if !wsCooldownActive(4) {
		t.Error("the DC that rejected the session must go into cooldown")
	}

	addr := "149.154.167.91:443"
	demoteRejectedRoute(dialInfo{transport: "tcp://" + addr, plan: transportPlan{kind: transportTCP, addr: addr}}, 4)
	if !tcpAddrInCooldown(addr) {
		t.Error("a direct address that rejected the session must go into cooldown")
	}
}

// Measured against 149.154.167.220 with a complete req_pq_multi and
// req_DH_params exchange: kwsN-1 answers a primary session with -444 while kwsN
// answers a media session with server_DH_params_ok. Both names resolve to the
// same address, so offering kwsN-1 to a primary session buys nothing and costs
// the user their proxy.
func TestNativeEdgePlansNeverOfferMediaToAPrimarySession(t *testing.T) {
	for _, dc := range []int{2, 4} {
		for _, p := range nativeEdgePlans(dc, dc, telegramWSEdgeIP) {
			if strings.HasSuffix(p.sni, "-1.web.telegram.org") {
				t.Errorf("DC %d is offered the media name %s", dc, p.sni)
			}
		}
		media := nativeEdgePlans(-dc, dc, telegramWSEdgeIP)
		if len(media) != 2 {
			t.Fatalf("DC -%d built %d plan(s), want the media name and the primary one behind it", dc, len(media))
		}
		if !strings.HasSuffix(media[0].sni, "-1.web.telegram.org") {
			t.Errorf("DC -%d must try the media name first, got %s", dc, media[0].sni)
		}
		if strings.HasSuffix(media[1].sni, "-1.web.telegram.org") {
			t.Errorf("DC -%d must fall back to the primary name, got %s", dc, media[1].sni)
		}
		for _, p := range append(nativeEdgePlans(dc, dc, telegramWSEdgeIP), media...) {
			if !p.native || p.dialHost != telegramWSEdgeIP {
				t.Errorf("plan %s lost its native marking or dial host", p.describe())
			}
		}
	}
}

// An error the client is entitled to see must not swallow the traffic behind it.
func TestRelayKeepsTrafficBehindAPassedThroughError(t *testing.T) {
	s := newDCFrameScanner(connectionTagPadded)
	after := interFrame(make([]byte, 60))
	stream := append(interFrame(codePayload(tgErrAuthKeyNotFound)), after...)
	out, rest, code, found := feedRest(s, stream)
	if !found || code != tgErrAuthKeyNotFound {
		t.Fatalf("error frame not detected: code=%d found=%v", code, found)
	}
	if len(out) != 0 {
		t.Fatalf("nothing preceded the error, got %d bytes", len(out))
	}
	if !bytes.Equal(rest, after) {
		t.Fatalf("rest is %d bytes, want the %d that followed the error frame", len(rest), len(after))
	}
}

// Intermediate and padded take the frame length straight off the wire, so a
// desynced or hostile upstream can declare one, two or three bytes. Reading a
// four-byte code out of a payload that short reaches past the bytes actually
// written, which is stale heap memory and, once the holding buffer's capacity
// stops covering it, a panic in a relay goroutine that nothing recovers.
func TestDCFrameScannerRejectsUndersizedFrames(t *testing.T) {
	for _, proto := range []uint32{connectionTagInter, connectionTagPadded} {
		for n := uint32(0); n < transportErrMinPayload; n++ {
			hdr := make([]byte, 4)
			binary.LittleEndian.PutUint32(hdr, n)
			stream := append(hdr, make([]byte, n)...)
			stream = append(stream, []byte("traffic that must survive")...)

			s := newDCFrameScanner(proto)
			out, rest, code, found := s.feed(append([]byte(nil), stream...))
			if found {
				t.Errorf("proto %08x length %d: reported as transport error %d", proto, n, code)
			}
			if len(rest) != 0 {
				t.Errorf("proto %08x length %d: %d bytes left in rest", proto, n, len(rest))
			}
			if !bytes.Equal(out, stream) {
				t.Errorf("proto %08x length %d: forwarded %d bytes, want the whole %d", proto, n, len(out), len(stream))
			}
			if !s.disabled {
				t.Errorf("proto %08x length %d: scanner must switch itself off", proto, n)
			}
		}
	}
}

// The read that motivated the guard above: every frame the scanner holds back
// has at least four payload bytes, so the code is read out of bytes that were
// actually written.
func TestDCFrameScannerHeldFramesAreLongEnoughToRead(t *testing.T) {
	for _, proto := range []uint32{connectionTagAbridged, connectionTagInter, connectionTagPadded} {
		for n := 0; n <= 64; n++ {
			s := newDCFrameScanner(proto)
			s.hdr = make([]byte, 0, 4)
			switch proto {
			case connectionTagAbridged:
				s.hdr = append(s.hdr, byte(n))
			default:
				b := make([]byte, 4)
				binary.LittleEndian.PutUint32(b, uint32(n))
				s.hdr = append(s.hdr, b...)
			}
			got, ready := s.headerLen()
			if !ready {
				continue
			}
			if got >= transportErrMinPayload && got <= transportErrMaxPayload && got < 4 {
				t.Fatalf("proto %08x header %d: payload %d would be held and read as an int32", proto, n, got)
			}
		}
	}
}
