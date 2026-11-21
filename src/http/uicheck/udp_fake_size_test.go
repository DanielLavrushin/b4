package uicheck

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestUDPTabSaysHowMuchOfThePayloadGoesOut(t *testing.T) {
	udp := readUI(t, udpFile)

	alert := regexp.MustCompile(`t\(\s*"sets\.udp\.fakePayloadTruncated"\s*,\s*\{[^}]*\blen:[^}]*\bcount:`)
	if !alert.MatchString(udp) {
		t.Error(`the UDP tab never says "only the first N of M bytes go out" for a payload longer than Fake Packet Size`)
	}
	if !regexp.MustCompile(`onChange\(\s*"udp\.fake_len"\s*,\s*Math\.min\([^)]*\bUDP_FAKE_LEN_MAX\b`).MatchString(udp) {
		t.Error("no action setting Fake Packet Size to the payload's size, capped at what one UDP fake carries")
	}
	if !regexp.MustCompile(`t\(\s*"sets\.udp\.fakePayloadUseSize"\s*\)`).MatchString(udp) {
		t.Error(`the action is not labelled "Use the file size"`)
	}
	if !strings.Contains(udp, "useBuiltinPayloads(") {
		t.Error("the sizes of the QUIC presets do not come from /api/capture/builtin")
	}
	for _, id := range []string{`"quic1"`, `"quic2"`} {
		if !strings.Contains(udp, id) {
			t.Errorf("the UDP tab never looks up the built-in %s", id)
		}
	}

	slider := jsxElement(t, udp, "B4Slider", `path="udp.fake_len"`)
	if !regexp.MustCompile(`max=\{\s*UDP_FAKE_LEN_MAX\s*\}`).MatchString(slider) {
		t.Errorf("the Fake Packet Size slider does not stop at the shared UDP_FAKE_LEN_MAX: %s", slider)
	}
	if !regexp.MustCompile(`export const UDP_FAKE_LEN_MAX\s*=\s*1472;`).MatchString(readUI(t, "models/config.ts")) {
		t.Error("models/config.ts does not hold the UDP fake cap, config.MaxUDPFakeLen (1472)")
	}
	literal := regexp.MustCompile(`\b1472\b`)
	err := filepath.WalkDir(uiSrc, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !(strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")) {
			return nil
		}
		rel, _ := filepath.Rel(uiSrc, path)
		rel = filepath.ToSlash(rel)
		if rel == "models/config.ts" {
			return nil
		}
		if literal.MatchString(readUI(t, rel)) {
			t.Errorf("%s spells out 1472 instead of using UDP_FAKE_LEN_MAX", rel)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	keys := []string{"sets.udp.fakePayloadTruncated", "sets.udp.fakePayloadUseSize", "sets.udp.fakePayloadOverOneFake"}
	requireKeysInBothLocales(t, keys)
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		forms := requireKey(t, doc, locale, "sets.udp.fakePayloadTruncated")
		for _, suffix := range pluralForms[locale] {
			text, ok := forms["fakePayloadTruncated"+suffix]
			if !ok {
				t.Errorf("%s sets.udp.fakePayloadTruncated has no %s form", locale, suffix)
				continue
			}
			for _, v := range []string{"{{len}}", "{{count}}"} {
				if !strings.Contains(text, v) {
					t.Errorf("%s fakePayloadTruncated%s does not carry %s: %q", locale, suffix, v, text)
				}
			}
		}
		for k, text := range textForms(doc, "sets.udp.fakePayloadOverOneFake") {
			if !strings.Contains(text, "{{max}}") {
				t.Errorf("%s %s does not name the cap: %q", locale, k, text)
			}
		}
	}
}
