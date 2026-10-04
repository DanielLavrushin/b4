package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/tables"
	"github.com/daniellavrushin/b4/tproxy"
	b4tun "github.com/daniellavrushin/b4/tun"
)

const engineRetryEnv = "B4_ENGINE_RETRY"

var (
	engineRetryDelays = []time.Duration{15 * time.Second, 30 * time.Second, 60 * time.Second}
	engineAttempt     int
)

func loadEngineAttempt() int {
	raw, ok := os.LookupEnv(engineRetryEnv)
	if !ok {
		return 0
	}
	_ = os.Unsetenv(engineRetryEnv)
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return min(n, len(engineRetryDelays))
}

func engineMode(cfg *config.Config) string {
	if cfg.Queue.Mode == "tun" {
		return metrics.EngineModeTUN
	}
	return metrics.EngineModeNFQueue
}

func engineInfo(cfg *config.Config, pool *nfq.Pool, state, firewall string) metrics.EngineInfo {
	threads := cfg.Queue.Threads
	if pool != nil && len(pool.Workers) > 0 {
		threads = len(pool.Workers)
	}
	return metrics.EngineInfo{State: state, Mode: engineMode(cfg), Threads: threads, Firewall: firewall}
}

func firewallName(cfg *config.Config) string {
	if cfg.System.Tables.SkipSetup {
		return metrics.FirewallExternal
	}
	return tables.DetectBackend(cfg)
}

func setEngineFirewall(mc *metrics.MetricsCollector, firewall string) {
	info := mc.Engine()
	info.Firewall = firewall
	mc.SetEngine(info)
}

func engineLabel(mode string) string {
	if mode == metrics.EngineModeTUN {
		return "TUN"
	}
	return "NFQUEUE"
}

func startTUNEngine(cfg *config.Config, pool *nfq.Pool, tproxyMgr *tproxy.Manager, mc *metrics.MetricsCollector) (*b4tun.Engine, error) {
	log.Infof("Starting TUN engine (device: %s, out: %s, threads: %d)",
		cfg.Queue.TUN.DeviceName, cfg.Queue.TUN.OutInterface, cfg.Queue.Threads)
	tables.SetTUNDevice(cfg.Queue.TUN.Device())

	skipTables := cfg.System.Tables.SkipSetup
	if !skipTables {
		log.Tracef("Clearing any pre-existing NFQUEUE/tables rules before TUN setup")
		if err := tables.ClearRules(cfg); err != nil {
			log.Warnf("TUN: failed to clear pre-existing tables rules (continuing): %v", err)
		}
		if err := tables.ApplyMasqueradeOnly(cfg); err != nil {
			tables.ClearTUNFirewall(cfg)
			return nil, fmt.Errorf("failed to apply masquerade: %w", err)
		}
		tables.ApplyConntrackSysctls()
		if err := tables.ApplyMSSClampOnly(cfg); err != nil {
			log.Errorf("Failed to apply MSS clamp in TUN mode: %v", err)
		}
		if err := tables.ApplyDSCPOnly(cfg); err != nil {
			log.Errorf("Failed to apply the DSCP stamp in TUN mode: %v", err)
		}
	} else {
		log.Infof("Skipping masquerade and conntrack sysctls (--skip-tables); the TUN engine also skips its own firewall/sysctl rules and only sets up routing")
	}

	tunEngine := b4tun.NewEngine(cfg, pool)
	if err := tunEngine.Start(); err != nil {
		for _, w := range pool.Workers {
			w.Stop()
		}
		if !skipTables {
			tables.ClearTUNFirewall(cfg)
			tables.RevertConntrackSysctls()
		}
		return nil, fmt.Errorf("TUN engine start failed: %w", err)
	}

	mc.SetEngine(engineInfo(cfg, pool, metrics.EngineRunning, firewallName(cfg)))

	if name := tunEngine.DeviceName(); name != cfg.Queue.TUN.Device() {
		tables.SetTUNDevice(name)
		if !skipTables {
			if err := tables.ApplyMasqueradeOnly(cfg); err != nil {
				log.Errorf("Failed to re-apply masquerade for TUN device %s: %v", name, err)
			}
		}
	}

	if !skipTables {
		tproxyMgr.SyncConfig(cfg)
		tables.RoutingSyncConfig(cfg)
	}
	return tunEngine, nil
}

func startNFQueueEngine(cfg *config.Config, pool *nfq.Pool, tproxyMgr *tproxy.Manager, mc *metrics.MetricsCollector) error {
	skipTables := cfg.System.Tables.SkipSetup
	if !skipTables {
		log.Tracef("Clearing existing iptables/nftables rules")
		tables.ClearRules(cfg)
		b4tun.ClearStaleArtifacts(cfg)

		log.Tracef("Adding tables rules")
		if err := tables.AddRules(cfg); err != nil {
			tables.ClearRules(cfg)
			return fmt.Errorf("failed to add tables rules: %w", err)
		}
	} else {
		log.Infof("Skipping tables setup (--skip-tables)")
	}
	firewall := firewallName(cfg)
	mc.SetEngine(engineInfo(cfg, pool, metrics.EngineStarting, firewall))

	if !skipTables {
		tproxyMgr.SyncConfig(cfg)
		tables.RoutingSyncConfig(cfg)
	} else {
		log.Tracef("Skipping routing sync due to --skip-tables")
	}

	log.Infof("Starting netfilter queue pool (queue: %d, threads: %d)", cfg.Queue.StartNum, cfg.Queue.Threads)
	if err := pool.Start(); err != nil {
		if !skipTables {
			tables.ClearRules(cfg)
		}
		return fmt.Errorf("netfilter queue start failed: %w", err)
	}

	mc.SetEngine(engineInfo(cfg, pool, metrics.EngineRunning, firewall))
	return nil
}

func enterDegradedMode(cfg *config.Config, cause error, attempt int, mc *metrics.MetricsCollector) *time.Timer {
	mode := engineMode(cfg)
	label := engineLabel(mode)
	log.Errorf("%s engine did not start: %v. b4 keeps running without it: traffic flows past b4 unprocessed, and the web interface stays up to switch the engine mode or fix the cause", label, cause)
	mc.SetEngineState(metrics.EngineFailed)
	mc.Event(metrics.LevelError, metrics.EventEngineFailed, map[string]string{"engine": mode, "error": cause.Error()},
		fmt.Sprintf("%s engine did not start: %v", label, cause))

	failure := &metrics.EngineFailure{Mode: mode, Error: cause.Error()}
	var retry *time.Timer
	if attempt < len(engineRetryDelays) {
		delay := engineRetryDelays[attempt]
		failure.RetryAt = time.Now().Add(delay).UnixMilli()
		failure.RetriesLeft = len(engineRetryDelays) - attempt
		log.Infof("Automatic engine retry %d of %d in %s", attempt+1, len(engineRetryDelays), delay)
		retry = time.AfterFunc(delay, func() { requestRestart(restartEngineRetry) })
	} else {
		log.Infof("No automatic engine retries left: b4 stays without a packet engine until it restarts")
	}
	mc.SetEngineFailure(failure)
	return retry
}
