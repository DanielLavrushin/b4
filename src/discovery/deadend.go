package discovery

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func upgradeCheckURL(di DomainInput, r CheckResult) (string, bool) {
	u, err := url.Parse(di.CheckURL)
	if err != nil || u.Scheme != "http" || u.Host == "" {
		return di.CheckURL, false
	}
	if r.Status != CheckStatusFailed || r.StatusCode < 400 || r.StatusCode == http.StatusUnavailableForLegalReasons {
		return di.CheckURL, false
	}
	if strings.Contains(r.Error, "ISP block page") {
		return di.CheckURL, false
	}
	if r.BytesRead >= minSuccessBytes {
		return di.CheckURL, false
	}
	u.Scheme = "https"
	return u.String(), true
}

func deadEndAnswer(results map[string]*DomainPresetResult) (CheckResult, bool) {
	if len(results) < 2 {
		return CheckResult{}, false
	}
	var first *DomainPresetResult
	for _, r := range results {
		if r == nil || r.Status != CheckStatusFailed || r.StatusCode < 400 || r.BytesRead >= minSuccessBytes || strings.Contains(r.Error, "ISP block page") {
			return CheckResult{}, false
		}
		if first == nil {
			first = r
			continue
		}
		if r.StatusCode != first.StatusCode {
			return CheckResult{}, false
		}
	}
	return CheckResult{Status: first.Status, StatusCode: first.StatusCode, BytesRead: first.BytesRead, Error: first.Error}, true
}

func (ds *DiscoverySuite) rewriteDeadEndCheckURLs(checksPerDomain int) []string {
	var upgraded []string

	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()
	for i, di := range ds.Domains {
		dr := ds.domainResults[di.Domain]
		if dr == nil {
			continue
		}
		r, uniform := deadEndAnswer(dr.Results)
		if !uniform {
			continue
		}
		next, ok := upgradeCheckURL(di, r)
		if !ok {
			continue
		}
		log.DiscoveryLogf("  [%s] http://%s answers HTTP %d with %d bytes whatever the strategy, no strategy can pass with it; checking https://%s instead",
			di.Domain, strings.TrimPrefix(di.CheckURL, "http://"), r.StatusCode, r.BytesRead, strings.TrimPrefix(next, "https://"))
		ds.Domains[i].CheckURL = next
		if ds.Domain == di.Domain {
			ds.CheckURL = next
		}
		dr.Url = next
		dr.Results = make(map[string]*DomainPresetResult)
		upgraded = append(upgraded, di.Domain)
	}
	ds.TotalChecks += len(upgraded) * checksPerDomain
	return upgraded
}

func (ds *DiscoverySuite) upgradeDeadEndCheckURLs(presets, cached []ConfigPreset) []StrategyFamily {
	upgraded := ds.rewriteDeadEndCheckURLs(len(presets))
	if len(upgraded) == 0 {
		return nil
	}

	scoped := scopePresets(presets, upgraded)
	ds.storeResultsMulti(scoped[0], ds.testPresetAllDomains(scoped[0]))
	early := append(scopePresets(cached, upgraded), scopePresets(ds.hubPresets, upgraded)...)
	if current, ok := ds.setCurrentPreset(); ok {
		early = append(scopePresets([]ConfigPreset{current}, upgraded), early...)
	}
	ds.retestEarlyPresets(early)
	ds.determineBest()
	return ds.runPhase1Multi(scoped)
}

func scopePresets(presets []ConfigPreset, domains []string) []ConfigPreset {
	out := make([]ConfigPreset, 0, len(presets))
	for _, p := range presets {
		var covered []string
		for _, d := range domains {
			if p.covers(d) {
				covered = append(covered, d)
			}
		}
		if len(covered) == 0 {
			continue
		}
		p.Domains = covered
		out = append(out, p)
	}
	return out
}

func (ds *DiscoverySuite) retestEarlyPresets(early []ConfigPreset) {
	if len(early) == 0 {
		return
	}
	checks := 0
	for _, p := range early {
		checks += len(p.Domains)
	}
	ds.CheckSuite.mu.Lock()
	ds.TotalChecks += checks
	ds.CheckSuite.mu.Unlock()

	ds.setPhase(PhaseCached)
	log.DiscoveryLogf("Re-testing %d cached and community strategies on the upgraded https addresses", len(early))
	for _, preset := range early {
		if ds.interrupted() {
			break
		}
		if preset.Config.Faking.SNIType == config.FakePayloadRandom {
			ds.applyBestPayload(&preset.Config.Faking)
		}
		ds.storeResultsMulti(preset, ds.testPresetAllDomains(preset))
	}
	ds.setPhase(PhaseStrategy)
}
