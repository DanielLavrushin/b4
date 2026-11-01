package tables

import (
	"context"
	"fmt"
	"net"
	"net/netip"
	"slices"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

const (
	keepHeldAddr = "198.51.100.20"
	keepOwnAddr  = "198.51.100.7"
)

func keepLearnedByMatcher(t *testing.T) *atomic.Pointer[sni.SuffixSet] {
	t.Helper()
	matcher := &atomic.Pointer[sni.SuffixSet]{}
	prev := LearnedAddressKeepFunc
	LearnedAddressKeepFunc = func(cfg *config.Config, set *config.SetConfig, ip net.IP) bool {
		return matcher.Load().KeepsLearned(cfg, set, ip)
	}
	t.Cleanup(func() { LearnedAddressKeepFunc = prev })
	return matcher
}

func keepNoLearned(t *testing.T) *atomic.Int32 {
	t.Helper()
	calls := &atomic.Int32{}
	prev := LearnedAddressKeepFunc
	LearnedAddressKeepFunc = func(*config.Config, *config.SetConfig, net.IP) bool {
		calls.Add(1)
		return false
	}
	t.Cleanup(func() { LearnedAddressKeepFunc = prev })
	return calls
}

func ipPrioritySet(id string, cidrs ...string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = id, id, true
	set.Targets.IPs = append([]string(nil), cidrs...)
	set.Targets.IpsToMatch = append([]string(nil), cidrs...)
	set.Targets.IPPriority = true
	return &set
}

type keepAdd struct {
	ips []string
	ttl int
}

type keepRouting struct {
	cfg     *config.Config
	held    *config.SetConfig
	learner *config.SetConfig
	matcher *atomic.Pointer[sni.SuffixSet]
	adds    chan keepAdd
}

func keepRoutingSetup(t *testing.T, shape func(held, learner *config.SetConfig) []*config.SetConfig) *keepRouting {
	t.Helper()
	matcher := keepLearnedByMatcher(t)
	cfg, learner, be := awaitSetup(t, false)
	held := ipPrioritySet("ggc", "198.51.100.0/24")
	sets := []*config.SetConfig{held, learner}
	if shape != nil {
		sets = shape(held, learner)
	}
	cfg.Sets = sets
	routeMu.Lock()
	routeForgetSetLearnState(learner.Id)
	routeRuleCache[learner.Id] = buildRouteState(cfg, learner)
	routeMu.Unlock()
	matcher.Store(sni.NewSuffixSet(cfg.Sets))
	adds := make(chan keepAdd, 64)
	be.addElementsFn = func(_ string, ips []string, ttl int) {
		adds <- keepAdd{ips: append([]string(nil), ips...), ttl: ttl}
	}
	return &keepRouting{cfg: cfg, held: held, learner: learner, matcher: matcher, adds: adds}
}

func (k *keepRouting) learned() []string {
	var got []string
	for {
		select {
		case add := <-k.adds:
			if add.ttl > 0 {
				got = append(got, add.ips...)
			}
		default:
			slices.Sort(got)
			return got
		}
	}
}

func (k *keepRouting) claimed(t *testing.T, addr string) bool {
	t.Helper()
	key := k.learner.Id + "|" + addr
	routeAsyncSeenMu.Lock()
	_, seen := routeAsyncSeen[key]
	_, pending := routeAsyncPending[key]
	routeAsyncSeenMu.Unlock()
	routeMu.Lock()
	_, stamped := routeLearnLast[key]
	routeMu.Unlock()
	return seen || pending || stamped
}

func keepIPs(addrs ...string) []net.IP {
	ips := make([]net.IP, len(addrs))
	for i, a := range addrs {
		ips[i] = net.ParseIP(a)
	}
	return ips
}

func TestLearnedRoutingWritesLeaveOutAnAddressASetWithIPPriorityAboveHolds(t *testing.T) {
	sinks := []struct {
		name  string
		other string
		learn func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP)
	}{
		{"RoutingHandleDNS", "203.0.113.11", func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP) {
			RoutingHandleDNS(cfg, set, ips)
		}},
		{"RoutingHandleDNSAwait", "203.0.113.12", func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP) {
			for _, w := range RoutingHandleDNSAwait(cfg, set, ips) {
				awaitClosed(t, w, "the learned addresses")
			}
		}},
		{"RoutingHandleDNSAsync", "203.0.113.13", func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP) {
			RoutingHandleDNSAsync(cfg, set, ips)
			drainAsync(t)
		}},
		{"RoutingLearnIP", "203.0.113.14", func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP) {
			for _, ip := range ips {
				RoutingLearnIP(cfg, set, ip)
			}
		}},
		{"RoutingLearnIPAsync", "203.0.113.15", func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP) {
			for _, ip := range ips {
				RoutingLearnIPAsync(cfg, set, ip)
			}
			drainAsync(t)
		}},
		{"resolved addresses", "203.0.113.16", func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP) {
			if !routeAddResolvedIPs(cfg, set, ips) {
				t.Error("resolved addresses for an installed set read as a changed set")
			}
		}},
	}
	for _, sink := range sinks {
		t.Run(sink.name, func(t *testing.T) {
			k := keepRoutingSetup(t, nil)
			sink.learn(t, k.cfg, k.learner, keepIPs(keepOwnAddr, keepHeldAddr, sink.other))
			want := []string{keepOwnAddr, sink.other}
			slices.Sort(want)
			if got := k.learned(); !slices.Equal(got, want) {
				t.Fatalf("learned %v, want %v: %s is held by set %s above, and %s is the set's own address", got, want, keepHeldAddr, k.held.Id, keepOwnAddr)
			}
			if k.claimed(t, keepHeldAddr) {
				t.Fatal("the held address was stamped as learned, so the set would not learn it for a while once the switch goes off")
			}
		})
	}
}

