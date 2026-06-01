package sock

import (
	"encoding/binary"

	"github.com/daniellavrushin/b4/utils"
)

// IPv6FragmentPacket creates IPv6 fragments using Fragment extension headers
// This implements true IPv6 fragmentation (IP-level)
func IPv6FragmentPacket(packet []byte, splitPos int) ([][]byte, bool) {
	if len(packet) < 40 || packet[0]>>4 != 6 {
		return nil, false
	}

	ipv6HdrLen := 40
	nextHeader := packet[6]

	// Only handle plain TCP/UDP (no extension headers for now)
	if nextHeader != 6 && nextHeader != 17 {
		return nil, false
	}

	payloadLen := int(binary.BigEndian.Uint16(packet[4:6]))
	if payloadLen < 8 || ipv6HdrLen+payloadLen > len(packet) {
		return nil, false
	}

	// Align split position to 8-byte boundary (required by IPv6 fragmentation)
	splitPos = (splitPos + 7) &^ 7
	if splitPos >= payloadLen {
		splitPos = payloadLen - 8
		if splitPos < 8 {
			return nil, false
		}
	}

	fragHdrLen := 8
	var identification uint32 = utils.RandUint32()

	// First fragment
	frag1Len := ipv6HdrLen + fragHdrLen + splitPos
	frag1 := make([]byte, frag1Len)

	// Copy IPv6 header
	copy(frag1, packet[:ipv6HdrLen])
	// Change next header to Fragment (44)
	frag1[6] = 44
	// Update payload length
	binary.BigEndian.PutUint16(frag1[4:6], uint16(fragHdrLen+splitPos))

	// Build fragment header for first fragment
	fragHdr1 := frag1[ipv6HdrLen : ipv6HdrLen+fragHdrLen]
	fragHdr1[0] = nextHeader                            // Next header (original protocol)
	fragHdr1[1] = 0                                     // Reserved
	binary.BigEndian.PutUint16(fragHdr1[2:4], 0|0x0001) // Offset 0, M flag set
	binary.BigEndian.PutUint32(fragHdr1[4:8], identification)

	// Copy first part of payload
	copy(frag1[ipv6HdrLen+fragHdrLen:], packet[ipv6HdrLen:ipv6HdrLen+splitPos])

	// Second fragment
	remainingLen := payloadLen - splitPos
	frag2Len := ipv6HdrLen + fragHdrLen + remainingLen
	frag2 := make([]byte, frag2Len)

	// Copy IPv6 header
	copy(frag2, packet[:ipv6HdrLen])
	// Change next header to Fragment (44)
	frag2[6] = 44
	// Update payload length
	binary.BigEndian.PutUint16(frag2[4:6], uint16(fragHdrLen+remainingLen))

	// Build fragment header for second fragment
	fragHdr2 := frag2[ipv6HdrLen : ipv6HdrLen+fragHdrLen]
	fragHdr2[0] = nextHeader                                  // Next header (original protocol)
	fragHdr2[1] = 0                                           // Reserved
	offsetUnits := uint16(splitPos / 8)                       // Offset in 8-byte units
	binary.BigEndian.PutUint16(fragHdr2[2:4], offsetUnits<<3) // Offset, M flag not set
	binary.BigEndian.PutUint32(fragHdr2[4:8], identification)

	// Copy remaining payload
	copy(frag2[ipv6HdrLen+fragHdrLen:], packet[ipv6HdrLen+splitPos:])

	return [][]byte{frag1, frag2}, true
}
