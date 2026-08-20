package quic

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/hex"
	"testing"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("decode %q: %v", s, err)
	}
	return b
}

func testVarint(v int) []byte {
	switch {
	case v < 64:
		return []byte{byte(v)}
	case v < 16384:
		return []byte{0x40 | byte(v>>8), byte(v)}
	default:
		return []byte{0x80, byte(v >> 16), byte(v >> 8), byte(v)}
	}
}

func testSealInitialDeclaring(t *testing.T, dcid, payload []byte, declaredLength int) []byte {
	t.Helper()

	secret := hkdfExtractSHA256(saltV1, dcid)
	client, err := hkdfExpandLabel(secret, "client in", secretSize)
	if err != nil {
		t.Fatalf("expand client in: %v", err)
	}
	key, err := hkdfExpandLabel(client, "quic key", keySize)
	if err != nil {
		t.Fatalf("expand key: %v", err)
	}
	iv, err := hkdfExpandLabel(client, "quic iv", ivSize)
	if err != nil {
		t.Fatalf("expand iv: %v", err)
	}
	hpkey, err := hkdfExpandLabel(client, "quic hp", keySize)
	if err != nil {
		t.Fatalf("expand hp: %v", err)
	}
	blk, err := aes.NewCipher(key)
	if err != nil {
		t.Fatalf("aes key: %v", err)
	}
	aead, err := cipher.NewGCM(blk)
	if err != nil {
		t.Fatalf("gcm: %v", err)
	}
	hpBlk, err := aes.NewCipher(hpkey)
	if err != nil {
		t.Fatalf("aes hp: %v", err)
	}

	hdr := []byte{0xC0, 0x00, 0x00, 0x00, 0x01, byte(len(dcid))}
	hdr = append(hdr, dcid...)
	hdr = append(hdr, 0x00, 0x00)
	hdr = append(hdr, testVarint(declaredLength)...)

	pnOff := len(hdr)
	pkt := append(hdr, 0x00)
	pkt = append(pkt, aead.Seal(nil, iv, payload, pkt)...)

	var mask [16]byte
	hpBlk.Encrypt(mask[:], pkt[pnOff+4:pnOff+20])
	pkt[0] ^= mask[0] & 0x0f
	pkt[pnOff] ^= mask[1]

	return pkt
}

func testSealInitial(t *testing.T, dcid, payload []byte) []byte {
	t.Helper()
	return testSealInitialDeclaring(t, dcid, payload, 1+len(payload)+16)
}

func testCryptoPayload(crypto []byte) []byte {
	payload := append([]byte{0x06}, testVarint(0)...)
	payload = append(payload, testVarint(len(crypto))...)
	payload = append(payload, crypto...)
	if len(payload) < 1000 {
		payload = append(payload, make([]byte, 1000-len(payload))...)
	}
	return payload
}

func testCoalescedHandshake(dcid []byte) []byte {
	p := []byte{0xE0, 0x00, 0x00, 0x00, 0x01, byte(len(dcid))}
	p = append(p, dcid...)
	p = append(p, 0x00)
	body := make([]byte, 64)
	for i := range body {
		body[i] = byte(i + 1)
	}
	p = append(p, testVarint(1+len(body))...)
	p = append(p, 0x00)
	return append(p, body...)
}

func testLongHeader(first byte, version uint32, dcid, scid []byte) []byte {
	b := []byte{first, byte(version >> 24), byte(version >> 16), byte(version >> 8), byte(version)}
	b = append(b, byte(len(dcid)))
	b = append(b, dcid...)
	b = append(b, byte(len(scid)))
	return append(b, scid...)
}

