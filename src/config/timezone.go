package config

import (
	"fmt"
	"os"
	"strings"
	"time"
)

var systemLocation = time.Local

// ApplyTimezone sets time.Local to the given timezone name.
// Supports IANA names (e.g. "Europe/Oslo") and fixed UTC offsets
// (e.g. "UTC+1", "UTC-5", "UTC+5:30").
func ApplyTimezone(tzName string) {
	if tzName == "" {
		if env := os.Getenv("TZ"); env != "" {
			ApplyTimezone(env)
			return
		}
		time.Local = systemLocation
		fmt.Fprintf(os.Stderr, "[INIT] Timezone reset to system default (%s)\n", systemLocation.String())
		return
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = parseFixedOffset(tzName)
		if loc == nil {
			fmt.Fprintf(os.Stderr, "[WARN] Failed to load timezone %s: %v, using UTC\n", tzName, err)
			loc, _ = time.LoadLocation("UTC")
		}
	}

	time.Local = loc
	fmt.Fprintf(os.Stderr, "[INIT] Timezone set to %s\n", loc.String())
}

func parseFixedOffset(name string) *time.Location {
	if strings.HasPrefix(name, "UTC") {
		return parseOffsetFrom(name, name[3:])
	}

	i := 0
	for i < len(name) && ((name[i] >= 'A' && name[i] <= 'Z') || (name[i] >= 'a' && name[i] <= 'z')) {
		i++
	}
	if i >= 2 && i < len(name) {
		return parseOffsetFrom(name, invertSign(name[i:]))
	}

	return nil
}

func parseOffsetFrom(name, rest string) *time.Location {
	if rest == "" {
		loc, _ := time.LoadLocation("UTC")
		return loc
	}

	sign := 1
	switch rest[0] {
	case '+':
		rest = rest[1:]
	case '-':
		sign = -1
		rest = rest[1:]
	default:
		return nil
	}

	hours, minutes := 0, 0
	if strings.Contains(rest, ":") {
		if _, err := fmt.Sscanf(rest, "%d:%d", &hours, &minutes); err != nil {
			return nil
		}
	} else {
		if _, err := fmt.Sscanf(rest, "%d", &hours); err != nil {
			return nil
		}
	}

	if hours < 0 || hours > 14 || minutes < 0 || minutes >= 60 {
		return nil
	}

	offset := sign * (hours*3600 + minutes*60)
	return time.FixedZone(name, offset)
}

// invertSign flips the leading +/- in an offset string (for POSIX TZ conversion).
func invertSign(s string) string {
	if len(s) == 0 {
		return s
	}
	switch s[0] {
	case '+':
		return "-" + s[1:]
	case '-':
		return "+" + s[1:]
	}
	return s
}
