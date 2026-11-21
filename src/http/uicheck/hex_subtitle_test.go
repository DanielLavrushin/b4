package uicheck

import (
	"regexp"
	"testing"
)

func TestHexSubtitleNamesNoCustomPayload(t *testing.T) {
	pointer := regexp.MustCompile(`(?i)custom|faking|фейкинг|свой`)
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for k, text := range requireKey(t, doc, locale, "settings.Capture.hexSubtitle") {
			if pointer.MatchString(text) {
				t.Errorf("%s settings.Capture.%s points at the Custom payload, which sends pasted hex as text: %q", locale, k, text)
			}
		}
	}
}
