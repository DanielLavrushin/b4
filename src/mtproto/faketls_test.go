package mtproto

import (
	"bytes"
	"encoding/hex"
	"errors"
	"net"
	"testing"
	"time"
)

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
