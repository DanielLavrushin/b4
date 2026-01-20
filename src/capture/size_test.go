package capture

import (
	"testing"
)

func TestPayloadSizeVariation(t *testing.T) {
	domains := []string{
		"a.ru",                   // 4 chars
		"max.ru",                 // 6 chars
		"hp.com",                 // 6 chars
		"vk.com",                 // 6 chars
		"google.com",             // 10 chars
		"example.org",            // 11 chars
		"verylongdomain.example", // 22 chars
	}

	t.Logf("\n=== Payload Size Analysis ===\n")

	for _, domain := range domains {
		payload, err := GenerateTLSClientHello(domain)
		if err != nil {
			t.Errorf("Failed to generate for %s: %v", domain, err)
			continue
		}

		sniExtLen := len(domain) + 5 // +5 for SNI structure overhead
		expectedTotal := 643 + sniExtLen

		match := "✅"
		if len(payload) != expectedTotal {
			match = "⚠️"
		}

		t.Logf("%-25s: %3d bytes (domain: %2d chars, SNI ext: %2d bytes) %s",
			domain, len(payload), len(domain), sniExtLen, match)
	}

}
