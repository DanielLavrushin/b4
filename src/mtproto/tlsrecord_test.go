package mtproto

import (
	"crypto/tls"
	"io"
	"net"
	"testing"
)

func clientHelloHead(t *testing.T, n int) []byte {
	t.Helper()
	c, s := net.Pipe()
	defer s.Close()
	go func() {
		_ = tls.Client(c, &tls.Config{ServerName: "core.telegram.org"}).Handshake()
		c.Close()
	}()
	head := make([]byte, n)
	if _, err := io.ReadFull(s, head); err != nil {
		t.Fatalf("read ClientHello: %v", err)
	}
	return head
}

func TestTLSClientHelloIsNamedInTheBridgeLog(t *testing.T) {
	head := clientHelloHead(t, obfuscatedFrameLen)
	if reservedFirst4(head[:4]) {
		t.Skipf("this ClientHello (% x) is already caught by the reserved-prefix check", head[:4])
	}
	if _, err := decodeObfuscatedDirect(head, nil); err == nil {
		t.Fatal("a TLS ClientHello decoded as an obfuscated2 handshake")
	}
	if !looksLikeTLSRecord(head) {
		t.Errorf("a TLS ClientHello starting % x was not recognised", head[:5])
	}
	if looksLikeTLSRecord(generateFrame(2, 0xeeeeeeee)) {
		t.Error("an obfuscated2 frame was taken for TLS")
	}
}
