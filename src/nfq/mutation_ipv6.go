package nfq

import (
	"crypto/rand"
	"encoding/binary"
	"net"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/utils"
)

const ipv6HdrLen = 40

func (w *Worker) MutateClientHelloV6(cfg *config.SetConfig, packet []byte, dst net.IP) []byte {
	if cfg == nil || cfg.Faking.SNIMutation.Mode == config.ConfigOff {
		return packet
	}

	if len(packet) < ipv6HdrLen+20 {
		return packet
	}

	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	payloadStart := ipv6HdrLen + tcpHdrLen

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
		return w.duplicateSNIv6(packet, cfg)
	case "grease":
		return w.addGREASEv6(packet, cfg)
	case "padding":
		return w.addPaddingv6(packet, cfg)
	case "reorder":
		return w.reorderExtensionsv6(packet, cfg)
	case "full":
		return w.fullMutationv6(packet, cfg)
	case "advanced":
		return w.addAdvancedMutationsv6(packet)
	default:
		return packet
	}
}

func (w *Worker) duplicateSNIv6(packet []byte, cfg *config.SetConfig) []byte {
	if cfg == nil {
		return packet
	}

	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	payloadStart := ipv6HdrLen + tcpHdrLen

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
		binary.BigEndian.PutUint16(sniExt[0:2], extServerName)
		binary.BigEndian.PutUint16(sniExt[2:4], uint16(5+len(sni)))
		binary.BigEndian.PutUint16(sniExt[4:6], uint16(3+len(sni)))
		sniExt[6] = 0
		binary.BigEndian.PutUint16(sniExt[7:9], uint16(len(sni)))
		copy(sniExt[9:], sni)

		fakeSNIs = append(fakeSNIs, sniExt...)
	}

	return w.insertExtensionsv6(packet, fakeSNIs)
}

func (w *Worker) addGREASEv6(packet []byte, cfg *config.SetConfig) []byte {
	if cfg == nil {
		return packet
	}

	grease := make([]byte, 0, cfg.Faking.SNIMutation.GreaseCount*8)

	for i := 0; i < cfg.Faking.SNIMutation.GreaseCount; i++ {
		var b [1]byte
		randMutex.Lock()
		rand.Read(b[:])
		randMutex.Unlock()
		greaseVal := greaseValues[b[0]%uint8(len(greaseValues))]

		ext := make([]byte, 8)
		binary.BigEndian.PutUint16(ext[0:2], greaseVal)
		binary.BigEndian.PutUint16(ext[2:4], 4)

		randMutex.Lock()
		rand.Read(ext[4:8])
		randMutex.Unlock()

		grease = append(grease, ext...)
	}

	return w.insertExtensionsv6(packet, grease)
}

func (w *Worker) addPaddingv6(packet []byte, cfg *config.SetConfig) []byte {
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

	if len(packet)+paddingSize+4 > MaxTCPPacketSize {
		paddingSize = MaxTCPPacketSize - len(packet) - 4
		if paddingSize <= 0 {
			return packet
		}
	}

	padding := make([]byte, 4+paddingSize)
	binary.BigEndian.PutUint16(padding[0:2], extPadding)
	binary.BigEndian.PutUint16(padding[2:4], uint16(paddingSize))

	return w.insertExtensionsv6(packet, padding)
}

func (w *Worker) reorderExtensionsv6(packet []byte, cfg *config.SetConfig) []byte {
	if cfg == nil {
		return packet
	}

	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	payloadStart := ipv6HdrLen + tcpHdrLen

	extOffset := w.findExtensionsOffset(packet[payloadStart:])
	if extOffset < 0 {
		return packet
	}

	extensions := w.parseExtensions(packet[payloadStart+extOffset:])
	if len(extensions) < 2 {
		return packet
	}

	var sniExt []byte
	var otherExts [][]byte

	for _, ext := range extensions {
		if len(ext) >= 2 && binary.BigEndian.Uint16(ext[0:2]) == extServerName {
			sniExt = ext
		} else {
			otherExts = append(otherExts, ext)
		}
	}

	for i := len(otherExts) - 1; i > 0; i-- {
		j := int(utils.RandUint32() % uint32(i+1))
		otherExts[i], otherExts[j] = otherExts[j], otherExts[i]
	}

	newExts := make([]byte, 0, 4096)
	if sniExt != nil {
		newExts = append(newExts, sniExt...)
	}
	for _, ext := range otherExts {
		newExts = append(newExts, ext...)
	}

	return w.replaceExtensionsv6(packet, newExts)
}

