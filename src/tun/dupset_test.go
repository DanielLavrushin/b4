package tun

import (
	"errors"
	"os/exec"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type fakeCapture struct {
	calls       []string
	fills       int
	lastMaxElem int
	appends     int
	fail        func(cmd string) bool
	onAppend    func(n int)
}

func stubCapture(t *testing.T, ipsetInstalled bool, fillErr error) *fakeCapture {
	t.Helper()
	f := &fakeCapture{}
	origRun, origLook, origFill := run, lookPath, fillDupSet
	t.Cleanup(func() { run, lookPath, fillDupSet = origRun, origLook, origFill })
	run = func(args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		f.calls = append(f.calls, cmd)
		if strings.HasPrefix(cmd, "ip -4 -o addr show") {
			return addrShowSample, nil
		}
		if strings.HasPrefix(cmd, "iptables -t mangle -A "+tunCaptureChain+" ") {
			f.appends++
			if f.onAppend != nil {
				f.onAppend(f.appends)
			}
		}
		if f.fail != nil && f.fail(cmd) {
			return "", errors.New("fake failure")
		}
		return "", nil
	}
	lookPath = func(file string) (string, error) {
		if ipsetInstalled {
			return "/usr/sbin/" + file, nil
		}
		return "", exec.ErrNotFound
	}
	fillDupSet = func(entries []string, maxElem int) error {
		f.fills++
		f.lastMaxElem = maxElem
		return fillErr
	}
	return f
}

func (f *fakeCapture) appended() []string {
	var out []string
	for _, c := range f.calls {
		if rest, ok := strings.CutPrefix(c, "iptables -t mangle -A "+tunCaptureChain+" "); ok {
			out = append(out, rest)
		}
	}
	return out
}

func (f *fakeCapture) first(cmd string) int {
	for i, c := range f.calls {
		if c == cmd {
			return i
		}
	}
	return -1
}

func (f *fakeCapture) count(match func(string) bool) int {
	n := 0
	for _, c := range f.calls {
		if match(c) {
			n++
		}
	}
	return n
}

func (f *fakeCapture) dupRules() (viaSet, perAddress int) {
	for _, s := range f.appended() {
		switch {
		case strings.Contains(s, "--match-set "+tunDupSet+" dst"):
			viaSet++
		case strings.HasPrefix(s, "-p tcp -d "):
			perAddress++
		}
	}
	return viaSet, perAddress
}

func isDupProbe(cmd string) bool {
	return strings.Contains(cmd, tunProbeChain) && strings.Contains(cmd, "--match-set "+tunDupSet)
}

func dupRebuildManager() *routeManager {
	r := dupCaptureManager(false, true)
	r.tunName = "b4tun0"
	return r
}

func TestRebuildMatchesDuplicationAddressesThroughTheSet(t *testing.T) {
	f := stubCapture(t, true, nil)
	r := dupRebuildManager()
	r.rebuildCaptureChain()

	if viaSet, perAddress := f.dupRules(); viaSet != 1 || perAddress != 0 {
		t.Fatalf("want one set rule and no per-address rule, got %d and %d in %q", viaSet, perAddress, f.appended())
	}
	if f.fills != 1 || f.lastMaxElem != dupSetMinMaxElem {
		t.Fatalf("want one fill with maxelem %d, got %d fill(s) with %d", dupSetMinMaxElem, f.fills, f.lastMaxElem)
	}
	flush := f.first("iptables -t mangle -F " + tunCaptureChain)
	destroy := f.first("ipset destroy " + tunDupSet)
	if flush < 0 || destroy < flush {
		t.Fatalf("the set may be destroyed only once the chain that references it is flushed (flush %d, destroy %d)", flush, destroy)
	}
	if r.captureInstalled != len(f.appended()) || r.captureMissing != 0 {
		t.Fatalf("installed %d missing %d, want %d and 0", r.captureInstalled, r.captureMissing, len(f.appended()))
	}

	f.calls = nil
	r.rebuildCaptureChain()
	if n := f.count(isDupProbe); n != 0 {
		t.Fatalf("a successful xt_set probe must not run again on the next rebuild, ran %d command(s)", n)
	}
	if viaSet, _ := f.dupRules(); viaSet != 1 {
		t.Fatalf("the next rebuild must keep the set rule, got %q", f.appended())
	}
}

func TestRebuildFallsBackToARuleEachWhenTheSetCannotBeFilled(t *testing.T) {
	f := stubCapture(t, true, errors.New("ipset restore failed"))
	r := dupRebuildManager()
	r.rebuildCaptureChain()

	if viaSet, perAddress := f.dupRules(); viaSet != 0 || perAddress != len(r.dupIPs) {
		t.Fatalf("want %d per-address rules and no set rule, got %d and %d", len(r.dupIPs), perAddress, viaSet)
	}
	if n := f.count(func(c string) bool { return c == "ipset destroy "+tunDupSet }); n != 2 {
		t.Fatalf("a half-filled set must be destroyed again, got %d destroy call(s)", n)
	}
}

func TestRebuildRemembersAFailedSetMatchProbe(t *testing.T) {
	f := stubCapture(t, true, nil)
	f.fail = isDupProbe
	r := dupRebuildManager()
	r.rebuildCaptureChain()

	if viaSet, perAddress := f.dupRules(); viaSet != 0 || perAddress != len(r.dupIPs) {
		t.Fatalf("without xt_set want %d per-address rules, got %d set and %d per-address", len(r.dupIPs), viaSet, perAddress)
	}

	f.calls = nil
	r.rebuildCaptureChain()
	if f.fills != 1 {
		t.Fatalf("after a failed probe the set must not be filled again, filled %d time(s)", f.fills)
	}
	if n := f.count(isDupProbe); n != 0 {
		t.Fatalf("a failed probe must not repeat on every rebuild, ran %d command(s)", n)
	}
	if _, perAddress := f.dupRules(); perAddress != len(r.dupIPs) {
		t.Fatalf("the next rebuild must keep the per-address rules, got %q", f.appended())
	}
}

func TestRebuildWithoutTheIpsetCommandKeepsARuleEach(t *testing.T) {
	f := stubCapture(t, false, nil)
	r := dupRebuildManager()
	r.rebuildCaptureChain()

	if f.fills != 0 {
		t.Fatalf("no ipset command, yet the set was filled %d time(s)", f.fills)
	}
	if n := f.count(func(c string) bool { return strings.HasPrefix(c, "ipset ") }); n != 0 {
		t.Fatalf("no ipset command, yet %d ipset call(s) ran", n)
	}
	if viaSet, perAddress := f.dupRules(); viaSet != 0 || perAddress != len(r.dupIPs) {
		t.Fatalf("want %d per-address rules, got %d set and %d per-address", len(r.dupIPs), viaSet, perAddress)
	}
}

func TestRebuildWithoutDuplicationAddressesRemovesALeftoverSet(t *testing.T) {
	f := stubCapture(t, true, nil)
	r := dupRebuildManager()
	r.dupIPs = nil
	r.rebuildCaptureChain()

	if f.fills != 0 {
		t.Fatalf("no duplication address, yet the set was filled %d time(s)", f.fills)
	}
	if viaSet, perAddress := f.dupRules(); viaSet != 0 || perAddress != 0 {
		t.Fatalf("no duplication address, yet %d set and %d per-address rule(s)", viaSet, perAddress)
	}
	if f.first("ipset destroy "+tunDupSet) < 0 {
		t.Fatalf("a set left by an earlier list must be removed")
	}
}

func TestDupSetGrowsPastTheDefaultLimit(t *testing.T) {
	if got := dupSetMaxElem(10); got != dupSetMinMaxElem {
		t.Fatalf("dupSetMaxElem(10) = %d, want %d", got, dupSetMinMaxElem)
	}
	if got := dupSetMaxElem(40000); got != 80000 {
		t.Fatalf("dupSetMaxElem(40000) = %d, want 80000", got)
	}
}

func TestRebuildEndsWhenTheEngineQuitsMidway(t *testing.T) {
	f := stubCapture(t, false, nil)
	quit := make(chan struct{})
	r := dupRebuildManager()
	r.quit = quit
	f.onAppend = func(n int) {
		if n == 3 {
			close(quit)
		}
	}
	r.rebuildCaptureChain()

	if f.appends != 3 {
		t.Fatalf("want the rebuild to stop right after the engine quit, got %d append(s)", f.appends)
	}
	if r.captureInstalled != 3 {
		t.Fatalf("captureInstalled = %d, want the 3 rules that did go in", r.captureInstalled)
	}
}

func TestNothingRunsOnceTheEngineQuits(t *testing.T) {
	f := stubCapture(t, true, nil)
	quit := make(chan struct{})
	close(quit)
	r := dupRebuildManager()
	r.quit = quit
	r.resolvedCapture = "ports"
	r.captureInstalled = 50

	r.reconcile()
	r.rebuildCaptureChain()
	if len(f.calls) != 0 {
		t.Fatalf("after quit nothing may touch the firewall, ran %q", f.calls)
	}
}

func TestTeardownDestroysTheDupSetAfterTheChain(t *testing.T) {
	f := stubCapture(t, true, nil)
	f.fail = func(c string) bool { return strings.Contains(c, " -D ") || strings.HasPrefix(c, "ip rule del") }
	r := dupRebuildManager()
	r.captureRulesAdded = true
	r.captureTable = 96
	r.teardownPortCapture()

	del := f.first("iptables -t mangle -X " + tunCaptureChain)
	destroy := f.first("ipset destroy " + tunDupSet)
	if del < 0 || destroy < del {
		t.Fatalf("the set must be destroyed after %s is deleted (delete %d, destroy %d)", tunCaptureChain, del, destroy)
	}
}

func TestClearStaleArtifactsDestroysTheDupSetAfterTheChain(t *testing.T) {
	f := stubCapture(t, true, nil)
	f.fail = func(c string) bool { return strings.Contains(c, " -D ") || strings.HasPrefix(c, "ip rule del") }
	ClearStaleArtifacts(&config.Config{})

	del := f.first("iptables -t mangle -X " + tunCaptureChain)
	destroy := f.first("ipset destroy " + tunDupSet)
	if del < 0 || destroy < del {
		t.Fatalf("a set left by a TUN run must go after %s is deleted (delete %d, destroy %d)", tunCaptureChain, del, destroy)
	}
}
