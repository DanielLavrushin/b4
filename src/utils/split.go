package utils

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

var listURLStart = regexp.MustCompile("^[\"'`]*[A-Za-z][A-Za-z0-9+.-]*://")

func SplitKeepingURLs(raw string, isSep func(rune) bool) []string {
	var out []string
	urlAt, start := -1, 0
	for i := 0; i <= len(raw); {
		r, size := rune(0), 1
		if i < len(raw) {
			r, size = utf8.DecodeRuneInString(raw[i:])
			if !isSep(r) {
				i += size
				continue
			}
		}
		seg := raw[start:i]
		part := strings.TrimSpace(seg)
		lead := len(seg) - len(strings.TrimLeftFunc(seg, unicode.IsSpace))
		if lead > 0 {
			urlAt = -1
		}
		switch {
		case listURLStart.MatchString(part):
			urlAt = start + lead
			out = append(out, part)
		case part == "":
		case urlAt >= 0:
			out[len(out)-1] = raw[urlAt:i]
		default:
			out = append(out, part)
		}
		if len(seg) > len(strings.TrimRightFunc(seg, unicode.IsSpace)) || unicode.IsSpace(r) {
			urlAt = -1
		}
		start = i + size
		i += size
	}
	return out
}
