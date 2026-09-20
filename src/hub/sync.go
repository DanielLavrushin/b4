package hub

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

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

	changed, retry, err := s.syncOnce(ctx, trusted)
	if err == nil || !retry || ctx.Err() != nil {
		return changed, err
	}
	plain := !s.PlainMode()
	s.setPlainMode(plain)
	retried, _, retryErr := s.syncOnce(ctx, trusted)
	if retryErr != nil {
		s.setPlainMode(!plain)
		s.markFailed(err)
		return changed, err
	}
	if plain {
		log.Warnf("hub: the hub answered only with b4's own packet processing bypassed for the connection (%v); something in this router's sets or rules stalls b4's own connections, sync continues over the bypass", err)
	} else {
		log.Infof("hub: the hub answers through b4's own packet processing again")
	}
	return retried, nil
}

func transportFailure(err error) bool {
	var dnsErr *net.DNSError
	return !errors.As(err, &dnsErr)
}

func (s *Service) syncOnce(ctx context.Context, trusted []string) (bool, bool, error) {
	var lastErr, catalogueErr error
	var failures []string
	staleBases := 0
	retry := false
	fail := func(err error) {
		lastErr = err
		failures = append(failures, err.Error())
	}
	for _, base := range s.BaseURLs() {
		if ctx.Err() != nil {
			return false, false, ctx.Err()
		}
		if err := s.healthy(ctx, base); err != nil {
			fail(fmt.Errorf("%s did not answer: %w", base, err))
			retry = retry || transportFailure(err)
			continue
		}
		m, err := s.fetchManifest(ctx, base)
		if err != nil {
			fail(err)
			retry = retry || transportFailure(err)
			continue
		}
		if err := hubwire.VerifyManifest(m, trusted); err != nil {
			fail(fmt.Errorf("%s: %w", base, err))
			continue
		}
		stored := s.Manifest()
		if stored != nil && s.catalogueLoaded() && !m.Newer(stored) {
			if sameCatalogue(m, stored) {
				s.setPreferredBase(base)
				s.adoptManifest(m)
				s.markSynced(base)
				s.learnNetwork(ctx, base)
				return false, false, nil
			}
			staleBases++
			fail(fmt.Errorf("%s serves an older catalogue %d-%d than the stored %d-%d", base, m.Epoch, m.Seq, stored.Epoch, stored.Seq))
			log.Warnf("hub: %v, trying the next address", lastErr)
			continue
		}
		s.setPreferredBase(base)

		gz, err := s.fetchCatalogueFile(ctx, base, m.Catalogue)
		if err != nil {
			catalogueErr = err
			failures = append(failures, err.Error())
			retry = retry || transportFailure(err)
			continue
		}
		cat, err := decodeCatalogue(gz)
		if err != nil {
			catalogueErr = fmt.Errorf("%s: %w", base, err)
			failures = append(failures, catalogueErr.Error())
			continue
		}
		if cat.Epoch != m.Epoch || cat.Seq != m.Seq {
			catalogueErr = fmt.Errorf("%s: catalogue %d-%d does not match manifest %d-%d", base, cat.Epoch, cat.Seq, m.Epoch, m.Seq)
			failures = append(failures, catalogueErr.Error())
			continue
		}
		if err := s.store().save(m, gz); err != nil {
			err = fmt.Errorf("could not store the catalogue: %w", err)
			s.markFailed(err)
			return false, false, err
		}
		s.install(m, cat)
		s.adoptManifest(m)
		s.markSynced(base)
		log.Infof("hub: catalogue %d-%d with %d sets synced from %s", cat.Epoch, cat.Seq, len(cat.Sets), base)
		s.learnNetwork(ctx, base)
		return true, false, nil
	}

	if catalogueErr != nil {
		lastErr = catalogueErr
	} else if staleBases > 0 && s.catalogueLoaded() {
		s.markSynced("")
		return false, false, nil
	}
	if lastErr == nil {
		lastErr = ErrUnreachable
	} else {
		if errors.Is(lastErr, hubwire.ErrManifestSigner) {
			retry = false
		}
		others := make([]string, 0, len(failures))
		skipped := false
		for _, f := range failures {
			if !skipped && f == lastErr.Error() {
				skipped = true
				continue
			}
			others = append(others, f)
		}
		if len(others) > 0 {
			lastErr = fmt.Errorf("%w: %s; %w", ErrUnreachable, strings.Join(others, "; "), lastErr)
		} else {
			lastErr = fmt.Errorf("%w: %w", ErrUnreachable, lastErr)
		}
	}
	s.markFailed(lastErr)
	return false, retry, lastErr
}

func sameCatalogue(a, b *hubwire.Manifest) bool {
	return a.Epoch == b.Epoch && a.Seq == b.Seq && a.Catalogue.SHA256 == b.Catalogue.SHA256
}
