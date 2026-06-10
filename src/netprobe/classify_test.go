package netprobe

import "testing"

func TestClassifyErrorString(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{"read: connection reset by peer", "connection reset by DPI/firewall"},
		{"tls: unrecognized name", "blocked by SNI filtering"},
		{"x509: certificate signed by unknown authority", "unknown CA (possible MITM)"},
		{"context deadline exceeded", "connection timed out (no response)"},
		{"dial tcp: connection refused", "connection refused (port closed)"},
		{"some unmapped error", "some unmapped error"},
	}
	for _, c := range cases {
		if got := ClassifyErrorString(c.raw); got != c.want {
			t.Errorf("ClassifyErrorString(%q) = %q, want %q", c.raw, got, c.want)
		}
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
