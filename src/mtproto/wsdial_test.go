package mtproto

import (
	"bufio"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func startTestWSServer(t *testing.T, handler func(io.Reader, io.Writer)) (host string, cleanup func()) {
	t.Helper()
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
			http.Error(w, "no upgrade", http.StatusBadRequest)
			return
		}
		hj, ok := w.(http.Hijacker)
		if !ok {
			http.Error(w, "no hijacker", http.StatusInternalServerError)
			return
		}
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		resp := "HTTP/1.1 101 Switching Protocols\r\n" +
			"Upgrade: websocket\r\n" +
			"Connection: Upgrade\r\n\r\n"
		if _, err := bufrw.WriteString(resp); err != nil {
			return
		}
		if err := bufrw.Flush(); err != nil {
			return
		}
		handler(bufrw, bufrw)
		_ = bufrw.Flush()
	}))
	srv.TLS = &tls.Config{InsecureSkipVerify: true}
	srv.StartTLS()
	u := srv.URL
	u = strings.TrimPrefix(u, "https://")
	return u, srv.Close
}

func dialTestWS(t *testing.T, addr string) (net.Conn, error) {
	t.Helper()
	host, port, _ := net.SplitHostPort(addr)
	if port == "" {
		port = "443"
	}
	raw, err := net.DialTimeout("tcp", net.JoinHostPort(host, port), 5*time.Second)
	if err != nil {
		return nil, err
	}
	tlsConn := tls.Client(raw, &tls.Config{InsecureSkipVerify: true, ServerName: host})
	if err := tlsConn.Handshake(); err != nil {
		raw.Close()
		return nil, err
	}
	req := "GET /apiws HTTP/1.1\r\n" +
		"Host: " + host + "\r\n" +
		"Upgrade: websocket\r\n" +
		"Connection: Upgrade\r\n" +
		"Sec-WebSocket-Key: dGhlIHNhbXBsZSBub25jZQ==\r\n" +
		"Sec-WebSocket-Version: 13\r\n\r\n"
	if _, err := tlsConn.Write([]byte(req)); err != nil {
		tlsConn.Close()
		return nil, err
	}
	br := bufio.NewReader(tlsConn)
	resp, err := http.ReadResponse(br, &http.Request{Method: "GET"})
	if err != nil {
		tlsConn.Close()
		return nil, err
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		loc := resp.Header.Get("Location")
		resp.Body.Close()
		tlsConn.Close()
		return nil, &wsHandshakeError{statusCode: resp.StatusCode, statusLine: resp.Status, location: loc}
	}
	resp.Body.Close()
	return &wsConn{tls: tlsConn, br: br}, nil
}

func TestWSRoundtrip(t *testing.T) {
	addr, cleanup := startTestWSServer(t, func(r io.Reader, w io.Writer) {
		hdr := make([]byte, 2)
		if _, err := io.ReadFull(r, hdr); err != nil {
			return
		}
		masked := hdr[1]&0x80 != 0
		length := int(hdr[1] & 0x7F)
		var mask [4]byte
		if masked {
			if _, err := io.ReadFull(r, mask[:]); err != nil {
				return
			}
		}
		payload := make([]byte, length)
		if _, err := io.ReadFull(r, payload); err != nil {
			return
		}
		if masked {
			for i := range payload {
				payload[i] ^= mask[i%4]
			}
		}
		out := make([]byte, 2+length)
		out[0] = 0x80 | wsOpcodeBinary
		out[1] = byte(length)
		copy(out[2:], payload)
		_, _ = w.Write(out)
	})
	defer cleanup()

	conn, err := dialTestWS(t, addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	msg := []byte("hello-mtproto")
	if _, err := conn.Write(msg); err != nil {
		t.Fatalf("write: %v", err)
	}
	got := make([]byte, len(msg))
	if _, err := conn.Read(got); err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != string(msg) {
		t.Fatalf("got %q want %q", got, msg)
	}
}

func TestWSRedirectError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://elsewhere.example/")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	addr := strings.TrimPrefix(srv.URL, "https://")
	_, err := dialTestWS(t, addr)
	if err == nil {
		t.Fatal("expected error")
	}
	var he *wsHandshakeError
	if !errors.As(err, &he) {
		t.Fatalf("want wsHandshakeError, got %T: %v", err, err)
	}
	if !he.isRedirect() {
		t.Fatalf("isRedirect=false for status %d", he.statusCode)
	}
	if he.location != "https://elsewhere.example/" {
		t.Fatalf("location=%q", he.location)
	}
}
