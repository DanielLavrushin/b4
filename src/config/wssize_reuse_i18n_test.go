package config

import (
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"
)

var collectAndReuseStringKeys = []string{
	"connections.flags.ipCollected",
	"connections.flags.wssizeReuseFallback",
	"sets.tcp.general.wssizeReuse",
	"sets.tcp.general.ibdCollect",
	"sets.targets.useCollectedIps",
	"dashboard.collected.",
	"errors.wssize_reuse_",
	"sets.tcp.general.mssScope",
	"settings.Devices.forgetDialog.mssBlocked",
}

func flattenUIStrings(prefix string, node any, out map[string]string) {
	switch v := node.(type) {
	case map[string]any:
		for key, child := range v {
			path := key
			if prefix != "" {
				path = prefix + "." + key
			}
			flattenUIStrings(path, child, out)
		}
	case string:
		out[prefix] = v
	}
}

func collectAndReuseStrings(t *testing.T, locale string) map[string]string {
	t.Helper()
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
	picked := make(map[string]string)
	for key, text := range all {
		if slices.ContainsFunc(collectAndReuseStringKeys, func(prefix string) bool { return strings.HasPrefix(key, prefix) }) {
			picked[key] = text
		}
	}
	for _, prefix := range collectAndReuseStringKeys {
		found := false
		for key := range picked {
			if strings.HasPrefix(key, prefix) {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("%s.json has no string under %s, so this test would check nothing there", locale, prefix)
		}
	}
	return picked
}

func TestRussianCollectAndReuseStringsHaveNoEnglishTerms(t *testing.T) {
	banned := []string{"tuple", "reuse", "firewall", "source port", "source device", "handshake", "hub", "collect", "listener"}
	for key, text := range collectAndReuseStrings(t, "ru") {
		lower := strings.ToLower(text)
		for _, word := range banned {
			if strings.Contains(lower, word) {
				t.Errorf("ru %s says %q in English: %q", key, word, text)
			}
		}
	}
}

func TestEnglishCollectAndReuseStringsUsePlainWords(t *testing.T) {
	for key, text := range collectAndReuseStrings(t, "en") {
		lower := strings.ToLower(text)
		for _, word := range []string{"tuple", "backend"} {
			if strings.Contains(lower, word) {
				t.Errorf("en %s says %q, not a word the UI uses for it: %q", key, word, text)
			}
		}
		if strings.Count(text, "Hub") != strings.Count(text, "Community Hub") {
			t.Errorf("en %s names the hub other than as the Community Hub: %q", key, text)
		}
	}
}

func TestReuseSwitchDescriptionNamesUseCollectedIPs(t *testing.T) {
	c := reuseConfig(t)
	c.Sets[0].Targets.IPs = nil
	c.Sets[0].Targets.IpsToMatch = nil
	c.Sets[0].Targets.UseCollectedIPs = true
	if err := c.Validate(); err != nil {
		t.Fatalf("a reuse set whose only target is Use collected IPs was refused: %v", err)
	}
	for locale, want := range useCollectedIPsLabel {
		text, ok := uiStrings(t, locale)["sets.tcp.general.wssizeReuseInfo"]
		if !ok {
			t.Fatalf("%s.json has no sets.tcp.general.wssizeReuseInfo", locale)
		}
		if !strings.Contains(text, want) {
			t.Errorf("%s sets.tcp.general.wssizeReuseInfo lists the targets a reuse set may have, and b4 saves one whose only target is Use collected IPs, but it does not name %q: %q", locale, want, text)
		}
	}
}

func TestWarmValidityHelpStatesTheRangeAndTheDefault(t *testing.T) {
	for locale, format := range map[string]string{"en": "1 to %d, default %d.", "ru": "От 1 до %d, по умолчанию %d."} {
		text, ok := uiStrings(t, locale)["sets.tcp.general.wssizeReuseAgeDesc"]
		if !ok {
			t.Fatalf("%s.json has no sets.tcp.general.wssizeReuseAgeDesc", locale)
		}
		if want := fmt.Sprintf(format, MaxWSReuseMaxAgeSeconds, DefaultWSReuseMaxAgeSeconds); !strings.HasPrefix(text, want) {
			t.Errorf("%s sets.tcp.general.wssizeReuseAgeDesc does not start with %q, the range and default a set gets: %q", locale, want, text)
		}
	}
}
