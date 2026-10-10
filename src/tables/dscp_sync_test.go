package tables

import (
	"context"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func dscpSyncIsolate(t *testing.T) {
	t.Helper()
	StopDSCPSync()
	origTick, origPass, origHook := dscpSyncTick, dscpSyncPassFn, dscpPreResolveFn
	rulesMu.Lock()
	origBackend, origClosed := rulesAppliedBackend, dscpSyncClosed.Load()
	rulesMu.Unlock()
	reset := func() {
		dscpSyncClosed.Store(false)
		dscpSyncWanted.Store(nil)
		select {
		case <-dscpSyncKick:
		default:
		}
	}
	reset()
	t.Cleanup(func() {
		StopDSCPSync()
		reset()
		dscpSyncTick, dscpSyncPassFn, dscpPreResolveFn = origTick, origPass, origHook
		rulesMu.Lock()
		rulesAppliedBackend = origBackend
		dscpSyncClosed.Store(origClosed)
		rulesMu.Unlock()
	})
	sys := t.TempDir()
	for _, module := range append(slices.Clone(kernelModuleList), "xt_hashlimit", "xt_DSCP", "xt_set") {
		if err := os.MkdirAll(filepath.Join(sys, kmodCanonical(module)), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	origSys := kmodSysRoot
	kmodSysRoot = sys
	t.Cleanup(func() { kmodSysRoot = origSys })
}

func dscpSyncSetBackend(backend string) {
	rulesMu.Lock()
	rulesAppliedBackend = backend
	rulesMu.Unlock()
}

func dscpSyncNftConfig(global config.DSCPConfig, sets ...*config.SetConfig) *config.Config {
	cfg := dscpPlanTestConfig(sets...)
	cfg.System.Tables.DSCP = global
	cfg.System.Tables.Engine = backendNFTables
	return cfg
}

func dscpSyncHasMutation(calls []string, op string) bool {
	return slices.ContainsFunc(calls, func(c string) bool { return strings.Contains(c, " "+op+" "+dscpChainName) })
}

func TestSetDSCPSyncNoopWithoutSets(t *testing.T) {
	quiet := dscpPlanTestSet("quiet", 31, "10.0.0.0/8")
	quiet.DSCP.Enabled = false
	proxy := dscpPlanTestSet("proxy", 31, "10.1.0.0/16")
	proxy.Routing.Enabled, proxy.Routing.Mode = true, config.RoutingModeProxy
	proxy.Routing.Upstream.Host, proxy.Routing.Upstream.Port = "192.0.2.10", 1080
	cases := []struct {
		name string
		cfg  func(global int) *config.Config
	}{
		{"global off", func(g int) *config.Config { return dscpIptTestConfig(g, false, nil, quiet) }},
		{"global on", func(g int) *config.Config { return dscpIptTestConfig(g, true, nil, quiet) }},
		{"global on with interfaces", func(g int) *config.Config { return dscpIptTestConfig(g, true, []string{"eth0"}, quiet) }},
		{"a set whose DSCP is refused", func(g int) *config.Config { return dscpIptTestConfig(g, true, nil, proxy) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
			dscpSyncIsolate(t)
			dscpSyncSetBackend(backendIPTables)
			hooked := 0
			dscpPreResolveFn = func(*dscpPlan, bool) { hooked++ }

			cfg := tc.cfg(7)
			for _, periodic := range []bool{false, true} {
				dscpSyncPass(cfg, periodic, nil)
			}
			if len(h.events) != 0 || dscpApplied.Load() != nil {
				t.Fatalf("a pass for a configuration without a set DSCP ran %q and recorded %+v", h.events, dscpApplied.Load())
			}

			if _, _, on := cfg.DSCPStamp(); on {
				if err := applyDSCPFor(cfg, backendIPTables); err != nil {
					t.Fatal(err)
				}
				applied := dscpApplied.Load()
				h.events = nil
				for _, periodic := range []bool{false, true} {
					dscpSyncPass(tc.cfg(31), periodic, nil)
				}
				if len(h.events) != 0 {
					t.Errorf("a pass took over a change of the global stamp alone, which stays RefreshRules' job: %q", h.events)
				}
				if dscpApplied.Load() != applied {
					t.Errorf("a pass replaced the recorded global stamp")
				}
			}
			if hooked != 0 {
				t.Errorf("the pre-resolve hook ran %d times without a set DSCP", hooked)
			}
		})
	}
}

func TestSetDSCPSyncNoopUnchangedPlan(t *testing.T) {
	t.Run("iptables", func(t *testing.T) {
		build := func() *config.Config {
			return dscpIptTestConfig(7, true, []string{"eth0"},
				dscpPlanTestSet("a", 31, "10.0.0.0/8", "fd00::/16"),
				dscpPlanTestSet("b", 6, "10.1.0.0/16"))
		}
		h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
		dscpSyncIsolate(t)
		var hooked []*dscpPlan
		var periodics []bool
		dscpPreResolveFn = func(p *dscpPlan, periodic bool) {
			if !rulesMu.TryLock() {
				t.Errorf("the pre-resolve hook ran while the pass still held the firewall lock")
			} else {
				rulesMu.Unlock()
			}
			hooked = append(hooked, p)
			periodics = append(periodics, periodic)
		}
		if err := applyDSCPFor(build(), backendIPTables); err != nil {
			t.Fatal(err)
		}
		applied := dscpApplied.Load()
		h.events = nil
		for _, periodic := range []bool{false, true} {
			dscpSyncPass(build(), periodic, nil)
		}
		if len(h.events) != 0 {
			t.Errorf("a pass with an unchanged plan ran commands: %q", h.events)
		}
		if dscpApplied.Load() != applied {
			t.Errorf("a pass with an unchanged plan replaced the recorded state")
		}
		if len(hooked) != 2 || hooked[0] != applied.plan || hooked[1] != applied.plan || !slices.Equal(periodics, []bool{false, true}) {
			t.Errorf("the pre-resolve hook must see the applied plan once per pass, got %d calls, periodic %v", len(hooked), periodics)
		}
	})

	t.Run("nftables", func(t *testing.T) {
		build := func() *config.Config {
			return dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7},
				dscpPlanTestSet("a", 31, "10.0.0.0/8"), dscpPlanTestSet("b", 6, "10.1.0.0/16"))
		}
		f := installDSCPNftFake(t)
		dscpSyncIsolate(t)
		if err := applyDSCPFor(build(), backendNFTables); err != nil {
			t.Fatal(err)
		}
		scripts, commands := len(f.scripts), len(f.commands)
		for _, periodic := range []bool{false, true} {
			dscpSyncPass(build(), periodic, nil)
		}
		if len(f.scripts) != scripts || len(f.commands) != commands {
			t.Errorf("a pass with an unchanged plan touched nftables: scripts %q, commands %q", f.since(scripts), f.commands[commands:])
		}
	})
}

func TestSetDSCPSyncSaveRetriesPending(t *testing.T) {
	for _, globalOn := range []bool{true, false} {
		t.Run(fmt.Sprintf("iptables, global %v", globalOn), func(t *testing.T) {
			h := dscpIptNewHost(t, backendIPTables)
			dscpSyncIsolate(t)
			dscpSyncSetBackend(backendIPTables)
			build := func() *config.Config {
				return dscpIptTestConfig(7, globalOn, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"))
			}
			h.probeErr[backendIPTables] = errors.New("iptables rejected the set match, which needs the xt_set kernel module and the iptables set extension (could not create probe chain B4_MODULE_TEST: command [iptables -w -t mangle -N B4_MODULE_TEST] failed: exit status 4 (Another app is currently holding the xtables lock. Stopped waiting after 1s.))")
			dscpSyncPass(build(), false, nil)
			if st := dscpApplied.Load(); st == nil || !st.pending {
				t.Fatalf("a busy probe must leave a pending state: %+v", st)
			}

			delete(h.probeErr, backendIPTables)
			cfg := build()
			dscpSyncPass(cfg, false, nil)
			if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptRender(dscpPlanFor(cfg), false, true)); !slices.Equal(got, want) {
				t.Errorf("a save with the same plan did not retry the pending per-set rules: chain %q, want %q", got, want)
			}
			if st := dscpApplied.Load(); st == nil || st.pending {
				t.Errorf("the save left the per-set rules pending: %+v", st)
			}
		})
	}

	t.Run("nftables", func(t *testing.T) {
		f := installDSCPNftFake(t)
		dscpSyncIsolate(t)
		dscpSyncSetBackend(backendNFTables)
		build := func() *config.Config {
			return dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, dscpPlanTestSet("a", 31, "10.1.2.0/24"))
		}
		stall := true
		f.fail = func(string) (string, error) {
			if stall {
				return "", fmt.Errorf("command [nft -f -] gave up after 15s: %w", context.DeadlineExceeded)
			}
			return "", nil
		}
		dscpSyncPass(build(), false, nil)
		if st := dscpApplied.Load(); st == nil || !st.pending {
			t.Fatalf("an nft timeout must leave a pending state: %+v", st)
		}

		stall = false
		dscpSyncPass(build(), false, nil)
		if got, ok := f.stamp(netip.MustParseAddr("10.1.2.3"), "wan"); !ok || got != 31 {
			t.Errorf("a save with the same plan did not retry the pending nft apply: 10.1.2.3 leaves with %d (stamped %v)", got, ok)
		}
		if st := dscpApplied.Load(); st == nil || st.pending {
			t.Errorf("the save left the per-set objects pending: %+v", st)
		}
	})
}

