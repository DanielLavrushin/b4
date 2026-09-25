package mtproto

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	TelegramCIDRURL        = "https://core.telegram.org/resources/cidr.txt"
	TelegramCIDRMirrorURL  = "https://proxy.b4core.app/telegram/cidr.txt"
	TelegramCIDRMirror2URL = "https://proxy2.b4core.app/telegram/cidr.txt"
	TelegramCIDRCacheFile  = "telegram_cidr.txt"

	telegramCIDRGeoIPCategory = "telegram"
	telegramCIDRMaxBody       = 64 << 10
	telegramCIDRTimeout       = 10 * time.Second
	telegramCIDRInterval      = 24 * time.Hour
	telegramCIDRRetryBase     = 30 * time.Second
	telegramCIDRRetryMax      = time.Hour
)

type telegramCIDRSource struct {
	url    string
	source string
}

var telegramCIDRSources = []telegramCIDRSource{
	{TelegramCIDRURL, config.TelegramCIDRSourceTelegram},
	{TelegramCIDRMirrorURL, config.TelegramCIDRSourceMirror},
	{TelegramCIDRMirror2URL, config.TelegramCIDRSourceMirror},
}

type TelegramCIDRStatus struct {
	Source      string `json:"source"`
	Total       int    `json:"total"`
	V4          int    `json:"v4"`
	V6          int    `json:"v6"`
	UpdatedAt   string `json:"updated_at,omitempty"`
	LastAttempt string `json:"last_attempt,omitempty"`
	LastError   string `json:"last_error,omitempty"`
}

var (
	telegramCIDRMu          sync.Mutex
	telegramCIDRFetchMu     sync.Mutex
	telegramCIDRLastAttempt time.Time
	telegramCIDRLastErr     string
	telegramCIDROnChange    func()
	telegramCIDRTrigger     = make(chan struct{}, 1)
	telegramCIDRLocalDone   bool
	telegramCIDRLocalGeoIP  string
)

func TelegramCIDRState() TelegramCIDRStatus {
	cur := config.CurrentTelegramCIDRs()
	st := TelegramCIDRStatus{Source: cur.Source, Total: len(cur.All), V4: cur.V4, V6: cur.V6}
	if !cur.UpdatedAt.IsZero() {
		st.UpdatedAt = cur.UpdatedAt.UTC().Format(time.RFC3339)
	}
	telegramCIDRMu.Lock()
	defer telegramCIDRMu.Unlock()
	if !telegramCIDRLastAttempt.IsZero() {
		st.LastAttempt = telegramCIDRLastAttempt.UTC().Format(time.RFC3339)
	}
	st.LastError = telegramCIDRLastErr
	return st
}

func telegramCIDRCachePath(cfg *config.Config) string {
	if cfg == nil || cfg.ConfigPath == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cfg.ConfigPath), TelegramCIDRCacheFile)
}

func LoadLocalTelegramCIDRs(cfg *config.Config) bool {
	telegramCIDRMu.Lock()
	telegramCIDRLocalDone = true
	telegramCIDRLocalGeoIP = ""
	if cfg != nil {
		telegramCIDRLocalGeoIP = cfg.System.Geo.GeoIpPath
	}
	telegramCIDRMu.Unlock()
	if source, at, prefixes, ok := readTelegramCIDRCache(telegramCIDRCachePath(cfg)); ok {
		_, changed := config.SetTelegramCIDRs(source, at, prefixes)
		log.Infof("Telegram bridge: using %d address ranges from the saved copy (%s, %s)", len(prefixes), source, at.UTC().Format(time.RFC3339))
		return changed
	}
	if prefixes := geoIPTelegramCIDRs(cfg); len(prefixes) > 0 {
		_, changed := config.SetTelegramCIDRs(config.TelegramCIDRSourceGeoIP, time.Now(), prefixes)
		log.Infof("Telegram bridge: using %d address ranges from the GeoIP '%s' category", len(prefixes), telegramCIDRGeoIPCategory)
		return changed
	}
	_, changed := config.SetTelegramCIDRs(config.TelegramCIDRSourceBuiltin, time.Time{}, nil)
	return changed
}

func geoIPTelegramCIDRs(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	path := cfg.System.Geo.GeoIpPath
	if path == "" {
		return nil
	}
	if _, err := os.Stat(path); err != nil {
		return nil
	}
	ips, err := geodat.LoadIpsFromCategories(path, []string{telegramCIDRGeoIPCategory})
	if err != nil {
		log.Warnf("Telegram bridge: cannot read the GeoIP '%s' category from %s: %v", telegramCIDRGeoIPCategory, path, err)
		return nil
	}
	return config.NormalizeTelegramCIDRs(ips)
}

func readTelegramCIDRCache(path string) (string, time.Time, []string, bool) {
	if path == "" {
		return "", time.Time{}, nil, false
	}
	f, err := os.Open(path)
	if err != nil {
		return "", time.Time{}, nil, false
	}
	defer f.Close()
	source := config.TelegramCIDRSourceCache
	var at time.Time
	var raw []string
	sc := bufio.NewScanner(io.LimitReader(f, telegramCIDRMaxBody))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			for _, field := range strings.Fields(strings.TrimPrefix(line, "#")) {
				k, v, ok := strings.Cut(field, "=")
				if !ok || k != "fetched" {
					continue
				}
				if t, err := time.Parse(time.RFC3339, v); err == nil {
					at = t
				}
			}
			continue
		}
		raw = append(raw, line)
	}
	prefixes := config.NormalizeTelegramCIDRs(raw)
	if len(prefixes) == 0 {
		return "", time.Time{}, nil, false
	}
	if at.IsZero() {
		if fi, err := f.Stat(); err == nil {
			at = fi.ModTime()
		}
	}
	return source, at, prefixes, true
}

