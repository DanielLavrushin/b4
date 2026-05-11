package mtproto

import (
	"encoding/hex"
	"net"
	"strings"
	"testing"
)

func TestParseSecret_Valid(t *testing.T) {
	host := "storage.googleapis.com"
	hostHex := hex.EncodeToString([]byte(host))
	keyHex := "0123456789abcdef0123456789abcdef"
	secretHex := "ee" + keyHex + hostHex

	sec, err := ParseSecret(secretHex)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sec.Host != host {
		t.Fatalf("expected host %q, got %q", host, sec.Host)
	}
	if hex.EncodeToString(sec.Key[:]) != keyHex {
		t.Fatalf("key mismatch")
	}
}

func TestParseSecret_InvalidHex(t *testing.T) {
	_, err := ParseSecret("not-hex")
	if err == nil {
		t.Fatal("expected error for invalid hex")
	}
}

func TestParseSecret_TooShort(t *testing.T) {
	_, err := ParseSecret("ee0102030405060708091011121314")
	if err == nil {
		t.Fatal("expected error for too-short secret")
	}
}

func TestParseSecret_WrongType(t *testing.T) {
	keyHex := "0123456789abcdef0123456789abcdef"
	hostHex := hex.EncodeToString([]byte("google.com"))
	_, err := ParseSecret("dd" + keyHex + hostHex)
	if err == nil {
		t.Fatal("expected error for non-ee secret type")
	}
}

func TestParseSecret_MissingHost(t *testing.T) {
	_, err := ParseSecret("ee0123456789abcdef0123456789abcdef")
	if err == nil {
		t.Fatal("expected error for missing host")
	}
}

func TestGenerateSecret(t *testing.T) {
	sec, err := GenerateSecret("cdn.jsdelivr.net")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sec.Host != "cdn.jsdelivr.net" {
		t.Fatalf("wrong host: %q", sec.Host)
	}
	h := sec.Hex()
	if !strings.HasPrefix(h, "ee") {
		t.Fatalf("hex should start with ee: %s", h)
	}

	sec2, err := ParseSecret(h)
	if err != nil {
		t.Fatalf("round-trip failed: %v", err)
	}
	if sec2.Host != sec.Host {
		t.Fatalf("host mismatch after round-trip")
	}
	if sec2.Key != sec.Key {
		t.Fatalf("key mismatch after round-trip")
	}
}

func TestGenerateSecret_EmptyHost(t *testing.T) {
	_, err := GenerateSecret("")
	if err == nil {
		t.Fatal("expected error for empty host")
	}
}

func TestResolveDC(t *testing.T) {
	for dc := 1; dc <= 5; dc++ {
		addr, err := ResolveDC(dc, false, "")
		if err != nil {
			t.Fatalf("DC %d: %v", dc, err)
		}
		if addr == "" {
			t.Fatalf("DC %d: empty address", dc)
		}
	}

	addr, err := ResolveDC(-2, false, "")
	if err != nil {
		t.Fatalf("DC -2: %v", err)
	}
	if addr == "" {
		t.Fatal("DC -2: empty address")
	}
}

func TestResolveDC_IPv6(t *testing.T) {
	addr, err := ResolveDC(1, true, "")
	if err != nil {
		t.Fatalf("DC 1 v6: %v", err)
	}
	if addr == "" || addr[0] != '[' {
		t.Fatalf("expected IPv6 address, got %q", addr)
	}
}

func TestResolveDC_Unknown(t *testing.T) {
	_, err := ResolveDC(99, false, "")
	if err == nil {
		t.Fatal("expected error for unknown DC")
	}
}

func TestObfuscatedRoundTrip(t *testing.T) {
	sec, _ := GenerateSecret("example.com")
	client, server := net.Pipe()
	defer client.Close()
	defer server.Close()

	go func() {
		frame := generateFrame(2, connectionTagPadded)
		origKeyIV := make([]byte, 48)
		copy(origKeyIV, frame[8:56])

		encKey := deriveKey(frame[8:40], sec.Key[:])
		encIV := make([]byte, 16)
		copy(encIV, frame[40:56])
		encStream, _ := newAESCTR(encKey, encIV)

		encrypted := make([]byte, obfuscatedFrameLen)
		copy(encrypted, frame)
		encStream.XORKeyStream(encrypted, encrypted)
		copy(encrypted[8:56], origKeyIV)

		client.Write(encrypted)
	}()

	result, err := AcceptObfuscated(server, sec)
	if err != nil {
		t.Fatalf("AcceptObfuscated: %v", err)
	}
	if result.DC != 2 {
		t.Fatalf("expected DC 2, got %d", result.DC)
	}
	if result.ProtoTag != connectionTagPadded {
		t.Fatalf("expected proto tag 0x%08x, got 0x%08x", uint32(connectionTagPadded), result.ProtoTag)
	}
}

func TestAcceptObfuscatedAllProtocols(t *testing.T) {
	sec, _ := GenerateSecret("example.com")
	for _, tag := range []uint32{connectionTagAbridged, connectionTagInter, connectionTagPadded} {
		client, server := net.Pipe()
		go func(tag uint32) {
			defer client.Close()
			frame := generateFrame(2, tag)
			origKeyIV := make([]byte, 48)
			copy(origKeyIV, frame[8:56])
			encKey := deriveKey(frame[8:40], sec.Key[:])
			encIV := make([]byte, 16)
			copy(encIV, frame[40:56])
			encStream, _ := newAESCTR(encKey, encIV)
			encrypted := make([]byte, obfuscatedFrameLen)
			copy(encrypted, frame)
			encStream.XORKeyStream(encrypted, encrypted)
			copy(encrypted[8:56], origKeyIV)
			client.Write(encrypted)
		}(tag)
		result, err := AcceptObfuscated(server, sec)
		server.Close()
		if err != nil {
			t.Fatalf("tag 0x%08x: %v", tag, err)
		}
		if result.ProtoTag != tag {
			t.Fatalf("tag 0x%08x: got 0x%08x", tag, result.ProtoTag)
		}
	}
}
