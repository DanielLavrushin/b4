package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestUploadFormDoesNotOfferUploadForAFileTheServerRefuses(t *testing.T) {
	page := readUI(t, "components/settings/Capture.tsx")
	if !regexp.MustCompile(`\.status\s*===\s*400\b`).MatchString(page) {
		t.Error("the upload form treats an inspect answer of 400, a file the server refuses, like a check that merely failed")
	}

	decl := regexp.MustCompile(`(?s)const (\w+)\s*=([^;]*"settings\.Capture\.cannotUpload"[^;]*);`).FindStringSubmatch(page)
	if decl == nil {
		t.Fatal("the upload form never says that the chosen file cannot be uploaded (settings.Capture.cannotUpload)")
	}
	refusal, init := decl[1], decl[2]
	for what, re := range map[string]string{
		"gives the server's text":           `t\(\s*"settings\.Capture\.cannotUpload"\s*,\s*\{[^}]*error:`,
		"refuses an empty file":             `\.size\s*===\s*0\b`,
		"says why an empty file is refused": `t\(\s*"settings\.Capture\.cannotUploadEmpty"\s*\)`,
	} {
		if !regexp.MustCompile(re).MatchString(init) {
			t.Errorf("%s never %s (%s)", refusal, what, re)
		}
	}

	warnings := regexp.MustCompile(`(?s)const uploadWarnings\s*=([^;]*);`).FindStringSubmatch(page)
	if warnings == nil || !regexp.MustCompile(`\b`+refusal+`\b`).MatchString(warnings[1]) {
		t.Errorf("the upload warnings repeat what %s already says for a refused file", refusal)
	}

	cardAt := strings.Index(page, "<PayloadWarnings")
	uploadAt := strings.Index(page, `t("core.upload")`)
	if cardAt < 0 || uploadAt < cardAt {
		t.Fatal("no upload warnings before the Upload button")
	}
	if !regexp.MustCompile(`\{\s*!` + refusal + `\s*&&`).MatchString(page[cardAt:uploadAt]) {
		t.Errorf("Upload is still offered for a file the server refuses: it is not rendered under !%s", refusal)
	}

	requireKeysInBothLocales(t, []string{"settings.Capture.cannotUpload", "settings.Capture.cannotUploadEmpty", "settings.Capture.inspectFailed"})
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for k, text := range textForms(doc, "settings.Capture.cannotUpload") {
			if !strings.Contains(text, "{{error}}") {
				t.Errorf("%s settings.Capture.%s has no {{error}}: %q", locale, k, text)
			}
		}
	}
}
