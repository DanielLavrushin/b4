package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

var failedCallRe = regexp.MustCompile(`t\(\s*"settings\.Capture\.(\w+Failed)"\s*(,\s*\{[^}]*\})?\s*\)`)

func TestPayloadErrorsShowTheServerText(t *testing.T) {
	page := payloadsPageSource(t)
	seen := map[string]bool{}
	for _, m := range failedCallRe.FindAllStringSubmatch(page, -1) {
		key := m[1]
		if key == "hexCopyFailed" {
			continue
		}
		seen[key] = true
		if !strings.Contains(m[2], "error:") {
			t.Errorf("%s is shown without the server's text: %s", key, m[0])
		}
	}
	for _, key := range []string{"generateFailed", "uploadFailed", "deleteFailed", "clearFailed", "listFailed"} {
		if !seen[key] {
			t.Errorf("the Payloads page never shows settings.Capture.%s", key)
		}
	}

	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for key := range seen {
			for k, text := range requireKey(t, doc, locale, "settings.Capture."+key) {
				if !strings.Contains(text, "{{error}}") {
					t.Errorf("%s settings.Capture.%s has no {{error}}: %q", locale, k, text)
				}
			}
		}
	}

	hook := readUI(t, "hooks/useCapture.ts")
	if !strings.Contains(hook, "setListError(") {
		t.Error("useCaptures swallows a failed list load: it keeps no list error")
	}
	if !regexp.MustCompile(`\blistError\b`).MatchString(page) {
		t.Error("the Payloads page does not show a failed list load")
	}
}
