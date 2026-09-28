package dns

import (
	"net/url"
	"sync"
	"time"
)

const (
	SourceFailuresToTrip = 3
	sourceCooldown       = 30 * time.Second
)

type sourceState struct {
	failures int
	retryAt  time.Time
}

var (
	sourceMu     sync.Mutex
	sourceHealth = map[string]*sourceState{}
)

func SourceLabel(source string) string {
	if u, err := url.Parse(source); err == nil && u.Host != "" {
		return u.Host
	}
	return source
}

func SourceUnreachable(source string) bool {
	if source == "" {
		return false
	}
	sourceMu.Lock()
	defer sourceMu.Unlock()

	state := sourceHealth[source]
	if state == nil || state.failures < SourceFailuresToTrip {
		return false
	}
	if time.Now().Before(state.retryAt) {
		return true
	}
	state.retryAt = time.Now().Add(sourceCooldown)
	return false
}

func NoteSourceFailure(source string) (tripped bool) {
	if source == "" {
		return false
	}
	sourceMu.Lock()
	defer sourceMu.Unlock()

	state := sourceHealth[source]
	if state == nil {
		state = &sourceState{}
		sourceHealth[source] = state
	}
	state.failures++
	if state.failures == SourceFailuresToTrip {
		state.retryAt = time.Now().Add(sourceCooldown)
		return true
	}
	return false
}

func NoteSourceSuccess(source string) (recovered bool) {
	if source == "" {
		return false
	}
	sourceMu.Lock()
	defer sourceMu.Unlock()

	state := sourceHealth[source]
	if state == nil || state.failures == 0 {
		return false
	}
	delete(sourceHealth, source)
	return state.failures >= SourceFailuresToTrip
}

func ResetSourceHealth() {
	sourceMu.Lock()
	sourceHealth = map[string]*sourceState{}
	sourceMu.Unlock()
}
