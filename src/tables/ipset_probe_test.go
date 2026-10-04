package tables

import (
	"errors"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

var errTestNoIPSetKernel = errors.New("ipset v7.19: Kernel error received: Operation not supported")

func stubIPSetProbe(t *testing.T, err error) {
	t.Helper()
	ipsetProbeMu.Lock()
	prev := maps.Clone(ipsetProbeErrs)
	for _, bin := range []string{backendIPTables, backendIP6Tables, backendIPTablesLegacy, backendIP6TablesLegacy} {
		ipsetProbeErrs[bin] = err
	}
	ipsetProbeMu.Unlock()
	t.Cleanup(func() {
		ipsetProbeMu.Lock()
		ipsetProbeErrs = prev
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

func TestIPSetProbeRunsOncePerBinary(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{"ipset": true})
	ipsetProbeMu.Lock()
	prev := ipsetProbeErrs
	ipsetProbeErrs = map[string]error{}
	ipsetProbeMu.Unlock()
	prevProbe := ipsetMatchProbe
	t.Cleanup(func() {
		ipsetMatchProbe = prevProbe
		ipsetProbeMu.Lock()
		ipsetProbeErrs = prev
		ipsetProbeMu.Unlock()
	})
	probed := map[string]int{}
	ipsetMatchProbe = func(_ *IPTablesManager, ipt string) error {
		probed[ipt]++
		return errTestNoIPSetKernel
	}

	cfg := config.NewConfig()
	im := NewIPTablesManager(&cfg, false)
	first, second := im.ipsetUnusable(backendIPTables), im.ipsetUnusable(backendIPTables)
	im.ipsetUnusable(backendIP6Tables)
	if probed[backendIPTables] != 1 || probed[backendIP6Tables] != 1 {
		t.Fatalf("the probe ran %v times per binary, want once each: a rebuild would create the probe set again, or one family would decide for the other", probed)
	}
	if first != second || !strings.Contains(first, errTestNoIPSetKernel.Error()) {
		t.Fatalf("the reason does not carry the probe's error: %q", first)
	}

	stubBinaryPresence(t, map[string]bool{"ipset": false})
	if reason := im.ipsetUnusable(backendIPTables); !strings.Contains(reason, "not found") {
		t.Fatalf("a missing ipset command is not named as such: %q", reason)
	}
}

func TestIPSetProbeChecksTheSetMatchOfEachFamily(t *testing.T) {
	prevRun := run
	t.Cleanup(func() { run = prevRun })
	var calls []string
	matchRejected := false
	run = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if matchRejected && slices.Contains(args, "--match-set") {
			return "Couldn't load match `set':No such file or directory", errors.New("exit status 2")
		}
		return "", nil
	}
	created := func(want string) bool {
		return slices.Contains(calls, want)
	}

	if err := ipsetMatchProbe(&IPTablesManager{}, backendIP6Tables); err != nil {
		t.Fatalf("a kernel and an ip6tables that take the set match were refused: %v", err)
	}
	if !created("ipset create b4_ipset_probe6 hash:net family inet6 -exist") {
		t.Fatalf("ip6tables was probed against a set that is not inet6: %v", calls)
	}
	if !slices.ContainsFunc(calls, func(c string) bool {
		return strings.HasPrefix(c, backendIP6Tables+" ") && strings.Contains(c, "--match-set b4_ipset_probe6 dst")
	}) {
		t.Fatalf("the probe never tried the set match in ip6tables: %v", calls)
	}
	if calls[len(calls)-1] != "ipset destroy b4_ipset_probe6" {
		t.Fatalf("the probe set was not destroyed last, after the probe chain that references it: %v", calls)
	}

	calls, matchRejected = nil, true
	err := ipsetMatchProbe(&IPTablesManager{}, backendIPTables)
	if err == nil || !strings.Contains(err.Error(), "xt_set") {
		t.Fatalf("an iptables without the set match passed the probe, so duplication and MSS rules would abort the firewall setup: %v", err)
	}
	if !created("ipset create b4_ipset_probe hash:net family inet -exist") || calls[len(calls)-1] != "ipset destroy b4_ipset_probe" {
		t.Fatalf("the IPv4 probe set was not created and destroyed: %v", calls)
	}
}
