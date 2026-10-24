package tables

import (
	"errors"
	"fmt"
	"slices"
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
	rest4  *int
	rest6  *int
}

func (s *discoveryStamp) covers(v6 bool) bool {
	return s != nil && ((v6 && s.v6) || (!v6 && s.v4))
}

func (s *discoveryStamp) write(v6 bool) (int, bool) {
	if s.covers(v6) {
		return s.value, true
	}
	if s == nil {
		return 0, false
	}
	rest := s.rest4
	if v6 {
		rest = s.rest6
	}
	if rest == nil {
		return 0, false
	}
	return *rest, true
}

func (s *discoveryStamp) probe() *config.DSCPProbe {
	if s == nil || (!s.v4 && !s.v6) {
		return nil
	}
	return &config.DSCPProbe{Value: s.value, IPv4: s.v4, IPv6: s.v6}
}

func discoveryCheckDSCP(dscp *config.DSCPProbe) error {
	if dscp != nil && (dscp.Value < 0 || dscp.Value > config.MaxDSCPValue) {
		return fmt.Errorf("DSCP %d for Discovery's probes is outside 0-%d", dscp.Value, config.MaxDSCPValue)
	}
	if dscp == nil && !dscpInPlay(dscpSyncWanted.Load()) {
		return nil
	}
	if st := dscpApplied.Load(); (st != nil && st.pending) || dscpStale.Load() != nil || dscpSyncQueued() {
		return ErrDiscoveryDSCPPending
	}
	return nil
}

func dscpInPlay(next *config.Config) bool {
	return dscpApplied.Load() != nil || dscpStale.Load() != nil || dscpWanted(next)
}

func discoveryStampFor(cfg *config.Config, dscp *config.DSCPProbe, be discoveryBackend) (*discoveryStamp, error) {
	st := dscpApplied.Load()
	if st != nil && st.cfg != nil {
		cfg = st.cfg
	}
	stamp := &discoveryStamp{ifaces: cfg.DSCPInterfaces()}
	if dscp != nil {
		stamp.value = dscp.Value
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
	}
	if st.perSet() {
		for _, family := range []struct {
			v6   bool
			rest **int
		}{{false, &stamp.rest4}, {true, &stamp.rest6}} {
			if stamp.covers(family.v6) || !dscpFamilyStamped(st, family.v6) {
				continue
			}
			value, _ := dscpGlobalOn(st, family.v6)
			*family.rest = &value
		}
	}
	if !stamp.v4 && !stamp.v6 && stamp.rest4 == nil && stamp.rest6 == nil {
		return nil, nil
	}
	return stamp, nil
}

func dscpFamilyStamped(st *dscpState, v6 bool) bool {
	if st == nil {
		return false
	}
	if st.backend == backendNFTables {
		return true
	}
	for _, bin := range st.bins {
		if dscpIptBinV6(bin) == v6 {
			return true
		}
	}
	return false
}

func dscpGlobalOn(st *dscpState, v6 bool) (int, bool) {
	if st == nil || st.cfg == nil || !dscpFamilyStamped(st, v6) {
		return 0, false
	}
	value, _, on := st.cfg.DSCPStamp()
	if !on {
		return 0, false
	}
	return value, true
}

func discoveryPathFor(cfg *config.Config, stamp *discoveryStamp) *config.DSCPPath {
	st := dscpApplied.Load()
	if st != nil && st.cfg != nil {
		cfg = st.cfg
	}
	path := &config.DSCPPath{}
	for _, f := range []struct {
		v6    bool
		value **int
	}{{false, &path.IPv4}, {true, &path.IPv6}} {
		value, on := dscpGlobalOn(st, f.v6)
		if stamp.covers(f.v6) {
			value, on = stamp.value, true
		}
		if on {
			*f.value = &value
		}
	}
	if path.IPv4 != nil || path.IPv6 != nil {
		path.Interfaces = cfg.DSCPInterfaces()
	}
	return path
}

func DiscoveryDSCPStamp(cfg *config.Config, dscp *config.DSCPProbe) (*config.DSCPProbe, *config.DSCPPath, error) {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	if err := discoveryCheckDSCP(dscp); err != nil {
		return nil, nil, err
	}
	if dscp == nil {
		return nil, discoveryPathFor(cfg, nil), nil
	}
	be := currentDiscoveryBackend(cfg)
	if be == nil {
		return nil, discoveryPathFor(cfg, nil), nil
	}
	stamp, err := discoveryStampFor(cfg, dscp, be)
	if err != nil {
		return nil, nil, err
	}
	var probe *config.DSCPProbe
	if stamp != nil {
		probe = stamp.probe()
	}
	return probe, discoveryPathFor(cfg, stamp), nil
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
	path         *config.DSCPPath
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
	s := &discoverySteering{cfg: cfg, backend: be, flowMark: flowMark, injectedMark: injectedMark, queueStart: queueStart, threads: threads, dscp: dscp, stamp: stamp, path: discoveryPathFor(cfg, stamp), lost: lost}
	activeSteering = s
	discoveryRun.Store(s)
	return nil
}

func ActiveDiscoveryDSCP() (*config.DSCPProbe, *config.DSCPPath) {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	s := activeSteering
	if s == nil {
		return nil, nil
	}
	var stamp *config.DSCPProbe
	if s.stamp != nil {
		stamp = s.stamp.probe()
	}
	path := *s.path
	path.Interfaces = slices.Clone(s.path.Interfaces)
	return stamp, &path
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
	s.path = discoveryPathFor(s.cfg, stamp)
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
