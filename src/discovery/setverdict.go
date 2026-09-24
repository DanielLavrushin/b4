package discovery

import (
	"fmt"
	"sort"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	presetSetCurrent = "set-current"

	coverBatch       = 3
	maxCoverFailures = 3
)

type coverStopReason string

const (
	coverStopCovered  coverStopReason = "covered"
	coverStopBaseline coverStopReason = "baseline"
)

type jointOutcome int

const (
	jointPassed jointOutcome = iota
	jointFailed
	jointAborted
)

func (ds *DiscoverySuite) setMode() bool {
	return ds.SetId != ""
}

func (ds *DiscoverySuite) setCurrentPreset() (ConfigPreset, bool) {
	if !ds.setMode() || ds.setStrategy == nil {
		return ConfigPreset{}, false
	}
	set := config.NewSetConfig()
	set.TCP = ds.setStrategy.TCP
	set.UDP = ds.setStrategy.UDP
	set.Fragmentation = ds.setStrategy.Fragmentation
	set.Faking = ds.setStrategy.Faking
	ds.loadSetPayloads(&set)
	return ConfigPreset{
		Name:         presetSetCurrent,
		Description:  "The set's current strategy",
		Family:       FamilyCurrent,
		Phase:        PhaseCached,
		Priority:     0,
		Config:       set,
		FixedPayload: true,
	}, true
}

func (ds *DiscoverySuite) loadSetPayloads(set *config.SetConfig) {
	if ds.cfg == nil || ds.cfg.ConfigPath == "" {
		return
	}
	probe := *set
	probe.Enabled = true
	(&config.Config{ConfigPath: ds.cfg.ConfigPath, Sets: []*config.SetConfig{&probe}}).LoadCapturePayloads()
	if len(set.Faking.PayloadData) == 0 {
		set.Faking.PayloadData = probe.Faking.PayloadData
	}
	if len(set.UDP.FakePayloadData) == 0 {
		set.UDP.FakePayloadData = probe.UDP.FakePayloadData
	}
}

func unscopedPresets(presets []ConfigPreset) []ConfigPreset {
	out := make([]ConfigPreset, len(presets))
	for i, p := range presets {
		p.Domains = nil
		out[i] = p
	}
	return out
}

func (ds *DiscoverySuite) runDomains() []string {
	domains := make([]string, 0, len(ds.Domains))
	for _, di := range ds.Domains {
		domains = append(domains, di.Domain)
	}
	return domains
}

func coveringPresets(domains []string, results map[string]*DomainDiscoveryResult) []string {
	if len(domains) == 0 {
		return nil
	}
	first := results[domains[0]]
	if first == nil {
		return nil
	}

	var names []string
	for name := range first.Results {
		switch name {
		case presetNoBypass, presetAltAddress, presetDNSRedirect:
			continue
		}
		if completeEverywhere(name, domains, results) {
			names = append(names, name)
		}
	}

	sort.Slice(names, func(i, j int) bool {
		a, b := names[i], names[j]
		if (a == presetSetCurrent) != (b == presetSetCurrent) {
			return a == presetSetCurrent
		}
		ar, br := first.Results[a], first.Results[b]
		return presetRanksBefore(a, ar.Phase, ar.Priority, b, br.Phase, br.Priority)
	})
	return names
}

func completeEverywhere(name string, domains []string, results map[string]*DomainDiscoveryResult) bool {
	if len(domains) == 0 {
		return false
	}
	for _, domain := range domains {
		dr := results[domain]
		if dr == nil {
			return false
		}
		r := dr.Results[name]
		if r == nil || r.Status != CheckStatusComplete || r.Set == nil {
			return false
		}
	}
	return true
}

func everyDomainHasBaseline(domains []string, results map[string]*DomainDiscoveryResult) bool {
	for _, domain := range domains {
		dr := results[domain]
		if dr == nil || dr.Results[presetNoBypass] == nil {
			return false
		}
	}
	return true
}

func everyDomainLoadsPlain(domains []string, results map[string]*DomainDiscoveryResult) bool {
	if len(domains) == 0 {
		return false
	}
	for _, domain := range domains {
		dr := results[domain]
		if dr == nil {
			return false
		}
		r := dr.Results[presetNoBypass]
		if r == nil || r.Status != CheckStatusComplete {
			return false
		}
		if name, _ := plainFixResult(dr); name != "" {
			return false
		}
	}
	return true
}

