package nfq

import (
	"crypto/rand"
	"encoding/binary"
	"net"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/utils"
)

const (
	MaxTCPPacketSize = 1460 // Standard MTU minus headers
	MaxSNILength     = 255  // Max SNI length per spec
)

// GREASE values (RFC 8701)
var greaseValues = []uint16{
	0x0a0a, 0x1a1a, 0x2a2a, 0x3a3a,
	0x4a4a, 0x5a5a, 0x6a6a, 0x7a7a,
	0x8a8a, 0x9a9a, 0xaaaa, 0xbaba,
	0xcaca, 0xdada, 0xeaea, 0xfafa,
}

// TLS Extension types
// TLS Extension types - only keep the ones we use
const (
	extServerName = 0x0000
	extALPN       = 0x0010
	extPadding    = 0x0015
)

// Common TLS extensions for advanced mutation
var commonExtensions = []uint16{
	0x0001, // max_fragment_length
	0x0005, // status_request
	0x000a, // supported_groups
	0x000d, // signature_algorithms
	0x000f, // heartbeat
	0x0017, // extended_master_secret
	0x0023, // session_ticket
	0x002b, // supported_versions
	0x0033, // key_share
}

var randMutex sync.Mutex

func (w *Worker) MutateClientHello(cfg *config.SetConfig, packet []byte, dst net.IP) []byte {
	if cfg == nil || cfg.Faking.SNIMutation.Mode == config.ConfigOff {
		return packet
	}

	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen

	if len(packet) <= payloadStart+5 {
		return packet
	}

	payload := packet[payloadStart:]
	if payload[0] != 0x16 || payload[1] != 0x03 {
		return packet
	}

	recordLen := int(binary.BigEndian.Uint16(payload[3:5]))
	if len(payload) < 5+recordLen {
		return packet
	}
	if len(payload) < 6 || payload[5] != 0x01 {
		return packet
	}

	switch cfg.Faking.SNIMutation.Mode {
	case "duplicate":
		return w.duplicateSNI(packet, cfg)
	case "grease":
		return w.addGREASE(packet, cfg)
	case "padding":
		return w.addPadding(packet, cfg)
	case "reorder":
		return w.reorderExtensions(packet, cfg)
	case "full":
		return w.fullMutation(packet, cfg)
	case "advanced":
		return w.addAdvancedMutations(packet)
	default:
		return packet
	}
}

func (w *Worker) duplicateSNI(packet []byte, cfg *config.SetConfig) []byte {
	if cfg == nil {
		return packet
	}

	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen

	extOffset := w.findExtensionsOffset(packet[payloadStart:])
	if extOffset < 0 {
		return packet
	}

	fakeSNIs := make([]byte, 0, 1024)

	for _, sni := range cfg.Faking.SNIMutation.FakeSNIs {
		if sni == "" {
			continue
		}

		if len(sni) > MaxSNILength {
			log.Tracef("SNI too long, skipping: %d bytes", len(sni))
			continue
		}

		sniExt := make([]byte, 9+len(sni))
		binary.BigEndian.PutUint16(sniExt[0:2], extServerName)      // Extension type
		binary.BigEndian.PutUint16(sniExt[2:4], uint16(5+len(sni))) // Extension length
		binary.BigEndian.PutUint16(sniExt[4:6], uint16(3+len(sni))) // Server name list length
		sniExt[6] = 0                                               // Name type: host_name
		binary.BigEndian.PutUint16(sniExt[7:9], uint16(len(sni)))   // Name length
		copy(sniExt[9:], sni)

		fakeSNIs = append(fakeSNIs, sniExt...)
	}

	return w.insertExtensions(packet, fakeSNIs)
}

func (w *Worker) addGREASE(packet []byte, cfg *config.SetConfig) []byte {
	if cfg == nil {
		return packet
	}

	grease := make([]byte, 0, cfg.Faking.SNIMutation.GreaseCount*8)

	for i := 0; i < cfg.Faking.SNIMutation.GreaseCount; i++ {
		// Pick random GREASE value
		var b [1]byte
		randMutex.Lock()
		rand.Read(b[:])
		randMutex.Unlock()
		greaseVal := greaseValues[b[0]%uint8(len(greaseValues))]

		ext := make([]byte, 8)
		binary.BigEndian.PutUint16(ext[0:2], greaseVal) // GREASE extension type
		binary.BigEndian.PutUint16(ext[2:4], 4)         // Length

		// Random GREASE data
		randMutex.Lock()
		rand.Read(ext[4:8])
		randMutex.Unlock()

		grease = append(grease, ext...)
	}

	return w.insertExtensions(packet, grease)
}

