package tables

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net"
	"net/netip"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type dscpLearnClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *dscpLearnClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *dscpLearnClock) advance(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

func dscpLearnResetState() {
	dscpLearnMu.Lock()
	dscpLearnCur.Store(nil)
	dscpLearnClaimMu.Lock()
	clear(dscpLearnKeys)
	clear(dscpLearnUnion)
	clear(dscpLearnPending)
	clear(dscpLearnOpen)
	clear(dscpLearnCounts)
	dscpLearnClaimMu.Unlock()
	dscpLearnMu.Unlock()
	dscpLearnDropped.Store(0)
	dscpPreMu.Lock()
	clear(dscpPreRuns)
	dscpPreMu.Unlock()
}

func dscpLearnIsolate(t *testing.T) *dscpLearnClock {
	t.Helper()
	StopDSCPSync()
	dscpPreResolveStop()
	dscpLearnStop()
	dscpLearnResetState()
	clock := &dscpLearnClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	origNow, origLookup := dscpLearnNow, dscpPreLookup
	dscpLearnNow = clock.Now
	dscpPreLookup = func(_ context.Context, _ *config.Config, _ *config.SetConfig, host string) []net.IP {
		t.Errorf("a pre-resolve lookup of %s ran without a stub", host)
		return nil
	}
	t.Cleanup(func() {
		dscpPreResolveStop()
		dscpLearnStop()
		dscpLearnResetState()
		dscpLearnNow, dscpPreLookup = origNow, origLookup
	})
	return clock
}

type dscpLearnIpt struct {
	*dscpIptHost
	restores []string
	timeouts map[string]map[string]int
	hold     map[string]chan struct{}
}

func dscpLearnIptSetup(t *testing.T, bins ...string) (*dscpLearnIpt, *dscpLearnClock) {
	t.Helper()
	if len(bins) == 0 {
		bins = []string{backendIPTables, backendIP6Tables}
	}
	h := &dscpLearnIpt{dscpIptHost: dscpIptNewHost(t, bins...), timeouts: map[string]map[string]int{}, hold: map[string]chan struct{}{}}
	base := runStdin
	runStdin = func(stdin string, args ...string) error {
		if !strings.Contains(stdin, " timeout ") {
			return base(stdin, args...)
		}
		return h.restore(stdin, args...)
	}
	t.Cleanup(func() { runStdin = base })
	return h, dscpLearnIsolate(t)
}

func (h *dscpLearnIpt) restore(stdin string, args ...string) error {
	if strings.Join(args, " ") != "ipset restore -exist" {
		return errors.New("unexpected command")
	}
	text := strings.TrimSpace(stdin)
	h.restores = append(h.restores, text)
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) != 5 || f[0] != "add" || f[3] != "timeout" || h.sets[f[1]] == nil {
			return fmt.Errorf("command [ipset restore -exist] failed: exit status 1 (ipset v7.19: Error in line 1: %s)", line)
		}
		if gate, ok := h.hold[f[2]]; ok {
			<-gate
		}
	}
	for _, line := range lines {
		f := strings.Fields(line)
		seconds, _ := strconv.Atoi(f[4])
		h.sets[f[1]][f[2]] = true
		if h.timeouts[f[1]] == nil {
			h.timeouts[f[1]] = map[string]int{}
		}
		h.timeouts[f[1]][f[2]] = seconds
	}
	return nil
}

type dscpLearnNft struct {
	*dscpNftFake
	refreshes []string
	timeouts  map[string]map[string]string
}

func dscpLearnNftSetup(t *testing.T) (*dscpLearnNft, *dscpLearnClock) {
	t.Helper()
	f := &dscpLearnNft{dscpNftFake: installDSCPNftFake(t), timeouts: map[string]map[string]string{}}
	base := run
	run = func(args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		if !strings.HasPrefix(cmd, "nft add element inet "+dscpNftTable+" ") {
			return base(args...)
		}
		return f.refresh(cmd)
	}
	t.Cleanup(func() { run = base })
	return f, dscpLearnIsolate(t)
}

func (f *dscpLearnNft) refresh(cmd string) (string, error) {
	f.refreshes = append(f.refreshes, cmd)
	parts := strings.Split(cmd, " ; ")
	head, body, ok := strings.Cut(parts[0], " { ")
	fields := strings.Fields(head)
	if len(parts) != 3 || !ok || len(fields) != 6 {
		return "", fmt.Errorf("unexpected command %q", cmd)
	}
	name := fields[5]
	if f.table == nil || f.table.sets[name] == nil {
		return "Error: No such file or directory", errors.New("exit status 1")
	}
	for _, element := range strings.Split(strings.TrimSuffix(body, " }"), " , ") {
		e := strings.Fields(element)
		if !slices.Contains(f.table.sets[name], e[0]) {
			f.table.sets[name] = append(f.table.sets[name], e[0])
		}
		if f.timeouts[name] == nil {
			f.timeouts[name] = map[string]string{}
		}
		f.timeouts[name][e[0]] = e[2]
	}
	return "", nil
}

func dscpLearnIPs(addrs ...string) []net.IP {
	ips := make([]net.IP, len(addrs))
	for i, a := range addrs {
		ips[i] = net.ParseIP(a)
	}
	return ips
}

func dscpLearnWaitAll(t *testing.T, waits []<-chan struct{}) {
	t.Helper()
	if len(waits) == 0 {
		t.Fatal("the learner gave nothing to wait for, so it queued no write")
	}
	for _, w := range waits {
		select {
		case <-w:
		case <-time.After(5 * time.Second):
			t.Fatal("a learned address was never written")
		}
	}
}

func dscpLearnClosed(waits []<-chan struct{}) bool {
	for _, w := range waits {
		select {
		case <-w:
		default:
			return false
		}
	}
	return true
}

func dscpLearnApply(t *testing.T, cfg *config.Config, backend string) {
	t.Helper()
	if err := applyDSCPFor(cfg, backend); err != nil {
		t.Fatal(err)
	}
}

func dscpLearnExpiry(sid, addr string) time.Time {
	dscpLearnClaimMu.Lock()
	defer dscpLearnClaimMu.Unlock()
	return dscpLearnKeys[dscpLearnKey{sid: sid, addr: netip.MustParseAddr(addr)}]
}

func dscpLearnManualQueue(t *testing.T, size int) chan *dscpLearnBatch {
	t.Helper()
	ch := make(chan *dscpLearnBatch, size)
	dscpLearnClaimMu.Lock()
	dscpLearnCh = ch
	dscpLearnClaimMu.Unlock()
	t.Cleanup(func() {
		dscpLearnClaimMu.Lock()
		dscpLearnCh = nil
		dscpLearnClaimMu.Unlock()
		for {
			select {
			case batch := <-ch:
				dscpLearnSettle(batch)
			default:
				return
			}
		}
	})
	return ch
}

func dscpLearnRoutedSet(id string, value, ttl int, targets ...string) *config.SetConfig {
	set := dscpPlanTestSet(id, value, targets...)
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeInterface
	set.Routing.EgressInterface = "wg0"
	set.Routing.IPTTLSeconds = ttl
	return set
}

