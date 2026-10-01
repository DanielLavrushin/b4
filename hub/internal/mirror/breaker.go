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
	openUntil time.Time
	downSince time.Time
}

func (b *breaker) allow(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures < breakerThreshold {
		return true
	}
	if now.Before(b.openUntil) {
		return false
	}
	b.openUntil = now.Add(b.backoff)
	return true
}

func (b *breaker) open(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.failures >= breakerThreshold && now.Before(b.openUntil)
}

func (b *breaker) failure(now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.downSince.IsZero() {
		b.downSince = now
	}
	if b.failures < breakerThreshold {
		return
	}
	switch {
	case b.backoff == 0:
		b.backoff = breakerMinBackoff
	case b.backoff < breakerMaxBackoff:
		b.backoff *= 2
		if b.backoff > breakerMaxBackoff {
			b.backoff = breakerMaxBackoff
		}
	}
	b.openUntil = now.Add(b.backoff)
}

func (b *breaker) success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.backoff = 0
	b.openUntil = time.Time{}
	b.downSince = time.Time{}
}

func (b *breaker) since() time.Time {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.failures < breakerThreshold {
		return time.Time{}
	}
	return b.downSince
}
