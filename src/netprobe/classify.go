package netprobe

import "strings"

func ClassifyErrorString(raw string) string {
	lower := strings.ToLower(raw)

	dpiPatterns := []struct {
		pattern, desc string
	}{
		{"unexpected eof", "connection closed by DPI (unexpected EOF)"},
		{"eof occurred in violation", "connection closed by DPI (EOF violation)"},
		{"connection reset", "connection reset by DPI/firewall"},
		{"bad record mac", "TLS record corrupted by DPI"},
		{"decryption failed", "TLS decryption failed (DPI tampering)"},
		{"illegal parameter", "TLS blocked (DPI injection)"},
		{"decode error", "TLS blocked (DPI injection)"},
		{"record overflow", "TLS record overflow (DPI injection)"},
		{"unrecognized name", "blocked by SNI filtering"},
		{"handshake failure", "TLS handshake blocked by DPI"},
		{"close notify", "connection closed by DPI (alert injection)"},
		{"wrong version number", "non-TLS response received (DPI replacement)"},
	}
	for _, p := range dpiPatterns {
		if strings.Contains(lower, p.pattern) {
			return p.desc
		}
	}

	mitmPatterns := []struct {
		pattern, desc string
	}{
		{"self-signed", "fake certificate detected (possible MITM)"},
		{"self signed", "fake certificate detected (possible MITM)"},
		{"unknown authority", "unknown CA (possible MITM)"},
		{"certificate has expired", "expired certificate (possible MITM)"},
		{"x509", "certificate error (possible MITM)"},
	}
	for _, p := range mitmPatterns {
		if strings.Contains(lower, p.pattern) {
			return p.desc
		}
	}

	switch {
	case strings.Contains(lower, "context deadline exceeded") || strings.Contains(lower, "i/o timeout"):
		return "connection timed out (no response)"
	case strings.Contains(lower, "connection refused"):
		return "connection refused (port closed)"
	case strings.Contains(lower, "no route to host"):
		return "no route to host (IP unreachable)"
	case strings.Contains(lower, "network is unreachable"):
		return "network unreachable"
	case strings.Contains(lower, "eof"):
		return "connection closed unexpectedly"
	}

	return raw
}