func TestSetDSCPLearnUnionWriteIpt(t *testing.T) {
	h, clock := dscpLearnIptSetup(t)
	a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
	b := dscpPlanTestSet("b", 6, "10.2.0.0/16")
	cfg := dscpIptTestConfig(7, true, nil, a, b)
	dscpLearnApply(t, cfg, backendIPTables)
	dscpLearnStart()
	sidA, sidB := routeSanitizeSetID("a"), routeSanitizeSetID("b")

	h.events = nil
	dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.10", "2001:db8::10"), false))
	want := strings.Join([]string{
		"add " + dscpIptLearnedSet(sidA, false) + " 203.0.113.10 timeout 3600",
		"add " + dscpIptLearnedUnionSet(false) + " 203.0.113.10 timeout 3600",
		"add " + dscpIptLearnedSet(sidA, true) + " 2001:db8::10 timeout 3600",
		"add " + dscpIptLearnedUnionSet(true) + " 2001:db8::10 timeout 3600",
	}, "\n")
	if len(h.restores) != 1 || h.restores[0] != want {
		t.Fatalf("restores = %q, want one ipset restore writing the set's and the union's entries:\n%s", h.restores, want)
	}
	for _, e := range h.events {
		if strings.HasPrefix(e, "ipset ") {
			t.Errorf("learning ran another ipset command next to the restore: %s", e)
		}
	}
	for name, addr := range map[string]string{
		dscpIptLearnedSet(sidA, false): "203.0.113.10",
		dscpIptLearnedUnionSet(false):  "203.0.113.10",
		dscpIptLearnedSet(sidA, true):  "2001:db8::10",
		dscpIptLearnedUnionSet(true):   "2001:db8::10",
	} {
		if !h.sets[name][addr] {
			t.Errorf("%s does not hold %s: %v", name, addr, h.entries(name))
		}
	}
	if got := h.entries(dscpIptLearnedSet(sidB, false)); len(got) != 0 {
		t.Errorf("set b learned addresses of set a: %v", got)
	}

	dscpLearnWaitAll(t, DSCPLearn(cfg, b, dscpLearnIPs("203.0.113.10"), true))
	if got, want := h.restores[1], "add "+dscpIptLearnedSet(sidB, false)+" 203.0.113.10 timeout 600"; got != want {
		t.Errorf("a shorter TLS entry of another set rewrote the union: %q, want only %q", got, want)
	}
	if got := h.timeouts[dscpIptLearnedUnionSet(false)]["203.0.113.10"]; got != 3600 {
		t.Errorf("the union entry was shortened to %d s while set a's entry lives 3600 s", got)
	}

	clock.advance(3300 * time.Second)
	dscpLearnWaitAll(t, DSCPLearn(cfg, b, dscpLearnIPs("203.0.113.10"), true))
	want = strings.Join([]string{
		"add " + dscpIptLearnedSet(sidB, false) + " 203.0.113.10 timeout 600",
		"add " + dscpIptLearnedUnionSet(false) + " 203.0.113.10 timeout 600",
	}, "\n")
	if got := h.restores[2]; got != want {
		t.Errorf("an entry outliving the union must extend it too: %q, want %q", got, want)
	}
	if got := dscpLearnStats(); got["a"].dns != 2 || got["b"].tls != 2 || got["a"].tls != 0 || got["b"].dns != 0 {
		t.Errorf("learned writes by set and source = %+v", got)
	}
}

func TestSetDSCPLearnNftRefreshTable(t *testing.T) {
	f, _ := dscpLearnNftSetup(t)
	a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
	cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, a)
	dscpLearnApply(t, cfg, backendNFTables)
	dscpLearnStart()
	sid := routeSanitizeSetID("a")

	dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.10", "2001:db8::10"), false))
	v4, v6 := dscpNftLearnedSet(sid, false), dscpNftLearnedSet(sid, true)
	want := []string{
		"nft add element inet b4_dscp " + v4 + " { 203.0.113.10 timeout 3600s } ; delete element inet b4_dscp " + v4 + " { 203.0.113.10 } ; add element inet b4_dscp " + v4 + " { 203.0.113.10 timeout 3600s }",
		"nft add element inet b4_dscp " + v6 + " { 2001:db8::10 timeout 3600s } ; delete element inet b4_dscp " + v6 + " { 2001:db8::10 } ; add element inet b4_dscp " + v6 + " { 2001:db8::10 timeout 3600s }",
	}
	if !slices.Equal(f.refreshes, want) {
		t.Fatalf("refreshes = %q, want %q", f.refreshes, want)
	}
	for addr, value := range map[string]int{"203.0.113.10": 31, "2001:db8::10": 31, "10.1.2.3": 31, "192.0.2.1": 7} {
		if got, _ := f.stamp(netip.MustParseAddr(addr), "wan"); got != value {
			t.Errorf("%s leaves with DSCP %d, want %d", addr, got, value)
		}
	}

	routed := strings.Join(routeNftRefreshArgs(routeNftTable, "b4r_x_v4_d", []string{"203.0.113.9"}, 600), " ")
	if want := "nft add element inet b4_route b4r_x_v4_d { 203.0.113.9 timeout 600s } ; delete element inet b4_route b4r_x_v4_d { 203.0.113.9 } ; add element inet b4_route b4r_x_v4_d { 203.0.113.9 timeout 600s }"; routed != want {
		t.Errorf("routing's refresh changed: %q, want %q", routed, want)
	}
}

func TestSetDSCPLearnTLSTTL(t *testing.T) {
	plain := func() *config.SetConfig { return dscpPlanTestSet("plain", 31, "10.1.2.0/24") }
	routed := func() *config.SetConfig { return dscpLearnRoutedSet("routed", 6, 7200, "10.2.0.0/16") }

	t.Run("iptables", func(t *testing.T) {
		h, _ := dscpLearnIptSetup(t, backendIPTables)
		p, r := plain(), routed()
		cfg := dscpIptTestConfig(7, true, nil, p, r)
		dscpLearnApply(t, cfg, backendIPTables)
		dscpLearnStart()
		cases := []struct {
			set     *config.SetConfig
			addr    string
			fromTLS bool
			want    int
		}{
			{p, "203.0.113.1", true, 600},
			{p, "203.0.113.2", false, 3600},
			{r, "203.0.113.3", true, 600},
			{r, "203.0.113.4", false, 7200},
		}
		for _, c := range cases {
			dscpLearnWaitAll(t, DSCPLearn(cfg, c.set, dscpLearnIPs(c.addr), c.fromTLS))
			name := dscpIptLearnedSet(routeSanitizeSetID(c.set.Id), false)
			if got := h.timeouts[name][c.addr]; got != c.want {
				t.Errorf("set %s, TLS %v: %s written with timeout %d, want %d", c.set.Id, c.fromTLS, c.addr, got, c.want)
			}
		}
	})

	t.Run("nftables", func(t *testing.T) {
		f, _ := dscpLearnNftSetup(t)
		p, r := plain(), routed()
		cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, p, r)
		dscpLearnApply(t, cfg, backendNFTables)
		dscpLearnStart()
		for _, c := range []struct {
			set     *config.SetConfig
			addr    string
			fromTLS bool
			want    string
		}{
			{p, "203.0.113.1", true, "600s"},
			{p, "203.0.113.2", false, "3600s"},
			{r, "203.0.113.3", true, "600s"},
			{r, "203.0.113.4", false, "7200s"},
		} {
			dscpLearnWaitAll(t, DSCPLearn(cfg, c.set, dscpLearnIPs(c.addr), c.fromTLS))
			name := dscpNftLearnedSet(routeSanitizeSetID(c.set.Id), false)
			if got := f.timeouts[name][c.addr]; got != c.want {
				t.Errorf("set %s, TLS %v: %s written with timeout %q, want %q", c.set.Id, c.fromTLS, c.addr, got, c.want)
			}
		}
	})
}

