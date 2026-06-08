package watchdog

import (
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

type domainWithSet struct {
	domain string
	set    *config.SetConfig
}

func applyBatchResults(cfg *config.Config, domains []string, suite *discovery.CheckSuite, saveFunc func(*config.Config) error) map[string]error {
	results := make(map[string]error)

	var successful []domainWithSet
	for _, input := range domains {
		domainKey := ExtractDomain(input)
		dr, ok := suite.DomainDiscoveryResults[domainKey]
		if !ok || !dr.BestSuccess {
			results[input] = fmt.Errorf("no working config found")
			continue
		}
		best, ok := dr.Results[dr.BestPreset]
		if !ok || best.Set == nil {
			results[input] = fmt.Errorf("best preset has no set config")
			continue
		}
		successful = append(successful, domainWithSet{domain: input, set: best.Set})
	}

	if len(successful) == 0 {
		return results
	}

	groups := groupByConfig(successful)

	for _, group := range groups {
		applyGroup(cfg, group)
	}

	if err := saveFunc(cfg); err != nil {
		for _, ds := range successful {
			results[ds.domain] = err
		}
		return results
	}

	return results
}

func groupByConfig(items []domainWithSet) [][]domainWithSet {
	var groups [][]domainWithSet
	used := make(map[int]bool)

	for i := 0; i < len(items); i++ {
		if used[i] {
			continue
		}
		group := []domainWithSet{items[i]}
		used[i] = true
		for j := i + 1; j < len(items); j++ {
			if used[j] {
				continue
			}
			if configsMatch(items[i].set, items[j].set) {
				group = append(group, items[j])
				used[j] = true
			}
		}
		groups = append(groups, group)
	}
	return groups
}

func configsMatch(a, b *config.SetConfig) bool {
	return a.Fragmentation.Strategy == b.Fragmentation.Strategy &&
		a.Faking.Strategy == b.Faking.Strategy &&
		a.Faking.TTL == b.Faking.TTL &&
		a.TCP.DropSACK == b.TCP.DropSACK
}

func applyGroup(cfg *config.Config, group []domainWithSet) {
	groupDomains := make([]string, len(group))
	for i, ds := range group {
		groupDomains[i] = ExtractDomain(ds.domain)
	}
	refSet := group[0].set

	var existingSet *config.SetConfig
	for _, set := range cfg.Sets {
		if !set.Enabled {
			continue
		}
		if set.Routing.Enabled {
			continue
		}
		if setListsAnyDomain(set, groupDomains) {
			existingSet = set
			break
		}
	}

	if existingSet != nil {
		oldStrategy := existingSet.Fragmentation.Strategy
		existingSet.TCP = refSet.TCP
		existingSet.UDP = refSet.UDP
		existingSet.Fragmentation = refSet.Fragmentation
		existingSet.Faking = refSet.Faking

		for _, domain := range groupDomains {
			if !domainInSNIList(existingSet.Targets.SNIDomains, domain) {
				existingSet.Targets.SNIDomains = append(existingSet.Targets.SNIDomains, domain)
				existingSet.Targets.DomainsToMatch = append(existingSet.Targets.DomainsToMatch, domain)
			}
		}

		log.Infof("[WATCHDOG] %s: applied to set %q (strategy: %s -> %s)",
			strings.Join(groupDomains, ", "), existingSet.Name, oldStrategy, refSet.Fragmentation.Strategy)
	} else {
		newSet := config.NewSetConfig()
		newSet.Id = uuid.New().String()
		newSet.Name = "watchdog-" + groupDomains[0]
		newSet.Enabled = true
		newSet.Targets.SNIDomains = groupDomains
		newSet.Targets.DomainsToMatch = groupDomains
		newSet.TCP = refSet.TCP
		newSet.UDP = refSet.UDP
		newSet.Fragmentation = refSet.Fragmentation
		newSet.Faking = refSet.Faking
		cfg.Sets = append([]*config.SetConfig{&newSet}, cfg.Sets...)
		log.Infof("[WATCHDOG] %s: created set %q (strategy: %s)",
			strings.Join(groupDomains, ", "), newSet.Name, refSet.Fragmentation.Strategy)
	}
}

func normalizeDomain(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func setListsAnyDomain(set *config.SetConfig, domains []string) bool {
	for _, rawSNI := range set.Targets.SNIDomains {
		sni := normalizeDomain(rawSNI)
		if sni == "" {
			continue
		}
		for _, rawDomain := range domains {
			domain := normalizeDomain(rawDomain)
			if domain == "" {
				continue
			}
			if sni == domain || (len(domain) > len(sni) && strings.HasSuffix(domain, "."+sni)) {
				return true
			}
		}
	}
	return false
}

func setContainsAnyDomain(set *config.SetConfig, domains []string) bool {
	matchList := set.Targets.DomainsToMatch
	if len(matchList) == 0 {
		matchList = set.Targets.SNIDomains
	}
	for _, rawTarget := range matchList {
		target := normalizeDomain(rawTarget)
		if target == "" {
			continue
		}
		for _, rawDomain := range domains {
			domain := normalizeDomain(rawDomain)
			if domain == "" {
				continue
			}
			if target == domain || domainMatchesSuffix(domain, target) {
				return true
			}
		}
	}
	return false
}

func domainMatchesSuffix(domain, target string) bool {
	if len(domain) > len(target) && strings.HasSuffix(domain, "."+target) {
		return true
	}
	if len(target) > len(domain) && strings.HasSuffix(target, "."+domain) {
		return true
	}
	return false
}

func domainInSNIList(sniList []string, domain string) bool {
	target := normalizeDomain(domain)
	for _, sni := range sniList {
		if normalizeDomain(sni) == target {
			return true
		}
	}
	return false
}
