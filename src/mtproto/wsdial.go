package mtproto

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/unix"

	"github.com/daniellavrushin/b4/log"
)

const (
	wsOpcodeBinary = 0x2
	wsOpcodeClose  = 0x8
	wsOpcodePing   = 0x9
	wsOpcodePong   = 0xA
)

type wsHandshakeError struct {
	statusCode int
	statusLine string
	location   string
}

func (e *wsHandshakeError) Error() string {
	if e.location != "" {
		return fmt.Sprintf("ws handshake %d: %s (location=%s)", e.statusCode, e.statusLine, e.location)
	}
	return fmt.Sprintf("ws handshake %d: %s", e.statusCode, e.statusLine)
}

func (e *wsHandshakeError) isRedirect() bool {
	switch e.statusCode {
	case 301, 302, 303, 307, 308:
		return true
	}
	return false
}

type wsConn struct {
	tls    *tls.Conn
	br     *bufio.Reader
	rxBuf  []byte
	wMu    sync.Mutex
	closed atomic.Bool
}

func (c *wsConn) Read(p []byte) (int, error) {
	if len(c.rxBuf) > 0 {
		n := copy(p, c.rxBuf)
		c.rxBuf = c.rxBuf[n:]
		return n, nil
	}
	var assembled []byte // accumulates fragmented data frames until FIN
	for {
		op, fin, payload, err := c.readFrame()
		if err != nil {
			return 0, err
		}
		switch op {
		case wsOpcodeBinary, 0x1:
			// fragmented data frame: start accumulating; continuation frames will follow
			if !fin {
				assembled = append(assembled, payload...)
				continue
			}
			full := payload
			if len(assembled) > 0 {
				full = append(assembled, payload...)
				assembled = nil
			}
			n := copy(p, full)
			if n < len(full) {
				c.rxBuf = append(c.rxBuf, full[n:]...)
			}
			return n, nil
		case 0x0: // continuation frame
			if assembled == nil {
				return 0, errors.New("ws: continuation frame without prior data frame")
			}
			assembled = append(assembled, payload...)
			if fin {
				n := copy(p, assembled)
				if n < len(assembled) {
					c.rxBuf = append(c.rxBuf, assembled[n:]...)
				}
				assembled = nil
				return n, nil
			}
		case wsOpcodePing:
			c.tryWriteControl(wsOpcodePong, payload)
		case wsOpcodePong:
		case wsOpcodeClose:
			c.tryWriteControl(wsOpcodeClose, nil)
			c.closed.Store(true)
			return 0, io.EOF
		default:
			return 0, fmt.Errorf("ws: unsupported opcode 0x%x", op)
		}
	}
}

