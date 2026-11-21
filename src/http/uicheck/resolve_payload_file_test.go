package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestPayloadPickersAskTheServerBeforeCallingAFileMissing(t *testing.T) {
	api := readUI(t, "api/capture.ts")
	if !regexp.MustCompile(`\bresolve:\s*\(`).MatchString(api) || !strings.Contains(api, "/api/capture/resolve") {
		t.Error("captureApi has no resolve call to /api/capture/resolve")
	}
	model := readUI(t, "models/capture.ts")
	answer := regexp.MustCompile(`(?s)export interface \w+ \{[^}]*\bfile:\s*string;[^}]*\blisted:\s*boolean;[^}]*\bexists:\s*boolean;[^}]*\bsize:\s*number;[^}]*\bfacts\??:`)
	if !answer.MatchString(model) {
		t.Error("models/capture.ts has no type for the resolve answer (file, listed, exists, size, facts)")
	}

	helpers := readUI(t, payloadFileHelpers)
	if !regexp.MustCompile(`state:\s*"unlisted"`).MatchString(helpers) {
		t.Error(`ChosenPayloadFile has no "unlisted" state for a file b4 loads although the payload list does not name it`)
	}
	fn := regexp.MustCompile(`(?s)export function chosenPayloadFile\b(.*?)\n}\n`).FindStringSubmatch(helpers)
	if fn == nil {
		t.Fatal("no chosenPayloadFile helper")
	}
	if !regexp.MustCompile(`samePayloadFile\(\s*\w+\.filepath\s*,\s*\w+\.file\s*\)`).MatchString(fn[1]) {
		t.Error("chosenPayloadFile never matches the list against the server's cleaned path, so ./captures//x.bin never finds captures/x.bin")
	}
	if !regexp.MustCompile(`\.exists\b`).MatchString(fn[1]) {
		t.Error("chosenPayloadFile never reads the server's exists answer, so a file outside the list is called missing")
	}
	if strings.Count(fn[1], `state: "unknown"`) < 2 {
		t.Error(`chosenPayloadFile has no "unknown" answer while the server has not answered, so it calls an unmatched file missing too early`)
	}
	hook := regexp.MustCompile(`(?s)export function useResolvedPayloadFile\b(.*?)\n}\n`).FindStringSubmatch(helpers)
	if hook == nil {
		t.Fatal("no useResolvedPayloadFile hook asking /api/capture/resolve")
	}
	if !strings.Contains(hook[1], "captureApi.resolve(") {
		t.Error("useResolvedPayloadFile never calls captureApi.resolve")
	}
	if !regexp.MustCompile(`\blisted\b`).MatchString(hook[1]) {
		t.Error("useResolvedPayloadFile asks before the payload list loaded, or after it failed")
	}

	pickers := map[string]struct{ chosen, valueOf string }{
		tcpFakingFile: {"chosenFile", "tlsCaptures"},
		udpFile:       {"chosenUdpFile", "sortedCaptures"},
	}
	for file, p := range pickers {
		src := readUI(t, file)
		resolved := regexp.MustCompile(`const\s+(\w+)\s*=\s*useResolvedPayloadFile\(([^;]*)\);`).FindStringSubmatch(src)
		if resolved == nil {
			t.Errorf("%s never asks the server about a file the list does not match", file)
			continue
		}
		if !strings.Contains(resolved[2], "Listed") {
			t.Errorf("%s asks the server whether or not the payload list loaded: useResolvedPayloadFile(%s)", file, resolved[2])
		}
		call := regexp.MustCompile(`(?s)const\s+` + p.chosen + `\s*=\s*chosenPayloadFile\((.*?)\);`).FindStringSubmatch(src)
		if call == nil || !regexp.MustCompile(`\b`+resolved[1]+`\b`).MatchString(call[1]) {
			t.Errorf("%s does not pass the server's answer to chosenPayloadFile", file)
		}
		if !regexp.MustCompile(p.chosen + `\.state\s*===\s*"unlisted"`).MatchString(src) {
			t.Errorf(`%s has no branch for an unlisted file`, file)
		}
		mapped := regexp.MustCompile(`(?s)\b` + p.valueOf + `\.map\(\(c\)\s*=>\s*\(\{\s*value:\s*(.*?)\?`).FindStringSubmatch(src)
		if mapped == nil || !regexp.MustCompile(`\bc\s*===|===\s*c\b`).MatchString(mapped[1]) {
			t.Errorf("%s gives a list option the set's spelling only when the strings match, so an alias resolved to that entry has no option", file)
		}
	}

	rule := readUI(t, fakePayloadHelpers)
	tlsMod := regexp.MustCompile(`(?s)case FakingPayloadType\.CAPTURE:\s*return\s+(.*?);`).FindStringSubmatch(rule)
	if tlsMod == nil || !strings.Contains(tlsMod[1], `"unlisted"`) {
		t.Error("the TLS modification rule ignores the facts of an unlisted file")
	}

	tcp := readUI(t, tcpFakingFile)
	udp := readUI(t, udpFile)
	for src, keys := range map[string][]string{
		tcp: {`t\(\s*"sets\.faking\.fakeSni\.payloadFileUnlisted"\s*,\s*\{[^}]*name:`, `t\(\s*"sets\.faking\.fakeSni\.payloadFileUnlistedHelper"\s*,\s*\{[^}]*file:`},
		udp: {
			`t\(\s*"sets\.udp\.fakePayloadFileUnlisted"\s*,\s*\{[^}]*name:`, `t\(\s*"sets\.udp\.fakePayloadFileUnlistedHelper"\s*,\s*\{[^}]*file:`,
			`t\(\s*"sets\.udp\.fakePayloadFileIsEmpty"\s*,\s*\{[^}]*name:`, `t\(\s*"sets\.udp\.fakePayloadFileIsEmptyHelper"\s*,\s*\{[^}]*file:`,
		},
	} {
		for _, re := range keys {
			if !regexp.MustCompile(re).MatchString(src) {
				t.Errorf("a picker never shows %s", re)
			}
		}
	}

	keys := []string{
		"sets.faking.fakeSni.payloadFileUnlisted",
		"sets.faking.fakeSni.payloadFileUnlistedHelper",
		"sets.udp.fakePayloadFileUnlisted",
		"sets.udp.fakePayloadFileUnlistedHelper",
	}
	requireKeysInBothLocales(t, keys)
	listWord := map[string]string{"en.json": "list", "ru.json": "списк"}
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for _, key := range keys {
			for k, text := range textForms(doc, key) {
				want := "{{name}}"
				if strings.HasSuffix(key, "Helper") {
					want = "{{file}}"
				}
				if !strings.Contains(text, want) {
					t.Errorf("%s %s does not carry %s: %q", locale, k, want, text)
				}
				if !strings.Contains(text, listWord[locale]) {
					t.Errorf("%s %s does not say the file is not in the payload list: %q", locale, k, text)
				}
			}
		}
	}

	requireKeysInBothLocales(t, []string{"sets.udp.fakePayloadFileIsEmpty", "sets.udp.fakePayloadFileIsEmptyHelper"})
	zeros := map[string]*regexp.Regexp{"en.json": regexp.MustCompile(`(?i)\bzero`), "ru.json": regexp.MustCompile(`нул`)}
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for k, text := range textForms(doc, "sets.udp.fakePayloadFileIsEmptyHelper") {
			if !zeros[locale].MatchString(text) || !strings.Contains(text, "{{file}}") {
				t.Errorf("%s %s does not name the empty file and say the UDP fakes carry zeros: %q", locale, k, text)
			}
		}
	}
}
