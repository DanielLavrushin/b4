package tun

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

const (
	stockRules = "0:\tfrom all lookup local\n32766:\tfrom all lookup main\n32767:\tfrom all lookup default\n"
	rosRules   = "200:\tfrom all lookup local\n32766:\tfrom all lookup main\n32767:\tfrom all lookup default\n"
)

func TestTheLocalGuardIsNeededOnlyWhenTheLocalTableComesAfterTheCaptureRule(t *testing.T) {
	cases := []struct {
		name string
		show string
		want bool
	}{
		{"stock rules", stockRules, false},
		{"RouterOS container rules", rosRules, true},
		{"RouterOS rules before the image fixes main", "200:\tfrom all lookup local\n2147483646:\tfrom all lookup main\n2147483647:\tfrom all lookup default\n", true},
		{"local table at the capture priority", "10:\tfrom all lookup local\n32766:\tfrom all lookup main\n", true},
		{"local table just above the capture rule", "9:\tfrom all lookup local\n32766:\tfrom all lookup main\n", false},
		{"local table by number", "0:\tfrom all lookup 255\n32766:\tfrom all lookup main\n", false},
		{"no rule for the local table", "32766:\tfrom all lookup main\n", true},
		{"only a narrowed local rule comes first", "0:\tfrom 10.0.0.0/8 lookup local\n200:\tfrom all lookup local\n", true},
		{"only a marked local rule comes first", "9:\tfrom all fwmark 0x40000000/0x40000000 lookup local\n200:\tfrom all lookup local\n", true},
		{"stock rules next to b4's own", "0:\tfrom all lookup local\n9:\tfrom all fwmark 0x40000000/0x40000000 lookup local\n10:\tfrom all fwmark 0x40000000/0x40000000 lookup 96\n", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := localGuardNeeded(c.show, 10); got != c.want {
				t.Errorf("localGuardNeeded = %v, want %v for\n%s", got, c.want, c.show)
			}
		})
	}
}

type ruleRecorder struct {
	show  string
	calls []string
	fail  func(cmd string) bool
}

func stubRules(t *testing.T, show string) *ruleRecorder {
	t.Helper()
	f := &ruleRecorder{show: show}
	orig := run
	t.Cleanup(func() { run = orig })
	run = func(args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		f.calls = append(f.calls, cmd)
		if f.fail != nil && f.fail(cmd) {
			return "", errors.New("fake failure")
		}
		switch {
		case cmd == "ip rule show":
			return f.show, nil
		case strings.HasPrefix(cmd, "ip rule del "), strings.Contains(cmd, " -D "):
			return "", errors.New("No such file or directory")
		}
		return "", nil
	}
	return f
}

func (f *ruleRecorder) index(prefix string) int {
	return slices.IndexFunc(f.calls, func(c string) bool { return strings.HasPrefix(c, prefix) })
}

const guardAdd = "ip rule add fwmark 0x40000000/0x40000000 lookup local priority 9"

func TestTheCaptureSetupAddsTheLocalGuardBeforeTheSteerRule(t *testing.T) {
	f := stubRules(t, rosRules)
	r := &routeManager{tunName: "b4tun0", captureTable: 96}
	if err := r.setupCaptureTable(); err != nil {
		t.Fatal(err)
	}
	guard := f.index(guardAdd)
	steer := f.index("ip rule add fwmark 0x40000000/0x40000000 lookup 96 priority 10")
	if guard < 0 || steer < 0 || guard > steer {
		t.Fatalf("the rule that looks up the local table must go in before the capture rule, so no steered packet for this device ever reaches b4tun0; calls:\n%s", strings.Join(f.calls, "\n"))
	}
	if !r.localGuardAdded {
		t.Error("the guard was added but not recorded, so teardown would leave it")
	}
}

func TestTheCaptureSetupLeavesStockRulesAlone(t *testing.T) {
	f := stubRules(t, stockRules)
	r := &routeManager{tunName: "b4tun0", captureTable: 96}
	if err := r.setupCaptureTable(); err != nil {
		t.Fatal(err)
	}
	if i := f.index("ip rule add fwmark 0x40000000/0x40000000 lookup local"); i >= 0 {
		t.Errorf("the local table already comes first, yet b4 ran %q", f.calls[i])
	}
	if r.localGuardAdded {
		t.Error("no guard was needed, yet one is recorded")
	}
}

func reconcilingManager(guardAdded bool) *routeManager {
	return &routeManager{tunName: "b4tun0", resolvedCapture: "ports", captureTable: 96, capturePrio: 10, localGuardAdded: guardAdded}
}

func TestReconcileRestoresTheLocalGuardBeforeTheSteerRule(t *testing.T) {
	f := stubRules(t, rosRules)
	r := reconcilingManager(true)
	r.ensurePortCapture()
	guard := f.index(guardAdd)
	steer := f.index("ip rule add fwmark 0x40000000/0x40000000 lookup 96 priority 10")
	if guard < 0 {
		t.Fatalf("a guard removed outside b4 was not restored by the reconcile; calls:\n%s", strings.Join(f.calls, "\n"))
	}
	if steer >= 0 && steer < guard {
		t.Errorf("the reconcile restored the capture rule before the guard, so packets for this device were steered into b4tun0 in between; calls:\n%s", strings.Join(f.calls, "\n"))
	}

	f.calls = nil
	f.show = "9:\tfrom all fwmark 0x40000000/0x40000000 lookup local\n10:\tfrom all fwmark 0x40000000/0x40000000 lookup 96\n" + rosRules
	r.ensurePortCapture()
	if i := f.index("ip rule add"); i >= 0 {
		t.Errorf("both rules are in place, yet the reconcile ran %q", f.calls[i])
	}
}

