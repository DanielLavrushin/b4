package discord

import "encoding/binary"

const (
	ipDiscoveryLen      = 74
	ipDiscoveryBodyLen  = 70
	ipDiscoveryRequest  = 0x0001
	ipDiscoveryResponse = 0x0002

	rtpHeaderLen       = 12
	rtpVersionMask     = 0xC0
	rtpVersionTwo      = 0x80
	rtpPayloadTypeMask = 0x7F
	rtpPayloadTypeOpus = 0x78
)

func IsVoicePacket(data []byte) bool {
	return IsIPDiscovery(data) || IsRTPMedia(data)
}

func IsIPDiscovery(data []byte) bool {
	if len(data) != ipDiscoveryLen {
		return false
	}
	msgType := binary.BigEndian.Uint16(data[0:2])
	if msgType != ipDiscoveryRequest && msgType != ipDiscoveryResponse {
		return false
	}
	return binary.BigEndian.Uint16(data[2:4]) == ipDiscoveryBodyLen
}

func IsRTPMedia(data []byte) bool {
	if len(data) < rtpHeaderLen {
		return false
	}
	if data[0]&rtpVersionMask != rtpVersionTwo {
		return false
	}
	return data[1]&rtpPayloadTypeMask == rtpPayloadTypeOpus
}
