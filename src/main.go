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
	"runtime/debug"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
	_ "time/tzdata"

	"github.com/daniellavrushin/b4/ai"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/discovery"
	b4http "github.com/daniellavrushin/b4/http"
	"github.com/daniellavrushin/b4/http/handler"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/mtproto"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/quic"
	"github.com/daniellavrushin/b4/socks5"
	"github.com/daniellavrushin/b4/tables"
	"github.com/daniellavrushin/b4/tproxy"
	"github.com/daniellavrushin/b4/watchdog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var (
	cfg             = config.NewConfig()
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
	cfg.BindFlags(rootCmd)

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
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}

func runB4(cmd *cobra.Command, args []string) error {
	if showVersion {
		fmt.Printf("B4 version: %s (%s) %s\n", Version, Commit, Date)
		return nil
	}

	releaseLock, err := ensureSingleInstance()
	if err != nil {
		return err
	}
	if releaseLock != nil {
		defer releaseLock()
	}

	initTimezone()
	initMemoryLimit()

	cfg.LoadWithMigration(cfg.ConfigPath)
	cfg.SaveToFile(cfg.ConfigPath)

	if cfg.System.Timezone != "" {
		config.ApplyTimezone(cfg.System.Timezone)
	}

	if cmd.Flags().Changed("verbose") {
		cfg.ApplyLogLevel(verboseFlag)
	}

	var cfgPtr atomic.Pointer[config.Config]
	cfgPtr.Store(&cfg)

	aiManager := ai.NewManager(cfg.System.AI, cfg.ConfigPath)
	handler.SetAIManager(aiManager)

	discoveryRT := discovery.NewRuntime()

	tproxyResolver := tproxy.NewLearnedIPResolver(nil)
	tproxyMgr := tproxy.NewManager(tproxyResolver)

	handler.SetTablesRefreshFunc(func() error {
		c := cfgPtr.Load()
		if c.System.Tables.SkipSetup {
			return nil
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
		if err := tables.ClearRules(c); err != nil {
			return err
		}
		if err := tables.AddRules(c); err != nil {
			return err
		}
		tproxyMgr.SyncConfig(c)
		tables.RoutingSyncConfig(c)
		handler.GetMetricsCollector().TablesStatus = tables.DetectBackend(c)
		return nil
	})
	handler.SetRoutingSyncFunc(func(c *config.Config) {
		tproxyMgr.SyncConfig(c)
		tables.RoutingSyncConfig(c)
	})
	handler.SetDiscoveryRuntime(discoveryRT)
	nfq.RoutingHandleDNSFunc = tables.RoutingHandleDNS

	if err := initLogging(&cfg); err != nil {
		return fmt.Errorf("logging initialization failed: %w", err)
	}

	if clearTables {
		log.Infof("Clearing iptables rules as requested (--clear-iptables)")
		tables.ClearRules(&cfg)
		tables.RoutingClearAll()
		log.Infof("IPTables rules cleared successfully")
		return nil
	}

	log.Infof("Starting B4 packet processor")

	// Validate configuration
	if err := cfg.Validate(); err != nil {
		return log.Errorf("invalid configuration: %w", err)
	}

	printConfigDefaults(cmd)

	// Initialize metrics collector early
	metrics := handler.GetMetricsCollector()
	metrics.RecordEvent("info", "B4 starting up")

	if cfg.System.WebServer.Port > 0 {
		metrics.RecordEvent("info", fmt.Sprintf("Web server started on port %d", cfg.System.WebServer.Port))
	}

	// Load domains
	_, totalDomains, totalIps, err := cfg.LoadTargets()
	if err != nil {
		metrics.RecordEvent("error", fmt.Sprintf("Failed to load domains: %v", err))
		return fmt.Errorf("failed to load domains: %w", err)
	}

	log.Infof("Loaded targets: %d domains, %d IPs across %d sets", totalDomains, totalIps, len(cfg.Sets))
	tables.RoutingClearAll()

	// Setup iptables/nftables rules
	if !cfg.System.Tables.SkipSetup {
		log.Tracef("Clearing existing iptables/nftables rules")
		tables.ClearRules(&cfg)

		log.Tracef("Adding tables rules")
		if err := tables.AddRules(&cfg); err != nil {
			metrics.RecordEvent("error", fmt.Sprintf("Failed to add tables rules: %v", err))
			return fmt.Errorf("failed to add tables rules: %w", err)
		}
		metrics.TablesStatus = tables.DetectBackend(&cfg)
		metrics.RecordEvent("info", "Tables rules configured successfully")
	} else {
		log.Infof("Skipping tables setup (--skip-tables)")
		metrics.TablesStatus = "skipped"
	}

	// Ensure routing runtime state is applied at startup as well.
	if !cfg.System.Tables.SkipSetup {
		tproxyMgr.SyncConfig(&cfg)
		tables.RoutingSyncConfig(&cfg)
	} else {
		log.Tracef("Skipping routing sync due to --skip-tables")
	}

	// Start netfilter queue pool
	log.Infof("Starting netfilter queue pool (queue: %d, threads: %d)", cfg.Queue.StartNum, cfg.Queue.Threads)
	pool := nfq.NewPool(&cfg)
	if err := pool.Start(); err != nil {
		metrics.RecordEvent("error", fmt.Sprintf("NFQueue start failed: %v", err))
		metrics.NFQueueStatus = "error"
		return fmt.Errorf("netfilter queue start failed: %w", err)
	}

	metrics.RecordEvent("info", fmt.Sprintf("NFQueue started with %d threads", cfg.Queue.Threads))
	metrics.NFQueueStatus = "active"

	tproxyResolver.Set(pool.GetMatcher())

	// Start tables monitor to handle rule restoration if system wipes them
	var tablesMonitor *tables.Monitor
	if !cfg.System.Tables.SkipSetup && cfg.System.Tables.MonitorInterval > 0 {
		tablesMonitor = tables.NewMonitor(&cfgPtr)
		tablesMonitor.Start()
	}

	// Start internal web server if configured
	httpServer, err := b4http.StartServer(&cfgPtr, pool)
	if err != nil {
		metrics.RecordEvent("error", fmt.Sprintf("Failed to start web server: %v", err))
		return log.Errorf("failed to start web server: %w", err)
	}

	// Start SOCKS5 server if configured.
	socks5Server := socks5.NewServer(&cfg)
	socks5Server.SetIPBlockCache(pool.GetIPBlockCache())
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

	wd := watchdog.New(&cfgPtr, discoveryRT, func(c *config.Config) error {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("invalid configuration: %v", err)
		}
		if pool != nil {
			if err := pool.UpdateConfig(c); err != nil {
				return fmt.Errorf("failed to update pool config: %v", err)
			}
		}
		if err := c.SaveToFile(c.ConfigPath); err != nil {
			return fmt.Errorf("failed to save config: %v", err)
		}
		cfgPtr.Store(c)
		tproxyResolver.Set(pool.GetMatcher())
		tproxyMgr.SyncConfig(c)
		tables.RoutingSyncConfig(c)
		aiManager.Update(c.System.AI)
		return nil
	})
	wd.Start()
	handler.SetWatchdog(wd)

	log.Infof("B4 is running. Press Ctrl+C to stop")
	metrics.RecordEvent("info", "B4 is fully operational")

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	sig := <-sigChan

	log.Infof("Received signal: %v, shutting down gracefully", sig)
	metrics.RecordEvent("info", fmt.Sprintf("Shutdown initiated by signal: %v", sig))

	wd.Stop()
	tproxyMgr.Stop()

	// Perform graceful shutdown with timeout
	return gracefulShutdown(cfgPtr.Load(), pool, httpServer, socks5Server, mtprotoServer, metrics, discoveryRT)
}

