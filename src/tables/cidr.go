package tables

import (
	"net"
	"strings"
)

var (
	zeroPrefixHalvesV4 = []string{"0.0.0.0/1", "128.0.0.0/1"}
	zeroPrefixHalvesV6 = []string{"::/1", "8000::/1"}
)

func zeroPrefixHalves(entry string) []string {
	entry = strings.TrimSpace(entry)
	if !strings.HasSuffix(entry, "/0") {
		return nil
	}

	_, ipNet, err := net.ParseCIDR(entry)
	if err != nil || ipNet == nil {
		return nil
	}

	ones, bits := ipNet.Mask.Size()
	if ones != 0 {
		return nil
	}
	if bits == 32 {
		return zeroPrefixHalvesV4
	}
	return zeroPrefixHalvesV6
}

func expandZeroPrefix(entries []string) []string {
	needed := false
	for _, entry := range entries {
		if zeroPrefixHalves(entry) != nil {
			needed = true
			break
		}
	}
	if !needed {
		return entries
	}

	out := make([]string, 0, len(entries)+2)
	seen := make(map[string]struct{}, len(zeroPrefixHalvesV4)+len(zeroPrefixHalvesV6))

	for _, entry := range entries {
		halves := zeroPrefixHalves(entry)
		if halves == nil {
			out = append(out, entry)
			continue
		}
		for _, half := range halves {
			if _, ok := seen[half]; ok {
				continue
			}
			seen[half] = struct{}{}
			out = append(out, half)
		}
	}

	return out
}
