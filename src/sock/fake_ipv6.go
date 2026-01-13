package sock

import (
	"crypto/rand"
	"encoding/binary"

	"github.com/daniellavrushin/b4/config"
)

// BuildFakeSNIPacketV6 creates a fake SNI packet for IPv6
func BuildFakeSNIPacketV6(original []byte, cfg *config.SetConfig) []byte {
	if len(original) < 60 || original[0]>>4 != 6 {
		return nil
	}

	// IPv6 header is fixed 40 bytes
	ipv6HdrLen := 40
	tcpHdrLen := int((original[ipv6HdrLen+12] >> 4) * 4)

	var originalTLS []byte
	if len(original) > ipv6HdrLen+tcpHdrLen {
		originalTLS = original[ipv6HdrLen+tcpHdrLen:]
	}

	var fakePayload = GetPayload(&cfg.Faking)

	if len(cfg.Faking.TLSMod) > 0 {
		flags := ParseTLSMod(cfg.Faking.TLSMod)
		fakePayload = ApplyTLSMod(fakePayload, originalTLS, flags)
	}

	fakeLen := ipv6HdrLen + tcpHdrLen + len(fakePayload)
	fake := make([]byte, fakeLen)
	copy(fake[:ipv6HdrLen+tcpHdrLen], original[:ipv6HdrLen+tcpHdrLen])
	copy(fake[ipv6HdrLen+tcpHdrLen:], fakePayload)

	// Update IPv6 payload length field (bytes 4-5)
	payloadLen := tcpHdrLen + len(fakePayload)
	binary.BigEndian.PutUint16(fake[4:6], uint16(payloadLen))

	off := cfg.Faking.SeqOffset
	if off <= 0 {
		off = 10000
	}

	switch cfg.Faking.Strategy {
	case "ttl":
		// IPv6 uses hop limit (byte 7) instead of TTL
		fake[7] = cfg.Faking.TTL
	case "pastseq":
		off := uint32(cfg.Faking.SeqOffset)
		if off == 0 {
			off = 8192
		}
		seq := binary.BigEndian.Uint32(fake[ipv6HdrLen+4 : ipv6HdrLen+8])
		binary.BigEndian.PutUint32(fake[ipv6HdrLen+4:ipv6HdrLen+8], seq-off)
	case "randseq":
		dlen := len(original) - ipv6HdrLen - tcpHdrLen
		if cfg.Faking.SeqOffset == 0 {
			var r [4]byte
			rand.Read(r[:])
			binary.BigEndian.PutUint32(fake[ipv6HdrLen+4:ipv6HdrLen+8], binary.BigEndian.Uint32(r[:]))
		} else {
			seq := binary.BigEndian.Uint32(fake[ipv6HdrLen+4 : ipv6HdrLen+8])
			off := uint32(cfg.Faking.SeqOffset) + uint32(dlen)
			binary.BigEndian.PutUint32(fake[ipv6HdrLen+4:ipv6HdrLen+8], seq-off)
		}
	case "tcp_check":
	default:
	}

	FixTCPChecksumV6(fake)

	if cfg.Faking.Strategy == "tcp_check" {
		fake[ipv6HdrLen+16] ^= 0xFF
	}

	return fake
}

// FixTCPChecksumv6 calculates and sets the TCP checksum for IPv6 packets
func FixTCPChecksumV6(packet []byte) {
	if len(packet) < 40 {
		return
	}

	ipv6HdrLen := 40
	tcpOffset := ipv6HdrLen

	// Clear existing checksum
	packet[tcpOffset+16] = 0
	packet[tcpOffset+17] = 0

	// Get payload length from IPv6 header
	payloadLen := int(binary.BigEndian.Uint16(packet[4:6]))

	// Build IPv6 pseudo-header for checksum calculation
	// Source address (16 bytes) + Destination address (16 bytes) +
	// TCP length (4 bytes) + zeros (3 bytes) + next header (1 byte)
	pseudo := make([]byte, 40)
	copy(pseudo[0:16], packet[8:24])   // Source address
	copy(pseudo[16:32], packet[24:40]) // Destination address
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(payloadLen))
	pseudo[39] = 6 // Next header = TCP

	var sum uint32

	// Sum pseudo-header
	for i := 0; i < len(pseudo); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(pseudo[i : i+2]))
	}

	// Sum TCP segment
	tcp := packet[tcpOffset : tcpOffset+payloadLen]
	for i := 0; i+1 < len(tcp); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(tcp[i : i+2]))
	}
	if len(tcp)%2 == 1 {
		sum += uint32(tcp[len(tcp)-1]) << 8
	}

	// Fold carries
	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}

	checksum := ^uint16(sum)
	binary.BigEndian.PutUint16(packet[tcpOffset+16:tcpOffset+18], checksum)
}

func FixUDPChecksumV6(packet []byte) {
	if len(packet) < 48 { // 40 (IPv6) + 8 (UDP min)
		return
	}

	ipv6HdrLen := 40
	udpOffset := ipv6HdrLen

	// Clear existing checksum
	packet[udpOffset+6] = 0
	packet[udpOffset+7] = 0

	// Get payload length from IPv6 header
	payloadLen := int(binary.BigEndian.Uint16(packet[4:6]))

	pseudo := make([]byte, 40)
	copy(pseudo[0:16], packet[8:24])   // Source address
	copy(pseudo[16:32], packet[24:40]) // Destination address
	binary.BigEndian.PutUint32(pseudo[32:36], uint32(payloadLen))
	pseudo[39] = 17 // Next header = UDP

	var sum uint32

	for i := 0; i < len(pseudo); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(pseudo[i : i+2]))
	}

	udp := packet[udpOffset : udpOffset+payloadLen]
	for i := 0; i+1 < len(udp); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(udp[i : i+2]))
	}
	if len(udp)%2 == 1 {
		sum += uint32(udp[len(udp)-1]) << 8
	}

	for sum > 0xffff {
		sum = (sum >> 16) + (sum & 0xffff)
	}

	checksum := ^uint16(sum)

	if checksum == 0 {
		checksum = 0xffff
	}

	binary.BigEndian.PutUint16(packet[udpOffset+6:udpOffset+8], checksum)
}
