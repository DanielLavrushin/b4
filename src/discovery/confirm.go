package discovery

import (
	"fmt"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const confirmRounds = 3

func (ds *DiscoverySuite) confirmWinners() {
	if ds.pool == nil || ds.cfg == nil {
		return
	}

	for round := 0; round < confirmRounds; round++ {
		if ds.canceled() {
			return
		}

		ds.buildStrategyGroups()

		pending := ds.winnersToConfirm()
		if len(pending) == 0 {
			return
		}

		if round == 0 {
			ds.setPhase(PhaseConfirm)
			log.DiscoveryLogf("Confirming winners as they will be deployed (%d tries each)", confirmTries)
		}

		demoted := false
		for presetName, domains := range pending {
			if ds.canceled() {
				return
			}
			set := ds.storedSetFor(presetName, domains)
			if set == nil {
				ds.demoteWinner(presetName, domains, "the tested configuration was not recorded")
				demoted = true
				continue
			}
			results, completed := ds.confirmPreset(presetName, set, domains)
			if !completed {
				return
			}
			for domain, passes := range results {
				if passes == confirmTries {
					continue
				}
				ds.demoteWinner(presetName, []string{domain},
					fmt.Sprintf("only %d of %d confirmation tries passed", passes, confirmTries))
				demoted = true
			}
		}

		if !demoted {
			return
		}
		ds.determineBest()
	}

	ds.buildStrategyGroups()
	if left := ds.winnersToConfirm(); len(left) > 0 {
		log.Warnf("Discovery gave up confirming after %d rounds, these are reported without a confirmation: %v", confirmRounds, left)
	}
}

func (ds *DiscoverySuite) winnersToConfirm() map[string][]string {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()

	pending := map[string][]string{}
	add := func(preset, domain string) {
		dr := ds.domainResults[domain]
		if dr == nil || !dr.BestSuccess || dr.BaselineWorks || preset == "" || preset == presetNoBypass {
			return
		}
		r := dr.Results[preset]
		if r == nil || r.Status != CheckStatusComplete || r.ConfirmTries > 0 {
			return
		}
		for _, existing := range pending[preset] {
			if existing == domain {
				return
			}
		}
		pending[preset] = append(pending[preset], domain)
	}

	for domain, dr := range ds.domainResults {
		if dr != nil {
			add(dr.BestPreset, domain)
		}
	}
	for _, group := range ds.StrategyGroups {
		for _, domain := range group.Domains {
			add(group.WinnerPreset, domain)
		}
	}
	return pending
}

func (ds *DiscoverySuite) storedSetFor(presetName string, domains []string) *config.SetConfig {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()

	for _, domain := range domains {
		dr := ds.domainResults[domain]
		if dr == nil {
			continue
		}
		if r := dr.Results[presetName]; r != nil && r.Set != nil {
			return r.Set
		}
	}
	return nil
}

func (ds *DiscoverySuite) confirmConfig(stored *config.SetConfig, domains []string) *config.Config {
	scoped := ds.scopeSetToDomains(stored, domains)
	if scoped == nil {
		return nil
	}

	set := *scoped
	set.Enabled = true
	set.Targets.IPs = nil
	set.Targets.IpsToMatch = nil

	if len(set.Targets.GeoIpCategories) > 0 || len(set.Targets.GeoSiteCategories) > 0 {
		tempCfg := &config.Config{System: ds.cfg.System}
		if _, _, err := tempCfg.GetTargetsForSet(&set); err != nil {
			log.DiscoveryLogf("Confirmation: failed to load CDN categories: %v", err)
		}
	}

	return &config.Config{
		ConfigPath: ds.cfg.ConfigPath,
		Queue:      ds.cfg.Queue,
		System:     ds.cfg.System,
		Sets:       []*config.SetConfig{&set},
	}
}

func (ds *DiscoverySuite) confirmPreset(presetName string, stored *config.SetConfig, domains []string) (map[string]int, bool) {
	passes := map[string]int{}
	for _, domain := range domains {
		passes[domain] = 0
	}

	confirmCfg := ds.confirmConfig(stored, domains)
	if confirmCfg == nil {
		log.DiscoveryLogf("Confirmation: could not rebuild '%s' as it would be deployed", presetName)
		return passes, true
	}
	if err := ds.pool.UpdateConfig(confirmCfg); err != nil {
		log.DiscoveryLogf("Confirmation: could not apply '%s': %v", presetName, err)
		return passes, true
	}
	time.Sleep(time.Duration(ds.cfg.System.Checker.ConfigPropagateMs) * time.Millisecond)

	timeout := time.Duration(ds.cfg.System.Checker.DiscoveryTimeoutSec) * time.Second
	inputs := ds.inputsFor(domains)

	var mu sync.Mutex
	var wg sync.WaitGroup

	for try := 0; try < confirmTries; try++ {
		if ds.canceled() {
			return passes, false
		}
		if try > 0 {
			time.Sleep(confirmDelay)
		}
		for _, di := range inputs {
			wg.Add(1)
			go func(di DomainInput) {
				defer wg.Done()
				if ds.fetchForDomain(di, timeout).Status != CheckStatusComplete {
					return
				}
				mu.Lock()
				passes[di.Domain]++
				mu.Unlock()
			}(di)
		}
		wg.Wait()
	}

	for _, di := range inputs {
		ds.recordConfirmation(di.Domain, presetName, passes[di.Domain])
		if passes[di.Domain] == confirmTries {
			log.DiscoveryLogf("  [%s] '%s' confirmed %d/%d", di.Domain, presetName, passes[di.Domain], confirmTries)
		} else {
			log.DiscoveryLogf("  [%s] '%s' only passed %d/%d, dropping it", di.Domain, presetName, passes[di.Domain], confirmTries)
		}
	}
	return passes, true
}

func (ds *DiscoverySuite) inputsFor(domains []string) []DomainInput {
	wanted := map[string]bool{}
	for _, d := range domains {
		wanted[d] = true
	}
	var inputs []DomainInput
	for _, di := range ds.Domains {
		if wanted[di.Domain] {
			inputs = append(inputs, di)
		}
	}
	return inputs
}

func (ds *DiscoverySuite) recordConfirmation(domain, presetName string, passes int) {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	dr := ds.domainResults[domain]
	if dr == nil {
		return
	}
	if r := dr.Results[presetName]; r != nil {
		r.Confirmed = passes
		r.ConfirmTries = confirmTries
	}
	if dr.BestPreset == presetName {
		dr.Confirmed = passes
		dr.ConfirmTries = confirmTries
	}
	ds.refreshOutcomes(false)
}

func (ds *DiscoverySuite) demoteWinner(presetName string, domains []string, reason string) {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	for _, domain := range domains {
		dr := ds.domainResults[domain]
		if dr == nil {
			continue
		}
		if r := dr.Results[presetName]; r != nil {
			r.Status = CheckStatusFailed
			r.Speed = 0
			r.Error = fmt.Sprintf("not reproducible: %s", reason)
			r.Confirmed = 0
			r.ConfirmTries = 0
			r.Set = nil
		}
		if dr.BestPreset == presetName {
			dr.Confirmed = 0
			dr.ConfirmTries = 0
		}
	}
	ds.refreshOutcomes(false)
}
