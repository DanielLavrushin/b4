package sni

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"io"
	"strings"
	"testing"

	"golang.org/x/crypto/hkdf"
)

var quicSaltV1 = []byte{0x38, 0x76, 0x2c, 0xf7, 0xf5, 0x59, 0x34, 0xb3, 0x4d, 0x17, 0x9a, 0xe6, 0xa4, 0xc8, 0x0c, 0xad, 0xcc, 0xbb, 0x7f, 0x0a}

func qu16(v int) []byte {
	b := make([]byte, 2)
	binary.BigEndian.PutUint16(b, uint16(v))
	return b
}

func qvarint(v int) []byte {
	switch {
	case v < 64:
		return []byte{byte(v)}
	case v < 16384:
		return []byte{0x40 | byte(v>>8), byte(v)}
	default:
		return []byte{0x80, byte(v >> 16), byte(v >> 8), byte(v)}
	}
}

func qExpandLabel(t *testing.T, secret []byte, label string, n int) []byte {
	t.Helper()
	full := "tls13 " + label
	info := make([]byte, 2+1+len(full)+1)
	info[0] = byte(n >> 8)
	info[1] = byte(n)
	info[2] = byte(len(full))
	copy(info[3:], full)
	out := make([]byte, n)
	if _, err := io.ReadFull(hkdf.Expand(sha256.New, secret, info), out); err != nil {
		t.Fatalf("hkdf expand %q: %v", label, err)
	}
	return out
}

func qBuildClientHello(serverName []byte) []byte {
	entry := append([]byte{0x00}, qu16(len(serverName))...)
	entry = append(entry, serverName...)

	list := append(qu16(len(entry)), entry...)
	exts := append(qu16(0), qu16(len(list))...)
	exts = append(exts, list...)

	ch := []byte{0x03, 0x03}
	ch = append(ch, make([]byte, 32)...)
	ch = append(ch, 0x00)
	ch = append(ch, qu16(2)...)
	ch = append(ch, 0x13, 0x01)
	ch = append(ch, 0x01, 0x00)
	ch = append(ch, qu16(len(exts))...)
	ch = append(ch, exts...)

	return append([]byte{0x01, byte(len(ch) >> 16), byte(len(ch) >> 8), byte(len(ch))}, ch...)
}

func qBuildInitial(t *testing.T, dcid, crypto []byte) []byte {
	t.Helper()
	return qBuildInitialAt(t, dcid, 0, crypto)
}

func qBuildInitialAt(t *testing.T, dcid []byte, cryptoOff int, crypto []byte) []byte {
	t.Helper()

	payload := append([]byte{0x06}, qvarint(cryptoOff)...)
	payload = append(payload, qvarint(len(crypto))...)
	payload = append(payload, crypto...)
	if len(payload) < 1000 {
		payload = append(payload, make([]byte, 1000-len(payload))...)
	}

	m := hmac.New(sha256.New, quicSaltV1)
	_, _ = m.Write(dcid)
	client := qExpandLabel(t, m.Sum(nil), "client in", 32)

	blk, err := aes.NewCipher(qExpandLabel(t, client, "quic key", 16))
	if err != nil {
		t.Fatalf("aes key: %v", err)
	}
	aead, err := cipher.NewGCM(blk)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	hpBlk, err := aes.NewCipher(qExpandLabel(t, client, "quic hp", 16))
	if err != nil {
		t.Fatalf("aes hp: %v", err)
	}
	iv := qExpandLabel(t, client, "quic iv", 12)

	hdr := []byte{0xC0, 0x00, 0x00, 0x00, 0x01, byte(len(dcid))}
	hdr = append(hdr, dcid...)
	hdr = append(hdr, 0x00, 0x00)
	hdr = append(hdr, qvarint(1+len(payload)+16)...)

	pnOff := len(hdr)
	pkt := append(hdr, 0x00)
	pkt = append(pkt, aead.Seal(nil, iv, payload, pkt)...)

	var mask [16]byte
	hpBlk.Encrypt(mask[:], pkt[pnOff+4:pnOff+20])
	pkt[0] ^= mask[0] & 0x0f
	pkt[pnOff] ^= mask[1]

	return pkt
}

func TestParseQUICClientHelloSNIAcceptsRealName(t *testing.T) {
	pkt := qBuildInitial(t, []byte{0xA0, 1, 2, 3, 4, 5, 6, 7}, qBuildClientHello([]byte("graph.whatsapp.com")))
	got, ok := ParseQUICClientHelloSNI(pkt)
	if !ok || got != "graph.whatsapp.com" {
		t.Fatalf("ParseQUICClientHelloSNI = %q, %v; want graph.whatsapp.com, true", got, ok)
	}
}

func TestParseQUICClientHelloSNIAcceptsMaxLengthName(t *testing.T) {
	longest := strings.Repeat("a", MaxSNINameLen-4) + ".com"
	pkt := qBuildInitial(t, []byte{0xC0, 1, 2, 3, 4, 5, 6, 7}, qBuildClientHello([]byte(longest)))
	got, ok := ParseQUICClientHelloSNI(pkt)
	if !ok || got != longest {
		t.Fatalf("a %d byte hostname should be accepted: ok=%v len=%d", len(longest), ok, len(got))
	}
}

