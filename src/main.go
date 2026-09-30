// src/main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"slices"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/daniellavrushin/b4/ai"
	"github.com/daniellavrushin/b4/asnprefix"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/geodat"
	b4http "github.com/daniellavrushin/b4/http"
	"github.com/daniellavrushin/b4/http/handler"
	"github.com/daniellavrushin/b4/hub"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/mtproto"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/quic"
	"github.com/daniellavrushin/b4/socks5"
	"github.com/daniellavrushin/b4/tables"
	"github.com/daniellavrushin/b4/tproxy"
	b4tun "github.com/daniellavrushin/b4/tun"
	"github.com/daniellavrushin/b4/watchdog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	cfg             = config.NewConfig()
	cliOverrides    config.CLIOverrides
	verboseFlag     string
	showVersion     bool
	clearTables     bool
	Version         = "dev"
	Commit          = "none"
	Date            = "unknown"
	currentLogLevel = log.LevelInfo
)

var rootCmd = &cobra.Command{
	Use:           "b4",
	Short:         "B4 network packet processor",
	Long:          `B4 is a netfilter queue based packet processor for DPI circumvention`,
	RunE:          runB4,
	SilenceUsage:  true,
	SilenceErrors: true,
}

func init() {
	// Bind all configuration flags
	cfg.BindFlags(rootCmd, &cliOverrides)

	// Add verbosity flags separately since they need special handling
	rootCmd.Flags().StringVar(&verboseFlag, "verbose", "info", "Set verbosity level (debug, trace, info, silent), default: info")
	rootCmd.Flags().BoolVarP(&showVersion, "version", "v", false, "Show version and exit")
	rootCmd.Flags().BoolVar(&clearTables, "clear-tables", false, "Perform only iptables/nftables cleanup and exit")

}

