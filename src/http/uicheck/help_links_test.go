package uicheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

const (
	docsEnglish = "../../../docs/docs"
	docsRussian = "../../../docs/i18n/ru/docusaurus-plugin-content-docs/current"
	topicsFile  = "../../ai/topics.json"
)

var (
	learnMoreRe = regexp.MustCompile(`\blearnMore="([^"]+)"`)
	aiTopicRe   = regexp.MustCompile(`\baiTopic="([^"]+)"`)
	headingRe   = regexp.MustCompile(`(?m)^#{2,4}\s+(.*?)(?:\s*\{#([\w-]+)\})?\s*$`)
	codeFenceRe = regexp.MustCompile("(?s)```.*?```")
)

var topicsWithoutFacts = map[string]bool{
	"dns.pins":                             true,
	"mss_clamp":                            true,
	"system.ip_health.retest_interval_sec": true,
	"tcp.http_methodeol":                   true,
	"tcp.ip_block_detect.heal_dns":         true,
	"tcp.ip_block_detect.heal_ttl_sec":     true,
	"tcp.ip_block_detect.syn_detect":       true,
	"tcp.ip_block_detect.syn_threshold":    true,
}

func headingSlug(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteRune('-')
		}
	}
	return b.String()
}

func docAnchors(t *testing.T, root, page string) map[string]bool {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, page+".md"))
	if err != nil {
		t.Errorf("docs page %s is missing under %s", page, root)
		return nil
	}
	anchors := map[string]bool{}
	for _, m := range headingRe.FindAllStringSubmatch(codeFenceRe.ReplaceAllString(string(raw), ""), -1) {
		if m[2] != "" {
			anchors[m[2]] = true
			continue
		}
		anchors[headingSlug(m[1])] = true
	}
	return anchors
}

func TestLearnMoreLinksLandInBothDocsLocales(t *testing.T) {
	seen := 0
	for _, rel := range tsxSourceFiles(t) {
		for _, m := range learnMoreRe.FindAllStringSubmatch(readUI(t, rel), -1) {
			seen++
			page, anchor, _ := strings.Cut(m[1], "#")
			for _, root := range []string{docsEnglish, docsRussian} {
				anchors := docAnchors(t, root, page)
				if anchors == nil || anchor == "" {
					continue
				}
				if !anchors[anchor] {
					t.Errorf("%s links to %s, but %s has no heading with the id %s", rel, m[1], filepath.Join(root, page+".md"), anchor)
				}
			}
		}
	}
	if seen == 0 {
		t.Fatal("no learnMore link found, the scan no longer matches the components")
	}
}

func TestAiTopicsHaveFacts(t *testing.T) {
	raw, err := os.ReadFile(topicsFile)
	if err != nil {
		t.Fatal(err)
	}
	facts := map[string]string{}
	if err := json.Unmarshal(raw, &facts); err != nil {
		t.Fatal(err)
	}
	used := map[string]string{}
	for _, rel := range tsxSourceFiles(t) {
		for _, m := range aiTopicRe.FindAllStringSubmatch(readUI(t, rel), -1) {
			used[m[1]] = rel
		}
	}
	if len(used) < 40 {
		t.Fatalf("found only %d aiTopic attributes, the scan no longer matches the components", len(used))
	}
	for _, topic := range sortedKeys(used) {
		known := strings.TrimSpace(facts[topic]) != ""
		switch {
		case known && topicsWithoutFacts[topic]:
			t.Errorf("%s has facts now: remove it from topicsWithoutFacts", topic)
		case !known && !topicsWithoutFacts[topic]:
			t.Errorf("%s (%s) names a topic with no entry in src/ai/topics.json, so the explanation has nothing to rely on", topic, used[topic])
		}
	}
	for topic := range topicsWithoutFacts {
		if _, ok := used[topic]; !ok {
			t.Errorf("%s is on topicsWithoutFacts but no control names it any more", topic)
		}
	}
}