func (ds *DiscoverySuite) closeFinishLocked() bool {
	if ds.finish == nil || (ds.Status != CheckStatusPending && ds.Status != CheckStatusRunning) {
		return false
	}
	select {
	case <-ds.finish:
		return false
	default:
		close(ds.finish)
		return true
	}
}

func (ds *DiscoverySuite) checkCoverage() {
	if !ds.setMode() || !ds.stopWhenCovered || ds.interrupted() {
		return
	}
	domains := ds.runDomains()

	ds.CheckSuite.mu.Lock()
	if ds.coverChecking || ds.coverFailures >= maxCoverFailures || ds.CurrentPhase == PhaseConfirm || len(domains) == 0 || !everyDomainHasBaseline(domains, ds.domainResults) {
		ds.CheckSuite.mu.Unlock()
		return
	}
	if everyDomainLoadsPlain(domains, ds.domainResults) {
		if ds.closeFinishLocked() {
			ds.coverStop = coverStopBaseline
		}
		ds.CheckSuite.mu.Unlock()
		return
	}
	var batch []string
	for _, name := range coveringPresets(domains, ds.domainResults) {
		if ds.coverTried[name] {
			continue
		}
		batch = append(batch, name)
		if len(batch) == coverBatch {
			break
		}
	}
	if len(batch) == 0 {
		ds.CheckSuite.mu.Unlock()
		return
	}
	if ds.coverTried == nil {
		ds.coverTried = map[string]bool{}
	}
	for _, name := range batch {
		ds.coverTried[name] = true
	}
	ds.coverChecking = true
	ds.CheckSuite.mu.Unlock()

	winner, failures := ds.confirmCoverBatch(batch, domains)
	demoted := failures > 0

	ds.CheckSuite.mu.Lock()
	ds.coverChecking = false
	ds.coverFailures += failures
	exhausted := winner == "" && failures > 0 && ds.coverFailures >= maxCoverFailures
	stopped := false
	if winner != "" {
		ds.coverWinner = winner
		stopped = ds.closeFinishLocked()
		if stopped {
			ds.coverStop = coverStopCovered
			ds.StoppedCovered = true
		}
	}
	ds.CheckSuite.mu.Unlock()

	if stopped {
		log.DiscoveryLogf("'%s' passed every confirmation try on every address of the set", winner)
	}
	if exhausted {
		log.DiscoveryLogf("%d strategies failed confirmation on every address of the set, the search continues without stopping early", maxCoverFailures)
	}
	if demoted && winner == "" && !ds.canceled() {
		ds.determineBest()
	}
}

func (ds *DiscoverySuite) confirmCoverBatch(batch, domains []string) (string, int) {
	failures := 0
	for _, name := range batch {
		if ds.interrupted() {
			break
		}
		switch ds.confirmAcross(name, domains, true) {
		case jointPassed:
			return name, failures
		case jointFailed:
			failures++
		case jointAborted:
			return "", failures
		}
	}
	return "", failures
}

func (ds *DiscoverySuite) jointConfirm(presetName string, domains []string) (map[string]int, bool) {
	if ds.jointConfirmFn != nil {
		return ds.jointConfirmFn(presetName, domains)
	}
	set := ds.storedSetFor(presetName, domains)
	if set == nil {
		return map[string]int{}, true
	}
	return ds.confirmPreset(presetName, set, domains)
}

func (ds *DiscoverySuite) confirmAcross(presetName string, domains []string, searching bool) jointOutcome {
	log.DiscoveryLogf("Confirming '%s' on every address of the set in one configuration (%d tries)", presetName, confirmTries)
	ds.CheckSuite.mu.Lock()
	if ds.jointTried == nil {
		ds.jointTried = map[string]bool{}
	}
	ds.jointTried[presetName] = true
	ds.CheckSuite.mu.Unlock()
	passes, completed := ds.jointConfirm(presetName, domains)
	if !completed || ds.canceled() || (searching && ds.finishing()) {
		ds.clearConfirmation(presetName, domains)
		return jointAborted
	}

	var failing []string
	for _, domain := range domains {
		if passes[domain] < confirmTries {
			failing = append(failing, domain)
		}
	}
	if len(failing) == 0 {
		ds.CheckSuite.mu.Lock()
		if ds.jointConfirmed == nil {
			ds.jointConfirmed = map[string]bool{}
		}
		ds.jointConfirmed[presetName] = true
		ds.CheckSuite.mu.Unlock()
		return jointPassed
	}
	for _, domain := range failing {
		ds.demoteWinner(presetName, []string{domain},
			fmt.Sprintf("only %d of %d tries passed with every address of the set in one configuration", passes[domain], confirmTries))
	}
	return jointFailed
}

