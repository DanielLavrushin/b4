package mtproto

import (
	"bufio"
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
			if err := c.writeFrame(wsOpcodePong, payload); err != nil {
				return 0, err
			}
		case wsOpcodePong:
		case wsOpcodeClose:
			c.closed.Store(true)
			_ = c.writeFrame(wsOpcodeClose, nil)
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
		_ = c.writeFrame(wsOpcodeClose, nil)
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

func (c *wsConn) writeFrame(op byte, payload []byte) error {
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
		return err
	}
	off += 4

	// single buffer + single tls.Write to avoid emitting two TLS records per frame
	buf := make([]byte, off+n)
	copy(buf, hdr[:off])
	maskKey := buf[off-4 : off]
	for i := 0; i < n; i++ {
		buf[off+i] = payload[i] ^ maskKey[i%4]
	}
	c.wMu.Lock()
	defer c.wMu.Unlock()
	_, err := c.tls.Write(buf)
	return err
}

func (c *wsConn) sendPing() error {
	if c.closed.Load() {
		return net.ErrClosed
	}
	return c.writeFrame(wsOpcodePing, nil)
}

func dialWS(host, sni, path string, timeout time.Duration, mark uint) (net.Conn, error) {
	if path == "" {
		path = "/apiws"
	}
	dialer := &net.Dialer{Timeout: timeout}
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
	raw, err := dialer.Dial("tcp", net.JoinHostPort(host, "443"))
	if err != nil {
		return nil, fmt.Errorf("tcp dial %s: %w", host, err)
	}
	if tc, ok := raw.(*net.TCPConn); ok {
		_ = tc.SetNoDelay(true)
		// match tg-ws-proxy: 256KB send/recv buffers - kernel default (~87KB recv,
		// ~16KB send) limits BDP for big media transfers from EU TG edge
		_ = tc.SetReadBuffer(256 * 1024)
		_ = tc.SetWriteBuffer(256 * 1024)
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
	_ = tlsConn.SetDeadline(time.Now().Add(timeout))
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