func TestAHeldAddressAloneLeavesTheRoutingWritersNothingToDo(t *testing.T) {
	k := keepRoutingSetup(t, nil)
	held := keepIPs(keepHeldAddr)
	if waits := RoutingHandleDNSAwait(k.cfg, k.learner, held); len(waits) != 0 {
		t.Fatalf("a DNS answer that carries only a held address was given %d writes to wait for", len(waits))
	}
	if !routeAddResolvedIPs(k.cfg, k.learner, held) {
		t.Fatal("a lookup that resolved only to a held address read as a changed set, so its host would be resolved again on every packet")
	}
	RoutingHandleDNS(k.cfg, k.learner, held)
	RoutingLearnIP(k.cfg, k.learner, held[0])
	RoutingLearnIPAsync(k.cfg, k.learner, held[0])
	drainAsync(t)
	if got := k.learned(); len(got) != 0 {
		t.Fatalf("learned %v, want nothing", got)
	}
}

func TestLearnedRoutingKeepsTheAddressWhenTheSwitchCannotHoldIt(t *testing.T) {
	const phone = "aa:bb:cc:dd:ee:01"
	beforeTheSwitch := func(on bool) func(cfg *config.Config) *sni.SuffixSet {
		return func(cfg *config.Config) *sni.SuffixSet {
			built := make([]*config.SetConfig, len(cfg.Sets))
			for i, set := range cfg.Sets {
				cp := *set
				built[i] = &cp
			}
			built[0].Targets.IPPriority = on
			return sni.NewSuffixSet(built)
		}
	}
	cases := []struct {
		name    string
		shape   func(held, learner *config.SetConfig) []*config.SetConfig
		matcher func(cfg *config.Config) *sni.SuffixSet
	}{
		{name: "the learning set is limited to devices", shape: func(held, learner *config.SetConfig) []*config.SetConfig {
			learner.Targets.SourceDevices = []string{phone}
			return []*config.SetConfig{held, learner}
		}},
		{name: "the set with IP priority is limited to devices", shape: func(held, learner *config.SetConfig) []*config.SetConfig {
			held.Targets.SourceDevices = []string{phone}
			return []*config.SetConfig{held, learner}
		}},
		{name: "the switch is off", shape: func(held, learner *config.SetConfig) []*config.SetConfig {
			held.Targets.IPPriority = false
			return []*config.SetConfig{held, learner}
		}},
		{name: "the set with IP priority sits below the learning set", shape: func(held, learner *config.SetConfig) []*config.SetConfig {
			return []*config.SetConfig{learner, held}
		}},
		{name: "the set with IP priority lists a catch-all range", shape: func(held, learner *config.SetConfig) []*config.SetConfig {
			held.Targets.IPs, held.Targets.IpsToMatch = []string{"0.0.0.0/0"}, []string{"0.0.0.0/0"}
			return []*config.SetConfig{held, learner}
		}},
		{name: "the pool has no matcher yet", matcher: func(*config.Config) *sni.SuffixSet { return nil }},
		{name: "the matcher does not know the learning set", matcher: func(cfg *config.Config) *sni.SuffixSet {
			return sni.NewSuffixSet(cfg.Sets[:1])
		}},
		{name: "the matcher was built before the switch was turned on", matcher: beforeTheSwitch(false)},
		{name: "the matcher was built before the switch was turned off", shape: func(held, learner *config.SetConfig) []*config.SetConfig {
			held.Targets.IPPriority = false
			return []*config.SetConfig{held, learner}
		}, matcher: beforeTheSwitch(true)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			k := keepRoutingSetup(t, tc.shape)
			if tc.matcher != nil {
				k.matcher.Store(tc.matcher(k.cfg))
			}
			RoutingHandleDNS(k.cfg, k.learner, keepIPs(keepHeldAddr))
			if got := k.learned(); !slices.Equal(got, []string{keepHeldAddr}) {
				t.Fatalf("learned %v, want %s kept", got, keepHeldAddr)
			}
		})
	}
}

