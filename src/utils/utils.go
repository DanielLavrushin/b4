package utils

import (
	crand "crypto/rand"
	"encoding/binary"
	"math/rand"
	"time"
)

func NewRand() *rand.Rand {
	return rand.New(rand.NewSource(time.Now().UnixNano()))
}

func RandUint16() uint16 {
	var b [2]byte
	_, _ = crand.Read(b[:])
	return binary.BigEndian.Uint16(b[:])
}

func RandUint32() uint32 {
	var b [4]byte
	_, _ = crand.Read(b[:])
	return binary.BigEndian.Uint32(b[:])
}
