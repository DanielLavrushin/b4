package quic

import (
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
)

// LocateSNIOffset returns the byte offset in the original packet where SNI value starts,
// and the length of the SNI. Returns (-1, 0) if SNI cannot be located.
func LocateSNIOffset(packet []byte) (offset int, length int) {
	if !IsInitial(packet) {
		return -1, 0
	}

	// Parse header to find payload start
	headerLen, pnLen, ok := parseHeaderLength(packet)
	if !ok {
		return -1, 0
	}

	dcid := ParseDCID(packet)
	if dcid == nil {
		return -1, 0
	}

	plain, ok := DecryptInitial(dcid, packet)
	if !ok {
		return -1, 0
	}

	// Find SNI offset within decrypted payload
	sniOffsetInPlain, sniLen := locateSNIInCrypto(plain)
	if sniOffsetInPlain < 0 {
		return -1, 0
	}

	// Map back to original packet
	// Encrypted payload starts at headerLen + pnLen
	// AES-GCM preserves byte positions (ciphertext[i] = encrypt(plaintext[i]))
	return headerLen + pnLen + sniOffsetInPlain, sniLen
}

// parseHeaderLength returns (headerLen, pnLen, ok)
// headerLen is bytes before packet number, pnLen is packet number length
func parseHeaderLength(packet []byte) (int, int, bool) {
	if len(packet) < 7 {
		return 0, 0, false
	}

	off := 1 + 4 // flags + version

	// DCID
	if len(packet) < off+1 {
		return 0, 0, false
	}
	dlen := int(packet[off])
	off++
	if len(packet) < off+dlen {
		return 0, 0, false
	}
	dcid := packet[off : off+dlen]
	off += dlen

	// SCID
	if len(packet) < off+1 {
		return 0, 0, false
	}
	slen := int(packet[off])
	off++
	if len(packet) < off+slen {
		return 0, 0, false
	}
	off += slen

	// Token (varint length + bytes)
	tlen, n := readVar(packet[off:])
	if n == 0 {
		return 0, 0, false
	}
	if tlen > uint64(len(packet)) || off+n+int(tlen) > len(packet) {
		return 0, 0, false
	}
	off += n + int(tlen)

	// Length field (varint)
	_, m := readVar(packet[off:])
	if m == 0 {
		return 0, 0, false
	}
	pnOff := off + m

	// Need HP sample to unmask first byte for PN length
	if pnOff+4+16 > len(packet) {
		return 0, 0, false
	}

	// Derive HP key to unmask
	ver := binary.BigEndian.Uint32(packet[1:5])
	hp, ok := deriveHP(dcid, ver)
	if !ok {
		// Fallback: assume 2-byte PN (most common)
		return pnOff, 2, true
	}

	// Get sample and compute mask
	var sample [16]byte
	copy(sample[:], packet[pnOff+4:pnOff+4+16])
	var mask [16]byte
	hp.Encrypt(mask[:], sample[:])

	// Unmask first byte to get actual PN length
	first := packet[0] ^ (mask[0] & 0x0f)
	pnLen := int((first & 0x03) + 1)

	return pnOff, pnLen, true
}

// deriveHP derives just the header protection key
func deriveHP(dcid []byte, version uint32) (cipher.Block, bool) {
	var salt []byte
	var labelPrefix string

	switch version {
	case versionV1:
		salt = saltV1
		labelPrefix = "quic"
	case versionV2:
		salt = saltV2
		labelPrefix = "quicv2"
	default:
		return nil, false
	}

	secret := hkdfExtractSHA256(salt, dcid)
	client, err := hkdfExpandLabel(secret, "client in", secretSize)
	if err != nil {
		return nil, false
	}
	hpkey, err := hkdfExpandLabel(client, labelPrefix+" hp", keySize)
	if err != nil {
		return nil, false
	}
	hp, err := aes.NewCipher(hpkey)
	if err != nil {
		return nil, false
	}
	return hp, true
}