func keepChainSet(id, to string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = id, id, true
	set.Escalate.To = to
	return &set
}

func TestTheHoldingSetsEscalationChainStillLearnsTheHeldAddress(t *testing.T) {
	cases := []struct {
		name  string
		shape func(held, learner *config.SetConfig) []*config.SetConfig
		keep  bool
	}{
		{"the holding set escalates to the learning set", func(held, learner *config.SetConfig) []*config.SetConfig {
			held.Escalate.To = learner.Id
			return []*config.SetConfig{held, learner}
		}, true},
		{"the learning set is the second step of the holding set's chain", func(held, learner *config.SetConfig) []*config.SetConfig {
			held.Escalate.To = "mid"
			return []*config.SetConfig{held, keepChainSet("mid", learner.Id), learner}
		}, true},
		{"the holding set escalates to another set", func(held, learner *config.SetConfig) []*config.SetConfig {
			held.Escalate.To = "other"
			return []*config.SetConfig{held, learner, keepChainSet("other", "")}
		}, false},
		{"the chain to the learning set runs through a disabled set", func(held, learner *config.SetConfig) []*config.SetConfig {
			mid := keepChainSet("mid", learner.Id)
			mid.Enabled = false
			held.Escalate.To = mid.Id
			return []*config.SetConfig{held, mid, learner}
		}, false},
	}
	writers := []struct {
		name  string
		learn func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP)
	}{
		{"RoutingHandleDNSAsync", func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP) {
			RoutingHandleDNSAsync(cfg, set, ips)
			drainAsync(t)
		}},
		{"RoutingHandleDNS", func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP) {
			RoutingHandleDNS(cfg, set, ips)
		}},
		{"RoutingLearnIPAsync", func(t *testing.T, cfg *config.Config, set *config.SetConfig, ips []net.IP) {
			for _, ip := range ips {
				RoutingLearnIPAsync(cfg, set, ip)
			}
			drainAsync(t)
		}},
	}
	for _, tc := range cases {
		for _, writer := range writers {
			t.Run(tc.name+"/"+writer.name, func(t *testing.T) {
				k := keepRoutingSetup(t, tc.shape)
				writer.learn(t, k.cfg, k.learner, keepIPs(keepHeldAddr, keepOwnAddr))
				want := []string{keepOwnAddr}
				if tc.keep {
					want = append(want, keepHeldAddr)
				}
				slices.Sort(want)
				if got := k.learned(); !slices.Equal(got, want) {
					t.Fatalf("learned %v, want %v: %s is held by set %s above, escalate.to %q", got, want, keepHeldAddr, k.held.Id, k.held.Escalate.To)
				}
			})
		}
	}
}

