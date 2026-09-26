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
	startupTimeout    = 5 * time.Minute
	startupRetry      = 60 * time.Second
	tickInterval      = 30 * time.Minute
	failureBackoff    = 2 * time.Hour
	maxFailureBackoff = 24 * time.Hour
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

	siteMissing := st.GeoSitePath != "" && st.GeoSiteURL != "" && needsDownload(st.GeoSitePath)
	ipMissing := st.GeoIpPath != "" && st.GeoIpURL != "" && needsDownload(st.GeoIpPath)
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
	deadline := time.Now().Add(timeout)
	for {
		err := s.refresh(destPath, siteURL, ipURL)
		if err == nil {
			return
		}
		log.Errorf("[GEODAT] refresh failed: %v", err)
		if s.ctx.Err() != nil {
			return
		}
		if errors.Is(err, ErrUnusable) {
			log.Errorf("[GEODAT] the source did not return a usable file, not retrying before %s", s.lastFailure.Add(s.backoff()).Format(time.RFC3339))
			return
		}
		if time.Now().After(deadline) {
			log.Errorf("[GEODAT] giving up after %s", timeout)
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

	siteURL, ipURL := "", ""
	reason := ""
	interval := intervalDuration(st.AutoUpdate.Interval)
	last := parseLastRun(st.AutoUpdate.LastRun)
	if interval != 0 && (last.IsZero() || time.Since(last) >= interval) {
		siteURL, ipURL = st.GeoSiteURL, st.GeoIpURL
		reason = "scheduled refresh (interval=" + st.AutoUpdate.Interval + ")"
	} else {
		if st.GeoSiteURL != "" && st.GeoSitePath != "" && IsDamaged(st.GeoSitePath) {
			siteURL = st.GeoSiteURL
		}
		if st.GeoIpURL != "" && st.GeoIpPath != "" && IsDamaged(st.GeoIpPath) {
			ipURL = st.GeoIpURL
		}
		reason = "damaged file refresh"
	}
	if siteURL == "" && ipURL == "" {
		return
	}
	if !s.lastFailure.IsZero() && time.Since(s.lastFailure) < s.backoff() {
		return
	}

	destPath := pickDestPath(st.GeoSitePath, st.GeoIpPath)
	if destPath == "" {
		return
	}

	log.Infof("[GEODAT] %s", reason)
	if err := s.refresh(destPath, siteURL, ipURL); err != nil {
		log.Errorf("[GEODAT] %s failed: %v", reason, err)
	}
}

func (s *Scheduler) refresh(destPath, siteURL, ipURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshFunc(s.ctx, destPath, siteURL, ipURL); err != nil {
		s.lastFailure = time.Now()
		s.failures++
		return err
	}
	s.lastFailure = time.Time{}
	s.failures = 0
	if s.persistLast != nil {
		s.persistLast(time.Now().UTC().Format(time.RFC3339))
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

func needsDownload(p string) bool {
	info, err := os.Stat(p)
	if err != nil {
		return true
	}
	if info.Size() == 0 || IsDamaged(p) {
		log.Errorf("[GEODAT] %s is damaged, downloading it again", p)
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