func (ds *DiscoverySuite) clearConfirmation(presetName string, domains []string) {
	ds.CheckSuite.mu.Lock()
	defer ds.CheckSuite.mu.Unlock()

	for _, domain := range domains {
		dr := ds.domainResults[domain]
		if dr == nil {
			continue
		}
		if r := dr.Results[presetName]; r != nil {
			r.Confirmed = 0
			r.ConfirmTries = 0
		}
		if dr.BestPreset == presetName {
			dr.Confirmed = 0
			dr.ConfirmTries = 0
		}
	}
	ds.refreshOutcomes(false)
}

func (ds *DiscoverySuite) resolveSetVerdict() {
	if !ds.setMode() || ds.canceled() {
		return
	}
	domains := ds.runDomains()

	ds.CheckSuite.mu.RLock()
	_, need, lost := ds.classifySetDomains(domains)
	winner := ds.coverWinner
	if winner != "" && !ds.jointlyConfirmedLocked(winner, domains) {
		winner = ""
	}
	candidates := coveringPresets(domains, ds.domainResults)
	ds.CheckSuite.mu.RUnlock()

	if (len(need) > 0 || len(lost) > 0) && winner == "" {
		winner = ds.findJointWinner(candidates, domains)
		if winner == "" && !ds.canceled() {
			winner = ds.confirmLargestGroup(domains)
		}
	}
	if ds.canceled() {
		return
	}

	ds.buildStrategyGroups()
	ds.CheckSuite.mu.Lock()
	verdict := ds.buildSetVerdict(domains, winner)
	ds.setVerdict = verdict
	ds.CheckSuite.mu.Unlock()

	switch verdict.Status {
	case SetVerdictCovered:
		log.DiscoveryLogf("Set verdict: '%s' works for every address of the set (confirmed)", verdict.WinnerPreset)
	case SetVerdictCurrentWorks:
		log.DiscoveryLogf("Set verdict: the set's current strategy works for every address (confirmed), nothing to change")
	case SetVerdictPartial:
		log.DiscoveryLogf("Set verdict: no single strategy works for every address; '%s' covers %v, not %v", verdict.WinnerPreset, verdict.Covered, verdict.Uncovered)
	case SetVerdictNotNeeded:
		log.DiscoveryLogf("Set verdict: every address loads without b4")
	case SetVerdictNone:
		log.DiscoveryLogf("Set verdict: nothing works for %v", verdict.Uncovered)
	}
}

func (ds *DiscoverySuite) publishSetVerdictLocked() {
	if !ds.setMode() {
		return
	}
	ds.SetVerdict = ds.setVerdict
	if ds.Status == CheckStatusCanceled || ds.SetVerdict == nil {
		ds.SetVerdict = &SetVerdict{Status: SetVerdictIncomplete}
	}
}

func (ds *DiscoverySuite) findJointWinner(candidates, domains []string) string {
	tried := 0
	demoted := false
	winner := ""
	for _, name := range candidates {
		if ds.canceled() {
			break
		}
		ds.CheckSuite.mu.RLock()
		confirmed := ds.jointlyConfirmedLocked(name, domains)
		ds.CheckSuite.mu.RUnlock()
		if confirmed {
			winner = name
			break
		}
		if tried == coverBatch {
			break
		}
		tried++
		ds.setPhase(PhaseConfirm)
		outcome := ds.confirmAcross(name, domains, false)
		if outcome == jointPassed {
			winner = name
			break
		}
		if outcome == jointAborted {
			break
		}
		demoted = true
	}
	if demoted && !ds.canceled() {
		ds.determineBest()
		ds.confirmWinners()
	}
	return winner
}

func (ds *DiscoverySuite) confirmLargestGroup(domains []string) string {
	ds.buildStrategyGroups()
	ds.CheckSuite.mu.RLock()
	name := ""
	if g := largestGroup(ds.StrategyGroups, domains); g != nil && !ds.jointTried[g.WinnerPreset] && completeEverywhere(g.WinnerPreset, domains, ds.domainResults) {
		name = g.WinnerPreset
	}
	ds.CheckSuite.mu.RUnlock()
	if name == "" {
		return ""
	}
	ds.setPhase(PhaseConfirm)
	switch ds.confirmAcross(name, domains, false) {
	case jointPassed:
		return name
	case jointFailed:
		if !ds.canceled() {
			ds.determineBest()
			ds.confirmWinners()
		}
	}
	return ""
}

