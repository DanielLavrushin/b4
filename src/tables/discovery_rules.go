package tables

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var (
	ErrDiscoveryDSCPPending   = errors.New("b4 is still applying its DSCP rules, so the run would not take the path the set's traffic takes; start it again once they are in place")
	errDiscoveryDSCPRewritten = errors.New("b4 rewrote its DSCP rules during the run, so the path the set's traffic takes may have changed")
)

type discoveryBackend interface {
	name() string
	available() bool
	takesSetValues(v6 bool) bool
	apply(flowMark uint, injectedMark uint, queueStart int, threads int, stamp *discoveryStamp) error
	clear(flowMark uint, injectedMark uint)
}

type discoveryStamp struct {
	value  int
	ifaces []string
	v4     bool
	v6     bool
}

func (s *discoveryStamp) covers(v6 bool) bool {
	return s != nil && ((v6 && s.v6) || (!v6 && s.v4))
}

func discoveryCheckDSCP(dscp *config.DSCPProbe) error {
	if dscp == nil {
		return nil
	}
	if dscp.Value < 0 || dscp.Value > config.MaxDSCPValue {
		return fmt.Errorf("DSCP %d for Discovery's probes is outside 0-%d", dscp.Value, config.MaxDSCPValue)
	}
	if st := dscpApplied.Load(); (st != nil && st.pending) || dscpSyncQueued() {
		return ErrDiscoveryDSCPPending
	}
	return nil
}

func discoveryStampFor(cfg *config.Config, dscp *config.DSCPProbe, be discoveryBackend) *discoveryStamp {
	if dscp == nil {
		return nil
	}
	if st := dscpApplied.Load(); st != nil && st.cfg != nil {
		cfg = st.cfg
	}
	stamp := &discoveryStamp{
		value:  dscp.Value,
		ifaces: cfg.DSCPInterfaces(),
		v4:     dscp.IPv4 && be.takesSetValues(false),
		v6:     dscp.IPv6 && be.takesSetValues(true),
	}
	if !stamp.v4 && !stamp.v6 {
		return nil
	}
	return stamp
}

func getDiscoveryBackend(cfg *config.Config) discoveryBackend {
	return discoveryBackendFor(detectFirewallBackend(cfg))
}

func currentDiscoveryBackend(cfg *config.Config) discoveryBackend {
	if rulesAppliedBackend != "" {
		return discoveryBackendFor(rulesAppliedBackend)
	}
	return getDiscoveryBackend(cfg)
}

func discoveryBackendFor(backend string) discoveryBackend {
	nft := &discoveryNftBackend{}
	ipt := &discoveryIptBackend{legacy: backend == backendIPTablesLegacy}

	switch backend {
	case backendNFTables:
		if nft.available() {
			return nft
		}
	default:
		if ipt.available() {
			return ipt
		}
	}

	if nft.available() {
		return nft
	}
	if ipt.available() {
		return ipt
	}
	return nil
}

type discoverySteering struct {
	cfg          *config.Config
	backend      discoveryBackend
	flowMark     uint
	injectedMark uint
	queueStart   int
	threads      int
	dscp         *config.DSCPProbe
	stamp        *discoveryStamp
	lost         func(error)
	failed       atomic.Bool
}

var (
	activeSteering *discoverySteering
	discoveryRun   atomic.Pointer[discoverySteering]
)

func ApplyDiscoverySteeringRules(cfg *config.Config, flowMark uint, injectedMark uint, queueStart int, threads int, dscp *config.DSCPProbe, lost func(error)) error {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	if err := discoveryCheckDSCP(dscp); err != nil {
		return err
	}
	be := currentDiscoveryBackend(cfg)
	if be == nil {
		return fmt.Errorf("no discovery firewall backend available")
	}
	stamp := discoveryStampFor(cfg, dscp, be)
	if err := be.apply(flowMark, injectedMark, queueStart, threads, stamp); err != nil {
		be.clear(flowMark, injectedMark)
		return err
	}
	s := &discoverySteering{cfg: cfg, backend: be, flowMark: flowMark, injectedMark: injectedMark, queueStart: queueStart, threads: threads, dscp: dscp, stamp: stamp, lost: lost}
	activeSteering = s
	discoveryRun.Store(s)
	return nil
}

func ActiveDiscoveryDSCP() *int {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	if s := activeSteering; s != nil && s.stamp != nil {
		value := s.stamp.value
		return &value
	}
	return nil
}

func ClearDiscoverySteeringRules(cfg *config.Config, flowMark uint, injectedMark uint) {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	var be discoveryBackend
	if s := activeSteering; s != nil {
		be = s.backend
	}
	activeSteering = nil
	discoveryRun.Store(nil)
	if be == nil {
		be = currentDiscoveryBackend(cfg)
	}
	if be == nil {
		return
	}
	be.clear(flowMark, injectedMark)
}

func discoveryDSCPRewritten() {
	if s := discoveryRun.Load(); s != nil && s.dscp != nil {
		s.fail(errDiscoveryDSCPRewritten)
	}
}

func DiscoveryRunInvalidated() bool {
	s := discoveryRun.Load()
	return s != nil && s.failed.Load()
}

func reapplyDiscoverySteering() {
	s := activeSteering
	if s == nil {
		return
	}
	be := currentDiscoveryBackend(s.cfg)
	if be == nil {
		s.fail(fmt.Errorf("no discovery firewall backend available"))
		return
	}
	if s.backend != nil && s.backend.name() != be.name() {
		s.backend.clear(s.flowMark, s.injectedMark)
	}
	s.backend = be
	s.stamp = discoveryStampFor(s.cfg, s.dscp, be)
	if err := be.apply(s.flowMark, s.injectedMark, s.queueStart, s.threads, s.stamp); err != nil {
		s.fail(err)
	}
}

func (s *discoverySteering) fail(err error) {
	if !s.failed.CompareAndSwap(false, true) {
		return
	}
	log.Tracef("Discovery: the run's firewall rules changed or could not be put back: %v", err)
	if s.lost != nil {
		go s.lost(err)
	}
}