func TestSetDSCPSyncAppliesGlobalWithPlan(t *testing.T) {
	t.Run("iptables", func(t *testing.T) {
		h := dscpIptNewHost(t, backendIPTables)
		dscpSyncIsolate(t)
		dscpSyncSetBackend(backendIPTables)
		a := func() *config.SetConfig { return dscpPlanTestSet("a", 31, "10.1.2.0/24") }
		expect := func(stage string, cfg *config.Config) {
			t.Helper()
			if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptRender(dscpPlanFor(cfg), false, true)); !slices.Equal(got, want) {
				t.Errorf("%s: chain = %q, want %q", stage, got, want)
			}
			if st := dscpApplied.Load(); st == nil || st.cfg != cfg || st.backend != backendIPTables {
				t.Errorf("%s: recorded state %+v, want the pass's configuration on %s", stage, st, backendIPTables)
			}
		}

		first := dscpIptTestConfig(7, true, nil, a())
		dscpSyncPass(first, false, nil)
		expect("a plan with nothing applied", first)

		h.mangle.calls = nil
		global9 := dscpIptTestConfig(9, true, nil, a())
		dscpSyncPass(global9, false, nil)
		expect("a new global value next to the plan", global9)
		if dscpSyncHasMutation(h.mangle.calls, "-F") || !dscpSyncHasMutation(h.mangle.calls, "-I") {
			t.Errorf("a global change with a plan must be a gap-free replace, not a flush: %q", h.mangle.mutations())
		}

		off := dscpIptTestConfig(9, false, nil, a())
		dscpSyncPass(off, false, nil)
		expect("the global stamp switched off while the plan stays", off)

		gone := dscpIptTestConfig(9, true, nil)
		dscpSyncPass(gone, false, nil)
		if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptSpecs(9, nil)); !slices.Equal(got, want) {
			t.Errorf("after the last set left: chain = %q, want the global chain %q", got, want)
		}
		if len(h.sets) != 0 {
			t.Errorf("the ipsets of a plan that left survived: %v", h.snapshot())
		}

		h.events = nil
		dscpSyncPass(dscpIptTestConfig(9, true, nil), false, nil)
		dscpSyncPass(dscpIptTestConfig(11, true, nil), false, nil)
		if len(h.events) != 0 {
			t.Errorf("once no plan is applied or wanted, the global stamp is RefreshRules' job again: %q", h.events)
		}

		dscpSyncPass(off, false, nil)
		expect("a plan with the global stamp off", off)
		dscpSyncPass(dscpIptTestConfig(9, false, nil), false, nil)
		if _, ok := h.mangle.chains[backendIPTables][dscpChainName]; ok || len(h.sets) != 0 || dscpApplied.Load() != nil {
			t.Errorf("with the global stamp off, the last set leaving must remove everything: chain %q, ipsets %v", h.chain(backendIPTables), h.snapshot())
		}
		if got := h.mangle.chains[backendIPTables]["POSTROUTING"]; len(got) != 0 {
			t.Errorf("POSTROUTING still jumps somewhere: %v", got)
		}
	})

	t.Run("nftables", func(t *testing.T) {
		f := installDSCPNftFake(t)
		dscpSyncIsolate(t)
		dscpSyncSetBackend(backendNFTables)
		a := func() *config.SetConfig { return dscpPlanTestSet("a", 31, "10.1.2.0/24") }
		probe := func(stage, addr string, want int) {
			t.Helper()
			if got, ok := f.stamp(netip.MustParseAddr(addr), "wan"); !ok || got != want {
				t.Errorf("%s: %s leaves with DSCP %d (stamped %v), want %d", stage, addr, got, ok, want)
			}
		}
		dscpSyncPass(dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, a()), false, nil)
		probe("first pass", "10.1.2.3", 31)
		probe("first pass", "192.0.2.1", 7)

		mark := len(f.scripts)
		dscpSyncPass(dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 9}, a()), false, nil)
		probe("global change", "10.1.2.3", 31)
		probe("global change", "192.0.2.1", 9)
		for _, s := range f.since(mark) {
			if strings.Contains(s, "delete table") {
				t.Errorf("a global change next to a plan rebuilt the table:\n%s", s)
			}
		}
	})
}

