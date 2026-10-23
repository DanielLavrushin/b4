package tables

import (
	"fmt"
	"slices"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
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

func (s *discoveryStamp) same(o *discoveryStamp) bool {
	if s == nil || o == nil {
		return s == o
	}
	return s.value == o.value && s.v4 == o.v4 && s.v6 == o.v6 && slices.Equal(s.ifaces, o.ifaces)
}

func discoveryCheckDSCP(dscp *config.DSCPProbe) error {
	if dscp != nil && (dscp.Value < 0 || dscp.Value > config.MaxDSCPValue) {
		return fmt.Errorf("DSCP %d for Discovery's probes is outside 0-%d", dscp.Value, config.MaxDSCPValue)
	}
	return nil
}

func discoveryStampFor(cfg *config.Config, dscp *config.DSCPProbe, be discoveryBackend) *discoveryStamp {
	if dscp == nil {
		return nil
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
}

var activeSteering *discoverySteering

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
	activeSteering = &discoverySteering{cfg: cfg, backend: be, flowMark: flowMark, injectedMark: injectedMark, queueStart: queueStart, threads: threads, dscp: dscp, stamp: stamp, lost: lost}
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
	if be == nil {
		be = currentDiscoveryBackend(cfg)
	}
	if be == nil {
		return
	}
	be.clear(flowMark, injectedMark)
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
	stamp := discoveryStampFor(s.cfg, s.dscp, be)
	changed := !stamp.same(s.stamp)
	s.stamp = stamp
	if err := be.apply(s.flowMark, s.injectedMark, s.queueStart, s.threads, stamp); err != nil {
		s.fail(err)
		return
	}
	if changed {
		s.fail(fmt.Errorf("the rebuild changed which of the run's probes can carry DSCP %d, so its results would mix two network paths", s.dscp.Value))
	}
}

func (s *discoverySteering) fail(err error) {
	log.Warnf("Discovery: the run's firewall rules changed or could not be put back after the firewall rules were rebuilt: %v", err)
	if s.lost != nil {
		go s.lost(err)
	}
}
