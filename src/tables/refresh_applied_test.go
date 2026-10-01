package tables

import (
	"errors"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func appliedResetGlobals(t *testing.T) {
	t.Helper()
	origAdd, origClear, origRun := addRulesFn, clearRulesFn, run
	origApplied, origBackend := rulesAppliedCfg, rulesAppliedBackend
	t.Cleanup(func() {
		addRulesFn, clearRulesFn, run = origAdd, origClear, origRun
		rulesMu.Lock()
		rulesAppliedCfg, rulesAppliedBackend = origApplied, origBackend
		rulesMu.Unlock()
		activeSteering = nil
	})
}

func TestRefreshRulesClearsTheRulesThatWereApplied(t *testing.T) {
	appliedResetGlobals(t)

	var cleared, added []*config.Config
	clearRulesFn = func(c *config.Config) error {
		cleared = append(cleared, c)
		return nil
	}
	addRulesFn = func(c *config.Config) error {
		added = append(added, c)
		rulesAppliedCfg = c
		return nil
	}

	before := config.NewConfig()
	before.Queue.Mark = 0x8000
	after := config.NewConfig()
	after.Queue.Mark = 0x4000000

	rulesAppliedCfg = nil
	if err := RefreshRules(&before); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if cleared[0] != &before {
		t.Errorf("with nothing applied yet, the refresh has only the new configuration to clear with")
	}

	if err := RefreshRules(&after); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if cleared[1] != &before {
		t.Errorf("the refresh cleared with the new configuration, so rules built from the old one, such as the CONNMARK rule of the old queue mark, stay in the firewall for good")
	}
	if added[1] != &after {
		t.Errorf("the refresh must apply the new configuration")
	}
}

func TestClearAppliedRulesClearsWhatWasInstalled(t *testing.T) {
	appliedResetGlobals(t)

	var cleared []*config.Config
	clearRulesFn = func(c *config.Config) error {
		cleared = append(cleared, c)
		return nil
	}

	installed := config.NewConfig()
	live := config.NewConfig()
	live.Queue.Mark = 0x4000000

	rulesAppliedCfg = &installed
	if err := ClearAppliedRules(&live); err != nil {
		t.Fatalf("clear: %v", err)
	}
	rulesAppliedCfg = nil
	if err := ClearAppliedRules(&live); err != nil {
		t.Fatalf("clear: %v", err)
	}
	if cleared[0] != &installed {
		t.Errorf("stopping b4 cleared with the live configuration although its rules were never applied, so the installed ones stay after stop")
	}
	if cleared[1] != &live {
		t.Errorf("with nothing recorded, stopping b4 has only the live configuration to clear with")
	}
}

func TestAddRulesDoesNotRecordASkipSetupConfig(t *testing.T) {
	appliedResetGlobals(t)

	installed := config.NewConfig()
	rulesAppliedCfg = &installed

	skip := config.NewConfig()
	skip.System.Tables.SkipSetup = true
	if err := addRules(&skip); err != nil {
		t.Fatalf("addRules: %v", err)
	}
	if rulesAppliedCfg != &installed {
		t.Errorf("a skip_setup configuration installs nothing, yet it replaced the applied one, so the next refresh clears nothing and the installed rules stay")
	}
}

func TestMonitorChecksTheBackendTheRulesWereAppliedWith(t *testing.T) {
	appliedResetGlobals(t)

	m, _ := newLockTestMonitor(t)
	rulesAppliedBackend = ""
	if got := m.firewallBackend(); got != backendIPTables {
		t.Fatalf("with nothing applied the monitor keeps the backend it started with, got %s", got)
	}
	rulesAppliedBackend = backendNFTables
	if got := m.firewallBackend(); got != backendNFTables {
		t.Errorf("after a refresh moved b4's rules to nftables the monitor still checks %s, finds nothing there and rebuilds the rules on every check", got)
	}
}

func TestMonitorLeavesTheRebuildToTheRefreshWhileDiscoveryRuns(t *testing.T) {
	appliedResetGlobals(t)
	hasBinaryCache.Store(backendIPTables, true)
	t.Cleanup(func() { hasBinaryCache.Delete(backendIPTables) })

	present := "-A B4_PREROUTING -p udp --sport 53 -j NFQUEUE -p tcp --dport 53 -j NFQUEUE -m mark 0x8000 -j B4"
	run = func(args ...string) (string, error) {
		return present, nil
	}
	var cleared, added []*config.Config
	clearRulesFn = func(c *config.Config) error {
		cleared = append(cleared, c)
		return nil
	}
	addRulesFn = func(c *config.Config) error {
		added = append(added, c)
		return nil
	}

	m, ptr := newLockTestMonitor(t)
	rulesAppliedBackend = ""
	applied := config.NewConfig()
	applied.Queue.IPv4Enabled = true
	applied.Queue.Mark = 0x8000
	published := config.NewConfig()
	published.Queue.IPv4Enabled = true
	published.Queue.Mark = 0x4000000
	ptr.Store(&published)
	rulesMu.Lock()
	rulesAppliedCfg = &applied
	rulesMu.Unlock()

	activeSteering = &discoverySteering{}
	for i := 0; i < 3; i++ {
		m.ensureRules(false)
	}
	if len(added) != 0 || len(cleared) != 0 {
		t.Fatalf("the monitor rebuilt the firewall while the refresh for the new queue mark waits for a Discovery run: added %d, cleared %d", len(added), len(cleared))
	}

	activeSteering = nil
	m.ensureRules(false)
	if len(added) != 1 || added[0] != &published {
		t.Fatalf("once Discovery ended the monitor must apply the new configuration, added %v", added)
	}
	if len(cleared) != 1 || cleared[0] != &applied {
		t.Errorf("the monitor applied the new queue mark over the old rules without clearing them, so the old CONNMARK rule stays for good")
	}
}

func TestMonitorRestoresTheAppliedRulesWhileDiscoveryRuns(t *testing.T) {
	appliedResetGlobals(t)
	hasBinaryCache.Store(backendIPTables, true)
	t.Cleanup(func() { hasBinaryCache.Delete(backendIPTables) })

	run = func(args ...string) (string, error) {
		return "", errors.New("chain missing")
	}
	var cleared, added []*config.Config
	clearRulesFn = func(c *config.Config) error {
		cleared = append(cleared, c)
		return nil
	}
	addRulesFn = func(c *config.Config) error {
		added = append(added, c)
		return nil
	}

	m, ptr := newLockTestMonitor(t)
	rulesAppliedBackend = ""
	applied := config.NewConfig()
	applied.Queue.IPv4Enabled = true
	applied.Queue.Mark = 0x8000
	published := config.NewConfig()
	published.Queue.IPv4Enabled = true
	published.Queue.Mark = 0x4000000
	ptr.Store(&published)
	rulesMu.Lock()
	rulesAppliedCfg = &applied
	rulesMu.Unlock()

	activeSteering = &discoverySteering{}
	m.ensureRules(false)
	if len(added) != 1 || added[0] != &applied {
		t.Fatalf("rules went missing during a Discovery run; the monitor must put back what is installed, the old queue mark, and leave the switch to the refresh after the run, added %v", added)
	}
	if len(cleared) != 0 {
		t.Errorf("the monitor cleared b4's rules in the middle of a Discovery run")
	}
}

func TestDiscoverySteeringComesBackAfterAFirewallRebuild(t *testing.T) {
	appliedResetGlobals(t)
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: false})
	calls := discoveryRecordRun(t, nil)

	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backendIPTables
	if err := ApplyDiscoverySteeringRules(&cfg, 0x8001, 0x8002, 541, 1); err != nil {
		t.Fatalf("apply: %v", err)
	}

	*calls = nil
	rulesMu.Lock()
	reapplyDiscoverySteering()
	rulesMu.Unlock()
	for _, want := range []string{
		"iptables -w -t mangle -I OUTPUT 1 -j B4_DISCOVERY",
		"iptables -w -t mangle -I B4 1 -m mark --mark 0x8001/0xffffffff -j RETURN",
	} {
		if discoveryCallIndex(*calls, want) < 0 {
			t.Errorf("a firewall rebuild during a run took Discovery's rules with it and they were not put back; missing %q", want)
		}
	}

	ClearDiscoverySteeringRules(&cfg, 0x8001, 0x8002)
	*calls = nil
	rulesMu.Lock()
	reapplyDiscoverySteering()
	rulesMu.Unlock()
	if len(*calls) != 0 {
		t.Errorf("after the run ended a rebuild put Discovery's rules back: %v", *calls)
	}
}
