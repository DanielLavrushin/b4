package discovery

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	activeSuites = make(map[string]*CheckSuite)
	suitesMu     sync.RWMutex
)

func NewCheckSuite(url string) *CheckSuite {
	domain, checkURL := parseDiscoveryInput(url)

	return &CheckSuite{
		Id:        uuid.New().String(),
		Status:    CheckStatusPending,
		StartTime: time.Now(),
		cancel:    make(chan struct{}),
		CheckURL:  checkURL,
		Domain:    domain,
	}
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

	if suite.Status == CheckStatusRunning {
		close(suite.cancel)
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
