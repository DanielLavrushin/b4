package uicheck

import (
	"regexp"
	"testing"
)

func TestSetEditorPayloadPickersRouteByteUnitsThroughI18n(t *testing.T) {
	literal := regexp.MustCompile(`(?i)[^.\w]bytes\b`)
	for _, f := range []string{tcpFakingFile, udpFile} {
		src := readUI(t, f)
		for _, m := range literal.FindAllString(src, -1) {
			t.Errorf("%s prints a literal byte unit: %q", f, m)
		}
		if !regexp.MustCompile(`\b(bytesLabel|payloadNote)\(\s*t\s*,`).MatchString(src) {
			t.Errorf("%s does not label payload sizes with bytesLabel, the plural-aware settings.Capture.bytes", f)
		}
		if regexp.MustCompile(`\bpayloadNote\(\s*t\s*,`).MatchString(src) && !regexp.MustCompile(`\bbytesLabel\(\s*t\s*,`).MatchString(readUI(t, payloadFileHelpers)) {
			t.Errorf("%s labels sizes through payloadNote, which does not use bytesLabel", f)
		}
	}
	requireKeysInBothLocales(t, []string{"settings.Capture.bytes", "core.units.bytes"})
}
