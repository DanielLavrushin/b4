package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/utils"
)

const (
	discoveryHistoryFile = "discovery_history.json"
	maxHistoryEntries    = 100
	maxAlternateSets     = 12
	maxSetRuns           = 64
)

var historyFileMu sync.Mutex

type AppliedMark struct {
	SetId string    `json:"set_id,omitempty"`
	At    time.Time `json:"at"`
}

// HistoryEntry represents a completed discovery result for a single domain.
type HistoryEntry struct {
	Domain        string                         `json:"domain"`
	Url           string                         `json:"url"`
	BestPreset    string                         `json:"best_preset"`
	BestSpeed     float64                        `json:"best_speed"`
	BestSuccess   bool                           `json:"best_success"`
	BestFamily    StrategyFamily                 `json:"best_family,omitempty"`
	Status        CheckStatus                    `json:"status"`
	StartTime     time.Time                      `json:"start_time"`
	EndTime       time.Time                      `json:"end_time"`
	Results       map[string]*DomainPresetResult `json:"results,omitempty"`
	DNSResult     *DNSDiscoveryResult            `json:"dns_result,omitempty"`
	BaselineSpeed float64                        `json:"baseline_speed,omitempty"`
	BaselineWorks bool                           `json:"baseline_works,omitempty"`
	Confirmed     int                            `json:"confirmed,omitempty"`
	ConfirmTries  int                            `json:"confirm_tries,omitempty"`
	FinalHost     string                         `json:"final_host,omitempty"`
	SuiteId       string                         `json:"suite_id,omitempty"`
	SetId         string                         `json:"set_id,omitempty"`
	Set           *config.SetConfig              `json:"set,omitempty"`
	Outcome       Outcome                        `json:"outcome,omitempty"`
	Unconfirmed   bool                           `json:"unconfirmed,omitempty"`
	StoppedEarly  bool                           `json:"stopped_early,omitempty"`
	Order         int                            `json:"order,omitempty"`
	Applied       map[string]AppliedMark         `json:"applied,omitempty"`
}

func (e HistoryEntry) StorageBytes() int {
	data, err := json.MarshalIndent(e, "    ", "  ")
	if err != nil {
		return 0
	}
	return len(data)
}

func (e HistoryEntry) AppliedSet(preset string) string {
	return e.Applied[preset].SetId
}

// EffectiveOutcome returns the recorded outcome, deriving it for entries
// written before the field existed.
func (e HistoryEntry) EffectiveOutcome() Outcome {
	if e.Outcome != "" {
		return e.Outcome
	}
	switch {
	case e.BaselineWorks:
		return OutcomeWorksWithoutBypass
	case e.BestSuccess && e.BestPreset != "" && e.BestPreset != presetNoBypass:
		return OutcomeFound
	case e.DNSResult.gatewayIntercepted():
		return OutcomeGatewayIntercepted
	case e.DNSResult != nil && e.DNSResult.TransportBlocked:
		return OutcomeAddressBlocked
	default:
		return OutcomeNotFound
	}
}

// ApplicableSet returns the set a caller can install for this entry: the
// group-scoped winner the run built, or the winning preset's own set for an
// entry written before that was recorded.
func (e HistoryEntry) ApplicableSet() *config.SetConfig {
	if e.Set != nil {
		return e.Set
	}
	if e.BestPreset == "" || e.Results == nil {
		return nil
	}
	if r, ok := e.Results[e.BestPreset]; ok && r != nil {
		return r.Set
	}
	return nil
}

type SetRunRecord struct {
	SetId          string     `json:"set_id"`
	SuiteId        string     `json:"suite_id"`
	StartTime      time.Time  `json:"start_time"`
	EndTime        time.Time  `json:"end_time"`
	URLs           []string   `json:"urls"`
	StoppedCovered bool       `json:"stopped_covered,omitempty"`
	Verdict        SetVerdict `json:"verdict"`
}

// DiscoveryHistory manages persistent discovery results.
type DiscoveryHistory struct {
	Entries []HistoryEntry          `json:"entries"`
	SetRuns map[string]SetRunRecord `json:"set_runs,omitempty"`
	mu      sync.Mutex              `json:"-"`
}

func historyFilePath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), discoveryHistoryFile)
}

// LoadDiscoveryHistory loads history from disk.
func LoadDiscoveryHistory(configPath string) *DiscoveryHistory {
	history := &DiscoveryHistory{}
	path := historyFilePath(configPath)
	if path == "" {
		return history
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return history
	}

	if err := json.Unmarshal(data, history); err != nil {
		log.Errorf("Failed to parse discovery history: %v", err)
		return &DiscoveryHistory{}
	}

	log.Tracef("Loaded discovery history with %d entries", len(history.Entries))
	return history
}

