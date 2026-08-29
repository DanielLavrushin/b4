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

func TestI18n_EveryReasonAndWarningIsTranslated(t *testing.T) {
	goFiles, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	goFiles = slices.DeleteFunc(goFiles, func(f string) bool { return strings.HasSuffix(f, "_test.go") })
	ruleFiles := []string{"rules/byedpi.json", "rules/zapret.json"}

	reasons := collect(t, goFiles, reasonCallRe, reasonVarRe, reasonFieldRe)
	for k := range collect(t, ruleFiles, noteFieldRe) {
		reasons[k] = true
	}
	warnings := collect(t, goFiles, warnCodeRe)

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
