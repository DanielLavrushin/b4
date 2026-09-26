package config

import (
	"strconv"
	"strings"
)

type asnRange struct {
	lo, hi uint64
}

var reservedASNRanges = []asnRange{
	{0, 0},
	{23456, 23456},
	{64496, 131071},
	{4200000000, 4294967295},
}

func NormalizeASN(raw string) (string, bool) {
	s := strings.TrimSpace(raw)
	switch {
	case len(s) >= 3 && strings.EqualFold(s[:3], "ASN"):
		s = s[3:]
	case len(s) >= 2 && strings.EqualFold(s[:2], "AS"):
		s = s[2:]
	}
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return "", false
		}
	}
	n, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return "", false
	}
	for _, r := range reservedASNRanges {
		if n >= r.lo && n <= r.hi {
			return "", false
		}
	}
	return strconv.FormatUint(n, 10), true
}

func NormalizeASNs(raw []string) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, entry := range raw {
		id, ok := NormalizeASN(entry)
		if !ok {
			continue
		}
		if _, dup := seen[id]; dup {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func asnKey(n uint32) string {
	return strconv.FormatUint(uint64(n), 10)
}

func asnNumber(id string) uint32 {
	n, _ := strconv.ParseUint(id, 10, 32)
	return uint32(n)
}
