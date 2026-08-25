package discovery

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

var (
	activeSuites = make(map[string]*CheckSuite)
	suitesMu     sync.RWMutex
)

// GetCurrentSuite returns the first running/pending suite, if any.
func GetCurrentSuite() (*CheckSuite, bool) {
	suitesMu.RLock()
	defer suitesMu.RUnlock()

	for _, suite := range activeSuites {
		if suite.Status == CheckStatusRunning || suite.Status == CheckStatusPending {
			return suite, true
		}
	}
	return nil, false
}

// GetHistory loads and returns discovery history from disk.
func GetHistory(configPath string) *DiscoveryHistory {
	return LoadDiscoveryHistory(configPath)
}

// SaveToHistory persists the suite results to history file.
func SaveToHistory(suite *CheckSuite, configPath string) {
	history := LoadDiscoveryHistory(configPath)
	history.AddFromSuite(suite)
	if err := history.Save(configPath); err != nil {
		log.Errorf("Failed to save discovery history: %v", err)
	} else {
		log.Tracef("Saved discovery results to history")
	}
}

func NewCheckSuite(domainInputs []DomainInput) *CheckSuite {
	if len(domainInputs) == 0 {
		return &CheckSuite{
			Id:        uuid.New().String(),
			Status:    CheckStatusFailed,
			StartTime: time.Now(),
			cancel:    make(chan struct{}),
			Domains:   domainInputs,
		}
	}

	primary := domainInputs[0]

	return &CheckSuite{
		Id:        uuid.New().String(),
		Status:    CheckStatusPending,
		StartTime: time.Now(),
		cancel:    make(chan struct{}),
		CheckURL:  primary.CheckURL,
		Domain:    primary.Domain,
		Domains:   domainInputs,
	}
}

func RegisterSuite(suite *CheckSuite) {
	suitesMu.Lock()
	activeSuites[suite.Id] = suite
	suitesMu.Unlock()
}

func GetCheckSuite(id string) (*CheckSuite, bool) {
	suitesMu.RLock()
	defer suitesMu.RUnlock()
	suite, ok := activeSuites[id]
	return suite, ok
}

func CancelCheckSuite(id string) error {
	suitesMu.Lock()
	defer suitesMu.Unlock()

	suite, ok := activeSuites[id]
	if !ok {
		return nil
	}

	suite.mu.Lock()
	defer suite.mu.Unlock()

	if suite.Status == CheckStatusPending || suite.Status == CheckStatusRunning {
		if suite.cancel != nil {
			select {
			case <-suite.cancel:
			default:
				close(suite.cancel)
			}
		}
		suite.Status = CheckStatusCanceled
	}

	return nil
}

func (ts *CheckSuite) MarshalJSON() ([]byte, error) {
	ts.mu.RLock()
	defer ts.mu.RUnlock()

	type Alias CheckSuite
	return json.Marshal(&struct {
		*Alias
	}{Alias: (*Alias)(ts)})
}
