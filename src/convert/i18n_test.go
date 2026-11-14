package convert

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

var (
	reasonCallRe  = regexp.MustCompile(`(?:StatusMapped|StatusApproximated|StatusUnsupported|StatusNotApplicable|StatusDegenerate|StatusUnknown|StatusInvalid),\s*"([a-zA-Z][a-zA-Z0-9]*)"`)
	reasonVarRe   = regexp.MustCompile(`notes\.set\([^\n]*?,\s*(?:src\.Status|st|status),\s*"([a-zA-Z][a-zA-Z0-9]*)"`)
	reasonFieldRe = regexp.MustCompile(`Reason:\s*"([a-zA-Z][a-zA-Z0-9]*)"`)
	warnCodeRe    = regexp.MustCompile(`Warning\{Code:\s*"([a-zA-Z][a-zA-Z0-9]*)"`)
	noteFieldRe   = regexp.MustCompile(`"note":\s*"([a-zA-Z][a-zA-Z0-9]*)"`)
)

func loadConvertStrings(t *testing.T, path string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	sets, _ := doc["sets"].(map[string]any)
	conv, _ := sets["convert"].(map[string]any)
	if conv == nil {
		t.Fatalf("%s has no sets.convert block", path)
	}
	return conv
}

func collect(t *testing.T, files []string, res ...*regexp.Regexp) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	for _, f := range files {
		raw, err := os.ReadFile(f)
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		for _, re := range res {
			for _, m := range re.FindAllStringSubmatch(string(raw), -1) {
				out[m[len(m)-1]] = true
			}
		}
	}
	return out
}

func emittedCodes(t *testing.T) (reasons, warnings map[string]bool) {
	t.Helper()
	goFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	goFiles = slices.DeleteFunc(goFiles, func(f string) bool { return strings.HasSuffix(f, "_test.go") })
	ruleFiles := []string{"rules/byedpi.json", "rules/zapret.json"}

	reasons = collect(t, goFiles, reasonCallRe, reasonVarRe, reasonFieldRe)
	for k := range collect(t, ruleFiles, noteFieldRe) {
		reasons[k] = true
	}
	return reasons, collect(t, goFiles, warnCodeRe)
}

func TestI18n_EveryReasonAndWarningIsTranslated(t *testing.T) {
	reasons, warnings := emittedCodes(t)

	for _, path := range []string{
		"../http/ui/src/i18n/en.json",
		"../http/ui/src/i18n/ru.json",
	} {
		t.Run(path, func(t *testing.T) {
			conv := loadConvertStrings(t, path)
			reasonStrings, _ := conv["reason"].(map[string]any)
			warningStrings, _ := conv["warning"].(map[string]any)
			statusStrings, _ := conv["status"].(map[string]any)

			for r := range reasons {
				if _, ok := reasonStrings[r]; !ok {
					t.Errorf("missing sets.convert.reason.%s", r)
				}
			}
			for w := range warnings {
				if _, ok := warningStrings[w]; !ok {
					t.Errorf("missing sets.convert.warning.%s", w)
				}
			}
			for _, s := range []Status{
				StatusMapped, StatusApproximated, StatusUnsupported,
				StatusNotApplicable, StatusDegenerate, StatusUnknown, StatusInvalid,
			} {
				if _, ok := statusStrings[string(s)]; !ok {
					t.Errorf("missing sets.convert.status.%s", s)
				}
			}
		})
	}
}

func placeholdersOf(text string) []string {
	var out []string
	for _, m := range englishPlaceholder.FindAllStringSubmatch(text, -1) {
		out = append(out, m[1])
	}
	return out
}

func TestEnglish_EveryReasonAndWarningHasText(t *testing.T) {
	reasons, warnings := emittedCodes(t)
	conv := loadConvertStrings(t, "../http/ui/src/i18n/en.json")

	for _, c := range []struct {
		kind    string
		emitted map[string]bool
		english map[string]string
	}{
		{"reason", reasons, reasonEnglish},
		{"warning", warnings, warningEnglish},
	} {
		ui, _ := conv[c.kind].(map[string]any)
		for code := range c.emitted {
			if c.english[code] == "" {
				t.Errorf("%s %s is emitted but has no English text in english.go, so b4_import_strategy would answer with the bare code", c.kind, code)
			}
		}
		for code, raw := range ui {
			text, ok := c.english[code]
			if !ok {
				t.Errorf("%s %s has text in en.json but none in english.go", c.kind, code)
				continue
			}
			uiText, _ := raw.(string)
			known := placeholdersOf(uiText)
			for _, name := range placeholdersOf(text) {
				if !slices.Contains(known, name) {
					t.Errorf("%s %s uses {{%s}} in english.go, a parameter its en.json text does not take", c.kind, code, name)
				}
			}
		}
		for code := range c.english {
			if _, ok := ui[code]; !ok {
				t.Errorf("%s %s has text in english.go but none in en.json: remove it, or add the code to both", c.kind, code)
			}
		}
	}
}

func TestEnglish_FillsParameters(t *testing.T) {
	for _, c := range []struct{ got, want string }{
		{
			Note{Reason: "hostsMapped", Params: map[string]any{"count": 3}}.English(),
			"3 domain(s) became the targets of this set.",
		},
		{
			Warning{Code: "ambiguousVersion", Params: map[string]any{"flags": []string{"-a", "-b"}}}.English(),
			"The version could not be determined from the options used, and -a, -b mean different things across byedpi versions. Pass the version explicitly if the result looks wrong.",
		},
		{Note{Reason: "noSuchReason"}.English(), "noSuchReason"},
		{Warning{Code: "udpDisabledUpstream"}.English(), warningEnglish["udpDisabledUpstream"]},
	} {
		if c.got != c.want {
			t.Errorf("got %q, want %q", c.got, c.want)
		}
	}
}

func TestEnglish_SamplesLeaveNoPlaceholder(t *testing.T) {
	samples, err := filepath.Glob("testdata/*")
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range samples {
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", path, err)
		}
		res, err := Analyze(string(raw), Options{})
		if err != nil {
			continue
		}
		for _, n := range res.Notes {
			if text := n.English(); strings.Contains(text, "{{") || text == n.Reason {
				t.Errorf("%s: note %s for %q renders as %q", path, n.Reason, n.Token, text)
			}
		}
		for _, w := range res.Warnings {
			if text := w.English(); strings.Contains(text, "{{") || text == w.Code {
				t.Errorf("%s: warning %s renders as %q", path, w.Code, text)
			}
		}
	}
}
