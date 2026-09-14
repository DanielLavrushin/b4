package store

import (
	"context"
	"fmt"
	"strconv"
	"sync"

	"github.com/daniellavrushin/b4hub/internal/ratelimit"
)

const (
	metaSharesPerDay    = "settings.shares_per_day"
	metaVotesPerDay     = "settings.votes_per_day"
	metaReportsPerDay   = "settings.reports_per_day"
	metaMirrorsPerDay   = "settings.mirrors_per_day"
	metaNewKeysPerDay   = "settings.new_keys_per_day"
	metaRequestsPerHour = "settings.requests_per_hour"

	MaxLimitValue = 1_000_000
)

type Settings struct {
	SharesPerDay    int
	VotesPerDay     int
	ReportsPerDay   int
	MirrorsPerDay   int
	NewKeysPerDay   int
	RequestsPerHour int
}

func DefaultSettings() Settings {
	return Settings{
		SharesPerDay:    ratelimit.SharesPerDay,
		VotesPerDay:     ratelimit.VotesPerDay,
		ReportsPerDay:   ratelimit.ReportsPerDay,
		MirrorsPerDay:   ratelimit.MirrorsPerDay,
		NewKeysPerDay:   ratelimit.NewKeysPerDay,
		RequestsPerHour: ratelimit.RequestsPerHour,
	}
}

func (s *Settings) fields() []settingsField {
	return []settingsField{
		{metaSharesPerDay, "shares_per_day", &s.SharesPerDay},
		{metaVotesPerDay, "votes_per_day", &s.VotesPerDay},
		{metaReportsPerDay, "reports_per_day", &s.ReportsPerDay},
		{metaMirrorsPerDay, "mirrors_per_day", &s.MirrorsPerDay},
		{metaNewKeysPerDay, "new_keys_per_day", &s.NewKeysPerDay},
		{metaRequestsPerHour, "requests_per_hour", &s.RequestsPerHour},
	}
}

type settingsField struct {
	meta  string
	name  string
	value *int
}

func (s Settings) Validate() error {
	for _, f := range s.fields() {
		if *f.value < 1 || *f.value > MaxLimitValue {
			return fmt.Errorf("%s must be between 1 and %d", f.name, MaxLimitValue)
		}
	}
	return nil
}

type settingsCache struct {
	mu     sync.Mutex
	loaded bool
	value  Settings
}

func (s *Store) Settings(ctx context.Context) (Settings, error) {
	s.settings.mu.Lock()
	defer s.settings.mu.Unlock()
	if s.settings.loaded {
		return s.settings.value, nil
	}
	out := DefaultSettings()
	for _, f := range out.fields() {
		raw, err := s.Meta(ctx, f.meta)
		if err != nil {
			return DefaultSettings(), err
		}
		if raw == "" {
			continue
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return DefaultSettings(), fmt.Errorf("meta %s holds %q", f.meta, raw)
		}
		*f.value = n
	}
	s.settings.value = out
	s.settings.loaded = true
	return out, nil
}

func (s *Store) SaveSettings(ctx context.Context, in Settings) error {
	if err := in.Validate(); err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, f := range in.fields() {
		if err := setMetaTx(ctx, tx, f.meta, strconv.Itoa(*f.value)); err != nil {
			return err
		}
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	s.settings.mu.Lock()
	s.settings.value = in
	s.settings.loaded = true
	s.settings.mu.Unlock()
	return nil
}
