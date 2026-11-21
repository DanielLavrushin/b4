package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestRussianLocaleSaysPayloadInEnglish(t *testing.T) {
	raw := readUI(t, "i18n/ru.json")
	translit := regexp.MustCompile(`(?i)п[эе]йлоад`)
	for i, line := range strings.Split(raw, "\n") {
		if translit.MatchString(line) {
			t.Errorf("ru.json line %d uses the transliteration instead of \"payload\": %s", i+1, strings.TrimSpace(line))
		}
	}

	ru := loadLocale(t, "ru.json")
	en := loadLocale(t, "en.json")
	englishForm := regexp.MustCompile(`\bpayloads\b|payload-[а-яё]`)
	walkStrings(ru, "", func(key, text string) {
		if englishForm.MatchString(text) {
			t.Errorf("ru.json %s declines payload in English or with a hyphen instead of keeping it as is: %q", key, text)
		}
	})
	for _, form := range pluralForms["ru.json"] {
		if text := textForms(ru, "settings.Capture.generatedDesc")["generatedDesc"+form]; text == "" {
			t.Errorf("ru.json has no settings.Capture.generatedDesc%s", form)
		}
	}
	for _, key := range []string{"settings.Capture.clearAll", "settings.tabs.payloads"} {
		for locale, doc := range map[string]map[string]any{"en.json": en, "ru.json": ru} {
			for k, text := range requireKey(t, doc, locale, key) {
				if !strings.Contains(strings.ToLower(text), "payload") {
					t.Errorf("%s %s (%s) does not say payload: %q", locale, key, k, text)
				}
			}
		}
	}
}

func walkStrings(node any, path string, visit func(key, text string)) {
	switch v := node.(type) {
	case map[string]any:
		for k, child := range v {
			next := k
			if path != "" {
				next = path + "." + k
			}
			walkStrings(child, next, visit)
		}
	case string:
		visit(path, v)
	}
}
