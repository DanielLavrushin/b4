package uicheck

import (
	"regexp"
	"testing"
)

func TestGenerateToastsNameTheNormalizedHost(t *testing.T) {
	model := readUI(t, "models/capture.ts")
	if !regexp.MustCompile(`(?s)interface CaptureGenerateResponse \{[^}]*\bdomain\??:`).MatchString(model) {
		t.Error("CaptureGenerateResponse has no domain, the host name the server normalized")
	}

	body := generateHandler(t)
	toasts := regexp.MustCompile(`t\(\s*"settings\.Capture\.(alreadyCaptured|generateSuccess)"\s*,\s*\{([^}]*)\}`).FindAllStringSubmatch(body, -1)
	if len(toasts) != 2 {
		t.Fatalf("generateCapture shows %d of the two generate toasts", len(toasts))
	}
	for _, m := range toasts {
		if !regexp.MustCompile(`\bdomain:\s*result\.domain\b`).MatchString(m[2]) {
			t.Errorf("settings.Capture.%s names the text as typed, not result.domain: {%s}", m[1], m[2])
		}
	}
}
