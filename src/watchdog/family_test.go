package watchdog

import (
	"context"
	"errors"
	"net/http"
	"net/netip"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

const (
	dualV4 = "203.0.113.9"
	dualV6 = "2001:db8::9"
)

type dialedAddrs struct {
	mu    sync.Mutex
	addrs []string
}

func (d *dialedAddrs) note(a netip.Addr) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if s := a.String(); !slices.Contains(d.addrs, s) {
		d.addrs = append(d.addrs, s)
	}
	return true
}

func (d *dialedAddrs) sorted() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := slices.Clone(d.addrs)
	slices.Sort(out)
	return out
}

type familyCheck struct {
	name string
	run  func(host, network string, isReserved func(netip.Addr) bool) (unusable bool, text string)
}

func familyChecks() []familyCheck {
	const timeout = 3 * time.Second
	return []familyCheck{
		{"domain check", func(host, network string, isReserved func(netip.Addr) bool) (bool, string) {
			res := guardedCheck(context.Background(), "https://"+host+"/", network, markThroughEngine, timeout, newDialGuard(isReserved))
			return res.Unusable, res.Error
		}},
		{"set check", func(host, network string, isReserved func(netip.Addr) bool) (bool, string) {
			res := strictCheck(context.Background(), "https://"+host+"/", strictOptions{Network: network, Timeout: timeout, isReserved: isReserved})
			return res.Status == URLStatusUnusable, res.Error
		}},
		{"MCP probe", func(host, network string, isReserved func(netip.Addr) bool) (bool, string) {
			res, err := probe(context.Background(), host, "https://"+host+"/", ProbeOptions{Network: network, Timeout: timeout}, newDialGuard(isReserved), nil)
			if err != nil {
				return true, err.Error()
			}
			return false, res.Error
		}},
	}
}

func TestEveryCheckDialsOnlyTheFamilyB4Handles(t *testing.T) {
	fakeNames(t, map[string][]string{"dual.test": {dualV4, dualV6}})
	for _, check := range familyChecks() {
		for network, want := range map[string][]string{
			"tcp4": {dualV4},
			"tcp6": {dualV6},
			"":     {dualV6, dualV4},
		} {
			dialed := &dialedAddrs{}
			check.run("dual.test", network, dialed.note)
			if got := dialed.sorted(); !slices.Equal(got, want) {
				t.Errorf("%s with network %q dialed %v, want %v", check.name, network, got, want)
			}
		}
	}
}

func TestANameOutsideTheHandledFamilyIsNoFailedCheck(t *testing.T) {
	fakeNames(t, map[string][]string{"six.test": {dualV6}, "four.test": {dualV4}})
	for _, check := range familyChecks() {
		for _, tc := range []struct{ host, network, says string }{
			{"six.test", "tcp4", "six.test has only IPv6 addresses"},
			{"[" + dualV6 + "]", "tcp4", dualV6 + " has only IPv6 addresses"},
			{"four.test", "tcp6", "four.test has only IPv4 addresses"},
		} {
			unusable, text := check.run(tc.host, tc.network, allowLoopback)
			if !unusable || !strings.Contains(text, tc.says) {
				t.Errorf("%s of %s over %s: unusable=%v %q, want no verdict on the site and %q", check.name, tc.host, tc.network, unusable, text, tc.says)
			}
		}
		if unusable, text := check.run("gone.test", "tcp4", allowLoopback); unusable || !strings.Contains(text, "no such host") {
			t.Errorf("%s of a name that does not exist stays a failed lookup, got unusable=%v %q", check.name, unusable, text)
		}
	}
}

func TestOnlyANameWithAnAddressOfTheOtherFamilyIsUnusable(t *testing.T) {
	fakeNames(t, map[string][]string{
		"flaky.test": {"servfail4", dualV6},
		"slow.test":  {"late6"},
		"sink.test":  {"fd00::1"},
	})
	for _, check := range familyChecks() {
		if unusable, text := check.run("flaky.test", "tcp4", allowLoopback); unusable || strings.Contains(text, "only IPv6") {
			t.Errorf("%s: a failed IPv4 lookup of a name with IPv6 addresses is a failure to heal, got unusable=%v %q", check.name, unusable, text)
		}
		start := time.Now()
		unusable, text := check.run("slow.test", "tcp4", allowLoopback)
		if took := time.Since(start); unusable || took > 2500*time.Millisecond || !strings.Contains(text, "no such host") {
			t.Errorf("%s: a name with no IPv4 address whose IPv6 lookup never answers fails as a lookup within half the timeout, got unusable=%v %q after %v", check.name, unusable, text, took)
		}
		if unusable, text := check.run("sink.test", "tcp4", func(a netip.Addr) bool { return a.IsPrivate() }); !unusable || !strings.Contains(text, "private or local") {
			t.Errorf("%s: a name whose only answers are private addresses of the other family is sinkholed, got unusable=%v %q", check.name, unusable, text)
		}
	}
}

