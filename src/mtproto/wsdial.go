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
	for {
		op, fin, payload, err := c.readFrame()
		if err != nil {
			return 0, err
		}
		switch op {
		case wsOpcodeBinary, 0x1:
			if !fin {
				return 0, errors.New("ws: fragmented data frames not supported")
			}
			n := copy(p, payload)
			if n < len(payload) {
				c.rxBuf = append(c.rxBuf, payload[n:]...)
			}
			return n, nil
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
	maskKey := hdr[off : off+4]
	off += 4

	masked := make([]byte, n)
	for i := range payload {
		masked[i] = payload[i] ^ maskKey[i%4]
	}
	c.wMu.Lock()
	defer c.wMu.Unlock()
	if _, err := c.tls.Write(hdr[:off]); err != nil {
		return err
	}
	if n > 0 {
		if _, err := c.tls.Write(masked); err != nil {
			return err
		}
	}
	return nil
}

func dialWS(host, sni string, timeout time.Duration, mark uint) (net.Conn, error) {
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
	}
	tlsConn := tls.Client(raw, &tls.Config{
		ServerName: sni,
		MinVersion: tls.VersionTLS12,
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

	req := "GET /apiws HTTP/1.1\r\n" +
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