// addPadding adds padding extension
func (w *Worker) addPadding(packet []byte, cfg *config.SetConfig) []byte {
	if cfg == nil {
		return packet
	}

	paddingSize := cfg.Faking.SNIMutation.PaddingSize
	if paddingSize < 16 {
		paddingSize = 16
	}
	if paddingSize > 4096 {
		paddingSize = 4096
	}

	// Check if adding padding would exceed MTU
	if len(packet)+paddingSize+4 > MaxTCPPacketSize {
		paddingSize = MaxTCPPacketSize - len(packet) - 4
		if paddingSize <= 0 {
			return packet
		}
	}

	padding := make([]byte, 4+paddingSize)
	binary.BigEndian.PutUint16(padding[0:2], extPadding)          // Extension type
	binary.BigEndian.PutUint16(padding[2:4], uint16(paddingSize)) // Length
	// Leave padding data as zeros (standard practice)

	return w.insertExtensions(packet, padding)
}

// reorderExtensions randomly reorders TLS extensions
func (w *Worker) reorderExtensions(packet []byte, cfg *config.SetConfig) []byte {
	if cfg == nil {
		return packet
	}

	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen

	extOffset := w.findExtensionsOffset(packet[payloadStart:])
	if extOffset < 0 {
		return packet
	}

	// Parse existing extensions
	extensions := w.parseExtensions(packet[payloadStart+extOffset:])
	if len(extensions) < 2 {
		return packet // Nothing to reorder
	}

	// Shuffle extensions (keep SNI first for compatibility)
	var sniExt []byte
	var otherExts [][]byte

	for _, ext := range extensions {
		if len(ext) >= 2 && binary.BigEndian.Uint16(ext[0:2]) == extServerName {
			sniExt = ext
		} else {
			otherExts = append(otherExts, ext)
		}
	}

	// Random shuffle other extensions
	for i := len(otherExts) - 1; i > 0; i-- {
		j := int(utils.RandUint32() % uint32(i+1))
		otherExts[i], otherExts[j] = otherExts[j], otherExts[i]
	}

	// Rebuild extensions
	newExts := make([]byte, 0, 4096)
	if sniExt != nil {
		newExts = append(newExts, sniExt...)
	}
	for _, ext := range otherExts {
		newExts = append(newExts, ext...)
	}

	return w.replaceExtensions(packet, newExts)
}

func (w *Worker) fullMutation(packet []byte, cfg *config.SetConfig) []byte {
	if cfg == nil {
		return packet
	}

	mutated := packet

	// 1. Add duplicate SNIs
	mutated = w.duplicateSNI(mutated, cfg)
	if len(mutated) > MaxTCPPacketSize {
		log.Tracef("Mutation too large after duplicateSNI, aborting")
		return packet
	}

	// 2. Add common TLS extensions with fake data
	mutated = w.addCommonExtensions(mutated)
	if len(mutated) > MaxTCPPacketSize {
		log.Tracef("Mutation too large after common extensions")
		return w.duplicateSNI(packet, cfg)
	}

	// 3. ADD ADVANCED MUTATIONS (TLS 1.3 features) - THIS WAS MISSING!
	mutated = w.addAdvancedMutations(mutated)
	if len(mutated) > MaxTCPPacketSize {
		log.Tracef("Mutation too large after advanced mutations")
		// Fall back to simpler mutations
		mutated = w.addCommonExtensions(w.duplicateSNI(packet, cfg))
	}

	// 4. Add GREASE
	mutated = w.addGREASE(mutated, cfg)
	if len(mutated) > MaxTCPPacketSize {
		log.Tracef("Mutation too large after GREASE")
		// Continue with what we have
	}

	// 5. Add fake ALPN with many protocols
	mutated = w.addFakeALPN(mutated)
	if len(mutated) > MaxTCPPacketSize {
		log.Tracef("Mutation too large after ALPN")
		// Continue with what we have
	}

	// 6. Add unknown extensions
	mutated = w.addUnknownExtensions(mutated, cfg.Faking.SNIMutation.FakeExtCount)

	// 7. Reorder extensions
	mutated = w.reorderExtensions(mutated, cfg)

	// 8. Add padding last to fill MTU
	mutated = w.addPadding(mutated, cfg)

	return mutated
}

