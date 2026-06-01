package detector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	detectorHistoryFile = "detector_history.json"
	maxHistoryEntries   = 100
)

// HistoryEntry represents a completed detector suite result.
type HistoryEntry struct {
	Id        string      `json:"id"`
	Status    SuiteStatus `json:"status"`
	Tests     []TestType  `json:"tests"`
	StartTime time.Time   `json:"start_time"`
	EndTime   time.Time   `json:"end_time"`

	DNSResult      *DNSResult      `json:"dns_result,omitempty"`
	DNSAvailResult *DNSAvailResult `json:"dnsavail_result,omitempty"`
	DomainsResult  *DomainsResult  `json:"domains_result,omitempty"`
	TCPResult      *TCPResult      `json:"tcp_result,omitempty"`
	SNIResult      *SNIResult      `json:"sni_result,omitempty"`
	TelegramResult *TelegramResult `json:"telegram_result,omitempty"`
}

// DetectorHistory manages persistent detector results.
type DetectorHistory struct {
	Entries []HistoryEntry `json:"entries"`
	mu      sync.Mutex     `json:"-"`
}

func historyFilePath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), detectorHistoryFile)
}

// LoadDetectorHistory loads history from disk.
func LoadDetectorHistory(configPath string) *DetectorHistory {
	history := &DetectorHistory{}
	path := historyFilePath(configPath)
	if path == "" {
		return history
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return history
	}

	if err := json.Unmarshal(data, history); err != nil {
		log.Errorf("Failed to parse detector history: %v", err)
		return &DetectorHistory{}
	}

	log.Tracef("Loaded detector history with %d entries", len(history.Entries))
	return history
}

// Save persists history to disk.
func (dh *DetectorHistory) Save(configPath string) error {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	path := historyFilePath(configPath)
	if path == "" {
		return nil
	}

	data, err := json.MarshalIndent(dh, "", "  ")
	if err != nil {
		return log.Errorf("failed to marshal detector history: %v", err)
	}

	if err := os.WriteFile(path, data, 0644); err != nil {
		return log.Errorf("failed to write detector history: %v", err)
	}

	log.Tracef("Saved detector history with %d entries to %s", len(dh.Entries), path)
	return nil
}

// AddFromSuite saves results from a completed detector suite.
func (dh *DetectorHistory) AddFromSuite(suite *DetectorSuite) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	suite.mu.RLock()
	entry := HistoryEntry{
		Id:             suite.Id,
		Status:         suite.Status,
		Tests:          suite.Tests,
		StartTime:      suite.StartTime,
		EndTime:        suite.EndTime,
		DNSResult:      suite.DNSResult,
		DNSAvailResult: suite.DNSAvailResult,
		DomainsResult:  suite.DomainsResult,
		TCPResult:      suite.TCPResult,
		SNIResult:      suite.SNIResult,
		TelegramResult: suite.TelegramResult,
	}
	suite.mu.RUnlock()

	// Prepend new entry (newest first)
	dh.Entries = append([]HistoryEntry{entry}, dh.Entries...)

	// Enforce max entries
	if len(dh.Entries) > maxHistoryEntries {
		dh.Entries = dh.Entries[:maxHistoryEntries]
	}
}

// Clear removes all history entries.
func (dh *DetectorHistory) Clear() {
	dh.mu.Lock()
	defer dh.mu.Unlock()
	dh.Entries = nil
}

// RemoveEntry removes a history entry by ID.
func (dh *DetectorHistory) RemoveEntry(id string) {
	dh.mu.Lock()
	defer dh.mu.Unlock()

	for i, entry := range dh.Entries {
		if entry.Id == id {
			dh.Entries = append(dh.Entries[:i], dh.Entries[i+1:]...)
			return
		}
	}
}

// GetHistory loads and returns detector history from disk.
func GetHistory(configPath string) *DetectorHistory {
	return LoadDetectorHistory(configPath)
}

// SaveToHistory persists the suite results to history file.
func SaveToHistory(suite *DetectorSuite, configPath string) {
	history := LoadDetectorHistory(configPath)
	history.AddFromSuite(suite)
	if err := history.Save(configPath); err != nil {
		log.Errorf("Failed to save detector history: %v", err)
	} else {
		log.Tracef("Saved detector results to history")
	}
}