func TestSetDSCPSyncSkipSetup(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	dscpSyncIsolate(t)
	dscpSyncSetBackend(backendIPTables)
	build := func(value int, skip bool) *config.Config {
		cfg := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", value, "10.1.2.0/24"))
		cfg.System.Tables.SkipSetup = skip
		return cfg
	}

	for _, periodic := range []bool{false, true} {
		dscpSyncPass(build(31, true), periodic, nil)
	}
	if len(h.events) != 0 || dscpApplied.Load() != nil {
		t.Fatalf("under skip_setup a pass installed %q", h.events)
	}

	if err := applyDSCPFor(build(31, false), backendIPTables); err != nil {
		t.Fatal(err)
	}
	applied := dscpApplied.Load()
	h.events = nil
	for _, periodic := range []bool{false, true} {
		dscpSyncPass(build(6, true), periodic, nil)
	}
	if len(h.events) != 0 || dscpApplied.Load() != applied {
		t.Errorf("under skip_setup a pass changed the firewall b4 no longer manages: %q", h.events)
	}
}

func TestSetDSCPSyncClosedAfterTeardown(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	dscpSyncIsolate(t)
	appliedResetGlobals(t)
	t.Cleanup(func() { tunFirewallClosed.Store(false) })
	clearRulesFn = func(c *config.Config) error {
		clearDSCPFor(c, backendIPTables)
		return nil
	}
	cfg := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.1.2.0/24"))
	cfg.System.Tables.Engine = backendIPTables
	later := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 6, "10.1.2.0/24"))
	quiet := func(stage string) {
		t.Helper()
		h.events = nil
		for _, periodic := range []bool{false, true} {
			dscpSyncPass(later, periodic, nil)
		}
		if len(h.events) != 0 || dscpApplied.Load() != nil {
			t.Errorf("%s: a save still in flight recreated the objects: %q", stage, h.events)
		}
		if _, ok := h.mangle.chains[backendIPTables][dscpChainName]; ok || len(h.sets) != 0 {
			t.Errorf("%s: objects outlive the teardown: chain %v, ipsets %v", stage, h.chain(backendIPTables), h.snapshot())
		}
	}

	rulesAppliedCfg, rulesAppliedBackend = cfg, backendIPTables
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatal(err)
	}
	if err := ClearAppliedRules(cfg); err != nil {
		t.Fatal(err)
	}
	quiet("after ClearAppliedRules")

	StartDSCPSync()
	if dscpSyncClosed.Load() {
		t.Errorf("starting the worker with the engine must reopen it")
	}
	StopDSCPSync()

	tun := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.1.2.0/24"))
	tun.Queue.Mode = "tun"
	tun.System.Tables.Engine = backendIPTables
	if err := ApplyDSCPOnly(tun); err != nil {
		t.Fatal(err)
	}
	if dscpApplied.Load() == nil || len(h.sets) == 0 {
		t.Fatalf("the TUN start did not apply the plan")
	}
	ClearTUNFirewall(tun)
	quiet("after ClearTUNFirewall")
}