func TestSetDSCPLearnDedupeNeverShortens(t *testing.T) {
	h, clock := dscpLearnIptSetup(t, backendIPTables)
	a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
	cfg := dscpIptTestConfig(7, true, nil, a)
	dscpLearnApply(t, cfg, backendIPTables)
	dscpLearnStart()
	sid := routeSanitizeSetID("a")
	const addr = "203.0.113.10"

	var last time.Time
	var timeouts []int
	step := func(stage string, advance time.Duration, fromTLS, writes bool) {
		t.Helper()
		clock.advance(advance)
		before := len(h.restores)
		waits := DSCPLearn(cfg, a, dscpLearnIPs(addr), fromTLS)
		if !writes {
			if len(waits) != 0 {
				t.Fatalf("%s: a live entry was queued for a write", stage)
			}
		} else {
			dscpLearnWaitAll(t, waits)
		}
		if got := len(h.restores) - before; (got == 1) != writes {
			t.Fatalf("%s: %d writes, want a write %v", stage, got, writes)
		}
		if writes {
			timeouts = append(timeouts, h.timeouts[dscpIptLearnedSet(sid, false)][addr])
		}
		expiry := dscpLearnExpiry(sid, addr)
		if expiry.Before(last) {
			t.Fatalf("%s: the entry's life went from %s down to %s", stage, last, expiry)
		}
		last = expiry
	}

	step("the first DNS answer", 0, false, true)
	step("the same answer again", 0, false, false)
	step("a TLS name while the DNS entry lives long", 100*time.Second, true, false)
	step("a DNS answer with more than half the life left", 1699*time.Second, false, false)
	step("a DNS answer with less than half the life left", 2*time.Second, false, true)
	step("a TLS name close to the end of the DNS entry", 3399*time.Second, true, true)
	step("a DNS answer extends the TLS entry", 100*time.Second, false, true)
	step("a TLS name right after", 0, true, false)
	if !slices.Equal(timeouts, []int{3600, 3600, 600, 3600}) {
		t.Errorf("written timeouts %v, want [3600 3600 600 3600]", timeouts)
	}
}

func TestSetDSCPLearnPendingWriteKeepsTheLongerTTL(t *testing.T) {
	const addr = "203.0.113.10"
	type claim struct {
		name   string
		source dscpLearnSource
	}
	tls := claim{"a TLS name", dscpLearnFromTLS}
	dns := claim{"a DNS answer", dscpLearnFromDNS}
	pre := claim{"a lookup in advance", dscpLearnFromPreResolve}
	for _, order := range [][]claim{{tls, dns}, {tls, pre}, {dns, tls}} {
		t.Run(order[1].name+" while "+order[0].name+" waits to be written", func(t *testing.T) {
			h, clock := dscpLearnIptSetup(t, backendIPTables)
			a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
			dscpLearnApply(t, dscpIptTestConfig(7, true, nil, a), backendIPTables)
			queue := dscpLearnManualQueue(t, 8)
			var waits []<-chan struct{}
			for _, c := range order {
				waits = append(waits, dscpLearnClaim(a.Id, dscpLearnIPs(addr), c.source)...)
			}
			for len(queue) > 0 {
				dscpLearnRun(<-queue)
			}
			if !dscpLearnClosed(waits) {
				t.Fatal("a wait stayed open after the queue ran")
			}
			sid := routeSanitizeSetID("a")
			for _, name := range []string{dscpIptLearnedSet(sid, false), dscpIptLearnedUnionSet(false)} {
				if got := h.timeouts[name][addr]; got != 3600 {
					t.Errorf("%s holds %s with timeout %d, want the DNS TTL of 3600", name, addr, got)
				}
			}
			if got, want := dscpLearnExpiry(sid, addr), clock.Now().Add(time.Hour); !got.Equal(want) {
				t.Errorf("the entry is recorded until %s, want %s", got, want)
			}
		})
	}

	t.Run("a DNS answer while a TLS write is running", func(t *testing.T) {
		h, _ := dscpLearnIptSetup(t, backendIPTables)
		a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
		cfg := dscpIptTestConfig(7, true, nil, a)
		dscpLearnApply(t, cfg, backendIPTables)
		gate := make(chan struct{})
		release := sync.OnceFunc(func() { close(gate) })
		t.Cleanup(release)
		h.hold[addr] = gate
		dscpLearnStart()
		first := DSCPLearn(cfg, a, dscpLearnIPs(addr), true)
		deadline := time.Now().Add(5 * time.Second)
		for dscpLearnBusy() {
			if time.Now().After(deadline) {
				t.Fatal("the learner never picked up the TLS write")
			}
			time.Sleep(time.Millisecond)
		}
		second := DSCPLearn(cfg, a, dscpLearnIPs(addr), false)
		if len(second) != 1 || len(first) != 1 || second[0] == first[0] {
			t.Fatalf("the DNS answer joined the running TLS write instead of queueing its own: %d and %d waits", len(first), len(second))
		}
		release()
		dscpLearnWaitAll(t, append(first, second...))
		if got := h.timeouts[dscpIptLearnedSet(routeSanitizeSetID("a"), false)][addr]; got != 3600 {
			t.Errorf("the entry ends with timeout %d, want the DNS TTL of 3600", got)
		}
		if got := dscpLearnStats()["a"]; got.dns != 1 || got.tls != 1 {
			t.Errorf("learned writes by source = %+v, want one DNS and one TLS write", got)
		}
	})

	t.Run("nftables", func(t *testing.T) {
		f, _ := dscpLearnNftSetup(t)
		a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
		dscpLearnApply(t, dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, a), backendNFTables)
		queue := dscpLearnManualQueue(t, 8)
		dscpLearnClaim(a.Id, dscpLearnIPs(addr), dscpLearnFromTLS)
		dscpLearnClaim(a.Id, dscpLearnIPs(addr), dscpLearnFromDNS)
		for len(queue) > 0 {
			dscpLearnRun(<-queue)
		}
		if got := f.timeouts[dscpNftLearnedSet(routeSanitizeSetID("a"), false)][addr]; got != "3600s" {
			t.Errorf("the entry ends with timeout %q, want the DNS TTL of 3600s", got)
		}
	})
}

func TestSetDSCPLearnDropsUnappliedSet(t *testing.T) {
	h, _ := dscpLearnIptSetup(t, backendIPTables)
	a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
	proxy := dscpPlanTestSet("proxy", 46, "10.3.0.0/16")
	proxy.Routing.Enabled, proxy.Routing.Mode = true, config.RoutingModeProxy
	proxy.Routing.Upstream.Host, proxy.Routing.Upstream.Port = "192.0.2.10", 1080
	cfg := dscpIptTestConfig(7, true, nil, a, proxy)
	dscpLearnApply(t, cfg, backendIPTables)
	queue := dscpLearnManualQueue(t, 8)

	stranger := dscpPlanTestSet("stranger", 12, "10.4.0.0/16")
	domainOnly := dscpPlanTestSet("a", 31, "10.1.2.0/24")
	domainOnly.Targets.DomainOnly = true
	discovery := dscpIptTestConfig(7, true, nil, a, proxy)
	discovery.Queue.IsDiscovery = true
	for name, waits := range map[string][]<-chan struct{}{
		"a set that is not in the configuration": DSCPLearn(cfg, stranger, dscpLearnIPs("203.0.113.1"), false),
		"a set whose DSCP is refused":            DSCPLearn(cfg, proxy, dscpLearnIPs("203.0.113.2"), false),
		"a domain-only set":                      DSCPLearn(cfg, domainOnly, dscpLearnIPs("203.0.113.3"), false),
		"a Discovery run":                        DSCPLearn(discovery, a, dscpLearnIPs("203.0.113.4"), false),
		"no addresses":                           DSCPLearn(cfg, a, nil, false),
	} {
		if len(waits) != 0 || len(queue) != 0 {
			t.Errorf("%s: the learner queued a write", name)
		}
	}

	waits := DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.5"), false)
	if len(waits) != 1 || len(queue) != 1 {
		t.Fatalf("an applied set queued %d batches with %d waits, want 1 and 1", len(queue), len(waits))
	}
	dscpLearnApply(t, dscpIptTestConfig(7, true, nil, proxy), backendIPTables)
	if again := DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.6"), false); len(again) != 0 {
		t.Errorf("a set that left the plan still learns")
	}
	h.events = nil
	dscpLearnRun(<-queue)
	if !dscpLearnClosed(waits) {
		t.Error("the wait of a batch for a set that left the plan was not released")
	}
	if len(h.restores) != 0 || len(h.events) != 0 {
		t.Errorf("a batch for a set that left the plan was written: restores %q, commands %q", h.restores, h.events)
	}
	if got := dscpLearnStats(); len(got) != 0 {
		t.Errorf("counters %+v for a plan without the set", got)
	}
}