func (ds *DiscoverySuite) jointlyConfirmedLocked(presetName string, domains []string) bool {
	return ds.jointConfirmed[presetName] && completeEverywhere(presetName, domains, ds.domainResults) && ds.confirmedOn(presetName, domains)
}

func (ds *DiscoverySuite) classifySetDomains(domains []string) (fine, need, lost []string) {
	for _, domain := range domains {
		dr := ds.domainResults[domain]
		switch {
		case dr != nil && dr.BaselineWorks:
			fine = append(fine, domain)
		case dr == nil || !dr.BestSuccess:
			lost = append(lost, domain)
		default:
			need = append(need, domain)
		}
	}
	return fine, need, lost
}

func (ds *DiscoverySuite) buildSetVerdict(domains []string, winner string) *SetVerdict {
	fine, need, lost := ds.classifySetDomains(domains)
	if len(need) == 0 && len(lost) == 0 {
		return &SetVerdict{Status: SetVerdictNotNeeded, NoBypass: fine}
	}
	open := inRunOrder(domains, append(append([]string(nil), need...), lost...))

	if winner != "" && completeEverywhere(winner, domains, ds.domainResults) {
		r := ds.domainResults[domains[0]].Results[winner]
		scoped := ds.scopeSetToDomains(r.Set, domains)
		status := SetVerdictCovered
		if winner == presetSetCurrent && !ds.setLacksDNSFix(scoped) {
			status = SetVerdictCurrentWorks
		}
		return &SetVerdict{
			Status:       status,
			WinnerPreset: winner,
			Family:       r.Family,
			Set:          scoped,
			Covered:      append([]string(nil), domains...),
			NoBypass:     fine,
			Confirmed:    true,
		}
	}

	if group := largestGroup(ds.StrategyGroups, domains); group != nil {
		inGroup := map[string]bool{}
		for _, d := range group.Domains {
			inGroup[d] = true
		}
		for _, d := range fine {
			if completeEverywhere(group.WinnerPreset, []string{d}, ds.domainResults) {
				inGroup[d] = true
			}
		}
		var covered, uncovered []string
		for _, d := range domains {
			if inGroup[d] {
				covered = append(covered, d)
			} else {
				uncovered = append(uncovered, d)
			}
		}
		return &SetVerdict{
			Status:       SetVerdictPartial,
			WinnerPreset: group.WinnerPreset,
			Family:       group.Family,
			Set:          group.Set,
			Covered:      covered,
			Uncovered:    uncovered,
			NoBypass:     fine,
			Confirmed:    ds.confirmedOn(group.WinnerPreset, group.Domains),
		}
	}

	return &SetVerdict{Status: SetVerdictNone, Uncovered: open, NoBypass: fine}
}

func (ds *DiscoverySuite) setLacksDNSFix(scoped *config.SetConfig) bool {
	if scoped == nil {
		return false
	}
	var live config.DNSConfig
	if ds.setStrategy != nil {
		live = ds.setStrategy.DNS
	}
	if scoped.DNS.Enabled && !live.Enabled {
		return true
	}
	for domain, ips := range scoped.DNS.Pins {
		if len(ips) > 0 && len(live.Pins[domain]) == 0 {
			return true
		}
	}
	return false
}

func (ds *DiscoverySuite) confirmedOn(presetName string, domains []string) bool {
	if len(domains) == 0 {
		return false
	}
	for _, domain := range domains {
		dr := ds.domainResults[domain]
		if dr == nil {
			return false
		}
		r := dr.Results[presetName]
		if r == nil || r.ConfirmTries == 0 || r.Confirmed < r.ConfirmTries {
			return false
		}
	}
	return true
}

func largestGroup(groups []StrategyGroup, domains []string) *StrategyGroup {
	var best *StrategyGroup
	bestHasFirst := false
	for i := range groups {
		g := &groups[i]
		hasFirst := len(domains) > 0 && containsString(g.Domains, domains[0])
		switch {
		case best == nil,
			len(g.Domains) > len(best.Domains),
			len(g.Domains) == len(best.Domains) && hasFirst && !bestHasFirst:
			best, bestHasFirst = g, hasFirst
		}
	}
	return best
}

func inRunOrder(domains, subset []string) []string {
	wanted := map[string]bool{}
	for _, d := range subset {
		wanted[d] = true
	}
	var out []string
	for _, d := range domains {
		if wanted[d] {
			out = append(out, d)
			delete(wanted, d)
		}
	}
	var rest []string
	for d := range wanted {
		rest = append(rest, d)
	}
	sort.Strings(rest)
	return append(out, rest...)
}
