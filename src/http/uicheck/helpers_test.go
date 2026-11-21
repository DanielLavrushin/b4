package uicheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"testing"
)

const uiSrc = "../ui/src"

var locales = []string{"en.json", "ru.json"}

var pluralForms = map[string][]string{
	"en.json": {"_one", "_other"},
	"ru.json": {"_one", "_few", "_many"},
}

func readUI(t *testing.T, rel string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(uiSrc, rel))
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(raw)
}

func payloadsPageFiles(t *testing.T) []string {
	t.Helper()
	extra, err := filepath.Glob(filepath.Join(uiSrc, "components/settings/payloads/*"))
	if err != nil {
		t.Fatal(err)
	}
	files := []string{"components/settings/Capture.tsx"}
	for _, f := range extra {
		rel, err := filepath.Rel(uiSrc, f)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, filepath.ToSlash(rel))
	}
	sort.Strings(files[1:])
	return files
}

func payloadsPageSource(t *testing.T) string {
	t.Helper()
	var b strings.Builder
	for _, f := range payloadsPageFiles(t) {
		b.WriteString(readUI(t, f))
		b.WriteString("\n")
	}
	return b.String()
}

func loadLocale(t *testing.T, name string) map[string]any {
	t.Helper()
	raw := readUI(t, filepath.Join("i18n", name))
	var doc map[string]any
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return doc
}

func lookupBlock(doc map[string]any, path []string) map[string]any {
	cur := doc
	for _, p := range path {
		next, ok := cur[p].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

func textForms(doc map[string]any, key string) map[string]string {
	parts := strings.Split(key, ".")
	block := lookupBlock(doc, parts[:len(parts)-1])
	out := map[string]string{}
	if block == nil {
		return out
	}
	last := parts[len(parts)-1]
	for k, v := range block {
		if k != last && !strings.HasPrefix(k, last+"_") {
			continue
		}
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

func requireKey(t *testing.T, doc map[string]any, locale, key string) map[string]string {
	t.Helper()
	forms := textForms(doc, key)
	if len(forms) == 0 {
		t.Errorf("%s has no %s", locale, key)
		return forms
	}
	last := key[strings.LastIndex(key, ".")+1:]
	if _, bare := forms[last]; bare {
		return forms
	}
	for _, suffix := range pluralForms[locale] {
		if _, ok := forms[last+suffix]; !ok {
			t.Errorf("%s %s has plural forms but no %s%s", locale, key, last, suffix)
		}
	}
	return forms
}

var literalKeyRe = regexp.MustCompile(`\bt\(\s*"([a-zA-Z0-9_.]+)"`)

func literalKeys(src string) []string {
	var keys []string
	for _, m := range literalKeyRe.FindAllStringSubmatch(src, -1) {
		if !slices.Contains(keys, m[1]) {
			keys = append(keys, m[1])
		}
	}
	sort.Strings(keys)
	return keys
}

func requireKeysInBothLocales(t *testing.T, keys []string) {
	t.Helper()
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for _, key := range keys {
			requireKey(t, doc, locale, key)
		}
	}
}