func TestSetDSCPSyncBackendInTUN(t *testing.T) {
	a := func() *config.SetConfig { return dscpPlanTestSet("a", 31, "10.1.2.0/24") }
	nftOnly := func(t *testing.T, f *dscpNftFake) {
		t.Helper()
		st := dscpApplied.Load()
		if st == nil || st.backend != backendNFTables || !st.nft.perSet() {
			t.Fatalf("the pass did not apply the plan to nftables: %+v", st)
		}
		if got, _ := f.stamp(netip.MustParseAddr("10.1.2.3"), "wan"); got != 31 {
			t.Errorf("10.1.2.3 leaves with DSCP %d, want 31", got)
		}
		for _, c := range f.commands {
			if strings.HasPrefix(c, "ip") && !strings.HasPrefix(c, "nft") {
				t.Errorf("the pass ran an iptables command: %s", c)
			}
		}
	}

	t.Run("TUN detects the backend", func(t *testing.T) {
		f := installDSCPNftFake(t)
		dscpSyncIsolate(t)
		dscpSyncSetBackend(backendIPTables)
		cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, a())
		cfg.Queue.Mode = "tun"
		dscpSyncPass(cfg, false, nil)
		nftOnly(t, f)
	})

	t.Run("NFQUEUE follows the backend the rules were applied with", func(t *testing.T) {
		f := installDSCPNftFake(t)
		dscpSyncIsolate(t)
		dscpSyncSetBackend(backendNFTables)
		cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, a())
		cfg.System.Tables.Engine = backendIPTables
		dscpSyncPass(cfg, false, nil)
		nftOnly(t, f)
	})

	t.Run("the applied stamp's backend wins", func(t *testing.T) {
		f := installDSCPNftFake(t)
		dscpSyncIsolate(t)
		if err := applyDSCPFor(dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}), backendNFTables); err != nil {
			t.Fatal(err)
		}
		dscpSyncSetBackend(backendIPTables)
		cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, a())
		cfg.System.Tables.Engine = backendIPTables
		dscpSyncPass(cfg, false, nil)
		nftOnly(t, f)
	})
}

