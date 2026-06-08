package discovery

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/tables"
)

var ErrDiscoveryAlreadyRunning = errors.New("discovery is already running")

type runtimeState struct {
	pool              *nfq.Pool
	discoveryStartNum int
	discoveryThreads  int
	discoveryFlowMark uint
	discoveryInjMark  uint
	activeSuiteID     string
	stopping          bool
}

type StartResult struct {
	Pool     *nfq.Pool
	FlowMark uint
}

type StartSuiteOptions struct {
	SkipDNS         bool
	SkipCache       bool
	PayloadFiles    []string
	ValidationTries int
	TLSVersion      string
	IPVersion       string
}

type Runtime struct {
	mu    sync.Mutex
	state *runtimeState
}

func NewRuntime() *Runtime {
	return &Runtime{}
}

func (m *Runtime) IsActive() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state != nil
}

func (m *Runtime) Start(cfg *config.Config) (*StartResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.state != nil {
		return nil, ErrDiscoveryAlreadyRunning
	}

	mainStart := cfg.Queue.StartNum
	mainThreads := cfg.Queue.Threads
	discoveryThreads := 1
	discoveryStart := mainStart + mainThreads
	discoveryEnd := discoveryStart + discoveryThreads - 1
	if discoveryStart < 0 || discoveryEnd > 65535 {
		return nil, fmt.Errorf("discovery queue range is out of bounds: %d-%d", discoveryStart, discoveryEnd)
	}

	flowMark := cfg.DiscoveryFlowMark()
	injectedMark := cfg.DiscoveryInjectedMark()

	log.Infof("Discovery queue range: main=%d-%d discovery=%d-%d", mainStart, mainStart+mainThreads-1, discoveryStart, discoveryEnd)
	log.Infof("Discovery marks: main_injected=0x%x discovery_flow=0x%x discovery_injected=0x%x", cfg.MainInjectedMark(), flowMark, injectedMark)

	if err := tables.ApplyDiscoverySteeringRules(cfg, flowMark, injectedMark, discoveryStart, discoveryThreads); err != nil {
		return nil, fmt.Errorf("failed to apply discovery steering rules: %w", err)
	}

	discoveryCfg := cfg.Clone()
	discoveryCfg.Queue.StartNum = discoveryStart
	discoveryCfg.Queue.Threads = discoveryThreads
	discoveryCfg.Queue.Mark = injectedMark
	discoveryCfg.Queue.IsDiscovery = true
	discoveryCfg.System.Tables.SkipSetup = true

	pool := nfq.NewPool(discoveryCfg)
	if err := pool.Start(); err != nil {
		tables.ClearDiscoverySteeringRules(cfg, flowMark, injectedMark)
		return nil, fmt.Errorf("failed to start discovery pool: %w", err)
	}

	m.state = &runtimeState{
		pool:              pool,
		discoveryStartNum: discoveryStart,
		discoveryThreads:  discoveryThreads,
		discoveryFlowMark: flowMark,
		discoveryInjMark:  injectedMark,
	}

	return &StartResult{
		Pool:     pool,
		FlowMark: flowMark,
	}, nil
}

func (m *Runtime) SetActiveSuiteID(suiteID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state != nil {
		m.state.activeSuiteID = suiteID
	}
}

func (m *Runtime) StartSuite(cfg *config.Config, urls []string, opts StartSuiteOptions) (*DiscoverySuite, error) {
	runtimeState, err := m.Start(cfg)
	if err != nil {
		return nil, err
	}

	suite := NewDiscoverySuite(
		urls,
		runtimeState.Pool,
		opts.SkipDNS,
		opts.SkipCache,
		opts.PayloadFiles,
		opts.ValidationTries,
		opts.TLSVersion,
		opts.IPVersion,
		runtimeState.FlowMark,
	)
	m.SetActiveSuiteID(suite.Id)
	RegisterSuite(suite.CheckSuite)

	go func() {
		suite.RunDiscovery()
		log.Infof("Discovery complete for %d domains", len(suite.Domains))
		m.Stop(cfg, suite.Id)
	}()

	return suite, nil
}

func (m *Runtime) Stop(cfg *config.Config, suiteID string) {
	m.mu.Lock()
	state := m.state
	if state == nil || state.stopping {
		m.mu.Unlock()
		return
	}
	if suiteID != "" && state.activeSuiteID != "" && state.activeSuiteID != suiteID {
		m.mu.Unlock()
		return
	}
	state.stopping = true
	activeSuite := state.activeSuiteID
	m.mu.Unlock()

	if activeSuite != "" {
		CancelCheckSuite(activeSuite)
		time.Sleep(500 * time.Millisecond)
	}

	state.pool.Stop()
	tables.ClearDiscoverySteeringRules(cfg, state.discoveryFlowMark, state.discoveryInjMark)
	log.Infof("Discovery runtime stopped: queue=%d-%d", state.discoveryStartNum, state.discoveryStartNum+state.discoveryThreads-1)

	m.mu.Lock()
	m.state = nil
	m.mu.Unlock()
}
