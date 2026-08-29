package convert

import (
	"regexp"
	"strings"
)

var (
	varRefRe     = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)
	percentRefRe = regexp.MustCompile(`%[A-Za-z_][A-Za-z0-9_]*%`)
	setPrefixRe  = regexp.MustCompile(`^(?i:set)\s+`)
	envPrefixRe  = regexp.MustCompile(`^(?i:Environment)=`)
	angleRefRe   = regexp.MustCompile(`<[A-Za-z_][A-Za-z0-9_]*>`)
	batCommentRe = regexp.MustCompile(`^(?:@?(?i:rem)\s|::)`)
	caretContRe  = regexp.MustCompile(`[ \t]*\^[ \t]*\n`)
	slashContRe  = regexp.MustCompile(`(?m)(^|[ \t])([^ \t\\\n]*)\\[ \t]*\n`)
)

type Assignment struct {
	Name   string
	Value  string
	Tokens []string
}

type Source struct {
	Vars         map[string]string
	Assignments  []Assignment
	Alternatives []Assignment
	Free         []string
	referenced   map[string]bool
}

func scanSource(input string) *Source {
	text := strings.ReplaceAll(input, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = caretContRe.ReplaceAllString(text, " ")
	text = slashContRe.ReplaceAllString(text, "$1$2 ")

	src := &Source{Vars: map[string]string{}, referenced: map[string]bool{}}
	for _, stmt := range splitStatements(text) {
		src.readStatement(stmt)
	}
	return src
}

func splitStatements(text string) []string {
	var out []string
	var cur strings.Builder
	quote := byte(0)
	for _, line := range strings.Split(text, "\n") {
		if quote == 0 && isCommentLine(line) {
			out = append(out, line)
			continue
		}
		if cur.Len() > 0 {
			cur.WriteByte('\n')
		}
		cur.WriteString(line)
		quote = scanQuote(line, quote)
		if quote == 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	if cur.Len() > 0 {
		out = append(out, cur.String())
	}
	return out
}

func isCommentLine(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "#") || batCommentRe.MatchString(t)
}

func scanQuote(line string, quote byte) byte {
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '#' && startsWord(line, i):
			return 0
		case c == '\'' || c == '"':
			quote = c
		}
	}
	return quote
}

func startsWord(line string, i int) bool {
	return i == 0 || line[i-1] == ' ' || line[i-1] == '\t'
}

func (s *Source) readStatement(stmt string) {
	trimmed := strings.TrimSpace(stmt)
	if trimmed == "" {
		return
	}
	if batCommentRe.MatchString(trimmed) || isShellKeywordLine(trimmed) {
		return
	}
	if strings.HasPrefix(trimmed, "#") {
		inner := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if m := assignRe.FindStringSubmatch(inner); m != nil {
			body := stripComment(m[2])
			s.Alternatives = append(s.Alternatives, Assignment{
				Name:   m[1],
				Value:  shellUnquote(body),
				Tokens: splitValue(body),
			})
		}
		return
	}
	if m := execStartRe.FindStringSubmatch(trimmed); m != nil {
		s.addFree(shellSplit(s.expand(m[1])))
		return
	}

	body := unwrapSetAssignment(envPrefixRe.ReplaceAllString(trimmed, ""))
	if m := assignRe.FindStringSubmatch(body); m != nil {
		was := s.referenced[m[1]]
		raw := stripComment(s.expand(m[2]))
		s.referenced[m[1]] = was
		s.Vars[m[1]] = shellUnquote(raw)
		s.Assignments = append(s.Assignments, Assignment{
			Name:   m[1],
			Value:  s.Vars[m[1]],
			Tokens: splitValue(raw),
		})
		return
	}
	s.addFree(shellSplit(s.expand(body)))
}

var shellKeywords = map[string]bool{
	"set": true, "unset": true, "shift": true, "trap": true, "local": true,
	"readonly": true, "if": true, "then": true, "else": true, "elif": true,
	"fi": true, "for": true, "while": true, "until": true, "do": true,
	"done": true, "case": true, "esac": true, "return": true, "exit": true,
	"test": true, "[": true, "source": true, ".": true,
}

func isShellKeywordLine(stmt string) bool {
	head, rest, _ := strings.Cut(stmt, " ")
	if !shellKeywords[head] {
		return false
	}
	return assignRe.FindStringSubmatch(strings.TrimSpace(rest)) == nil
}

func stripComment(raw string) string {
	quote := byte(0)
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		switch {
		case quote != 0:
			if c == quote {
				quote = 0
			}
		case c == '\'' || c == '"':
			quote = c
		case c == '#' && startsWord(raw, i):
			return raw[:i]
		}
	}
	return raw
}