func TestSetDSCPSyncScopeChangeWhileGlobalOff(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	dscpSyncIsolate(t)
	dscpSyncSetBackend(backendIPTables)
	a := func() *config.SetConfig { return dscpPlanTestSet("a", 31, "10.1.2.0/24") }
	before := dscpIptTestConfig(7, false, []string{"eth0"}, a())
	after := dscpIptTestConfig(7, false, []string{"wan"}, a())
	if !before.System.Tables.DSCP.Equal(after.System.Tables.DSCP) || config.FirewallRefreshNeeded(before, after) {
		t.Fatalf("this case needs an interface change that neither the DSCP comparison nor the firewall refresh sees")
	}

	dscpSyncPass(before, false, nil)
	if got := strings.Join(h.chain(backendIPTables), "\n"); !strings.Contains(got, "-o eth0 -m set --match-set b4d_s31_v4 dst") {
		t.Fatalf("the plan is not scoped to eth0:\n%s", got)
	}
	h.mangle.calls = nil
	dscpSyncPass(after, false, nil)
	got := strings.Join(h.chain(backendIPTables), "\n")
	if strings.Contains(got, "-o eth0 ") || !strings.Contains(got, "-o wan -m set --match-set b4d_s31_v4 dst") {
		t.Errorf("the interface change was not applied while the global stamp is off:\n%s", got)
	}
	if dscpSyncHasMutation(h.mangle.calls, "-F") {
		t.Errorf("the scope change flushed the chain: %q", h.mangle.mutations())
	}
	if st := dscpApplied.Load(); st == nil || !slices.Equal(st.plan.scopes, []string{"wan"}) {
		t.Errorf("recorded plan scopes %+v, want [wan]", st)
	}
}

