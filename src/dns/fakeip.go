package dns

import (
	"encoding/binary"
	"net"
	"strings"
)

func FakeIPAddress(name string, network *net.IPNet) net.IP {
	if network == nil {
		return nil
	}
	base := network.IP.To4()
	if base == nil {
		return nil
	}
	ones, bits := network.Mask.Size()
	if bits != 32 {
		return nil
	}
	hostBits := 32 - ones
	if hostBits < 2 || hostBits > 30 {
		return nil
	}
	size := uint64(1) << uint(hostBits)
	if size <= 2 {
		return nil
	}

	name = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(name), "."))
	if name == "" {
		return nil
	}
	offset := uint64(fnv1a32(name))%(size-2) + 1

	value := uint64(binary.BigEndian.Uint32(base)) + offset
	out := make(net.IP, 4)
	binary.BigEndian.PutUint32(out, uint32(value))
	return out
}

func InFakeIPNetwork(ip net.IP, network *net.IPNet) bool {
	if ip == nil || network == nil {
		return false
	}
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	return network.Contains(v4)
}

func fnv1a32(s string) uint32 {
	var h uint32 = 2166136261
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}
