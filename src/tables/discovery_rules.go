package tables

import (
	"errors"
	"fmt"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var (
	ErrDiscoveryDSCPPending   = errors.New("b4 is still applying its DSCP rules, which decide the network path of the run's probes; start it again once they are in place")
	errDiscoveryDSCPRewritten = errors.New("b4 rewrote its DSCP rules during the run, so the network path of its probes may have changed")
)

type discoveryBackend interface {
	name() string
	available() bool
	takesSetValues(cfg *config.Config, v6 bool) (bool, error)
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
	if dscp != nil && (dscp.Value < 0 || dscp.Value > config.MaxDSCPValue) {
		return fmt.Errorf("DSCP %d for Discovery's probes is outside 0-%d", dscp.Value, config.MaxDSCPValue)
	}
	if dscp == nil && !dscpInPlay(dscpSyncWanted.Load()) {
		return nil
	}
	if st := dscpApplied.Load(); (st != nil && st.pending) || dscpSyncQueued() {
		return ErrDiscoveryDSCPPending
	}
	return nil
}

func dscpInPlay(next *config.Config) bool {
	return dscpApplied.Load() != nil || dscpStale.Load() != nil || dscpWanted(next)
}

func discoveryStampFor(cfg *config.Config, dscp *config.DSCPProbe, be discoveryBackend) (*discoveryStamp, error) {
	if dscp == nil {
		return nil, nil
	}
	if st := dscpApplied.Load(); st != nil && st.cfg != nil {
		cfg = st.cfg
	}
	stamp := &discoveryStamp{value: dscp.Value, ifaces: cfg.DSCPInterfaces()}
	for _, family := range []struct {
		v6     bool
		wanted bool
		takes  *bool
	}{{false, dscp.IPv4, &stamp.v4}, {true, dscp.IPv6, &stamp.v6}} {
		if !family.wanted {
			continue
		}
		takes, err := be.takesSetValues(cfg, family.v6)
		if err != nil {
			return nil, err
		}
		*family.takes = takes
	}
	if !stamp.v4 && !stamp.v6 {
		return nil, nil
	}
	return stamp, nil
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
	stamp, err := discoveryStampFor(cfg, dscp, be)
	if err != nil {
		return err
	}
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

func discoveryDSCPRewritten(next *config.Config) {
	s := discoveryRun.Load()
	if s == nil {
		return
	}
	if s.dscp != nil || dscpInPlay(next) {
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
	stamp, err := discoveryStampFor(s.cfg, s.dscp, be)
	if err != nil {
		s.fail(err)
	}
	s.stamp = stamp
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