func TestDeriveInitialMatchesRFC9001AppendixA(t *testing.T) {
	dcid := mustHex(t, "8394c8f03e515708")

	secret := hkdfExtractSHA256(saltV1, dcid)
	if want := mustHex(t, "7db5df06e7a69e432496adedb00851923595221596ae2ae9fb8115c1e9ed0a44"); !bytes.Equal(secret, want) {
		t.Fatalf("initial_secret = %x, want %x", secret, want)
	}

	client, err := hkdfExpandLabel(secret, "client in", secretSize)
	if err != nil {
		t.Fatalf("expand client in: %v", err)
	}
	if want := mustHex(t, "c00cf151ca5be075ed0ebfb5c80323c42d6b7db67881289af4008f1f6c357aea"); !bytes.Equal(client, want) {
		t.Fatalf("client_initial_secret = %x, want %x", client, want)
	}

	wantKey := mustHex(t, "1f369613dd76d5467730efcbe3b1a22d")
	wantIV := mustHex(t, "fa044b2f42a3fd3b46fb255c")
	wantHP := mustHex(t, "9f50449e04a0e810283a1e9933adedd2")

	key, err := hkdfExpandLabel(client, "quic key", keySize)
	if err != nil {
		t.Fatalf("expand key: %v", err)
	}
	if !bytes.Equal(key, wantKey) {
		t.Errorf("client key = %x, want %x", key, wantKey)
	}
	iv, err := hkdfExpandLabel(client, "quic iv", ivSize)
	if err != nil {
		t.Fatalf("expand iv: %v", err)
	}
	if !bytes.Equal(iv, wantIV) {
		t.Errorf("client iv = %x, want %x", iv, wantIV)
	}
	hpkey, err := hkdfExpandLabel(client, "quic hp", keySize)
	if err != nil {
		t.Fatalf("expand hp: %v", err)
	}
	if !bytes.Equal(hpkey, wantHP) {
		t.Errorf("client hp = %x, want %x", hpkey, wantHP)
	}

	hp, aead, gotIV, err := deriveInitial(dcid, versionV1)
	if err != nil {
		t.Fatalf("deriveInitial: %v", err)
	}
	if !bytes.Equal(gotIV, wantIV) {
		t.Errorf("deriveInitial iv = %x, want %x", gotIV, wantIV)
	}

	sample := mustHex(t, "d1b1c98dd7689fb8ec11d242b123dc9b")
	wantHPBlock, err := aes.NewCipher(wantHP)
	if err != nil {
		t.Fatalf("aes hp vector: %v", err)
	}
	var gotMask, wantMask [16]byte
	hp.Encrypt(gotMask[:], sample)
	wantHPBlock.Encrypt(wantMask[:], sample)
	if gotMask != wantMask {
		t.Errorf("deriveInitial hp mask = %x, want %x", gotMask, wantMask)
	}

	wantBlock, err := aes.NewCipher(wantKey)
	if err != nil {
		t.Fatalf("aes key vector: %v", err)
	}
	wantAEAD, err := cipher.NewGCM(wantBlock)
	if err != nil {
		t.Fatalf("gcm vector: %v", err)
	}
	plain := []byte("rfc9001 appendix a")
	if got, want := aead.Seal(nil, wantIV, plain, nil), wantAEAD.Seal(nil, wantIV, plain, nil); !bytes.Equal(got, want) {
		t.Errorf("deriveInitial aead sealed %x, want %x", got, want)
	}
}

func TestDeriveInitialVersions(t *testing.T) {
	dcid := mustHex(t, "8394c8f03e515708")

	_, _, ivV1, err := deriveInitial(dcid, versionV1)
	if err != nil {
		t.Fatalf("deriveInitial v1: %v", err)
	}
	_, _, ivV2, err := deriveInitial(dcid, versionV2)
	if err != nil {
		t.Fatalf("deriveInitial v2: %v", err)
	}
	if bytes.Equal(ivV1, ivV2) {
		t.Error("v1 and v2 derived the same iv; the v2 salt/label is not being applied")
	}
	if _, _, _, err := deriveInitial(dcid, 0x0a0a0a0a); err == nil {
		t.Error("deriveInitial accepted an unknown version")
	}
}

func TestDecryptInitialRoundTrip(t *testing.T) {
	dcid := mustHex(t, "8394c8f03e515708")
	payload := testCryptoPayload([]byte("client hello"))
	pkt := testSealInitial(t, dcid, payload)

	plain, ok := DecryptInitial(dcid, pkt)
	if !ok {
		t.Fatal("DecryptInitial failed on a well formed Initial")
	}
	if !bytes.Equal(plain, payload) {
		t.Fatalf("plaintext mismatch: got %d bytes, want %d", len(plain), len(payload))
	}
	if plain[0] != 0x06 {
		t.Fatalf("first frame type = %#x, want 0x06 (CRYPTO)", plain[0])
	}
}

