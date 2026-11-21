package sock

import (
	"crypto/rand"
	"encoding/binary"
	"strings"
)

type TLSModFlags struct {
	Randomize bool
	DupSessID bool
}

func ParseTLSMod(mods []string) TLSModFlags {
	flags := TLSModFlags{}
	for _, mod := range mods {
		switch strings.ToLower(strings.TrimSpace(mod)) {
		case "rnd":
			flags.Randomize = true
		case "dupsid":
			flags.DupSessID = true
		}
	}
	return flags
}

func ExtractSessionID(payload []byte) []byte {
	if len(payload) < 5 || payload[0] != 0x16 {
		return nil
	}

	hs := payload[5:]
	if len(hs) < 4 || hs[0] != 0x01 {
		return nil
	}

	pos := 4 + 2 + 32
	if pos >= len(hs) {
		return nil
	}

	sidLen := int(hs[pos])
	pos++
	if sidLen == 0 || pos+sidLen > len(hs) || int(binary.BigEndian.Uint16(payload[3:5])) < pos+sidLen {
		return nil
	}

	sid := make([]byte, sidLen)
	copy(sid, hs[pos:pos+sidLen])
	return sid
}

func ApplyTLSMod(fakePayload, originalPayload []byte, flags TLSModFlags) []byte {
	if len(fakePayload) < 5 || fakePayload[0] != 0x16 {
		return fakePayload
	}

	recLen := int(binary.BigEndian.Uint16(fakePayload[3:5]))
	if len(fakePayload) < 5+recLen {
		return fakePayload
	}

	hsStart := 5
	hs := fakePayload[hsStart:]
	if len(hs) < 4 || hs[0] != 0x01 {
		return fakePayload
	}

	if flags.Randomize {
		redrawHello(fakePayload, true)
	}

	if flags.DupSessID && originalPayload != nil {
		origSID := ExtractSessionID(originalPayload)
		if len(origSID) > 0 {
			fakePayload = injectSessionID(fakePayload, origSID)
		}
	}

	return fakePayload
}

func redrawHello(hello []byte, sessionID bool) {
	if len(hello) < 43 || hello[0] != 0x16 || hello[5] != 0x01 {
		return
	}
	rand.Read(hello[11:43])
	if !sessionID || len(hello) < 44 {
		return
	}
	if end := 44 + int(hello[43]); end <= len(hello) {
		rand.Read(hello[44:end])
	}
}

func injectSessionID(fakePayload, newSID []byte) []byte {
	if len(fakePayload) < 5 || fakePayload[0] != 0x16 {
		return fakePayload
	}

	hsStart := 5
	hs := fakePayload[hsStart:]
	if len(hs) < 39 || hs[0] != 0x01 {
		return fakePayload
	}

	sidLenPos := 38
	if sidLenPos >= len(hs) {
		return fakePayload
	}

	oldSIDLen := int(hs[sidLenPos])
	newSIDLen := len(newSID)

	oldSIDEnd := hsStart + sidLenPos + 1 + oldSIDLen
	if oldSIDEnd > len(fakePayload) {
		return fakePayload
	}

	sizeDiff := newSIDLen - oldSIDLen
	newLen := len(fakePayload) + sizeDiff
	newPayload := make([]byte, newLen)

	copy(newPayload, fakePayload[:hsStart+sidLenPos])

	newPayload[hsStart+sidLenPos] = byte(newSIDLen)

	copy(newPayload[hsStart+sidLenPos+1:], newSID)

	copy(newPayload[hsStart+sidLenPos+1+newSIDLen:], fakePayload[oldSIDEnd:])

	newRecLen := int(binary.BigEndian.Uint16(newPayload[3:5])) + sizeDiff
	binary.BigEndian.PutUint16(newPayload[3:5], uint16(newRecLen))

	hsLen := int(newPayload[6])<<16 | int(newPayload[7])<<8 | int(newPayload[8])
	hsLen += sizeDiff
	newPayload[6] = byte(hsLen >> 16)
	newPayload[7] = byte(hsLen >> 8)
	newPayload[8] = byte(hsLen)

	return newPayload
}
