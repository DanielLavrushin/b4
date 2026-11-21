package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestPayloadCardShowsAMissingFileAndDownloadErrors(t *testing.T) {
	model := readUI(t, "models/capture.ts")
	if !regexp.MustCompile(`\bmissing\??:\s*boolean`).MatchString(model) {
		t.Error("the Capture model has no missing flag")
	}

	page := payloadsPageSource(t)
	if !regexp.MustCompile(`\.missing\b`).MatchString(page) {
		t.Error("the payload card never reads the entry's missing flag")
	}
	if regexp.MustCompile(`void\s+download\(`).MatchString(page) {
		t.Error("the page discards the download promise, so a failed download shows nothing")
	}
	if !regexp.MustCompile(`t\(\s*"settings\.Capture\.downloadFailed"\s*,\s*\{[^}]*error:`).MatchString(page) {
		t.Error("the page never shows settings.Capture.downloadFailed with the server's text")
	}

	hook := readUI(t, "hooks/useCapture.ts") + readUI(t, "api/capture.ts")
	if strings.Contains(hook, "Download failed: ${") {
		t.Error("a failed download throws only the status text, not the server's reason")
	}

	requireKeysInBothLocales(t, []string{
		"settings.Capture.downloadFailed",
		"settings.Capture.missingFileTls",
		"settings.Capture.missingFileQuic",
	})
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for k, text := range textForms(doc, "settings.Capture.downloadFailed") {
			if !strings.Contains(text, "{{error}}") {
				t.Errorf("%s settings.Capture.%s has no {{error}}: %q", locale, k, text)
			}
		}
	}
}
