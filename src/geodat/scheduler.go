package geodat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

type StateFunc func() GeoDatConfig
type RefreshFunc func(ctx context.Context, destPath, siteURL, ipURL string) error
type PersistLastRunFunc func(ts string)

type Scheduler struct {
	getState    StateFunc
	refreshFunc RefreshFunc
	persistLast PersistLastRunFunc
	ctx         context.Context
	cancel      context.CancelFunc
	stop        chan struct{}
	stopped     chan struct{}
	mu          sync.Mutex
	lastFailure time.Time
	failures    int
}

const (
	startupDelay      = 45 * time.Second
	tickInterval      = 30 * time.Minute
	failureBackoff    = 2 * time.Hour
	maxFailureBackoff = 24 * time.Hour
)

var (
	startupTimeout = 5 * time.Minute
	startupRetry   = 60 * time.Second
)

func NewScheduler(getState StateFunc, refreshFunc RefreshFunc, persistLast PersistLastRunFunc) *Scheduler {
	return &Scheduler{getState: getState, refreshFunc: refreshFunc, persistLast: persistLast}
}

func (s *Scheduler) Start() {
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.stop = make(chan struct{})
	s.stopped = make(chan struct{})
	log.Infof("[GEODAT] scheduler starting")
	go s.run()
}

func (s *Scheduler) Stop() {
	if s.stop == nil {
		return
	}
	s.cancel()
	close(s.stop)
	<-s.stopped
	log.Infof("[GEODAT] scheduler stopped")
}

func (s *Scheduler) run() {
	defer close(s.stopped)

	select {
	case <-s.stop:
		return
	case <-time.After(startupDelay):
	}

	s.runStartup()

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.runScheduled()
			ticker.Reset(tickInterval)
		}
	}
}

func (s *Scheduler) runStartup() {
	st := s.getState()

	siteMissing := st.GeoSitePath != "" && st.GeoSiteURL != "" && needsDownload(st.GeoSitePath, true)
	ipMissing := st.GeoIpPath != "" && st.GeoIpURL != "" && needsDownload(st.GeoIpPath, true)
	forced := st.AutoUpdate.OnStartup && (st.GeoSiteURL != "" || st.GeoIpURL != "")

	if !siteMissing && !ipMissing && !forced {
		return
	}

	siteURL := ""
	ipURL := ""
	if forced || siteMissing {
		siteURL = st.GeoSiteURL
	}
	if forced || ipMissing {
		ipURL = st.GeoIpURL
	}
	if siteURL == "" && ipURL == "" {
		return
	}

	destPath := pickDestPath(st.GeoSitePath, st.GeoIpPath)
	if destPath == "" {
		log.Errorf("[GEODAT] startup refresh skipped: no destination directory derivable from configured paths")
		return
	}

	log.Infof("[GEODAT] startup refresh: dest=%s site=%v ip=%v forced=%v", destPath, siteURL != "", ipURL != "", forced)
	s.refreshWithRetry(destPath, siteURL, ipURL, startupTimeout)
}

func (s *Scheduler) refreshWithRetry(destPath, siteURL, ipURL string, timeout time.Duration) {
	full := s.coversAll(siteURL, ipURL)
	deadline := time.Now().Add(timeout)
	for {
		siteURL, ipURL = s.stillConfigured(siteURL, ipURL)
		if siteURL == "" && ipURL == "" {
			return
		}
		err := s.attempt(destPath, siteURL, ipURL)
		if err == nil {
			s.settle(nil, full)
			return
		}
		log.Errorf("[GEODAT] refresh failed: %v", err)
		if s.ctx.Err() != nil {
			return
		}
		siteURL, ipURL = retryable(err, siteURL, ipURL)
		if siteURL == "" && ipURL == "" || time.Now().After(deadline) {
			s.settle(err, full)
			if errors.Is(err, ErrUnusable) {
				log.Errorf("[GEODAT] the source did not return a usable file, not retrying before %s", s.lastFailure.Add(s.backoff()).Format(time.RFC3339))
			} else {
				log.Errorf("[GEODAT] giving up after %s", timeout)
			}
			return
		}
		select {
		case <-s.stop:
			return
		case <-time.After(startupRetry):
		}
	}
}

