package convert

import (
	"regexp"
	"strings"
)

var (
	assignRe    = regexp.MustCompile(`^\s*(?:export\s+)?[A-Za-z_][A-Za-z0-9_]*\s*=\s*(.*)$`)
	execStartRe = regexp.MustCompile(`^\s*ExecStart\s*=\s*(.*)$`)
	launcherRe  = regexp.MustCompile(`^(sudo|exec|nohup|env|setsid|start-stop-daemon)$`)
	binaryRe    = regexp.MustCompile(`(?i)(^|/)(ciadpi|byedpi|nfqws|winws|tpws)(\.exe)?$`)
)

func extractArgv(input string) []string {
	text := strings.ReplaceAll(input, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.ReplaceAll(text, "\\\n", " ")

	var payloads []string
	for _, line := range strings.Split(text, "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		if m := execStartRe.FindStringSubmatch(line); m != nil {
			payloads = append(payloads, stripOuterQuotes(m[1]))
			continue
		}
		if m := assignRe.FindStringSubmatch(line); m != nil {
			payloads = append(payloads, stripOuterQuotes(m[1]))
			continue
		}
		payloads = append(payloads, line)
	}

	argv := shellSplit(strings.Join(payloads, "\n"))
	for len(argv) > 0 {
		head := argv[0]
		if strings.HasPrefix(head, "-") {
			break
		}
		base := head
		if i := strings.LastIndex(base, "/"); i >= 0 {
			base = base[i+1:]
		}
		if launcherRe.MatchString(base) || binaryRe.MatchString(head) {
			argv = argv[1:]
			continue
		}
		break
	}
	return argv
}

func stripOuterQuotes(s string) string {
	s = strings.TrimSpace(s)
	if len(s) < 2 {
		return s
	}
	q := s[0]
	if (q != '"' && q != '\'') || s[len(s)-1] != q {
		return s
	}
	inner := s[1 : len(s)-1]
	if strings.IndexByte(inner, q) >= 0 {
		return s
	}
	return inner
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
		case c == '\\' && i+1 < len(s):
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
