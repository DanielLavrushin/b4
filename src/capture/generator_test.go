package capture

import (
	"encoding/hex"
	"testing"
)

func TestGenerateTLSClientHello(t *testing.T) {
	domain := "max.ru"
	payload, err := GenerateTLSClientHello(domain)

	if err != nil {
		t.Fatalf("Failed to generate ClientHello: %v", err)
	}

	// Basic structure checks
	if len(payload) < 100 {
		t.Fatalf("Payload too small: %d bytes", len(payload))
	}

	// Check TLS Record header
	if payload[0] != 0x16 {
		t.Errorf("Expected TLS Handshake record type 0x16, got 0x%02x", payload[0])
	}

	if payload[1] != 0x03 || payload[2] != 0x01 {
		t.Errorf("Expected TLS 1.0 record version 0x0301, got 0x%02x%02x", payload[1], payload[2])
	}

	// Check Handshake type
	if payload[5] != 0x01 {
		t.Errorf("Expected ClientHello handshake type 0x01, got 0x%02x", payload[5])
	}

	// Check ClientHello version
	if payload[9] != 0x03 || payload[10] != 0x03 {
		t.Errorf("Expected ClientHello version 0x0303 (TLS 1.2), got 0x%02x%02x", payload[9], payload[10])
	}

	t.Logf("✓ Generated payload: %d bytes", len(payload))
	t.Logf("✓ First 64 bytes: %s", hex.EncodeToString(payload[:64]))
}

func TestSNIFirstExtension(t *testing.T) {
	domain := "max.ru"
	payload, err := GenerateTLSClientHello(domain)

	if err != nil {
		t.Fatalf("Failed to generate ClientHello: %v", err)
	}

	// Find extensions
	// Skip: Record(5) + Handshake(4) + Version(2) + Random(32) + SessionID(1+32) + Ciphers(2+34) + Compression(2)
	offset := 5 + 4 + 2 + 32
	sessionIDLen := int(payload[offset])
	offset += 1 + sessionIDLen

	cipherLen := int(payload[offset])<<8 | int(payload[offset+1])
	offset += 2 + cipherLen

	compressionLen := int(payload[offset])
	offset += 1 + compressionLen

	// Extensions start here
	if offset+4 > len(payload) {
		t.Fatalf("Invalid payload structure")
	}

	extensionsLen := int(payload[offset])<<8 | int(payload[offset+1])
	offset += 2

	if extensionsLen == 0 {
		t.Fatalf("No extensions found")
	}

	// First extension should be SNI (0x0000)
	firstExtType := int(payload[offset])<<8 | int(payload[offset+1])
	firstExtLen := int(payload[offset+2])<<8 | int(payload[offset+3])

	if firstExtType != 0x0000 {
		t.Errorf("CRITICAL: First extension is NOT SNI! Got 0x%04x instead of 0x0000", firstExtType)
		t.Errorf("This payload will NOT bypass Russian DPI!")
	} else {
		t.Logf("✓ CRITICAL CHECK PASSED: SNI (0x0000) is the FIRST extension")
		t.Logf("✓ This payload structure matches working DPI bypass pattern")
	}

	// Verify SNI contains the domain
	if firstExtLen > 0 {
		sniData := payload[offset+4 : offset+4+firstExtLen]
		domainBytes := []byte(domain)
		found := false

		for i := 0; i <= len(sniData)-len(domainBytes); i++ {
			if string(sniData[i:i+len(domainBytes)]) == domain {
				found = true
				break
			}
		}

		if !found {
			t.Errorf("Domain '%s' not found in SNI extension data", domain)
		} else {
			t.Logf("✓ Domain '%s' found in SNI extension", domain)
		}
	}

	// Check next few extensions to verify order
	offset += 4 + firstExtLen

	expectedOrder := []uint16{
		0x0000, // SNI
		0x0017, // extended_master_secret
		0xff01, // renegotiation_info
		0x000a, // supported_groups
		0x000b, // ec_point_formats
	}

	offset = 5 + 4 + 2 + 32
	offset += 1 + sessionIDLen
	offset += 2 + cipherLen
	offset += 1 + compressionLen
	offset += 2 // skip extensions length

	t.Logf("\nExtension order (first 5):")
	for i := 0; i < 5 && offset+4 <= len(payload); i++ {
		extType := int(payload[offset])<<8 | int(payload[offset+1])
		extLen := int(payload[offset+2])<<8 | int(payload[offset+3])

		extName := map[int]string{
			0x0000: "SNI",
			0x0017: "extended_master_secret",
			0xff01: "renegotiation_info",
			0x000a: "supported_groups",
			0x000b: "ec_point_formats",
		}[extType]

		if extName == "" {
			extName = "unknown"
		}

		t.Logf("  %d. 0x%04x (%s): %d bytes", i+1, extType, extName, extLen)

		if uint16(extType) != expectedOrder[i] {
			t.Errorf("Extension %d: expected 0x%04x, got 0x%04x", i+1, expectedOrder[i], extType)
		}

		offset += 4 + extLen
	}
}

func TestCipherSuiteOrder(t *testing.T) {
	domain := "test.com"
	payload, err := GenerateTLSClientHello(domain)

	if err != nil {
		t.Fatalf("Failed to generate ClientHello: %v", err)
	}

	// Find cipher suites
	offset := 5 + 4 + 2 + 32
	sessionIDLen := int(payload[offset])
	offset += 1 + sessionIDLen

	cipherLen := int(payload[offset])<<8 | int(payload[offset+1])
	offset += 2

	// First 3 cipher suites should be TLS 1.3
	expectedFirst3 := []uint16{0x1301, 0x1303, 0x1302}

	t.Logf("\nCipher Suites (first 3):")
	for i := 0; i < 3; i++ {
		cs := uint16(payload[offset+i*2])<<8 | uint16(payload[offset+i*2+1])
		t.Logf("  %d. 0x%04x", i+1, cs)

		if cs != expectedFirst3[i] {
			t.Errorf("Cipher suite %d: expected 0x%04x, got 0x%04x", i+1, expectedFirst3[i], cs)
		}
	}

	totalCiphers := cipherLen / 2
	t.Logf("✓ Total cipher suites: %d", totalCiphers)

	if totalCiphers != 17 {
		t.Errorf("Expected 17 cipher suites, got %d", totalCiphers)
	}
}

func TestMultipleDomains(t *testing.T) {
	domains := []string{"max.ru", "google.com", "example.org", "test.local"}

	for _, domain := range domains {
		t.Run(domain, func(t *testing.T) {
			payload, err := GenerateTLSClientHello(domain)
			if err != nil {
				t.Errorf("Failed to generate for %s: %v", domain, err)
				return
			}

			// Verify domain is in payload
			if len(payload) < 100 {
				t.Errorf("Payload too small for %s", domain)
				return
			}

			// Check SNI is first
			offset := 5 + 4 + 2 + 32
			sessionIDLen := int(payload[offset])
			offset += 1 + sessionIDLen
			cipherLen := int(payload[offset])<<8 | int(payload[offset+1])
			offset += 2 + cipherLen
			compressionLen := int(payload[offset])
			offset += 1 + compressionLen
			offset += 2 // extensions length

			firstExtType := int(payload[offset])<<8 | int(payload[offset+1])
			if firstExtType != 0x0000 {
				t.Errorf("Domain %s: SNI not first! Got 0x%04x", domain, firstExtType)
			} else {
				t.Logf("✓ Domain %s: SNI is first extension", domain)
			}
		})
	}
}
