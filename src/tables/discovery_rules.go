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
	backend := detectFirewallBackend(cfg)
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
	flowMark     uint
	injectedMark uint
	queueStart   int
	threads      int
}

var activeSteering *discoverySteering

func ApplyDiscoverySteeringRules(cfg *config.Config, flowMark uint, injectedMark uint, queueStart int, threads int) error {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	be := getDiscoveryBackend(cfg)
	if be == nil {
		return fmt.Errorf("no discovery firewall backend available")
	}
	if err := be.apply(flowMark, injectedMark, queueStart, threads); err != nil {
		return err
	}
	activeSteering = &discoverySteering{cfg: cfg, flowMark: flowMark, injectedMark: injectedMark, queueStart: queueStart, threads: threads}
	return nil
}

func ClearDiscoverySteeringRules(cfg *config.Config, flowMark uint, injectedMark uint) {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	activeSteering = nil
	be := getDiscoveryBackend(cfg)
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
	be := getDiscoveryBackend(s.cfg)
	if be == nil {
		return
	}
	if err := be.apply(s.flowMark, s.injectedMark, s.queueStart, s.threads); err != nil {
		log.Warnf("Discovery: putting the steering rules back after the firewall rules were rebuilt failed: %v", err)
	}
}
