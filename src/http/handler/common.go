package handler

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptrace"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/ai"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/geodat"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/mtproto"
	"github.com/daniellavrushin/b4/nfq"
	b4tun "github.com/daniellavrushin/b4/tun"
	"github.com/daniellavrushin/b4/utils"
	"github.com/daniellavrushin/b4/watchdog"
	"golang.org/x/sys/unix"
)

// These variables are set at build time via ldflags
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

type ConfigRefresher interface {
	UpdateConfig(newCfg *config.Config)
}

var (
	globalPool          *nfq.Pool
	globalSocks5Server  ConfigRefresher
	globalMTProtoServer ConfigRefresher
	globalMTProtoBridge ConfigRefresher
	tablesRefreshFunc   func() error
	routingSyncFunc     func(*config.Config)
	upstreamHealthFunc  func() []DiagUpstream
	discoveryRuntime    *discovery.Runtime
	globalWatchdog      *watchdog.Watchdog
	globalAIManager     *ai.Manager
	globalTUNEngine     *b4tun.Engine
)

func SetTUNEngine(e *b4tun.Engine) {
	globalTUNEngine = e
}

func SetAIManager(m *ai.Manager) {
	globalAIManager = m
}

func GetAIManager() *ai.Manager {
	return globalAIManager
}

func setJsonHeader(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
}

func writeJsonError(w http.ResponseWriter, status int, message string) {
	setJsonHeader(w)
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]string{"error": message})
}

func SetNFQPool(pool *nfq.Pool) {
	globalPool = pool
}

func SetSocks5Server(s ConfigRefresher) {
	globalSocks5Server = s
}

func SetMTProtoServer(s ConfigRefresher) {
	globalMTProtoServer = s
	if p, ok := s.(interface{ Stats() mtproto.Stats }); ok {
		metrics.SetMTProtoStatsProvider(func() *metrics.MTProtoStats {
			return mtprotoStatsSnapshot(p.Stats())
		})
	}
}

func mtprotoStatsSnapshot(st mtproto.Stats) *metrics.MTProtoStats {
	out := &metrics.MTProtoStats{
		Enabled:           st.Enabled,
		Port:              st.Port,
		Networks:          st.Networks,
		ActiveConnections: st.ActiveConnections,
		TotalConnections:  st.TotalConnections,
		BytesUp:           st.BytesUp,
		BytesDown:         st.BytesDown,
	}
	for _, sec := range st.Secrets {
		out.Secrets = append(out.Secrets, metrics.MTProtoSecretStat{
			Name:         sec.Name,
			Active:       sec.Active,
			Total:        sec.Total,
			BytesUp:      sec.BytesUp,
			BytesDown:    sec.BytesDown,
			Networks:     sec.Networks,
			NetworkAddrs: sec.NetworkAddrs,
		})
	}
	return out
}

func SetMTProtoBridge(s ConfigRefresher) {
	globalMTProtoBridge = s
}

func NewAPIHandler(cfgPtr *atomic.Pointer[config.Config]) *API {
	cfg := cfgPtr.Load()
	// Initialize geodata manager
	geodataManager := geodat.NewGeodataManager(cfg.System.Geo.GeoSitePath, cfg.System.Geo.GeoIpPath)

	// Preload geosite categories if configured
	geositeCategories := []string{}
	if len(cfg.Sets) > 0 {
		for _, set := range cfg.Sets {
			if len(set.Targets.GeoSiteCategories) > 0 {
				geositeCategories = append(geositeCategories, set.Targets.GeoSiteCategories...)
			}
		}
	}
	geositeCategories = utils.FilterUniqueStrings(geositeCategories)

	if cfg.System.Geo.GeoSitePath != "" && len(geositeCategories) > 0 {
		_, err := geodataManager.PreloadCategories(geodat.GEOSITE, geositeCategories)
		if err != nil {
			log.Errorf("Failed to preload categories: %v", err)
		}
	}

	geoipCategories := []string{}
	if len(cfg.Sets) > 0 {
		for _, set := range cfg.Sets {
			if len(set.Targets.GeoIpCategories) > 0 {
				geoipCategories = append(geoipCategories, set.Targets.GeoIpCategories...)
			}
		}
	}
	geoipCategories = utils.FilterUniqueStrings(geoipCategories)

	if cfg.System.Geo.GeoIpPath != "" && len(geoipCategories) > 0 {
		_, err := geodataManager.PreloadCategories(geodat.GEOIP, geoipCategories)
		if err != nil {
			log.Errorf("Failed to preload categories: %v", err)
		}
	}

	return &API{
		cfgPtr:         cfgPtr,
		geodataManager: geodataManager,
		discoveryRT:    discoveryRuntime,
	}
}
func (api *API) RegisterEndpoints(mux *http.ServeMux, cfgPtr *atomic.Pointer[config.Config]) {
	cfg := cfgPtr.Load()
	api.cfgPtr = cfgPtr
	api.mux = mux

	api.geodataManager.UpdatePaths(cfg.System.Geo.GeoSitePath, cfg.System.Geo.GeoIpPath)

	api.RegisterConfigApi()
	api.RegisterUIApi()
	api.RegisterMetricsApi()
	api.RegisterGeositeApi()
	api.RegisterGeoipApi()
	api.RegisterSystemApi()
	api.RegisterDiscoveryApi()
	api.RegisterIntegrationApi()
	api.RegisterGeodatApi()
	api.RegisterCaptureApi()
	api.RegisterSetsApi()
	api.RegisterHubApi()
	api.RegisterConvertApi()
	api.RegisterDnsApi()
	api.RegisterDevicesApi()
	api.RegisterSocks5Api()
	api.RegisterMTProtoApi()
	api.RegisterDetectorApi()
	api.RegisterBackupApi()
	api.RegisterAsnApi()
	api.RegisterWatchdogApi()
	api.RegisterAIApi()
	api.RegisterMCPApi()
	api.RegisterLogTraceApi()
	api.RegisterDebugApi()
}

