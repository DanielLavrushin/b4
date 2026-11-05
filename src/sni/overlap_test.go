package sni

import (
	"reflect"
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func overlapSet(name string, enabled bool, domains ...string) *config.SetConfig {
	set := &config.SetConfig{Id: name, Name: name, Enabled: enabled}
	set.Targets.DomainsToMatch = domains
	return set
}

func withGeo(set *config.SetConfig, geosite, geoip []string) *config.SetConfig {
	set.Targets.GeoSiteCategories = geosite
	set.Targets.GeoIpCategories = geoip
	return set
}

func ex(domain, first, second string, relation DomainRelation, winner, reason string) OverlapExample {
	return OverlapExample{
		Domain:     domain,
		Entries:    [2]OverlapEntry{{Entry: first}, {Entry: second}},
		Relation:   relation,
		WinnerId:   winner,
		WinnerName: winner,
		Reason:     reason,
	}
}

func TestFindSetOverlaps(t *testing.T) {
	tests := []struct {
		name     string
		sets     []*config.SetConfig
		expected []SetOverlap
	}{
		{
			name: "same domain in two enabled sets",
			sets: []*config.SetConfig{
				overlapSet("first", true, "zona.media", "meduza.io"),
				overlapSet("second", true, "meduza.io", "zona.media"),
			},
			expected: []SetOverlap{{
				SetIds: [2]string{"first", "second"}, SetNames: [2]string{"first", "second"}, Domains: 2,
				Examples: []OverlapExample{
					ex("zona.media", "zona.media", "zona.media", RelationExact, "first", OverlapReasonOrder),
					ex("meduza.io", "meduza.io", "meduza.io", RelationExact, "first", OverlapReasonOrder),
				},
			}},
		},
		{
			name: "disabled set does not overlap",
			sets: []*config.SetConfig{
				overlapSet("first", true, "meduza.io"),
				overlapSet("second", false, "meduza.io"),
			},
			expected: []SetOverlap{},
		},
		{
			name: "different domains do not overlap",
			sets: []*config.SetConfig{
				overlapSet("first", true, "meduza.io"),
				overlapSet("second", true, "zona.media"),
			},
			expected: []SetOverlap{},
		},
		{
			name: "broader entry above covers a subdomain below, the more specific entry wins",
			sets: []*config.SetConfig{
				overlapSet("broad", true, "googleapis.com"),
				overlapSet("specific", true, "youtubei.googleapis.com", "youtube.com"),
			},
			expected: []SetOverlap{{
				SetIds: [2]string{"broad", "specific"}, SetNames: [2]string{"broad", "specific"}, Domains: 1,
				Examples: []OverlapExample{
					ex("youtubei.googleapis.com", "googleapis.com", "youtubei.googleapis.com", RelationCovered, "specific", OverlapReasonSpecific),
				},
			}},
		},
		{
			name: "a shared parent and child count once, not again as covered",
			sets: []*config.SetConfig{
				overlapSet("first", true, "icloud.com", "www.icloud.com"),
				overlapSet("second", true, "icloud.com", "www.icloud.com"),
			},
			expected: []SetOverlap{{
				SetIds: [2]string{"first", "second"}, SetNames: [2]string{"first", "second"}, Domains: 2,
				Examples: []OverlapExample{
					ex("icloud.com", "icloud.com", "icloud.com", RelationExact, "first", OverlapReasonOrder),
					ex("www.icloud.com", "www.icloud.com", "www.icloud.com", RelationExact, "first", OverlapReasonOrder),
				},
			}},
		},
		{
			name: "entries are canonicalized before comparing",
			sets: []*config.SetConfig{
				overlapSet("first", true, "Meduza.IO."),
				overlapSet("second", true, "meduza.io"),
			},
			expected: []SetOverlap{{
				SetIds: [2]string{"first", "second"}, SetNames: [2]string{"first", "second"}, Domains: 1,
				Examples: []OverlapExample{
					ex("meduza.io", "meduza.io", "meduza.io", RelationExact, "first", OverlapReasonOrder),
				},
			}},
		},
		{
			name: "duplicate entry inside one set is not an overlap",
			sets: []*config.SetConfig{
				overlapSet("first", true, "meduza.io", "meduza.io"),
			},
			expected: []SetOverlap{},
		},
		{
			name: "regexp entries are not compared",
			sets: []*config.SetConfig{
				overlapSet("first", true, "regexp:.*\\.meduza\\.io"),
				overlapSet("second", true, "regexp:.*\\.meduza\\.io"),
			},
			expected: []SetOverlap{},
		},
		{
			name: "shared geosite and geoip tags are reported per pair",
			sets: []*config.SetConfig{
				withGeo(overlapSet("APPLE", true, "apple.com"), []string{"Apple"}, []string{"apple"}),
				withGeo(overlapSet("DPI", true, "youtube.com"), []string{"youtube"}, nil),
				withGeo(overlapSet("DPI_SNI", true, "youtube.com", "apple.com"), []string{"youtube", "apple"}, []string{"meta", "apple"}),
			},
			expected: []SetOverlap{
				{
					SetIds: [2]string{"APPLE", "DPI_SNI"}, SetNames: [2]string{"APPLE", "DPI_SNI"},
					SharedGeosite: []string{"apple"}, SharedGeoip: []string{"apple"}, Domains: 1,
					Examples: []OverlapExample{
						ex("apple.com", "apple.com", "apple.com", RelationExact, "APPLE", OverlapReasonOrder),
					},
				},
				{
					SetIds: [2]string{"DPI", "DPI_SNI"}, SetNames: [2]string{"DPI", "DPI_SNI"},
					SharedGeosite: []string{"youtube"}, Domains: 1,
					Examples: []OverlapExample{
						ex("youtube.com", "youtube.com", "youtube.com", RelationExact, "DPI", OverlapReasonOrder),
					},
				},
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := FindSetOverlaps(tc.sets, NewSuffixSet(tc.sets), nil)
			if !reflect.DeepEqual(got, tc.expected) {
				t.Fatalf("got  %+v\nwant %+v", got, tc.expected)
			}
		})
	}
}

func TestFindSetOverlapsExampleCap(t *testing.T) {
	domains := []string{"a.io", "b.io", "c.io", "d.io", "e.io", "f.io", "g.io"}
	sets := []*config.SetConfig{overlapSet("first", true, domains...), overlapSet("second", true, domains...)}
	got := FindSetOverlaps(sets, NewSuffixSet(sets), nil)
	if len(got) != 1 || got[0].Domains != len(domains) || len(got[0].Examples) != maxOverlapExamples {
		t.Fatalf("want one pair with %d domains and %d examples, got %+v", len(domains), maxOverlapExamples, got)
	}
}

func TestFindSetOverlapsNamesSourcesAndWinReasons(t *testing.T) {
	rkn := overlapSet("RKN_BLOCK", true, "zendesk.com")
	rkn.Targets.SNIDomains = []string{"zendesk.com"}
	dpi := withGeo(overlapSet("DPI_SNI", true, "hammerandchisel.ssl.zendesk.com", "x.ai"), []string{"discord", "x"}, nil)
	ai := withGeo(overlapSet("AI", true, "x.ai"), []string{"xai"}, nil)
	sets := []*config.SetConfig{rkn, ai, dpi}
	categories := map[string]map[string]string{
		"DPI_SNI": {"hammerandchisel.ssl.zendesk.com": "discord", "x.ai": "x"},
		"AI":      {"x.ai": "xai"},
	}
	source := func(set *config.SetConfig, entry string) string {
		if slices.Contains(set.Targets.SNIDomains, entry) {
			return "manual"
		}
		return "geosite:" + categories[set.Name][entry]
	}

	got := FindSetOverlaps(sets, NewSuffixSet(sets), source)
	if len(got) != 2 {
		t.Fatalf("want two pairs, got %+v", got)
	}
	zendesk := got[0].Examples[0]
	want := OverlapExample{
		Domain:     "hammerandchisel.ssl.zendesk.com",
		Entries:    [2]OverlapEntry{{Entry: "zendesk.com", Source: "manual"}, {Entry: "hammerandchisel.ssl.zendesk.com", Source: "geosite:discord"}},
		Relation:   RelationCovered,
		WinnerId:   "DPI_SNI",
		WinnerName: "DPI_SNI",
		Reason:     OverlapReasonSpecific,
	}
	if !reflect.DeepEqual(zendesk, want) {
		t.Fatalf("zendesk example\ngot  %+v\nwant %+v", zendesk, want)
	}
	xai := got[1].Examples[0]
	if xai.Entries[0].Source != "geosite:xai" || xai.Entries[1].Source != "geosite:x" ||
		xai.WinnerId != "AI" || xai.Reason != OverlapReasonOrder {
		t.Fatalf("the same entry from two categories goes to the higher set, got %+v", xai)
	}
}
