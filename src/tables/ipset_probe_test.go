package tables

import (
	"errors"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

var errTestNoIPSetKernel = errors.New("ipset v7.19: Kernel error received: Operation not supported")

func stubIPSetProbe(t *testing.T, err error) {
	t.Helper()
	ipsetProbeMu.Lock()
	prevProbed, prevErr := ipsetProbed, ipsetProbeErr
	ipsetProbed, ipsetProbeErr = true, err
	ipsetProbeMu.Unlock()
	t.Cleanup(func() {
		ipsetProbeMu.Lock()
		ipsetProbed, ipsetProbeErr = prevProbed, prevErr
		ipsetProbeMu.Unlock()
	})
}

func manifestUsesIPSet(sets []IPSet, rules []Rule, name string) (declared, matched bool) {
	for _, s := range sets {
		declared = declared || s.Name == name
	}
	for _, r := range rules {
		matched = matched || strings.Contains(strings.Join(r.Spec, " "), "--match-set "+name)
	}
	return declared, matched
}

func TestDuplicationRulesUseIPSetOnlyWhereTheKernelTakesIt(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true, "ipset": true})
	build := func() Manifest {
		t.Helper()
		manager := NewIPTablesManager(dupTestConfig(), false)
		stubProbes(manager, backendIPTables, backendIP6Tables)
		m, err := manager.buildManifest()
		if err != nil {
			t.Fatalf("buildManifest: %v", err)
		}
		return m
	}

	stubIPSetProbe(t, errTestNoIPSetKernel)
	m := build()
	for _, name := range []string{"b4_dup_v4", "b4_dup_v6"} {
		if declared, matched := manifestUsesIPSet(m.IPSets, m.Rules, name); declared || matched {
			t.Fatalf("a kernel that rejects ipset still got %s (declared %v, matched %v), and the first ipset create would stop the engine from starting", name, declared, matched)
		}
	}

	stubIPSetProbe(t, nil)
	m = build()
	for _, name := range []string{"b4_dup_v4", "b4_dup_v6"} {
		if declared, matched := manifestUsesIPSet(m.IPSets, m.Rules, name); !declared || !matched {
			t.Fatalf("a working ipset was not used for %s (declared %v, matched %v)", name, declared, matched)
		}
	}
}

func TestPerSetMSSUsesIPSetOnlyWhereTheKernelTakesIt(t *testing.T) {
	stubBinaries(t, backendIPTables, backendIP6Tables, "ipset")
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, []string{"203.0.113.7"}, nil)}

	stubIPSetProbe(t, errTestNoIPSetKernel)
	sets, rules := NewIPTablesManager(&cfg, false).buildMSSManifest("PREROUTING")
	if declared, matched := manifestUsesIPSet(sets, rules, "b4_mss_0_v4"); declared || matched || len(sets) != 0 {
		t.Fatalf("a kernel that rejects ipset still got the per-set MSS set: %d sets, matched %v", len(sets), matched)
	}

	stubIPSetProbe(t, nil)
	sets, rules = NewIPTablesManager(&cfg, false).buildMSSManifest("PREROUTING")
	if len(sets) != 1 {
		t.Fatalf("a working ipset was not used for per-set MSS, got %d sets", len(sets))
	}
	if _, matched := manifestUsesIPSet(sets, rules, sets[0].Name); !matched {
		t.Fatalf("no per-set MSS rule matches the set %s", sets[0].Name)
	}
}

func TestIPSetProbeRunsOnceAndNamesTheCause(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{"ipset": true})
	ipsetProbeMu.Lock()
	ipsetProbed = false
	ipsetProbeMu.Unlock()
	prevProbe := ipsetKernelProbe
	t.Cleanup(func() { ipsetKernelProbe = prevProbe })
	calls := 0
	ipsetKernelProbe = func() error {
		calls++
		return errTestNoIPSetKernel
	}

	first, second := ipsetUnusable(), ipsetUnusable()
	if calls != 1 {
		t.Fatalf("the kernel probe ran %d times, every rule rebuild would create a set again", calls)
	}
	if first != second || !strings.Contains(first, "does not work on this kernel") || !strings.Contains(first, errTestNoIPSetKernel.Error()) {
		t.Fatalf("the reason does not name the kernel's refusal: %q", first)
	}

	stubBinaryPresence(t, map[string]bool{"ipset": false})
	if reason := ipsetUnusable(); !strings.Contains(reason, "not found") {
		t.Fatalf("a missing ipset command is not named as such: %q", reason)
	}
}
