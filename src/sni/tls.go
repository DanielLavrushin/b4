package sni

import (
	"github.com/daniellavrushin/b4/log"
)

const (
	tlsHandshakeClientHello uint8 = 1
)

const (
	tlsExtServerName uint16 = 0
)

type parseErr string

func (e parseErr) Error() string { return string(e) }

var errNotHello = parseErr("not a ClientHello")

func isValidSNIChar(b byte) bool {
	if (b >= 'a' && b <= 'z') ||
		(b >= 'A' && b <= 'Z') ||
		(b >= '0' && b <= '9') ||
		b == '-' || b == '.' || b == '_' {
		return true
	}
	if b >= 128 {
		return true
	}
	return false
}

func validateSNI(sni string) bool {
	if len(sni) == 0 {
		return false
	}
	for i := 0; i < len(sni); i++ {
		if !isValidSNIChar(sni[i]) {
			log.Tracef("Invalid SNI char at position %d: 0x%02x in %q", i, sni[i], sni)
			return false
		}
	}
	if sni != "localhost" && !contains(sni, '.') {
		return false
	}
	return true
}

func contains(s string, char byte) bool {
	for i := 0; i < len(s); i++ {
		if s[i] == char {
			return true
		}
	}
	return false
}

// ParseTLSClientHelloSNI extracts the SNI and max supported TLS version from a TLS ClientHello.
// Returns (sni, maxTLSVersion, ok). maxTLSVersion uses TLS wire format: 0x0303=TLS1.2, 0x0304=TLS1.3.
func ParseTLSClientHelloSNI(b []byte) (string, uint16, bool) {
	i := 0
	for i+5 <= len(b) {
		if b[i] != 0x16 {
			i++
			continue
		}

		// Parse TLS record length
		recLen := int(b[i+3])<<8 | int(b[i+4])
		if recLen <= 0 {
			i++
			continue
		}

		if i+5+recLen > len(b) {
			recLen = len(b) - i - 5
			if recLen <= 0 {
				i++
				continue
			}
		}

		rec := b[i+5 : i+5+recLen]
		if len(rec) < 4 {
			i++
			continue
		}

		// Check if this is ClientHello (0x01)
		if rec[0] == 0x01 {
			// Parse handshake length (3 bytes)
			hl := int(rec[1])<<16 | int(rec[2])<<8 | int(rec[3])
			if 4+hl > len(rec) {
				// Truncated handshake, try to parse what we have
				hl = len(rec) - 4
				if hl <= 0 {
					i++
					continue
				}
			}

			ch := rec[4 : 4+hl]
			sni, hasECH, _, tlsVer := parseTLSClientHelloMeta(ch)
			if sni == "" {
				if hasECH {
					log.Tracef("TLS: ECH present, no clear SNI")
				} else {
					log.Tracef("TLS: SNI missing")
				}
				i++
				continue
			}

			if !validateSNI(sni) {
				log.Tracef("TLS: Invalid SNI extracted: %q", sni)
				i++
				continue
			}

			return sni, tlsVer, true
		}
		i += 5 + recLen
	}
	return "", 0, false
}

func IsValidSNI(name []byte) bool {
	return validateSNI(string(name))
}

func ParseTLSClientHelloBodySNI(ch []byte) (string, bool) {
	sni, _, _, _ := parseTLSClientHelloMeta(ch)
	if sni == "" {
		return "", false
	}

	if !validateSNI(sni) {
		return "", false
	}

	return sni, true
}