// @title B4 API
// @version 1.0
// @description B4 network packet processor REST API
// @BasePath /api
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization
// @description Enter "Bearer {token}" to authorize
func main() {
	err := rootCmd.Execute()
	if err == nil {
		err = restartIfRequested()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runB4(cmd *cobra.Command, args []string) error {
	handler.Version = Version
	handler.Commit = Commit
	handler.Date = Date

	if showVersion {
		fmt.Printf("B4 version: %s (%s) %s\n", Version, Commit, Date)
		return nil
	}

	var tablesMonitorRef atomic.Pointer[tables.Monitor]
	var tunEngineRef atomic.Pointer[b4tun.Engine]
	recheckSig := make(chan os.Signal, 1)
	signal.Notify(recheckSig, syscall.SIGUSR1)
	defer func() {
		signal.Stop(recheckSig)
		close(recheckSig)
	}()
	go func() {
		for range recheckSig {
			tables.KickExposure()
			mon := tablesMonitorRef.Load()
			tunEng := tunEngineRef.Load()
			if mon == nil && tunEng == nil {
				if st := tables.ExposureStatus(); len(st.Ports) > 0 && !st.SkipSetup {
					log.Infof("Received SIGUSR1, re-checking the rules that open b4's ports; the tables monitor is not running, so no other firewall rule is re-checked")
				} else {
					log.Infof("Received SIGUSR1, but the tables monitor is not running and no port is exposed, so there are no firewall rules to re-check")
				}
				continue
			}
			log.Infof("Received SIGUSR1, re-checking firewall rules")
			if tunEng != nil {
				tunEng.Recheck()
			}
			if mon != nil {
				mon.Kick()
			}
		}
	}()

	releaseLock, err := ensureSingleInstance()
	if err != nil {
		return err
	}
	if releaseLock != nil {
		defer releaseLock()
	}

	initTimezone()
	config.ApplyPATH()

	needsSave, loadErr := cfg.LoadWithMigration(cfg.ConfigPath)
	unreadableConfigNotice := keepUnreadableConfig(cfg.ConfigPath, loadErr)
	if needsSave {
		cfg.SaveToFile(cfg.ConfigPath)
	}
	cfg.ApplyCLIOverrides(cmd, &cliOverrides)

	if cfg.System.Timezone != "" {
		config.ApplyTimezone(cfg.System.Timezone)
	}

	if limit, err := config.ApplyMemoryLimit(cfg.System.MemoryLimit); err != nil {
		log.InitWarnf("invalid system.memory_limit %q: %v", cfg.System.MemoryLimit, err)
	} else if limit > 0 {
		fmt.Fprintf(os.Stderr, "[INIT] Memory limit set to %d MB\n", limit/(1024*1024))
	}

	fmt.Fprintf(os.Stderr, "[INIT] OS thread limit set to %d\n", metrics.ApplyThreadLimit())

	if cmd.Flags().Changed("verbose") {
		cfg.ApplyLogLevel(verboseFlag)
	}

	var cfgPtr atomic.Pointer[config.Config]
	cfgPtr.Store(&cfg)

	appCtx, appCancel := context.WithCancel(context.Background())
	defer appCancel()

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigChan)

	aiManager := ai.NewManager(cfg.System.AI, cfg.ConfigPath)
	handler.SetAIManager(aiManager)

	discoveryRT := discovery.NewRuntime()

	dnsNames := dns.NewNameCache()
	nfq.DNSNames = dnsNames
	tables.DNSNames = dnsNames
	tproxyResolver := tproxy.NewResolver(dnsNames)
	tproxyMgr := tproxy.NewManager(tproxyResolver)

	mtprotoBridge := mtproto.NewTransparentBridge(&cfg)
	tproxyMgr.SetMTProtoBridge(mtprotoBridge)
	tproxyMgr.SetTelegramBridgeHook(func(v4, v6, retried bool) {
		if tables.SetTelegramBridgeListener(v4, v6) && retried {
			tables.RoutingResyncLatest()
		}
	})
	handler.SetMTProtoBridge(mtprotoBridge)
	handler.SetBridgeListenerFunc(func() handler.BridgeListenerInfo {
		st := tproxyMgr.ListenerStatus(config.TelegramBridgeSetID, tproxy.PortFor(config.TelegramBridgeMark))
		return handler.BridgeListenerInfo{
			Running: st.Running,
			Port:    st.Port,
			V4:      st.V4,
			V6:      st.V6,
			Active:  st.Active,
			Error:   st.Error,
			V6Error: st.V6Error,
		}
	})
	handler.SetUpstreamHealthFunc(func() []handler.DiagUpstream {
		health := tproxyMgr.UpstreamHealth()
		out := make([]handler.DiagUpstream, 0, len(health))
		for _, h := range health {
			d := handler.DiagUpstream{
				SetID:               h.SetID,
				SetName:             h.SetName,
				Upstream:            h.Upstream,
				FailOpen:            h.FailOpen,
				Reachable:           h.ConsecutiveFailures == 0,
				ConsecutiveFailures: h.ConsecutiveFailures,
				LastError:           h.LastError,
			}
			if !h.LastFailure.IsZero() {
				d.LastFailure = h.LastFailure.UTC().Format(time.RFC3339)
			}
			if !h.LastSuccess.IsZero() {
				d.LastSuccess = h.LastSuccess.UTC().Format(time.RFC3339)
			}
			out = append(out, d)
		}
		return out
	})
	var engineDown atomic.Bool
	refreshTables := func() error {
		c := cfgPtr.Load()
		if c.System.Tables.SkipSetup {
			return nil
		}
		if engineDown.Load() {
			log.Infof("Firewall refresh skipped: the packet engine is not running, so the change applies when b4 restarts")
			return nil
		}
		if tunEngineRef.Load() != nil {
			firewallErr := tables.RefreshTUNFirewall(c)
			tproxyMgr.SyncConfig(c)
			tables.RoutingSyncConfig(c)
			return firewallErr
		}
		if discoveryRT.IsActive() {
			log.Warnf("Tables refresh requested while discovery is active, waiting for discovery to finish...")
			deadline := time.After(5 * time.Minute)
			ticker := time.NewTicker(1 * time.Second)
			defer ticker.Stop()
			for discoveryRT.IsActive() {
				select {
				case <-deadline:
					return fmt.Errorf("tables refresh timed out: discovery did not finish within 5 minutes")
				case <-ticker.C:
				}
			}
		}
		if err := tables.RefreshRules(c); err != nil {
			return err
		}
		nfq.ResetIfaceTraffic()
		tproxyMgr.SyncConfig(c)
		tables.RoutingSyncConfig(c)
		handler.GetMetricsCollector().TablesStatus = tables.DetectBackend(c)
		return nil
	}
	handler.SetTablesRefreshFunc(refreshTables)
	handler.SetDiscoveryRuntime(discoveryRT)
	nfq.DNSTCPReadyFunc = tables.SetDNSTCPListenerReady
	nfq.RoutingHandleDNSFunc = tables.RoutingHandleDNS
	nfq.RoutingLearnIPFunc = tables.RoutingLearnIP
	nfq.RoutingLearnHostFunc = tables.RoutingLearnHost
	nfq.RoutingHandleDNSAsyncFunc = tables.RoutingHandleDNSAsync
	nfq.RoutingHandleDNSAwaitFunc = tables.RoutingHandleDNSAwait
	nfq.RoutingLearnIPAsyncFunc = tables.RoutingLearnIPAsync
	nfq.RoutingLearnHostAsyncFunc = tables.RoutingLearnHostAsync

	if err := initLogging(&cfg); err != nil {
		return fmt.Errorf("logging initialization failed: %w", err)
	}
	if unreadableConfigNotice != "" {
		log.Errorf("%s", unreadableConfigNotice)
	}

	if clearTables {
		log.Infof("Clearing iptables rules as requested (--clear-iptables)")
		clearErr := tables.ClearRules(&cfg)
		b4tun.RestoreFromState()
		tables.RoutingClearAll()
		tables.SweepExposure()
		b4tun.ClearStaleArtifacts(&cfg)
		if clearErr != nil {
			return log.Errorf("failed to clear iptables/nftables rules: %w", clearErr)
		}
		log.Infof("IPTables rules cleared successfully")
		return nil
	}

	log.Infof("Starting B4 packet processor")

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return log.Errorf("invalid configuration: %w", err)
	}

	printConfigDefaults(cmd)

	config.WarnIPv6Bypass(&cfg)

	// Initialize metrics collector early
	metrics := handler.GetMetricsCollector()
	metrics.RecordEvent("info", "B4 starting up")

	if cfg.System.WebServer.Port > 0 {
		metrics.RecordEvent("info", fmt.Sprintf("Web server started on port %d", cfg.System.WebServer.Port))
	}

	config.InitAsnStore(cfg.ConfigPath)

	_, totalDomains, totalIps, targetWarnings := cfg.LoadTargets()
	for _, warning := range targetWarnings {
		log.Errorf("%v", warning)
		metrics.RecordEvent("error", warning.Error())
	}

	log.Infof("Loaded targets: %d domains, %d IPs across %d sets", totalDomains, totalIps, len(cfg.Sets))
	if cfg.TelegramBridgeEnabled() {
		mtproto.LoadLocalTelegramCIDRs(&cfg)
	}
	b4tun.RestoreFromState()
	tables.RoutingClearAll()

	isTUN := cfg.Queue.Mode == "tun"
	engineAttempt = loadEngineAttempt()

	pool := nfq.NewPool(&cfg)

	var tunEngine *b4tun.Engine
	var tablesMonitor *tables.Monitor
	var engineErr error

	if isTUN {
		tunEngine, engineErr = startTUNEngine(&cfg, pool, tproxyMgr, metrics)
	} else {
		engineErr = startNFQueueEngine(&cfg, pool, tproxyMgr, metrics)
	}

	engineName := engineLabel(engineMode(&cfg))
	if engineErr != nil {
		tproxyMgr.Stop()
		tables.RoutingClearAll()
		if cfg.System.WebServer.Port == 0 {
			return log.Errorf("%s engine did not start: %v. b4 stops because the web server is off (port 0)", engineName, engineErr)
		}
		engineDown.Store(true)
		if retry := enterDegradedMode(&cfg, engineErr, engineAttempt, metrics); retry != nil {
			defer retry.Stop()
		}
	} else {
		if tunEngine != nil {
			tunEngineRef.Store(tunEngine)
		}
		if engineAttempt > 0 {
			log.Infof("%s engine started on automatic retry %d of %d", engineName, engineAttempt, len(engineRetryDelays))
		}
	}

	if !engineDown.Load() && !cfg.System.Tables.SkipSetup && (isTUN || cfg.System.Tables.MonitorInterval > 0) {
		tablesMonitor = tables.NewMonitor(&cfgPtr)
		tablesMonitor.Start()
		tablesMonitorRef.Store(tablesMonitor)
	}

	stopExposureWatch := func() {}
	shutdownHandled := false
	defer func() {
		if shutdownHandled {
			return
		}
		stopExposureWatch()
		tables.ClearExposure()
		if tablesMonitor != nil {
			tablesMonitor.Stop()
		}
		c := cfgPtr.Load()
		if tunEngine != nil {
			tunEngine.Stop()
			if !c.System.Tables.SkipSetup {
				tables.ClearTUNFirewall(c)
				tables.RevertConntrackSysctls()
			}
		} else if !c.System.Tables.SkipSetup && !engineDown.Load() {
			tables.ClearRules(c)
		}
		tables.RoutingClearAll()
	}()

	tproxyResolver.Set(pool.GetMatcher())

	mtproto.StartUpstreamRefresh(appCtx, cfgPtr.Load)

	cidrCtx, cidrCancel := context.WithCancel(appCtx)
	defer cidrCancel()
	mtproto.StartTelegramCIDRRefresh(cidrCtx, cfgPtr.Load, func() {
		unlock := config.LockWrites()
		defer unlock()
		if cidrCtx.Err() != nil {
			return
		}
		c := cfgPtr.Load()
		if c.System.Tables.SkipSetup || engineDown.Load() || !c.TelegramBridgeEnabled() {
			return
		}
		tables.RoutingSyncConfig(c)
	})

	handler.SetRoutingSyncFunc(func(c *config.Config) {
		tproxyResolver.Set(pool.GetMatcher())
		config.WarnIPv6Bypass(c)
		if engineDown.Load() {
			return
		}
		tproxyMgr.SyncConfig(c)
		tables.RoutingSyncConfig(c)
	})

	handler.SetTUNEngine(tunEngine)
	handler.SetSelfRestartFunc(func() { requestRestart(restartManual) })

	hubService := hub.New(func() *config.Config { return cfgPtr.Load() }, hub.Options{Version: Version})
	handler.SetHubService(hubService)

	// Start SOCKS5 server if configured.
	socks5Server := socks5.NewServer(&cfg)
	socks5Server.SetIPBlockCache(pool.GetIPBlockCache())
	socks5Server.SetUpstreamDialer(tproxyMgr)
	if err := socks5Server.Start(); err != nil {
		metrics.RecordEvent("error", fmt.Sprintf("Failed to start SOCKS5 server: %v", err))
		log.Errorf("SOCKS5 server did not start: %v (b4 continues without it; fix in Settings or config)", err)
	}
	handler.SetSocks5Server(socks5Server)

	// Start MTProto server if configured.
	mtprotoServer := mtproto.NewServer(&cfg)
	if err := mtprotoServer.Start(); err != nil {
		metrics.RecordEvent("error", fmt.Sprintf("Failed to start MTProto server: %v", err))
		log.Errorf("MTProto server did not start: %v (b4 continues without it; fix in Settings or config)", err)
	}
	handler.SetMTProtoServer(mtprotoServer)

	wd := watchdog.New(&cfgPtr, discoveryRT, watchdog.NewUpdateFunc(cfgPtr.Load, func(_, c *config.Config) error {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %v", err)
		}
		if pool != nil {
			if err := pool.UpdateConfig(c); err != nil {
				return fmt.Errorf("failed to update pool config: %v", err)
			}
		}
		if tunEngine != nil {
			tunEngine.UpdateConfig(c)
		}
		if err := c.SaveToFile(c.ConfigPath); err != nil {
			return fmt.Errorf("failed to save config: %v", err)
		}
		cfgPtr.Store(c)
		socks5Server.UpdateConfig(c)
		mtprotoServer.UpdateConfig(c)
		mtprotoBridge.UpdateConfig(c)
		mtproto.KickUpstreamRefresh()
		tproxyResolver.Set(pool.GetMatcher())
		if !c.System.Tables.SkipSetup {
			tproxyMgr.SyncConfig(c)
			tables.RoutingSyncConfig(c)
		}
		aiManager.Update(c.System.AI)
		if _, err := config.ApplyMemoryLimit(c.System.MemoryLimit); err != nil {
			log.Errorf("invalid system.memory_limit %q: %v", c.System.MemoryLimit, err)
		}
		return nil
	}, func() {
		log.Infof("[WATCHDOG] the healed strategy changes the ports b4 intercepts, refreshing firewall rules")
		if err := refreshTables(); err != nil {
			log.Errorf("[WATCHDOG] firewall refresh after heal failed: %v", err)
		}
	}))
	wd.SetEngine(watchdog.NewEngineView(pool, &cfgPtr))
	if !engineDown.Load() {
		wd.Start()
		handler.SetWatchdog(wd)
	}

	geodat.RemoveStaleDownloads(cfg.System.Geo.GeoSitePath, cfg.System.Geo.GeoIpPath)

	webListener := cfgPtr.Load().ConfiguredWebListener()
	handler.SetRunningWebListener(webListener)
	listening := func(service string) bool {
		switch service {
		case config.ExposeWebServer:
			return b4http.WebListening()
		case config.ExposeMTProto:
			return mtprotoServer.Running()
		case config.ExposeMTProtoWebProxy:
			return mtprotoServer.WebProxyOwnListener()
		case config.ExposeSocks5:
			return socks5Server.Running()
		}
		return false
	}
	exposurePlan := func(c *config.Config) ([]config.ExposedPort, []config.ExposeBlock, bool) {
		ports, blocked := c.ExposurePlan(webListener)
		ports, blocked = exposeListeningOnly(ports, blocked, listening)
		return ports, blocked, c.System.Tables.SkipSetup
	}
	handler.SetExposureSyncFunc(func(c *config.Config) { tables.SyncExposure(exposurePlan(c)) })
	handler.SetExposureShrinkFunc(func(c *config.Config) {
		ports, _, _ := exposurePlan(c)
		tables.ShrinkExposure(ports)
	})

	// Start internal web server if configured
	httpServer, apiHandler, err := b4http.StartServer(&cfgPtr, pool)
	if err != nil {
		metrics.RecordEvent("error", fmt.Sprintf("Failed to start web server: %v", err))
		return log.Errorf("failed to start web server: %w", err)
	}

	tables.SyncExposure(exposurePlan(cfgPtr.Load()))
	stopExposureWatch = tables.StartExposureWatch(
		time.Duration(cfgPtr.Load().System.Tables.MonitorInterval)*time.Second,
		func() {
			unlock := config.LockWrites()
			defer unlock()
			tables.SyncExposure(exposurePlan(cfgPtr.Load()))
		},
	)

	var geoScheduler *geodat.Scheduler
	if apiHandler != nil {
		geoScheduler = geodat.NewScheduler(
			func() geodat.GeoDatConfig { return cfgPtr.Load().System.Geo },
			func(ctx context.Context, dest, siteURL, ipURL string) error {
				_, _, _, err := apiHandler.RefreshGeodat(ctx, dest, siteURL, ipURL)
				return err
			},
			func(ts string) {
				unlock := config.LockWrites()
				defer unlock()
				c := cfgPtr.Load().Clone()
				c.System.Geo.AutoUpdate.LastRun = ts
				if err := c.SaveToFile(c.ConfigPath); err != nil {
					log.Errorf("failed to persist geo last_run: %v", err)
					return
				}
				cfgPtr.Store(c)
			},
		)
		geoScheduler.Start()
	}

	hubService.Start()

	asnCtx, asnCancel := context.WithCancel(appCtx)
	defer asnCancel()
	asnprefix.Start(asnCtx, cfgPtr.Load, func(changed []string) {
		if asnCtx.Err() != nil {
			return
		}
		if apiHandler != nil {
			apiHandler.ReloadASNTargets(changed)
			return
		}
		refresh := reloadASNTargetsHeadless(asnCtx, cfgPtr.Load, changed, func(_, c *config.Config) error {
			if pool != nil {
				if err := pool.UpdateConfig(c); err != nil {
					return fmt.Errorf("failed to update pool config: %v", err)
				}
			}
			if tunEngine != nil {
				tunEngine.UpdateConfig(c)
			}
			cfgPtr.Store(c)
			socks5Server.UpdateConfig(c)
			tproxyResolver.Set(pool.GetMatcher())
			if !c.System.Tables.SkipSetup && !engineDown.Load() {
				tproxyMgr.SyncConfig(c)
				tables.RoutingSyncConfig(c)
			}
			return nil
		})
		if refresh {
			if err := refreshTables(); err != nil {
				log.Errorf("Firewall refresh after the ASN prefix change failed: %v", err)
			}
		}
	})

	log.Infof("B4 is running. Press Ctrl+C to stop")
	metrics.RecordEvent("info", "B4 is fully operational")

	// Wait for shutdown signal
	var sig os.Signal
	var restart restartKind
	select {
	case sig = <-sigChan:
	case restart = <-restartRequests:
		pendingRestart.Store(int32(restart))
	}

	go func() {
		for repeat := range sigChan {
			if restartKind(pendingRestart.Swap(int32(restartNone))) != restartNone {
				log.Infof("Received %v during the restart, b4 stops instead of starting again", repeat)
				continue
			}
			log.Infof("Received %v while already shutting down, ignoring it (SIGKILL forces an exit)", repeat)
		}
	}()

	switch {
	case sig != nil:
		log.Infof("Received signal: %v, shutting down gracefully", sig)
		metrics.RecordEvent("info", fmt.Sprintf("Shutdown initiated by signal: %v", sig))
	case restart == restartEngineRetry:
		log.Infof("Restarting b4 for automatic engine retry %d of %d", engineAttempt+1, len(engineRetryDelays))
		metrics.RecordEvent("info", "Restart initiated for an automatic engine retry")
	default:
		log.Infof("Restarting b4 in place")
		metrics.RecordEvent("info", "Restart initiated")
	}

	hardExit := make(chan struct{})
	defer close(hardExit)
	go func() {
		select {
		case <-hardExit:
		case <-time.After(shutdownHardLimit):
			go func() {
				log.Errorf("Shutdown exceeded %s, forcing exit", shutdownHardLimit)
				log.Flush()
			}()
			time.Sleep(100 * time.Millisecond)
			os.Exit(1)
		}
	}()

	wd.Stop()
	hubService.Stop()
	handler.StopGeodatDownloads()
	if geoScheduler != nil {
		geoScheduler.Stop()
	}
	if tablesMonitor != nil {
		tablesMonitor.Stop()
	}
	stopExposureWatch()
	cidrCancel()
	asnCancel()
	tproxyMgr.Stop()

	// Perform graceful shutdown with timeout
	shutdownHandled = true
	return gracefulShutdown(cfgPtr.Load(), pool, tunEngine, !engineDown.Load(), httpServer, socks5Server, mtprotoServer, metrics, discoveryRT)
}