func TestSetDSCPLearnDroppedBatchReleasesWait(t *testing.T) {
	t.Run("a full queue", func(t *testing.T) {
		h, _ := dscpLearnIptSetup(t, backendIPTables)
		a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
		cfg := dscpIptTestConfig(7, true, nil, a)
		dscpLearnApply(t, cfg, backendIPTables)
		queue := dscpLearnManualQueue(t, 1)
		filler := &dscpLearnBatch{setID: "filler", done: make(chan struct{})}
		queue <- filler

		waits := DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.10"), false)
		if len(waits) != 1 || !dscpLearnClosed(waits) {
			t.Fatalf("a batch that did not fit the queue must hand back a released wait, got %d waits, released %v", len(waits), dscpLearnClosed(waits))
		}
		if n := dscpLearnDropped.Load(); n != 1 {
			t.Errorf("dropped batches = %d, want 1", n)
		}
		dscpLearnClaimMu.Lock()
		pending, open := len(dscpLearnPending), len(dscpLearnOpen)
		dscpLearnClaimMu.Unlock()
		if pending != 0 || open != 0 {
			t.Errorf("a dropped batch left %d pending addresses and %d open batches", pending, open)
		}

		<-queue
		dscpLearnSettle(filler)
		waits = DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.10"), false)
		if len(waits) != 1 || dscpLearnClosed(waits) || len(queue) != 1 {
			t.Fatalf("the dropped address was not queued again once the queue had room")
		}
		dscpLearnRun(<-queue)
		if !dscpLearnClosed(waits) || !h.sets[dscpIptLearnedSet(routeSanitizeSetID("a"), false)]["203.0.113.10"] {
			t.Errorf("the address queued again was not written")
		}
	})

	t.Run("stop", func(t *testing.T) {
		h, _ := dscpLearnIptSetup(t, backendIPTables)
		a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
		cfg := dscpIptTestConfig(7, true, nil, a)
		dscpLearnApply(t, cfg, backendIPTables)
		gate := make(chan struct{})
		h.hold["203.0.113.10"] = gate
		dscpLearnStart()

		first := DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.10"), false)
		deadline := time.Now().Add(5 * time.Second)
		for dscpLearnBusy() {
			if time.Now().After(deadline) {
				t.Fatal("the learner never picked up the first batch")
			}
			time.Sleep(time.Millisecond)
		}
		queued := DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.11"), false)
		if len(first) != 1 || len(queued) != 1 || dscpLearnClosed(first) || dscpLearnClosed(queued) {
			t.Fatalf("the batches were not in flight: %d and %d waits", len(first), len(queued))
		}
		stopped := make(chan struct{})
		go func() {
			dscpLearnStop()
			close(stopped)
		}()
		close(gate)
		select {
		case <-stopped:
		case <-time.After(5 * time.Second):
			t.Fatal("stopping the learner hung")
		}
		if !dscpLearnClosed(first) || !dscpLearnClosed(queued) {
			t.Errorf("stopping the learner left waits open: first %v, queued %v", dscpLearnClosed(first), dscpLearnClosed(queued))
		}
		if waits := DSCPLearn(cfg, a, dscpLearnIPs("203.0.113.12"), false); len(waits) != 0 {
			t.Errorf("a stopped learner still queues writes")
		}
	})
}

func dscpLearnBusy() bool {
	dscpLearnClaimMu.Lock()
	defer dscpLearnClaimMu.Unlock()
	return len(dscpLearnOpen) > 0
}

func TestSetDSCPLearnBatchLimit(t *testing.T) {
	dscpLearnIptSetup(t, backendIPTables)
	a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
	cfg := dscpIptTestConfig(7, true, nil, a)
	dscpLearnApply(t, cfg, backendIPTables)
	queue := dscpLearnManualQueue(t, dscpLearnQueueSize)
	if dscpLearnQueueSize != 1024 || dscpLearnBatchMax != 128 {
		t.Fatalf("queue %d batches of %d addresses, the design asks for 1024 of at most 128", dscpLearnQueueSize, dscpLearnBatchMax)
	}

	var ips []net.IP
	for i := range 300 {
		ips = append(ips, net.IPv4(198, 18, byte(i/256), byte(i%256)))
	}
	waits := DSCPLearn(cfg, a, ips, false)
	more := DSCPLearn(cfg, a, dscpLearnIPs("198.19.0.1"), false)
	tls := DSCPLearn(cfg, a, dscpLearnIPs("198.19.0.2"), true)
	var sizes []int
	for len(queue) > 0 {
		batch := <-queue
		sizes = append(sizes, len(batch.addrs))
		dscpLearnSettle(batch)
	}
	if len(waits) != 3 || !slices.Equal(sizes, []int{128, 128, 45, 1}) {
		t.Errorf("300 addresses went out in batches %v with %d waits; later addresses must join the open batch and a TLS name gets its own", sizes, len(waits))
	}
	if len(more) != 1 || more[0] != waits[2] || len(tls) != 1 || tls[0] == waits[2] {
		t.Errorf("waits of the later calls do not follow their batches")
	}
}

func TestSetDSCPLearnKeysBounded(t *testing.T) {
	clock := dscpLearnIsolate(t)
	now := clock.Now()
	if dscpLearnKeysMax != 65536 {
		t.Fatalf("the learner keeps %d records, the design asks for 64k", dscpLearnKeysMax)
	}
	keep := dscpLearnKeysMax * 3 / 4
	addr := func(i int) netip.Addr {
		return netip.AddrFrom4([4]byte{10, byte(i >> 16), byte(i >> 8), byte(i)})
	}
	fresh := netip.MustParseAddr("192.0.2.1")
	later := func(i int) time.Time { return now.Add(time.Duration(i+1) * time.Millisecond) }
	for _, tc := range []struct {
		name   string
		expiry func(i int) time.Time
		want   int
		oldest int
	}{
		{"expired records go first", func(i int) time.Time {
			if i < 20000 {
				return now.Add(-time.Second)
			}
			return later(i)
		}, dscpLearnKeysMax - 20000, 20000},
		{"then the earliest expiries", later, keep, dscpLearnKeysMax - keep},
		{"ties stop at the floor", func(int) time.Time { return now.Add(time.Minute) }, keep, -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dscpLearnClaimMu.Lock()
			clear(dscpLearnKeys)
			clear(dscpLearnUnion)
			for i := range dscpLearnKeysMax - 1 {
				dscpLearnKeys[dscpLearnKey{sid: "s", addr: addr(i)}] = tc.expiry(i)
				dscpLearnUnion[addr(i)] = tc.expiry(i)
			}
			dscpLearnClaimMu.Unlock()
			dscpLearnRecord(&dscpLearnBatch{setID: "a", sid: "s"}, now, []netip.Addr{fresh}, []bool{true}, nil, time.Hour)
			dscpLearnClaimMu.Lock()
			defer dscpLearnClaimMu.Unlock()
			if len(dscpLearnKeys) != tc.want || len(dscpLearnUnion) != tc.want {
				t.Fatalf("%d records and %d union records past the cap, want %d", len(dscpLearnKeys), len(dscpLearnUnion), tc.want)
			}
			if tc.oldest < 0 {
				return
			}
			for i := tc.oldest; i < dscpLearnKeysMax-1; i++ {
				if _, ok := dscpLearnKeys[dscpLearnKey{sid: "s", addr: addr(i)}]; !ok {
					t.Fatalf("record %d was dropped before an older one", i)
				}
			}
			_, key := dscpLearnKeys[dscpLearnKey{sid: "s", addr: fresh}]
			_, union := dscpLearnUnion[fresh]
			if !key || !union {
				t.Errorf("the record just written was dropped: key %v, union %v", key, union)
			}
		})
	}
}

