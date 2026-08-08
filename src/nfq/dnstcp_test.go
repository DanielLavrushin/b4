package nfq

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"
)

func TestReadDNSTCPMessageRoundTrip(t *testing.T) {
	payload := bytes.Repeat([]byte{0xAB}, 300)
	framed := make([]byte, 2+len(payload))
	binary.BigEndian.PutUint16(framed[0:2], uint16(len(payload)))
	copy(framed[2:], payload)

	got, err := readDNSTCPMessage(bytes.NewReader(framed))
	if err != nil {
		t.Fatalf("readDNSTCPMessage: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("payload mismatch: got %d bytes, want %d", len(got), len(payload))
	}
}

func TestReadDNSTCPMessageRejectsZeroLength(t *testing.T) {
	if _, err := readDNSTCPMessage(bytes.NewReader([]byte{0x00, 0x00})); err == nil {
		t.Fatal("expected error for zero-length message")
	}
}

func TestReadDNSTCPMessageTruncatedBody(t *testing.T) {
	framed := []byte{0x00, 0x10, 0x01, 0x02}
	_, err := readDNSTCPMessage(bytes.NewReader(framed))
	if err == nil {
		t.Fatal("expected error for truncated body")
	}
	if err != io.ErrUnexpectedEOF {
		t.Fatalf("expected ErrUnexpectedEOF, got %v", err)
	}
}

func TestReadDNSTCPMessageTruncatedHeader(t *testing.T) {
	if _, err := readDNSTCPMessage(bytes.NewReader([]byte{0x00})); err == nil {
		t.Fatal("expected error for truncated header")
	}
}

func TestWriteDNSTCPMessageFraming(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	payload := []byte{0xDE, 0xAD, 0xBE, 0xEF}
	go func() {
		_ = writeDNSTCPMessage(server, payload, 5*time.Second)
	}()

	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	got, err := readDNSTCPMessage(client)
	if err != nil {
		t.Fatalf("readDNSTCPMessage: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("got %x, want %x", got, payload)
	}
}

func TestWriteDNSTCPMessageRejectsEmpty(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	if err := writeDNSTCPMessage(server, nil, 5*time.Second); err == nil {
		t.Fatal("expected error for empty message")
	}
}

func TestOriginalDstNonTCP(t *testing.T) {
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()

	if _, _, err := originalDst(server); err == nil {
		t.Fatal("expected error for non-tcp connection")
	}
}