func sendResponse(w http.ResponseWriter, response interface{}) {
	setJsonHeader(w)
	json.NewEncoder(w).Encode(response)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func SetTablesRefreshFunc(fn func() error) {
	tablesRefreshFunc = fn
}

func SetRoutingSyncFunc(fn func(*config.Config)) {
	routingSyncFunc = fn
}

func SetUpstreamHealthFunc(fn func() []DiagUpstream) {
	upstreamHealthFunc = fn
}

func SetDiscoveryRuntime(rt *discovery.Runtime) {
	discoveryRuntime = rt
}

func SetWatchdog(wd *watchdog.Watchdog) {
	globalWatchdog = wd
}

func checkDiskSpace(dir string, needed int64) error {
	var stat unix.Statfs_t
	if err := unix.Statfs(dir, &stat); err != nil {
		return fmt.Errorf("failed to check disk space on %s: %v", dir, err)
	}
	available := int64(stat.Bavail) * int64(stat.Bsize)
	if available < needed {
		availMB := float64(available) / (1024 * 1024)
		neededMB := float64(needed) / (1024 * 1024)
		return fmt.Errorf("not enough disk space in %s: %.1f MB available, need %.1f MB", dir, availMB, neededMB)
	}
	return nil
}

var (
	downloadStallTimeout = 60 * time.Second
	downloadMaxDuration  = time.Hour
	errDownloadStalled   = errors.New("download stalled")
	errDownloadTooLong   = errors.New("download took too long")
)

type stallReader struct {
	r     io.Reader
	timer *time.Timer
}

func (s *stallReader) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	if n > 0 {
		s.timer.Reset(downloadStallTimeout)
	}
	return n, err
}

func downloadError(ctx context.Context, format string, err error, args ...any) error {
	switch cause := context.Cause(ctx); {
	case errors.Is(cause, errDownloadStalled):
		err = fmt.Errorf("%w: no data received for %s", cause, downloadStallTimeout)
	case errors.Is(cause, errDownloadTooLong):
		err = fmt.Errorf("%w: gave up after %s", cause, downloadMaxDuration)
	}
	return fmt.Errorf(format+": %w", append(args, err)...)
}

func downloadFile(parent context.Context, url, destPath string, verify func(path string) error) (int64, error) {
	ctx, cancel := context.WithCancelCause(parent)
	defer cancel(nil)
	ctx, cancelTimeout := context.WithTimeoutCause(ctx, downloadMaxDuration, errDownloadTooLong)
	defer cancelTimeout()
	stall := time.AfterFunc(downloadStallTimeout, func() { cancel(errDownloadStalled) })
	defer stall.Stop()
	progress := func() { stall.Reset(downloadStallTimeout) }
	ctx = httptrace.WithClientTrace(ctx, &httptrace.ClientTrace{
		ConnectDone:          func(string, string, error) { progress() },
		TLSHandshakeDone:     func(tls.ConnectionState, error) { progress() },
		GotFirstResponseByte: progress,
	})

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, fmt.Errorf("failed to fetch %s: %w", url, err)
	}
	resp, err := downloadClient.Do(req)
	if err != nil {
		return 0, downloadError(ctx, "failed to fetch %s", err, url)
	}
	defer resp.Body.Close()
	progress()

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("remote server returned %s for %s", resp.Status, url)
	}

	dir := filepath.Dir(destPath)

	if resp.ContentLength > 0 {
		if err := checkDiskSpace(dir, resp.ContentLength); err != nil {
			return 0, err
		}
	}

	tmpFile, err := os.CreateTemp(dir, ".download-*.tmp")
	if err != nil {
		return 0, fmt.Errorf("failed to create temp file in %s: %v", dir, err)
	}
	tmpPath := tmpFile.Name()

	cleanup := func() {
		tmpFile.Close()
		os.Remove(tmpPath)
	}

	size, err := io.Copy(tmpFile, &stallReader{r: resp.Body, timer: stall})
	if err != nil {
		cleanup()
		return 0, downloadError(ctx, "failed to download %s (%d bytes written)", err, url, size)
	}

	if err := tmpFile.Sync(); err != nil {
		cleanup()
		return 0, fmt.Errorf("failed to flush data to disk: %v", err)
	}

	if err := tmpFile.Close(); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("failed to finalize file write: %v", err)
	}

	if verify != nil {
		if err := verify(tmpPath); err != nil {
			os.Remove(tmpPath)
			return 0, fmt.Errorf("the file downloaded from %s (%d bytes) was rejected: %w", url, size, err)
		}
	}

	if err := os.Rename(tmpPath, destPath); err != nil {
		os.Remove(tmpPath)
		return 0, fmt.Errorf("failed to move downloaded file to %s: %v", destPath, err)
	}

	return size, nil
}

type MTProtoWebProxy interface {
	ServeWebProxy(w http.ResponseWriter, r *http.Request) bool
	WebProxyHost() string
	WebProxyOwnListener() bool
}

func MTProtoWebProxyServer() MTProtoWebProxy {
	if p, ok := globalMTProtoServer.(MTProtoWebProxy); ok {
		return p
	}
	return nil
}