func TestSetDSCPLearnResetOnTeardown(t *testing.T) {
	a := func() *config.SetConfig { return dscpPlanTestSet("a", 31, "10.1.2.0/24") }

	t.Run("iptables", func(t *testing.T) {
		h, _ := dscpLearnIptSetup(t, backendIPTables)
		set := a()
		cfg := dscpIptTestConfig(7, true, nil, set)
		dscpLearnApply(t, cfg, backendIPTables)
		dscpLearnStart()
		learned := dscpIptLearnedSet(routeSanitizeSetID("a"), false)

		dscpLearnWaitAll(t, DSCPLearn(cfg, set, dscpLearnIPs("203.0.113.10"), false))
		if waits := DSCPLearn(cfg, set, dscpLearnIPs("203.0.113.10"), false); len(waits) != 0 {
			t.Fatal("a live entry was queued again")
		}

		clearDSCPFor(cfg, backendIPTables)
		if len(h.sets) != 0 {
			t.Fatalf("the teardown left ipsets %v", h.snapshot())
		}
		if waits := DSCPLearn(cfg, set, dscpLearnIPs("203.0.113.10"), false); len(waits) != 0 {
			t.Error("the learner queued a write while nothing was applied")
		}
		if got := dscpLearnStats(); len(got) != 0 {
			t.Errorf("counters survived the teardown: %+v", got)
		}

		dscpLearnApply(t, cfg, backendIPTables)
		dscpLearnWaitAll(t, DSCPLearn(cfg, set, dscpLearnIPs("203.0.113.10"), false))
		if !h.sets[learned]["203.0.113.10"] || len(h.restores) != 2 {
			t.Errorf("the address learned before the teardown was not written again: %v, %d restores", h.entries(learned), len(h.restores))
		}

		monitor := func(drop func()) {
			t.Helper()
			chain := h.mangle.chains[backendIPTables][dscpChainName]
			h.mangle.chains[backendIPTables][dscpChainName] = chain[:len(chain)-1]
			drop()
			rulesMu.Lock()
			restored := ensureDSCPLocked(cfg, false)
			rulesMu.Unlock()
			if !restored {
				t.Fatal("the monitor did not restore the broken chain")
			}
		}
		monitor(func() {})
		if waits := DSCPLearn(cfg, set, dscpLearnIPs("203.0.113.10"), false); len(waits) != 0 || len(h.restores) != 2 {
			t.Errorf("after the monitor rebuilt only the chain the learner rewrote an entry its ipset still holds: %d waits, %d restores", len(waits), len(h.restores))
		}
		union := dscpIptLearnedUnionSet(false)
		for i, name := range []string{learned, union} {
			monitor(func() { delete(h.sets, name) })
			dscpLearnWaitAll(t, DSCPLearn(cfg, set, dscpLearnIPs("203.0.113.10"), false))
			if !h.sets[learned]["203.0.113.10"] || !h.sets[union]["203.0.113.10"] || len(h.restores) != 3+i {
				t.Errorf("after %s was made again the learner trusted what it wrote before: %v, union %v, %d restores", name, h.entries(learned), h.entries(union), len(h.restores))
			}
		}

		dscpLearnWaitAll(t, DSCPLearn(cfg, set, dscpLearnIPs("203.0.113.20"), false))
		if _, _, left := removeDSCPObjects(dscpApplied.Load()); len(left) != 0 {
			t.Fatalf("the removal left %v", left)
		}
		if expiry := dscpLearnExpiry(routeSanitizeSetID("a"), "203.0.113.20"); !expiry.IsZero() {
			t.Errorf("removing the objects kept what was learned into them")
		}
	})

	t.Run("nftables", func(t *testing.T) {
		f, _ := dscpLearnNftSetup(t)
		set := a()
		cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, set)
		dscpLearnApply(t, cfg, backendNFTables)
		dscpLearnStart()
		learned := dscpNftLearnedSet(routeSanitizeSetID("a"), false)

		dscpLearnWaitAll(t, DSCPLearn(cfg, set, dscpLearnIPs("203.0.113.10"), false))
		if waits := DSCPLearn(cfg, set, dscpLearnIPs("203.0.113.10"), false); len(waits) != 0 {
			t.Fatal("a live entry was queued again")
		}

		rejected := false
		f.fail = func(script string) (string, error) {
			if !rejected && strings.Contains(script, "flush chain inet b4_dscp postrouting\n") {
				rejected = true
				return "Error: Could not process rule: Operation not supported", errors.New("command [nft -f -] failed: exit status 1 (Error: Could not process rule: Operation not supported)")
			}
			return "", nil
		}
		changed := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 9}, a())
		if err := applyDSCPFor(changed, backendNFTables); err == nil || !rejected {
			t.Fatalf("the update was not rejected into a rebuild: %v", err)
		}
		if got := f.table.sets[learned]; len(got) != 0 {
			t.Fatalf("the rebuilt table kept learned addresses %v", got)
		}
		dscpLearnWaitAll(t, DSCPLearn(changed, set, dscpLearnIPs("203.0.113.10"), false))
		if !slices.Contains(f.table.sets[learned], "203.0.113.10") || len(f.refreshes) != 2 {
			t.Errorf("after the rebuild the address was not written again: %v", f.table.sets[learned])
		}

		ClearDSCPOnly(changed)
		if f.table != nil {
			t.Fatal("the table survived ClearDSCPOnly")
		}
		if waits := DSCPLearn(changed, set, dscpLearnIPs("203.0.113.10"), false); len(waits) != 0 {
			t.Error("the learner queued a write after the table was gone")
		}
	})
}