func (w *Worker) fullMutationv6(packet []byte, cfg *config.SetConfig) []byte {
	if cfg == nil {
		return packet
	}

	mutated := packet

	mutated = w.duplicateSNIv6(mutated, cfg)
	if len(mutated) > MaxTCPPacketSize {
		log.Tracef("Mutation too large after duplicateSNI, aborting")
		return packet
	}

	mutated = w.addCommonExtensionsv6(mutated)
	if len(mutated) > MaxTCPPacketSize {
		log.Tracef("Mutation too large after common extensions")
		return w.duplicateSNIv6(packet, cfg)
	}

	mutated = w.addAdvancedMutationsv6(mutated)
	if len(mutated) > MaxTCPPacketSize {
		log.Tracef("Mutation too large after advanced mutations")
		mutated = w.addCommonExtensionsv6(w.duplicateSNIv6(packet, cfg))
	}

	mutated = w.addGREASEv6(mutated, cfg)
	if len(mutated) > MaxTCPPacketSize {
		log.Tracef("Mutation too large after GREASE")
	}

	mutated = w.addFakeALPNv6(mutated)
	if len(mutated) > MaxTCPPacketSize {
		log.Tracef("Mutation too large after ALPN")
	}

	mutated = w.addUnknownExtensionsv6(mutated, cfg.Faking.SNIMutation.FakeExtCount)

	mutated = w.reorderExtensionsv6(mutated, cfg)

	mutated = w.addPaddingv6(mutated, cfg)

	return mutated
}

func (w *Worker) addFakeALPNv6(packet []byte) []byte {
	protocols := []string{
		"http/1.0", "http/1.1", "h2", "h3",
		"spdy/3", "spdy/3.1",
		"quic", "hq", "doq",
		"xmpp", "mqtt", "amqp",
		"grpc", "websocket",
	}

	alpnData := make([]byte, 0, 256)
	for _, proto := range protocols {
		if len(alpnData)+1+len(proto) > 255 {
			break
		}
		alpnData = append(alpnData, byte(len(proto)))
		alpnData = append(alpnData, proto...)
	}

	alpn := make([]byte, 6+len(alpnData))
	binary.BigEndian.PutUint16(alpn[0:2], extALPN)
	binary.BigEndian.PutUint16(alpn[2:4], uint16(2+len(alpnData)))
	binary.BigEndian.PutUint16(alpn[4:6], uint16(len(alpnData)))
	copy(alpn[6:], alpnData)

	return w.insertExtensionsv6(packet, alpn)
}

func (w *Worker) addUnknownExtensionsv6(packet []byte, count int) []byte {
	unknown := make([]byte, 0, count*8)

	for i := 0; i < count; i++ {
		var extType uint16

		if i < len(commonExtensions) {
			extType = commonExtensions[i]
		} else {
			reservedTypes := []uint16{0x00ff, 0x1234, 0x5678, 0x9abc, 0xfe00, 0xffff}
			extType = reservedTypes[i%len(reservedTypes)]
		}

		ext := make([]byte, 8)
		binary.BigEndian.PutUint16(ext[0:2], extType)
		binary.BigEndian.PutUint16(ext[2:4], 4)

		randMutex.Lock()
		rand.Read(ext[4:8])
		randMutex.Unlock()

		unknown = append(unknown, ext...)
	}

	return w.insertExtensionsv6(packet, unknown)
}

func (w *Worker) addCommonExtensionsv6(packet []byte) []byte {
	extensions := make([]byte, 0, 256)

	groups := []byte{
		0x00, 0x0a,
		0x00, 0x08,
		0x00, 0x06,
		0x00, 0x1d,
		0x00, 0x17,
		0x00, 0x18,
	}
	extensions = append(extensions, groups...)

	sigAlgs := []byte{
		0x00, 0x0d,
		0x00, 0x0e,
		0x00, 0x0c,
		0x04, 0x03,
		0x05, 0x03,
		0x06, 0x03,
		0x04, 0x01,
		0x05, 0x01,
		0x06, 0x01,
	}
	extensions = append(extensions, sigAlgs...)

	versions := []byte{
		0x00, 0x2b,
		0x00, 0x05,
		0x04,
		0x03, 0x04,
		0x03, 0x03,
	}
	extensions = append(extensions, versions...)

	ticket := []byte{
		0x00, 0x23,
		0x00, 0x00,
	}
	extensions = append(extensions, ticket...)

	return w.insertExtensionsv6(packet, extensions)
}

