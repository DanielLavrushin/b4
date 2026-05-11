package config

import (
	"fmt"
	"math"
	"os"
	"runtime/debug"
	"strconv"
	"strings"
	"syscall"
)

const memoryLimitFloor = 32 * 1024 * 1024 // 32 MiB

func ParseMemoryLimit(s string) (int64, error) {
	v := strings.TrimSpace(s)
	if v == "" || strings.EqualFold(v, "auto") {
		return autoMemoryLimit(), nil
	}
	if v == "0" || strings.EqualFold(v, "off") || strings.EqualFold(v, "disabled") {
		return math.MaxInt64, nil
	}

	// strip optional trailing "B" / "iB"
	low := strings.ToLower(v)
	low = strings.TrimSuffix(low, "ib")
	if !strings.HasSuffix(low, "b") || hasUnitSuffix(low) {
		low = strings.TrimSuffix(low, "b")
	}

	mult := int64(1)
	switch {
	case strings.HasSuffix(low, "k"):
		mult = 1 << 10
		low = strings.TrimSuffix(low, "k")
	case strings.HasSuffix(low, "m"):
		mult = 1 << 20
		low = strings.TrimSuffix(low, "m")
	case strings.HasSuffix(low, "g"):
		mult = 1 << 30
		low = strings.TrimSuffix(low, "g")
	}

	low = strings.TrimSpace(low)
	n, err := strconv.ParseFloat(low, 64)
	if err != nil || n <= 0 {
		return 0, fmt.Errorf("invalid memory limit %q (use e.g. 128MiB, 256m, 1g, auto, off)", s)
	}
	limit := int64(n * float64(mult))
	if limit < memoryLimitFloor {
		limit = memoryLimitFloor
	}
	return limit, nil
}

func hasUnitSuffix(s string) bool {
	if len(s) < 2 {
		return false
	}
	c := s[len(s)-2]
	return c == 'k' || c == 'm' || c == 'g'
}

func autoMemoryLimit() int64 {
	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return memoryLimitFloor
	}
	totalRAM := uint64(info.Totalram) * uint64(info.Unit)
	limit := int64(totalRAM / 2)
	if limit < memoryLimitFloor {
		limit = memoryLimitFloor
	}
	return limit
}

// ApplyMemoryLimit applies s as GOMEMLIMIT to the Go runtime.
// Env var GOMEMLIMIT, if set, always wins and this is a no-op.
func ApplyMemoryLimit(s string) (int64, error) {
	if os.Getenv("GOMEMLIMIT") != "" {
		return 0, nil
	}
	limit, err := ParseMemoryLimit(s)
	if err != nil {
		return 0, err
	}
	debug.SetMemoryLimit(limit)
	return limit, nil
}
