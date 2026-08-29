package convert

import (
	"regexp"
	"strings"
)

var posHintRe = regexp.MustCompile(`^-{1,2}[A-Za-z-]*=?[+-]?[0-9]+(:[0-9]+)+|\+[shn][emrs]`)

type usage struct {
	shorts map[string]bool
	longs  map[string]bool
	all    []string
	count  int
}

func scanUsage(argv []string) usage {
	u := usage{shorts: map[string]bool{}, longs: map[string]bool{}}
	for _, a := range argv {
		switch {
		case strings.HasPrefix(a, "--"):
			name, _, _ := strings.Cut(a[2:], "=")
			if name == "" {
				continue
			}
			u.longs[name] = true
			u.all = append(u.all, "--"+name)
			u.count++
		case len(a) > 1 && a[0] == '-':
			c := string(a[1])
			u.shorts[c] = true
			u.all = append(u.all, "-"+c)
			u.count++
		}
	}
	return u
}

func (u usage) has(marker string) bool {
	if strings.HasPrefix(marker, "--") {
		return u.longs[marker[2:]]
	}
	if strings.HasPrefix(marker, "-") && len(marker) == 2 {
		return u.shorts[marker[1:]]
	}
	return false
}

func containsWord(haystack, word string) bool {
	for i := 0; ; {
		j := strings.Index(haystack[i:], word)
		if j < 0 {
			return false
		}
		j += i
		if (j == 0 || !isWordByte(haystack[j-1])) &&
			(j+len(word) == len(haystack) || !isWordByte(haystack[j+len(word)])) {
			return true
		}
		i = j + 1
	}
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func specKnows(s *Spec, marker string) bool {
	for _, o := range s.Options {
		if strings.HasPrefix(marker, "--") {
			if o.Long == marker[2:] {
				return true
			}
			continue
		}
		if len(marker) == 2 && o.Short == marker[1:] {
			return true
		}
	}
	return false
}

func detectTool(input string, argv []string, all map[string]*Spec) (*Spec, float64) {
	u := scanUsage(argv)
	lower := strings.ToLower(input)
	var best *Spec
	bestScore := -1.0
	for _, s := range all {
		rejected := false
		for _, m := range s.Detect.Reject {
			if u.has(m) {
				rejected = true
				break
			}
		}
		if rejected {
			continue
		}
		known := 0
		for _, m := range u.all {
			if specKnows(s, m) {
				known++
			}
		}
		score := 0.0
		if u.count > 0 {
			score = float64(known) / float64(u.count)
		}
		for _, sig := range s.Detect.Signature {
			if u.has(sig) {
				score += 0.15
			}
		}
		for _, m := range s.Detect.Markers {
			if containsWord(lower, strings.ToLower(m)) {
				score += 0.25
			}
		}
		if score > bestScore {
			best, bestScore = s, score
		}
	}
	if bestScore > 1 {
		bestScore = 1
	}
	if bestScore < 0 {
		bestScore = 0
	}
	return best, bestScore
}

func detectVersion(s *Spec, argv []string) (string, bool) {
	u := scanUsage(argv)
	posHint := false
	for _, a := range argv {
		if posHintRe.MatchString(a) || strings.Contains(a, "+sm") || strings.Contains(a, "+se") ||
			strings.Contains(a, "+sr") || strings.Contains(a, "+hm") || strings.Contains(a, "+n") {
			posHint = true
			break
		}
	}
	for _, v := range s.Versions {
		for _, m := range v.Markers {
			if u.has(m) {
				return v.ID, true
			}
		}
		if v.PosHint && posHint {
			return v.ID, true
		}
	}
	return s.defaultVersion(), false
}

func ambiguousFlags(s *Spec, argv []string) []string {
	u := scanUsage(argv)
	var out []string
	for _, m := range s.Ambiguous {
		if u.has(m) {
			out = append(out, m)
		}
	}
	return out
}
