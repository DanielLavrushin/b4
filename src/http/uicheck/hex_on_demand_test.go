package uicheck

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestPayloadHexIsFetchedForOnePayloadOnDemand(t *testing.T) {
	err := filepath.WalkDir(uiSrc, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !(strings.HasSuffix(path, ".ts") || strings.HasSuffix(path, ".tsx")) {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(raw), "hex_data") {
			t.Errorf("%s still reads hex_data, which the list no longer carries", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	api := readUI(t, "api/capture.ts")
	if !strings.Contains(api, "/api/capture/download") || !strings.Contains(api, "arrayBuffer()") {
		t.Error("captureApi does not fetch one payload's bytes from /api/capture/download")
	}

	page := payloadsPageSource(t)
	if !regexp.MustCompile(`toString\(16\)`).MatchString(page) {
		t.Error("the Payloads page does not hex-encode the fetched bytes itself")
	}
	if !regexp.MustCompile(`t\(\s*"settings\.Capture\.hexFailed"\s*,\s*\{[^}]*error:`).MatchString(page) {
		t.Error("a failed hex fetch is not shown with the server's text")
	}
	requireKeysInBothLocales(t, []string{"settings.Capture.hexFailed"})
	for _, locale := range locales {
		for k, text := range textForms(loadLocale(t, locale), "settings.Capture.hexFailed") {
			if !strings.Contains(text, "{{error}}") {
				t.Errorf("%s settings.Capture.%s has no {{error}}: %q", locale, k, text)
			}
		}
	}
}