func parseTLSClientHelloMeta(ch []byte) (string, bool, []string, uint16) {
	p := 0
	chLen := len(ch)

	// Version (2 bytes) - legacy version field
	if p+2 > chLen {
		return "", false, nil, 0
	}
	legacyVersion := uint16(ch[p])<<8 | uint16(ch[p+1])
	p += 2

	// Random (32 bytes)
	if p+32 > chLen {
		return "", false, nil, 0
	}
	p += 32

	// Session ID
	if p+1 > chLen {
		return "", false, nil, 0
	}
	sidLen := int(ch[p])
	p++
	if p+sidLen > chLen {
		return "", false, nil, 0
	}
	p += sidLen

	// Cipher suites
	if p+2 > chLen {
		return "", false, nil, 0
	}
	csLen := int(ch[p])<<8 | int(ch[p+1])
	p += 2
	if p+csLen > chLen {
		return "", false, nil, 0
	}
	p += csLen

	// Compression methods
	if p+1 > chLen {
		return "", false, nil, 0
	}
	cmLen := int(ch[p])
	p++
	if p+cmLen > chLen {
		return "", false, nil, 0
	}
	p += cmLen

	// Extensions - be tolerant if truncated
	if p+2 > chLen {
		return "", false, nil, 0
	}
	extLen := int(ch[p])<<8 | int(ch[p+1])
	p += 2
	if extLen == 0 {
		return "", false, nil, 0
	}

	// Handle truncated extensions
	if p+extLen > chLen {
		extLen = chLen - p
		if extLen <= 0 {
			return "", false, nil, 0
		}
	}

	exts := ch[p : p+extLen]
	extEnd := len(exts)

	var sni string
	var hasECH bool
	var alpns []string
	var maxTLSVersion uint16

	q := 0
	for q+4 <= extEnd {
		// Extension type (2 bytes)
		et := int(exts[q])<<8 | int(exts[q+1])
		// Extension length (2 bytes)
		el := int(exts[q+2])<<8 | int(exts[q+3])
		q += 4

		if el < 0 || q+el > extEnd {
			// Truncated extension, break
			break
		}

		ed := exts[q : q+el]

		switch et {
		case 0: // Server Name extension
			sniStr := extractSNIFromExtension(ed)
			if sniStr != "" {
				sni = sniStr
			}

		case 16: // ALPN extension
			alpns = extractALPNFromExtension(ed)

		case 43: // supported_versions extension (0x002B)
			maxTLSVersion = extractMaxSupportedVersion(ed)

		default:
			if et == 0xfe0d || et == 0xfe0e || et == 0xfe0f {
				hasECH = true
			}
		}
		q += el
	}

	// If supported_versions wasn't present, fall back to legacy version
	if maxTLSVersion == 0 {
		maxTLSVersion = legacyVersion
	}

	return sni, hasECH, alpns, maxTLSVersion
}

// extractMaxSupportedVersion parses the supported_versions extension data
// from a ClientHello and returns the highest TLS version advertised.
// Format: 1 byte length + N*2 bytes version entries.
func extractMaxSupportedVersion(ed []byte) uint16 {
	if len(ed) < 1 {
		return 0
	}
	listLen := int(ed[0])
	if listLen < 2 || 1+listLen > len(ed) {
		return 0
	}

	var maxVer uint16
	for i := 1; i+1 < 1+listLen; i += 2 {
		ver := uint16(ed[i])<<8 | uint16(ed[i+1])
		// Skip GREASE values (0x?A?A pattern)
		if ver&0x0f0f == 0x0a0a {
			continue
		}
		if ver > maxVer {
			maxVer = ver
		}
	}
	return maxVer
}

func extractSNIFromExtension(ed []byte) string {
	if len(ed) < 2 {
		return ""
	}

	listLen := int(ed[0])<<8 | int(ed[1])
	if listLen <= 0 || 2+listLen > len(ed) {
		return ""
	}

	r := 2
	listEnd := 2 + listLen

	for r+3 <= listEnd {
		nameType := ed[r]
		r++

		if r+2 > listEnd {
			break
		}
		nameLen := int(ed[r])<<8 | int(ed[r+1])
		r += 2

		if nameLen <= 0 || r+nameLen > listEnd || r+nameLen > len(ed) {
			break
		}

		if nameType == 0 {
			sniBytes := make([]byte, nameLen)
			copy(sniBytes, ed[r:r+nameLen])

			for i, b := range sniBytes {
				if !isValidSNIChar(b) {
					if i > 0 {
						return string(sniBytes[:i])
					}
					return ""
				}
			}

			return string(sniBytes)
		}

		r += nameLen
	}

	return ""
}

func extractALPNFromExtension(ed []byte) []string {
	var alpns []string

	if len(ed) < 2 {
		return alpns
	}

	listLen := int(ed[0])<<8 | int(ed[1])
	if listLen <= 0 || 2+listLen > len(ed) {
		return alpns
	}

	r := 2
	listEnd := 2 + listLen

	for r < listEnd {
		if r >= len(ed) {
			break
		}

		protoLen := int(ed[r])
		r++

		if protoLen <= 0 || r+protoLen > listEnd || r+protoLen > len(ed) {
			break
		}

		alpns = append(alpns, string(ed[r:r+protoLen]))
		r += protoLen
	}

	return alpns
}
