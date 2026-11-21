package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestRegenerateIsOfferedForGeneratedPayloadsOnly(t *testing.T) {
	api := readUI(t, "api/capture.ts")
	if !strings.Contains(api, `"/api/capture/regenerate"`) {
		t.Error("captureApi never posts to /api/capture/regenerate")
	}

	page := payloadsPageSource(t)
	for what, re := range map[string]string{
		"offers Regenerate only for origin generated": `\.origin\s*===\s*"generated"`,
		"passes a Regenerate action to the card":      `onRegenerate=`,
		"asks in a B4Dialog first":                    `settings\.Capture\.regenerateTitle`,
		"names the sets that use it":                  `settings\.Capture\.regenerateInUse`,
		"shows a failure with the server's text":      `t\(\s*"settings\.Capture\.regenerateFailed"\s*,\s*\{[^}]*error:`,
	} {
		if !regexp.MustCompile(re).MatchString(page) {
			t.Errorf("the Payloads page never %s (%s)", what, re)
		}
	}

	requireKeysInBothLocales(t, []string{
		"settings.Capture.regenerate",
		"settings.Capture.regenerateTitle",
		"settings.Capture.regenerateBody",
		"settings.Capture.regenerateInUse",
		"settings.Capture.regenerateFixes",
		"settings.Capture.regenerateSuccess",
		"settings.Capture.regenerateFailed",
	})
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for k, text := range textForms(doc, "settings.Capture.regenerateFailed") {
			if !strings.Contains(text, "{{error}}") {
				t.Errorf("%s settings.Capture.%s has no {{error}}: %q", locale, k, text)
			}
		}
		for k, text := range textForms(doc, "settings.Capture.regenerateInUse") {
			if !strings.Contains(text, "{{sets}}") {
				t.Errorf("%s settings.Capture.%s does not name the sets: %q", locale, k, text)
			}
		}
	}
}
