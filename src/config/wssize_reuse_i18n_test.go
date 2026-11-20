package config

import (
	"encoding/json"
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