func TestReconcileLeavesStockRulesAlone(t *testing.T) {
	f := stubRules(t, "0:\tfrom all lookup local\n10:\tfrom all fwmark 0x40000000/0x40000000 lookup 96\n32766:\tfrom all lookup main\n")
	r := reconcilingManager(false)
	r.ensurePortCapture()
	if i := f.index("ip rule add"); i >= 0 {
		t.Errorf("the local table already comes first, yet the reconcile ran %q", f.calls[i])
	}
}

func TestAFailedLocalGuardIsRetriedAtEveryReconcile(t *testing.T) {
	f := stubRules(t, rosRules)
	f.fail = func(cmd string) bool {
		return strings.HasPrefix(cmd, "ip rule add fwmark 0x40000000/0x40000000 lookup local")
	}
	r := reconcilingManager(false)
	r.ensurePortCapture()
	r.ensurePortCapture()
	adds := 0
	for _, c := range f.calls {
		if c == guardAdd {
			adds++
		}
	}
	if adds != 2 {
		t.Errorf("the guard add ran %d times over two reconciles, want 2", adds)
	}
	if r.localGuardAdded || !r.localGuardWarned {
		t.Errorf("after failed adds: added=%v warned=%v, want added=false warned=true", r.localGuardAdded, r.localGuardWarned)
	}
}

func TestTeardownRemovesTheLocalGuardByItsMarkOnly(t *testing.T) {
	for _, captureAdded := range []bool{true, false} {
		f := stubRules(t, rosRules)
		r := &routeManager{tunName: "b4tun0", captureTable: 96, capturePrio: 10, captureRulesAdded: captureAdded, localGuardAdded: true}
		r.teardownPortCapture()
		if f.index("ip rule del fwmark 0x40000000/0x40000000 lookup local") < 0 {
			t.Errorf("captureRulesAdded=%v: teardown left the guard; calls: %q", captureAdded, f.calls)
		}
		for _, c := range f.calls {
			if strings.HasPrefix(c, "ip rule del") && strings.Contains(c, "lookup local") && !strings.Contains(c, "fwmark 0x40000000/0x40000000") {
				t.Errorf("captureRulesAdded=%v: %q could delete the system's local rule", captureAdded, c)
			}
		}
		if r.localGuardAdded {
			t.Errorf("captureRulesAdded=%v: the guard is still recorded after teardown", captureAdded)
		}
	}
}

func TestTheSweepRemovesALeftoverLocalGuardAndNeverTheSystemRule(t *testing.T) {
	out := strings.Join([]string{
		"0:\tfrom all lookup local",
		"3:\tfrom all fwmark 0x40000000/0x40000000 lookup local",
		"9:\tfrom all fwmark 0x40000000/0x40000000 lookup local",
		"10:\tfrom all fwmark 0x40000000/0x40000000 lookup 96",
		"50:\tfrom all fwmark 0x40000000/0x40000000 lookup local",
		"9:\tfrom all fwmark 0x1234 lookup local",
		"9:\tfrom 10.0.0.0/8 fwmark 0x40000000/0x40000000 lookup local",
		"200:\tfrom all lookup local",
		"32766:\tfrom all lookup main",
	}, "\n")
	dels, flush := planTunRuleSweep(out, ownedTunTables(0), 0x8000)
	want := strings.Join([]string{
		"ip rule del priority 3 fwmark 0x40000000/0x40000000 lookup local",
		"ip rule del priority 9 fwmark 0x40000000/0x40000000 lookup local",
		"ip rule del priority 10 fwmark 0x40000000/0x40000000 lookup 96",
	}, "\n")
	if got := joinDels(dels); got != want {
		t.Errorf("sweep deletes\n%s\nwant\n%s", got, want)
	}
	if slices.Contains(flush, "local") || slices.Contains(flush, "255") {
		t.Errorf("the sweep would flush the local table: %v", flush)
	}
}

func TestTheCapturePriorityIgnoresB4sOwnRules(t *testing.T) {
	stubRules(t, "0:\tfrom all lookup local\n9:\tfrom all fwmark 0x40000000/0x40000000 lookup local\n9:\tfrom all fwmark 0x40000000/0x40000000 lookup 95\n32766:\tfrom all lookup main\n")
	if got := (&routeManager{}).pickCapturePriority(); got != defaultCapturePrio {
		t.Errorf("rules left by b4 itself moved the capture priority to %d, want %d; every restart would push it lower", got, defaultCapturePrio)
	}
	stubRules(t, "0:\tfrom all lookup local\n7:\tfrom all fwmark 0x10 lookup 120\n32766:\tfrom all lookup main\n")
	if got := (&routeManager{}).pickCapturePriority(); got != 6 {
		t.Errorf("a foreign rule at 7 gave capture priority %d, want 6", got)
	}
}
