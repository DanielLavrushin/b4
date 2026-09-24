package watchdog

import (
	"errors"
	"fmt"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

var (
	ErrBaselineWorks = errors.New("the domain loads without a bypass")
	errUnconfirmed   = errors.New("the strategy discovery found did not pass confirmation")
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
		if dr.BaselineWorks {
			results[input] = ErrBaselineWorks
			continue
		}
		if dr.Unconfirmed {
			results[input] = errUnconfirmed
			continue
		}
		set := winnerSetFor(suite, domainKey, dr)
		if set == nil {
			results[input] = fmt.Errorf("best preset has no set config")
			continue
		}
		successful = append(successful, domainWithSet{domain: input, set: set})
	}

	if len(successful) == 0 {
		return results
	}

	for _, group := range groupBySet(successful) {
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

func winnerSetFor(suite *discovery.CheckSuite, domainKey string, dr *discovery.DomainDiscoveryResult) *config.SetConfig {
	for _, group := range suite.StrategyGroups {
		if group.Set == nil {
			continue
		}
		for _, d := range group.Domains {
			if d == domainKey {
				return group.Set
			}
		}
	}
	if best, ok := dr.Results[dr.BestPreset]; ok && best.Set != nil {
		return best.Set
	}
	return nil
}

func groupBySet(items []domainWithSet) [][]domainWithSet {
	var groups [][]domainWithSet
	index := make(map[*config.SetConfig]int)
	for _, item := range items {
		if i, ok := index[item.set]; ok {
			groups[i] = append(groups[i], item)
			continue
		}
		index[item.set] = len(groups)
		groups = append(groups, []domainWithSet{item})
	}
	return groups
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
		changes := describeSetChanges(existingSet, refSet)

		existingSet.AdoptStrategy(refSet)

		for _, domain := range groupDomains {
			if !domainInSNIList(existingSet.Targets.SNIDomains, domain) {
				existingSet.Targets.SNIDomains = append(existingSet.Targets.SNIDomains, domain)
				existingSet.Targets.DomainsToMatch = append(existingSet.Targets.DomainsToMatch, domain)
			}
		}

		if len(changes) == 0 {
			log.Infof("[WATCHDOG] %s: set %q already matched the discovered strategy, left unchanged",
				strings.Join(groupDomains, ", "), existingSet.Name)
		} else {
			log.Infof("[WATCHDOG] %s: adopted the discovered strategy into set %q (%s)",
				strings.Join(groupDomains, ", "), existingSet.Name, strings.Join(changes, ", "))
		}
	} else {
		newSet := config.NewSetConfig()
		newSet.Id = uuid.New().String()
		newSet.Name = "watchdog-" + groupDomains[0]
		newSet.Enabled = true
		newSet.Targets.SNIDomains = groupDomains
		newSet.Targets.DomainsToMatch = groupDomains
		newSet.AdoptStrategy(refSet)
		cfg.Sets = append([]*config.SetConfig{&newSet}, cfg.Sets...)
		log.Infof("[WATCHDOG] %s: created set %q (strategy: %s)",
			strings.Join(groupDomains, ", "), newSet.Name, refSet.Fragmentation.Strategy)
	}
}

// describeSetChanges lists the fields the discovered strategy will overwrite, so
// the log says what a heal actually did to a hand-tuned set rather than just
// naming the fragmentation strategy.
func describeSetChanges(old, next *config.SetConfig) []string {
	var changes []string

	add := func(field string, from, to any) {
		if fmt.Sprint(from) == fmt.Sprint(to) {
			return
		}
		changes = append(changes, fmt.Sprintf("%s %v -> %v", field, from, to))
	}

	add("fragmentation.strategy", old.Fragmentation.Strategy, next.Fragmentation.Strategy)
	add("fragmentation.sni_position", old.Fragmentation.SNIPosition, next.Fragmentation.SNIPosition)
	add("fragmentation.tlsrec_pos", old.Fragmentation.TLSRecordPosition, next.Fragmentation.TLSRecordPosition)
	add("fragmentation.combo.shuffle_mode", old.Fragmentation.Combo.ShuffleMode, next.Fragmentation.Combo.ShuffleMode)
	add("fragmentation.combo.first_delay_ms", old.Fragmentation.Combo.FirstDelayMs, next.Fragmentation.Combo.FirstDelayMs)
	add("faking.strategy", old.Faking.Strategy, next.Faking.Strategy)
	add("faking.ttl", old.Faking.TTL, next.Faking.TTL)
	add("faking.sni_type", old.Faking.SNIType, next.Faking.SNIType)
	add("faking.sni_seq_length", old.Faking.SNISeqLength, next.Faking.SNISeqLength)
	add("faking.tcp_md5", old.Faking.TCPMD5, next.Faking.TCPMD5)
	add("faking.tls_mod", strings.Join(old.Faking.TLSMod, "+"), strings.Join(next.Faking.TLSMod, "+"))
	add("tcp.seg2delay", old.TCP.Seg2Delay, next.TCP.Seg2Delay)
	add("tcp.conn_bytes_limit", old.TCP.ConnBytesLimit, next.TCP.ConnBytesLimit)
	add("tcp.desync.mode", old.TCP.Desync.Mode, next.TCP.Desync.Mode)

	return changes
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
