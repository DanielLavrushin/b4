package discovery

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	discoveryHistoryFile = "discovery_history.json"
	maxHistoryEntries    = 100
)

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
	Set           *config.SetConfig              `json:"set,omitempty"`
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

// DiscoveryHistory manages persistent discovery results.
type DiscoveryHistory struct {
	Entries []HistoryEntry `json:"entries"`
	mu      sync.Mutex     `json:"-"`
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

	if err := os.WriteFile(path, data, 0644); err != nil {
		return log.Errorf("failed to write discovery history: %v", err)
	}

	log.Tracef("Saved discovery history with %d entries to %s", len(dh.Entries), path)
	return nil
}

// AddFromSuite saves all domain results from a completed suite.
func (dh *DiscoveryHistory) AddFromSuite(suite *CheckSuite) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	if suite.DomainDiscoveryResults == nil {
		return
	}

	for _, domainResult := range suite.DomainDiscoveryResults {
		bestFamily := StrategyFamily("")
		if domainResult.BestPreset != "" {
			if r, ok := domainResult.Results[domainResult.BestPreset]; ok {
				bestFamily = r.Family
			}
		}

		entry := HistoryEntry{
			SuiteId:       suite.Id,
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
		}

		// Replace existing entry for the same domain, or append
		replaced := false
		for i, existing := range dh.Entries {
			if existing.Domain == domainResult.Domain {
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

// Clear removes all history entries.
func (dh *DiscoveryHistory) Clear() {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.Entries = nil
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
	out := make(map[string]*DomainPresetResult, len(results))
	for name, r := range results {
		if r == nil {
			continue
		}
		if name == best || r.Set == nil {
			out[name] = r
			continue
		}
		trimmed := *r
		trimmed.Set = nil
		out[name] = &trimmed
	}
	return out
}
