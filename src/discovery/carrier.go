package discovery

import (
	"fmt"
	"slices"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/tables"
)

type CarrierRequest struct {
	FlowMark     uint
	InjectedMark uint
	Worker       *nfq.Worker
	Lost         func(error)
}

type Carrier interface {
	Stop()
}

type CarrierFactory func(CarrierRequest) (Carrier, error)

func (m *Runtime) SetEngineTUN(on bool) {
	m.tunEngine.Store(on)
}

func (m *Runtime) usesTUNCarrier() bool {
	return m.tunEngine.Load()
}

func (m *Runtime) SetTUNCarrier(factory CarrierFactory) {
	m.mu.Lock()
	m.carrier = factory
	m.mu.Unlock()
}

var watchCarrierDSCP = tables.WatchCarrierDiscoveryDSCP

var newCarrierPool = func(cfg *config.Config) (*nfq.Pool, error) {
	pool := nfq.NewPool(cfg)
	for _, w := range pool.Workers {
		if err := w.InitSender(); err != nil {
			pool.Stop()
			return nil, err
		}
	}
	pool.SetTUNLocalOnly(true)
	return pool, nil
}

func (m *Runtime) startCarrier(cfg *config.Config, dscp *config.DSCPProbe) (*StartResult, error) {
	if dscp != nil {
		return nil, fmt.Errorf("%w (DSCP %d)", ErrDSCPUnsupportedTUN, dscp.Value)
	}
	factory := m.carrier
	if factory == nil {
		return nil, fmt.Errorf("%w: the TUN engine is not running", ErrTUNCarrierUnavailable)
	}

	flowMark := cfg.DiscoveryFlowMark()
	injectedMark := cfg.DiscoveryInjectedMark()
	threads := 1
	start := cfg.Queue.StartNum + cfg.Queue.Threads
	log.Infof("Discovery marks: main_injected=0x%x discovery_flow=0x%x discovery_injected=0x%x, carried through the TUN engine without a queue", cfg.MainInjectedMark(), flowMark, injectedMark)

	state := &runtimeState{
		discoveryStartNum: start,
		discoveryThreads:  threads,
		discoveryFlowMark: flowMark,
		discoveryInjMark:  injectedMark,
		carrier:           true,
	}
	lost := func(err error) { m.rulesLost(state, err) }
	path, release, err := watchCarrierDSCP(cfg, lost)
	if err != nil {
		return nil, err
	}

	poolCfg := discoveryPoolConfig(cfg, start, threads, flowMark, injectedMark)
	poolCfg.Queue.Mode = "tun"
	pool, err := newCarrierPool(poolCfg)
	if err != nil {
		release()
		return nil, fmt.Errorf("%w: failed to start discovery pool: %w", ErrTUNCarrierUnavailable, err)
	}
	if len(pool.Workers) == 0 {
		pool.Stop()
		release()
		return nil, fmt.Errorf("%w: the discovery pool has no worker", ErrTUNCarrierUnavailable)
	}

	carrier, err := factory(CarrierRequest{
		FlowMark:     flowMark,
		InjectedMark: injectedMark,
		Worker:       pool.Workers[0],
		Lost:         lost,
	})
	if err != nil {
		pool.Stop()
		release()
		return nil, fmt.Errorf("%w: %w", ErrTUNCarrierUnavailable, err)
	}

	state.pool = pool
	state.clearRules = func() {
		carrier.Stop()
		release()
	}
	m.state = state
	return &StartResult{Pool: pool, FlowMark: flowMark, Path: path, Carrier: true}, nil
}

func noteCarrierRun() {
	log.DiscoveryLogf("Probes go through the TUN engine's Discovery carrier: IPv4 only, and replies to this router's own connections never reach it, so strategies that act on replies (an incoming mode, RST protection) cannot be tested")
}

func carrierUntestable(set *config.SetConfig) string {
	if set == nil {
		return ""
	}
	var parts []string
	if set.TCP.Incoming.Mode != "" && set.TCP.Incoming.Mode != config.ConfigOff {
		parts = append(parts, "an incoming mode ("+set.TCP.Incoming.Mode+")")
	}
	if set.TCP.RSTProtection.Enabled {
		parts = append(parts, "RST protection")
	}
	return strings.Join(parts, " and ")
}

func carrierPoolConfig(cfg *config.Config) *config.Config {
	if cfg == nil || !slices.ContainsFunc(cfg.Sets, func(set *config.SetConfig) bool { return carrierUntestable(set) != "" }) {
		return cfg
	}
	out := *cfg
	out.Sets = make([]*config.SetConfig, len(cfg.Sets))
	for i, set := range cfg.Sets {
		if carrierUntestable(set) == "" {
			out.Sets[i] = set
			continue
		}
		stripped := *set
		stripped.TCP.Incoming.Mode = config.ConfigOff
		stripped.TCP.RSTProtection.Enabled = false
		out.Sets[i] = &stripped
	}
	return &out
}

func (ds *DiscoverySuite) applyPoolConfig(cfg *config.Config) error {
	if ds.carrier {
		cfg = carrierPoolConfig(cfg)
	}
	return ds.pool.UpdateConfig(cfg)
}

func (ds *DiscoverySuite) skipOnCarrier(preset ConfigPreset) bool {
	if !ds.carrier {
		return false
	}
	why := carrierUntestable(&preset.Config)
	if why == "" {
		return false
	}
	if preset.Name == presetSetCurrent {
		log.DiscoveryLogf("  '%s' is tested without %s: replies to this router's own connections never reach the TUN carrier; if it passes, the set keeps them, and a replacement strategy found instead brings no incoming mode", preset.Name, why)
		return false
	}
	log.DiscoveryLogf("  Skipping '%s': it uses %s, which acts on replies, and replies to this router's own connections never reach the TUN carrier", preset.Name, why)
	return true
}

func carrierSkipped(domain string) CheckResult {
	return CheckResult{
		Domain:  domain,
		Status:  CheckStatusFailed,
		Error:   "acts on replies, which the TUN carrier does not see, not tried",
		untried: true,
	}
}
