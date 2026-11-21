package tlsgen

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/binary"
	"fmt"
)

func GenerateTLSClientHello(domain string) ([]byte, error) {
	if domain == "" {
		return nil, fmt.Errorf("domain required")
	}

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

	extensions, err := buildExtensions(domain)
	if err != nil {
		return nil, err
	}

	// Assemble ClientHello
	clientHello := make([]byte, 0, 1024)

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

func buildExtensions(domain string) ([]Extension, error) {
	keyShareData, err := buildKeyShare()
	if err != nil {
		return nil, err
	}

	groups := []uint16{0x001d, 0x0017, 0x0018, 0x0019, 0x0100, 0x0101}
	groupsData := make([]byte, 2+len(groups)*2)
	binary.BigEndian.PutUint16(groupsData[0:2], uint16(len(groups)*2))
	for i, group := range groups {
		binary.BigEndian.PutUint16(groupsData[2+i*2:], group)
	}

	sigAlgs := []uint16{0x0403, 0x0503, 0x0603, 0x0804, 0x0805, 0x0806, 0x0401, 0x0501, 0x0601, 0x0203, 0x0201}
	sigData := make([]byte, 2+len(sigAlgs)*2)
	binary.BigEndian.PutUint16(sigData[0:2], uint16(len(sigAlgs)*2))
	for i, alg := range sigAlgs {
		binary.BigEndian.PutUint16(sigData[2+i*2:], alg)
	}

	return []Extension{
		{Type: 0x0000, Data: buildSNI(domain)},
		{Type: 0x0017, Data: []byte{}},
		{Type: 0xff01, Data: []byte{0x00}},
		{Type: 0x000a, Data: groupsData},
		{Type: 0x000b, Data: []byte{0x01, 0x00}},
		{Type: 0x0023, Data: []byte{}},
		{Type: 0x0010, Data: []byte{
			0x00, 0x0c,
			0x02, 'h', '2',
			0x08, 'h', 't', 't', 'p', '/', '1', '.', '1',
		}},
		{Type: 0x0005, Data: []byte{0x01, 0x00, 0x00, 0x00, 0x00}},
		{Type: 0x0022, Data: []byte{0x00, 0x08, 0x04, 0x03, 0x05, 0x03, 0x06, 0x03, 0x02, 0x03}},
		{Type: 0x0033, Data: keyShareData},
		{Type: 0x002b, Data: []byte{0x04, 0x03, 0x04, 0x03, 0x03}},
		{Type: 0x000d, Data: sigData},
		{Type: 0x002d, Data: []byte{0x01, 0x01}},
		{Type: 0x001c, Data: []byte{0x40, 0x01}},
		{Type: 0xfe0d, Data: buildECH()},
	}, nil
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

func buildKeyShare() ([]byte, error) {
	x25519Key := make([]byte, 32)
	rand.Read(x25519Key)

	p256, err := ecdh.P256().GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate P-256 key share: %v", err)
	}
	p256Key := p256.PublicKey().Bytes()

	data := make([]byte, 0, 2+4+len(x25519Key)+4+len(p256Key))
	data = binary.BigEndian.AppendUint16(data, uint16(4+len(x25519Key)+4+len(p256Key)))
	data = binary.BigEndian.AppendUint16(data, 0x001d)
	data = binary.BigEndian.AppendUint16(data, uint16(len(x25519Key)))
	data = append(data, x25519Key...)
	data = binary.BigEndian.AppendUint16(data, 0x0017)
	data = binary.BigEndian.AppendUint16(data, uint16(len(p256Key)))
	data = append(data, p256Key...)
	return data, nil
}

const (
	echExtensionLen = 281
	echEncLen       = 32
	echPayloadLen   = echExtensionLen - (1 + 2 + 2 + 1 + 2 + echEncLen + 2)
)

func buildECH() []byte {
	data := make([]byte, 0, echExtensionLen)
	data = append(data, 0x00)
	data = binary.BigEndian.AppendUint16(data, 0x0001)
	data = binary.BigEndian.AppendUint16(data, 0x0001)
	random := make([]byte, 1+echEncLen+echPayloadLen)
	rand.Read(random)
	data = append(data, random[0])
	data = binary.BigEndian.AppendUint16(data, echEncLen)
	data = append(data, random[1:1+echEncLen]...)
	data = binary.BigEndian.AppendUint16(data, echPayloadLen)
	data = append(data, random[1+echEncLen:]...)
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
