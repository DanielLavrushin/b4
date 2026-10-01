package mirror

import (
	"sync"
	"time"
)

const (
	breakerThreshold  = 2
	breakerMinBackoff = 30 * time.Second
	breakerMaxBackoff = 5 * time.Minute
)

type breaker struct {
	mu        sync.Mutex
	failures  int
	backoff   time.Duration
	openedAt  time.Time
	openUntil time.Time
	downSince time.Time
	probing   bool
}

func (b *breaker) allow(now time.Time) (ok, probe bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures < breakerThreshold {
		return true, false
	}
	if b.probing || now.Before(b.openUntil) {
		return false, false
	}
	b.probing = true
	return true, true
}

func (b *breaker) failure(start, now time.Time, probe bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if probe {
		b.probing = false
	}
	b.failures++
	if b.downSince.IsZero() {
		b.downSince = start
	}
	if b.failures < breakerThreshold {
		return
	}
	switch {
	case b.backoff == 0:
		b.backoff = breakerMinBackoff
	case probe || !start.Before(b.openedAt):
		b.backoff *= 2
		if b.backoff > breakerMaxBackoff {
			b.backoff = breakerMaxBackoff
		}
	default:
		return
	}
	b.openedAt = now
	b.openUntil = now.Add(b.backoff)
}

func (b *breaker) release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.probing = false
}

func (b *breaker) success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.backoff = 0
	b.openedAt = time.Time{}
	b.openUntil = time.Time{}
	b.downSince = time.Time{}
	b.probing = false
}

func (b *breaker) since() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures < breakerThreshold {
		return time.Time{}
	}
	return b.downSince
}
