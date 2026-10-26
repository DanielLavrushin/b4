package utils

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

const listQuotes = "\"'`"

var listURLStart = regexp.MustCompile("^[\"'`]*[A-Za-z][A-Za-z0-9+.-]*://")

func SplitKeepingURLs(raw string, isSep func(rune) bool) []string {
	var out []string
	urlAt, urlEnd, start := -1, 0, 0
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
		switch {
		case listURLStart.MatchString(part):
			urlAt, urlEnd = start+lead, start+lead+len(part)
			out = append(out, part)
		case part == "":
		case lead == 0 && urlAt >= 0 && urlContinues(raw[urlAt:urlEnd], raw[start:]):
			urlEnd = i
			out[len(out)-1] = raw[urlAt:i]
		default:
			urlAt = -1
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

func urlContinues(soFar, rest string) bool {
	if q := soFar[0]; strings.IndexByte(listQuotes, q) >= 0 && len(soFar) > 1 && soFar[len(soFar)-1] == q {
		return false
	}
	if strings.ContainsAny(soFar[strings.Index(soFar, "://")+3:], "/?#") {
		return true
	}
	end := strings.IndexFunc(rest, func(r rune) bool { return strings.ContainsRune("/?#", r) || unicode.IsSpace(r) })
	if end < 0 {
		end = len(rest)
	}
	return strings.Contains(rest[:end], "@")
}