func TestDecryptInitialBoundsCiphertextByLengthField(t *testing.T) {
	dcid := mustHex(t, "8394c8f03e515709")
	payload := testCryptoPayload([]byte("padded outside the packet"))
	pkt := testSealInitial(t, dcid, payload)

	tests := []struct {
		name string
		tail []byte
	}{
		{name: "zero padding after the packet", tail: make([]byte, 235)},
		{name: "coalesced handshake packet", tail: testCoalescedHandshake(dcid)},
		{name: "single trailing byte", tail: []byte{0xFF}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			datagram := append(append([]byte(nil), pkt...), tc.tail...)
			plain, ok := DecryptInitial(dcid, datagram)
			if !ok {
				t.Fatalf("DecryptInitial failed on a datagram with %d trailing bytes", len(tc.tail))
			}
			if !bytes.Equal(plain, payload) {
				t.Fatalf("plaintext mismatch: got %d bytes, want %d", len(plain), len(payload))
			}
			if plain[0] != 0x06 {
				t.Fatalf("first frame type = %#x, want 0x06 (CRYPTO)", plain[0])
			}
		})
	}
}

func TestDecryptInitialFallsBackOnImplausibleLength(t *testing.T) {
	payload := testCryptoPayload([]byte("bogus length field"))

	tests := []struct {
		name     string
		dcid     []byte
		declared int
	}{
		{name: "length past the datagram", dcid: mustHex(t, "8394c8f03e51570a"), declared: 1 + len(payload) + 16 + 500},
		{name: "length shorter than the packet number", dcid: mustHex(t, "8394c8f03e51570b"), declared: 1},
		{name: "zero length", dcid: mustHex(t, "8394c8f03e51570c"), declared: 0},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pkt := testSealInitialDeclaring(t, tc.dcid, payload, tc.declared)
			plain, ok := DecryptInitial(tc.dcid, pkt)
			if !ok {
				t.Fatalf("DecryptInitial regressed on a packet declaring length %d", tc.declared)
			}
			if !bytes.Equal(plain, payload) {
				t.Fatalf("plaintext mismatch: got %d bytes, want %d", len(plain), len(payload))
			}
		})
	}
}

func TestDecryptInitialRejectsMalformed(t *testing.T) {
	dcid := mustHex(t, "8394c8f03e51570d")
	pkt := testSealInitial(t, dcid, testCryptoPayload([]byte("truncate me")))

	tests := []struct {
		name string
		pkt  []byte
	}{
		{name: "empty", pkt: nil},
		{name: "too short", pkt: pkt[:6]},
		{name: "short header", pkt: append([]byte{0x40}, pkt[1:]...)},
		{name: "truncated before the hp sample", pkt: pkt[:20]},
		{name: "truncated ciphertext", pkt: pkt[:len(pkt)-1]},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if _, ok := DecryptInitial(dcid, tc.pkt); ok {
				t.Error("DecryptInitial accepted a malformed packet")
			}
		})
	}

	if _, ok := DecryptInitial(mustHex(t, "0000000000000000"), pkt); ok {
		t.Error("DecryptInitial accepted a packet under the wrong DCID")
	}
}

