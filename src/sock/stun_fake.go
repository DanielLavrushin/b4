package sock

import (
	crand "crypto/rand"
	"encoding/binary"
)

const (
	stunHeaderLen      = 20
	stunAttrHeaderLen  = 4
	stunBindingRequest = 0x0001
	stunMagicCookie    = 0x2112A442
	stunPaddingAttr    = 0x0026
)

func stunFakeLenValid(size int) bool {
	return size == stunHeaderLen || (size >= stunHeaderLen+stunAttrHeaderLen && size%4 == 0)
}

func BuildSTUNRequest(size int) []byte {
	if !stunFakeLenValid(size) {
		return nil
	}
	out := make([]byte, size)

	binary.BigEndian.PutUint16(out[0:2], stunBindingRequest)
	binary.BigEndian.PutUint16(out[2:4], uint16(size-stunHeaderLen))
	binary.BigEndian.PutUint32(out[4:8], stunMagicCookie)
	if _, err := crand.Read(out[8:stunHeaderLen]); err != nil {
		return nil
	}

	if size > stunHeaderLen {
		binary.BigEndian.PutUint16(out[stunHeaderLen:stunHeaderLen+2], stunPaddingAttr)
		binary.BigEndian.PutUint16(out[stunHeaderLen+2:stunHeaderLen+4], uint16(size-stunHeaderLen-stunAttrHeaderLen))
		if _, err := crand.Read(out[stunHeaderLen+stunAttrHeaderLen:]); err != nil {
			return nil
		}
	}

	return out
}