func gracefulShutdown(cfg *config.Config, pool *nfq.Pool, httpServer *http.Server, socks5Server *socks5.Server, mtprotoServer *mtproto.Server, metrics *handler.MetricsCollector, discoveryRT *discovery.Runtime) error {
	// Create shutdown context with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
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
			if err := httpServer.Shutdown(shutdownCtx); err != nil {
				log.Errorf("HTTP server shutdown error: %v", err)
				shutdownErrors <- fmt.Errorf("HTTP shutdown: %w", err)
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
		discoveryRT.Stop(cfg, "")
	}

	// Stop NFQueue pool
	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Infof("Stopping netfilter queue pool...")
		metrics.NFQueueStatus = "stopping"

		// Use a goroutine with timeout for pool.Stop()
		stopDone := make(chan struct{})
		go func() {
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
	if !cfg.System.Tables.SkipSetup {
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

	tables.RoutingClearAll()

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
	candidates := []string{"/var/run/b4.pid", "/run/b4.pid"}
	var f *os.File
	var path string
	for _, p := range candidates {
		fp, err := os.OpenFile(p, os.O_CREATE|os.O_RDWR|syscall.O_NOFOLLOW, 0600)
		if err == nil {
			f = fp
			path = p
			break
		}
	}
	if f == nil {
		return nil, nil
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			fmt.Fprintf(os.Stderr, "[INIT] single-instance check skipped: flock(%s): %v\n", path, err)
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
		fmt.Fprintf(os.Stderr, "[INIT] could not update pidfile %s: %v\n", path, err)
	}

	cleanup := func() {
		syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
		os.Remove(path)
	}
	return cleanup, nil
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

func initMemoryLimit() {
	if os.Getenv("GOMEMLIMIT") != "" {
		return
	}

	var info syscall.Sysinfo_t
	if err := syscall.Sysinfo(&info); err != nil {
		return
	}

	totalRAM := uint64(info.Totalram) * uint64(info.Unit)

	limit := int64(totalRAM / 2)

	const minLimit = 32 * 1024 * 1024
	if limit < minLimit {
		limit = minLimit
	}

	debug.SetMemoryLimit(limit)
	fmt.Fprintf(os.Stderr, "[INIT] Memory limit set to %d MB (total RAM: %d MB)\n",
		limit/(1024*1024), totalRAM/(1024*1024))
}

func initTimezone() {
	// Apply TZ env var if set; otherwise keep Go's default (system timezone from /etc/localtime)
	if tzName := os.Getenv("TZ"); tzName != "" {
		config.ApplyTimezone(tzName)
	}
}

func initLogging(cfg *config.Config) error {

	fmt.Fprintf(os.Stderr, "[INIT] Logging initialized at level %d\n", cfg.System.Logging.Level)

	if cfg.System.Logging.Syslog {
		if err := log.EnableSyslog("b4"); err != nil {
			log.Warnf("Syslog unavailable, continuing without it: %v", err)
			cfg.System.Logging.Syslog = false
		} else {
			log.Infof("Syslog enabled")
		}
	}

	if cfg.System.Logging.ErrorFile != "" {
		if err := log.InitErrorFile(cfg.System.Logging.ErrorFile); err != nil {
			log.Errorf("Failed to open error log file: %v", err)
		} else {
			log.Infof("Error logging to file: %s", cfg.System.Logging.ErrorFile)
		}
	}

	w := io.MultiWriter(log.OrigStderr(), b4http.LogWriter())
	log.Init(w, log.Level(cfg.System.Logging.Level), cfg.System.Logging.Instaflush)

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
