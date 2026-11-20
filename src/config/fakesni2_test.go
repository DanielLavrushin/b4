package config

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"testing"
)

func builtInHelloServerName(t *testing.T, hello []byte) string {
	t.Helper()
	if len(hello) < 44 || hello[0] != 0x16 || hello[5] != 0x01 {
		t.Fatalf("not a TLS ClientHello record: % x", hello[:min(len(hello), 12)])
	}
	if got := int(binary.BigEndian.Uint16(hello[3:5])); got != len(hello)-5 {
		t.Fatalf("record length %d, %d bytes follow the header", got, len(hello)-5)
	}
	if got := int(hello[6])<<16 | int(hello[7])<<8 | int(hello[8]); got != len(hello)-9 {
		t.Fatalf("handshake length %d, %d bytes follow it", got, len(hello)-9)
	}
	p := 9 + 2 + 32
	p += 1 + int(hello[p])
	p += 2 + int(binary.BigEndian.Uint16(hello[p:]))
	p += 1 + int(hello[p])
	end := p + 2 + int(binary.BigEndian.Uint16(hello[p:]))
	p += 2
	if end != len(hello) {
		t.Fatalf("extensions end at %d, record ends at %d", end, len(hello))
	}
	name := ""
	for p+4 <= end {
		typ := binary.BigEndian.Uint16(hello[p:])
		n := int(binary.BigEndian.Uint16(hello[p+2:]))
		if p+4+n > end {
			t.Fatalf("extension 0x%04x overruns the record", typ)
		}
		if typ == 0x0000 {
			body := hello[p+4 : p+4+n]
			if len(body) < 5 || body[2] != 0x00 {
				t.Fatalf("server_name body is malformed: % x", body)
			}
			l := int(binary.BigEndian.Uint16(body[3:5]))
			if 5+l != len(body) {
				t.Fatalf("server_name length %d in a %d-byte body", l, len(body))
			}
			name = string(body[5:])
		}
		p += 4 + n
	}
	if p != end {
		t.Fatalf("%d stray bytes after the last extension", end-p)
	}
	return name
}

func TestTheSecondBuiltInHelloIsTheVKHello(t *testing.T) {
	if len(FakeSNI2) != 517 {
		t.Errorf("FakeSNI2 is %d bytes, want 517", len(FakeSNI2))
	}
	sum := sha256.Sum256(FakeSNI2)
	if got, want := hex.EncodeToString(sum[:]), "d0d2c3eb90b3d5b6eea849adb1fd3d998c4a5f03b90739dd023319a32740f132"; got != want {
		t.Errorf("FakeSNI2 sha256 is %s, want %s", got, want)
	}
	if name := builtInHelloServerName(t, FakeSNI2); name != "vk.com" {
		t.Errorf("FakeSNI2 names %q, want vk.com", name)
	}
}
