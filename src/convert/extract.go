package convert

import (
	"regexp"
	"strings"
)

var (
	assignRe    = regexp.MustCompile(`(?s)^[ \t]*(?:export[ \t]+)?([A-Za-z_][A-Za-z0-9_]*)[ \t]*=(.*)$`)
	execStartRe = regexp.MustCompile(`(?s)^[ \t]*ExecStart[ \t]*=(.*)$`)
	launcherRe  = regexp.MustCompile(`^(sudo|exec|nohup|env|setsid|start|start-stop-daemon|cmd)$`)
	batNoiseRe  = regexp.MustCompile(`^(?i:/(min|max|b|wait|d|realtime|high|low))$`)
	binaryRe    = regexp.MustCompile(`(?i)(^|[/\\])(ciadpi|byedpi|nfqws2?|winws2?|dvtws2?|tpws|goodbyedpi)(\.exe)?$`)
)

func extractArgv(input string) []string {
	return scanSource(input).probeArgv()
}

func stripLauncher(argv []string) []string {
	lead := 0
	for lead < len(argv) && !strings.HasPrefix(argv[lead], "-") {
		lead++
	}
	cut := -1
	for i := 0; i < lead; i++ {
		if binaryRe.MatchString(argv[i]) {
			cut = i
		}
	}
	if cut >= 0 {
		return argv[cut+1:]
	}
	i := 0
	for i < lead {
		base := argv[i]
		if j := strings.LastIndexAny(base, `/\`); j >= 0 {
			base = base[j+1:]
		}
		if !launcherRe.MatchString(strings.ToLower(base)) && !batNoiseRe.MatchString(base) {
			break
		}
		i++
	}
	return argv[i:]
}

func shellSplit(s string) []string {
	var out []string
	var cur strings.Builder
	inWord := false
	i := 0
	flush := func() {
		if inWord {
			out = append(out, cur.String())
			cur.Reset()
			inWord = false
		}
	}
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n':
			flush()
			i++
		case c == '#' && !inWord:
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '\'':
			inWord = true
			i++
			for i < len(s) && s[i] != '\'' {
				cur.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				i++
			}
		case c == '"':
			inWord = true
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\' || s[i+1] == '$' || s[i+1] == '`') {
					cur.WriteByte(s[i+1])
					i += 2
					continue
				}
				cur.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				i++
			}
		case c == '\\' && i+1 < len(s) && isShellEscapable(s[i+1]):
			inWord = true
			cur.WriteByte(s[i+1])
			i += 2
		default:
			inWord = true
			cur.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

func isShellEscapable(c byte) bool {
	switch c {
	case ' ', '\t', '\n', '"', '\'', '\\', '$', '`', '#':
		return true
	}
	return false
}
