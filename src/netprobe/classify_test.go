package netprobe

import (
	"errors"
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