// locateSNIInCrypto finds SNI position within decrypted QUIC CRYPTO frames
// Returns (offset, length) or (-1, 0) if not found
func locateSNIInCrypto(plain []byte) (int, int) {
	pos := 0

	for pos < len(plain) {
		if plain[pos] == 0x00 { // PADDING
			pos++
			continue
		}
		if plain[pos] == 0x01 { // PING
			pos++
			continue
		}
		if plain[pos] != 0x06 { // Not CRYPTO frame
			return -1, 0
		}

		pos++ // skip frame type

		// Offset (varint)
		cryptoOff, n := readVar(plain[pos:])
		if n == 0 {
			return -1, 0
		}
		pos += n

		// Length (varint)
		cryptoLen, n := readVar(plain[pos:])
		if n == 0 {
			return -1, 0
		}
		pos += n

		// CRYPTO data starts here
		cryptoDataStart := pos

		if pos+int(cryptoLen) > len(plain) {
			return -1, 0
		}

		cryptoData := plain[pos : pos+int(cryptoLen)]

		// Find SNI within ClientHello
		sniOffInClientHello, sniLen := locateSNIInClientHello(cryptoData)
		if sniOffInClientHello >= 0 {
			// Total offset = position of CRYPTO data + offset within it
			// But we also need to account for CRYPTO frame's offset field if non-zero
			if cryptoOff == 0 {
				return cryptoDataStart + sniOffInClientHello, sniLen
			}
			// If cryptoOff != 0, this is a continuation - more complex
			// For simplicity, only handle first CRYPTO frame
		}

		pos += int(cryptoLen)
	}

	return -1, 0
}

// locateSNIInClientHello finds SNI value position in TLS ClientHello
// Returns (offset, length) or (-1, 0)
func locateSNIInClientHello(ch []byte) (int, int) {
	if len(ch) < 4 {
		return -1, 0
	}

	// Handshake: type(1) + length(3)
	if ch[0] != 0x01 { // ClientHello
		return -1, 0
	}

	pos := 4 // skip handshake header

	// Version (2) + Random (32)
	if pos+34 > len(ch) {
		return -1, 0
	}
	pos += 34

	// Session ID
	if pos >= len(ch) {
		return -1, 0
	}
	sidLen := int(ch[pos])
	pos++
	pos += sidLen

	// Cipher suites
	if pos+2 > len(ch) {
		return -1, 0
	}
	csLen := int(binary.BigEndian.Uint16(ch[pos : pos+2]))
	pos += 2 + csLen

	// Compression
	if pos >= len(ch) {
		return -1, 0
	}
	compLen := int(ch[pos])
	pos++
	pos += compLen

	// Extensions length
	if pos+2 > len(ch) {
		return -1, 0
	}
	extLen := int(binary.BigEndian.Uint16(ch[pos : pos+2]))
	pos += 2

	extEnd := pos + extLen
	if extEnd > len(ch) {
		extEnd = len(ch)
	}

	// Walk extensions
	for pos+4 <= extEnd {
		extType := binary.BigEndian.Uint16(ch[pos : pos+2])
		extDataLen := int(binary.BigEndian.Uint16(ch[pos+2 : pos+4]))
		pos += 4

		if pos+extDataLen > extEnd {
			break
		}

		if extType == 0 { // server_name extension
			// SNI extension: list_len(2) + name_type(1) + name_len(2) + name
			if extDataLen < 5 {
				return -1, 0
			}

			// list_len at pos
			// name_type at pos+2
			// name_len at pos+3
			nameLen := int(binary.BigEndian.Uint16(ch[pos+3 : pos+5]))
			nameStart := pos + 5

			if nameStart+nameLen > pos+extDataLen {
				return -1, 0
			}

			return nameStart, nameLen
		}

		pos += extDataLen
	}

	return -1, 0
}
