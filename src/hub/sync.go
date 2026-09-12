package hub

import (
	"context"
	"fmt"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
)

func (s *Service) Sync(ctx context.Context) (bool, error) {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	trusted := s.TrustedKeys()
	if !s.Configured() {
		s.markFailed(ErrNotConfigured)
		return false, ErrNotConfigured
	}

	var lastErr error
	for _, base := range s.BaseURLs() {
		if ctx.Err() != nil {
			return false, ctx.Err()
		}
		if !s.healthy(ctx, base) {
			lastErr = fmt.Errorf("%s did not answer the health check", base)
			continue
		}
		m, err := s.fetchManifest(ctx, base)
		if err != nil {
			lastErr = err
			continue
		}
		if err := hubwire.VerifyManifest(m, trusted); err != nil {
			lastErr = fmt.Errorf("%s: %w", base, err)
			continue
		}
		s.setPreferredBase(base)

		stored := s.Manifest()
		if stored != nil && !m.Newer(stored) && s.catalogueLoaded() {
			s.adoptManifest(m)
			s.markSynced()
			return false, nil
		}

		gz, err := s.fetchCatalogueFile(ctx, base, m.Catalogue)
		if err != nil {
			lastErr = err
			continue
		}
		cat, err := decodeCatalogue(gz)
		if err != nil {
			lastErr = fmt.Errorf("%s: %w", base, err)
			continue
		}
		if cat.Epoch != m.Epoch || cat.Seq != m.Seq {
			lastErr = fmt.Errorf("%s: catalogue %d-%d does not match manifest %d-%d", base, cat.Epoch, cat.Seq, m.Epoch, m.Seq)
			continue
		}
		if err := s.store().save(m, gz); err != nil {
			err = fmt.Errorf("could not store the catalogue: %w", err)
			s.markFailed(err)
			return false, err
		}
		s.install(m, cat)
		s.adoptManifest(m)
		s.markSynced()
		log.Infof("hub: catalogue %d-%d with %d sets synced from %s", cat.Epoch, cat.Seq, len(cat.Sets), base)
		return true, nil
	}

	if lastErr == nil {
		lastErr = ErrUnreachable
	} else {
		lastErr = fmt.Errorf("%w: %w", ErrUnreachable, lastErr)
	}
	s.markFailed(lastErr)
	return false, lastErr
}
