package sock

import (
	"crypto/rand"
	"encoding/binary"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func cloneBytes(src []byte) []byte {
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

func fakeTCPPayload(faking *config.FakingConfig, originalTLS []byte) []byte {
	var fakePayload []byte
	if faking.SNIType == config.FakePayloadInverted && len(originalTLS) > 0 {
		fakePayload = make([]byte, len(originalTLS))
		for i, b := range originalTLS {
			fakePayload[i] = ^b
		}
	} else {
		fakePayload = GetPayload(faking)
	}

	flags := ParseTLSMod(faking.TLSMod)
	if len(faking.TLSMod) > 0 {
		fakePayload = ApplyTLSMod(fakePayload, originalTLS, flags)
	}

	return matchPayloadLength(fakePayload, originalTLS, faking.FakeLenMode, flags)
}

func behindRealData(back int64, fakeLen int) uint32 {
	return uint32(max(back, int64(fakeLen)))
}

func MatchPayloadLength(fakePayload, originalTLS []byte, mode string) []byte {
	return matchPayloadLength(fakePayload, originalTLS, mode, TLSModFlags{})
}

func matchPayloadLength(fakePayload, originalTLS []byte, mode string, flags TLSModFlags) []byte {
	if mode != "match" || len(originalTLS) == 0 || len(fakePayload) == 0 {
		return fakePayload
	}
	if mirrored := MirrorClientHello(originalTLS, fakePayload); mirrored != nil {
		if flags.Randomize {
			redrawHello(mirrored, !flags.DupSessID)
		}
		return mirrored
	}
	target := len(originalTLS)
	if len(fakePayload) == target {
		return fakePayload
	}

	out := make([]byte, target)
	for i := 0; i < target; i++ {
		out[i] = fakePayload[i%len(fakePayload)]
	}
	FixTLSRecordLength(out)
	return out
}

func TCPChecksumValid(packet []byte) bool {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	probe := cloneBytes(packet)
	FixTCPChecksum(probe)
	return probe[ipHdrLen+16] == packet[ipHdrLen+16] && probe[ipHdrLen+17] == packet[ipHdrLen+17]
}

func TCPChecksumValidV6(packet []byte) bool {
	probe := cloneBytes(packet)
	FixTCPChecksumV6(probe)
	return probe[40+16] == packet[40+16] && probe[40+17] == packet[40+17]
}

func CorruptTCPChecksum(packet []byte) {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	packet[ipHdrLen+16] ^= 0xFF
}

func CorruptTCPChecksumV6(packet []byte) {
	packet[40+16] ^= 0xFF
}

const fakeAckLead uint32 = 0x60000000

func moveFakeAck(tcp []byte) {
	if len(tcp) < 12 {
		return
	}
	binary.BigEndian.PutUint32(tcp[8:12], binary.BigEndian.Uint32(tcp[8:12])+fakeAckLead)
}

func FixTLSRecordLength(payload []byte) {
	if len(payload) < 5 || payload[0] != 0x16 {
		return
	}
	recLen := len(payload) - 5
	payload[3] = byte(recLen >> 8)
	payload[4] = byte(recLen)
	if len(payload) >= 9 && payload[5] == 0x01 {
		hsLen := recLen - 4
		payload[6], payload[7], payload[8] = byte(hsLen>>16), byte(hsLen>>8), byte(hsLen)
	}
}

const defaultFakeTTL = 5

func fakeTTLOrDefault(configured uint8) uint8 {
	if configured == 0 {
		return defaultFakeTTL
	}
	return configured
}

func resolveFakeTTL(configured, originalTTL uint8) uint8 {
	ttl := fakeTTLOrDefault(configured)
	if ttl >= originalTTL && originalTTL > 1 {
		ttl = originalTTL - 1
	}
	if ttl < 1 {
		ttl = 1
	}
	return ttl
}

func setDistinctIPID(fake, original []byte) {
	if len(fake) < 6 || len(original) < 6 {
		return
	}
	var r [2]byte
	if _, err := rand.Read(r[:]); err != nil {
		log.Warnf("crypto/rand read failed: %v", err)
		return
	}
	id := uint16(r[0])<<8 | uint16(r[1])
	if id == uint16(original[4])<<8|uint16(original[5]) {
		id++
	}
	fake[4] = byte(id >> 8)
	fake[5] = byte(id)
}

func GetPayload(faking *config.FakingConfig) []byte {
	switch faking.SNIType {
	case config.FakePayloadRandom:
		p := make([]byte, 1200)
		if _, err := rand.Read(p); err != nil {
			log.Warnf("crypto/rand read failed: %v", err)
		}
		return p
	case config.FakePayloadZero:
		return make([]byte, 1200)
	}

	if len(faking.PayloadData) > 0 {
		out := cloneBytes(faking.PayloadData)
		log.Tracef("Using fake SNI payload of %d bytes", len(out))
		return out
	}

	switch faking.SNIType {
	case config.FakePayloadDefault2:
		return cloneBytes(config.FakeSNI2)
	case config.FakePayloadSTUN:
		return cloneBytes(config.FakeSTUN)
	case config.FakePayloadCustom:
		return []byte(faking.CustomPayload)
	}
	return cloneBytes(config.FakeSNI1)
}
