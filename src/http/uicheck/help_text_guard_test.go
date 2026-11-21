package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

var (
	translateCallRe = regexp.MustCompile(`\bt\(`)
	stringLiteralRe = regexp.MustCompile(`"(?:[^"\\]|\\.)*"|'(?:[^'\\]|\\.)*'`)
	builtKeyRe      = regexp.MustCompile("`|\\+|\\b\\w+\\(")
	familyArgRe     = regexp.MustCompile("^`[a-zA-Z0-9_]+(?:\\.[a-zA-Z0-9_]+)+\\.\\$\\{[^{}]+\\}`$")
)

func firstArgument(expr string, from int) string {
	depth := 0
	var quote byte
	for i := from; i < len(expr); i++ {
		c := expr[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '(', '{', '[':
			depth++
		case ')', '}', ']':
			if depth == 0 {
				return expr[from:i]
			}
			depth--
		case ',':
			if depth == 0 {
				return expr[from:i]
			}
		}
	}
	return expr[from:]
}

func unreadableKeys(expr string) []string {
	var found []string
	for _, at := range translateCallRe.FindAllStringIndex(expr, -1) {
		arg := strings.TrimSpace(firstArgument(expr, at[1]))
		if familyArgRe.MatchString(arg) {
			continue
		}
		bare := stringLiteralRe.ReplaceAllString(arg, `""`)
		if builtKeyRe.MatchString(bare) || !quotedKeyRe.MatchString(arg) {
			found = append(found, arg)
		}
	}
	return found
}

func TestHelperScanTellsLiteralKeysFromBuiltOnes(t *testing.T) {
	cases := []struct {
		expr string
		bad  int
	}{
		{`{t("sets.targets.addIpHelper")}`, 0},
		{`{t("sets.udp.portFilterHelper", { max: MAX_PORTS })}`, 0},
		{`{t(config.targets.tls ? "sets.targets.tlsHelperVersion" : "sets.targets.tlsHelperText", { version })}`, 0},
		{`{blocked ? t("settings.Listener.exposeBlocked") : t("settings.Listener.exposeDesc")}`, 0},
		{`{helperText}`, 0},
		{`{notes || helperText}`, 0},
		{"{error ? t(`settings.Checker.trustedDnsError.${error}`) : t(\"settings.Checker.trustedDnsHelper\")}", 0},
		{`{t(K("description"))}`, 1},
		{"{t(`settings.Listener.${kind}Desc`)}", 1},
		{`{t("settings.Listener." + kind)}`, 1},
		{`{t(descriptionKey)}`, 1},
		{`{ready ? t("settings.Api.tokenHelper") : t(K("pending"))}`, 1},
	}
	for _, c := range cases {
		if got := unreadableKeys(c.expr); len(got) != c.bad {
			t.Errorf("%s: %d keys the length check cannot read (%q), want %d", c.expr, len(got), got, c.bad)
		}
	}
}

func TestHelperLinesNameTheirKeysLiterally(t *testing.T) {
	for _, rel := range tsxSourceFiles(t) {
		src := readUI(t, rel)
		for _, at := range helperAttrRe.FindAllStringIndex(src, -1) {
			expr := jsxExpression(src, at[1]-1)
			for _, arg := range unreadableKeys(expr) {
				line := strings.Count(src[:at[0]], "\n") + 1
				t.Errorf("%s:%d builds the key of a helper line at run time, t(%s): TestHelperLinesStayShort cannot check its length, so name the key as a string literal", rel, line, arg)
			}
		}
	}
}

func TestHelperScanReadsTheManualPanelsHelper(t *testing.T) {
	keys := helperLineKeys(t)
	for _, key := range []string{"sets.targets.addIpHelper", "sets.targets.addDomainHelper"} {
		if _, ok := keys[key]; !ok {
			t.Errorf("%s reaches the screen through inputHelper, a prop the helper scan does not read", key)
		}
	}
}

func TestHelperScanReadsEveryKeyOfAFamilyPickedAtRunTime(t *testing.T) {
	keys := helperLineKeys(t)
	for _, key := range []string{
		"settings.Checker.trustedDnsError.invalid",
		"settings.Checker.trustedDnsError.unsupported",
		"discovery.options.pinsError.private",
	} {
		if _, ok := keys[key]; !ok {
			t.Errorf("%s is shown in a helper line through a key picked at run time, and the helper scan does not read it", key)
		}
	}
}
