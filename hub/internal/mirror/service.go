package mirror

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
)

const (
	DefaultRefresh   = 5 * time.Minute
	AnnounceInterval = 24 * time.Hour
	ManifestLimit    = 1 << 20
	CatalogueLimit   = 32 << 20
	BlobLimit        = 4 << 20
	fetchTimeout     = 2 * time.Minute
)

type Options struct {
	Upstream    string
	TrustedKeys []string
	Layout      hubdata.Layout
	PublicURL   string
	Identity    *hubwire.Identity
	Announce    bool
	Version     string
	Refresh     time.Duration
	Client      *http.Client
	Now         func() time.Time
}

type Status struct {
	Upstream     string
	PublicURL    string
	Manifest     *hubwire.Manifest
	Sets         int
	LastRefresh  time.Time
	LastError    string
	LastAnnounce time.Time
	AnnounceNote string
	Queued       int
	Version      string
}

type Service struct {
	opts  Options
	relay *Relay

	mu     sync.Mutex
	status Status
}

func New(opts Options) (*Service, error) {
	opts.Upstream = strings.TrimRight(strings.TrimSpace(opts.Upstream), "/")
	if opts.Upstream == "" {
		return nil, errors.New("upstream url is required")
	}
	if opts.Refresh <= 0 {
		opts.Refresh = DefaultRefresh
	}
	if opts.Client == nil {
		opts.Client = &http.Client{}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if len(opts.TrustedKeys) == 0 {
		opts.TrustedKeys = hubwire.BuiltinHubKeys
	}
	if opts.Announce && (opts.Identity == nil || opts.PublicURL == "") {
		return nil, errors.New("announcing needs an identity and a public url")
	}
	if err := opts.Layout.EnsureDirs(); err != nil {
		return nil, err
	}
	s := &Service{opts: opts}
	s.relay = &Relay{Upstream: opts.Upstream, Dir: filepath.Join(opts.Layout.Root, RelayDir), Client: opts.Client, Now: opts.Now}
	s.status = Status{Upstream: opts.Upstream, PublicURL: opts.PublicURL, Version: opts.Version, Queued: s.relay.Queued()}
	if result, err := catalogue.ReadPublished(opts.Layout.Public()); err == nil {
		s.status.Manifest = result.Manifest
		s.status.Sets = len(result.Catalogue.Sets)
	} else if !errors.Is(err, catalogue.ErrNotPublished) {
		log.Printf("mirror: stored catalogue ignored: %v", err)
	}
	return s, nil
}

func (s *Service) Relay() *Relay {
	return s.relay
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

func (s *Service) current() *hubwire.Manifest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status.Manifest
}

func (s *Service) get(ctx context.Context, url string, limit int64) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, fetchTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "b4hub-mirror")
	resp, err := s.opts.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %d", url, resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > limit {
		return nil, fmt.Errorf("%s exceeds %d bytes", url, limit)
	}
	return body, nil
}

func (s *Service) Refresh(ctx context.Context) error {
	err := s.refresh(ctx)
	s.mu.Lock()
	s.status.LastRefresh = s.opts.Now().UTC()
	if err != nil {
		s.status.LastError = err.Error()
	} else {
		s.status.LastError = ""
	}
	s.mu.Unlock()
	return err
}

