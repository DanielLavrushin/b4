package discovery

import (
	"errors"
	"fmt"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/tables"
)

var (
	ErrDiscoveryAlreadyRunning = errors.New("discovery is already running")
	ErrDSCPRulesPending        = tables.ErrDiscoveryDSCPPending
)

type poolStopper interface {
	Stop()
}

type runtimeState struct {
	pool              poolStopper
	clearRules        func()
	discoveryStartNum int
	discoveryThreads  int
	discoveryFlowMark uint
	discoveryInjMark  uint
	activeSuiteID     string
	stopping          bool
	lostErr           error
	wg                sync.WaitGroup
}

type StartResult struct {
	Pool     *nfq.Pool
	FlowMark uint
	DSCP     *config.DSCPProbe
	Path     *config.DSCPPath
}

type StartSuiteOptions struct {
	SkipDNS         bool
	SkipCache       bool
	PayloadFiles    []string
	ValidationTries int
	TLSVersion      string
	IPVersion       string
	Source          string
	SetId           string
	SetStrategy     *config.SetConfig
	DSCP            *config.DSCPProbe
	StopWhenCovered bool
	HubPresets      func() []ConfigPreset
	DNSServer       string
	Pins            map[string][]string
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

func (m *Runtime) Start(cfg *config.Config, dscp *config.DSCPProbe) (*StartResult, error) {
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

	state := &runtimeState{
		discoveryStartNum: discoveryStart,
		discoveryThreads:  discoveryThreads,
		discoveryFlowMark: flowMark,
		discoveryInjMark:  injectedMark,
	}
	lost := func(err error) { m.rulesLost(state, err) }
	if err := tables.ApplyDiscoverySteeringRules(cfg, flowMark, injectedMark, discoveryStart, discoveryThreads, dscp, lost); err != nil {
		return nil, fmt.Errorf("failed to apply discovery steering rules: %w", err)
	}
	stamped, path := tables.ActiveDiscoveryDSCP()
	if stamped != nil {
		log.Infof("Discovery probes leave with DSCP %d", stamped.Value)
	}

	discoveryCfg := discoveryPoolConfig(cfg, discoveryStart, discoveryThreads, flowMark, injectedMark)

	pool := nfq.NewPool(discoveryCfg)
	if err := pool.Start(); err != nil {
		tables.ClearDiscoverySteeringRules(cfg, flowMark, injectedMark)
		return nil, fmt.Errorf("failed to start discovery pool: %w", err)
	}

	state.pool = pool
	state.clearRules = func() { tables.ClearDiscoverySteeringRules(cfg, flowMark, injectedMark) }
	m.state = state

	return &StartResult{
		Pool:     pool,
		FlowMark: flowMark,
		DSCP:     stamped,
		Path:     path,
	}, nil
}

func (m *Runtime) rulesLost(state *runtimeState, err error) {
	m.mu.Lock()
	if m.state != state || state.stopping {
		m.mu.Unlock()
		return
	}
	suiteID := state.activeSuiteID
	if suiteID == "" {
		if state.lostErr == nil {
			state.lostErr = err
		}
		m.mu.Unlock()
		return
	}
	m.mu.Unlock()
	stopForLostRules(suiteID, err)
}

func stopForLostRules(suiteID string, err error) {
	log.Warnf("Discovery: stopping run %s, its firewall rules changed or could not be put back after a firewall rebuild: %v", suiteID, err)
	log.DiscoveryLogf("Stopped: the firewall rules this run depends on changed or could not be put back after a firewall rebuild (%v), so its results could come from another network path and none of them can be applied", err)
	cancelCheckSuite(suiteID, true)
}

func discoveryPoolConfig(cfg *config.Config, start, threads int, flowMark, injectedMark uint) *config.Config {
	discoveryCfg := cfg.Clone()
	discoveryCfg.Queue.StartNum = start
	discoveryCfg.Queue.Threads = threads
	discoveryCfg.Queue.Mark = injectedMark
	discoveryCfg.Queue.IsDiscovery = true
	discoveryCfg.System.Tables.SkipSetup = true
	discoveryCfg.System.Checker.DiscoveryFlowMark = flowMark
	discoveryCfg.System.Checker.DiscoveryInjectedMark = injectedMark

	for _, set := range discoveryCfg.Sets {
		set.DNS = config.DNSConfig{}
	}
	return discoveryCfg
}

func (m *Runtime) activateSuite(suite *CheckSuite) {
	m.mu.Lock()
	var lost error
	if m.state != nil {
		m.state.activeSuiteID = suite.Id
		lost = m.state.lostErr
	}
	RegisterSuite(suite)
	m.mu.Unlock()
	if lost != nil {
		stopForLostRules(suite.Id, lost)
	}
}

func probeForRun(dscp *config.DSCPProbe, ipVersion string) *config.DSCPProbe {
	if dscp == nil {
		return nil
	}
	return &config.DSCPProbe{Value: dscp.Value, IPv4: ipVersion != "ipv6", IPv6: ipVersion != "ipv4"}
}

func (s *CheckSuite) recordStamp(asked, stamped *config.DSCPProbe, path *config.DSCPPath) {
	s.DSCPPath = path
	if stamped == nil {
		return
	}
	value := stamped.Value
	s.DSCP = &value
	if asked != nil && asked.IPv4 && asked.IPv6 && stamped.IPv4 != stamped.IPv6 {
		s.DSCPIPVersion = "6"
		if stamped.IPv4 {
			s.DSCPIPVersion = "4"
		}
	}
}

func (m *Runtime) StartSuite(cfg *config.Config, urls []string, opts StartSuiteOptions) (*DiscoverySuite, error) {
	dscp := probeForRun(opts.DSCP, opts.IPVersion)
	runtimeState, err := m.Start(cfg, dscp)
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
	suite.hubPresetsFn = opts.HubPresets
	suite.Source = opts.Source
	if suite.Source == "" {
		suite.Source = SourceWeb
	}
	suite.SetId = opts.SetId
	suite.recordStamp(dscp, runtimeState.DSCP, runtimeState.Path)
	suite.setStrategy = opts.SetStrategy
	suite.stopWhenCovered = opts.StopWhenCovered
	suite.dnsServerOverride = opts.DNSServer
	suite.runPins = opts.Pins

	log.GetDiscoveryHub().Reset()
	m.activateSuite(suite.CheckSuite)

	m.launchSuite(suite.Id, func() {
		suite.RunDiscovery()
		log.Infof("Discovery complete for %d domains", len(suite.Domains))
	})

	return suite, nil
}

func (m *Runtime) launchSuite(suiteID string, run func()) {
	m.mu.Lock()
	state := m.state
	if state == nil || state.stopping {
		m.mu.Unlock()
		return
	}
	state.wg.Add(1)
	m.mu.Unlock()

	go func() {
		defer m.Stop(suiteID)
		defer state.wg.Done()
		run()
	}()
}

func (m *Runtime) Stop(suiteID string) {
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
	}

	state.wg.Wait()

	state.pool.Stop()
	if state.clearRules != nil {
		state.clearRules()
	}
	log.Infof("Discovery runtime stopped: queue=%d-%d", state.discoveryStartNum, state.discoveryStartNum+state.discoveryThreads-1)

	m.mu.Lock()
	m.state = nil
	m.mu.Unlock()
}
