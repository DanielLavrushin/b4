package handler

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func fakeSniStrings(t *testing.T, locale string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile("../ui/src/i18n/" + locale)
	if err != nil {
		t.Fatalf("read %s: %v", locale, err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("parse %s: %v", locale, err)
	}
	cur := doc
	for _, p := range []string{"sets", "faking", "fakeSni"} {
		next, ok := cur[p].(map[string]any)
		if !ok {
			t.Fatalf("%s has no sets.faking.fakeSni block", locale)
		}
		cur = next
	}
	return cur
}

func TestOversizedPayloadWarningDeclinesTheByteCountAndStatesItsCondition(t *testing.T) {
	const key = "payloadOverOnePacket"
	forms := map[string]map[string]string{
		"en.json": {"_one": "{{count}} byte.", "_other": "{{count}} bytes."},
		"ru.json": {"_one": "{{count}} байт.", "_few": "{{count}} байта.", "_many": "{{count}} байт.", "_other": "{{count}} байта."},
	}
	conditions := map[string][]string{
		"en.json": {"1500-byte packet", "TCP timestamps", "{{max}}", "Match Fake Size"},
		"ru.json": {"1500 байт", "TCP timestamp", "{{max}}", "Подгонять размер фейка"},
	}
	for locale, want := range forms {
		t.Run(locale, func(t *testing.T) {
			block := fakeSniStrings(t, locale)
			if bare, ok := block[key]; ok {
				t.Errorf("%s has a bare %s, which i18next uses without a count: %v", locale, key, bare)
			}
			for suffix, noun := range want {
				text, _ := block[key+suffix].(string)
				if text == "" {
					t.Errorf("%s has no %s%s", locale, key, suffix)
					continue
				}
				if !strings.Contains(text, noun) {
					t.Errorf("%s %s%s does not decline the byte count as %q: %s", locale, key, suffix, noun, text)
				}
				for _, c := range conditions[locale] {
					if !strings.Contains(text, c) {
						t.Errorf("%s %s%s does not say %q: %s", locale, key, suffix, c, text)
					}
				}
				if strings.Contains(text, "does not fit one packet and is not sent") {
					t.Errorf("%s %s%s states as always true what holds only on a 1500-byte link: %s", locale, key, suffix, text)
				}
			}
		})
	}
}