// Helper: Add fake ALPN extension with many protocols
func (w *Worker) addFakeALPN(packet []byte) []byte {
	protocols := []string{
		"http/1.0", "http/1.1", "h2", "h3",
		"spdy/3", "spdy/3.1",
		"quic", "hq", "doq",
		"xmpp", "mqtt", "amqp",
		"grpc", "websocket",
	}

	alpnData := make([]byte, 0, 256)
	for _, proto := range protocols {
		if len(alpnData)+1+len(proto) > 255 { // ALPN max size
			break
		}
		alpnData = append(alpnData, byte(len(proto)))
		alpnData = append(alpnData, proto...)
	}

	alpn := make([]byte, 6+len(alpnData))
	binary.BigEndian.PutUint16(alpn[0:2], extALPN)                 // Extension type
	binary.BigEndian.PutUint16(alpn[2:4], uint16(2+len(alpnData))) // Extension length
	binary.BigEndian.PutUint16(alpn[4:6], uint16(len(alpnData)))   // ALPN list length
	copy(alpn[6:], alpnData)

	return w.insertExtensions(packet, alpn)
}

// Helper: Add unknown/reserved extension types
func (w *Worker) addUnknownExtensions(packet []byte, count int) []byte {
	unknown := make([]byte, 0, count*8)

	// Mix of reserved and common extensions to confuse DPI
	for i := 0; i < count; i++ {
		var extType uint16

		if i < len(commonExtensions) {
			// Use real extension types with fake data
			extType = commonExtensions[i]
		} else {
			// Use reserved/unassigned extension types
			reservedTypes := []uint16{0x00ff, 0x1234, 0x5678, 0x9abc, 0xfe00, 0xffff}
			extType = reservedTypes[i%len(reservedTypes)]
		}

		ext := make([]byte, 8)
		binary.BigEndian.PutUint16(ext[0:2], extType)
		binary.BigEndian.PutUint16(ext[2:4], 4) // Length

		randMutex.Lock()
		rand.Read(ext[4:8]) // Random data
		randMutex.Unlock()

		unknown = append(unknown, ext...)
	}

	return w.insertExtensions(packet, unknown)
}

func (w *Worker) addCommonExtensions(packet []byte) []byte {
	extensions := make([]byte, 0, 256)

	// Add fake supported_groups (elliptic curves)
	groups := []byte{
		0x00, 0x0a, // Extension type: supported_groups
		0x00, 0x08, // Length
		0x00, 0x06, // Groups length
		0x00, 0x1d, // x25519
		0x00, 0x17, // secp256r1
		0x00, 0x18, // secp384r1
	}
	extensions = append(extensions, groups...)

	// Add fake signature_algorithms
	sigAlgs := []byte{
		0x00, 0x0d, // Extension type: signature_algorithms
		0x00, 0x0e, // Length
		0x00, 0x0c, // Algorithms length
		0x04, 0x03, // ECDSA-SHA256
		0x05, 0x03, // ECDSA-SHA384
		0x06, 0x03, // ECDSA-SHA512
		0x04, 0x01, // RSA-SHA256
		0x05, 0x01, // RSA-SHA384
		0x06, 0x01, // RSA-SHA512
	}
	extensions = append(extensions, sigAlgs...)

	// Add fake supported_versions (TLS 1.2, 1.3)
	versions := []byte{
		0x00, 0x2b, // Extension type: supported_versions
		0x00, 0x05, // Length
		0x04,       // Versions length
		0x03, 0x04, // TLS 1.3
		0x03, 0x03, // TLS 1.2
	}
	extensions = append(extensions, versions...)

	// Add session ticket
	ticket := []byte{
		0x00, 0x23, // Extension type: session_ticket
		0x00, 0x00, // Length (empty)
	}
	extensions = append(extensions, ticket...)

	return w.insertExtensions(packet, extensions)
}