// Save persists history to disk.
func (dh *DiscoveryHistory) Save(configPath string) error {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	path := historyFilePath(configPath)
	if path == "" {
		return nil
	}

	data, err := json.MarshalIndent(dh, "", "  ")
	if err != nil {
		return log.Errorf("failed to marshal discovery history: %v", err)
	}

	if err := utils.WriteFileAtomic(path, data, 0644); err != nil {
		return log.Errorf("failed to write discovery history: %v", err)
	}

	log.Tracef("Saved discovery history with %d entries to %s", len(dh.Entries), path)
	return nil
}

func UpdateHistory(configPath string, change func(*DiscoveryHistory) bool) error {
	historyFileMu.Lock()
	defer historyFileMu.Unlock()
	history := LoadDiscoveryHistory(configPath)
	if !change(history) {
		return nil
	}
	return history.Save(configPath)
}

// AddFromSuite saves all domain results from a completed suite.
func (dh *DiscoveryHistory) AddFromSuite(suite *CheckSuite) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	dh.recordSetRun(suite)

	if suite.DomainDiscoveryResults == nil {
		return
	}

	ordered := make([]*DomainDiscoveryResult, 0, len(suite.DomainDiscoveryResults))
	seen := make(map[string]bool, len(suite.DomainDiscoveryResults))
	for _, di := range suite.Domains {
		if dr := suite.DomainDiscoveryResults[di.Domain]; dr != nil && !seen[di.Domain] {
			ordered = append(ordered, dr)
			seen[di.Domain] = true
		}
	}
	rest := make([]string, 0, len(suite.DomainDiscoveryResults))
	for domain := range suite.DomainDiscoveryResults {
		if !seen[domain] {
			rest = append(rest, domain)
		}
	}
	sort.Strings(rest)
	for _, domain := range rest {
		if dr := suite.DomainDiscoveryResults[domain]; dr != nil {
			ordered = append(ordered, dr)
		}
	}

	for position, domainResult := range ordered {
		bestFamily := StrategyFamily("")
		if domainResult.BestPreset != "" {
			if r, ok := domainResult.Results[domainResult.BestPreset]; ok {
				bestFamily = r.Family
			}
		}

		entry := HistoryEntry{
			SuiteId:       suite.Id,
			SetId:         suite.SetId,
			Set:           suite.scopedSetFor(domainResult.Domain),
			Domain:        domainResult.Domain,
			Url:           domainResult.Url,
			BestPreset:    domainResult.BestPreset,
			BestSpeed:     domainResult.BestSpeed,
			BestSuccess:   domainResult.BestSuccess,
			BestFamily:    bestFamily,
			Status:        suite.Status,
			StartTime:     suite.StartTime,
			EndTime:       suite.EndTime,
			Results:       trimPresetSets(domainResult.Results, domainResult.BestPreset),
			DNSResult:     domainResult.DNSResult,
			BaselineSpeed: domainResult.BaselineSpeed,
			BaselineWorks: domainResult.BaselineWorks,
			Confirmed:     domainResult.Confirmed,
			ConfirmTries:  domainResult.ConfirmTries,
			FinalHost:     domainResult.FinalHost,
			Outcome:       domainResult.Outcome,
			Unconfirmed:   domainResult.Unconfirmed,
			StoppedEarly:  suite.StoppedEarly,
			Order:         position + 1,
		}

		// Replace existing entry for the same domain, or append
		replaced := false
		for i, existing := range dh.Entries {
			if existing.Domain == domainResult.Domain {
				entry.Applied = existing.Applied
				dh.Entries[i] = entry
				replaced = true
				break
			}
		}
		if !replaced {
			dh.Entries = append(dh.Entries, entry)
		}
	}

	// Enforce max entries — keep most recent
	if len(dh.Entries) > maxHistoryEntries {
		sort.Slice(dh.Entries, func(i, j int) bool {
			return dh.Entries[i].EndTime.After(dh.Entries[j].EndTime)
		})
		dh.Entries = dh.Entries[:maxHistoryEntries]
	}
}

