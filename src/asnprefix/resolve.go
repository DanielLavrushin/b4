package asnprefix

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	StaleAfter    = 20 * time.Hour
	flightTimeout = 2*fetchTimeout + 5*time.Second
)

type flight struct {
	done chan struct{}
	info *config.AsnInfo
	err  error
}

var (
	flightMu sync.Mutex
	flights  = map[string]*flight{}

	statusMu sync.Mutex
	failures = map[string]string{}
)

func copyInfo(info *config.AsnInfo) *config.AsnInfo {
	if info == nil {
		return nil
	}
	c := *info
	c.Prefixes = slices.Clone(info.Prefixes)
	return &c
}

func fetchShared(ctx context.Context, id string) (*config.AsnInfo, error) {
	flightMu.Lock()
	f, running := flights[id]
	if !running {
		f = &flight{done: make(chan struct{})}
		flights[id] = f
		go func() {
			fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), flightTimeout)
			defer cancel()
			f.info, f.err = Fetch(fctx, id)
			flightMu.Lock()
			delete(flights, id)
			flightMu.Unlock()
			close(f.done)
		}()
	}
	flightMu.Unlock()
	select {
	case <-f.done:
		if f.err != nil {
			return nil, f.err
		}
		return copyInfo(f.info), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func isFresh(info *config.AsnInfo, now time.Time) bool {
	if info == nil || len(info.Prefixes) == 0 || info.UpdatedAt <= 0 {
		return false
	}
	return now.Sub(time.Unix(info.UpdatedAt, 0)) < StaleAfter
}

func recordFailure(id string, err error) {
	statusMu.Lock()
	failures[id] = err.Error()
	statusMu.Unlock()
}

func clearFailure(id string) {
	statusMu.Lock()
	delete(failures, id)
	statusMu.Unlock()
}

func LastError(id string) string {
	norm, ok := config.NormalizeASN(id)
	if !ok {
		return ""
	}
	statusMu.Lock()
	defer statusMu.Unlock()
	return failures[norm]
}

func store(info, previous *config.AsnInfo) *config.AsnInfo {
	if info.Name == "" && previous != nil {
		info.Name = previous.Name
	}
	s := config.Asns()
	if err := s.Put(info); err != nil {
		log.Errorf("ASN AS%s: the fetched prefixes are in use but could not be saved to %s: %v", info.ID, s.Path(), err)
	}
	if stored := s.Get(info.ID); stored != nil {
		return stored
	}
	return info
}

func Resolve(ctx context.Context, id string, force bool) (*config.AsnInfo, error) {
	norm, ok := config.NormalizeASN(id)
	if !ok {
		return nil, fmt.Errorf("%q: %w", strings.TrimSpace(id), ErrInvalidASN)
	}
	current := config.Asns().Get(norm)
	if !force && isFresh(current, nowFn()) {
		return current, nil
	}
	info, err := fetchShared(ctx, norm)
	if err != nil {
		if ctx.Err() == nil {
			recordFailure(norm, err)
		}
		return nil, err
	}
	clearFailure(norm)
	return store(info, current), nil
}
