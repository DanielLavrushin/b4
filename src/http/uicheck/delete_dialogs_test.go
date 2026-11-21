package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestDeleteAndClearDialogsSayTheSetsSwitchAtOnce(t *testing.T) {
	page := payloadsPageSource(t)
	if regexp.MustCompile(`\bconfirm\(`).MatchString(page) {
		t.Error("the Payloads page still asks through the browser's confirm() instead of a B4Dialog")
	}
	for what, re := range map[string]string{
		"a delete dialog":             `settings\.Capture\.deleteTitle`,
		"a Clear all dialog":          `settings\.Capture\.clearTitle`,
		"a failed reload of the sets": `\.reload_error\b`,
	} {
		if !regexp.MustCompile(re).MatchString(page) {
			t.Errorf("the Payloads page has no %s (%s)", what, re)
		}
	}

	requireKeysInBothLocales(t, []string{
		"settings.Capture.deleteTitle",
		"settings.Capture.clearTitle",
		"settings.Capture.reloadFailed",
	})
	atOnce := map[string]string{"en.json": "at once", "ru.json": "сразу"}
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for _, key := range []string{"settings.Capture.deleteInUseConfirm", "settings.Capture.clearInUseConfirm"} {
			for k, text := range requireKey(t, doc, locale, key) {
				if !strings.Contains(text, atOnce[locale]) {
					t.Errorf("%s settings.Capture.%s does not say the sets switch %q: %q", locale, k, atOnce[locale], text)
				}
				if !strings.Contains(text, "{{sets}}") {
					t.Errorf("%s settings.Capture.%s does not name the sets: %q", locale, k, text)
				}
			}
		}
	}
}