func (s *Service) refresh(ctx context.Context) error {
	raw, err := s.get(ctx, s.opts.Upstream+hubwire.PathManifest, ManifestLimit)
	if err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	var m hubwire.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("manifest does not decode: %w", err)
	}
	if err := hubwire.VerifyManifest(&m, s.opts.TrustedKeys); err != nil {
		return fmt.Errorf("manifest: %w", err)
	}
	if !catalogue.ValidFileName(m.Catalogue.File) {
		return fmt.Errorf("manifest names %q", m.Catalogue.File)
	}
	if !m.Newer(s.current()) {
		return nil
	}
	if m.Catalogue.Size > CatalogueLimit {
		return fmt.Errorf("catalogue %s is %d bytes, over the %d limit", m.Catalogue.File, m.Catalogue.Size, CatalogueLimit)
	}
	gz, err := s.get(ctx, s.opts.Upstream+hubwire.PathFiles+m.Catalogue.File, CatalogueLimit)
	if err != nil {
		return fmt.Errorf("catalogue: %w", err)
	}
	if int64(len(gz)) != m.Catalogue.Size {
		return fmt.Errorf("catalogue %s is %d bytes, the manifest says %d", m.Catalogue.File, len(gz), m.Catalogue.Size)
	}
	if hubwire.BlobHash(gz) != m.Catalogue.SHA256 {
		return fmt.Errorf("catalogue %s does not match the manifest hash", m.Catalogue.File)
	}
	cat, err := catalogue.Decode(gz)
	if err != nil {
		return err
	}
	blobs := s.opts.Layout.Blobs()
	for _, ref := range cat.Blobs {
		if !hubdata.ValidBlobHash(ref.SHA256) {
			return fmt.Errorf("catalogue lists a malformed blob hash %q", ref.SHA256)
		}
		if blobs.Exists(ref.SHA256) {
			continue
		}
		data, err := s.get(ctx, s.opts.Upstream+hubwire.PathBlob+ref.SHA256, BlobLimit)
		if err != nil {
			return fmt.Errorf("blob %s: %w", ref.SHA256[:12], err)
		}
		if hubwire.BlobHash(data) != ref.SHA256 {
			return fmt.Errorf("blob %s does not match its sha256", ref.SHA256[:12])
		}
		if _, err := blobs.Put(data); err != nil {
			return err
		}
	}
	public := s.opts.Layout.Public()
	if err := hubdata.WriteFileAtomic(filepath.Join(public, m.Catalogue.File), gz, 0o644); err != nil {
		return err
	}
	if err := hubdata.WriteFileAtomic(filepath.Join(public, catalogue.ManifestFile), raw, 0o644); err != nil {
		return err
	}
	catalogue.Prune(public, m.Catalogue.File, catalogue.DefaultKeep)
	s.mu.Lock()
	s.status.Manifest = &m
	s.status.Sets = len(cat.Sets)
	s.mu.Unlock()
	log.Printf("mirror: copied %s with %d sets and %d blobs from %s", m.Catalogue.File, len(cat.Sets), len(cat.Blobs), s.opts.Upstream)
	return nil
}

func (s *Service) Announce(ctx context.Context) error {
	if !s.opts.Announce {
		return nil
	}
	rec, err := hubwire.SignRecord(s.opts.Identity, hubwire.RecordMirror, hubwire.MirrorBody{URL: s.opts.PublicURL, Version: s.opts.Version}, s.opts.Now())
	if err != nil {
		return err
	}
	raw, err := json.Marshal(rec)
	if err != nil {
		return err
	}
	answer, err := s.relay.Forward(ctx, raw)
	note := ""
	if err != nil {
		note = "failed: " + err.Error()
	} else {
		note = fmt.Sprintf("%d %s", answer.Status, strings.TrimSpace(string(answer.Body)))
	}
	s.mu.Lock()
	s.status.LastAnnounce = s.opts.Now().UTC()
	s.status.AnnounceNote = note
	s.mu.Unlock()
	log.Printf("mirror: announced %s to %s: %s", s.opts.PublicURL, s.opts.Upstream, note)
	return err
}

func (s *Service) tick(ctx context.Context) {
	if err := s.Refresh(ctx); err != nil {
		log.Printf("mirror: refresh: %v", err)
	}
	if err := s.relay.Retry(ctx); err != nil {
		log.Printf("mirror: relay: %v", err)
	}
	s.mu.Lock()
	s.status.Queued = s.relay.Queued()
	s.mu.Unlock()
}

func (s *Service) Run(ctx context.Context) {
	s.tick(ctx)
	if s.opts.Announce {
		_ = s.Announce(ctx)
	}
	refresh := time.NewTicker(s.opts.Refresh)
	defer refresh.Stop()
	announce := time.NewTicker(AnnounceInterval)
	defer announce.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-refresh.C:
			s.tick(ctx)
		case <-announce.C:
			if s.opts.Announce {
				_ = s.Announce(ctx)
			}
		}
	}
}
