package uicheck

import (
	"io/fs"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"unicode/utf8"
)

const (
	helperLineBackstop  = 120
	helperLineSentences = 2
)

var (
	helperAttrRe  = regexp.MustCompile(`\b(helperText|description|inputHelper)=\{`)
	quotedKeyRe   = regexp.MustCompile(`"([a-zA-Z0-9_]+(?:\.[a-zA-Z0-9_]+)+)"`)
	keyFamilyRe   = regexp.MustCompile("`([a-zA-Z0-9_]+(?:\\.[a-zA-Z0-9_]+)+)\\.\\$\\{[^{}]+\\}`")
	markupRe      = regexp.MustCompile(`<[^>]+>`)
	sentenceEndRe = regexp.MustCompile(`[.!?;]\s+(?:[\p{Lu}"«(]|b4\b)`)
)

func tsxSourceFiles(t *testing.T) []string {
	t.Helper()
	var files []string
	err := filepath.WalkDir(uiSrc, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".tsx") {
			return nil
		}
		rel, err := filepath.Rel(uiSrc, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(files)
	return files
}

func jsxExpression(src string, open int) string {
	depth := 0
	var quote byte
	for i := open; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == '\\' {
				i++
			} else if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '"', '\'', '`':
			quote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[open : i+1]
			}
		}
	}
	return src[open:]
}

func helperLineKeys(t *testing.T) map[string]string {
	t.Helper()
	keys := map[string]string{}
	en := loadLocale(t, "en.json")
	for _, rel := range tsxSourceFiles(t) {
		src := readUI(t, rel)
		for _, at := range helperAttrRe.FindAllStringIndex(src, -1) {
			expr := jsxExpression(src, at[1]-1)
			for _, m := range quotedKeyRe.FindAllStringSubmatch(expr, -1) {
				if _, seen := keys[m[1]]; seen || len(textForms(en, m[1])) == 0 {
					continue
				}
				keys[m[1]] = rel
			}
			for _, m := range keyFamilyRe.FindAllStringSubmatch(expr, -1) {
				members := familyKeys(en, m[1])
				if len(members) == 0 {
					t.Errorf("%s picks a helper line under %s. at run time, but en.json has no key there", rel, m[1])
				}
				for _, key := range members {
					if _, seen := keys[key]; !seen {
						keys[key] = rel
					}
				}
			}
		}
	}
	return keys
}

func familyKeys(doc map[string]any, prefix string) []string {
	var keys []string
	for name, value := range lookupBlock(doc, strings.Split(prefix, ".")) {
		if _, ok := value.(string); ok {
			keys = append(keys, prefix+"."+name)
		}
	}
	sort.Strings(keys)
	return keys
}

func sortedKeys(keys map[string]string) []string {
	names := make([]string, 0, len(keys))
	for key := range keys {
		names = append(names, key)
	}
	sort.Strings(names)
	return names
}

var abbreviations = []string{"e.g.", "i.e.", "etc.", "напр.", "т.е.", "т.д."}

func sentenceCount(plain string) int {
	count := 1
	for _, at := range sentenceEndRe.FindAllStringIndex(plain, -1) {
		head := plain[:at[0]+1]
		abbreviated := false
		for _, a := range abbreviations {
			if strings.HasSuffix(head, a) {
				abbreviated = true
				break
			}
		}
		if !abbreviated {
			count++
		}
	}
	return count
}

func helperLineShape(text string) (int, int) {
	plain := strings.TrimSpace(markupRe.ReplaceAllString(text, ""))
	return utf8.RuneCountInString(plain), sentenceCount(plain)
}

func TestHelperLinesStayShort(t *testing.T) {
	keys := helperLineKeys(t)
	if len(keys) < 100 {
		t.Fatalf("found only %d helper and description keys, the scan no longer matches the components", len(keys))
	}
	longest := map[string]int{}
	sentences := map[string]int{}
	for _, locale := range locales {
		doc := loadLocale(t, locale)
		for key := range keys {
			for _, text := range requireKey(t, doc, locale, key) {
				n, s := helperLineShape(text)
				if n > longest[key] {
					longest[key] = n
				}
				if s > sentences[key] {
					sentences[key] = s
				}
			}
		}
	}
	for _, key := range sortedKeys(keys) {
		n, s := longest[key], sentences[key]
		fits := n <= helperLineBackstop && s <= helperLineSentences
		allowed, known := helperLineBaseline[key]
		switch {
		case fits && known:
			t.Errorf("%s now reads as a helper line: remove it from helperLineBaseline", key)
		case !fits && !known:
			t.Errorf("%s (%s) reads as a paragraph, %d characters in %d sentences: a helper line is one sentence, two at most, and the rest belongs in the info panel", key, keys[key], n, s)
		case known && n > allowed:
			t.Errorf("%s grew from %d to %d characters while it is on the baseline of long helper lines", key, allowed, n)
		}
	}
	for key := range helperLineBaseline {
		if _, used := keys[key]; !used {
			t.Errorf("%s is on helperLineBaseline but no helperText or description shows it any more", key)
		}
	}
}