func (w *Worker) addAdvancedMutations(packet []byte) []byte {
	mutated := packet

	// Add fake PSK exchange modes (TLS 1.3)
	pskModes := []byte{
		0x00, 0x2d, // Extension type: psk_key_exchange_modes
		0x00, 0x02, // Length
		0x01, // Modes length
		0x01, // PSK with (EC)DHE key establishment
	}

	if len(mutated)+len(pskModes) <= MaxTCPPacketSize {
		mutated = w.insertExtensions(mutated, pskModes)
	}

	// Add fake key_share (TLS 1.3)
	keyShare := make([]byte, 0, 128)
	keyShare = append(keyShare,
		0x00, 0x33, // Extension type: key_share
		0x00, 0x26, // Length
		0x00, 0x24, // Client shares length
		0x00, 0x1d, // Group: x25519
		0x00, 0x20, // Key length: 32 bytes
	)

	// Add 32 random bytes for the key
	key := make([]byte, 32)
	randMutex.Lock()
	rand.Read(key)
	randMutex.Unlock()
	keyShare = append(keyShare, key...)

	if len(mutated)+len(keyShare) <= MaxTCPPacketSize {
		mutated = w.insertExtensions(mutated, keyShare)
	}
	// Add fake status_request (OCSP)
	statusReq := []byte{
		0x00, 0x05, // Extension type: status_request
		0x00, 0x05, // Length
		0x01,       // Status type: OCSP
		0x00, 0x00, // Responder ID list length
		0x00, 0x00, // Request extensions length
	}

	if len(mutated)+len(statusReq) <= MaxTCPPacketSize {
		mutated = w.insertExtensions(mutated, statusReq)
	}

	return mutated
}

// Helper: Find extensions offset in ClientHello with proper bounds checking
func (w *Worker) findExtensionsOffset(payload []byte) int {
	if len(payload) < 43 {
		return -1
	}

	// Skip TLS header (5 bytes) + Handshake header (4 bytes) + Version (2) + Random (32)
	pos := 43

	// Session ID
	if pos >= len(payload) {
		return -1
	}
	sidLen := int(payload[pos])
	if pos+1+sidLen > len(payload) {
		return -1
	}
	pos += 1 + sidLen

	// Cipher suites
	if pos+2 > len(payload) {
		return -1
	}
	csLen := int(binary.BigEndian.Uint16(payload[pos : pos+2]))
	if pos+2+csLen > len(payload) {
		return -1
	}
	pos += 2 + csLen

	// Compression methods
	if pos >= len(payload) {
		return -1
	}
	compLen := int(payload[pos])
	if pos+1+compLen > len(payload) {
		return -1
	}
	pos += 1 + compLen

	// Extensions start here
	if pos+2 > len(payload) {
		return -1
	}

	return pos
}

// Helper: Parse extensions from buffer with improved safety
func (w *Worker) parseExtensions(data []byte) [][]byte {
	if len(data) < 2 {
		return nil
	}

	extLen := int(binary.BigEndian.Uint16(data[0:2]))
	if len(data) < 2+extLen {
		return nil
	}

	extensions := [][]byte{}
	pos := 2

	for pos < 2+extLen && pos+4 <= len(data) {
		// Extension type (2 bytes) + Extension length (2 bytes)
		if pos+4 > 2+extLen {
			break
		}

		el := int(binary.BigEndian.Uint16(data[pos+2 : pos+4]))
		if pos+4+el > 2+extLen || pos+4+el > len(data) {
			break
		}

		ext := make([]byte, 4+el)
		copy(ext, data[pos:pos+4+el])
		extensions = append(extensions, ext)

		pos += 4 + el
	}

	return extensions
}

// Helper: Insert extensions into packet with size limits
func (w *Worker) insertExtensions(packet []byte, newExts []byte) []byte {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen

	extOffset := w.findExtensionsOffset(packet[payloadStart:])
	if extOffset < 0 {
		return packet
	}

	// Get current extensions
	extPos := payloadStart + extOffset
	if len(packet) < extPos+2 {
		return packet
	}

	currentExtLen := int(binary.BigEndian.Uint16(packet[extPos : extPos+2]))

	// Validate extension length
	if extPos+2+currentExtLen > len(packet) {
		return packet
	}

	// Check MTU limit
	if len(packet)+len(newExts) > MaxTCPPacketSize {
		maxNewExts := MaxTCPPacketSize - len(packet)
		if maxNewExts <= 0 {
			return packet
		}
		newExts = newExts[:maxNewExts]
	}

	// Build new packet with inserted extensions
	newPacket := make([]byte, len(packet)+len(newExts))

	// Copy everything before extensions length field
	copy(newPacket, packet[:extPos])

	// Write new extensions length
	newExtLen := currentExtLen + len(newExts)
	binary.BigEndian.PutUint16(newPacket[extPos:extPos+2], uint16(newExtLen))

	// Copy original extensions
	copy(newPacket[extPos+2:], packet[extPos+2:extPos+2+currentExtLen])

	// Add new extensions
	copy(newPacket[extPos+2+currentExtLen:], newExts)

	// Copy everything after extensions
	if extPos+2+currentExtLen < len(packet) {
		copy(newPacket[extPos+2+currentExtLen+len(newExts):], packet[extPos+2+currentExtLen:])
	}

	// Update lengths
	w.updatePacketLengths(newPacket)

	return newPacket
}

