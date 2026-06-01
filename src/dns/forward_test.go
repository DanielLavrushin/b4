package dns

import (
	"bytes"
	"net"
	"testing"
	"time"
)

func TestResolveUpstreamNonFragment(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer pc.Close()

	query := []byte{0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	want := []byte{0x12, 0x34, 0x81, 0x80, 0x00, 0x01, 0x00, 0x01, 0xde, 0xad, 0xbe, 0xef}

	go func() {
		buf := make([]byte, 2048)
		n, addr, err := pc.ReadFrom(buf)
		if err != nil {
			return
		}
		if !bytes.Equal(buf[:n], query) {
			return
		}
		_, _ = pc.WriteTo(want, addr)
	}()

	la := pc.LocalAddr().(*net.UDPAddr)
	resp, err := ResolveUpstream(query, net.IPv4(127, 0, 0, 1), ForwardOptions{
		Port:    la.Port,
		Timeout: 2 * time.Second,
	})
	if err != nil {
		t.Fatalf("ResolveUpstream: %v", err)
	}
	if !bytes.Equal(resp, want) {
		t.Fatalf("response mismatch: got %x want %x", resp, want)
	}
}

func TestResolveUpstreamTimeout(t *testing.T) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer pc.Close()

	la := pc.LocalAddr().(*net.UDPAddr)
	_, err = ResolveUpstream([]byte{0, 0}, net.IPv4(127, 0, 0, 1), ForwardOptions{
		Port:    la.Port,
		Timeout: 150 * time.Millisecond,
	})
	if err == nil {
		t.Fatal("expected timeout error, got nil")
	}
}
