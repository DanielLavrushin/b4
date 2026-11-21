package uicheck

import (
	"regexp"
	"strings"
	"testing"
)

func TestDecoyCountsOnlyUnderAStrategyThatSendsComboFragments(t *testing.T) {
	helpers := readUI(t, fakePayloadHelpers)
	body := regexp.MustCompile(`(?s)export const sendsSynOrDecoyFake\b(.*?);\n`).FindStringSubmatch(helpers)
	if body == nil {
		t.Fatal("no sendsSynOrDecoyFake helper")
	}
	gated := regexp.MustCompile(`decoy_enabled\s*&&\s*sendsComboFragments\(\s*config\s*\)|sendsComboFragments\(\s*config\s*\)\s*&&[^|]*decoy_enabled`)
	if !gated.MatchString(body[1]) {
		t.Errorf("sendsSynOrDecoyFake counts the combo decoy whatever the strategy, but Go sends it only from the combo fragment path: %s", strings.TrimSpace(body[1]))
	}

	fn := regexp.MustCompile(`(?s)export function sendsComboFragments\b(.*?)\n}\n`).FindStringSubmatch(helpers)
	if fn == nil {
		t.Fatal("no sendsComboFragments helper mirroring the strategy switch of nfq.go and hybrid.go")
	}
	for what, re := range map[string]string{
		"reads the strategy pool, which Go picks from when it is not empty": `fragmentation\.strategy_pool`,
		"falls back to the single strategy":                                 `fragmentation\.strategy\b[^_]`,
		"looks at the pool only when it is not empty":                       `\.length\b`,
		"counts combo": `"combo"`,
		"counts hybrid, whose ClientHello branch sends combo fragments": `"hybrid"`,
	} {
		if !regexp.MustCompile(re).MatchString(fn[1]) {
			t.Errorf("sendsComboFragments never %s (%s)", what, re)
		}
	}
	for _, other := range []string{"tcp", "ip", "oob", "tls", "disorder", "extsplit", "firstbyte", "none"} {
		if strings.Contains(fn[1], `"`+other+`"`) {
			t.Errorf("sendsComboFragments names %q, a strategy that never sends the decoy", other)
		}
	}
}
