package sock

import "encoding/binary"

const (
	TCPOptionNOP       = 1
	TCPOptionTimestamp = 8
	TCPTimestampLength = 10
)

func DecreaseTCPTimestamp(packet []byte, decrease uint32, isIPv6 bool) bool {
	var ipHdrLen int
	if isIPv6 {
		ipHdrLen = 40
	} else {
		ipHdrLen = int((packet[0] & 0x0F) * 4)
	}

	if len(packet) < ipHdrLen+20 {
		return false
	}

	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || len(packet) < ipHdrLen+tcpHdrLen {
		return false
	}

	optionsStart := ipHdrLen + 20
	optionsEnd := ipHdrLen + tcpHdrLen

	i := optionsStart
	for i < optionsEnd {
		kind := packet[i]

		if kind == 0 {
			break
		}

		if kind == TCPOptionNOP {
			i++
			continue
		}

		if i+1 >= optionsEnd {
			break
		}

		length := int(packet[i+1])
		if length < 2 || i+length > optionsEnd {
			break
		}

		if kind == TCPOptionTimestamp && length == TCPTimestampLength {
			if i+10 <= optionsEnd {
				tsvalOffset := i + 2
				tsval := binary.BigEndian.Uint32(packet[tsvalOffset : tsvalOffset+4])

				newTSval := tsval - decrease
				binary.BigEndian.PutUint32(packet[tsvalOffset:tsvalOffset+4], newTSval)

				return true
			}
		}

		i += length
	}

	return false
}
