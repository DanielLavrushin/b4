package detector

import (
	"encoding/json"
	"sync"

	"github.com/google/uuid"
)

var (
	activeSuites = make(map[string]*DetectorSuite)
	suitesMu     sync.RWMutex
)

func NewDetectorSuite(tests []TestType, mark uint) *DetectorSuite {
	suite := &DetectorSuite{
		Id:     uuid.New().String(),
		Status: StatusPending,
		Tests:  tests,
		mark:   mark,
		cancel: make(chan struct{}),
	}

	suitesMu.Lock()
	activeSuites[suite.Id] = suite
	suitesMu.Unlock()

	return suite
}

func GetSuite(id string) (*DetectorSuite, bool) {
	suitesMu.RLock()
	defer suitesMu.RUnlock()
	suite, ok := activeSuites[id]
	return suite, ok
}

func CancelSuite(id string) error {
	suitesMu.Lock()
	defer suitesMu.Unlock()

	suite, ok := activeSuites[id]
	if !ok {
		return nil
	}

	if suite.Status == StatusRunning {
		close(suite.cancel)
		suite.Status = StatusCanceled
	}

	return nil
}

func (s *DetectorSuite) isCanceled() bool {
	select {
	case <-s.cancel:
		return true
	default:
		return false
	}
}

func (s *DetectorSuite) MarshalJSON() ([]byte, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	type Alias DetectorSuite
	return json.Marshal(&struct {
		*Alias
	}{Alias: (*Alias)(s)})
}
