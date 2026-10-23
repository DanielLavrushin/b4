package tables

import (
	"fmt"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type discoveryBackend interface {
	name() string
	available() bool
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

func discoveryStampFor(cfg *config.Config, dscp *config.DSCPProbe) (*discoveryStamp, error) {
	if dscp == nil {
		return nil, nil
	}
	if dscp.Value < 0 || dscp.Value > config.MaxDSCPValue {
		return nil, fmt.Errorf("DSCP %d for Discovery's probes is outside 0-%d", dscp.Value, config.MaxDSCPValue)
	}
	stamp := &discoveryStamp{
		value:  dscp.Value,
		ifaces: cfg.DSCPInterfaces(),
		v4:     dscp.IPv4 && dscpFamilyTakesSetValues(false),
		v6:     dscp.IPv6 && dscpFamilyTakesSetValues(true),
	}
	if !stamp.v4 && !stamp.v6 {
		return nil, nil
	}
	return stamp, nil
}

func dscpFamilyTakesSetValues(v6 bool) bool {
	st := dscpApplied.Load()
	if st == nil || st.ipt == nil {
		return true
	}
	for bin, reason := range st.ipt.incapable {
		if reason != "" && dscpIptBinV6(bin) == v6 {
			return false
		}
	}
	return true
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
	stamp, err := discoveryStampFor(cfg, dscp)
	if err != nil {
		return err
	}
	be := currentDiscoveryBackend(cfg)
	if be == nil {
		return fmt.Errorf("no discovery firewall backend available")
	}
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
	stamp, err := discoveryStampFor(s.cfg, s.dscp)
	if err == nil {
		s.stamp = stamp
		err = be.apply(s.flowMark, s.injectedMark, s.queueStart, s.threads, stamp)
	}
	if err != nil {
		s.fail(err)
	}
}

func (s *discoverySteering) fail(err error) {
	log.Warnf("Discovery: putting the steering rules back after the firewall rules were rebuilt failed: %v", err)
	if s.lost != nil {
		go s.lost(err)
	}
}
