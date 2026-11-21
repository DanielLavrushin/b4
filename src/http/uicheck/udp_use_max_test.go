package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestUDPUseSizeActionNamesTheCapWhenTheFileIsLarger(t *testing.T) {
	udp := readUI(t, udpFile)
	start := regexp.MustCompile(`"udp\.fake_len",\s*Math\.min\(`).FindStringIndex(udp)
	if start == nil {
		t.Fatal("no action setting Fake Packet Size to the payload's size, capped at what one UDP fake carries")
	}
	end := strings.Index(udp[start[1]:], "</Button>")
	if end < 0 {
		t.Fatal("the Fake Packet Size action never closes")
	}
	button := udp[start[1] : start[1]+end]

	capped := regexp.MustCompile(`truncatedPayloadSize\s*>\s*UDP_FAKE_LEN_MAX\s*\?\s*t\(\s*"sets\.udp\.fakePayloadUseMax"\s*,\s*\{[^}]*\bsize:[^}]*\bUDP_FAKE_LEN_MAX\b`)
	if !capped.MatchString(button) {
		t.Errorf(`the action says "Use the file size" while it sets Fake Packet Size to UDP_FAKE_LEN_MAX for a larger file: %s`, strings.TrimSpace(button))
	}
	if !regexp.MustCompile(`t\(\s*"sets\.udp\.fakePayloadUseSize"\s*\)`).MatchString(button) {
		t.Error(`the action is no longer labelled "Use the file size" for a file one UDP fake carries`)
	}

	requireKeysInBothLocales(t, []string{"sets.udp.fakePayloadUseMax"})
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for k, text := range textForms(doc, "sets.udp.fakePayloadUseMax") {
			if !strings.Contains(text, "{{size}}") {
				t.Errorf("%s sets.udp.%s does not name the size it sets: %q", locale, k, text)
			}
		}
	}
}
