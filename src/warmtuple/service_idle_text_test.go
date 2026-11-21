package warmtuple

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/ai"
)

func TestIdleRetentionIsStatedWhereWarmValidityIs(t *testing.T) {
	minutes := int(idleRetention / time.Minute)
	if time.Duration(minutes)*time.Minute != idleRetention {
		t.Fatalf("an idle warmed port is dropped after %s, not a whole number of minutes as the texts state it", idleRetention)
	}
	topic := "tcp.wssize_reuse.max_age_seconds"
	if want := fmt.Sprintf("An idle tuple is also dropped after %d minutes", minutes); !strings.Contains(ai.TopicFacts(topic), want) {
		t.Errorf("%s does not say %q: %s", topic, want, ai.TopicFacts(topic))
	}
	for locale, format := range map[string]string{
		"en": "A warmed port that carries no connection for %d minutes is dropped earlier.",
		"ru": "Прогретый порт, через который %d минут",
	} {
		raw, err := os.ReadFile("../http/ui/src/i18n/" + locale + ".json")
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			Sets struct {
				TCP struct {
					General map[string]any `json:"general"`
				} `json:"tcp"`
			} `json:"sets"`
		}
		if err := json.Unmarshal(raw, &doc); err != nil {
			t.Fatal(err)
		}
		help, _ := doc.Sets.TCP.General["wssizeReuseAgeDesc"].(string)
		want := fmt.Sprintf(format, minutes)
		if !strings.Contains(help, want) {
			t.Errorf("%s sets.tcp.general.wssizeReuseAgeDesc does not say %q, so it promises the full validity to a port b4 drops after %d idle minutes: %q", locale, want, minutes, help)
		}
		page := "../../docs/docs/sets/tcp/general.md"
		if locale == "ru" {
			page = "../../docs/i18n/ru/docusaurus-plugin-content-docs/current/sets/tcp/general.md"
		}
		text, err := os.ReadFile(page)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(text), want) {
			t.Errorf("%s does not say %q, so it promises the full validity to a port b4 drops after %d idle minutes", page, want, minutes)
		}
	}
}
