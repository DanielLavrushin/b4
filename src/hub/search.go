package hub

import (
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
	"golang.org/x/net/publicsuffix"
)

const (
	MatchViaDomain   = "domain"
	MatchViaCategory = "category"

	DefaultSearchLimit = 50
)

type Match struct {
	Entry    string `json:"entry"`
	Relation string `json:"relation"`
	Via      string `json:"via"`
}

type Result struct {
	Set     *hubwire.CatalogueSet
	Match   *Match
	Display hubwire.Displayed
}

func setActive(cs *hubwire.CatalogueSet) bool {
	return cs.Status == "" || cs.Status == hubwire.SetStatusActive
}

func stringList(v interface{}) []string {
	items, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func TargetList(projection map[string]interface{}, key string) []string {
	targets, ok := projection["targets"].(map[string]interface{})
	if !ok {
		return nil
	}
	return stringList(targets[key])
}

func RegistrableBaseName(domain string) string {
	domain = strings.TrimPrefix(sni.NormalizeDomain(domain), "www.")
	registered, err := publicsuffix.EffectiveTLDPlusOne(domain)
	if err != nil {
		registered = domain
	}
	parts := strings.Split(registered, ".")
	if len(parts) == 0 {
		return ""
	}
	return strings.ToLower(parts[0])
}

func matchSet(cs *hubwire.CatalogueSet, domain, baseName string) *Match {
	var best *Match
	bestRank := 0
	for _, entry := range TargetList(cs.Set, "sni_domains") {
		rel, matched := sni.MatchDomainEntry(entry, domain)
		switch rel {
		case sni.RelationExact, sni.RelationCovered, sni.RelationRegexp:
		default:
			continue
		}
		if rank := rel.Priority(); rank > bestRank {
			bestRank = rank
			best = &Match{Entry: matched, Relation: string(rel), Via: MatchViaDomain}
		}
	}
	if best != nil {
		return best
	}
	if baseName == "" {
		return nil
	}
	for _, category := range TargetList(cs.Set, "geosite_categories") {
		if strings.EqualFold(strings.TrimSpace(category), baseName) {
			return &Match{Entry: category, Relation: string(sni.RelationCovered), Via: MatchViaCategory}
		}
	}
	return nil
}

func matchRank(m *Match) int {
	if m == nil {
		return 0
	}
	if m.Via == MatchViaCategory {
		return 1
	}
	return 1 + sni.DomainRelation(m.Relation).Priority()
}

func bucketRank(bucket string) int {
	switch bucket {
	case hubwire.BucketASN:
		return 3
	case hubwire.BucketCountry:
		return 2
	case hubwire.BucketGlobal:
		return 1
	}
	return 0
}

func rankResults(results []Result) {
	sort.SliceStable(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if ra, rb := matchRank(a.Match), matchRank(b.Match); ra != rb {
			return ra > rb
		}
		if ba, bb := bucketRank(a.Display.Bucket), bucketRank(b.Display.Bucket); ba != bb {
			return ba > bb
		}
		if a.Display.Score != b.Display.Score {
			return a.Display.Score > b.Display.Score
		}
		if a.Display.N != b.Display.N {
			return a.Display.N > b.Display.N
		}
		if a.Set.UpdatedAt != b.Set.UpdatedAt {
			return a.Set.UpdatedAt > b.Set.UpdatedAt
		}
		return a.Set.ID < b.Set.ID
	})
}

func (s *Service) Search(domain string, limit int) ([]Result, int) {
	domain = sni.NormalizeDomain(domain)
	baseName := ""
	if domain != "" {
		baseName = RegistrableBaseName(domain)
	}
	net := s.Network()

	s.mu.RLock()
	cat := s.catalogue
	s.mu.RUnlock()
	if cat == nil {
		return []Result{}, 0
	}

	results := make([]Result, 0, len(cat.Sets))
	for i := range cat.Sets {
		cs := &cat.Sets[i]
		if !setActive(cs) {
			continue
		}
		var m *Match
		if domain != "" {
			m = matchSet(cs, domain, baseName)
			if m == nil {
				continue
			}
		}
		results = append(results, Result{Set: cs, Match: m, Display: hubwire.PickScore(cs.Scores, net.ASN, net.CC)})
	}
	rankResults(results)
	total := len(results)
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if len(results) > limit {
		results = results[:limit]
	}
	return results, total
}

func (s *Service) Get(id string) (Result, bool) {
	s.mu.RLock()
	cs, ok := s.byID[id]
	s.mu.RUnlock()
	if !ok {
		return Result{}, false
	}
	net := s.Network()
	return Result{Set: cs, Display: hubwire.PickScore(cs.Scores, net.ASN, net.CC)}, true
}
