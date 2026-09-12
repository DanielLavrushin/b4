package geo

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
)

const (
	DefaultGeoSiteURL = "https://github.com/Loyalsoldier/v2ray-rules-dat/releases/latest/download/geosite.dat"
	DefaultGeoIPURL   = "https://github.com/DanielLavrushin/b4geoip/releases/latest/download/geoip.dat"

	GeoSiteFile = "geosite.dat"
	GeoIPFile   = "geoip.dat"
	StateFile   = "state.json"

	RefreshInterval = 24 * time.Hour
	MaxFileBytes    = 512 << 20
	downloadTimeout = 10 * time.Minute
)

type Options struct {
	Dir        string
	GeoSiteURL string
	GeoIPURL   string
	Client     *http.Client
	Now        func() time.Time
}

type FileStatus struct {
	Name      string    `json:"name"`
	URL       string    `json:"url"`
	FinalURL  string    `json:"final_url,omitempty"`
	SHA256    string    `json:"sha256,omitempty"`
	Size      int64     `json:"size,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
	Error     string    `json:"error,omitempty"`
}

type Service struct {
	opts   Options
	mu     sync.Mutex
	status map[string]FileStatus
}

func New(opts Options) *Service {
	if opts.GeoSiteURL == "" {
		opts.GeoSiteURL = DefaultGeoSiteURL
	}
	if opts.GeoIPURL == "" {
		opts.GeoIPURL = DefaultGeoIPURL
	}
	if opts.Client == nil {
		opts.Client = &http.Client{Timeout: downloadTimeout}
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	s := &Service{opts: opts, status: make(map[string]FileStatus)}
	s.loadState()
	return s
}

func (s *Service) GeoSitePath() string { return filepath.Join(s.opts.Dir, GeoSiteFile) }
func (s *Service) GeoIPPath() string   { return filepath.Join(s.opts.Dir, GeoIPFile) }

func (s *Service) Sources() []hubwire.GeoSource {
	return []hubwire.GeoSource{{SiteURL: s.opts.GeoSiteURL, IPURL: s.opts.GeoIPURL}}
}

func (s *Service) Status() []FileStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]FileStatus, 0, len(s.status))
	for _, name := range []string{GeoSiteFile, GeoIPFile} {
		if st, ok := s.status[name]; ok {
			out = append(out, st)
		}
	}
	return out
}

func (s *Service) statePath() string {
	return filepath.Join(s.opts.Dir, StateFile)
}

func (s *Service) loadState() {
	raw, err := os.ReadFile(s.statePath())
	if err != nil {
		return
	}
	var saved map[string]FileStatus
	if err := json.Unmarshal(raw, &saved); err != nil {
		return
	}
	s.mu.Lock()
	for name, st := range saved {
		s.status[name] = st
	}
	s.mu.Unlock()
}

func (s *Service) saveState() {
	s.mu.Lock()
	raw, err := json.MarshalIndent(s.status, "", "  ")
	s.mu.Unlock()
	if err != nil {
		return
	}
	_ = hubdata.WriteFileAtomic(s.statePath(), raw, 0o644)
}

func (s *Service) record(st FileStatus) {
	s.mu.Lock()
	s.status[st.Name] = st
	s.mu.Unlock()
	s.saveState()
}

func (s *Service) Refresh(ctx context.Context) error {
	if err := os.MkdirAll(s.opts.Dir, 0o755); err != nil {
		return err
	}
	var failures []error
	for _, target := range []struct{ name, url string }{{GeoSiteFile, s.opts.GeoSiteURL}, {GeoIPFile, s.opts.GeoIPURL}} {
		st := FileStatus{Name: target.name, URL: target.url}
		s.mu.Lock()
		previous, had := s.status[target.name]
		s.mu.Unlock()
		if had {
			st = previous
			st.URL = target.url
		}
		finalURL, sum, size, err := s.download(ctx, target.url, filepath.Join(s.opts.Dir, target.name))
		if err != nil {
			st.Error = err.Error()
			failures = append(failures, fmt.Errorf("%s: %w", target.name, err))
		} else {
			st.FinalURL = finalURL
			st.SHA256 = sum
			st.Size = size
			st.FetchedAt = s.opts.Now().UTC()
			st.Error = ""
		}
		s.record(st)
	}
	return errors.Join(failures...)
}

func (s *Service) download(ctx context.Context, url, dest string) (finalURL, sum string, size int64, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return "", "", 0, err
	}
	req.Header.Set("User-Agent", "b4hub")
	resp, err := s.opts.Client.Do(req)
	if err != nil {
		return "", "", 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", "", 0, fmt.Errorf("%s returned %d", url, resp.StatusCode)
	}
	finalURL = resp.Request.URL.String()
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".*")
	if err != nil {
		return "", "", 0, err
	}
	tmpName := tmp.Name()
	discard := func(err error) (string, string, int64, error) {
		tmp.Close()
		os.Remove(tmpName)
		return "", "", 0, err
	}
	hasher := sha256.New()
	size, err = io.Copy(io.MultiWriter(tmp, hasher), io.LimitReader(resp.Body, MaxFileBytes+1))
	if err != nil {
		return discard(err)
	}
	if size > MaxFileBytes {
		return discard(fmt.Errorf("%s exceeds %d bytes", url, MaxFileBytes))
	}
	if size == 0 {
		return discard(fmt.Errorf("%s is empty", url))
	}
	if err := tmp.Chmod(0o644); err != nil {
		return discard(err)
	}
	if err := tmp.Sync(); err != nil {
		return discard(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return "", "", 0, err
	}
	if err := os.Rename(tmpName, dest); err != nil {
		os.Remove(tmpName)
		return "", "", 0, err
	}
	return finalURL, hex.EncodeToString(hasher.Sum(nil)), size, nil
}

func (s *Service) Fresh() bool {
	now := s.opts.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, name := range []string{GeoSiteFile, GeoIPFile} {
		st, ok := s.status[name]
		if !ok || st.Error != "" || st.FetchedAt.IsZero() || now.Sub(st.FetchedAt) >= RefreshInterval {
			return false
		}
		if _, err := os.Stat(filepath.Join(s.opts.Dir, name)); err != nil {
			return false
		}
	}
	return true
}

func (s *Service) RunDaily(ctx context.Context) {
	if s.Fresh() {
		log.Printf("geo: files fetched within the last day, next refresh in %s", RefreshInterval)
	} else {
		s.refreshAndLog(ctx)
	}
	ticker := time.NewTicker(RefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.refreshAndLog(ctx)
		}
	}
}

func (s *Service) refreshAndLog(ctx context.Context) {
	if err := s.Refresh(ctx); err != nil {
		log.Printf("geo: refresh incomplete: %v", err)
		return
	}
	for _, st := range s.Status() {
		log.Printf("geo: %s %d bytes sha256 %s from %s", st.Name, st.Size, st.SHA256, st.FinalURL)
	}
}
