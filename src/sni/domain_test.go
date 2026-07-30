package sni

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestMatchDomainEntry(t *testing.T) {
	tests := []struct {
		name     string
		entry    string
		domain   string
		relation DomainRelation
		matched  string
	}{
		{"exact", "example.com", "example.com", RelationExact, "example.com"},
		{"exact case insensitive", "Example.COM", "example.com", RelationExact, "example.com"},
		{"exact trailing dot", "example.com.", "example.com.", RelationExact, "example.com"},
		{"subdomain covered", "example.com", "www.example.com", RelationCovered, "example.com"},
		{"deep subdomain covered", "example.com", "a.b.example.com", RelationCovered, "example.com"},
		{"parent covers entry", "www.example.com", "example.com", RelationCovers, "www.example.com"},
		{"partial label no match", "example.com", "notexample.com", RelationNone, ""},
		{"suffix without dot no match", "ample.com", "example.com", RelationNone, ""},
		{"unrelated", "example.com", "example.org", RelationNone, ""},
		{"regexp match", `regexp:^.*\.example\.com$`, "www.example.com", RelationRegexp, `regexp:^.*\.example\.com$`},
		{"regexp no match", `regexp:^.*\.example\.com$`, "example.com", RelationNone, ""},
		{"invalid regexp", "regexp:[", "example.com", RelationNone, ""},
		{"empty entry", "  ", "example.com", RelationNone, ""},
		{"empty domain", "example.com", "", RelationNone, ""},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			relation, matched := MatchDomainEntry(tc.entry, tc.domain)
			if relation != tc.relation {
				t.Errorf("relation = %q, want %q", relation, tc.relation)
			}
			if matched != tc.matched {
				t.Errorf("matched entry = %q, want %q", matched, tc.matched)
			}
		})
	}
}

func TestMatchDomainEntryAgreesWithMatchSNI(t *testing.T) {
	entries := []string{"example.com", "cdn.example.org", `regexp:^video-[0-9]+\.example\.net$`}

	set := &config.SetConfig{
		Id:      "s1",
		Name:    "test",
		Enabled: true,
	}
	set.Targets.DomainsToMatch = entries
	suffixSet := NewSuffixSet([]*config.SetConfig{set})

	domains := []string{
		"example.com",
		"www.example.com",
		"a.b.example.com",
		"notexample.com",
		"example.org",
		"cdn.example.org",
		"static.cdn.example.org",
		"video-12.example.net",
		"video-x.example.net",
		"example.com.",
	}

	for _, domain := range domains {
		var routed bool
		for _, entry := range entries {
			switch relation, _ := MatchDomainEntry(entry, domain); relation {
			case RelationExact, RelationCovered, RelationRegexp:
				routed = true
			}
		}

		matched, _ := suffixSet.MatchSNI(domain)
		if matched != routed {
			t.Errorf("%q: MatchSNI = %v, MatchDomainEntry says routed = %v", domain, matched, routed)
		}
	}
}

func TestCanonicalDomainEntry(t *testing.T) {
	tests := []struct {
		entry string
		want  string
	}{
		{" Example.COM. ", "example.com"},
		{"regexp:^A$", "regexp:^a$"},
		{"   ", ""},
		{"regexp:", ""},
	}

	for _, tc := range tests {
		if got := CanonicalDomainEntry(tc.entry); got != tc.want {
			t.Errorf("CanonicalDomainEntry(%q) = %q, want %q", tc.entry, got, tc.want)
		}
	}
}
