package nfq

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func buildHTTPPacketV4(payload []byte) []byte {
	pkt := make([]byte, 20+20+len(payload))
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], uint16(len(pkt)))
	pkt[9] = 6
	copy(pkt[12:16], []byte{192, 168, 1, 2})
	copy(pkt[16:20], []byte{93, 184, 216, 34})
	binary.BigEndian.PutUint16(pkt[20:22], 12345)
	binary.BigEndian.PutUint16(pkt[22:24], 80)
	pkt[32] = 5 << 4
	copy(pkt[40:], payload)
	return pkt
}

func setWithMethodEOL(on bool) *config.SetConfig {
	set := &config.SetConfig{}
	set.TCP.HTTPMethodEOL = on
	return set
}

func TestApplyHTTPMethodEOLKeepsPayloadLength(t *testing.T) {
	w := &Worker{}
	req := []byte("GET / HTTP/1.1\r\nHost: forum.ru-board.com\r\nUser-Agent: curl/8.5.0\r\n\r\n")
	pkt := buildHTTPPacketV4(req)

	out := w.applyHTTPMethodEOL(setWithMethodEOL(true), pkt)

	if len(out) != len(pkt) {
		t.Fatalf("payload length changed by %d bytes, client sequence numbers would desync", len(out)-len(pkt))
	}
	body := out[40:]
	if !bytes.HasPrefix(body, []byte("\r\nGET / HTTP/1.1\r\n")) {
		t.Fatalf("payload does not start with an empty line: %q", body[:20])
	}
	if !bytes.Contains(body, []byte("Host: forum.ru-board.com")) {
		t.Fatal("Host header was damaged")
	}
	if !bytes.Contains(body, []byte("User-Agent: curl/8.5")) {
		t.Fatalf("User-Agent should keep all but its last two bytes: %q", body)
	}
	if bytes.Contains(body, []byte("curl/8.5.0")) {
		t.Fatal("User-Agent was not trimmed, so the length cannot be preserved")
	}
	if got := binary.BigEndian.Uint16(out[2:4]); int(got) != len(out) {
		t.Fatalf("IP total length %d, want %d", got, len(out))
	}
}

func TestApplyHTTPMethodEOLNeedsAUserAgent(t *testing.T) {
	w := &Worker{}
	pkt := buildHTTPPacketV4([]byte("GET / HTTP/1.1\r\nHost: forum.ru-board.com\r\n\r\n"))
	if out := w.applyHTTPMethodEOL(setWithMethodEOL(true), pkt); !bytes.Equal(out, pkt) {
		t.Fatal("without a User-Agent there is nothing to trim, packet must be left alone")
	}
	short := buildHTTPPacketV4([]byte("GET / HTTP/1.1\r\nUser-Agent: a\r\n\r\n"))
	if out := w.applyHTTPMethodEOL(setWithMethodEOL(true), short); !bytes.Equal(out, short) {
		t.Fatal("a one-byte User-Agent value cannot pay for the two prepended bytes")
	}
}

func TestLocateUserAgentValueIsCaseInsensitive(t *testing.T) {
	payload := []byte("GET / HTTP/1.1\r\nHost: x\r\nuSeR-aGeNt:\tMozilla/5.0\r\n\r\n")
	start, end := locateUserAgentValue(payload)
	if start < 0 || end < 0 {
		t.Fatal("header not found")
	}
	if got := string(payload[start:end]); got != "Mozilla/5.0" {
		t.Fatalf("value %q, want Mozilla/5.0", got)
	}
}

func TestApplyHTTPMethodEOLDisabledByDefault(t *testing.T) {
	w := &Worker{}
	pkt := buildHTTPPacketV4([]byte("GET / HTTP/1.1\r\nHost: x\r\n\r\n"))
	if out := w.applyHTTPMethodEOL(&config.SetConfig{}, pkt); !bytes.Equal(out, pkt) {
		t.Fatal("zero-value config must leave the packet alone")
	}
}

func TestApplyHTTPMethodEOLLeavesNonHTTPAlone(t *testing.T) {
	w := &Worker{}
	for name, payload := range map[string][]byte{
		"tls":     {0x16, 0x03, 0x01, 0x00, 0x05, 0x01, 0, 0, 0, 1},
		"garbage": []byte("NOTAMETHOD / HTTP/1.1\r\n\r\n"),
		"empty":   {},
	} {
		pkt := buildHTTPPacketV4(payload)
		if out := w.applyHTTPMethodEOL(setWithMethodEOL(true), pkt); !bytes.Equal(out, pkt) {
			t.Fatalf("%s payload was modified", name)
		}
	}
}

func TestApplyHTTPMethodEOLNeverChangesLength(t *testing.T) {
	w := &Worker{}
	req := []byte("GET / HTTP/1.1\r\nHost: a\r\nUser-Agent: curl/8.5.0\r\n\r\n")
	pkt := buildHTTPPacketV4(req)
	out := w.applyHTTPMethodEOL(setWithMethodEOL(true), pkt)
	if len(out) != len(pkt) {
		t.Fatalf("length must never change, got %d want %d", len(out), len(pkt))
	}
}

func TestLooksLikeHTTPRequestCoversCommonMethods(t *testing.T) {
	for _, m := range []string{"GET /", "POST /", "HEAD /", "PUT /", "DELETE /", "OPTIONS /", "PATCH /", "TRACE /", "CONNECT h"} {
		if !looksLikeHTTPRequest([]byte(m + " HTTP/1.1")) {
			t.Fatalf("%q not recognised as an HTTP request", m)
		}
	}
	if looksLikeHTTPRequest([]byte("GETX / HTTP/1.1")) {
		t.Fatal("GETX must not be recognised")
	}
}
