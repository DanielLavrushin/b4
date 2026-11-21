package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

var contractKinds = []string{"tls_client_hello", "quic_initial", "stun", "dtls_client_hello", "other"}

func TestUploadFormInspectsTheFileWarnsAndAsksBeforeReplacing(t *testing.T) {
	api := readUI(t, "api/capture.ts")
	if !strings.Contains(api, `"/api/capture/inspect"`) {
		t.Error("captureApi never posts the chosen file to /api/capture/inspect")
	}
	if !regexp.MustCompile(`append\(\s*"replace"\s*,\s*"1"\s*\)`).MatchString(api) {
		t.Error(`captureApi.upload cannot send replace=1`)
	}

	model := readUI(t, "models/capture.ts")
	for _, field := range []string{"size", "facts", "protocol", "name", "fits", "limits"} {
		if !regexp.MustCompile(`(?s)interface PayloadInspection \{[^}]*\b` + field + `\??:`).MatchString(model) {
			t.Errorf("the PayloadInspection model has no %s field", field)
		}
	}

	page := payloadsPageSource(t)
	if strings.Contains(page, "detectPayloadProtocol") || strings.Contains(page, "0x16") {
		t.Error("the upload form still guesses the protocol from the first bytes in the browser")
	}
	for what, re := range map[string]string{
		"calls the inspect route":                      `\binspect\(`,
		"reads the suggested name":                     `\.name\b`,
		"reads the suggested protocol":                 `result\.protocol|inspection\.protocol|\.protocol\s*\|\|`,
		"warns from fits for the chosen protocol":      `\.fits\??\.?\[`,
		"reads the detected facts":                     `\.facts\b`,
		"remembers whether the user typed the name":    `\bnameTyped\b`,
		"answers the server's 409 by asking":           `"exists"`,
		"names the sets of a 409 from its used_by":     `used_by`,
		"asks before replacing in a B4Dialog":          `settings\.Capture\.replaceTitle`,
		"uploads with replace once the user confirmed": `(?i)upload\([^)]*,\s*true\s*\)|replace:\s*true`,
	} {
		if !regexp.MustCompile(re).MatchString(page) {
			t.Errorf("the upload form never %s (%s)", what, re)
		}
	}

	keys := []string{
		"settings.Capture.found",
		"settings.Capture.inspecting",
		"settings.Capture.inspectFailed",
		"settings.Capture.nameFromSni",
		"settings.Capture.protocolDetected",
		"settings.Capture.replaceTitle",
		"settings.Capture.replaceInUse",
		"settings.Capture.replaceBody",
		"settings.Capture.replace",
		"settings.Capture.bytes",
		"settings.Capture.sni",
		"settings.Capture.warnings.empty",
		"settings.Capture.warnings.too_large_tls",
		"settings.Capture.warnings.too_large_quic",
		"settings.Capture.warnings.unrecognized",
		"settings.Capture.warnings.malformed",
		"settings.Capture.warnings.kind_mismatch_tls",
		"settings.Capture.warnings.kind_mismatch_quic",
	}
	for _, kind := range contractKinds {
		keys = append(keys, "settings.Capture.kinds."+kind)
	}
	requireKeysInBothLocales(t, keys)
}
