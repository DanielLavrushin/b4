package hub

import (
	"time"

	"github.com/daniellavrushin/b4/log"
)

type CatalogueStatus struct {
	Epoch       int64  `json:"epoch"`
	Seq         int64  `json:"seq"`
	GeneratedAt string `json:"generated_at"`
	ExpiresAt   string `json:"expires_at"`
	Sets        int    `json:"sets"`
	Expired     bool   `json:"expired"`
}

type Status struct {
	Enabled    bool             `json:"enabled"`
	Configured bool             `json:"configured"`
	KeyID      string           `json:"key_id"`
	LastSync   string           `json:"last_sync"`
	LastError  string           `json:"last_error"`
	Catalogue  *CatalogueStatus `json:"catalogue"`
	URLs       []string         `json:"urls"`
	Network    Network          `json:"network"`
	Outbox     int              `json:"outbox"`
}

func (s *Service) Status() Status {
	st := Status{
		Enabled:    s.Enabled(),
		Configured: s.Configured(),
		URLs:       s.BaseURLs(),
		Network:    s.Network(),
		Outbox:     s.OutboxCount(),
	}
	if id, _, err := s.Identity(); err == nil {
		st.KeyID = id.KeyID()
	} else {
		log.Warnf("hub: identity unavailable: %v", err)
	}

	s.mu.RLock()
	if !s.lastSync.IsZero() {
		st.LastSync = s.lastSync.UTC().Format(time.RFC3339)
	}
	st.LastError = s.lastError
	if s.manifest != nil && s.catalogue != nil {
		st.Catalogue = &CatalogueStatus{
			Epoch:       s.manifest.Epoch,
			Seq:         s.manifest.Seq,
			GeneratedAt: s.manifest.GeneratedAt,
			ExpiresAt:   s.manifest.ExpiresAt,
			Sets:        len(s.catalogue.Sets),
			Expired:     s.manifest.Expired(s.now()),
		}
	}
	s.mu.RUnlock()
	return st
}