const (
	shutdownGrace     = 9 * time.Second
	httpShutdownGrace = 3 * time.Second
	shutdownHardLimit = 15 * time.Second
)

func exposeListeningOnly(ports []config.ExposedPort, blocked []config.ExposeBlock, listening func(string) bool) ([]config.ExposedPort, []config.ExposeBlock) {
	var kept []config.ExposedPort
	for _, p := range ports {
		if listening(p.Service) {
			kept = append(kept, p)
			continue
		}
		blocked = append(blocked, config.ExposeBlock{Service: p.Service, Reason: config.ExposeBlockedNotListening})
	}
	return kept, blocked
}

func gracefulShutdown(cfg *config.Config, pool *nfq.Pool, tunEngine *b4tun.Engine, engineUp bool, httpServer *http.Server, socks5Server *socks5.Server, mtprotoServer *mtproto.Server, metrics *handler.MetricsCollector, discoveryRT *discovery.Runtime) error {
	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownGrace)
	defer cancel()

	// Create wait group for parallel shutdown
	var wg sync.WaitGroup
	shutdownErrors := make(chan error, 4)

	// Shutdown HTTP server
	if httpServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Infof("Shutting down HTTP server...")
			httpCtx, httpCancel := context.WithTimeout(shutdownCtx, httpShutdownGrace)
			defer httpCancel()
			if err := httpServer.Shutdown(httpCtx); err != nil {
				log.Warnf("HTTP server did not drain in %s, closing its connections: %v", httpShutdownGrace, err)
				_ = httpServer.Close()
			} else {
				log.Infof("HTTP server stopped")
			}
		}()
	}

	// Shutdown SOCKS5 server
	if socks5Server != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := socks5Server.Stop(); err != nil {
				log.Errorf("SOCKS5 server shutdown error: %v", err)
				shutdownErrors <- fmt.Errorf("SOCKS5 shutdown: %w", err)
			} else {
				log.Infof("SOCKS5 server stopped")
			}
		}()
	}

	if mtprotoServer != nil {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := mtprotoServer.Stop(); err != nil {
				log.Errorf("MTProto server shutdown error: %v", err)
				shutdownErrors <- fmt.Errorf("MTProto shutdown: %w", err)
			} else {
				log.Infof("MTProto server stopped")
			}
		}()
	}

	// Shutdown WebSocket connections
	log.Infof("Shutting down WebSocket connections...")
	b4http.Shutdown()

	if discoveryRT != nil && discoveryRT.IsActive() {
		log.Infof("Stopping active discovery...")
		discoveryRT.Stop("")
	}

	// Stop NFQueue pool
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Infof("Stopping netfilter queue pool...")
		metrics.NFQueueStatus = "stopping"

		// Use a goroutine with timeout for engine stop
		stopDone := make(chan struct{})
		go func() {
			if tunEngine != nil {
				tunEngine.Stop()
			}
			pool.Stop()
			close(stopDone)
		}()

		select {
		case <-stopDone:
			log.Infof("Netfilter queue pool stopped")
		case <-shutdownCtx.Done():
			log.Errorf("Netfilter queue pool stop timed out")
			shutdownErrors <- fmt.Errorf("NFQueue stop timeout")
		}

		quic.Shutdown()
	}()

	// Clean up iptables/nftables rules
	if tunEngine != nil {
		if !cfg.System.Tables.SkipSetup {
			wg.Add(1)
			go func() {
				defer wg.Done()
				tables.ClearTUNFirewall(cfg)
				tables.RevertConntrackSysctls()
			}()
		}
		metrics.TablesStatus = "inactive"
	} else if engineUp && !cfg.System.Tables.SkipSetup {
		wg.Add(1)
		go func() {
			defer wg.Done()
			log.Infof("Clearing iptables/nftables rules...")
			if err := tables.ClearRules(cfg); err != nil {
				log.Errorf("Failed to clear tables rules: %v", err)
				metrics.RecordEvent("error", fmt.Sprintf("Failed to clear tables rules: %v", err))
				shutdownErrors <- fmt.Errorf("tables cleanup: %w", err)
			} else {
				log.Infof("Tables rules cleared")
				metrics.TablesStatus = "inactive"
			}
		}()
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		tables.ClearExposure()
		tables.RoutingClearAll()
	}()

	// Wait for all shutdown tasks or timeout
	shutdownDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(shutdownDone)
	}()

	select {
	case <-shutdownDone:
		// All tasks completed
		close(shutdownErrors)

		// Check for any errors
		var errs []error
		for err := range shutdownErrors {
			errs = append(errs, err)
		}

		if len(errs) > 0 {
			log.Errorf("Shutdown completed with %d errors", len(errs))
			for _, err := range errs {
				log.Errorf("  - %v", err)
			}
			metrics.RecordEvent("warning", fmt.Sprintf("B4 shutdown with %d errors", len(errs)))
		} else {
			log.Infof("B4 stopped successfully")
			metrics.RecordEvent("info", "B4 shutdown complete")
		}

	case <-shutdownCtx.Done():
		log.Errorf("Shutdown timeout reached, forcing exit")
		metrics.RecordEvent("error", "Forced shutdown due to timeout")

		log.Flush()
		time.Sleep(100 * time.Millisecond)

		os.Exit(1)
	}

	nfq.ShutdownDNSRouteRuntime()

	log.CloseErrorFile()
	log.Flush()
	return nil
}

