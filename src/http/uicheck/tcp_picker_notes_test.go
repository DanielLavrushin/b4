package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestTCPPayloadPickerIsSortedWithNoteLines(t *testing.T) {
	tcp := readUI(t, tcpFakingFile)
	tls := filteredListName(tcp, "captures", "tls")
	if tls == "" {
		t.Fatal(`no TLS payload list filtered on protocol === "tls"`)
	}
	if !regexp.MustCompile(`const\s+` + tls + `\s*=\s*captures\s*\.filter\([^;]*?\)\s*\.sort\(`).MatchString(tcp) {
		t.Errorf("%s, the TCP picker's payloads, is not sorted by name", tls)
	}
	if !regexp.MustCompile(`\b` + tls + `\s*\.map\(\s*\(?\s*(\w+)\s*\)?\s*=>\s*\(\{[^}]*?note:\s*payloadNote\(\s*t\s*,`).MatchString(tcp) {
		t.Error("the TCP picker's payload options have no note line from payloadNote")
	}
	if !regexp.MustCompile(`note:\s*t\(\s*"sets\.faking\.fakeSni\.payloadFileMissingNote"`).MatchString(tcp) {
		t.Error("the missing-file option's note does not say what the set sends instead")
	}

	helpers := readUI(t, payloadFileHelpers)
	fn := regexp.MustCompile(`(?s)export function payloadNote\b(.*?)\n}\n`).FindStringSubmatch(helpers)
	if fn == nil {
		t.Fatal("no payloadNote helper building an option's note line")
	}
	for part, re := range map[string]string{
		"origin":       "settings\\.Capture\\.origins\\.",
		"kind":         `kindLabel\(\s*t\s*,`,
		"size":         `bytesLabel\(\s*t\s*,`,
		"faults count": `t\(\s*"sets\.faking\.fakeSni\.payloadFaults"\s*,\s*\{\s*count:`,
	} {
		if !regexp.MustCompile(re).MatchString(fn[1]) {
			t.Errorf("payloadNote does not give the payload's %s", part)
		}
	}
	if !strings.Contains(fn[1], "faults") {
		t.Error("payloadNote never reads facts.faults")
	}

	keys := []string{"sets.faking.fakeSni.payloadFaults", "sets.faking.fakeSni.payloadFileMissingNote"}
	requireKeysInBothLocales(t, keys)
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for k, text := range requireKey(t, doc, locale, "sets.faking.fakeSni.payloadFaults") {
			if !strings.Contains(text, "{{count}}") {
				t.Errorf("%s %s does not carry the count: %q", locale, k, text)
			}
		}
		google := textForms(doc, "sets.faking.fakeSni.payloadGoogle")["payloadGoogle"]
		for k, text := range textForms(doc, "sets.faking.fakeSni.payloadFileMissingNote") {
			if google == "" || !strings.Contains(text, google) {
				t.Errorf("%s %s does not say the set sends %q instead: %q", locale, k, google, text)
			}
		}
	}
}
