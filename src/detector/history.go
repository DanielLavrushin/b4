package detector

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

const (
	detectorHistoryFile = "detector_history.json"
	maxHistoryEntries   = 50
)

type History struct {
	Entries []*Suite `json:"entries"`
	mu      sync.Mutex
}

func historyFilePath(configPath string) string {
	if configPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(configPath), detectorHistoryFile)
}

func LoadHistory(configPath string) *History {
	history := &History{}
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
		return &History{}
	}
	kept := history.Entries[:0]
	for _, e := range history.Entries {
		if e != nil && len(e.Options.Scopes) > 0 {
			kept = append(kept, e)
		}
	}
	history.Entries = kept
	return history
}

func (h *History) Save(configPath string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	path := historyFilePath(configPath)
	if path == "" {
		return nil
	}
	data, err := json.MarshalIndent(h, "", "  ")
	if err != nil {
		return log.Errorf("failed to marshal detector history: %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		return log.Errorf("failed to write detector history: %v", err)
	}
	return nil
}

func (h *History) Add(s *Suite, final SuiteStatus) {
	h.mu.Lock()
	defer h.mu.Unlock()
	entry := s.snapshot()
	entry.Status = final
	entry.Stopping = false
	entry.Progress = Progress{}
	kept := make([]*Suite, 0, len(h.Entries)+1)
	kept = append(kept, entry)
	for _, e := range h.Entries {
		if e.Id != entry.Id {
			kept = append(kept, e)
		}
	}
	if len(kept) > maxHistoryEntries {
		kept = kept[:maxHistoryEntries]
	}
	h.Entries = kept
}

func (h *History) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.Entries = nil
}

func (h *History) Remove(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, e := range h.Entries {
		if e.Id == id {
			h.Entries = append(h.Entries[:i], h.Entries[i+1:]...)
			return
		}
	}
}

func (h *History) Get(id string) *Suite {
	h.mu.Lock()
	defer h.mu.Unlock()
	for _, e := range h.Entries {
		if e.Id == id {
			return e
		}
	}
	return nil
}

func SaveToHistory(s *Suite, configPath string, final SuiteStatus) {
	history := LoadHistory(configPath)
	history.Add(s, final)
	if err := history.Save(configPath); err != nil {
		log.Errorf("Failed to save detector history: %v", err)
	}
}
