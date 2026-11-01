package discovery

import (
	"time"

	"github.com/daniellavrushin/b4/config"
)

var retainTimers = map[string]*time.Timer{}

func retainFinished(suite *CheckSuite) {
	if suite == nil {
		return
	}
	suitesMu.Lock()
	defer suitesMu.Unlock()

	for id, timer := range retainTimers {
		if id == suite.Id {
			continue
		}
		if other, ok := activeSuites[id]; ok && other.Source != suite.Source {
			continue
		}
		timer.Stop()
		delete(retainTimers, id)
		delete(activeSuites, id)
	}
	if timer := retainTimers[suite.Id]; timer != nil {
		timer.Stop()
	}
	id := suite.Id
	retainTimers[id] = time.AfterFunc(suiteRetention, func() { releaseSuite(id) })
}

func releaseSuite(id string) {
	suitesMu.Lock()
	defer suitesMu.Unlock()
	delete(retainTimers, id)
	delete(activeSuites, id)
}

func (ds *DiscoverySuite) releaseMatchLists() {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	lean := map[*config.SetConfig]*config.SetConfig{}
	for _, dr := range ds.domainResults {
		if dr == nil {
			continue
		}
		for _, r := range dr.Results {
			if r == nil || r.Set == nil || len(r.Set.Targets.DomainsToMatch)+len(r.Set.Targets.IpsToMatch) == 0 {
				continue
			}
			copied, ok := lean[r.Set]
			if !ok {
				set := *r.Set
				set.Targets.DomainsToMatch = nil
				set.Targets.IpsToMatch = nil
				copied = &set
				lean[r.Set] = copied
			}
			r.Set = copied
		}
	}
}