func TestSetDSCPLearnForgetsDepartedSet(t *testing.T) {
	const addr = "203.0.113.10"
	off := func() *config.SetConfig {
		s := dscpPlanTestSet("a", 31, "10.1.2.0/24")
		s.DSCP.Enabled = false
		return s
	}

	t.Run("iptables, a set leaving and coming back", func(t *testing.T) {
		h, _ := dscpLearnIptSetup(t, backendIPTables)
		a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
		b := dscpPlanTestSet("b", 6, "10.2.0.0/16")
		cfg := dscpIptTestConfig(7, true, nil, a, b)
		dscpLearnApply(t, cfg, backendIPTables)
		dscpLearnStart()
		learned := dscpIptLearnedSet(routeSanitizeSetID("a"), false)
		dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs(addr), false))

		dscpLearnApply(t, dscpIptTestConfig(7, true, nil, off(), b), backendIPTables)
		if _, ok := h.sets[learned]; ok {
			t.Fatalf("the learned ipset %s of a set that left the plan survived", learned)
		}
		dscpLearnApply(t, cfg, backendIPTables)
		dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs(addr), false))
		if !h.sets[learned][addr] {
			t.Errorf("the address learned before the set left was not written again into %s: %v", learned, h.entries(learned))
		}
	})

	t.Run("nftables, a set leaving and coming back", func(t *testing.T) {
		f, _ := dscpLearnNftSetup(t)
		a := dscpPlanTestSet("a", 31, "10.1.2.0/24")
		b := dscpPlanTestSet("b", 6, "10.2.0.0/16")
		global := config.DSCPConfig{Enabled: true, Value: 7}
		cfg := dscpSyncNftConfig(global, a, b)
		dscpLearnApply(t, cfg, backendNFTables)
		dscpLearnStart()
		learned := dscpNftLearnedSet(routeSanitizeSetID("a"), false)
		dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs(addr), false))

		dscpLearnApply(t, dscpSyncNftConfig(global, off(), b), backendNFTables)
		if _, ok := f.table.sets[learned]; ok {
			t.Fatalf("the learned set %s of a set that left the plan survived", learned)
		}
		dscpLearnApply(t, cfg, backendNFTables)
		dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs(addr), false))
		if !slices.Contains(f.table.sets[learned], addr) {
			t.Errorf("the address learned before the set left was not written again into %s: %v", learned, f.table.sets[learned])
		}
	})

	t.Run("iptables, a family losing and regaining its per-set rules", func(t *testing.T) {
		h, _ := dscpLearnIptSetup(t)
		a := dscpPlanTestSet("a", 31, "10.1.2.0/24", "2001:db8:1::/48")
		cfg := dscpIptTestConfig(7, true, nil, a)
		dscpLearnApply(t, cfg, backendIPTables)
		dscpLearnStart()
		learned, union := dscpIptLearnedSet(routeSanitizeSetID("a"), true), dscpIptLearnedUnionSet(true)
		dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs("2001:db8::10"), false))

		h.refuseSets[backendIP6Tables] = true
		dscpLearnApply(t, cfg, backendIPTables)
		if _, ok := h.sets[learned]; ok {
			t.Fatalf("the learned ipset %s survived the IPv6 per-set rules being refused", learned)
		}

		h.refuseSets[backendIP6Tables] = false
		later := time.Now().Add(dscpIptReprobe)
		dscpIptNow = func() time.Time { return later }
		dscpLearnApply(t, cfg, backendIPTables)
		dscpLearnWaitAll(t, DSCPLearn(cfg, a, dscpLearnIPs("2001:db8::10"), false))
		if !h.sets[learned]["2001:db8::10"] || !h.sets[union]["2001:db8::10"] {
			t.Errorf("after the IPv6 per-set rules came back the address was not written again: %v, union %v", h.entries(learned), h.entries(union))
		}
	})
}

func TestSetDSCPPreResolveAfterReset(t *testing.T) {
	dscpLearnIptSetup(t, backendIPTables)
	a := dscpPreDomainSet("a", 31, "a1.example")
	cfg := dscpIptTestConfig(7, true, nil, a)
	dscpLearnApply(t, cfg, backendIPTables)
	dscpLearnManualQueue(t, dscpLearnQueueSize)
	dscpPreResolveStart()
	l := dscpPreStub(t)

	dscpPreTrigger()
	clearDSCPFor(cfg, backendIPTables)
	dscpLearnApply(t, cfg, backendIPTables)
	dscpPreTrigger()
	if n := l.count("a1.example"); n != 2 {
		t.Errorf("after a teardown emptied the learned sets the set was looked up %d times, want 2", n)
	}
}

func TestSetDSCPLearnKeepsEntriesThroughARuleRestore(t *testing.T) {
	const addr = "203.0.113.10"

	t.Run("iptables", func(t *testing.T) {
		h, _ := dscpLearnIptSetup(t, backendIPTables)
		b := dscpPlanTestSet("b", 31, "10.1.2.0/24")
		c := dscpPlanTestSet("c", 6, "10.2.0.0/16")
		cfg := dscpIptTestConfig(7, true, nil, b, c)
		dscpLearnApply(t, cfg, backendIPTables)
		dscpLearnStart()
		dscpLearnWaitAll(t, DSCPLearn(cfg, b, dscpLearnIPs(addr), false))
		dscpLearnWaitAll(t, DSCPLearn(cfg, c, dscpLearnIPs(addr), false))

		delete(h.mangle.chains[backendIPTables], dscpChainName)
		h.mangle.chains[backendIPTables]["POSTROUTING"] = nil
		rulesMu.Lock()
		restored := ensureDSCPLocked(cfg, false)
		rulesMu.Unlock()
		if st := dscpApplied.Load(); !restored || st == nil || !dscpIptShapeOf(st.ipt.chains[backendIPTables]).guard {
			t.Fatal("the monitor did not put the per-set rules back after a firewall reload")
		}
		if waits := DSCPLearn(cfg, b, dscpLearnIPs(addr), true); len(waits) != 0 {
			t.Fatal("a TLS name right after the restore queued a write over an entry the ipsets still hold for an hour")
		}
		for _, name := range []string{dscpIptLearnedSet(routeSanitizeSetID("b"), false), dscpIptLearnedSet(routeSanitizeSetID("c"), false), dscpIptLearnedUnionSet(false)} {
			if got := h.timeouts[name][addr]; got != 3600 || !h.sets[name][addr] {
				t.Errorf("%s holds %s with timeout %d (present %v), want the DNS entry's 3600", name, addr, got, h.sets[name][addr])
			}
		}
	})

	t.Run("nftables", func(t *testing.T) {
		f, _ := dscpLearnNftSetup(t)
		b := dscpPlanTestSet("b", 31, "10.1.2.0/24")
		cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, b)
		dscpLearnApply(t, cfg, backendNFTables)
		dscpLearnStart()
		dscpLearnWaitAll(t, DSCPLearn(cfg, b, dscpLearnIPs(addr), false))

		rules := f.table.chains[dscpNftChain]
		f.table.chains[dscpNftChain] = rules[:len(rules)-1]
		rulesMu.Lock()
		restored := ensureDSCPLocked(cfg, false)
		rulesMu.Unlock()
		if !restored || len(f.table.chains[dscpNftChain]) != len(rules) {
			t.Fatal("the monitor did not put the missing rule back")
		}
		if waits := DSCPLearn(cfg, b, dscpLearnIPs(addr), true); len(waits) != 0 {
			t.Fatal("a TLS name right after the restore queued a write over an entry the set still holds for an hour")
		}
		if got := f.timeouts[dscpNftLearnedSet(routeSanitizeSetID("b"), false)][addr]; got != "3600s" {
			t.Errorf("the learned entry was rewritten with timeout %q, want the DNS entry's 3600s", got)
		}
	})
}