func ensureSingleInstance() (func(), error) {
	candidates := []string{"/var/run/b4.pid", "/run/b4.pid", "/tmp/b4.pid"}
	var f *os.File
	var path string
	var lastErr error
	for _, p := range candidates {
		fp, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
		if err == nil {
			f = fp
			path = p
			break
		}
		lastErr = err
	}
	if f == nil {
		log.InitWarnf("WARNING: single-instance guard DISABLED, no lock file could be opened (tried %s; last error: %v)",
			strings.Join(candidates, ", "), lastErr)
		return nil, nil
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			log.InitWarnf("WARNING: single-instance guard DISABLED, flock(%s): %v", path, err)
			f.Close()
			return nil, nil
		}
		data, _ := io.ReadAll(f)
		pid := strings.TrimSpace(string(data))
		f.Close()
		if pid == "" {
			return nil, fmt.Errorf("another b4 instance is already running (lock: %s)", path)
		}
		return nil, fmt.Errorf("another b4 instance is already running (pid %s)", pid)
	}

	if err := writePidFile(f, os.Getpid()); err != nil {
		log.InitWarnf("could not update pidfile %s: %v", path, err)
	}

	cleanup := func() {
		if ownsPidFile(f, path) {
			os.Remove(path)
		}
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
	return cleanup, nil
}

