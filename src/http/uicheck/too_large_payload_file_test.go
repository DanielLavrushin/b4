package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestPayloadPickersJudgeAnUnlistedFileBySizeNotByFacts(t *testing.T) {
	model := readUI(t, "models/capture.ts")
	answer := regexp.MustCompile(`(?s)export interface PayloadFileResolution \{([^}]*)\}`).FindStringSubmatch(model)
	if answer == nil || !regexp.MustCompile(`\btoo_large:\s*boolean;`).MatchString(answer[1]) {
		t.Error("the resolve answer type has no too_large: boolean, so the set editor cannot tell a file over the payload limit from any other")
	}

	helpers := readUI(t, payloadFileHelpers)
	unlisted := regexp.MustCompile(`\{\s*state:\s*"unlisted";([^}]*)\}`).FindStringSubmatch(helpers)
	if unlisted == nil {
		t.Fatal(`ChosenPayloadFile has no "unlisted" state`)
	}
	if !regexp.MustCompile(`\bfacts\?:`).MatchString(unlisted[1]) || !regexp.MustCompile(`\btooLarge:\s*boolean\b`).MatchString(unlisted[1]) {
		t.Errorf(`the "unlisted" state needs facts optional and a tooLarge flag, since a file the server cannot judge still goes out over UDP: {%s}`, unlisted[1])
	}
	fn := regexp.MustCompile(`(?s)export function chosenPayloadFile\b(.*?)\n}\n`).FindStringSubmatch(helpers)
	if fn == nil {
		t.Fatal("no chosenPayloadFile helper")
	}
	for _, cond := range regexp.MustCompile(`\bif\s*\((.*?)\)\s*(?:\{|return\b)`).FindAllStringSubmatch(fn[1], -1) {
		if strings.Contains(cond[1], ".facts") {
			t.Errorf("chosenPayloadFile decides a file's state from its facts (if (%s)), so a file that exists but carries no facts, such as one over the payload limit, is called missing", cond[1])
		}
	}
	if !regexp.MustCompile(`\btooLarge:\s*\w+\.too_large\b`).MatchString(fn[1]) {
		t.Error("chosenPayloadFile does not carry the server's too_large answer on an unlisted file")
	}

	tcp := readUI(t, tcpFakingFile)
	branch := regexp.MustCompile(`(?s)chosenFile\.state\s*===\s*"unlisted"\s*&&\s*chosenFile\.tooLarge\s*\)\s*\{(.*?)\n  \}`).FindStringSubmatchIndex(tcp)
	if branch == nil {
		t.Fatal("the TCP picker has no branch for a chosen file over the payload limit, driven by too_large")
	}
	body := tcp[branch[2]:branch[3]]
	for what, re := range map[string]string{
		"marks the option as too large":          `t\(\s*"sets\.faking\.fakeSni\.payloadFileTooLarge"\s*,\s*\{[^}]*name:`,
		"says what the set sends instead":        `note:\s*t\(\s*"sets\.faking\.fakeSni\.payloadFileMissingNote"`,
		"explains the fallback under the picker": `t\(\s*"sets\.faking\.fakeSni\.payloadFileTooLargeHelper"\s*,\s*\{[^}]*file:`,
	} {
		if !regexp.MustCompile(re).MatchString(body) {
			t.Errorf("the too-large branch never %s (%s)", what, re)
		}
	}
	if generic := strings.Index(tcp, `"sets.faking.fakeSni.payloadFileUnlisted"`); generic >= 0 && generic < branch[0] {
		t.Error("the TCP picker checks for an unlisted file before a too-large one, so a file the TCP loader refuses is offered as sent as it is")
	}
	size := regexp.MustCompile(`(?s)const\s+chosenSize\s*=(.*?);`).FindStringSubmatch(tcp)
	if size == nil || !strings.Contains(size[1], "tooLarge") {
		t.Error("the TCP picker counts a file over the payload limit toward the one-packet warning, although the set sends Preset: Google (classic) instead")
	}

	rule := readUI(t, fakePayloadHelpers)
	capture := regexp.MustCompile(`(?s)case FakingPayloadType\.CAPTURE:\s*return\s+(.*?);`).FindStringSubmatch(rule)
	if capture == nil || !strings.Contains(capture[1], "tooLarge") {
		t.Error("the TLS modification rule does not treat a file over the payload limit like a missing one, whose fallback is a ClientHello")
	}

	udp := readUI(t, udpFile)
	if strings.Contains(udp, "tooLarge") || strings.Contains(udp, "too_large") {
		t.Error("the UDP picker reads the payload limit, which the UDP loader does not apply")
	}
	if !regexp.MustCompile(`chosenUdpFile\.state\s*===\s*"unlisted"\s*\?\s*chosenUdpFile\.size\b`).MatchString(udp) {
		t.Error("the size of an unlisted UDP payload file does not reach the note on how much of it goes out")
	}

	keys := []string{"sets.faking.fakeSni.payloadFileTooLarge", "sets.faking.fakeSni.payloadFileTooLargeHelper"}
	requireKeysInBothLocales(t, keys)
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		google := textForms(doc, "sets.faking.fakeSni.payloadGoogle")["payloadGoogle"]
		missing := textForms(doc, "sets.faking.fakeSni.payloadFileMissing")["payloadFileMissing"]
		for k, text := range textForms(doc, "sets.faking.fakeSni.payloadFileTooLarge") {
			if !strings.Contains(text, "{{name}}") || text == missing {
				t.Errorf("%s %s does not carry the payload's name with a mark of its own: %q", locale, k, text)
			}
		}
		for k, text := range textForms(doc, "sets.faking.fakeSni.payloadFileTooLargeHelper") {
			if google == "" || !strings.Contains(text, google) || !strings.Contains(text, "{{file}}") {
				t.Errorf("%s %s does not name the file and say the set sends %q instead: %q", locale, k, google, text)
			}
		}
	}
}
