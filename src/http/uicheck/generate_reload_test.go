package uicheck

import (
	"regexp"
	"testing"
)

func generateHandler(t *testing.T) string {
	t.Helper()
	page := readUI(t, "components/settings/Capture.tsx")
	m := regexp.MustCompile(`(?s)const generateCapture = async \(\) => \{(.*?)\n  \};\n`).FindStringSubmatch(page)
	if m == nil {
		t.Fatal("no generateCapture handler on the Payloads page")
	}
	return m[1]
}

func TestGenerateReportsAFailedReloadOfTheSets(t *testing.T) {
	model := readUI(t, "models/capture.ts")
	for _, field := range []string{"reloaded", "reload_error"} {
		if !regexp.MustCompile(`(?s)interface CaptureGenerateResponse \{[^}]*\b` + field + `\??:`).MatchString(model) {
			t.Errorf("CaptureGenerateResponse has no %s, which the generate answer carries", field)
		}
	}
	if !regexp.MustCompile(`reportReload\(\s*result\s*\)`).MatchString(generateHandler(t)) {
		t.Error("generateCapture ignores reload_error, which upload, delete and regenerate report")
	}
}
