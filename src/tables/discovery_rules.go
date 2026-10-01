package tables

import (
	"fmt"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type discoveryBackend interface {
	name() string
	available() bool
	apply(flowMark uint, injectedMark uint, queueStart int, threads int) error
	clear(flowMark uint, injectedMark uint)
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
}

var activeSteering *discoverySteering

func ApplyDiscoverySteeringRules(cfg *config.Config, flowMark uint, injectedMark uint, queueStart int, threads int) error {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	be := currentDiscoveryBackend(cfg)
	if be == nil {
		return fmt.Errorf("no discovery firewall backend available")
	}
	if err := be.apply(flowMark, injectedMark, queueStart, threads); err != nil {
		return err
	}
	activeSteering = &discoverySteering{cfg: cfg, backend: be, flowMark: flowMark, injectedMark: injectedMark, queueStart: queueStart, threads: threads}
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
		return
	}
	if s.backend != nil && s.backend.name() != be.name() {
		s.backend.clear(s.flowMark, s.injectedMark)
	}
	s.backend = be
	if err := be.apply(s.flowMark, s.injectedMark, s.queueStart, s.threads); err != nil {
		log.Warnf("Discovery: putting the steering rules back after the firewall rules were rebuilt failed: %v", err)
	}
}
