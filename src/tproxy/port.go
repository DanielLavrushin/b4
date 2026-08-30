package tproxy

import (
	"hash/fnv"

	"github.com/daniellavrushin/b4/config"
)

const (
	DefaultPortBase = 13000
	PortRange       = 50000
	MarkBase        = 0x20000
	MarkRange       = 0x7E00
)

func MarkForSet(setID string, pinned uint32) uint32 {
	if MarkIsUsable(pinned) {
		return pinned
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(setID))
	return MarkBase + (h.Sum32() % MarkRange)
}

func PortFor(mark uint32) int {
	if mark == 0 {
		return DefaultPortBase
	}
	return DefaultPortBase + int(mark%PortRange)
}

func MarkIsUsable(mark uint32) bool {
	return mark > 0 && mark&^config.PerSetRouteMarkBits == 0
}

func InMarkRange(mark uint32) bool {
	return mark >= MarkBase && mark < MarkBase+MarkRange
}
