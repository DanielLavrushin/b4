package discord

import "testing"

func buildIPDiscovery(msgType uint16) []byte {
	pkt := make([]byte, ipDiscoveryLen)
	pkt[0] = byte(msgType >> 8)
	pkt[1] = byte(msgType)
	pkt[2] = byte(ipDiscoveryBodyLen >> 8)
	pkt[3] = byte(ipDiscoveryBodyLen)
	return pkt
}

func buildRTP(firstByte, payloadType byte) []byte {
	pkt := make([]byte, rtpHeaderLen+8)
	pkt[0] = firstByte
	pkt[1] = payloadType
	return pkt
}

func TestIsIPDiscovery(t *testing.T) {
	if !IsIPDiscovery(buildIPDiscovery(ipDiscoveryRequest)) {
		t.Error("request packet should be detected")
	}
	if !IsIPDiscovery(buildIPDiscovery(ipDiscoveryResponse)) {
		t.Error("response packet should be detected")
	}

	wrongLen := buildIPDiscovery(ipDiscoveryRequest)[:73]
	if IsIPDiscovery(wrongLen) {
		t.Error("packet with wrong total length should not match")
	}

	wrongBody := buildIPDiscovery(ipDiscoveryRequest)
	wrongBody[3] = 0x40
	if IsIPDiscovery(wrongBody) {
		t.Error("packet with wrong body length should not match")
	}

	wrongType := buildIPDiscovery(0x0003)
	if IsIPDiscovery(wrongType) {
		t.Error("packet with unknown type should not match")
	}
}

func TestIsRTPMedia(t *testing.T) {
	if !IsRTPMedia(buildRTP(0x80, 0x78)) {
		t.Error("standard opus voice packet should be detected")
	}
	if !IsRTPMedia(buildRTP(0x90, 0x78)) {
		t.Error("opus packet with extension bit should be detected")
	}
	if !IsRTPMedia(buildRTP(0x80, 0xF8)) {
		t.Error("opus packet with marker bit should be detected")
	}

	if IsRTPMedia(buildRTP(0x40, 0x78)) {
		t.Error("non rtp-version-2 packet should not match")
	}
	if IsRTPMedia(buildRTP(0x80, 0x65)) {
		t.Error("non-opus payload type should not match")
	}
	if IsRTPMedia(make([]byte, 4)) {
		t.Error("packet shorter than rtp header should not match")
	}
}

func TestIsVoicePacketRejectsOther(t *testing.T) {
	stunLike := make([]byte, 20)
	stunLike[4] = 0x21
	stunLike[5] = 0x12
	stunLike[6] = 0xA4
	stunLike[7] = 0x42
	if IsVoicePacket(stunLike) {
		t.Error("stun magic-cookie packet should not be treated as discord voice")
	}

	quicLike := make([]byte, 1200)
	quicLike[0] = 0xC0
	if IsVoicePacket(quicLike) {
		t.Error("quic long-header packet should not be treated as discord voice")
	}
}