func unwrapValue(raw string) string {
	v := strings.TrimSpace(raw)
	if len(v) < 2 {
		return raw
	}
	q := v[0]
	if (q != '\'' && q != '"') || v[len(v)-1] != q {
		return raw
	}
	if strings.IndexByte(v[1:len(v)-1], q) >= 0 {
		return raw
	}
	return v[1 : len(v)-1]
}

func splitValue(raw string) []string {
	tokens := shellSplit(unwrapValue(raw))
	if carriesOptions(tokens) || scanQuote(raw, 0) == 0 {
		return tokens
	}
	return strings.Fields(quoteStripper.Replace(raw))
}

var quoteStripper = strings.NewReplacer("\"", "", "'", "")

func (s *Source) addFree(tokens []string) {
	if !carriesOptions(tokens) {
		return
	}
	s.Free = append(s.Free, tokens...)
}

func unwrapSetAssignment(stmt string) string {
	body := setPrefixRe.ReplaceAllString(stmt, "")
	if body == stmt {
		return body
	}
	if len(body) > 1 && body[0] == '"' && body[len(body)-1] == '"' {
		body = body[1 : len(body)-1]
	}
	return body
}

func (s *Source) expand(raw string) string {
	return s.expandPercent(s.expandDollar(raw))
}

func (s *Source) expandAngleTokens(argv []string) []string {
	out := make([]string, 0, len(argv))
	for _, a := range argv {
		if !angleRefRe.MatchString(a) || len(a) < 3 || a[0] != '<' || a[len(a)-1] != '>' {
			out = append(out, a)
			continue
		}
		val, ok := s.Vars[a[1:len(a)-1]]
		if !ok {
			out = append(out, a)
			continue
		}
		out = append(out, splitValue(val)...)
	}
	return out
}

func (s *Source) expandPercent(raw string) string {
	if !strings.Contains(raw, "%") {
		return raw
	}
	return percentRefRe.ReplaceAllStringFunc(raw, func(m string) string {
		name := m[1 : len(m)-1]
		val, ok := s.Vars[name]
		if !ok {
			return ""
		}
		s.referenced[name] = true
		return val
	})
}

func (s *Source) expandDollar(raw string) string {
	if !strings.Contains(raw, "$") {
		return raw
	}
	var out strings.Builder
	quote := byte(0)
	for i := 0; i < len(raw); i++ {
		c := raw[i]
		if quote == 0 && (c == '\'' || c == '"') {
			quote = c
			out.WriteByte(c)
			continue
		}
		if quote != 0 && c == quote {
			quote = 0
			out.WriteByte(c)
			continue
		}
		if c != '$' || quote == '\'' {
			out.WriteByte(c)
			continue
		}
		m := varRefRe.FindStringSubmatchIndex(raw[i:])
		if m == nil || m[0] != 0 {
			out.WriteByte(c)
			continue
		}
		lo, hi := m[2], m[3]
		if lo < 0 {
			lo, hi = m[4], m[5]
		}
		name := raw[i+lo : i+hi]
		if val, ok := s.Vars[name]; ok {
			s.referenced[name] = true
			out.WriteString(val)
		}
		i += m[1] - 1
	}
	return out.String()
}

func shellUnquote(s string) string {
	var out strings.Builder
	i := 0
	for i < len(s) {
		c := s[i]
		switch c {
		case '\'':
			i++
			for i < len(s) && s[i] != '\'' {
				out.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				i++
			}
		case '"':
			i++
			for i < len(s) && s[i] != '"' {
				if s[i] == '\\' && i+1 < len(s) && (s[i+1] == '"' || s[i+1] == '\\' || s[i+1] == '$' || s[i+1] == '`') {
					out.WriteByte(s[i+1])
					i += 2
					continue
				}
				out.WriteByte(s[i])
				i++
			}
			if i < len(s) {
				i++
			}
		default:
			out.WriteByte(c)
			i++
		}
	}
	return out.String()
}

func carriesOptions(tokens []string) bool {
	for _, t := range tokens {
		if len(t) > 1 && t[0] == '-' {
			return true
		}
	}
	return false
}

func (s *Source) assignment(name string) (Assignment, bool) {
	for i := len(s.Assignments) - 1; i >= 0; i-- {
		if s.Assignments[i].Name == name {
			return s.Assignments[i], true
		}
	}
	return Assignment{}, false
}

func (s *Source) probeArgv() []string {
	argv := append([]string{}, s.Free...)
	seen := map[string]bool{}
	for _, a := range s.Assignments {
		if !carriesOptions(a.Tokens) || s.referenced[a.Name] || seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		last, _ := s.assignment(a.Name)
		argv = append(argv, last.Tokens...)
	}
	return stripLauncher(argv)
}