func (dh *DiscoveryHistory) recordSetRun(suite *CheckSuite) {
	if suite.SetId == "" || suite.SetVerdict == nil {
		return
	}
	urls := make([]string, 0, len(suite.Domains))
	for _, di := range suite.Domains {
		if di.CheckURL != "" {
			urls = append(urls, di.CheckURL)
		}
	}
	if dh.SetRuns == nil {
		dh.SetRuns = map[string]SetRunRecord{}
	}
	dh.SetRuns[suite.SetId] = SetRunRecord{
		SetId:          suite.SetId,
		SuiteId:        suite.Id,
		StartTime:      suite.StartTime,
		EndTime:        suite.EndTime,
		URLs:           urls,
		StoppedCovered: suite.StoppedCovered,
		Verdict:        *suite.SetVerdict,
	}
	for len(dh.SetRuns) > maxSetRuns {
		oldest := ""
		for id, rec := range dh.SetRuns {
			if oldest == "" || rec.EndTime.Before(dh.SetRuns[oldest].EndTime) {
				oldest = id
			}
		}
		delete(dh.SetRuns, oldest)
	}
}

func (dh *DiscoveryHistory) SetRunsNewestFirst() []SetRunRecord {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	runs := make([]SetRunRecord, 0, len(dh.SetRuns))
	for _, run := range dh.SetRuns {
		runs = append(runs, run)
	}
	sort.Slice(runs, func(i, j int) bool {
		if !runs[i].EndTime.Equal(runs[j].EndTime) {
			return runs[i].EndTime.After(runs[j].EndTime)
		}
		return runs[i].SetId < runs[j].SetId
	})
	return runs
}

func (dh *DiscoveryHistory) SetRunForSuite(suiteID string) (SetRunRecord, bool) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	if suiteID == "" {
		return SetRunRecord{}, false
	}
	for _, run := range dh.SetRuns {
		if run.SuiteId == suiteID {
			return run, true
		}
	}
	return SetRunRecord{}, false
}

func (dh *DiscoveryHistory) MarkApplied(domains []string, preset, setID string) int {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	if preset == "" || len(domains) == 0 {
		return 0
	}
	wanted := make(map[string]bool, len(domains))
	for _, d := range domains {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			wanted[d] = true
		}
	}

	marked := 0
	for i := range dh.Entries {
		if !wanted[strings.ToLower(dh.Entries[i].Domain)] {
			continue
		}
		if dh.Entries[i].Applied == nil {
			dh.Entries[i].Applied = make(map[string]AppliedMark, 1)
		}
		dh.Entries[i].Applied[preset] = AppliedMark{SetId: setID, At: time.Now()}
		marked++
	}
	return marked
}

func (dh *DiscoveryHistory) AppliedSetFor(domain, preset string) string {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	domain = strings.ToLower(strings.TrimSpace(domain))
	for _, e := range dh.Entries {
		if strings.ToLower(e.Domain) == domain {
			return e.AppliedSet(preset)
		}
	}
	return ""
}

// Clear removes all history entries.
func (dh *DiscoveryHistory) Clear() {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.Entries = nil
	dh.SetRuns = nil
}

// RemoveDomain removes history for a specific domain.
func (dh *DiscoveryHistory) RemoveDomain(domain string) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	for i, entry := range dh.Entries {
		if entry.Domain == domain {
			dh.Entries = append(dh.Entries[:i], dh.Entries[i+1:]...)
			return
		}
	}
}

func (ts *CheckSuite) scopedSetFor(domain string) *config.SetConfig {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	for _, g := range ts.StrategyGroups {
		if g.Set == nil {
			continue
		}
		for _, d := range g.Domains {
			if d == domain {
				return g.Set
			}
		}
	}
	return nil
}

func trimPresetSets(results map[string]*DomainPresetResult, best string) map[string]*DomainPresetResult {
	if len(results) == 0 {
		return results
	}
	keep := map[string]bool{best: true}
	for _, name := range fastestAlternates(results, best, maxAlternateSets) {
		keep[name] = true
	}
	out := make(map[string]*DomainPresetResult, len(results))
	for name, r := range results {
		if r == nil {
			continue
		}
		if keep[name] || r.Set == nil {
			out[name] = r
			continue
		}
		trimmed := *r
		trimmed.Set = nil
		out[name] = &trimmed
	}
	return out
}

func fastestAlternates(results map[string]*DomainPresetResult, best string, limit int) []string {
	names := make([]string, 0, len(results))
	for name, r := range results {
		if r == nil || r.Set == nil || name == best || name == presetNoBypass {
			continue
		}
		if r.Status != CheckStatusComplete {
			continue
		}
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		a, b := results[names[i]], results[names[j]]
		if a.Speed != b.Speed {
			return a.Speed > b.Speed
		}
		return names[i] < names[j]
	})
	if len(names) > limit {
		names = names[:limit]
	}
	return names
}
