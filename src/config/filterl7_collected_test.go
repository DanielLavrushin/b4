package config

import (
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestACallsOnlySetThatUsesCollectedIPsIsToldToTurnThemOff(t *testing.T) {
	set := l7Set(t)
	set.Targets.UseCollectedIPs = true
	err := l7Config(t, set).Validate()
	var verr *ValidationError
	if !errors.As(err, &verr) || len(verr.Fields) == 0 || verr.Fields[0].Path != "sets[0].udp.filter_l7" {
		t.Fatalf("a calls-only set with Use collected IPs validated as %v, want the filter_l7 refusal", err)
	}
	if !strings.Contains(verr.Fields[0].Message, "collected IP") {
		t.Errorf("the refusal does not name collected IPs, although Use collected IPs alone triggers it: %q", verr.Fields[0].Message)
	}
	for locale, want := range map[string]string{"en": "Use collected IPs", "ru": "«Использовать собранные IP»"} {
		raw, err := os.ReadFile("../http/ui/src/i18n/" + locale + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var doc map[string]any
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		all := make(map[string]string)
		flattenUIStrings("", doc, all)
		text, ok := all["sets.udp.filterL7TargetsWarning"]
		if !ok {
			t.Fatalf("%s.json has no sets.udp.filterL7TargetsWarning", locale)
		}
		if !strings.Contains(text, want) {
			t.Errorf("%s sets.udp.filterL7TargetsWarning shows for a set whose only target is Use collected IPs but does not name %q: %q", locale, want, text)
		}
	}
}
