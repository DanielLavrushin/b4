package mtproto

import (
	"bytes"
	"io"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestReservedFirst4(t *testing.T) {
	reserved := [][]byte{
		{0xef, 0x00, 0x00, 0x00},
		{0x48, 0x45, 0x41, 0x44}, // HEAD
		{0x50, 0x4f, 0x53, 0x54}, // POST
		{0x47, 0x45, 0x54, 0x20}, // "GET "
		{0x4f, 0x50, 0x54, 0x49}, // OPTI
		{0x16, 0x03, 0x01, 0x02}, // TLS record header
		{0xdd, 0xdd, 0xdd, 0xdd},
		{0xee, 0xee, 0xee, 0xee},
	}
	for _, b := range reserved {
		if !reservedFirst4(b) {
			t.Errorf("reservedFirst4(% x) = false, want true", b)
		}
	}
	// a plausible random obfuscated prefix must not be flagged
	normal := []byte{0x01, 0x02, 0x03, 0x04}
	if reservedFirst4(normal) {
		t.Errorf("reservedFirst4(% x) = true, want false", normal)
	}
}

func TestValidTransparentDC(t *testing.T) {
	cases := []struct {
		dc   int
		want bool
	}{
		{1, true}, {2, true}, {5, true}, {203, true},
		{-1, true}, {-2, true}, {-5, true}, {-203, true},
		{0, false}, {6, false}, {99, false}, {-99, false},
	}
	for _, c := range cases {
		if got := validTransparentDC(c.dc); got != c.want {
			t.Errorf("validTransparentDC(%d) = %v, want %v", c.dc, got, c.want)
		}
	}
}

func TestPrefixConnReplaysBeforePassthrough(t *testing.T) {
	prefix := []byte("PREFIX")
	body := []byte("BODY")
	pc := &prefixConn{Conn: fakeConn{r: bytes.NewReader(body)}, prefix: append([]byte(nil), prefix...)}

	got, err := io.ReadAll(pc)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	want := append(append([]byte(nil), prefix...), body...)
	if !bytes.Equal(got, want) {
		t.Errorf("prefixConn read = %q, want %q", got, want)
	}
}

func TestPrefixConnPartialReadDoesNotLosePrefix(t *testing.T) {
	prefix := []byte("ABCDEF")
	pc := &prefixConn{Conn: fakeConn{r: bytes.NewReader(nil)}, prefix: append([]byte(nil), prefix...)}
	buf := make([]byte, 2)
	var got []byte
	for {
		n, err := pc.Read(buf)
		got = append(got, buf[:n]...)
		if err != nil {
			break
		}
		if len(got) >= len(prefix) {
			break
		}
	}
	if !bytes.Equal(got, prefix) {
		t.Errorf("got %q, want %q", got, prefix)
	}
}

// fakeConn is a minimal net.Conn backed by a reader (and optional write sink).
type fakeConn struct {
	r io.Reader
	w io.Writer
}

func (c fakeConn) Read(p []byte) (int, error) {
	if c.r == nil {
		return 0, io.EOF
	}
	return c.r.Read(p)
}
func (c fakeConn) Write(p []byte) (int, error) {
	if c.w == nil {
		return len(p), nil
	}
	return c.w.Write(p)
}
func (c fakeConn) Close() error                       { return nil }
func (c fakeConn) LocalAddr() net.Addr                { return fakeAddr{} }
func (c fakeConn) RemoteAddr() net.Addr               { return fakeAddr{} }
func (c fakeConn) SetDeadline(t time.Time) error      { return nil }
func (c fakeConn) SetReadDeadline(t time.Time) error  { return nil }
func (c fakeConn) SetWriteDeadline(t time.Time) error { return nil }

type fakeAddr struct{}

func (fakeAddr) Network() string { return "tcp" }
func (fakeAddr) String() string  { return "1.2.3.4:5" }

func newBridge() *TransparentBridge {
	return NewTransparentBridge(&config.Config{})
}

func TestHandleEmptyConnReturnsHandledNil(t *testing.T) {
	// immediate EOF (head==0) -> connection closed, nothing to fail open with
	b := newBridge()
	handled, failover := b.Handle(fakeConn{r: bytes.NewReader(nil)}, net.ParseIP("1.2.3.4"), 443)
	if !handled || failover != nil {
		t.Fatalf("empty conn: got (handled=%v, failover=%v), want (true, nil)", handled, failover)
	}
}

func TestHandleSilentConnWaitsForConfiguredTimeout(t *testing.T) {
	// a client that opens a DC connection ahead of having anything to send must
	// be given the configured grace period, not torn down immediately
	cfg := &config.Config{}
	cfg.System.MTProto.BridgeWaitSec = 1
	b := NewTransparentBridge(cfg)

	client, peer := net.Pipe()
	defer peer.Close()

	start := time.Now()
	handled, failover := b.Handle(client, net.ParseIP("1.2.3.4"), 443)
	elapsed := time.Since(start)

	if !handled || failover != nil {
		t.Fatalf("silent conn: got (handled=%v, failover=%v), want (true, nil)", handled, failover)
	}
	if elapsed < 900*time.Millisecond {
		t.Errorf("silent conn dropped after %v, want it held for the full ~1s wait", elapsed)
	}
	if elapsed > 5*time.Second {
		t.Errorf("silent conn held for %v, want it dropped near the 1s wait", elapsed)
	}
}

func TestHandleAcceptsHandshakeAfterSilence(t *testing.T) {
	// the handshake arriving well after the old fixed 5s deadline must still be
	// processed instead of the connection having been dropped underneath it
	cfg := &config.Config{}
	cfg.System.MTProto.BridgeWaitSec = 10
	b := NewTransparentBridge(cfg)

	client, peer := net.Pipe()
	defer peer.Close()

	in := []byte{0x16, 0x03, 0x01, 0x02}
	go func() {
		time.Sleep(300 * time.Millisecond)
		_, _ = peer.Write(in)
	}()

	handled, failover := b.Handle(client, net.ParseIP("1.2.3.4"), 443)
	if handled {
		t.Fatalf("late reserved prefix should fail open, not be handled")
	}
	if failover == nil {
		t.Fatal("late reserved prefix: expected failover conn, got nil")
	}
	got := make([]byte, len(in))
	if _, err := io.ReadFull(failover, got); err != nil {
		t.Fatalf("replay read: %v", err)
	}
	if !bytes.Equal(got, in) {
		t.Errorf("failover replayed % x, want % x", got, in)
	}
}

func TestBridgeWait(t *testing.T) {
	cases := []struct {
		sec  int
		want time.Duration
	}{
		{0, defaultBridgeWait},
		{-1, 0},
		{45, 45 * time.Second},
	}
	for _, c := range cases {
		cfg := &config.Config{}
		cfg.System.MTProto.BridgeWaitSec = c.sec
		if got := bridgeWait(cfg); got != c.want {
			t.Errorf("bridgeWait(%d) = %v, want %v", c.sec, got, c.want)
		}
	}
}

func TestHandleReservedPrefixFailsOpenWithBytes(t *testing.T) {
	// non-obfuscated transport (TLS record header) -> fail open, replay the 4 bytes
	b := newBridge()
	in := []byte{0x16, 0x03, 0x01, 0x02, 0x00, 0xaa, 0xbb}
	handled, failover := b.Handle(fakeConn{r: bytes.NewReader(in)}, net.ParseIP("1.2.3.4"), 443)
	if handled {
		t.Fatalf("reserved prefix should not be handled by bridge")
	}
	if failover == nil {
		t.Fatal("reserved prefix: expected failover conn, got nil")
	}
	// failover is the original conn with the 4 read bytes re-prepended, so the
	// whole original stream must be recoverable intact for the direct dial.
	got, _ := io.ReadAll(failover)
	if !bytes.Equal(got, in) {
		t.Errorf("failover replayed % x, want full original stream % x", got, in)
	}
}

func TestHandlePartialReadFailsOpenWithBytes(t *testing.T) {
	// fewer than 4 bytes then EOF -> fail open replaying what arrived
	b := newBridge()
	in := []byte{0x01, 0x02}
	handled, failover := b.Handle(fakeConn{r: bytes.NewReader(in)}, net.ParseIP("1.2.3.4"), 443)
	if handled {
		t.Fatalf("partial read should fail open, not be handled")
	}
	if failover == nil {
		t.Fatal("partial read: expected failover conn, got nil")
	}
	got, _ := io.ReadAll(failover)
	if !bytes.Equal(got, in) {
		t.Errorf("failover replayed % x, want % x", got, in)
	}
}

func TestHandleUnresolvedDCFailsOpenWithFullFrame(t *testing.T) {
	// a full 64-byte obfuscated frame whose decoded DC is invalid and whose
	// source IP maps to no DC -> fail open replaying all 64 bytes.
	b := newBridge()
	frame := make([]byte, obfuscatedFrameLen)
	for i := range frame {
		frame[i] = byte(i + 1) // non-reserved first 4 bytes (0x01..)
	}
	// ensure first4 is not accidentally reserved and byte0 != 0xef
	if reservedFirst4(frame[:4]) {
		t.Fatal("test frame unexpectedly reserved")
	}
	handled, failover := b.Handle(fakeConn{r: bytes.NewReader(frame)}, net.ParseIP("8.8.8.8"), 443)
	if handled {
		t.Fatalf("unresolved DC should fail open, not be handled")
	}
	if failover == nil {
		t.Fatal("unresolved DC: expected failover conn, got nil")
	}
	got, _ := io.ReadAll(failover)
	if len(got) != obfuscatedFrameLen || !bytes.Equal(got, frame) {
		t.Errorf("failover replayed %d bytes, want full %d-byte frame intact", len(got), obfuscatedFrameLen)
	}
}

func TestApplyHandshakeMedia(t *testing.T) {
	cases := []struct {
		name      string
		resolved  int
		handshake int
		wantDC    int
		wantOK    bool
	}{
		{"media dc203 from ip table", 203, -203, -203, true},
		{"media dc2 from ip table", 2, -2, -2, true},
		{"main dc stays main", 2, 2, 2, false},
		{"garbage handshake ignored", 2, -606, 2, false},
		{"mismatched magnitude ignored", 2, -4, 2, false},
		{"already signed resolved untouched", -2, -2, -2, false},
		{"zero handshake ignored", 203, 0, 203, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := applyHandshakeMedia(c.resolved, c.handshake)
			if got != c.wantDC || ok != c.wantOK {
				t.Fatalf("applyHandshakeMedia(%d, %d) = (%d, %v), want (%d, %v)",
					c.resolved, c.handshake, got, ok, c.wantDC, c.wantOK)
			}
		})
	}
}