func TestIsInitial(t *testing.T) {
	cid := []byte{1, 2, 3, 4, 5, 6, 7, 8}

	tests := []struct {
		name string
		pkt  []byte
		want bool
	}{
		{name: "v1 initial", pkt: testLongHeader(0xC0, versionV1, cid, nil), want: true},
		{name: "v1 zero rtt", pkt: testLongHeader(0xD0, versionV1, cid, nil), want: false},
		{name: "v1 handshake", pkt: testLongHeader(0xE0, versionV1, cid, nil), want: false},
		{name: "v1 retry", pkt: testLongHeader(0xF0, versionV1, cid, nil), want: false},
		{name: "v2 initial", pkt: testLongHeader(0xD0, versionV2, cid, nil), want: true},
		{name: "v2 retry", pkt: testLongHeader(0xC0, versionV2, cid, nil), want: false},
		{name: "v2 handshake", pkt: testLongHeader(0xF0, versionV2, cid, nil), want: false},
		{name: "unknown version", pkt: testLongHeader(0xC0, 0x0a0a0a0a, cid, nil), want: false},
		{name: "version negotiation", pkt: testLongHeader(0xC0, 0x00000000, cid, nil), want: false},
		{name: "short header", pkt: testLongHeader(0x40, versionV1, cid, nil), want: false},
		{name: "truncated", pkt: []byte{0xC0, 0x00, 0x00, 0x00, 0x01, 0x00}, want: false},
		{name: "empty", pkt: nil, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := IsInitial(tc.pkt); got != tc.want {
				t.Errorf("IsInitial = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestLooksLikeQUIC(t *testing.T) {
	cid := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	oversized := make([]byte, 21)

	tests := []struct {
		name string
		pkt  []byte
		want bool
	}{
		{name: "long header with both cids", pkt: testLongHeader(0xC0, versionV1, cid, cid), want: true},
		{name: "long header with empty cids", pkt: testLongHeader(0xC0, versionV1, nil, nil), want: true},
		{name: "short header", pkt: testLongHeader(0x40, versionV1, cid, cid), want: false},
		{name: "fixed bit clear", pkt: testLongHeader(0x80, versionV1, cid, cid), want: false},
		{name: "oversized dcid length", pkt: testLongHeader(0xC0, versionV1, oversized, cid), want: false},
		{name: "oversized scid length", pkt: testLongHeader(0xC0, versionV1, cid, oversized), want: false},
		{name: "truncated before scid length", pkt: testLongHeader(0xC0, versionV1, cid, nil)[:14], want: false},
		{name: "truncated scid", pkt: testLongHeader(0xC0, versionV1, cid, cid)[:18], want: false},
		{name: "too short", pkt: []byte{0xC0, 0x00, 0x00, 0x00, 0x01, 0x08}, want: false},
		{name: "empty", pkt: nil, want: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LooksLikeQUIC(tc.pkt); got != tc.want {
				t.Errorf("LooksLikeQUIC = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestParseDCID(t *testing.T) {
	cid := []byte{0xA0, 1, 2, 3, 4, 5, 6, 7}

	tests := []struct {
		name string
		pkt  []byte
		want []byte
	}{
		{name: "long header", pkt: testLongHeader(0xC0, versionV1, cid, nil), want: cid},
		{name: "v2 long header", pkt: testLongHeader(0xD0, versionV2, cid, nil), want: cid},
		{name: "empty dcid", pkt: testLongHeader(0xC0, versionV1, nil, nil), want: []byte{}},
		{name: "short header", pkt: testLongHeader(0x40, versionV1, cid, nil), want: nil},
		{name: "truncated dcid", pkt: testLongHeader(0xC0, versionV1, cid, nil)[:10], want: nil},
		{name: "oversized dcid length", pkt: []byte{0xC0, 0x00, 0x00, 0x00, 0x01, 0xFF, 1, 2}, want: nil},
		{name: "too short", pkt: []byte{0xC0, 0x00, 0x00, 0x00, 0x01, 0x00}, want: nil},
		{name: "empty", pkt: nil, want: nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseDCID(tc.pkt)
			if tc.want == nil {
				if got != nil {
					t.Errorf("ParseDCID = %x, want nil", got)
				}
				return
			}
			if !bytes.Equal(got, tc.want) {
				t.Errorf("ParseDCID = %x, want %x", got, tc.want)
			}
		})
	}
}

func TestParseDCIDMatchesDecryptableKey(t *testing.T) {
	dcid := mustHex(t, "8394c8f03e51570e")
	pkt := testSealInitial(t, dcid, testCryptoPayload([]byte("hello")))

	got := ParseDCID(pkt)
	if !bytes.Equal(got, dcid) {
		t.Fatalf("ParseDCID = %x, want %x", got, dcid)
	}
	if _, ok := DecryptInitial(got, pkt); !ok {
		t.Fatal("DecryptInitial failed with the DCID that ParseDCID returned")
	}
}