func TestStaticRoutingEntriesNeverAskTheLearnedFilter(t *testing.T) {
	calls := keepNoLearned(t)
	cfg, learner, be := awaitSetup(t, false)
	stubRetryState(t)
	learner.Targets.IpsToMatch = append(learner.Targets.IpsToMatch, keepHeldAddr+"/32")
	cfg.Sets = []*config.SetConfig{ipPrioritySet("ggc", "198.51.100.0/24"), learner}
	delete(routeRuleCache, learner.Id)
	routeMu.Lock()
	delete(routeLearnedHosts, learner.Id)
	routeMu.Unlock()
	var static []string
	be.addElementsFn = func(_ string, ips []string, ttl int) {
		if ttl == 0 {
			static = append(static, ips...)
		}
	}

	RoutingSyncConfig(cfg)
	for _, entry := range []string{keepOwnAddr, keepHeldAddr + "/32"} {
		if !slices.Contains(static, entry) {
			t.Errorf("static entries written %v, want %s among them", static, entry)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("the learned filter was asked %d times while static entries were written", n)
	}
}

func TestLearnedDSCPLeavesOutAnAddressASetWithIPPriorityAboveHolds(t *testing.T) {
	const other = "203.0.113.21"
	for _, backend := range []string{backendIPTables, backendNFTables} {
		t.Run(backend, func(t *testing.T) {
			matcher := keepLearnedByMatcher(t)
			held := dscpPlanTestSet("ggc", 10, "198.51.100.0/24")
			held.Targets.IPPriority = true
			learner := dscpPlanTestSet("video", 6, "10.6.0.0/16")
			sid := routeSanitizeSetID(learner.Id)
			var cfg *config.Config
			var learned func(addr string) bool
			if backend == backendIPTables {
				h, _ := dscpLearnIptSetup(t, backendIPTables)
				cfg = dscpIptTestConfig(7, true, nil, held, learner)
				learned = func(addr string) bool { return h.sets[dscpIptLearnedSet(sid, false)][addr] }
			} else {
				f, _ := dscpLearnNftSetup(t)
				cfg = dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, held, learner)
				learned = func(addr string) bool { return slices.Contains(f.table.sets[dscpNftLearnedSet(sid, false)], addr) }
			}
			dscpLearnApply(t, cfg, backend)
			dscpLearnStart()
			matcher.Store(sni.NewSuffixSet(cfg.Sets))

			if waits := DSCPLearn(cfg, learner, dscpLearnIPs(keepHeldAddr), false); len(waits) != 0 {
				t.Fatal("a DNS answer that carries only a held address queued a learned DSCP write")
			}
			DSCPLearnAsync(cfg, learner, dscpLearnIPs(keepHeldAddr), true)
			dscpLearnWaitAll(t, DSCPLearn(cfg, learner, dscpLearnIPs(keepHeldAddr, other), true))
			if learned(keepHeldAddr) {
				t.Errorf("set %s learned %s, which set %s above holds", learner.Id, keepHeldAddr, held.Id)
			}
			if !learned(other) {
				t.Errorf("set %s did not learn %s, which no set holds", learner.Id, other)
			}
			if got := dscpLearnStats()[learner.Id]; got.tls != 1 || got.dns != 0 {
				t.Errorf("learned writes = %+v, want the one address no set holds", got)
			}
		})
	}
}

func TestTheHoldingSetsEscalationTargetStillLearnsTheHeldAddressForDSCP(t *testing.T) {
	for _, onChain := range []bool{true, false} {
		for _, backend := range []string{backendIPTables, backendNFTables} {
			t.Run(fmt.Sprintf("%s, on the chain=%v", backend, onChain), func(t *testing.T) {
				matcher := keepLearnedByMatcher(t)
				held := dscpPlanTestSet("ggc", 10, "198.51.100.0/24")
				held.Targets.IPPriority = true
				learner := dscpPlanTestSet("video", 6, "10.6.0.0/16")
				held.Escalate.To = "elsewhere"
				if onChain {
					held.Escalate.To = learner.Id
				}
				sid := routeSanitizeSetID(learner.Id)
				var cfg *config.Config
				var learned func(addr string) bool
				if backend == backendIPTables {
					h, _ := dscpLearnIptSetup(t, backendIPTables)
					cfg = dscpIptTestConfig(7, true, nil, held, learner)
					learned = func(addr string) bool { return h.sets[dscpIptLearnedSet(sid, false)][addr] }
				} else {
					f, _ := dscpLearnNftSetup(t)
					cfg = dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, held, learner)
					learned = func(addr string) bool { return slices.Contains(f.table.sets[dscpNftLearnedSet(sid, false)], addr) }
				}
				dscpLearnApply(t, cfg, backend)
				dscpLearnStart()
				matcher.Store(sni.NewSuffixSet(cfg.Sets))

				const other = "203.0.113.24"
				dscpLearnWaitAll(t, DSCPLearn(cfg, learner, dscpLearnIPs(keepHeldAddr, other), false))
				if got := learned(keepHeldAddr); got != onChain {
					t.Errorf("set %s learned %s: %v, want %v (set %s holds it, escalate.to %q)", learner.Id, keepHeldAddr, got, onChain, held.Id, held.Escalate.To)
				}
				if !learned(other) {
					t.Errorf("set %s did not learn %s, which no set holds", learner.Id, other)
				}
			})
		}
	}
}

