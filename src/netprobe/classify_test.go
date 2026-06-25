package netprobe

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
)

func TestClassifyTLSError(t *testing.T) {
	cases := []struct {
		raw        string
		stage      TLSStage
		bytesRead  int
		wantStatus DomainStatus
	}{
		{"read: connection reset by peer", StageHandshake, 0, DomainTLSReset},
		{"tls: unrecognized name", StageHandshake, 0, DomainTLSAlert},
		{"remote error: tls: protocol version", StageHandshake, 0, DomainTLSAlert},
		{"tls: wrong version number", StageHandshake, 0, DomainTLSSpoof},
		{"x509: certificate signed by unknown authority", StageHandshake, 0, DomainTLSMITM},
		{"remote error: tls: certificate required", StageHandshake, 0, DomainMTLS},
		{"dial tcp: i/o timeout", StageConnect, 0, DomainSYNDrop},
		{"i/o timeout", StageRead, 20 * 1024, DomainTCP16},
		{"connection refused", StageHandshake, 0, DomainBlocked},
		{"dial tcp 1.2.3.4:443: connect: no route to host", StageConnect, 0, DomainError},
		{"dial tcp 1.2.3.4:443: connect: network is unreachable", StageConnect, 0, DomainError},
		{"no such host", StageHandshake, 0, DomainError},
	}
	for _, c := range cases {
		got, _ := ClassifyTLSErrorStaged(errors.New(c.raw), c.stage, c.bytesRead)
		if got != c.wantStatus {
			t.Errorf("ClassifyTLSErrorStaged(%q, stage=%d, n=%d) = %q, want %q", c.raw, c.stage, c.bytesRead, got, c.wantStatus)
		}
	}
	if s, _ := ClassifyTLSError(nil); s != DomainOk {
		t.Errorf("ClassifyTLSError(nil) = %q, want OK", s)
	}
	if s, _ := ClassifyTLSError(tls.AlertError(116)); s != DomainMTLS {
		t.Errorf("alert 116 (certificate required) should be MTLS, got %q", s)
	}
}

func TestClassifyTLSErrorTyped(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		stage      TLSStage
		bytesRead  int
		wantStatus DomainStatus
	}{
		{"dns not found", &net.DNSError{Err: "no such host", IsNotFound: true}, StageHandshake, 0, DomainError},
		{"dns timeout not tls drop", &net.DNSError{Err: "i/o timeout", IsTimeout: true}, StageHandshake, 0, DomainTimeout},
		{"conn refused", syscall.ECONNREFUSED, StageConnect, 0, DomainBlocked},
		{"net unreachable", syscall.ENETUNREACH, StageConnect, 0, DomainError},
		{"host unreachable", syscall.EHOSTUNREACH, StageConnect, 0, DomainError},
		{"conn reset handshake", syscall.ECONNRESET, StageHandshake, 0, DomainTLSReset},
		{"wrapped reset during transfer", &net.OpError{Op: "read", Err: syscall.ECONNRESET}, StageRead, 40 * 1024, DomainTLSReset},
		{"eof handshake", io.EOF, StageHandshake, 0, DomainTLSReset},
		{"deadline at connect is syn drop", context.DeadlineExceeded, StageConnect, 0, DomainSYNDrop},
		{"typed timeout still hits tcp16", context.DeadlineExceeded, StageRead, 20 * 1024, DomainTCP16},
	}
	for _, c := range cases {
		got, _ := ClassifyTLSErrorStaged(c.err, c.stage, c.bytesRead)
		if got != c.wantStatus {
			t.Errorf("%s: ClassifyTLSErrorStaged(%v, stage=%d, n=%d) = %q, want %q", c.name, c.err, c.stage, c.bytesRead, got, c.wantStatus)
		}
	}
}

func TestClassifyHTTPResponse(t *testing.T) {
	if s, _ := ClassifyHTTPResponse(451, "", ""); s != DomainISPPage {
		t.Errorf("HTTP 451 should be ISP_PAGE, got %q", s)
	}
	if s, _ := ClassifyHTTPResponse(302, "https://warning.rt.ru/blocked", ""); s != DomainISPPage {
		t.Errorf("block redirect should be ISP_PAGE, got %q", s)
	}
	if s, _ := ClassifyHTTPResponse(200, "", "Доступ заблокирован по решению суда"); s != DomainISPPage {
		t.Errorf("block body should be ISP_PAGE, got %q", s)
	}
	if s, _ := ClassifyHTTPResponse(200, "", "<html>normal page</html>"); s != DomainOk {
		t.Errorf("benign page should be OK, got %q", s)
	}
}

func TestDetectBlockPageBody(t *testing.T) {
	if DetectBlockPageBody([]byte("Доступ заблокирован по решению суда")) == "" {
		t.Error("expected block page detection on Russian RKN body")
	}
	if DetectBlockPageBody([]byte("<html>normal youtube page, nothing blocked here</html>")) != "" {
		t.Error("benign body with the word 'blocked' must not trip body detection")
	}
	if DetectBlockPageBody(nil) != "" {
		t.Error("empty body must return no detection")
	}
}

func TestIsBlockPageRedirect(t *testing.T) {
	if !IsBlockPageRedirect("https://warning.rt.ru/blocked") {
		t.Error("expected redirect block detection")
	}
	if IsBlockPageRedirect("https://accounts.google.com/login") {
		t.Error("benign redirect must not trip detection")
	}
}
