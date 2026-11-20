package tables

import (
	"errors"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestCollectedRefreshAddsNoQueueRulesToAnUnappliedFirewall(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			h := newGoldenHost(t)
			goldenIsolate(t, h)
			run = refuseDeletes(run)
			rulesMu.Lock()
			rulesAppliedCfg = nil
			rulesMu.Unlock()
			cfg := goldenConfig(backend, config.DSCPConfig{})
			cfg.Queue.Mode = "tun"
			if err := cfg.Validate(); err != nil {
				t.Fatal(err)
			}
			h.take()
			if err := RefreshCollectedRules(cfg); err != nil {
				t.Fatal(err)
			}
			if lines := h.take(); len(lines) != 0 {
				t.Fatalf("a collected refresh with no firewall applied ran %d commands, first %q", len(lines), lines[0])
			}
		})
	}
}

func TestCollectedRefreshAfterTheFinalClearAddsNothing(t *testing.T) {
	oldAdd, oldClear, oldRun := addRulesFn, clearRulesFn, run
	rulesMu.Lock()
	oldApplied, oldClosed := rulesAppliedCfg, dscpSyncClosed.Load()
	rulesMu.Unlock()
	t.Cleanup(func() {
		addRulesFn, clearRulesFn, run = oldAdd, oldClear, oldRun
		rulesMu.Lock()
		rulesAppliedCfg = oldApplied
		dscpSyncClosed.Store(oldClosed)
		rulesMu.Unlock()
	})
	var calls []string
	run = refuseDeletes(func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})
	clearRulesFn = func(*config.Config) error {
		calls = append(calls, "clear")
		return nil
	}
	addRulesFn = func(*config.Config) error {
		calls = append(calls, "add")
		return nil
	}
	applied := config.NewConfig()
	rulesMu.Lock()
	rulesAppliedCfg = &applied
	rulesMu.Unlock()
	if err := ClearAppliedRules(&applied); err != nil {
		t.Fatal(err)
	}
	calls = nil
	next := config.NewConfig()
	next.Queue.TCPConnBytesLimit = applied.Queue.TCPConnBytesLimit + 2
	if err := RefreshCollectedRules(&next); err != nil || len(calls) != 0 {
		t.Fatalf("a collected refresh after the final clear ran %q, err=%v", calls, err)
	}
}

func TestACollectedViewNeverRebuildsTheFirewall(t *testing.T) {
	oldAdd, oldClear, oldRun := addRulesFn, clearRulesFn, run
	rulesMu.Lock()
	oldApplied, oldLogged := rulesAppliedCfg, collectedRefreshPendingLogged
	rulesMu.Unlock()
	t.Cleanup(func() {
		addRulesFn, clearRulesFn, run = oldAdd, oldClear, oldRun
		rulesMu.Lock()
		rulesAppliedCfg, collectedRefreshPendingLogged = oldApplied, oldLogged
		rulesMu.Unlock()
	})
	var calls []string
	run = refuseDeletes(func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	})
	clearRulesFn = func(*config.Config) error {
		calls = append(calls, "clear")
		return nil
	}
	addRulesFn = func(*config.Config) error {
		calls = append(calls, "add")
		return nil
	}
	logs := captureTablesLog(t)
	pending := func() int { return strings.Count(logs.String(), "wait for the refresh of the firewall") }
	applied := config.NewConfig()
	saved := config.NewConfig()
	saved.Queue.TCPConnBytesLimit = applied.Queue.TCPConnBytesLimit + 2
	rulesMu.Lock()
	rulesAppliedCfg, collectedRefreshPendingLogged = &applied, false
	rulesMu.Unlock()
	view := saved.WithCollectedIPs([]string{"203.0.113.7"})

	for range 3 {
		if err := RefreshCollectedRules(view); !errors.Is(err, ErrFirewallRefreshPending) {
			t.Fatalf("a collected view on a base the firewall does not hold yet must wait for its refresh, got %v", err)
		}
	}
	if len(calls) != 0 {
		t.Fatalf("a collected view rebuilt the firewall: %q", calls)
	}
	if n := pending(); n != 1 {
		t.Fatalf("a streak of pending collected refreshes must be logged once, got %d lines", n)
	}

	rulesMu.Lock()
	rulesAppliedCfg = &saved
	rulesMu.Unlock()
	if err := RefreshCollectedRules(view); err != nil {
		t.Fatalf("once the firewall holds the saved base the collected refresh must pass, got %v", err)
	}
	other := config.NewConfig()
	other.Queue.TCPConnBytesLimit = saved.Queue.TCPConnBytesLimit + 2
	if err := RefreshCollectedRules(other.WithCollectedIPs(nil)); !errors.Is(err, ErrFirewallRefreshPending) {
		t.Fatalf("a later pending refresh returned %v", err)
	}
	if n := pending(); n != 2 || len(calls) != 0 {
		t.Fatalf("a new pending streak must be logged again: %d lines, calls %q", n, calls)
	}
}

