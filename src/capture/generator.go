package capture

import (
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

// GenerateTLSClientHello generates a TLS ClientHello with SNI as the FIRST extension
// This is critical for bypassing Russian TSPU DPI systems which use fast-path optimization
// for whitelisted domains when SNI appears first in the extension list.
func GenerateTLSClientHello(domain string) ([]byte, error) {
	if domain == "" {
		return nil, fmt.Errorf("domain required")
	}

	// Random (32 bytes) - unique per connection
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return nil, fmt.Errorf("failed to generate random: %v", err)
	}

	// Session ID (32 bytes for resumption capability)
	sessionID := make([]byte, 32)
	if _, err := rand.Read(sessionID); err != nil {
		return nil, fmt.Errorf("failed to generate session ID: %v", err)
	}

	// Cipher Suites - exact order matters for fingerprint matching
	cipherSuites := []uint16{
		0x1301, // TLS_AES_128_GCM_SHA256
		0x1303, // TLS_CHACHA20_POLY1305_SHA256
		0x1302, // TLS_AES_256_GCM_SHA384
		0xc02b, // TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256
		0xc02f, // TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256
		0xcca9, // TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256
		0xcca8, // TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256
		0xc02c, // TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384
		0xc030, // TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384
		0xc00a, // TLS_ECDHE_ECDSA_WITH_AES_256_CBC_SHA
		0xc009, // TLS_ECDHE_ECDSA_WITH_AES_128_CBC_SHA
		0xc013, // TLS_ECDHE_RSA_WITH_AES_128_CBC_SHA
		0xc014, // TLS_ECDHE_RSA_WITH_AES_256_CBC_SHA
		0x009c, // TLS_RSA_WITH_AES_128_GCM_SHA256
		0x009d, // TLS_RSA_WITH_AES_256_GCM_SHA384
		0x002f, // TLS_RSA_WITH_AES_128_CBC_SHA
		0x0035, // TLS_RSA_WITH_AES_256_CBC_SHA
	}

	// Build extensions in the CRITICAL order:
	// SNI MUST BE FIRST for Russian DPI bypass!
	extensions := buildExtensions(domain)

	// Assemble ClientHello
	clientHello := make([]byte, 0, 1024)

	// Client Version (TLS 1.2)
	clientHello = append(clientHello, 0x03, 0x03)

	// Random
	clientHello = append(clientHello, random...)

	// Session ID
	clientHello = append(clientHello, byte(len(sessionID)))
	clientHello = append(clientHello, sessionID...)

	// Cipher Suites
	cipherSuitesLen := len(cipherSuites) * 2
	clientHello = append(clientHello, byte(cipherSuitesLen>>8), byte(cipherSuitesLen))
	for _, cs := range cipherSuites {
		clientHello = append(clientHello, byte(cs>>8), byte(cs))
	}

	// Compression Methods (no compression)
	clientHello = append(clientHello, 0x01, 0x00)

	// Extensions - serialize all
	extensionsData := serializeExtensions(extensions)
	clientHello = append(clientHello, byte(len(extensionsData)>>8), byte(len(extensionsData)))
	clientHello = append(clientHello, extensionsData...)

	// Handshake wrapper
	handshake := make([]byte, 0, len(clientHello)+4)
	handshake = append(handshake, 0x01) // ClientHello type
	// 3-byte length
	handshake = append(handshake,
		byte(len(clientHello)>>16),
		byte(len(clientHello)>>8),
		byte(len(clientHello)))
	handshake = append(handshake, clientHello...)

	// TLS Record wrapper
	record := make([]byte, 0, len(handshake)+5)
	record = append(record, 0x16)       // Handshake
	record = append(record, 0x03, 0x01) // TLS 1.0 (for compatibility)
	record = append(record, byte(len(handshake)>>8), byte(len(handshake)))
	record = append(record, handshake...)

	return record, nil
}

// Extension represents a TLS extension
type Extension struct {
	Type uint16
	Data []byte
}

