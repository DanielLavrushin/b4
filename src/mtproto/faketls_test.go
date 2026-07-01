package mtproto

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net"
	"testing"
	"time"
)

func newFakeSecretWithKey(t *testing.T, keyHex string) *Secret {
	hostHex := hex.EncodeToString([]byte("storage.googleapis.com"))
	sec, err := ParseSecret("ee" + keyHex + hostHex)
	if err != nil {
		t.Fatalf("ParseSecret: %v", err)
	}
	return sec
}

// makeValidClientHello crafts a ClientHello whose client-random field carries
// the HMAC a real fake-TLS client would send for the given secret.
func makeValidClientHello(sec *Secret) []byte {
	const bodyLen = 39
	body := make([]byte, bodyLen)
	body[0] = 0x01
	body[1] = byte((bodyLen - 4) >> 16)
	body[2] = byte((bodyLen - 4) >> 8)
	body[3] = byte(bodyLen - 4)
	body[4] = 0x03
	body[5] = 0x03
	body[38] = 0 // session id length
	hdr := []byte{0x16, 0x03, 0x01, byte(bodyLen >> 8), byte(bodyLen)}
	clientHello := append(hdr, body...)

	zeroed := make([]byte, len(clientHello))
	copy(zeroed, clientHello)
	mac := hmac.New(sha256.New, sec.Key[:])
	mac.Write(zeroed)
	expected := mac.Sum(nil)

	random := make([]byte, 32)
	copy(random, expected[:28])
	tsBytes := make([]byte, 4)
	binary.LittleEndian.PutUint32(tsBytes, uint32(time.Now().Unix()))
	for i := 0; i < 4; i++ {
		random[28+i] = expected[28+i] ^ tsBytes[i]
	}
	copy(clientHello[11:43], random)
	return clientHello
}

func TestAcceptFakeTLSMulti_SelectsMatchingSecret(t *testing.T) {
	sec1 := newFakeSecretWithKey(t, "0123456789abcdef0123456789abcdef")
	sec2 := newFakeSecretWithKey(t, "ffffffffffffffffffffffffffffffff")
	sec2.ID = "user-2"
	sec2.Name = "Ivan"

	hello := makeValidClientHello(sec2)

	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(hello)
		buf := make([]byte, 4096)
		_, _ = cli.Read(buf) // consume ServerHello so the server's Write unblocks
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	conn, matched, err := AcceptFakeTLSMulti(srv, []*Secret{sec1, sec2})
	if err != nil {
		t.Fatalf("AcceptFakeTLSMulti: %v", err)
	}
	if conn == nil {
		t.Fatalf("expected a connection")
	}
	if matched != sec2 {
		t.Fatalf("expected sec2 to match, got %v", matched)
	}
	if matched.Label() != "Ivan" {
		t.Fatalf("expected label Ivan, got %q", matched.Label())
	}
}

func TestAcceptFakeTLSMulti_NoMatchReturnsVerifyError(t *testing.T) {
	sec1 := newFakeSecretWithKey(t, "0123456789abcdef0123456789abcdef")
	other := newFakeSecretWithKey(t, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
	hello := makeValidClientHello(other)

	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(hello)
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	_, _, err := AcceptFakeTLSMulti(srv, []*Secret{sec1})
	if err == nil {
		t.Fatalf("expected error when no secret matches")
	}
	var vErr *FakeTLSVerifyError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected FakeTLSVerifyError for masking fallback, got %T: %v", err, err)
	}
}

func makeBogusClientHello(bodyLen int) []byte {
	hdr := []byte{0x16, 0x03, 0x01, byte(bodyLen >> 8), byte(bodyLen)}
	body := make([]byte, bodyLen)
	body[0] = 0x01
	body[1] = byte((bodyLen - 4) >> 16)
	body[2] = byte((bodyLen - 4) >> 8)
	body[3] = byte(bodyLen - 4)
	body[4] = 0x03
	body[5] = 0x03
	for i := 6; i < 38 && i < bodyLen; i++ {
		body[i] = byte(i)
	}
	return append(hdr, body...)
}

func newFakeSecret(t *testing.T) *Secret {
	keyHex := "0123456789abcdef0123456789abcdef"
	hostHex := hex.EncodeToString([]byte("storage.googleapis.com"))
	sec, err := ParseSecret("ee" + keyHex + hostHex)
	if err != nil {
		t.Fatalf("ParseSecret: %v", err)
	}
	return sec
}

func TestAcceptFakeTLS_HMACFail_ReturnsVerifyErrorWithInitial(t *testing.T) {
	hello := makeBogusClientHello(64)
	sec := newFakeSecret(t)

	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(hello)
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	_, err := AcceptFakeTLS(srv, sec)
	if err == nil {
		t.Fatalf("expected error on bogus ClientHello")
	}
	var vErr *FakeTLSVerifyError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected FakeTLSVerifyError, got %T: %v", err, err)
	}
	if !bytes.Equal(vErr.Initial, hello) {
		t.Fatalf("Initial bytes mismatch: got %d bytes, want %d (hello)", len(vErr.Initial), len(hello))
	}
}

func TestAcceptFakeTLS_NotClientHello_ReturnsVerifyErrorWithInitial(t *testing.T) {
	hello := makeBogusClientHello(64)
	hello[5] = 0x02

	sec := newFakeSecret(t)
	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(hello)
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	_, err := AcceptFakeTLS(srv, sec)
	if err == nil {
		t.Fatalf("expected error when body[0] != ClientHello")
	}
	var vErr *FakeTLSVerifyError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected FakeTLSVerifyError, got %T: %v", err, err)
	}
	if !bytes.Equal(vErr.Initial, hello) {
		t.Fatalf("Initial bytes must contain full read buffer for masking-fallback replay")
	}
}

func TestAcceptFakeTLS_BadRecordLength_ReturnsVerifyErrorWithHeader(t *testing.T) {
	hdr := []byte{0x16, 0x03, 0x01, 0x00, 0x10}

	sec := newFakeSecret(t)
	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(hdr)
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	_, err := AcceptFakeTLS(srv, sec)
	if err == nil {
		t.Fatalf("expected error on too-short record length")
	}
	var vErr *FakeTLSVerifyError
	if !errors.As(err, &vErr) {
		t.Fatalf("expected FakeTLSVerifyError, got %T: %v", err, err)
	}
	if !bytes.Equal(vErr.Initial, hdr) {
		t.Fatalf("Initial bytes should contain the record header, got %d bytes", len(vErr.Initial))
	}
}

func TestAcceptFakeTLS_NonTLSFirstByte_NoVerifyError(t *testing.T) {
	garbage := []byte{0xFF, 0x00, 0x00, 0x00, 0x00}

	sec := newFakeSecret(t)
	srv, cli := net.Pipe()
	go func() {
		cli.SetDeadline(time.Now().Add(2 * time.Second))
		_, _ = cli.Write(garbage)
		_ = cli.Close()
	}()

	srv.SetDeadline(time.Now().Add(2 * time.Second))
	_, err := AcceptFakeTLS(srv, sec)
	if err == nil {
		t.Fatalf("expected error on non-TLS first byte")
	}
	var vErr *FakeTLSVerifyError
	if errors.As(err, &vErr) {
		t.Fatalf("non-TLS first byte should NOT trigger masking-fallback (no FakeTLSVerifyError)")
	}
}
