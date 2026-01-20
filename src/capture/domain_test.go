package capture

import (
	"testing"
)

func TestNonExistentDomains(t *testing.T) {
	domains := []string{
		"real-site.com",                 // Real domain
		"does-not-exist-12345.com",      // Fake domain
		"test-dpi-bypass.ru",            // Non-existent .ru
		"completely-made-up-name.local", // Local fake
		"🦄unicorn-domain.com",           // Even with emoji!
	}

	t.Logf("\n=== Testing Domain Generation ===\n")

	for _, domain := range domains {
		payload, err := GenerateTLSClientHello(domain)

		status := "✅ Generated"
		if err != nil {
			status = "❌ Failed: " + err.Error()
		}

		t.Logf("%-40s: %s (%d bytes)", domain, status, len(payload))
	}

}