func ownsPidFile(f *os.File, path string) bool {
	held, err := f.Stat()
	if err != nil {
		return false
	}
	cur, err := os.Stat(path)
	if err != nil {
		return false
	}
	return os.SameFile(held, cur)
}

func writePidFile(f *os.File, pid int) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(f, "%d\n", pid); err != nil {
		return err
	}
	return f.Sync()
}

func initTimezone() {
	// Apply TZ env var if set; otherwise keep Go's default (system timezone from /etc/localtime)
	if tzName := os.Getenv("TZ"); tzName != "" {
		config.ApplyTimezone(tzName)
	}
}

func reloadASNTargetsHeadless(ctx context.Context, load func() *config.Config, changed []string, commit func(previous, next *config.Config) error) bool {
	ids := make(map[string]bool, len(changed))
	for _, raw := range changed {
		if id, ok := config.NormalizeASN(raw); ok {
			ids[id] = true
		}
	}
	if len(ids) == 0 {
		return false
	}
	unlock := config.LockWrites()
	defer unlock()
	if ctx.Err() != nil {
		return false
	}
	previous := load()
	next := previous.Clone()
	var names []string
	for _, set := range next.Sets {
		if set == nil || !slices.ContainsFunc(set.Targets.ASNs, func(raw string) bool {
			id, ok := config.NormalizeASN(raw)
			return ok && ids[id]
		}) {
			continue
		}
		if _, _, err := next.GetTargetsForSet(set); err != nil {
			log.Warnf("Set '%s' takes its new ASN prefixes without the geo categories that could not be read: %v", set.Name, err)
		}
		names = append(names, set.Name)
	}
	if len(names) == 0 {
		return false
	}
	if err := commit(previous, next); err != nil {
		log.Errorf("ASN prefixes changed but the sets using them could not be reloaded: %v", err)
		return false
	}
	log.Infof("Reloaded the ASN targets of %s", strings.Join(names, ", "))
	return config.FirewallRefreshNeeded(previous, next)
}