func writeTelegramCIDRCache(path, source string, at time.Time, prefixes []string) error {
	if path == "" {
		return nil
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Telegram address ranges used by the b4 Telegram bridge\n# from=%s fetched=%s\n", source, at.UTC().Format(time.RFC3339))
	for _, p := range prefixes {
		b.WriteString(p)
		b.WriteByte('\n')
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var telegramCIDRHTTPClient = func() *http.Client {
	return netprobe.HTTPClient(int(selfDialMark()), telegramCIDRTimeout)
}

func downloadTelegramCIDRs(ctx context.Context, target string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "b4-mtproto")
	resp, err := telegramCIDRHTTPClient().Do(req)
	if err != nil {
		var ue *url.Error
		if errors.As(err, &ue) {
			return nil, ue.Err
		}
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, telegramCIDRMaxBody))
	if err != nil {
		return nil, err
	}
	var raw []string
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		raw = append(raw, line)
	}
	prefixes := config.NormalizeTelegramCIDRs(raw)
	if len(prefixes) == 0 {
		return nil, errors.New("no usable address ranges in the response")
	}
	if dropped := len(raw) - len(prefixes); dropped > 0 {
		log.Warnf("Telegram bridge: %d of %d lines from %s were not usable address ranges and were skipped", dropped, len(raw), target)
	}
	return prefixes, nil
}

func FetchTelegramCIDRs(ctx context.Context, cfg *config.Config) (bool, error) {
	telegramCIDRFetchMu.Lock()
	defer telegramCIDRFetchMu.Unlock()

	var errs []string
	for _, src := range telegramCIDRSources {
		prefixes, err := downloadTelegramCIDRs(ctx, src.url)
		if err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", src.url, err))
			continue
		}
		now := time.Now()
		_, changed := config.SetTelegramCIDRs(src.source, now, prefixes)
		if werr := writeTelegramCIDRCache(telegramCIDRCachePath(cfg), src.source, now, prefixes); werr != nil {
			log.Warnf("Telegram bridge: cannot save the address list: %v", werr)
		}
		telegramCIDRMu.Lock()
		telegramCIDRLastAttempt = now
		telegramCIDRLastErr = ""
		telegramCIDRMu.Unlock()
		log.Infof("Telegram bridge: loaded %d address ranges from %s", len(prefixes), src.url)
		return changed, nil
	}
	msg := strings.Join(errs, "; ")
	if ctx.Err() != nil {
		return false, ctx.Err()
	}
	telegramCIDRMu.Lock()
	telegramCIDRLastAttempt = time.Now()
	telegramCIDRLastErr = msg
	telegramCIDRMu.Unlock()
	return false, errors.New(msg)
}

func telegramCIDRDownloaded() bool {
	switch config.CurrentTelegramCIDRs().Source {
	case config.TelegramCIDRSourceTelegram, config.TelegramCIDRSourceMirror, config.TelegramCIDRSourceCache:
		return true
	}
	return false
}

func telegramCIDRLocalStale(cfg *config.Config) bool {
	if telegramCIDRDownloaded() {
		return false
	}
	telegramCIDRMu.Lock()
	defer telegramCIDRMu.Unlock()
	return !telegramCIDRLocalDone || (cfg != nil && cfg.System.Geo.GeoIpPath != telegramCIDRLocalGeoIP)
}

func RefreshTelegramCIDRsNow(ctx context.Context, cfg *config.Config) error {
	changed := false
	if telegramCIDRLocalStale(cfg) {
		changed = LoadLocalTelegramCIDRs(cfg)
	}
	fetched, err := FetchTelegramCIDRs(ctx, cfg)
	if (changed || fetched) && telegramCIDROnChange != nil {
		telegramCIDROnChange()
	}
	return err
}

func TriggerTelegramCIDRRefresh() {
	select {
	case telegramCIDRTrigger <- struct{}{}:
	default:
	}
}

func StartTelegramCIDRRefresh(ctx context.Context, getCfg func() *config.Config, onChange func()) {
	telegramCIDROnChange = onChange
	go runTelegramCIDRRefresh(ctx, getCfg)
	TriggerTelegramCIDRRefresh()
}

func telegramCIDRRetryDelay(failures int) time.Duration {
	d := telegramCIDRRetryBase
	for i := 1; i < failures && d < telegramCIDRRetryMax; i++ {
		d *= 2
	}
	if d > telegramCIDRRetryMax {
		d = telegramCIDRRetryMax
	}
	return d
}

func runTelegramCIDRRefresh(ctx context.Context, getCfg func() *config.Config) {
	timer := time.NewTimer(telegramCIDRInterval)
	timer.Stop()
	defer timer.Stop()
	failures := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-telegramCIDRTrigger:
		case <-timer.C:
		}
		cfg := getCfg()
		if !cfg.TelegramBridgeEnabled() {
			timer.Stop()
			continue
		}
		fetchCtx, cancel := context.WithTimeout(ctx, time.Duration(len(telegramCIDRSources)+1)*telegramCIDRTimeout)
		err := RefreshTelegramCIDRsNow(fetchCtx, cfg)
		cancel()
		next := telegramCIDRInterval
		if err != nil {
			failures++
			next = telegramCIDRRetryDelay(failures)
			if failures == 1 {
				log.Warnf("Telegram bridge: cannot download the address list, keeping %d ranges from '%s', next attempt in %v: %v",
					len(config.CurrentTelegramCIDRs().All), config.CurrentTelegramCIDRs().Source, next, err)
			} else {
				log.Tracef("Telegram bridge: address list download failed again (%d), next attempt in %v: %v", failures, next, err)
			}
		} else {
			failures = 0
		}
		timer.Stop()
		timer.Reset(next)
	}
}
