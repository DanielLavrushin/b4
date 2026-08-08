package sni

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func overlapSet(name string, enabled bool, domains ...string) *config.SetConfig {
	set := &config.SetConfig{Name: name, Enabled: enabled}
	set.Targets.SNIDomains = domains
	return set
}

func TestFindDomainOverlaps(t *testing.T) {
	tests := []struct {
		name     string
		sets     []*config.SetConfig
		expected map[string][]string
	}{
		{
			name: "same domain in two enabled sets",
			sets: []*config.SetConfig{
				overlapSet("Fake SNI", true, "zona.media", "meduza.io"),
				overlapSet("multidisorder-combo", true, "meduza.io", "zona.media"),
			},
			expected: map[string][]string{
				"zona.media": {"Fake SNI", "multidisorder-combo"},
				"meduza.io":  {"Fake SNI", "multidisorder-combo"},
			},
		},
		{
			name: "disabled set does not overlap",
			sets: []*config.SetConfig{
				overlapSet("first", true, "meduza.io"),
				overlapSet("second", false, "meduza.io"),
			},
			expected: map[string][]string{},
		},
		{
			name: "different domains do not overlap",
			sets: []*config.SetConfig{
				overlapSet("first", true, "meduza.io"),
				overlapSet("second", true, "zona.media"),
			},
			expected: map[string][]string{},
		},
		{
			name: "parent and subdomain do not overlap",
			sets: []*config.SetConfig{
				overlapSet("first", true, "meduza.io"),
				overlapSet("second", true, "www.meduza.io"),
			},
			expected: map[string][]string{},
		},
		{
			name: "entries are canonicalized before comparing",
			sets: []*config.SetConfig{
				overlapSet("first", true, "Meduza.IO."),
				overlapSet("second", true, "meduza.io"),
			},
			expected: map[string][]string{
				"meduza.io": {"first", "second"},
			},
		},
		{
			name: "duplicate entry inside one set is not an overlap",
			sets: []*config.SetConfig{
				overlapSet("first", true, "meduza.io", "meduza.io"),
			},
			expected: map[string][]string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			overlaps := FindDomainOverlaps(tc.sets)
			if len(overlaps) != len(tc.expected) {
				t.Fatalf("got %d overlaps, want %d: %+v", len(overlaps), len(tc.expected), overlaps)
			}
			for _, o := range overlaps {
				want, ok := tc.expected[o.Entry]
				if !ok {
					t.Fatalf("unexpected overlap for %q", o.Entry)
				}
				if len(o.SetNames) != len(want) {
					t.Fatalf("%q: got sets %v, want %v", o.Entry, o.SetNames, want)
				}
				for i := range want {
					if o.SetNames[i] != want[i] {
						t.Fatalf("%q: got sets %v, want %v (config order)", o.Entry, o.SetNames, want)
					}
				}
			}
		})
	}
}
