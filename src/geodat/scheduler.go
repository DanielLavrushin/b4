package geodat

import (
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

type StateFunc func() GeoDatConfig
type RefreshFunc func(destPath, siteURL, ipURL string) error
type PersistLastRunFunc func(ts string)

type Scheduler struct {
	getState    StateFunc
	refreshFunc RefreshFunc
	persistLast PersistLastRunFunc
	stop        chan struct{}
	stopped     chan struct{}
	mu          sync.Mutex
}

const (
	startupDelay   = 45 * time.Second
	startupTimeout = 5 * time.Minute
	startupRetry   = 60 * time.Second
	tickInterval   = 30 * time.Minute
)

func NewScheduler(getState StateFunc, refreshFunc RefreshFunc, persistLast PersistLastRunFunc) *Scheduler {
	return &Scheduler{getState: getState, refreshFunc: refreshFunc, persistLast: persistLast}
}

func (s *Scheduler) Start() {
	s.stop = make(chan struct{})
	s.stopped = make(chan struct{})
	log.Infof("[GEODAT] scheduler starting")
	go s.run()
}

func (s *Scheduler) Stop() {
	if s.stop == nil {
		return
	}
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
		}
	}
}

func (s *Scheduler) runStartup() {
	st := s.getState()

	siteMissing := st.GeoSitePath != "" && st.GeoSiteURL != "" && !fileExists(st.GeoSitePath)
	ipMissing := st.GeoIpPath != "" && st.GeoIpURL != "" && !fileExists(st.GeoIpPath)
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
	interval := intervalDuration(st.AutoUpdate.Interval)
	if interval == 0 {
		return
	}
	if st.GeoSiteURL == "" && st.GeoIpURL == "" {
		return
	}

	last := parseLastRun(st.AutoUpdate.LastRun)
	if !last.IsZero() && time.Since(last) < interval {
		return
	}

	destPath := pickDestPath(st.GeoSitePath, st.GeoIpPath)
	if destPath == "" {
		return
	}

	log.Infof("[GEODAT] scheduled refresh (interval=%s)", st.AutoUpdate.Interval)
	if err := s.refresh(destPath, st.GeoSiteURL, st.GeoIpURL); err != nil {
		log.Errorf("[GEODAT] scheduled refresh failed: %v", err)
	}
}

func (s *Scheduler) refresh(destPath, siteURL, ipURL string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.refreshFunc(destPath, siteURL, ipURL); err != nil {
		return err
	}
	if s.persistLast != nil {
		s.persistLast(time.Now().UTC().Format(time.RFC3339))
	}
	return nil
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

func fileExists(p string) bool {
	if p == "" {
		return false
	}
	_, err := os.Stat(p)
	return err == nil
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
