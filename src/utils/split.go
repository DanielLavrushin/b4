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
	urlAt, urlEnd, authEnd, lastAt, start := -1, 0, 0, -1, 0
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
			authEnd, lastAt = urlAuthority(raw, urlAt)
			out = append(out, part)
		case part == "":
		case lead == 0 && urlAt >= 0 && !urlQuoteClosed(raw, urlAt, urlEnd) && (start > authEnd || lastAt >= start):
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

func urlAuthority(raw string, urlAt int) (end, lastAt int) {
	from := urlAt + strings.Index(raw[urlAt:], "://") + 3
	end = len(raw)
	if n := strings.IndexFunc(raw[from:], func(r rune) bool { return strings.ContainsRune("/?#", r) || unicode.IsSpace(r) }); n >= 0 {
		end = from + n
	}
	lastAt = -1
	if n := strings.LastIndexByte(raw[from:end], '@'); n >= 0 {
		lastAt = from + n
	}
	return end, lastAt
}

func urlQuoteClosed(raw string, urlAt, urlEnd int) bool {
	q := raw[urlAt]
	return strings.IndexByte(listQuotes, q) >= 0 && urlEnd-urlAt > 1 && raw[urlEnd-1] == q
}
