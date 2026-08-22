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
}

func (ds *DiscoverySuite) winnersToConfirm() map[string][]string {
	ds.CheckSuite.mu.RLock()
	defer ds.CheckSuite.mu.RUnlock()

	pending := map[string][]string{}
	for domain, dr := range ds.domainResults {
		if dr == nil || !dr.BestSuccess || dr.BaselineWorks || dr.BestPreset == "" {
			continue
		}
		if dr.ConfirmTries > 0 {
			continue
		}
		pending[dr.BestPreset] = append(pending[dr.BestPreset], domain)
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
	set := *stored
	set.Enabled = true
	set.Targets.SNIDomains = append([]string(nil), domains...)
	set.Targets.DomainsToMatch = append([]string(nil), domains...)
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

	if err := ds.pool.UpdateConfig(ds.confirmConfig(stored, domains)); err != nil {
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
		ds.recordConfirmation(di.Domain, passes[di.Domain])
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

func (ds *DiscoverySuite) recordConfirmation(domain string, passes int) {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	if dr := ds.domainResults[domain]; dr != nil {
		dr.Confirmed = passes
		dr.ConfirmTries = confirmTries
	}
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
		}
		dr.Confirmed = 0
		dr.ConfirmTries = 0
	}
}
