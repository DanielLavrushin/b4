package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

const fakePayloadHelpers = "components/sets/tcp/fakePayload.ts"

func TestSetEditorFlagsAnEmptyFakePayloadChoice(t *testing.T) {
	tcp := readUI(t, tcpFakingFile)
	keys := []string{"sets.faking.fakeSni.payloadFileNotChosen", "sets.faking.fakeSni.payloadDomainNotGiven"}
	for _, key := range keys {
		re := regexp.MustCompile(`<B4Alert\s+severity="warning"[^>]*>\s*\{\s*t\(\s*"` + regexp.QuoteMeta(key) + `"`)
		if !re.MatchString(tcp) {
			t.Errorf("the set editor never flags %s in a warning", key)
		}
	}

	helpers := readUI(t, fakePayloadHelpers)
	body := regexp.MustCompile(`(?s)export const sendsFake\b(.*?);\n`).FindStringSubmatch(helpers)
	if body == nil {
		t.Fatal("no sendsFake helper mirroring SetConfig.SendsFake")
	}
	reads := body[1]
	if strings.Contains(reads, "sendsSynOrDecoyFake(") {
		if inner := regexp.MustCompile(`(?s)export const sendsSynOrDecoyFake\b(.*?);\n`).FindStringSubmatch(helpers); inner != nil {
			reads += inner[1]
		}
	}
	for _, field := range []string{"faking.sni", "tcp.syn_fake", "tcp.syn_fake_len", "decoy_enabled"} {
		if !strings.Contains(reads, field) {
			t.Errorf("sendsFake does not read %s, as SetConfig.SendsFake does", field)
		}
	}
	if !strings.Contains(tcp, "sendsFake(config)") {
		t.Error("the empty-choice flag is not gated on the set sending a fake at all")
	}

	requireKeysInBothLocales(t, keys)
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		google := textForms(doc, "sets.faking.fakeSni.payloadGoogle")["payloadGoogle"]
		for _, key := range keys {
			for k, text := range textForms(doc, key) {
				if google == "" || !strings.Contains(text, google) {
					t.Errorf("%s %s does not say the set sends %q instead: %q", locale, k, google, text)
				}
			}
		}
	}
}
