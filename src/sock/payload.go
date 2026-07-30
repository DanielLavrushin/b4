package sock

import (
	"crypto/rand"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func cloneBytes(src []byte) []byte {
	dst := make([]byte, len(src))
	copy(dst, src)
	return dst
}

func MatchPayloadLength(fakePayload, originalTLS []byte, mode string) []byte {
	if mode != "match" || len(originalTLS) == 0 || len(fakePayload) == 0 {
		return fakePayload
	}
	target := len(originalTLS)
	if len(fakePayload) == target {
		return fakePayload
	}

	out := make([]byte, target)
	for i := 0; i < target; i++ {
		out[i] = fakePayload[i%len(fakePayload)]
	}
	fixTLSRecordLength(out)
	return out
}

func fixTLSRecordLength(payload []byte) {
	if len(payload) < 5 || payload[0] != 0x16 {
		return
	}
	payload[3] = byte((len(payload) - 5) >> 8)
	payload[4] = byte(len(payload) - 5)
}

func resolveFakeTTL(configured, originalTTL uint8) uint8 {
	ttl := configured
	if ttl == 0 {
		ttl = 5
	}
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
	case config.FakePayloadCustom:
		return []byte(faking.CustomPayload)
	}
	return cloneBytes(config.FakeSNI1)
}