func (c *wsConn) Write(p []byte) (int, error) {
	if c.closed.Load() {
		return 0, net.ErrClosed
	}
	if err := c.writeFrame(wsOpcodeBinary, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wsConn) Close() error {
	if !c.closed.Swap(true) {
		c.tryWriteControl(wsOpcodeClose, nil)
		_ = c.tls.SetWriteDeadline(time.Now())
	}
	return c.tls.Close()
}

// alive does a non-destructive liveness check on the conn. Pool entries can
// sit idle long enough for TG to FIN/RST them; handing such a conn to a client
// causes up=N down=0 short-lived sessions, which break short RPCs (notably
// auth.importAuthorization on secondary DCs - the exact path that makes
// foreign-channel media downloads fail). Cheap (~few ms) since FIN/RST is
// already in the kernel buffer if it happened.
func (c *wsConn) alive() bool {
	if c.closed.Load() {
		return false
	}
	if err := c.tls.SetReadDeadline(time.Now().Add(5 * time.Millisecond)); err != nil {
		return false
	}
	defer func() { _ = c.tls.SetReadDeadline(time.Time{}) }()
	buf, err := c.br.Peek(1)
	if err == nil && len(buf) >= 1 {
		// any buffered byte indicates the conn is alive; reject if it's a CLOSE frame
		if buf[0]&0x0F == wsOpcodeClose {
			return false
		}
		return true
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	return false
}

// liveNow is a zero-wait liveness poll used right after the obfuscated handshake
// is written to a pooled conn, to catch conns Telegram FIN/RST'd between the
// pool's alive() check and the relay's first write (the up=N down=0 in ~1ms
// failure). Uses an already-expired read deadline so Peek returns immediately:
// a pending FIN/RST surfaces as a non-timeout error (dead), an idle-but-open
// conn surfaces as a timeout (alive). Peek does not consume, so buffered data is
// preserved for the relay. Cost is microseconds, unlike alive()'s 5ms wait.
func (c *wsConn) liveNow() bool {
	if c.closed.Load() {
		return false
	}
	if err := c.tls.SetReadDeadline(time.Now().Add(-time.Second)); err != nil {
		return false
	}
	defer func() { _ = c.tls.SetReadDeadline(time.Time{}) }()
	buf, err := c.br.Peek(1)
	if err == nil && len(buf) >= 1 {
		return buf[0]&0x0F != wsOpcodeClose
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return true
	}
	return false
}

func (c *wsConn) LocalAddr() net.Addr  { return c.tls.LocalAddr() }
func (c *wsConn) RemoteAddr() net.Addr { return c.tls.RemoteAddr() }
func (c *wsConn) SetDeadline(t time.Time) error {
	return c.tls.SetDeadline(t)
}
func (c *wsConn) SetReadDeadline(t time.Time) error  { return c.tls.SetReadDeadline(t) }
func (c *wsConn) SetWriteDeadline(t time.Time) error { return c.tls.SetWriteDeadline(t) }

func (c *wsConn) readFrame() (op byte, fin bool, payload []byte, err error) {
	hdr := make([]byte, 2)
	if _, err = io.ReadFull(c.br, hdr); err != nil {
		return 0, false, nil, err
	}
	fin = hdr[0]&0x80 != 0
	op = hdr[0] & 0x0F
	masked := hdr[1]&0x80 != 0
	length := uint64(hdr[1] & 0x7F)
	switch length {
	case 126:
		ext := make([]byte, 2)
		if _, err = io.ReadFull(c.br, ext); err != nil {
			return 0, false, nil, err
		}
		length = uint64(binary.BigEndian.Uint16(ext))
	case 127:
		ext := make([]byte, 8)
		if _, err = io.ReadFull(c.br, ext); err != nil {
			return 0, false, nil, err
		}
		length = binary.BigEndian.Uint64(ext)
	}
	var maskKey [4]byte
	if masked {
		if _, err = io.ReadFull(c.br, maskKey[:]); err != nil {
			return 0, false, nil, err
		}
	}
	if length > 16*1024*1024 {
		return 0, false, nil, fmt.Errorf("ws frame too large: %d", length)
	}
	payload = make([]byte, length)
	if _, err = io.ReadFull(c.br, payload); err != nil {
		return 0, false, nil, err
	}
	if masked {
		for i := range payload {
			payload[i] ^= maskKey[i%4]
		}
	}
	return op, fin, payload, nil
}

func buildWSFrame(op byte, payload []byte) ([]byte, error) {
	var hdr [14]byte
	hdr[0] = 0x80 | op
	n := len(payload)
	var off int
	switch {
	case n < 126:
		hdr[1] = 0x80 | byte(n)
		off = 2
	case n < 65536:
		hdr[1] = 0x80 | 126
		binary.BigEndian.PutUint16(hdr[2:4], uint16(n))
		off = 4
	default:
		hdr[1] = 0x80 | 127
		binary.BigEndian.PutUint64(hdr[2:10], uint64(n))
		off = 10
	}
	if _, err := rand.Read(hdr[off : off+4]); err != nil {
		return nil, err
	}
	off += 4

	buf := make([]byte, off+n)
	copy(buf, hdr[:off])
	maskKey := buf[off-4 : off]
	for i := 0; i < n; i++ {
		buf[off+i] = payload[i] ^ maskKey[i%4]
	}
	return buf, nil
}

func (c *wsConn) writeFrame(op byte, payload []byte) error {
	buf, err := buildWSFrame(op, payload)
	if err != nil {
		return err
	}
	c.wMu.Lock()
	defer c.wMu.Unlock()
	_, werr := c.tls.Write(buf)
	return werr
}

func (c *wsConn) writeFrameLocked(op byte, payload []byte) error {
	buf, err := buildWSFrame(op, payload)
	if err != nil {
		return err
	}
	_, werr := c.tls.Write(buf)
	return werr
}

func (c *wsConn) tryWriteControl(op byte, payload []byte) {
	if !c.wMu.TryLock() {
		return
	}
	defer c.wMu.Unlock()
	_ = c.tls.SetWriteDeadline(time.Now().Add(time.Second))
	_ = c.writeFrameLocked(op, payload)
	if !c.closed.Load() {
		_ = c.tls.SetWriteDeadline(time.Time{})
	}
}

// wsDialPort is the port every WebSocket route is reached on. It is a variable
// only so a test can point a plan at a local listener.
var wsDialPort = "443"

// wsDialEndpoints resolves host to the addresses a dial may use, with any
// address currently cooling moved to the back rather than dropped - a cooling
// address is still a route, and dropping the last one leaves none.
func wsDialEndpoints(ctx context.Context, host, sni string) []string {
	if ip := net.ParseIP(host); ip != nil {
		if !dialFamilyAllows(ip) {
			return nil
		}
		return []string{host}
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil || len(addrs) == 0 {
		return []string{host}
	}
	fresh := make([]string, 0, len(addrs))
	cooling := make([]string, 0, len(addrs))
	skippedV6 := 0
	for _, a := range addrs {
		if !dialFamilyAllows(a.IP) {
			skippedV6++
			continue
		}
		s := a.IP.String()
		if wsEndpointCooling(s, sni) {
			cooling = append(cooling, s)
			continue
		}
		fresh = append(fresh, s)
	}
	if len(fresh) == 0 && len(cooling) == 0 {
		log.Tracef("[tg-ws] %s resolves to %d IPv6 address(es) and this router has no IPv6 route, so there is nothing to dial", host, skippedV6)
		return nil
	}
	return append(fresh, cooling...)
}

// dialWS opens a WebSocket to host, presenting sni. timeout bounds the whole
// call, not one address.
//
// Go's dialer stops walking a name's addresses as soon as one of them completes
// the TCP handshake, so an address that connects and then swallows the
// ClientHello - the shape an SNI filter leaves behind - is never retried against
// a sibling that would have answered in 100 ms. Measured on a censored network:
// kws203.sorokodin.co.uk at 172.67.197.117 timed out after 8067 ms while the
// same name at 104.21.84.223 connected in 227 ms one second later.
func dialWS(host, sni, path string, timeout time.Duration, mark uint) (net.Conn, error) {
	if path == "" {
		path = "/apiws"
	}
	deadline := time.Now().Add(timeout)

	// Resolution is paid out of the same budget as the dial, so it gets a bounded
	// slice of it. Given the whole budget, a slow lookup leaves the handshake to
	// be cut off half-finished and the address blamed for a fault that was the
	// resolver's. On the way out of that window the name is left unresolved and
	// the dialler resolves it again, which is the old behaviour and no worse.
	resolveWindow := timeout / 3
	if resolveWindow > wsResolveMaxWait {
		resolveWindow = wsResolveMaxWait
	}
	rctx, rcancel := context.WithTimeout(context.Background(), resolveWindow)
	eps := wsDialEndpoints(rctx, host, sni)
	rcancel()

	if len(eps) == 0 {
		return nil, fmt.Errorf("tcp dial %s: no usable address (IPv6 is off and the name has no IPv4 address)", host)
	}
	attempts := len(eps)
	if attempts > wsDialMaxEndpoints {
		attempts = wsDialMaxEndpoints
	}

	var lastErr error
	for i := 0; i < attempts; i++ {
		remaining := time.Until(deadline)
		// The first address is tried on whatever is left, because the caller
		// already decided this dial was worth starting. A later one is not: the
		// first has had that time, and a sliver is not a trial.
		if remaining <= 0 || (i > 0 && remaining < wsDialMinAttempt) {
			break
		}
		slot := remaining
		if left := attempts - i; left > 1 {
			if slot = remaining / time.Duration(left); slot < wsDialMinAttempt {
				slot = wsDialMinAttempt
			}
			if slot > remaining {
				slot = remaining
			}
		}
		conn, err := dialWSEndpoint(eps[i], sni, path, slot, mark)
		if err == nil {
			wsEndpointRecovered(eps[i], sni)
			return conn, nil
		}
		lastErr = err
		// Silence is only evidence against an address that was given long enough
		// to break it. Cooling one off for five minutes on the strength of a
		// handshake cut short by the budget would retire a healthy route.
		if isDialTimeout(err) && slot >= wsDialMinAttempt {
			wsEndpointFailed(eps[i], sni)
		}
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("tcp dial %s: no time left after resolving the name", host)
	}
	return nil, lastErr
}

// dialWSEndpoint opens one WebSocket to one address. timeout bounds the attempt
// end to end - connect, TLS and upgrade share a single deadline. A timeout per
// stage instead of a deadline over all of them let a connect that nearly ran out
// hand a full second helping to the handshake behind it, which is the shape a
// censored path has: the SYN arrives after retransmits and the ClientHello is
// then swallowed. Twice the intended wait lands on the client, and the dial
// budget it was drawn from is already spent.
func dialWSEndpoint(host, sni, path string, timeout time.Duration, mark uint) (net.Conn, error) {
	deadline := time.Now().Add(timeout)
	dialer := &net.Dialer{Deadline: deadline}
	if mark > 0 {
		dialer.Control = func(network, address string, c syscall.RawConn) error {
			var sErr error
			if err := c.Control(func(fd uintptr) {
				sErr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, unix.SO_MARK, int(mark))
			}); err != nil {
				return err
			}
			return sErr
		}
	}
	raw, err := dialer.Dial(dialNetwork(), net.JoinHostPort(host, wsDialPort))
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", host, err)
	}
	if tc, ok := raw.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		// match tg-ws-proxy: 256KB send/recv buffers - kernel default (~87KB recv,
		// ~16KB send) limits BDP for big media transfers from EU TG edge
		_ = tc.SetReadBuffer(256 * 1024)
		_ = tc.SetWriteBuffer(256 * 1024)
		setTCPUserTimeout(tc, defaultUserTimeout)
	}
	// Telegram's WS edge only presents proper certs for kws2/kws4; kws1/kws3/kws5
	// fall back to a *.telegram.org cert that doesn't match the 3-label SNI.
	// Cert verification adds no real security here - the MTProto payload is
	// already end-to-end encrypted with the proxy secret. Match tg-ws-proxy.
	tlsConn := tls.Client(raw, &tls.Config{
		ServerName:         sni,
		MinVersion:         tls.VersionTLS12,
		InsecureSkipVerify: true,
	})
	_ = tlsConn.SetDeadline(deadline)
	if err := tlsConn.Handshake(); err != nil {
		raw.Close()
		return nil, fmt.Errorf("tls handshake %s: %w", sni, err)
	}

	keyBytes := make([]byte, 16)
	if _, err := rand.Read(keyBytes); err != nil {
		tlsConn.Close()
		return nil, err
	}
	wsKey := base64.StdEncoding.EncodeToString(keyBytes)

	req := "GET " + path + " HTTP/1.1\r\n" +
		"Host: " + sni + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: " + wsKey + "\r\n" +
		"Sec-WebSocket-Version: 13\r\n" +
		"Sec-WebSocket-Protocol: binary\r\n" +
		"\r\n"
	if _, err := tlsConn.Write([]byte(req)); err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("ws write upgrade: %w", err)
	}

	br := bufio.NewReader(tlsConn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		tlsConn.Close()
		return nil, fmt.Errorf("ws read response: %w", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		loc := resp.Header.Get("Location")
		resp.Body.Close()
		tlsConn.Close()
		return nil, &wsHandshakeError{
			statusCode: resp.StatusCode,
			statusLine: resp.Status,
			location:   loc,
		}
	}
	if !strings.EqualFold(resp.Header.Get("Upgrade"), "websocket") {
		resp.Body.Close()
		tlsConn.Close()
		return nil, errors.New("ws upgrade header missing")
	}
	resp.Body.Close()

	_ = tlsConn.SetDeadline(time.Time{})
	return &wsConn{tls: tlsConn, br: br}, nil
}