// buildExtensions creates all extensions in the correct order
func buildExtensions(domain string) []Extension {
	extensions := make([]Extension, 0, 15)

	// 1. SNI - CRITICAL: MUST BE FIRST!
	sniData := buildSNI(domain)
	extensions = append(extensions, Extension{Type: 0x0000, Data: sniData})

	// 2. extended_master_secret
	extensions = append(extensions, Extension{Type: 0x0017, Data: []byte{}})

	// 3. renegotiation_info
	extensions = append(extensions, Extension{Type: 0xff01, Data: []byte{0x00}})

	// 4. supported_groups
	groups := []uint16{0x001d, 0x0017, 0x0018, 0x0019, 0x0100, 0x0101}
	groupsData := make([]byte, 2+len(groups)*2)
	binary.BigEndian.PutUint16(groupsData[0:2], uint16(len(groups)*2))
	for i, group := range groups {
		binary.BigEndian.PutUint16(groupsData[2+i*2:], group)
	}
	extensions = append(extensions, Extension{Type: 0x000a, Data: groupsData})

	// 5. ec_point_formats
	extensions = append(extensions, Extension{Type: 0x000b, Data: []byte{0x01, 0x00}})

	// 6. session_ticket
	extensions = append(extensions, Extension{Type: 0x0023, Data: []byte{}})

	// 7. ALPN
	alpnData := []byte{
		0x00, 0x0c, // total length
		0x02, 'h', '2', // h2
		0x08, 'h', 't', 't', 'p', '/', '1', '.', '1', // http/1.1
	}
	extensions = append(extensions, Extension{Type: 0x0010, Data: alpnData})

	// 8. status_request (OCSP)
	extensions = append(extensions, Extension{Type: 0x0005, Data: []byte{0x01, 0x00, 0x00, 0x00, 0x00}})

	// 9. encrypt_then_mac (with specific data pattern)
	extensions = append(extensions, Extension{
		Type: 0x0022,
		Data: []byte{0x00, 0x04, 0x03, 0x05, 0x03, 0x06, 0x03, 0x02, 0x03, 0x00},
	})

	// 10. key_share
	keyShareData := buildKeyShare()
	extensions = append(extensions, Extension{Type: 0x0033, Data: keyShareData})

	// 11. supported_versions
	versionsData := []byte{
		0x04,       // length
		0x03, 0x04, // TLS 1.3
		0x03, 0x03, // TLS 1.2
	}
	extensions = append(extensions, Extension{Type: 0x002b, Data: versionsData})

	// 12. signature_algorithms
	sigAlgs := []uint16{0x0403, 0x0503, 0x0603, 0x0804, 0x0805, 0x0806, 0x0401, 0x0501, 0x0601, 0x0203, 0x0201}
	sigData := make([]byte, 2+len(sigAlgs)*2)
	binary.BigEndian.PutUint16(sigData[0:2], uint16(len(sigAlgs)*2))
	for i, alg := range sigAlgs {
		binary.BigEndian.PutUint16(sigData[2+i*2:], alg)
	}
	extensions = append(extensions, Extension{Type: 0x000d, Data: sigData})

	// 13. psk_key_exchange_modes
	extensions = append(extensions, Extension{Type: 0x002d, Data: []byte{0x01, 0x01}})

	// 14. record_size_limit
	extensions = append(extensions, Extension{Type: 0x001c, Data: []byte{0x40, 0x01}})

	// 15. encrypted_client_hello (padding to match working payload)
	echData := buildECH()
	extensions = append(extensions, Extension{Type: 0xfe0d, Data: echData})

	return extensions
}

// buildSNI creates SNI extension data
func buildSNI(domain string) []byte {
	// Server Name List Length (2 bytes)
	// Server Name Type (1 byte) = 0x00 (host_name)
	// Server Name Length (2 bytes)
	// Server Name (N bytes)

	sniLen := len(domain)
	data := make([]byte, 5+sniLen)

	// Server Name List Length
	binary.BigEndian.PutUint16(data[0:2], uint16(3+sniLen))

	// Server Name Type: host_name
	data[2] = 0x00

	// Server Name Length
	binary.BigEndian.PutUint16(data[3:5], uint16(sniLen))

	// Server Name
	copy(data[5:], []byte(domain))

	return data
}

// buildKeyShare creates key_share extension with x25519 and secp256r1 keys
func buildKeyShare() []byte {
	// x25519 key (32 bytes) + secp256r1 key (65 bytes)
	x25519Key := make([]byte, 32)
	secp256r1Key := make([]byte, 65)

	rand.Read(x25519Key)
	rand.Read(secp256r1Key)

	// Format: Client Shares Length (2) + [Group(2) + Length(2) + Key(N)]...
	data := make([]byte, 2+4+32+4+65)

	// Client Shares Length
	binary.BigEndian.PutUint16(data[0:2], uint16(4+32+4+65))

	offset := 2

	// x25519 share
	binary.BigEndian.PutUint16(data[offset:], 0x001d) // group: x25519
	offset += 2
	binary.BigEndian.PutUint16(data[offset:], 32) // key length
	offset += 2
	copy(data[offset:], x25519Key)
	offset += 32

	// secp256r1 share
	binary.BigEndian.PutUint16(data[offset:], 0x0017) // group: secp256r1
	offset += 2
	binary.BigEndian.PutUint16(data[offset:], 65) // key length
	offset += 2
	copy(data[offset:], secp256r1Key)

	return data
}

// buildECH creates encrypted_client_hello extension (dummy/padding)
func buildECH() []byte {
	// Simplified ECH structure for padding/fingerprint matching
	// This doesn't need to be a valid ECH, just needs to look right to DPI
	data := make([]byte, 281)

	// ECH header pattern
	data[0] = 0xfe
	data[1] = 0x0d
	data[2] = 0x01
	data[3] = 0x19
	data[4] = 0x00
	data[5] = 0x00
	data[6] = 0x01
	data[7] = 0x00
	data[8] = 0x01
	data[9] = 0xf8
	data[10] = 0x00
	data[11] = 0x20

	// Random padding for the rest
	rand.Read(data[12:])

	return data
}

// serializeExtensions converts extensions to wire format
func serializeExtensions(extensions []Extension) []byte {
	data := make([]byte, 0, 1024)

	for _, ext := range extensions {
		// Extension Type (2 bytes)
		data = append(data, byte(ext.Type>>8), byte(ext.Type))

		// Extension Length (2 bytes)
		data = append(data, byte(len(ext.Data)>>8), byte(len(ext.Data)))

		// Extension Data
		data = append(data, ext.Data...)
	}

	return data
}
