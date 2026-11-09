package dns

import "time"

func expireCooldown(source string) {
	sourceMu.Lock()
	defer sourceMu.Unlock()
	if state := sourceHealth[source]; state != nil {
		state.retryAt = time.Now().Add(-time.Second)
	}
}
