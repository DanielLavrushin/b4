package mtproto

import (
	"strconv"
	"sync/atomic"
)

const logTag = "tg-bridge"

var connSeq atomic.Uint64

func nextConnID() string {
	return strconv.FormatUint(connSeq.Add(1), 36)
}

func tg(id string) string {
	if id == "" {
		return "[" + logTag + "]"
	}
	return "[" + logTag + " c=" + id + "]"
}