func TestParseQUICClientHelloSNIRejectsHostileName(t *testing.T) {
	tests := []struct {
		name string
		dcid []byte
		sni  []byte
	}{
		{
			name: "newline forges a log line",
			dcid: []byte{0xB0, 1, 2, 3, 4, 5, 6, 7},
			sni:  []byte("evil.com\n2026/08/17 00:00:00.000000 [INFO] forged.example.com"),
		},
		{
			name: "nul byte",
			dcid: []byte{0xB1, 1, 2, 3, 4, 5, 6, 7},
			sni:  []byte("evil.com\x00tail"),
		},
		{
			name: "carriage return",
			dcid: []byte{0xB2, 1, 2, 3, 4, 5, 6, 7},
			sni:  []byte("evil.com\rtail"),
		},
		{
			name: "no dot",
			dcid: []byte{0xB3, 1, 2, 3, 4, 5, 6, 7},
			sni:  []byte("notahostname"),
		},
		{
			name: "over max length",
			dcid: []byte{0xB4, 1, 2, 3, 4, 5, 6, 7},
			sni:  []byte(strings.Repeat("a", MaxSNINameLen-3) + ".com"),
		},
		{
			name: "space",
			dcid: []byte{0xB5, 1, 2, 3, 4, 5, 6, 7},
			sni:  []byte("evil.com and more"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkt := qBuildInitial(t, tc.dcid, qBuildClientHello(tc.sni))
			if got, ok := ParseQUICClientHelloSNI(pkt); ok {
				t.Errorf("accepted hostile SNI %q (%d bytes)", got, len(got))
			}
		})
	}
}

func TestParseQUICClientHelloSNIAssemblesFragmentedHello(t *testing.T) {
	host := "fragmented.example.com"
	crypto := qBuildClientHello([]byte(host))
	split := len(crypto) / 2
	dcid := []byte{0xE0, 1, 2, 3, 4, 5, 6, 7}

	if got, ok := ParseQUICClientHelloSNI(qBuildInitialAt(t, dcid, 0, crypto[:split])); ok {
		t.Fatalf("first fragment alone should not yield an SNI, got %q", got)
	}

	got, ok := ParseQUICClientHelloSNI(qBuildInitialAt(t, dcid, split, crypto[split:]))
	if !ok || got != host {
		t.Fatalf("second fragment should complete the hello: got (%q,%v), want (%q,true)", got, ok, host)
	}
}

func TestParseQUICClientHelloSNIAssemblesOutOfOrderFragments(t *testing.T) {
	host := "outoforder.example.com"
	crypto := qBuildClientHello([]byte(host))
	split := len(crypto) / 3
	dcid := []byte{0xE1, 1, 2, 3, 4, 5, 6, 7}

	if _, ok := ParseQUICClientHelloSNI(qBuildInitialAt(t, dcid, split, crypto[split:])); ok {
		t.Fatal("trailing fragment alone should not yield an SNI")
	}

	got, ok := ParseQUICClientHelloSNI(qBuildInitialAt(t, dcid, 0, crypto[:split]))
	if !ok || got != host {
		t.Fatalf("late head fragment should complete the hello: got (%q,%v), want (%q,true)", got, ok, host)
	}
}

func TestSNIExtractorsCapNameBeforeCopying(t *testing.T) {
	oversized := strings.Repeat("a", MaxSNINameLen-3) + ".com"
	atLimit := strings.Repeat("a", MaxSNINameLen-4) + ".com"

	if len(oversized) <= MaxSNINameLen || len(atLimit) != MaxSNINameLen {
		t.Fatalf("fixture lengths wrong: oversized=%d atLimit=%d", len(oversized), len(atLimit))
	}

	if got, err := extractSNIFromQUIC(qBuildClientHello([]byte(oversized))); err == nil || got != nil {
		t.Errorf("QUIC extractor returned %d bytes for an oversized name, want a rejection", len(got))
	}
	if got, err := extractSNIFromQUIC(qBuildClientHello([]byte(atLimit))); err != nil || string(got) != atLimit {
		t.Errorf("QUIC extractor rejected a name at the limit: err=%v len=%d", err, len(got))
	}

	if got := extractSNIFromExtension(sniExtensionData(oversized)); got != "" {
		t.Errorf("TLS extractor returned %d bytes for an oversized name, want empty", len(got))
	}
	if got := extractSNIFromExtension(sniExtensionData(atLimit)); got != atLimit {
		t.Errorf("TLS extractor rejected a name at the limit: got %d bytes", len(got))
	}
}

func sniExtensionData(host string) []byte {
	entry := append([]byte{0x00}, qu16(len(host))...)
	entry = append(entry, host...)
	return append(qu16(len(entry)), entry...)
}

func TestParseTLSClientHelloSNIRejectsOversizedName(t *testing.T) {
	oversized := strings.Repeat("a", MaxSNINameLen-3) + ".com"
	if got, _, ok := ParseTLSClientHelloSNI(tlsRecordWithSNI(oversized)); ok {
		t.Errorf("accepted oversized TLS SNI of %d bytes", len(got))
	}
}
