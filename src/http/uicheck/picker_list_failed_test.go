package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestPayloadPickersSayWhenTheListCouldNotBeLoaded(t *testing.T) {
	helpers := readUI(t, payloadFileHelpers)
	hook := regexp.MustCompile(`(?s)export function useListedCaptures\b(.*?)\n}\n`).FindStringSubmatch(helpers)
	if hook == nil {
		t.Fatal("no useListedCaptures hook")
	}
	if !regexp.MustCompile(`return\s*\{[^}]*\blistError\b`).MatchString(hook[1]) {
		t.Error("useListedCaptures does not pass on a failed list load, so the pickers say no payloads exist")
	}

	for file, noneKeys := range map[string][]string{
		tcpFakingFile: {`"sets.faking.fakeSni.noPayloadsHelper"`, `"sets.faking.fakeSni.noPayloads"`},
		udpFile:       {`"sets.udp.fakePayloadFileEmpty"`},
	} {
		src := readUI(t, file)
		if !regexp.MustCompile(`\{[^}]*\blistError\b[^}]*\}\s*=\s*useListedCaptures\(`).MatchString(src) {
			t.Errorf("%s does not read listError from useListedCaptures", file)
		}
		if !regexp.MustCompile(`t\(\s*"settings\.Capture\.listFailed"\s*,\s*\{[^}]*error:\s*listError\b`).MatchString(src) {
			t.Errorf("%s never says that the payload list could not be loaded (settings.Capture.listFailed)", file)
		}
		for _, key := range noneKeys {
			at := strings.Index(src, key)
			if at < 0 {
				t.Errorf("%s no longer shows %s", file, key)
				continue
			}
			if !strings.Contains(src[max(0, at-250):at], "listError") {
				t.Errorf("%s shows %s, no payloads, when the list could not be loaded", file, key)
			}
		}
	}
	requireKeysInBothLocales(t, []string{"settings.Capture.listFailed"})
}
