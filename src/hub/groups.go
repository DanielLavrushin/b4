package hub

import (
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
)

func listedGroups(cat *hubwire.Catalogue, byID map[string]*hubwire.CatalogueSet) []*hubwire.CatalogueGroup {
	out := make([]*hubwire.CatalogueGroup, 0, len(cat.Groups))
	for i := range cat.Groups {
		g := &cat.Groups[i]
		if g.ID != "" && groupResolves(g, byID) {
			out = append(out, g)
		}
	}
	return out
}

func groupResolves(g *hubwire.CatalogueGroup, byID map[string]*hubwire.CatalogueSet) bool {
	if len(g.Members) == 0 || len(g.Members) > hubwire.MaxGroupMembers {
		return false
	}
	seen := make(map[string]bool, len(g.Members))
	for i, m := range g.Members {
		cs, ok := byID[m.SetID]
		if !ok || !setListed(cs) || seen[m.SetID] || !hubwire.ValidGroupEscalation(m.Escalate, i, len(g.Members)) {
			return false
		}
		if field, _ := hubwire.EscalationOutOfRange(m.Escalate); field != "" {
			return false
		}
		seen[m.SetID] = true
	}
	return true
}

func (s *Service) Groups(domain string) []*hubwire.CatalogueGroup {
	domain = sni.NormalizeDomain(domain)
	s.mu.RLock()
	groups, byID := s.groups, s.byID
	s.mu.RUnlock()
	if domain == "" {
		return append([]*hubwire.CatalogueGroup(nil), groups...)
	}
	baseName := RegistrableBaseName(domain)
	resolve := s.categoryMatcher()
	var out []*hubwire.CatalogueGroup
	for _, g := range groups {
		for _, m := range g.Members {
			if matchSet(byID[m.SetID], domain, baseName, resolve) != nil {
				out = append(out, g)
				break
			}
		}
	}
	return out
}

func (s *Service) Group(id string) (*hubwire.CatalogueGroup, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	g, ok := s.groupByID[id]
	return g, ok
}

func (s *Service) GroupsOf(setID string) []*hubwire.CatalogueGroup {
	s.mu.RLock()
	groups := s.groups
	s.mu.RUnlock()
	var out []*hubwire.CatalogueGroup
	for _, g := range groups {
		for _, m := range g.Members {
			if m.SetID == setID {
				out = append(out, g)
				break
			}
		}
	}
	return out
}
