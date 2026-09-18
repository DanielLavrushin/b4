package web

import (
	"strings"

	"github.com/daniellavrushin/b4/sni"
)

const (
	TidyDeadWildcard = "dead_wildcard"
	TidyCovered      = "covered"
	TidyDuplicate    = "duplicate"
	TidyWWWOnly      = "www_only"
)

type Suggestion struct {
	Kind        string `json:"kind"`
	Entry       string `json:"entry"`
	By          string `json:"by,omitempty"`
	Replacement string `json:"replacement,omitempty"`
}

type Tidy struct {
	Suggestions []Suggestion `json:"suggestions"`
	Domains     []string     `json:"domains"`
}

type tidyItem struct {
	entry    string
	value    string
	regex    bool
	replaced bool
}

func TidyDomains(entries []string) *Tidy {
	present := make(map[string]bool, len(entries))
	for _, entry := range entries {
		value, isRegex := sni.ParseDomainEntry(entry)
		if value != "" && !isRegex && !strings.HasPrefix(value, "*") {
			present[value] = true
		}
	}
	suggestions := make([]Suggestion, 0)
	items := make([]tidyItem, 0, len(entries))
	for _, entry := range entries {
		canon := sni.CanonicalDomainEntry(entry)
		if canon == "" {
			continue
		}
		value, isRegex := sni.ParseDomainEntry(canon)
		if isRegex {
			items = append(items, tidyItem{entry: entry, value: canon, regex: true})
			continue
		}
		if strings.HasPrefix(value, "*") {
			base := strings.TrimLeft(value, "*.")
			if base == "" || present[base] {
				suggestions = append(suggestions, Suggestion{Kind: TidyDeadWildcard, Entry: entry, By: base})
				continue
			}
			present[base] = true
			suggestions = append(suggestions, Suggestion{Kind: TidyDeadWildcard, Entry: entry, Replacement: base})
			items = append(items, tidyItem{entry: entry, value: base, replaced: true})
			continue
		}
		if base := strings.TrimPrefix(value, "www."); base != value && base != "" && !present[base] {
			present[base] = true
			suggestions = append(suggestions, Suggestion{Kind: TidyWWWOnly, Entry: entry, Replacement: base})
			items = append(items, tidyItem{entry: entry, value: base, replaced: true})
			continue
		}
		items = append(items, tidyItem{entry: entry, value: value})
	}

	seen := make(map[string]string, len(items))
	kept := make([]tidyItem, 0, len(items))
	for _, it := range items {
		if first, ok := seen[it.value]; ok {
			if !it.replaced {
				suggestions = append(suggestions, Suggestion{Kind: TidyDuplicate, Entry: it.entry, By: first})
			}
			continue
		}
		seen[it.value] = it.entry
		kept = append(kept, it)
	}

	domains := make([]string, 0, len(kept))
	for i, it := range kept {
		if !it.regex {
			if by, ok := coveringEntry(kept, i); ok {
				suggestions = append(suggestions, Suggestion{Kind: TidyCovered, Entry: it.entry, By: by})
				continue
			}
		}
		domains = append(domains, it.value)
	}
	if len(suggestions) == 0 {
		return nil
	}
	return &Tidy{Suggestions: suggestions, Domains: domains}
}

func coveringEntry(items []tidyItem, i int) (string, bool) {
	for j, other := range items {
		if j == i || other.regex {
			continue
		}
		if rel, _ := sni.MatchDomainEntry(other.value, items[i].value); rel == sni.RelationCovered {
			return other.value, true
		}
	}
	return "", false
}
