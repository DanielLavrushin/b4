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
		if !regexp.MustCompile(`\bbytesLabel\(\s*t\s*,`).MatchString(src) {
			t.Errorf("%s does not label payload sizes with bytesLabel, the plural-aware settings.Capture.bytes", f)
		}
	}
	requireKeysInBothLocales(t, []string{"settings.Capture.bytes", "core.units.bytes"})
}