type sourceReport struct {
	Layout    string
	Used      []string
	Foreign   []string
	Skipped   []string
	Alternate map[string]int
}

func (s *Source) assemble(spec *Spec) ([]string, sourceReport) {
	rep := sourceReport{Alternate: map[string]int{}}
	for _, a := range s.Alternatives {
		if carriesOptions(a.Tokens) && spec.Sources.carries(a.Name) {
			rep.Alternate[a.Name]++
		}
	}
	for _, name := range spec.Sources.Foreign {
		if a, ok := s.assignment(name); ok && carriesOptions(a.Tokens) {
			rep.Foreign = append(rep.Foreign, name)
		}
	}

	groups, layout, used := s.layoutGroups(spec)
	rep.Layout, rep.Used = layout, used
	rep.Skipped = s.skippedVars(spec, used)

	var argv []string
	for _, g := range groups {
		if len(argv) > 0 {
			argv = append(argv, "--new")
		}
		argv = append(argv, g...)
	}
	for len(argv) > 0 && argv[len(argv)-1] == "--new" {
		argv = argv[:len(argv)-1]
	}
	return spec.Sources.substitute(s.expandAngleTokens(stripLauncher(argv))), rep
}

func (s *Source) skippedVars(spec *Spec, used []string) []string {
	taken := map[string]bool{}
	for _, n := range used {
		taken[n] = true
	}
	var out []string
	for _, a := range s.Assignments {
		if taken[a.Name] || !carriesOptions(a.Tokens) || s.referenced[a.Name] {
			continue
		}
		if spec.Sources.foreign(a.Name) || spec.Sources.carries(a.Name) {
			continue
		}
		if len(spec.Sources.Vars) == 0 {
			continue
		}
		out = append(out, a.Name)
	}
	return uniqueStrings(out)
}

func (s *Source) layoutGroups(spec *Spec) ([][]string, string, []string) {
	for _, l := range spec.Sources.Layouts {
		if len(l.Require) == 0 || !s.hasAll(l.Require) {
			continue
		}
		var groups [][]string
		var used []string
		for _, g := range l.Groups {
			tokens, names := s.group(g)
			if len(tokens) == 0 {
				continue
			}
			groups = append(groups, tokens)
			used = append(used, names...)
		}
		if len(groups) > 0 {
			return groups, l.ID, used
		}
	}

	var groups [][]string
	var used []string
	seen := map[string]bool{}
	for _, a := range s.Assignments {
		if !carriesOptions(a.Tokens) || s.referenced[a.Name] || seen[a.Name] {
			continue
		}
		seen[a.Name] = true
		a, _ = s.assignment(a.Name)
		if len(spec.Sources.Vars) > 0 && !spec.Sources.carries(a.Name) {
			continue
		}
		if spec.Sources.foreign(a.Name) {
			continue
		}
		groups = append(groups, a.Tokens)
		used = append(used, a.Name)
	}
	if len(s.Free) > 0 {
		groups = append([][]string{s.Free}, groups...)
	}
	return groups, "", used
}

func (s *Source) hasAll(names []string) bool {
	for _, n := range names {
		a, ok := s.assignment(n)
		if !ok || !carriesOptions(a.Tokens) {
			return false
		}
	}
	return true
}

func (s *Source) group(g SourceGroup) ([]string, []string) {
	if !s.hasAll(g.Require) {
		return nil, nil
	}
	if len(g.Vars) == 0 {
		return nil, nil
	}
	lead, ok := s.assignment(g.Vars[0])
	if !ok || !carriesOptions(lead.Tokens) {
		return nil, nil
	}
	var tokens, names []string
	for _, name := range g.Vars {
		a, ok := s.assignment(name)
		if !ok || len(a.Tokens) == 0 {
			continue
		}
		tokens = append(tokens, a.Tokens...)
		names = append(names, name)
	}
	return append(tokens, g.Append...), names
}

func (ss SpecSources) carries(name string) bool {
	for _, v := range ss.Vars {
		if v == name {
			return true
		}
	}
	return false
}

func (ss SpecSources) foreign(name string) bool {
	for _, v := range ss.Foreign {
		if v == name {
			return true
		}
	}
	return false
}

func (ss SpecSources) substitute(argv []string) []string {
	if len(ss.Placeholders) == 0 {
		return argv
	}
	out := make([]string, 0, len(argv))
	for _, a := range argv {
		if repl, ok := ss.Placeholders[a]; ok {
			if repl != "" {
				out = append(out, repl)
			}
			continue
		}
		out = append(out, a)
	}
	return out
}
