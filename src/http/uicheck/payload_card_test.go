package uicheck

import (
	"regexp"
	"testing"
)

var contractOrigins = []string{"generated", "uploaded", "imported"}

func TestPayloadCardShowsFactsWarningsAndUsers(t *testing.T) {
	model := readUI(t, "models/capture.ts")
	for _, field := range []string{"origin", "facts", "warnings", "missing", "used_by"} {
		if !regexp.MustCompile(`(?s)interface Capture \{[^}]*\b` + field + `\??:`).MatchString(model) {
			t.Errorf("the Capture model has no %s field", field)
		}
	}
	for _, field := range []string{"kind", "quic_version", "sni", "extensions", "ech", "fits_one_packet", "faults"} {
		if !regexp.MustCompile(`(?s)interface PayloadFacts \{[^}]*\b` + field + `\??:`).MatchString(model) {
			t.Errorf("the PayloadFacts model has no %s field", field)
		}
	}

	page := payloadsPageSource(t)
	for what, re := range map[string]string{
		"the detected kind":        `\.kind\b`,
		"fits one packet":          `\.fits_one_packet\b`,
		"the SNI":                  `\.sni\b`,
		"the extension count":      `\.extensions\b`,
		"ECH":                      `\.ech\b`,
		"the origin":               `\.origin\b`,
		"the date":                 `\.timestamp\b`,
		"the server's warnings":    `\.warnings\b`,
		"the sets that use it":     `\.used_by\b`,
		"the warning list":         `<PayloadWarnings\b`,
		"the Used by line":         `settings\.Capture\.usedBy`,
		"a card of its own module": `<PayloadCard\b`,
	} {
		if !regexp.MustCompile(re).MatchString(page) {
			t.Errorf("the payload card never shows %s (%s)", what, re)
		}
	}

	keys := []string{
		"settings.Capture.fitsOnePacket",
		"settings.Capture.overOnePacket",
		"settings.Capture.extensions",
		"settings.Capture.ech",
		"settings.Capture.usedBy",
		"settings.Capture.missingFileTls",
		"settings.Capture.missingFileQuic",
		"settings.Capture.kinds.quic_initial_version",
	}
	for _, kind := range contractKinds {
		keys = append(keys, "settings.Capture.kinds."+kind)
	}
	for _, origin := range contractOrigins {
		keys = append(keys, "settings.Capture.origins."+origin)
	}
	for _, code := range []string{"empty", "too_large_tls", "too_large_quic", "unrecognized", "malformed", "kind_mismatch_tls", "kind_mismatch_quic"} {
		keys = append(keys, "settings.Capture.warnings."+code)
	}
	requireKeysInBothLocales(t, keys)
}

func TestPayloadsPageKeysExistInBothLocales(t *testing.T) {
	keys := literalKeys(payloadsPageSource(t))
	if len(keys) == 0 {
		t.Fatal("no t() keys found on the Payloads page")
	}
	requireKeysInBothLocales(t, keys)
}
