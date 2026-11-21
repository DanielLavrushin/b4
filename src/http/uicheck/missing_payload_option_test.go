package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

const payloadFileHelpers = "components/sets/payloadFile.ts"

func TestSetEditorKeepsAMissingPayloadFileSelectedAndMarked(t *testing.T) {
	tcp := readUI(t, tcpFakingFile)
	picker := jsxElement(t, tcp, "B4Select", `path="faking.payload_file"`)
	branch := strings.Index(tcp, "config.faking.sni_type === FakingPayloadType.CAPTURE && (")
	pickerAt := strings.Index(tcp, picker)
	if branch < 0 || branch > pickerAt {
		t.Fatal("no My own Payload File branch before the TCP payload picker")
	}
	if regexp.MustCompile(`\.length\s*>\s*0\s*&&`).MatchString(tcp[branch:pickerAt]) {
		t.Error("the TCP payload picker renders only while the payload list is not empty")
	}

	helpers := readUI(t, payloadFileHelpers)
	if !regexp.MustCompile(`export function chosenPayloadFile\b`).MatchString(helpers) {
		t.Fatal("no chosenPayloadFile helper resolving a set's payload file against the list")
	}
	if !regexp.MustCompile(`\.missing\b`).MatchString(helpers) {
		t.Error("chosenPayloadFile never reads the entry's missing flag")
	}
	if !strings.Contains(helpers, "refreshCaptures") {
		t.Error("the set editor would call a file missing before the list loaded or after it failed: loadCaptures swallows errors, refreshCaptures does not")
	}

	udp := readUI(t, udpFile)
	for name, src := range map[string]string{tcpFakingFile: tcp, udpFile: udp} {
		if !strings.Contains(src, "chosenPayloadFile(") {
			t.Errorf("%s never resolves the set's payload file with chosenPayloadFile", name)
		}
		if !strings.Contains(src, "useListedCaptures(") {
			t.Errorf("%s does not know whether the payload list loaded", name)
		}
	}

	keys := map[string][]string{
		tcpFakingFile: {"sets.faking.fakeSni.payloadFileMissing", "sets.faking.fakeSni.payloadFileMissingHelper", "sets.faking.fakeSni.payloadFileQuic", "sets.faking.fakeSni.payloadFileQuicHelper"},
		udpFile:       {"sets.udp.fakePayloadFileMissing", "sets.udp.fakePayloadFileMissingHelper"},
	}
	var all []string
	for name, list := range keys {
		src := readUI(t, name)
		for _, key := range list {
			if !regexp.MustCompile(`t\(\s*"` + regexp.QuoteMeta(key) + `"`).MatchString(src) {
				t.Errorf("%s never shows %s", name, key)
			}
		}
		all = append(all, list...)
	}
	requireKeysInBothLocales(t, all)

	zeros := map[string]*regexp.Regexp{"en.json": regexp.MustCompile(`(?i)\bzero`), "ru.json": regexp.MustCompile(`нул`)}
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		google := textForms(doc, "sets.faking.fakeSni.payloadGoogle")["payloadGoogle"]
		for k, text := range textForms(doc, "sets.faking.fakeSni.payloadFileMissingHelper") {
			if google == "" || !strings.Contains(text, google) {
				t.Errorf("%s %s does not say the set sends %q instead: %q", locale, k, google, text)
			}
			if !strings.Contains(text, "{{file}}") {
				t.Errorf("%s %s does not name the missing file: %q", locale, k, text)
			}
		}
		for k, text := range textForms(doc, "sets.udp.fakePayloadFileMissingHelper") {
			if !zeros[locale].MatchString(text) {
				t.Errorf("%s %s does not say the UDP fakes carry zeros instead: %q", locale, k, text)
			}
			if !strings.Contains(text, "{{file}}") {
				t.Errorf("%s %s does not name the missing file: %q", locale, k, text)
			}
		}
		for _, key := range []string{"sets.faking.fakeSni.payloadFileMissing", "sets.udp.fakePayloadFileMissing", "sets.faking.fakeSni.payloadFileQuic"} {
			for k, text := range textForms(doc, key) {
				if !strings.Contains(text, "{{name}}") {
					t.Errorf("%s %s does not carry the payload's name: %q", locale, k, text)
				}
			}
		}
	}
}