func TestAProbeNamesTheHostARedirectLeadsTo(t *testing.T) {
	var lab *probeLab
	lab = newProbeLab(t, map[string][]string{"example.test": labLoopback, "www.example.test": {dualV6}},
		func(host string, w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, lab.url("www.example.test", "/"), http.StatusMovedPermanently)
		})

	_, err := probe(context.Background(), "example.test", lab.url("example.test", "/"), ProbeOptions{Network: "tcp4", Timeout: 3 * time.Second}, newDialGuard(allowLoopback), nil)

	var unhandled *ErrUnhandledFamily
	if !errors.As(err, &unhandled) || !strings.Contains(err.Error(), "www.example.test has only IPv6 addresses") || !strings.Contains(err.Error(), "redirect from example.test") {
		t.Fatalf("the error names the host the redirect led to and where it came from, got %v", err)
	}
	if strings.Contains(err.Error(), "would not go through b4") {
		t.Errorf("the reason fits both arms of the probe: %v", err)
	}
}

func TestAProbeNamesTheFamilyInItsError(t *testing.T) {
	fakeNames(t, map[string][]string{"six.test": {dualV6}})
	_, err := ProbeHost(context.Background(), "six.test", ProbeOptions{Network: "tcp4", Timeout: 3 * time.Second})
	var unhandled *ErrUnhandledFamily
	if !errors.As(err, &unhandled) || unhandled.Host != "six.test" || unhandled.Family != "IPv6" {
		t.Fatalf("the caller can tell a name b4 does not handle from a failed fetch, got %v", err)
	}
}

func TestChecksFollowTheEngineAndTheIPVersionSettings(t *testing.T) {
	for _, tc := range []struct {
		name       string
		ipv4, ipv6 bool
		tun        bool
		want       string
	}{
		{"IPv6 support off", true, false, false, "tcp4"},
		{"both families on", true, true, false, ""},
		{"IPv4 support off", false, true, false, "tcp6"},
		{"TUN with IPv6 support on", true, true, true, "tcp4"},
		{"TUN with IPv4 support off", false, true, true, "tcp4"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config.SetRunningEngineTUN(tc.tun)
			t.Cleanup(func() { config.SetRunningEngineTUN(false) })
			only := func(h *harness, step string) {
				t.Helper()
				got := h.check.takeNetworks()
				if len(got) == 0 {
					t.Errorf("%s made no check", step)
				}
				for _, network := range got {
					if network != tc.want {
						t.Errorf("%s dialed %q, want %q", step, network, tc.want)
					}
				}
			}
			families := func(c *config.Config) {
				c.Queue.IPv4Enabled, c.Queue.IPv6Enabled = tc.ipv4, tc.ipv6
			}

			set := newHarness(t, watchedSet("yt", ytURL))
			set.editGlobal(families)
			set.check.set(ytURL, failCheck)
			set.tickUntilQueued("yt")
			only(set, "a tick of a watched set")
			set.check.set(ytURL, okCheck)
			set.driver.verdict = coveredVerdict("disorder")
			set.healNow("yt")
			only(set, "the check after a set heal")

			legacy := legacyHarness(t)
			legacy.editGlobal(func(c *config.Config) {
				families(c)
				c.System.Checker.Watchdog.Domains = []string{"youtube.com"}
				c.System.Checker.Watchdog.MaxRetries = 1
			})
			legacy.w.tick()
			only(legacy, "a tick of the global list and the check after its heal")
			legacy.w.healWG.Wait()
			legacy.check.takeNetworks()
			legacy.w.healBatch([]string{"youtube.com"}, nil)
			only(legacy, "the check after a domain heal")
		})
	}
}

func TestANameWithOnlyIPv6StartsNoDomainHeal(t *testing.T) {
	fakeNames(t, map[string][]string{"six.test": {dualV6}})
	h := newHarness(t)
	h.editGlobal(func(c *config.Config) {
		c.Queue.IPv4Enabled, c.Queue.IPv6Enabled = true, false
		c.System.Checker.Watchdog.Domains = []string{"six.test"}
		c.System.Checker.Watchdog.MaxRetries = 1
	})
	h.w.checkLegacy = checkAllConcurrently

	h.w.tick()
	h.w.healWG.Wait()

	h.w.mu.Lock()
	st := *h.w.domainStates["six.test"]
	h.w.mu.Unlock()
	if st.ConsecutiveFailures != 0 || st.CooldownUntil.IsZero() || !strings.Contains(st.LastError, "has only IPv6 addresses") {
		t.Errorf("no strategy reaches a name b4 does not handle, so it cools down with the reason: %+v", st)
	}
	if len(h.driver.starts) != 0 {
		t.Errorf("no heal may start, got %v", h.driver.starts)
	}
}