func (s *Scheduler) runScheduled() {
	st := s.getState()
	if st.GeoSiteURL == "" && st.GeoIpURL == "" {
		return
	}
	if !s.lastFailure.IsZero() && time.Since(s.lastFailure) < s.backoff() {
		return
	}

	siteURL, ipURL := "", ""
	reason := ""
	interval := intervalDuration(st.AutoUpdate.Interval)
	last := parseLastRun(st.AutoUpdate.LastRun)
	if interval != 0 && (last.IsZero() || time.Since(last) >= interval) {
		siteURL, ipURL = st.GeoSiteURL, st.GeoIpURL
		reason = "scheduled refresh (interval=" + st.AutoUpdate.Interval + ")"
	} else {
		if st.GeoSiteURL != "" && st.GeoSitePath != "" && needsDownload(st.GeoSitePath, false) {
			siteURL = st.GeoSiteURL
		}
		if st.GeoIpURL != "" && st.GeoIpPath != "" && needsDownload(st.GeoIpPath, false) {
			ipURL = st.GeoIpURL
		}
		reason = "refresh of a missing or damaged file"
	}
	if siteURL == "" && ipURL == "" {
		return
	}

	destPath := pickDestPath(st.GeoSitePath, st.GeoIpPath)
	if destPath == "" {
		return
	}

	log.Infof("[GEODAT] %s", reason)
	err := s.attempt(destPath, siteURL, ipURL)
	if err != nil {
		log.Errorf("[GEODAT] %s failed: %v", reason, err)
	}
	if s.ctx.Err() == nil {
		s.settle(err, s.coversAll(siteURL, ipURL))
	}
}

func (s *Scheduler) attempt(destPath, siteURL, ipURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.refreshFunc(s.ctx, destPath, siteURL, ipURL)
}

func (s *Scheduler) settle(err error, full bool) {
	if err == nil {
		s.lastFailure = time.Time{}
		s.failures = 0
		if full && s.persistLast != nil {
			s.persistLast(time.Now().UTC().Format(time.RFC3339))
		}
		return
	}
	if errors.Is(err, ErrBusy) {
		return
	}
	s.lastFailure = time.Now()
	s.failures++
}

func (s *Scheduler) coversAll(siteURL, ipURL string) bool {
	st := s.getState()
	return siteURL == st.GeoSiteURL && ipURL == st.GeoIpURL
}

func (s *Scheduler) stillConfigured(siteURL, ipURL string) (string, string) {
	st := s.getState()
	if siteURL != st.GeoSiteURL {
		siteURL = ""
	}
	if ipURL != st.GeoIpURL {
		ipURL = ""
	}
	return siteURL, ipURL
}

func retryable(err error, siteURL, ipURL string) (string, string) {
	failures := downloadFailures(err)
	if len(failures) == 0 {
		if errors.Is(err, ErrUnusable) {
			return "", ""
		}
		return siteURL, ipURL
	}
	site, ip := "", ""
	for _, failure := range failures {
		if errors.Is(failure, ErrUnusable) {
			continue
		}
		if failure.Kind == KindIP {
			ip = ipURL
		} else {
			site = siteURL
		}
	}
	return site, ip
}

func downloadFailures(err error) []*DownloadError {
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		var out []*DownloadError
		for _, e := range joined.Unwrap() {
			out = append(out, downloadFailures(e)...)
		}
		return out
	}
	var failure *DownloadError
	if errors.As(err, &failure) {
		return []*DownloadError{failure}
	}
	return nil
}

func (s *Scheduler) backoff() time.Duration {
	d := failureBackoff
	for i := 1; i < s.failures && d < maxFailureBackoff; i++ {
		d *= 2
	}
	return min(d, maxFailureBackoff)
}

func intervalDuration(v string) time.Duration {
	switch v {
	case "daily":
		return 24 * time.Hour
	case "weekly":
		return 7 * 24 * time.Hour
	case "monthly":
		return 30 * 24 * time.Hour
	default:
		return 0
	}
}

func parseLastRun(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func needsDownload(p string, report bool) bool {
	info, err := os.Stat(p)
	if err != nil {
		return true
	}
	if info.Size() == 0 || IsDamaged(p) {
		if report {
			log.Errorf("[GEODAT] %s is damaged, downloading it again", p)
		}
		return true
	}
	return false
}

func pickDestPath(sitePath, ipPath string) string {
	for _, p := range []string{sitePath, ipPath} {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			log.Warnf("[GEODAT] ignoring non-absolute geo path: %q", p)
			continue
		}
		return filepath.Dir(p)
	}
	return ""
}
