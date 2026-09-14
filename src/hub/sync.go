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
	s.dropUntrusted(trusted)

	var lastErr, catalogueErr error
	staleBases := 0
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
		stored := s.Manifest()
		if stored != nil && s.catalogueLoaded() && !m.Newer(stored) {
			if sameCatalogue(m, stored) {
				s.setPreferredBase(base)
				s.adoptManifest(m)
				s.markSynced()
				return false, nil
			}
			staleBases++
			lastErr = fmt.Errorf("%s serves an older catalogue %d-%d than the stored %d-%d", base, m.Epoch, m.Seq, stored.Epoch, stored.Seq)
			log.Warnf("hub: %v, trying the next address", lastErr)
			continue
		}
		s.setPreferredBase(base)

		gz, err := s.fetchCatalogueFile(ctx, base, m.Catalogue)
		if err != nil {
			catalogueErr = err
			continue
		}
		cat, err := decodeCatalogue(gz)
		if err != nil {
			catalogueErr = fmt.Errorf("%s: %w", base, err)
			continue
		}
		if cat.Epoch != m.Epoch || cat.Seq != m.Seq {
			catalogueErr = fmt.Errorf("%s: catalogue %d-%d does not match manifest %d-%d", base, cat.Epoch, cat.Seq, m.Epoch, m.Seq)
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

	if catalogueErr != nil {
		lastErr = catalogueErr
	} else if staleBases > 0 && s.catalogueLoaded() {
		s.markSynced()
		return false, nil
	}
	if lastErr == nil {
		lastErr = ErrUnreachable
	} else {
		lastErr = fmt.Errorf("%w: %w", ErrUnreachable, lastErr)
	}
	s.markFailed(lastErr)
	return false, lastErr
}

func sameCatalogue(a, b *hubwire.Manifest) bool {
	return a.Epoch == b.Epoch && a.Seq == b.Seq && a.Catalogue.SHA256 == b.Catalogue.SHA256
}