func TestSetDSCPLearnIPVersionFilter(t *testing.T) {
	only4 := func() *config.SetConfig {
		s := dscpPlanTestSet("only4", 31, "10.1.2.0/24")
		s.Targets.IPVersion = "4"
		return s
	}
	only6 := func() *config.SetConfig {
		s := dscpPlanTestSet("only6", 6, "2001:db8:6::/48")
		s.Targets.IPVersion = "6"
		return s
	}
	both := func() *config.SetConfig { return dscpPlanTestSet("both", 25, "10.2.0.0/16") }
	mixed := dscpLearnIPs("203.0.113.10", "2001:db8::10", "::ffff:203.0.113.11", "0.0.0.0")

	t.Run("iptables", func(t *testing.T) {
		h, _ := dscpLearnIptSetup(t)
		h.probeErr[backendIP6Tables] = errors.New("xt_set is not loaded")
		s4, s6, sb := only4(), only6(), both()
		cfg := dscpIptTestConfig(7, true, nil, s4, s6, sb)
		dscpLearnApply(t, cfg, backendIPTables)
		dscpLearnStart()

		dscpLearnWaitAll(t, DSCPLearn(cfg, s4, mixed, false))
		if got, want := h.restores[len(h.restores)-1], strings.Join([]string{
			"add " + dscpIptLearnedSet(routeSanitizeSetID("only4"), false) + " 203.0.113.10 timeout 3600",
			"add " + dscpIptLearnedUnionSet(false) + " 203.0.113.10 timeout 3600",
			"add " + dscpIptLearnedSet(routeSanitizeSetID("only4"), false) + " 203.0.113.11 timeout 3600",
			"add " + dscpIptLearnedUnionSet(false) + " 203.0.113.11 timeout 3600",
		}, "\n"); got != want {
			t.Errorf("an IPv4-only set wrote %q, want %q", got, want)
		}
		if waits := DSCPLearn(cfg, s6, mixed, false); len(waits) != 0 {
			t.Errorf("an IPv6-only set learned while ip6tables has no per-set rules")
		}
		before := len(h.restores)
		dscpLearnWaitAll(t, DSCPLearn(cfg, sb, mixed, false))
		if len(h.restores) != before+1 || strings.Contains(h.restores[before], "_v6") {
			t.Errorf("IPv6 addresses were written for a family without per-set rules: %q", h.restores[before:])
		}
	})

	t.Run("nftables", func(t *testing.T) {
		f, _ := dscpLearnNftSetup(t)
		s4, s6 := only4(), only6()
		cfg := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, s4, s6)
		dscpLearnApply(t, cfg, backendNFTables)
		dscpLearnStart()

		dscpLearnWaitAll(t, DSCPLearn(cfg, s4, mixed, false))
		dscpLearnWaitAll(t, DSCPLearn(cfg, s6, mixed, false))
		if got := f.table.sets[dscpNftLearnedSet(routeSanitizeSetID("only4"), false)]; !slices.Equal(got, []string{"203.0.113.10", "203.0.113.11"}) {
			t.Errorf("the IPv4-only set learned %v", got)
		}
		if got := f.table.sets[dscpNftLearnedSet(routeSanitizeSetID("only4"), true)]; len(got) != 0 {
			t.Errorf("the IPv4-only set learned IPv6 addresses %v", got)
		}
		if got := f.table.sets[dscpNftLearnedSet(routeSanitizeSetID("only6"), true)]; !slices.Equal(got, []string{"2001:db8::10"}) {
			t.Errorf("the IPv6-only set learned %v", got)
		}
		if got := f.table.sets[dscpNftLearnedSet(routeSanitizeSetID("only6"), false)]; len(got) != 0 {
			t.Errorf("the IPv6-only set learned IPv4 addresses %v", got)
		}
	})
}

type dscpPreLookups struct {
	mu    sync.Mutex
	asked []string
	sets  map[string]int
	block map[string]chan struct{}
	began chan string
}

func dscpPreStub(t *testing.T) *dscpPreLookups {
	t.Helper()
	l := &dscpPreLookups{sets: map[string]int{}, block: map[string]chan struct{}{}, began: make(chan string, 16)}
	dscpPreLookup = func(ctx context.Context, _ *config.Config, set *config.SetConfig, host string) []net.IP {
		l.mu.Lock()
		l.asked = append(l.asked, host)
		l.sets[set.Id]++
		n := len(l.asked)
		gate := l.block[host]
		l.mu.Unlock()
		if gate != nil {
			l.began <- host
			select {
			case <-gate:
			case <-ctx.Done():
				return nil
			}
		}
		return []net.IP{net.IPv4(198, 18, byte(n/256), byte(n%256))}
	}
	return l
}

func (l *dscpPreLookups) count(host string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, h := range l.asked {
		if h == host {
			n++
		}
	}
	return n
}

func (l *dscpPreLookups) runs() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]int{}
	for k, v := range l.sets {
		out[k] = v
	}
	return out
}

func dscpPreTrigger() {
	dscpPreResolve(dscpApplied.Load().plan, true)
	dscpPreWG.Wait()
}

func dscpPreDomainSet(id string, value int, domains ...string) *config.SetConfig {
	set := dscpPlanTestSet(id, value, "10."+strconv.Itoa(value)+".0.0/16")
	set.Targets.SNIDomains = domains
	set.Targets.DomainsToMatch = domains
	return set
}

func TestSetDSCPPreResolveCap256(t *testing.T) {
	dscpLearnIptSetup(t, backendIPTables)
	var domains []string
	for i := range 300 {
		domains = append(domains, fmt.Sprintf("d%03d.example", i))
	}
	set := dscpPreDomainSet("big", 31, domains...)
	cfg := dscpIptTestConfig(7, true, nil, set)
	dscpLearnApply(t, cfg, backendIPTables)
	queue := dscpLearnManualQueue(t, dscpLearnQueueSize)
	dscpPreResolveStart()
	l := dscpPreStub(t)

	dscpPreTrigger()
	queued := 0
	for len(queue) > 0 {
		batch := <-queue
		queued += len(batch.addrs)
		dscpLearnSettle(batch)
	}
	if queued != dscpPreResolveCap {
		t.Errorf("the learner got %d pre-resolved addresses, want %d", queued, dscpPreResolveCap)
	}
	if len(l.asked) != dscpPreResolveCap || !slices.Equal(l.asked, domains[:dscpPreResolveCap]) {
		t.Fatalf("pre-resolve looked up %d names, want the first %d in list order", len(l.asked), dscpPreResolveCap)
	}
}

func TestSetDSCPPreResolveKeepsEntriesAlive(t *testing.T) {
	h, clock := dscpLearnIptSetup(t, backendIPTables)
	plain := dscpPlanTestSet("plain", 31, "10.1.2.0/24")
	short := dscpLearnRoutedSet("short", 6, 300, "10.2.0.0/16")
	cfg := dscpIptTestConfig(7, true, nil, plain, short)
	dscpLearnApply(t, cfg, backendIPTables)
	dscpLearnStart()
	learned := func(set *config.SetConfig) string { return dscpIptLearnedSet(routeSanitizeSetID(set.Id), false) }
	const addr = "203.0.113.30"

	dscpLearnWaitAll(t, dscpLearnClaim(plain.Id, dscpLearnIPs(addr), dscpLearnFromPreResolve))
	clock.advance(dscpPreResolveInterval(dscpPlanSet{dnsTTL: time.Hour}))
	if waits := DSCPLearn(cfg, plain, dscpLearnIPs(addr), false); len(waits) != 0 {
		t.Fatal("a DNS answer rewrote an entry with half its life left")
	}
	before := len(h.restores)
	dscpLearnWaitAll(t, dscpLearnClaim(plain.Id, dscpLearnIPs(addr), dscpLearnFromPreResolve))
	if len(h.restores) != before+1 || h.timeouts[learned(plain)][addr] != 3600 {
		t.Fatalf("the next pre-resolve left the entry to run out: %d writes, timeout %d", len(h.restores)-before, h.timeouts[learned(plain)][addr])
	}
	if got, want := dscpLearnExpiry(routeSanitizeSetID(plain.Id), addr), clock.Now().Add(time.Hour); !got.Equal(want) {
		t.Errorf("the pre-resolved entry lives until %s, want %s", got, want)
	}

	dscpLearnWaitAll(t, DSCPLearn(cfg, short, dscpLearnIPs(addr), true))
	clock.advance(100 * time.Second)
	if waits := dscpLearnClaim(short.Id, dscpLearnIPs(addr), dscpLearnFromPreResolve); len(waits) != 0 {
		t.Fatal("a pre-resolve with a 300 s TTL cut short a TLS entry with 500 s left")
	}
	clock.advance(250 * time.Second)
	dscpLearnWaitAll(t, dscpLearnClaim(short.Id, dscpLearnIPs(addr), dscpLearnFromPreResolve))
	if got := h.timeouts[learned(short)][addr]; got != 300 {
		t.Errorf("the pre-resolve wrote timeout %d, want 300", got)
	}
	if got := dscpLearnStats(); got["plain"].preResolve != 2 || got["short"].preResolve != 1 || got["short"].tls != 1 {
		t.Errorf("learned writes by set and source = %+v", got)
	}
}

