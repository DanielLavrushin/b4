package uicheck

import (
	"regexp"
	"testing"
)

const tlsModOrMatchMode = `\??\.tls_mod\s*!==\s*false\s*\|\|\s*config\.faking\.fake_len_mode\s*===\s*"match"`

func tlsModRuleFor(helpers, expr, facts string) string {
	q := regexp.QuoteMeta(facts)
	clientHello := q + `\?\.kind\s*===\s*"tls_client_hello"\s*&&\s*\(?\s*`
	if regexp.MustCompile(clientHello + q + tlsModOrMatchMode).MatchString(expr) {
		return ""
	}
	call := regexp.MustCompile(clientHello + `(\w+)\(\s*config\s*,\s*` + q + `\s*\)`).FindStringSubmatch(expr)
	if call == nil {
		return "no tls_mod check joined to the kind check by &&"
	}
	def := regexp.MustCompile(`(?s)(?:const\s+` + call[1] + `\s*=\s*|function\s+` + call[1] + `\s*)\(\s*config:\s*B4SetConfig\s*,\s*(\w+)\b(.*?)(?:;\n|\n}\n)`).FindStringSubmatch(helpers)
	if def == nil {
		return call[1] + " is not defined beside the rule with a config and a facts parameter"
	}
	if !regexp.MustCompile(regexp.QuoteMeta(def[1]) + tlsModOrMatchMode).MatchString(def[2]) {
		return call[1] + ` does not read tls_mod !== false || fake_len_mode === "match"`
	}
	return ""
}

func TestTLSModificationSwitchesHideForAPayloadFileTheyCannotChange(t *testing.T) {
	model := readUI(t, "models/capture.ts")
	if !regexp.MustCompile(`(?s)interface PayloadFacts \{[^}]*\btls_mod\?:\s*boolean;`).MatchString(model) {
		t.Error("the PayloadFacts model has no tls_mod field")
	}

	helpers := readUI(t, fakePayloadHelpers)
	fn := regexp.MustCompile(`(?s)export function tlsModChangesTheFake\b(.*?)\n}\n`).FindStringSubmatch(helpers)
	if fn == nil {
		t.Fatal("no tlsModChangesTheFake rule deciding when Fake Packet TLS Modification can change the fake")
	}
	returns, _ := switchReturns(fn[1])
	capture := returns["CAPTURE"]
	for what, facts := range map[string]string{
		"a listed payload file":    "chosen.capture.facts",
		"an unlisted payload file": "chosen.facts",
	} {
		if problem := tlsModRuleFor(helpers, capture, facts); problem != "" {
			t.Errorf("for %s the switches still show when TLS modification leaves the file unchanged and Match Fake Size is off (%s): %q", what, problem, capture)
		}
	}
}