func TestSetDSCPSyncWorkerCoalescesAndStops(t *testing.T) {
	dscpSyncIsolate(t)
	dscpSyncTick = time.Hour
	release := make(chan struct{})
	started := make(chan *config.Config, 8)
	dscpSyncPassFn = func(cfg *config.Config, periodic bool, _ <-chan struct{}) {
		started <- cfg
		<-release
	}
	cfgs := make([]*config.Config, 40)
	for i := range cfgs {
		cfgs[i] = dscpTestConfig(true, i)
	}

	StartDSCPSync()
	SyncDSCP(cfgs[0])
	first := <-started
	saved := make(chan struct{})
	go func() {
		for _, cfg := range cfgs[1:] {
			SyncDSCP(cfg)
		}
		SyncDSCP(nil)
		close(saved)
	}()
	select {
	case <-saved:
	case <-time.After(5 * time.Second):
		t.Fatal("SyncDSCP blocked while a pass was running")
	}
	close(release)
	second := <-started
	StopDSCPSync()
	if first != cfgs[0] || second != cfgs[len(cfgs)-1] {
		t.Errorf("the passes saw %p and %p, want the first and the latest configuration", first, second)
	}
	select {
	case extra := <-started:
		t.Errorf("the saves made during one pass were not merged into one more pass, an extra pass saw %p", extra)
	default:
	}
	SyncDSCP(cfgs[1])
	select {
	case <-started:
		t.Errorf("a pass ran after the worker stopped")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSetDSCPSyncTickerRunsPeriodicPasses(t *testing.T) {
	dscpSyncIsolate(t)
	dscpSyncTick = time.Millisecond
	ticks := make(chan *config.Config, 1)
	kicks := make(chan struct{}, 1)
	dscpSyncPassFn = func(cfg *config.Config, periodic bool, _ <-chan struct{}) {
		if !periodic {
			select {
			case kicks <- struct{}{}:
			default:
			}
			return
		}
		select {
		case ticks <- cfg:
		default:
		}
	}
	cfg := dscpTestConfig(true, 7)
	dscpSyncWanted.Store(cfg)

	StartDSCPSync()
	select {
	case got := <-ticks:
		if got != cfg {
			t.Errorf("the periodic pass saw %p, want the latest saved configuration %p", got, cfg)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the ticker never ran a periodic pass")
	}
	StopDSCPSync()
	select {
	case <-kicks:
		t.Errorf("a tick ran a pass marked as a save")
	default:
	}
}

func TestSetDSCPSyncStopSkipsAPassWaitingForTheLock(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	dscpSyncIsolate(t)
	dscpSyncSetBackend(backendIPTables)
	dscpSyncTick = time.Hour
	stops := make(chan (<-chan struct{}), 1)
	dscpSyncPassFn = func(cfg *config.Config, periodic bool, stop <-chan struct{}) {
		stops <- stop
		dscpSyncPass(cfg, periodic, stop)
	}

	StartDSCPSync()
	rulesMu.Lock()
	locked := true
	t.Cleanup(func() {
		if locked {
			rulesMu.Unlock()
		}
	})
	SyncDSCP(dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.1.2.0/24")))
	stop := <-stops
	stopped := make(chan struct{})
	go func() {
		StopDSCPSync()
		close(stopped)
	}()
	<-stop
	select {
	case <-stopped:
		t.Fatal("StopDSCPSync returned while a pass was still waiting for the lock")
	default:
	}
	locked = false
	rulesMu.Unlock()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("StopDSCPSync did not return after the pass finished")
	}
	if len(h.events) != 0 || dscpApplied.Load() != nil {
		t.Errorf("a pass that got the lock after the stop still changed the firewall: %q", h.events)
	}
}