func TestSetDSCPPreResolveSingleFlight(t *testing.T) {
	dscpLearnIptSetup(t, backendIPTables)
	a := dscpPreDomainSet("a", 31, "a1.example", "a2.example")
	b := dscpPreDomainSet("b", 6, "b1.example")
	cfg := dscpIptTestConfig(7, true, nil, a, b)
	dscpLearnApply(t, cfg, backendIPTables)
	dscpLearnManualQueue(t, dscpLearnQueueSize)
	dscpPreResolveStart()
	l := dscpPreStub(t)
	release := make(chan struct{})
	l.block["a1.example"] = release

	dscpPreResolve(dscpApplied.Load().plan, false)
	select {
	case <-l.began:
	case <-time.After(5 * time.Second):
		t.Fatal("the pre-resolve of set a never started")
	}
	dscpPreResolve(dscpApplied.Load().plan, true)
	grown := dscpPreDomainSet("a", 31, "a1.example", "a2.example", "a3.example")
	dscpLearnApply(t, dscpIptTestConfig(7, true, nil, grown, b), backendIPTables)
	dscpPreResolve(dscpApplied.Load().plan, false)
	if n := l.count("a1.example"); n != 1 {
		t.Errorf("set a was looked up %d times while its first run was still going", n)
	}
	close(release)
	dscpPreWG.Wait()
	if l.count("a1.example") != 1 || l.count("a2.example") != 1 || l.count("b1.example") != 1 || l.count("a3.example") != 0 {
		t.Fatalf("lookups %v, want one run per set", l.asked)
	}

	dscpPreTrigger()
	if l.count("a1.example") != 2 || l.count("a3.example") != 1 || l.count("b1.example") != 1 {
		t.Errorf("the changed domains of set a were not looked up once its run ended: %v", l.asked)
	}
}

func TestSetDSCPPreResolveStopCancelsALookupInFlight(t *testing.T) {
	dscpLearnIptSetup(t, backendIPTables)
	set := dscpPreDomainSet("a", 31, "a1.example", "a2.example")
	dscpLearnApply(t, dscpIptTestConfig(7, true, nil, set), backendIPTables)
	dscpLearnManualQueue(t, dscpLearnQueueSize)
	dscpPreResolveStart()
	l := dscpPreStub(t)
	gate := make(chan struct{})
	l.block["a1.example"] = gate
	t.Cleanup(func() { close(gate) })

	dscpPreResolve(dscpApplied.Load().plan, false)
	select {
	case <-l.began:
	case <-time.After(5 * time.Second):
		t.Fatal("the pre-resolve of set a never started")
	}
	stopped := make(chan struct{})
	go func() {
		dscpPreResolveStop()
		close(stopped)
	}()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("stopping the pre-resolver waited for a lookup it should have cancelled")
	}
	if n := l.count("a2.example"); n != 0 {
		t.Errorf("the run went on to the next domain after the stop: %v", l.asked)
	}
}

func TestSetDSCPPreResolveRunsOnEveryTickOfAShortTTL(t *testing.T) {
	_, clock := dscpLearnIptSetup(t, backendIPTables)
	short := dscpPreDomainSet("short", 6, "short.example")
	short.Routing.Enabled, short.Routing.Mode, short.Routing.EgressInterface, short.Routing.IPTTLSeconds = true, config.RoutingModeInterface, "wg0", 300
	dscpLearnApply(t, dscpIptTestConfig(7, true, nil, short), backendIPTables)
	dscpLearnManualQueue(t, dscpLearnQueueSize)
	dscpPreResolveStart()
	l := dscpPreStub(t)

	dscpPreTrigger()
	for i, latency := range []time.Duration{-3 * time.Millisecond, 0, -time.Millisecond, 2 * time.Millisecond, -2 * time.Millisecond} {
		clock.advance(dscpSyncTick + latency)
		dscpPreTrigger()
		if got := l.runs()["short"]; got != i+2 {
			t.Fatalf("tick %d, %v off the previous pass: %d runs, want %d; a skipped tick leaves the 300 s entries expired for five minutes", i+1, latency, got, i+2)
		}
	}
}

func TestSetDSCPPreResolveInterval(t *testing.T) {
	_, clock := dscpLearnIptSetup(t, backendIPTables)
	plain := dscpPreDomainSet("plain", 31, "plain.example")
	short := dscpPreDomainSet("short", 6, "short.example")
	short.Routing.Enabled, short.Routing.Mode, short.Routing.EgressInterface, short.Routing.IPTTLSeconds = true, config.RoutingModeInterface, "wg0", 300
	long := dscpPreDomainSet("long", 25, "long.example")
	long.Routing.Enabled, long.Routing.Mode, long.Routing.EgressInterface, long.Routing.IPTTLSeconds = true, config.RoutingModeInterface, "wg0", 7200
	cfg := dscpIptTestConfig(7, true, nil, plain, short, long)
	dscpLearnApply(t, cfg, backendIPTables)
	dscpLearnManualQueue(t, dscpLearnQueueSize)
	dscpPreResolveStart()
	l := dscpPreStub(t)
	expect := func(stage string, want map[string]int) {
		t.Helper()
		if got := l.runs(); !maps.Equal(got, want) {
			t.Errorf("%s: runs %v, want %v", stage, got, want)
		}
	}

	dscpPreTrigger()
	expect("sets entering the plan", map[string]int{"plain": 1, "short": 1, "long": 1})
	clock.advance(dscpSyncTick/2 - time.Second)
	dscpPreTrigger()
	expect("well before the next tick", map[string]int{"plain": 1, "short": 1, "long": 1})
	clock.advance(dscpSyncTick / 2)
	dscpPreTrigger()
	expect("a tick a second before five minutes, the floor for a 300 s TTL", map[string]int{"plain": 1, "short": 2, "long": 1})
	clock.advance(time.Second)
	dscpPreTrigger()
	expect("five minutes", map[string]int{"plain": 1, "short": 2, "long": 1})
	clock.advance(25 * time.Minute)
	dscpPreTrigger()
	expect("half of 3600 s", map[string]int{"plain": 2, "short": 3, "long": 1})
	clock.advance(30 * time.Minute)
	dscpPreTrigger()
	expect("half of 7200 s", map[string]int{"plain": 3, "short": 4, "long": 2})

	clock.advance(time.Second)
	renamed := dscpPreDomainSet("plain", 31, "plain.example", "plain2.example")
	dscpLearnApply(t, dscpIptTestConfig(7, true, nil, renamed, short, long), backendIPTables)
	dscpPreTrigger()
	expect("a domain change", map[string]int{"plain": 5, "short": 4, "long": 2})

	dscpLearnApply(t, dscpIptTestConfig(7, true, nil, short, long), backendIPTables)
	dscpPreTrigger()
	dscpLearnApply(t, dscpIptTestConfig(7, true, nil, renamed, short, long), backendIPTables)
	dscpPreTrigger()
	expect("a set leaving and coming back", map[string]int{"plain": 7, "short": 4, "long": 2})

	rulesMu.Lock()
	closed := dscpSyncClosed.Load()
	dscpSyncClosed.Store(false)
	rulesMu.Unlock()
	t.Cleanup(func() { dscpSyncClosed.Store(closed) })
	clock.advance(time.Hour)
	dscpSyncPass(dscpApplied.Load().cfg, true, nil)
	dscpPreWG.Wait()
	expect("a periodic pass of the sync worker", map[string]int{"plain": 9, "short": 5, "long": 3})
}