func keepUnreadableConfig(path string, loadErr error) string {
	if loadErr == nil {
		return ""
	}
	kept, err := config.KeepCorruptCopy(path)
	switch {
	case err != nil:
		return fmt.Sprintf("The config file %s could not be loaded (%v) and no copy of it could be kept (%v); b4 starts without the settings it could not read, and the next save replaces the file", path, loadErr, err)
	case kept != "":
		return fmt.Sprintf("The config file %s could not be loaded (%v); a copy was kept as %s, b4 starts without the settings it could not read, and the next save replaces the original", path, loadErr, kept)
	}
	return ""
}

func initLogging(cfg *config.Config) error {

	fmt.Fprintf(os.Stderr, "[INIT] Logging initialized at level %d\n", cfg.System.Logging.Level)

	w := io.MultiWriter(log.OrigStderr(), b4http.LogWriter())
	log.Init(w, log.Level(cfg.System.Logging.Level), cfg.System.Logging.Instaflush)

	if cfg.System.Logging.Syslog {
		if err := log.EnableSyslog("b4"); err != nil {
			log.Warnf("Syslog unavailable, continuing without it: %v", err)
			cfg.System.Logging.Syslog = false
		} else {
			log.Infof("Syslog enabled")
		}
	}

	metrics.SetThreadDumpDir(cfg.System.Logging.Directory)

	if errFilePath := cfg.System.Logging.ErrorFilePath(); errFilePath != "" {
		if err := log.InitErrorFile(errFilePath); err != nil {
			log.Errorf("Failed to open error log file: %v", err)
		} else {
			log.Infof("Error logging to file: %s", errFilePath)
		}
	}

	currentLogLevel = log.Level(cfg.System.Logging.Level)
	return nil
}

func printConfigDefaults(cmd *cobra.Command) {
	var all []*pflag.Flag
	cmd.InheritedFlags().VisitAll(func(f *pflag.Flag) { all = append(all, f) })
	cmd.Flags().VisitAll(func(f *pflag.Flag) { all = append(all, f) })
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	log.Infof("Effective CLI flags:")
	line := ""
	for _, f := range all {
		if line == "" {
			line = fmt.Sprintf("--%s=%s", f.Name, f.Value.String())
		} else {
			line += " " + fmt.Sprintf("--%s=%s", f.Name, f.Value.String())
		}
	}
	log.Infof("  %s", line)
}