func (w *Worker) addAdvancedMutationsv6(packet []byte) []byte {
	mutated := packet

	pskModes := []byte{
		0x00, 0x2d,
		0x00, 0x02,
		0x01,
		0x01,
	}

	if len(mutated)+len(pskModes) <= MaxTCPPacketSize {
		mutated = w.insertExtensionsv6(mutated, pskModes)
	}

	keyShare := make([]byte, 0, 128)
	keyShare = append(keyShare,
		0x00, 0x33,
		0x00, 0x26,
		0x00, 0x24,
		0x00, 0x1d,
		0x00, 0x20,
	)

	key := make([]byte, 32)
	randMutex.Lock()
	rand.Read(key)
	randMutex.Unlock()
	keyShare = append(keyShare, key...)

	if len(mutated)+len(keyShare) <= MaxTCPPacketSize {
		mutated = w.insertExtensionsv6(mutated, keyShare)
	}

	statusReq := []byte{
		0x00, 0x05,
		0x00, 0x05,
		0x01,
		0x00, 0x00,
		0x00, 0x00,
	}

	if len(mutated)+len(statusReq) <= MaxTCPPacketSize {
		mutated = w.insertExtensionsv6(mutated, statusReq)
	}

	return mutated
}

func (w *Worker) insertExtensionsv6(packet []byte, newExts []byte) []byte {
	if len(packet) < ipv6HdrLen+20 {
		return packet
	}

	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	payloadStart := ipv6HdrLen + tcpHdrLen

	extOffset := w.findExtensionsOffset(packet[payloadStart:])
	if extOffset < 0 {
		return packet
	}

	extPos := payloadStart + extOffset
	if len(packet) < extPos+2 {
		return packet
	}

	currentExtLen := int(binary.BigEndian.Uint16(packet[extPos : extPos+2]))

	if extPos+2+currentExtLen > len(packet) {
		return packet
	}

	if len(packet)+len(newExts) > MaxTCPPacketSize {
		maxNewExts := MaxTCPPacketSize - len(packet)
		if maxNewExts <= 0 {
			return packet
		}
		newExts = newExts[:maxNewExts]
	}

	newPacket := make([]byte, len(packet)+len(newExts))

	copy(newPacket, packet[:extPos])

	newExtLen := currentExtLen + len(newExts)
	binary.BigEndian.PutUint16(newPacket[extPos:extPos+2], uint16(newExtLen))

	copy(newPacket[extPos+2:], packet[extPos+2:extPos+2+currentExtLen])

	copy(newPacket[extPos+2+currentExtLen:], newExts)

	if extPos+2+currentExtLen < len(packet) {
		copy(newPacket[extPos+2+currentExtLen+len(newExts):], packet[extPos+2+currentExtLen:])
	}

	w.updatePacketLengths(newPacket)

	return newPacket
}

func (w *Worker) replaceExtensionsv6(packet []byte, newExts []byte) []byte {
	if len(packet) < ipv6HdrLen+20 {
		return packet
	}

	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	payloadStart := ipv6HdrLen + tcpHdrLen

	extOffset := w.findExtensionsOffset(packet[payloadStart:])
	if extOffset < 0 {
		return packet
	}

	extPos := payloadStart + extOffset
	if len(packet) < extPos+2 {
		return packet
	}

	currentExtLen := int(binary.BigEndian.Uint16(packet[extPos : extPos+2]))

	if extPos+2+currentExtLen > len(packet) {
		return packet
	}

	sizeDiff := len(newExts) - currentExtLen
	newPacketLen := len(packet) + sizeDiff

	if newPacketLen > MaxTCPPacketSize {
		return packet
	}

	newPacket := make([]byte, newPacketLen)

	copy(newPacket, packet[:extPos])

	binary.BigEndian.PutUint16(newPacket[extPos:extPos+2], uint16(len(newExts)))

	copy(newPacket[extPos+2:], newExts)

	if extPos+2+currentExtLen < len(packet) {
		copy(newPacket[extPos+2+len(newExts):], packet[extPos+2+currentExtLen:])
	}

	w.updatePacketLengths(newPacket)

	return newPacket
}
