package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestPayloadsPageRoutesUnitsAndCaptionThroughI18n(t *testing.T) {
	for _, f := range payloadsPageFiles(t) {
		src := readUI(t, f)
		if strings.Contains(src, "TLS payload for") {
			t.Errorf("%s prints the literal caption \"TLS payload for\", for QUIC payloads too", f)
		}
		for _, m := range regexp.MustCompile(`\}[ \t]+bytes\b`).FindAllString(src, -1) {
			t.Errorf("%s prints a literal byte unit after a number: %q", f, m)
		}
	}

	page := payloadsPageSource(t)
	if !regexp.MustCompile(`t\(\s*"settings\.Capture\.hexCaption"\s*,\s*\{[^}]*protocol:`).MatchString(page) {
		t.Error("the hex dialog caption does not name the payload's protocol through t()")
	}
	requireKeysInBothLocales(t, []string{"settings.Capture.hexCaption", "settings.Capture.bytes"})
}