func TestMonitorRebuildsAPendingApplyBehindACollectedView(t *testing.T) {
	origRun, origAdd, origClear := run, addRulesFn, clearRulesFn
	rulesMu.Lock()
	origApplied, origBackend := rulesAppliedCfg, rulesAppliedBackend
	rulesAppliedBackend = ""
	rulesMu.Unlock()
	stubSyncMapEntry(t, &hasBinaryCache, backendIPTables, true)
	t.Cleanup(func() {
		run, addRulesFn, clearRulesFn = origRun, origAdd, origClear
		rulesMu.Lock()
		rulesAppliedCfg, rulesAppliedBackend = origApplied, origBackend
		rulesMu.Unlock()
	})
	present := strings.Join([]string{
		"-A B4_PREROUTING -p udp -m udp --sport 53 -j NFQUEUE --queue-num 537 --queue-bypass",
		"-A B4_PREROUTING -p udp -m udp --dport 53 -j NFQUEUE --queue-num 537 --queue-bypass",
		"-A B4_PREROUTING -p tcp -m multiport --sports 443 -m tcp --tcp-flags SYN,ACK SYN,ACK -j NFQUEUE --queue-num 537 --queue-bypass",
		"-A OUTPUT -m mark --mark 0x8000/0x8000 -j ACCEPT",
		"-A B4 -m mark --mark 0x8000/0x88f98000 -j RETURN",
		"-A B4 -p tcp -m multiport --dports 443 -j NFQUEUE --queue-num 537 --queue-bypass",
		"-A B4 -p udp -m multiport --dports 443 -j NFQUEUE --queue-num 537 --queue-bypass",
	}, "\n")
	calls := 0
	run = refuseDeletes(func(args ...string) (string, error) {
		calls++
		if calls == 1 {
			return "", errors.New("chain missing")
		}
		return present, nil
	})
	restores := 0
	addRulesFn = func(*config.Config) error {
		restores++
		return nil
	}
	clearRulesFn = func(*config.Config) error { return nil }

	m, ptr := newLockTestMonitor(t)
	m.configView = func(c *config.Config) *config.Config { return c.WithCollectedIPs(nil) }
	applied := config.NewConfig()
	applied.Queue.IPv4Enabled = true
	published := config.NewConfig()
	published.Queue.IPv4Enabled = true
	published.Queue.TCPConnBytesLimit = applied.Queue.TCPConnBytesLimit + 2
	ptr.Store(&published)
	rulesMu.Lock()
	rulesAppliedCfg = &applied
	rulesMu.Unlock()

	if _, acted := m.ensureRules(false); !acted || restores != 0 {
		t.Fatalf("the first tick must defer to the pending apply, acted=%v restores=%d", acted, restores)
	}
	calls = 0
	m.ensureRules(false)
	if restores != 1 {
		t.Fatalf("the monitor did not rebuild on the second tick behind a collected view, restores=%d", restores)
	}
}
