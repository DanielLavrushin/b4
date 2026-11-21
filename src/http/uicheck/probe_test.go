package uicheck

import (
	"regexp"
	"testing"
)

var probeWordRe = regexp.MustCompile(`\bprobe\b`)

func TestPayloadsUIHasNoCaptureProbe(t *testing.T) {
	for _, f := range []string{"api/capture.ts", "hooks/useCapture.ts", "models/capture.ts"} {
		if loc := probeWordRe.FindStringIndex(readUI(t, f)); loc != nil {
			t.Errorf("%s still names the removed capture probe at byte %d", f, loc[0])
		}
	}
}

func TestPayloadCopySaysGenerateOrUpload(t *testing.T) {
	keys := []string{
		"discovery.options.payloads",
		"discovery.options.payloadsHint",
		"discovery.options.selectPayloads",
		"discovery.options.noPayloads",
		"discovery.results.nothingFound",
		"settings.Capture.clearAll",
		"settings.Capture.clearConfirm",
		"settings.Capture.clearInUseConfirm",
		"settings.Backup.downloadInfo",
		"sets.udp.fakePayloadFileHelper",
		"sets.udp.fakePayloadFileEmpty",
	}
	capturing := map[string]*regexp.Regexp{
		"en.json": regexp.MustCompile(`(?i)captur`),
		"ru.json": regexp.MustCompile(`(?i)захва`),
	}
	for _, locale := range locales {
		t.Run(locale, func(t *testing.T) {
			doc := loadLocale(t, locale)
			for _, key := range keys {
				for k, text := range requireKey(t, doc, locale, key) {
					if capturing[locale].MatchString(text) {
						t.Errorf("%s %s (%s) still speaks of capturing: %q", locale, key, k, text)
					}
				}
			}
		})
	}
}
