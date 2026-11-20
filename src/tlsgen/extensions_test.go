package tlsgen_test

import (
	"crypto/ecdh"
	"encoding/binary"
	"testing"

	"github.com/daniellavrushin/b4/tlsgen"
)

type helloExtension struct {
	typ  uint16
	body []byte
}

func generatedExtensions(t *testing.T, name string) ([]byte, []helloExtension) {
	t.Helper()
	hello, err := tlsgen.GenerateTLSClientHello(name)
	if err != nil {
		t.Fatal(err)
	}
	if len(hello) < 44 || hello[0] != 0x16 || hello[5] != 0x01 {
		t.Fatalf("not a TLS ClientHello record: % x", hello[:min(len(hello), 12)])
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
	var out []helloExtension
	for p+4 <= end {
		typ := binary.BigEndian.Uint16(hello[p:])
		n := int(binary.BigEndian.Uint16(hello[p+2:]))
		if p+4+n > end {
			t.Fatalf("extension 0x%04x claims %d bytes, %d remain", typ, n, end-p-4)
		}
		out = append(out, helloExtension{typ: typ, body: hello[p+4 : p+4+n]})
		p += 4 + n
	}
	if p != end {
		t.Fatalf("%d stray bytes after the last extension", end-p)
	}
	return hello, out
}

func extensionBody(t *testing.T, exts []helloExtension, typ uint16) []byte {
	t.Helper()
	for _, e := range exts {
		if e.typ == typ {
			return e.body
		}
	}
	t.Fatalf("the generated hello has no extension 0x%04x", typ)
	return nil
}

func TestTheGeneratedDelegatedCredentialsListCoversItsBody(t *testing.T) {
	_, exts := generatedExtensions(t, "www.google.com")
	body := extensionBody(t, exts, 0x0022)
	if len(body) < 2 {
		t.Fatalf("delegated_credentials body is %d bytes", len(body))
	}
	inner := int(binary.BigEndian.Uint16(body))
	if 2+inner != len(body) || inner%2 != 0 {
		t.Fatalf("delegated_credentials 0x0022: list length %d, %d bytes follow it: % x", inner, len(body)-2, body)
	}
}

func TestTheGeneratedECHIsAnOuterClientHelloOfFixedLength(t *testing.T) {
	_, exts := generatedExtensions(t, "www.google.com")
	body := extensionBody(t, exts, 0xfe0d)
	if len(body) != 281 {
		t.Fatalf("ECH extension is %d bytes, want 281", len(body))
	}
	if body[0] != 0x00 {
		t.Fatalf("ECHClientHelloType is 0x%02x, want 0x00 (outer); body starts % x", body[0], body[:12])
	}
	if kdf, aead := binary.BigEndian.Uint16(body[1:]), binary.BigEndian.Uint16(body[3:]); kdf != 0x0001 || aead != 0x0001 {
		t.Fatalf("ECH cipher suite is KDF 0x%04x AEAD 0x%04x, want 0x0001 0x0001", kdf, aead)
	}
	p := 1 + 2 + 2 + 1
	encLen := int(binary.BigEndian.Uint16(body[p:]))
	if encLen != 32 {
		t.Fatalf("ECH enc is %d bytes, want 32", encLen)
	}
	p += 2 + encLen
	if p+2 > len(body) {
		t.Fatalf("ECH ends inside its enc field")
	}
	payloadLen := int(binary.BigEndian.Uint16(body[p:]))
	p += 2 + payloadLen
	if p != len(body) {
		t.Fatalf("ECH outer fields cover %d bytes of a %d-byte extension", p, len(body))
	}
}

func TestTheGeneratedP256KeyShareIsACurvePoint(t *testing.T) {
	_, exts := generatedExtensions(t, "www.google.com")
	body := extensionBody(t, exts, 0x0033)
	if len(body) < 2 || 2+int(binary.BigEndian.Uint16(body)) != len(body) {
		t.Fatalf("key_share list length does not cover its body: % x", body[:min(len(body), 8)])
	}
	p := 2
	for p+4 <= len(body) {
		group := binary.BigEndian.Uint16(body[p:])
		n := int(binary.BigEndian.Uint16(body[p+2:]))
		key := body[p+4 : p+4+n]
		p += 4 + n
		if group != 0x0017 {
			continue
		}
		if _, err := ecdh.P256().NewPublicKey(key); err != nil {
			t.Fatalf("secp256r1 key_share is not a P-256 point (first byte 0x%02x): %v", key[0], err)
		}
		return
	}
	t.Fatal("the generated hello has no secp256r1 key share")
}

func TestGeneratedHellosDifferInTheirKeyShares(t *testing.T) {
	_, a := generatedExtensions(t, "www.google.com")
	_, b := generatedExtensions(t, "www.google.com")
	if string(extensionBody(t, a, 0x0033)) == string(extensionBody(t, b, 0x0033)) {
		t.Fatal("two generated hellos carry the same key shares")
	}
}

func TestTheGeneratedHelloKeepsItsLength(t *testing.T) {
	for _, name := range []string{"a.ru", "max.ru", "www.google.com", "verylongdomain.example"} {
		hello, exts := generatedExtensions(t, name)
		if want := 648 + len(name); len(hello) != want {
			t.Errorf("%s: generated hello is %d bytes, want %d", name, len(hello), want)
		}
		if got := int(binary.BigEndian.Uint16(hello[3:5])); got != len(hello)-5 {
			t.Errorf("%s: record length %d, %d bytes follow the header", name, got, len(hello)-5)
		}
		if got := int(hello[6])<<16 | int(hello[7])<<8 | int(hello[8]); got != len(hello)-9 {
			t.Errorf("%s: handshake length %d, %d bytes follow it", name, got, len(hello)-9)
		}
		if exts[0].typ != 0x0000 {
			t.Errorf("%s: first extension is 0x%04x, want server_name", name, exts[0].typ)
		}
	}
}
