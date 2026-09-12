package api

import (
	"net/http"
	"sort"
	"strconv"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	MatchViaDomain   = "domain"
	MatchViaCategory = "category"

	DefaultSearchLimit = 50
	MaxSearchLimit     = 500
)

type Match struct {
	Entry    string `json:"entry"`
	Relation string `json:"relation"`
	Via      string `json:"via"`
}

type Hit struct {
	hubwire.CatalogueSet
	Match *Match `json:"match"`
}

func (s *Server) matchSet(cs *hubwire.CatalogueSet, domain string) *Match {
	var best *Match
	bestRank := 0
	for _, entry := range store.TargetList(cs.Set, "sni_domains") {
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
	if s.Geo == nil {
		return nil
	}
	for _, category := range store.TargetList(cs.Set, "geosite_categories") {
		if cov, ok := s.Geo.Match(category, domain); ok {
			return &Match{Entry: cov.Category, Relation: string(cov.Relation), Via: MatchViaCategory}
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

func rankHits(hits []Hit) {
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		if ra, rb := matchRank(a.Match), matchRank(b.Match); ra != rb {
			return ra > rb
		}
		if a.Scores.Global.Score != b.Scores.Global.Score {
			return a.Scores.Global.Score > b.Scores.Global.Score
		}
		if a.Scores.Global.N != b.Scores.Global.N {
			return a.Scores.Global.N > b.Scores.Global.N
		}
		if a.UpdatedAt != b.UpdatedAt {
			return a.UpdatedAt > b.UpdatedAt
		}
		return a.ID < b.ID
	})
}

func (s *Server) Search(domain string, limit int) ([]Hit, int) {
	domain = sni.NormalizeDomain(domain)
	hits := make([]Hit, 0)
	latest := s.Catalogue.Latest()
	if latest == nil {
		return hits, 0
	}
	for i := range latest.Catalogue.Sets {
		cs := &latest.Catalogue.Sets[i]
		if cs.Status != "" && cs.Status != hubwire.SetStatusActive {
			continue
		}
		var m *Match
		if domain != "" {
			m = s.matchSet(cs, domain)
			if m == nil {
				continue
			}
		}
		hits = append(hits, Hit{CatalogueSet: *cs, Match: m})
	}
	rankHits(hits)
	total := len(hits)
	if limit <= 0 {
		limit = DefaultSearchLimit
	}
	if limit > MaxSearchLimit {
		limit = MaxSearchLimit
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	return hits, total
}

func (s *Server) search(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	limit, _ := strconv.Atoi(query.Get("limit"))
	hits, total := s.Search(query.Get("domain"), limit)
	w.Header().Set("Cache-Control", cacheNever)
	writeJSON(w, http.StatusOK, map[string]interface{}{"sets": hits, "total": total})
}
