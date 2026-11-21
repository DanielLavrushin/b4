package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestTCPPickerTreatsAnEmptyPayloadFileLikeAMissingOne(t *testing.T) {
	helpers := readUI(t, payloadFileHelpers)
	if !regexp.MustCompile(`state:\s*"empty"`).MatchString(helpers) {
		t.Error(`chosenPayloadFile has no "empty" state: an empty file, which Go replaces with Preset: Google (classic) like a missing one, resolves as a normal choice`)
	}
	if !strings.Contains(helpers, `"empty"`) || !regexp.MustCompile(`\.warnings\b`).MatchString(helpers) {
		t.Error(`chosenPayloadFile never reads the list entry's "empty" warning`)
	}

	tcp := readUI(t, tcpFakingFile)
	tls := filteredListName(tcp, "captures", "tls")
	if tls == "" {
		t.Fatal(`no TLS payload list filtered on protocol === "tls"`)
	}
	filter := regexp.MustCompile(`(?s)const\s+` + tls + `\s*=\s*captures\s*\.filter\((.*?)\)\s*\.sort\(`).FindStringSubmatch(tcp)
	if filter == nil || !regexp.MustCompile(`isEmptyPayload\(|"empty"`).MatchString(filter[1]) {
		t.Errorf("%s, the TCP picker's choices, still offers an empty file as a normal choice", tls)
	}

	branch := regexp.MustCompile(`(?s)chosenFile\.state\s*===\s*"empty"\s*\)\s*\{(.*?)\n  \}`).FindStringSubmatch(tcp)
	if branch == nil {
		t.Fatal(`the TCP picker has no branch for a chosen file that is empty`)
	}
	for what, re := range map[string]string{
		"marks the option as an empty file":      `t\(\s*"sets\.faking\.fakeSni\.payloadFileEmpty"\s*,\s*\{[^}]*name:`,
		"says what the set sends instead":        `note:\s*t\(\s*"sets\.faking\.fakeSni\.payloadFileMissingNote"`,
		"explains the fallback under the picker": `t\(\s*"sets\.faking\.fakeSni\.payloadFileEmptyHelper"\s*,\s*\{[^}]*file:`,
	} {
		if !regexp.MustCompile(re).MatchString(branch[1]) {
			t.Errorf("the empty-file branch never %s (%s)", what, re)
		}
	}

	requireKeysInBothLocales(t, []string{"sets.faking.fakeSni.payloadFileEmpty", "sets.faking.fakeSni.payloadFileEmptyHelper"})
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		google := textForms(doc, "sets.faking.fakeSni.payloadGoogle")["payloadGoogle"]
		for k, text := range textForms(doc, "sets.faking.fakeSni.payloadFileEmptyHelper") {
			if google == "" || !strings.Contains(text, google) {
				t.Errorf("%s %s does not say the set sends %q instead: %q", locale, k, google, text)
			}
			if !strings.Contains(text, "{{file}}") {
				t.Errorf("%s %s does not name the file: %q", locale, k, text)
			}
		}
		for k, text := range textForms(doc, "sets.faking.fakeSni.payloadFileEmpty") {
			if !strings.Contains(text, "{{name}}") {
				t.Errorf("%s %s does not carry the payload's name: %q", locale, k, text)
			}
		}
	}
}
