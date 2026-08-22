package sock

import (
	"encoding/binary"
)

// StripSACKFromTCP removes SACK from TCP options in packet
func StripSACKFromTCP(packet []byte) []byte {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)

	if tcpHdrLen <= 20 {
		return packet // No options
	}

	optStart := ipHdrLen + 20
	optEnd := ipHdrLen + tcpHdrLen
	i := optStart

	newOpts := []byte{}

	for i < optEnd {
		kind := packet[i]

		if kind == 0 { // End of options
			break
		}
		if kind == 1 { // NOP
			newOpts = append(newOpts, 1)
			i++
			continue
		}

		if i+1 >= optEnd {
			break
		}

		length := int(packet[i+1])
		if length < 2 || i+length > optEnd {
			break
		}

		// SACK = kind 5, SACK-Permitted = kind 4
		if kind != 4 && kind != 5 {
			newOpts = append(newOpts, packet[i:i+length]...)
		}

		i += length
	}

	// Rebuild packet
	newTCPHdrLen := 20 + len(newOpts)
	newTCPHdrLen = (newTCPHdrLen + 3) &^ 3 // Align to 4 bytes
	for len(newOpts) < newTCPHdrLen-20 {
		newOpts = append(newOpts, 0) // Pad with EOL
	}

	newPkt := make([]byte, ipHdrLen+newTCPHdrLen+len(packet)-(ipHdrLen+tcpHdrLen))
	copy(newPkt[:ipHdrLen], packet[:ipHdrLen])
	copy(newPkt[ipHdrLen:ipHdrLen+20], packet[ipHdrLen:ipHdrLen+20])
	copy(newPkt[ipHdrLen+20:ipHdrLen+newTCPHdrLen], newOpts)
	copy(newPkt[ipHdrLen+newTCPHdrLen:], packet[ipHdrLen+tcpHdrLen:])

	// Update data offset
	newPkt[ipHdrLen+12] = byte((newTCPHdrLen / 4) << 4)

	// Update lengths
	binary.BigEndian.PutUint16(newPkt[2:4], uint16(len(newPkt)))
	FixIPv4Checksum(newPkt[:ipHdrLen])
	FixTCPChecksum(newPkt)

	return newPkt
}

func StripSACKFromTCPv6(packet []byte) []byte {
	ipv6HdrLen := 40
	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)

	if tcpHdrLen <= 20 {
		return packet
	}

	optStart := ipv6HdrLen + 20
	optEnd := ipv6HdrLen + tcpHdrLen
	i := optStart

	newOpts := []byte{}

	for i < optEnd {
		kind := packet[i]

		if kind == 0 {
			break
		}
		if kind == 1 {
			newOpts = append(newOpts, 1)
			i++
			continue
		}

		if i+1 >= optEnd {
			break
		}

		length := int(packet[i+1])
		if length < 2 || i+length > optEnd {
			break
		}

		// Skip SACK (5) and SACK-Permitted (4)
		if kind != 4 && kind != 5 {
			newOpts = append(newOpts, packet[i:i+length]...)
		}

		i += length
	}

	// Rebuild
	newTCPHdrLen := 20 + len(newOpts)
	newTCPHdrLen = (newTCPHdrLen + 3) &^ 3
	for len(newOpts) < newTCPHdrLen-20 {
		newOpts = append(newOpts, 0)
	}

	newPkt := make([]byte, ipv6HdrLen+newTCPHdrLen+len(packet)-(ipv6HdrLen+tcpHdrLen))
	copy(newPkt[:ipv6HdrLen], packet[:ipv6HdrLen])
	copy(newPkt[ipv6HdrLen:ipv6HdrLen+20], packet[ipv6HdrLen:ipv6HdrLen+20])
	copy(newPkt[ipv6HdrLen+20:ipv6HdrLen+newTCPHdrLen], newOpts)
	copy(newPkt[ipv6HdrLen+newTCPHdrLen:], packet[ipv6HdrLen+tcpHdrLen:])

	newPkt[ipv6HdrLen+12] = byte((newTCPHdrLen / 4) << 4)

	binary.BigEndian.PutUint16(newPkt[4:6], uint16(len(newPkt)-ipv6HdrLen))
	FixTCPChecksumV6(newPkt)

	return newPkt
}