func TestStaticDSCPRangesNeverAskTheLearnedFilter(t *testing.T) {
	calls := keepNoLearned(t)
	f, _ := dscpLearnNftSetup(t)
	held := dscpPlanTestSet("ggc", 10, "198.51.100.0/24")
	held.Targets.IPPriority = true
	learner := dscpPlanTestSet("video", 6, keepHeldAddr+"/32")
	cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, held, learner)

	dscpLearnApply(t, cfg, backendNFTables)
	for addr, want := range map[string]int{keepHeldAddr: 6, "198.51.100.21": 10} {
		if got, _ := f.stamp(netip.MustParseAddr(addr), "wan"); got != want {
			t.Errorf("%s leaves with DSCP %d, want the static value %d", addr, got, want)
		}
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("the learned filter was asked %d times while static ranges were written", n)
	}
}

func TestTheDSCPPreResolveFollowsASaveThatOnlyTurnsTheSwitchOn(t *testing.T) {
	const other = "203.0.113.23"
	matcher := keepLearnedByMatcher(t)
	dscpLearnIptSetup(t, backendIPTables)
	before := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("ggc", 10, "198.51.100.0/24"), dscpPreDomainSet("video", 6, "video.example"))
	dscpLearnApply(t, before, backendIPTables)
	saved := before.Clone()
	saved.Sets[0].Targets.IPPriority = true
	matcher.Store(sni.NewSuffixSet(saved.Sets))
	queue := dscpLearnManualQueue(t, dscpLearnQueueSize)
	dscpPreResolveStart()
	dscpPreLookup = func(context.Context, *config.Config, *config.SetConfig, string) []net.IP {
		return dscpLearnIPs(keepHeldAddr, other)
	}
	rulesMu.Lock()
	closed := dscpSyncClosed.Load()
	dscpSyncClosed.Store(false)
	rulesMu.Unlock()
	t.Cleanup(func() { dscpSyncClosed.Store(closed) })

	dscpSyncPass(saved, true, nil)
	dscpPreWG.Wait()
	if st := dscpApplied.Load(); st == nil || st.cfg != before {
		t.Fatal("the switch changed the DSCP plan, so this no longer covers a save the plan does not see")
	}
	if got := dscpPreDrain(queue); !slices.Equal(got, []string{other}) {
		t.Fatalf("the periodic pre-resolve queued %v, want only %s: it must judge the addresses by the saved config, not the one the unchanged plan was applied with", got, other)
	}
}

func TestDSCPPreResolveLeavesOutAnAddressASetWithIPPriorityAboveHolds(t *testing.T) {
	const other = "203.0.113.22"
	matcher := keepLearnedByMatcher(t)
	dscpLearnIptSetup(t, backendIPTables)
	held := dscpPlanTestSet("ggc", 10, "198.51.100.0/24")
	held.Targets.IPPriority = true
	learner := dscpPreDomainSet("video", 6, "held.example", "video.example")
	cfg := dscpIptTestConfig(7, true, nil, held, learner)
	dscpLearnApply(t, cfg, backendIPTables)
	matcher.Store(sni.NewSuffixSet(cfg.Sets))
	queue := dscpLearnManualQueue(t, dscpLearnQueueSize)
	dscpPreResolveStart()
	dscpPreLookup = func(_ context.Context, _ *config.Config, _ *config.SetConfig, host string) []net.IP {
		if host == "held.example" {
			return dscpLearnIPs(keepHeldAddr)
		}
		return dscpLearnIPs(keepHeldAddr, other)
	}

	dscpPreTrigger()
	if got := dscpPreDrain(queue); !slices.Equal(got, []string{other}) {
		t.Fatalf("the pre-resolve queued %v, want only %s: %s is held by set %s above", got, other, keepHeldAddr, held.Id)
	}
}