// Helper: Replace all extensions with safety checks
func (w *Worker) replaceExtensions(packet []byte, newExts []byte) []byte {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen

	extOffset := w.findExtensionsOffset(packet[payloadStart:])
	if extOffset < 0 {
		return packet
	}

	extPos := payloadStart + extOffset
	if len(packet) < extPos+2 {
		return packet
	}

	currentExtLen := int(binary.BigEndian.Uint16(packet[extPos : extPos+2]))

	// Validate extension length
	if extPos+2+currentExtLen > len(packet) {
		return packet
	}

	// Build new packet
	sizeDiff := len(newExts) - currentExtLen
	newPacketLen := len(packet) + sizeDiff

	// Check MTU
	if newPacketLen > MaxTCPPacketSize {
		return packet
	}

	newPacket := make([]byte, newPacketLen)

	// Copy everything before extensions length field
	copy(newPacket, packet[:extPos])

	// Write new extensions length
	binary.BigEndian.PutUint16(newPacket[extPos:extPos+2], uint16(len(newExts)))

	// Write new extensions
	copy(newPacket[extPos+2:], newExts)

	// Copy everything after old extensions
	if extPos+2+currentExtLen < len(packet) {
		copy(newPacket[extPos+2+len(newExts):], packet[extPos+2+currentExtLen:])
	}

	// Update lengths
	w.updatePacketLengths(newPacket)

	return newPacket
}

// Helper: Update all packet lengths after mutation with IPv6 support
func (w *Worker) updatePacketLengths(packet []byte) {
	if len(packet) < 1 {
		return
	}

	version := packet[0] >> 4

	if version == 6 {
		// IPv6 handling
		ipv6HdrLen := 40
		if len(packet) < ipv6HdrLen+20 {
			return
		}

		tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
		payloadStart := ipv6HdrLen + tcpHdrLen
		payloadLen := len(packet) - payloadStart

		// Update IPv6 payload length
		binary.BigEndian.PutUint16(packet[4:6], uint16(len(packet)-ipv6HdrLen))

		// Update TLS record length
		if payloadLen >= 5 && packet[payloadStart] == 0x16 {
			binary.BigEndian.PutUint16(packet[payloadStart+3:payloadStart+5], uint16(payloadLen-5))
		}

		// Update ClientHello length
		if payloadLen >= 9 && packet[payloadStart] == 0x16 && packet[payloadStart+5] == 0x01 {
			helloLen := payloadLen - 9
			packet[payloadStart+6] = byte(helloLen >> 16)
			packet[payloadStart+7] = byte(helloLen >> 8)
			packet[payloadStart+8] = byte(helloLen)
		}

		// Fix TCP checksum for IPv6
		sock.FixTCPChecksumV6(packet)
		return
	}

	// IPv4 handling
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	if len(packet) < ipHdrLen+20 {
		return
	}

	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen
	payloadLen := len(packet) - payloadStart

	// Update IP total length
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))

	// Update TLS record length
	if payloadLen >= 5 && packet[payloadStart] == 0x16 {
		binary.BigEndian.PutUint16(packet[payloadStart+3:payloadStart+5], uint16(payloadLen-5))
	}

	// Update ClientHello length
	if payloadLen >= 9 && packet[payloadStart] == 0x16 && packet[payloadStart+5] == 0x01 {
		helloLen := payloadLen - 9
		packet[payloadStart+6] = byte(helloLen >> 16)
		packet[payloadStart+7] = byte(helloLen >> 8)
		packet[payloadStart+8] = byte(helloLen)
	}

	// Fix checksums
	sock.FixIPv4Checksum(packet[:ipHdrLen])
	sock.FixTCPChecksum(packet)
}
