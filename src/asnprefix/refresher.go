package asnprefix

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	passInterval        = time.Hour
	retryBase           = 30 * time.Second
	retryMax            = time.Hour
	shrinkConfirmations = 3
	minWait             = time.Second
)

type attempt struct {
	failures  int
	shrinks   int
	candidate [sha256.Size]byte
	next      time.Time
}

func prefixFingerprint(prefixes []string) [sha256.Size]byte {
	return sha256.Sum256([]byte(strings.Join(slices.Sorted(slices.Values(prefixes)), "\n")))
}

type refresher struct {
	getCfg   func() *config.Config
	onChange func([]string)
	now      func() time.Time
	wake     <-chan struct{}
	state    map[string]*attempt
}

func newRefresher(getCfg func() *config.Config, onChange func([]string)) *refresher {
	return &refresher{
		getCfg:   getCfg,
		onChange: onChange,
		now:      nowFn,
		wake:     config.ASNRefreshRequests(),
		state:    make(map[string]*attempt),
	}
}

func Start(ctx context.Context, getCfg func() *config.Config, onChange func(changedIDs []string)) {
	r := newRefresher(getCfg, onChange)
	go r.run(ctx)
}

func ReferencedASNs(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	var raw []string
	for _, set := range cfg.Sets {
		if set == nil {
			continue
		}
		raw = append(raw, set.Targets.ASNs...)
	}
	ids := config.NormalizeASNs(raw)
	slices.SortFunc(ids, compareASN)
	return ids
}

func compareASN(a, b string) int {
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	}
	return 0
}

func retryDelay(n int) time.Duration {
	d := retryBase
	for i := 1; i < n && d < retryMax; i++ {
		d *= 2
	}
	if d > retryMax {
		d = retryMax
	}
	return d
}

func shrinksCoverage(previous, fetched *config.AsnInfo) bool {
	if previous == nil || len(previous.Prefixes) == 0 || previous.UpdatedAt <= 0 || previous.Source != config.AsnSourceRIPEstat {
		return false
	}
	before, after := previous.Counts(), fetched.Counts()
	return lessThanHalf(after.IPv4Addresses, before.IPv4Addresses) || lessThanHalf(after.IPv6Slash64s, before.IPv6Slash64s)
}

func lessThanHalf(after, before uint64) bool {
	return after < before && before-after > after
}

var ErrCoverageShrunk = errors.New("the fetched prefixes cover less than half of the known addresses")

func shrinkError(id string, previous, fetched *config.AsnInfo) error {
	before, after := previous.Counts(), fetched.Counts()
	return fmt.Errorf("AS%s: RIPEstat lists %d prefixes (%d IPv4 addresses, %d IPv6 /64s), the known list has %d (%d IPv4 addresses, %d IPv6 /64s): %w; the known list stays until the background refresh sees the same shrink %d times in a row",
		id, len(fetched.Prefixes), after.IPv4Addresses, after.IPv6Slash64s, len(previous.Prefixes), before.IPv4Addresses, before.IPv6Slash64s, ErrCoverageShrunk, shrinkConfirmations)
}

func (r *refresher) run(ctx context.Context) {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-r.wake:
		case <-timer.C:
		}
		wait := r.pass(ctx)
		if ctx.Err() != nil {
			return
		}
		timer.Stop()
		timer.Reset(wait)
	}
}

func (r *refresher) entry(id string) *attempt {
	st := r.state[id]
	if st == nil {
		st = &attempt{}
		r.state[id] = st
	}
	return st
}

func (r *refresher) pass(ctx context.Context) time.Duration {
	ids := ReferencedASNs(r.getCfg())
	wait := passInterval
	later := func(at time.Time) {
		if d := at.Sub(r.now()); d < wait {
			wait = d
		}
	}
	live := make(map[string]bool, len(ids))
	var changed []string
	s := config.Asns()
	for _, id := range ids {
		live[id] = true
		if ctx.Err() != nil {
			break
		}
		current := s.Get(id)
		if isFresh(current, r.now()) {
			delete(r.state, id)
			later(time.Unix(current.UpdatedAt, 0).Add(StaleAfter))
			continue
		}
		st := r.state[id]
		if st != nil && r.now().Before(st.next) {
			later(st.next)
			continue
		}
		fetched, err := fetchShared(ctx, id)
		if err != nil {
			if ctx.Err() != nil {
				break
			}
			recordFailure(id, err)
			st = r.entry(id)
			st.failures++
			st.next = r.now().Add(retryDelay(st.failures))
			if st.failures == 1 {
				if current != nil && len(current.Prefixes) > 0 {
					log.Warnf("ASN AS%s: cannot refresh the prefixes, keeping the %d known ones, next attempt in %v: %v", id, len(current.Prefixes), st.next.Sub(r.now()).Round(time.Second), err)
				} else {
					log.Warnf("ASN AS%s: cannot resolve the prefixes, sets that reference it match none of its addresses yet, next attempt in %v: %v", id, st.next.Sub(r.now()).Round(time.Second), err)
				}
			} else {
				log.Tracef("ASN AS%s: prefix refresh failed again (%d), next attempt in %v: %v", id, st.failures, st.next.Sub(r.now()).Round(time.Second), err)
			}
			later(st.next)
			continue
		}
		if shrinksCoverage(current, fetched) {
			st = r.entry(id)
			st.failures = 0
			if fp := prefixFingerprint(fetched.Prefixes); st.shrinks == 0 || fp != st.candidate {
				st.candidate = fp
				st.shrinks = 1
			} else {
				st.shrinks++
			}
			before, after := current.Counts(), fetched.Counts()
			if st.shrinks < shrinkConfirmations {
				st.next = r.now().Add(retryMax)
				recordFailure(id, shrinkError(id, current, fetched))
				log.Warnf("ASN AS%s: RIPEstat now lists %d prefixes covering %d IPv4 addresses and %d IPv6 /64s, less than half of the %d known prefixes (%d IPv4 addresses, %d IPv6 /64s); keeping the known list until the same shrink is seen %d times in a row (%d so far)", id, len(fetched.Prefixes), after.IPv4Addresses, after.IPv6Slash64s, len(current.Prefixes), before.IPv4Addresses, before.IPv6Slash64s, shrinkConfirmations, st.shrinks)
				later(st.next)
				continue
			}
			log.Warnf("ASN AS%s: accepting the reduced prefix list (%d prefixes, %d IPv4 addresses, %d IPv6 /64s; was %d, %d, %d) after %d consecutive fetches", id, len(fetched.Prefixes), after.IPv4Addresses, after.IPv6Slash64s, len(current.Prefixes), before.IPv4Addresses, before.IPv6Slash64s, st.shrinks)
		}
		delete(r.state, id)
		clearFailure(id)
		stored := store(fetched, current)
		if current == nil || !slices.Equal(current.Prefixes, stored.Prefixes) {
			changed = append(changed, id)
			log.Infof("ASN AS%s (%s): %d prefixes", id, stored.Name, len(stored.Prefixes))
		} else {
			log.Debugf("ASN AS%s: prefixes unchanged (%d)", id, len(stored.Prefixes))
		}
	}
	for id := range r.state {
		if !live[id] {
			delete(r.state, id)
		}
	}
	if len(changed) > 0 && r.onChange != nil && ctx.Err() == nil {
		r.onChange(changed)
	}
	if wait < minWait {
		wait = minWait
	}
	return wait
}
