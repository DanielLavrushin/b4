package ratelimit

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"sync"
	"time"
)

const (
	SharesPerDay    = 5
	VotesPerDay     = 20
	ReportsPerDay   = 20
	MirrorsPerDay   = 48
	NewKeysPerDay   = 5
	RequestsPerHour = 2000

	Day  = 24 * time.Hour
	Hour = time.Hour

	ScopeShare   = "share"
	ScopeVote    = "vote"
	ScopeReport  = "report"
	ScopeMirror  = "mirror"
	ScopeNewKey  = "newkey"
	ScopeRequest = "request"
)

type bucket struct {
	start time.Time
	count int
}

type Limiter struct {
	mu      sync.Mutex
	now     func() time.Time
	buckets map[string]*bucket
	sweptAt time.Time
}

func New(now func() time.Time) *Limiter {
	if now == nil {
		now = time.Now
	}
	return &Limiter{now: now, buckets: make(map[string]*bucket)}
}

func (l *Limiter) Allow(scope, id string, limit int, window time.Duration) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	l.sweep(now)
	start := now.Truncate(window)
	key := scope + "|" + id
	b := l.buckets[key]
	if b == nil {
		if len(l.buckets) >= maxLiveBuckets {
			return false, crowdedInterval
		}
		b = &bucket{start: start}
		l.buckets[key] = b
	} else if !b.start.Equal(start) {
		b.start = start
		b.count = 0
	}
	if b.count >= limit {
		return false, start.Add(window).Sub(now)
	}
	b.count++
	return true, 0
}

const (
	sweepInterval   = 10 * time.Minute
	crowdedInterval = time.Minute
	maxLiveBuckets  = 100000
)

func (l *Limiter) sweep(now time.Time) {
	since := now.Sub(l.sweptAt)
	crowded := len(l.buckets) >= maxLiveBuckets
	if since < sweepInterval && !(crowded && since >= crowdedInterval) {
		return
	}
	l.sweptAt = now
	for key, b := range l.buckets {
		if now.Sub(b.start) > Day {
			delete(l.buckets, key)
		}
	}
}

func Prefix(ip net.IP) string {
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return (&net.IPNet{IP: v4.Mask(net.CIDRMask(24, 32)), Mask: net.CIDRMask(24, 32)}).String()
	}
	return (&net.IPNet{IP: ip.Mask(net.CIDRMask(48, 128)), Mask: net.CIDRMask(48, 128)}).String()
}

func AddressKey(secret []byte, ip net.IP, now time.Time) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(now.UTC().Format("2006-01-02")))
	mac.Write([]byte{0})
	mac.Write([]byte(Prefix(ip)))
	return hex.EncodeToString(mac.Sum(nil))
}
