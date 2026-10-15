package tables

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type fakeMangle struct {
	chains     map[string]map[string][]string
	rejectDSCP map[string]bool
	calls      []string
}

func newFakeMangle(bins ...string) *fakeMangle {
	f := &fakeMangle{chains: map[string]map[string][]string{}, rejectDSCP: map[string]bool{}}
	for _, bin := range bins {
		f.chains[bin] = map[string][]string{"POSTROUTING": nil}
	}
	return f
}

func fakeMangleCanon(spec []string) string {
	out := append([]string(nil), spec...)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == "--set-dscp" {
			if n, err := strconv.ParseUint(out[i+1], 0, 8); err == nil {
				out[i+1] = fmt.Sprintf("0x%02x", n)
			}
		}
	}
	return strings.Join(out, " ")
}

func fakeMangleTarget(rule string) string {
	f := strings.Fields(rule)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "-j" {
			return f[i+1]
		}
	}
	return ""
}

func (f *fakeMangle) run(args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))
	if len(args) < 5 || args[1] != "-w" || args[2] != "-t" || args[3] != "mangle" {
		return "", errors.New("unexpected command")
	}
	bin, op, rest := args[0], args[4], args[5:]
	tables, ok := f.chains[bin]
	if !ok || len(rest) == 0 {
		return "", errors.New("no such binary")
	}
	chain := rest[0]
	rules, exists := tables[chain]
	switch op {
	case "-S":
		if !exists {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "-N %s\n", chain)
		for _, r := range rules {
			fmt.Fprintf(&b, "-A %s %s\n", chain, r)
		}
		return b.String(), nil
	case "-N":
		if exists {
			return "iptables: Chain already exists.", errors.New("exit status 1")
		}
		tables[chain] = nil
	case "-F":
		if !exists {
			return "", errors.New("exit status 1")
		}
		tables[chain] = nil
	case "-X":
		if !exists {
			return "", errors.New("exit status 1")
		}
		delete(tables, chain)
	case "-A", "-I":
		if !exists {
			return "", errors.New("exit status 1")
		}
		spec := rest[1:]
		pos := 1
		if op == "-I" && len(spec) > 0 {
			if n, err := strconv.Atoi(spec[0]); err == nil {
				pos, spec = n, spec[1:]
			}
		}
		rule := fakeMangleCanon(spec)
		if fakeMangleTarget(rule) == "DSCP" && f.rejectDSCP[bin] {
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		if op == "-A" {
			tables[chain] = append(rules, rule)
		} else {
			tables[chain] = append(rules[:pos-1], append([]string{rule}, rules[pos-1:]...)...)
		}
	case "-D":
		if !exists || len(rest) < 2 {
			return "", errors.New("exit status 1")
		}
		if n, err := strconv.Atoi(rest[1]); err == nil && len(rest) == 2 {
			if n < 1 || n > len(rules) {
				return "", errors.New("exit status 1")
			}
			tables[chain] = append(rules[:n-1:n-1], rules[n:]...)
			return "", nil
		}
		rule := fakeMangleCanon(rest[1:])
		for i, r := range rules {
			if r == rule {
				tables[chain] = append(rules[:i:i], rules[i+1:]...)
				return "", nil
			}
		}
		return "", errors.New("exit status 1")
	case "-C":
		rule := fakeMangleCanon(rest[1:])
		for _, r := range rules {
			if r == rule {
				return "", nil
			}
		}
		return "", errors.New("exit status 1")
	case "-L":
		if !exists {
			return "", errors.New("exit status 1")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "Chain %s (policy ACCEPT)\nnum  target     prot opt source               destination\n", chain)
		for i, r := range rules {
			fmt.Fprintf(&b, "%-4d %-10s all  --  0.0.0.0/0            0.0.0.0/0\n", i+1, fakeMangleTarget(r))
		}
		return b.String(), nil
	default:
		return "", errors.New("unsupported op " + op)
	}
	return "", nil
}

func (f *fakeMangle) mutations() []string {
	var out []string
	for _, c := range f.calls {
		for _, op := range []string{" -N ", " -F ", " -X ", " -A ", " -I ", " -D "} {
			if strings.Contains(c, op) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

func resetDSCPState(t *testing.T) {
	t.Helper()
	reset := func() {
		dscpApplied.Store(nil)
		dscpStale.Store(nil)
		dscpLast.Store(nil)
		dscpKeepOnRefresh = false
		dscpWarned.Range(func(k, _ any) bool {
			dscpWarned.Delete(k)
			return true
		})
	}
	reset()
	t.Cleanup(reset)
}

func installFakeMangle(t *testing.T, f *fakeMangle) {
	t.Helper()
	resetDSCPState(t)
	present := map[string]bool{backendIPTables: false, backendIP6Tables: false, backendIPTablesLegacy: false, backendIP6TablesLegacy: false}
	for bin := range f.chains {
		present[bin] = true
	}
	stubBinaryPresence(t, present)
	sys := t.TempDir()
	if err := os.MkdirAll(filepath.Join(sys, "xt_DSCP"), 0o755); err != nil {
		t.Fatal(err)
	}
	origRun, origSys := run, kmodSysRoot
	run, kmodSysRoot = f.run, sys
	t.Cleanup(func() { run, kmodSysRoot = origRun, origSys })
}

func dscpTestConfig(enabled bool, value int, ifaces ...string) *config.Config {
	cfg := config.NewConfig()
	cfg.System.Tables.DSCP = config.DSCPConfig{Enabled: enabled, Value: value, Interfaces: ifaces}
	return &cfg
}

func TestDSCPIptSpecs(t *testing.T) {
	got := dscpIptSpecs(7, nil)
	want := [][]string{
		{"-o", "lo", "-j", "RETURN"},
		{"-m", "mark", "--mark", "0x20000000/0x20000000", "-j", "RETURN"},
		{"-m", "conntrack", "--ctdir", "REPLY", "-j", "RETURN"},
		{"-j", "DSCP", "--set-dscp", "7"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("specs without interfaces:\n got %v\nwant %v", got, want)
	}
	got = dscpIptSpecs(46, []string{"eth0", "wg0"})
	if n := len(got); n != 5 {
		t.Fatalf("expected the three RETURN rules and one stamp per interface, got %d specs: %v", n, got)
	}
	if !reflect.DeepEqual(got[3], []string{"-o", "eth0", "-j", "DSCP", "--set-dscp", "46"}) ||
		!reflect.DeepEqual(got[4], []string{"-o", "wg0", "-j", "DSCP", "--set-dscp", "46"}) {
		t.Errorf("per-interface stamps are wrong: %v", got[3:])
	}
	if dscpIptStampCount(nil) != 1 || dscpIptStampCount([]string{"a", "b"}) != 2 {
		t.Errorf("stamp count does not follow the interface list")
	}
	returns := 0
	for _, spec := range got {
		if iptSpecTarget(spec) == "RETURN" {
			returns++
		}
	}
	if returns != dscpReturnRules {
		t.Errorf("the chain has %d RETURN rules but the monitor expects %d", returns, dscpReturnRules)
	}
}

func TestDSCPNftScript(t *testing.T) {
	script, stamps := dscpNftScript(7, nil)
	want := "add table inet b4_dscp\n" +
		"delete table inet b4_dscp\n" +
		"add table inet b4_dscp\n" +
		"add chain inet b4_dscp postrouting { type filter hook postrouting priority 150 ; policy accept ; }\n" +
		"add rule inet b4_dscp postrouting oifname \"lo\" return\n" +
		"add rule inet b4_dscp postrouting meta mark & 0x20000000 == 0x20000000 return\n" +
		"add rule inet b4_dscp postrouting ct direction reply return\n" +
		"add rule inet b4_dscp postrouting meta nfproto ipv4 ip dscp set 7\n" +
		"add rule inet b4_dscp postrouting meta nfproto ipv6 ip6 dscp set 7\n"
	if script != want || stamps != 2 {
		t.Fatalf("script without interfaces (stamps %d):\n%s\nwant:\n%s", stamps, script, want)
	}

	script, stamps = dscpNftScript(31, []string{"eth0", "ppp0"})
	if stamps != 4 {
		t.Errorf("two families on two interfaces are four stamps, got %d", stamps)
	}
	for _, line := range []string{
		"add rule inet b4_dscp postrouting oifname \"eth0\" meta nfproto ipv4 ip dscp set 31\n",
		"add rule inet b4_dscp postrouting oifname \"eth0\" meta nfproto ipv6 ip6 dscp set 31\n",
		"add rule inet b4_dscp postrouting oifname \"ppp0\" meta nfproto ipv4 ip dscp set 31\n",
		"add rule inet b4_dscp postrouting oifname \"ppp0\" meta nfproto ipv6 ip6 dscp set 31\n",
	} {
		if !strings.Contains(script, line) {
			t.Errorf("missing %q in:\n%s", line, script)
		}
	}
	if strings.Contains(script, "ip dscp set 31\nadd rule inet b4_dscp postrouting meta nfproto") {
		t.Errorf("an interface list must not also leave an unscoped stamp:\n%s", script)
	}
	if n := strings.Count(script, " return\n"); n != dscpReturnRules {
		t.Errorf("the table has %d return rules but the monitor expects %d", n, dscpReturnRules)
	}
}

func TestDSCPJumpSeated(t *testing.T) {
	list := func(targets ...string) string {
		var b strings.Builder
		b.WriteString("Chain POSTROUTING (policy ACCEPT)\nnum  target     prot opt source               destination\n")
		for i, tg := range targets {
			fmt.Fprintf(&b, "%d    %s    all  --  0.0.0.0/0            0.0.0.0/0\n", i+1, tg)
		}
		return b.String()
	}
	cases := []struct {
		name    string
		listing string
		want    bool
	}{
		{"above the capture jump", list(dscpChainName, dscpCaptureChain), true},
		{"below the capture jump", list(dscpCaptureChain, dscpChainName), false},
		{"missing", list(dscpCaptureChain), false},
		{"no capture jump in device mode", list("FOREIGN", dscpChainName), true},
		{"two copies", list(dscpChainName, dscpCaptureChain, dscpChainName), false},
		{"a foreign rule on top does not matter", list("FOREIGN", dscpChainName, dscpCaptureChain), true},
	}
	for _, tc := range cases {
		if got := dscpJumpSeated(tc.listing); got != tc.want {
			t.Errorf("%s: seated=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDSCPIptablesGoesAboveTheCaptureJumpAndStaysThere(t *testing.T) {
	f := newFakeMangle(backendIPTables, backendIP6Tables)
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		f.chains[bin]["POSTROUTING"] = []string{"-j B4"}
	}
	installFakeMangle(t, f)

	cfg := dscpTestConfig(true, 7)
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		if got := f.chains[bin]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4_DSCP", "-j B4"}) {
			t.Errorf("%s POSTROUTING = %v, want the stamp jump above the capture jump", bin, got)
		}
		want := []string{"-o lo -j RETURN", "-m mark --mark 0x20000000/0x20000000 -j RETURN", "-m conntrack --ctdir REPLY -j RETURN", "-j DSCP --set-dscp 0x07"}
		if got := f.chains[bin][dscpChainName]; !reflect.DeepEqual(got, want) {
			t.Errorf("%s %s = %v, want %v", bin, dscpChainName, got, want)
		}
	}
	st := dscpApplied.Load()
	if st == nil || !reflect.DeepEqual(st.bins, []string{backendIPTables, backendIP6Tables}) || st.stamps != 1 {
		t.Fatalf("recorded state is wrong: %+v", st)
	}

	f.calls = nil
	if ensureDSCPLocked(nil, false) {
		t.Errorf("the monitor restored a stamp that was in place")
	}
	if m := f.mutations(); len(m) != 0 {
		t.Errorf("a check of an intact stamp changed the firewall: %v", m)
	}

	f.chains[backendIPTables]["POSTROUTING"] = []string{"-j B4", "-j B4_DSCP"}
	if !ensureDSCPLocked(nil, false) {
		t.Fatalf("the monitor did not notice the stamp below the capture jump")
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4_DSCP", "-j B4"}) {
		t.Errorf("after the monitor check POSTROUTING = %v", got)
	}

	f.calls = nil
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("second applyDSCPFor: %v", err)
	}
	if m := f.mutations(); len(m) != 0 {
		t.Errorf("re-applying an unchanged stamp rewrote the firewall: %v", m)
	}
}

func TestDSCPIptablesRebuildsTheChainOnAChange(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)

	if err := applyDSCPFor(dscpTestConfig(true, 7), backendIPTables); err != nil {
		t.Fatalf("first apply: %v", err)
	}
	if err := applyDSCPFor(dscpTestConfig(true, 31, "eth0", "wg0"), backendIPTables); err != nil {
		t.Fatalf("second apply: %v", err)
	}
	want := []string{"-o lo -j RETURN", "-m mark --mark 0x20000000/0x20000000 -j RETURN", "-m conntrack --ctdir REPLY -j RETURN", "-o eth0 -j DSCP --set-dscp 0x1f", "-o wg0 -j DSCP --set-dscp 0x1f"}
	if got := f.chains[backendIPTables][dscpChainName]; !reflect.DeepEqual(got, want) {
		t.Errorf("chain after the change = %v, want %v", got, want)
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4_DSCP"}) {
		t.Errorf("POSTROUTING after the change = %v, want exactly one jump", got)
	}
	if st := dscpApplied.Load(); st == nil || st.stamps != 2 {
		t.Errorf("recorded stamps = %+v, want 2", st)
	}

	if err := applyDSCPFor(dscpTestConfig(false, 31), backendIPTables); err != nil {
		t.Fatalf("switching off: %v", err)
	}
	if _, ok := f.chains[backendIPTables][dscpChainName]; ok {
		t.Errorf("the chain survived the switch going off")
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; len(got) != 0 {
		t.Errorf("POSTROUTING still holds %v after the switch went off", got)
	}
	if dscpApplied.Load() != nil {
		t.Errorf("the stamp is still recorded as applied")
	}
}

func TestDSCPIptablesRejectedTargetSkipsOnlyThatFamily(t *testing.T) {
	f := newFakeMangle(backendIPTables, backendIP6Tables)
	f.rejectDSCP[backendIP6Tables] = true
	installFakeMangle(t, f)

	err := applyDSCPFor(dscpTestConfig(true, 7), backendIPTables)
	if err == nil {
		t.Fatalf("a rejected DSCP target must be reported")
	}
	for _, want := range []string{backendIP6Tables, "IPv6", "iptables-mod-ipopt"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err, want)
		}
	}
	if _, ok := f.chains[backendIP6Tables][dscpChainName]; ok {
		t.Errorf("a half-built chain was left behind for the rejected family")
	}
	if got := f.chains[backendIP6Tables]["POSTROUTING"]; len(got) != 0 {
		t.Errorf("the rejected family still jumps somewhere: %v", got)
	}
	st := dscpApplied.Load()
	if st == nil || !reflect.DeepEqual(st.bins, []string{backendIPTables}) {
		t.Fatalf("only the IPv4 stamp should be recorded, got %+v", st)
	}
	f.calls = nil
	if ensureDSCPLocked(nil, false) {
		t.Errorf("the monitor demanded the stamp the kernel refused, which would rebuild it every tick")
	}
	if m := f.mutations(); len(m) != 0 {
		t.Errorf("the monitor check changed the firewall: %v", m)
	}
}

func TestDSCPOffTouchesNothingUnlessItWasOn(t *testing.T) {
	f := newFakeMangle(backendIPTables, backendIP6Tables)
	installFakeMangle(t, f)

	if err := applyDSCPFor(dscpTestConfig(false, 7), backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	if len(f.calls) != 0 {
		t.Errorf("with the stamp off and nothing installed, no command may run: %v", f.calls)
	}
	if ensureDSCPLocked(nil, false) || len(f.calls) != 0 {
		t.Errorf("the monitor acted on a stamp that was never installed: %v", f.calls)
	}
}

func TestDSCPTransientFailureStaysWantedAndIsRetried(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)
	realRun := run
	vanish := true
	run = func(args ...string) (string, error) {
		if vanish && len(args) > 6 && args[4] == "-A" && args[5] == dscpChainName && fakeMangleTarget(strings.Join(args[6:], " ")) == "DSCP" {
			vanish = false
			delete(f.chains[args[0]], dscpChainName)
			return "iptables: No chain/target/match by that name.", errors.New("exit status 1")
		}
		return realRun(args...)
	}

	cfg := dscpTestConfig(true, 7)
	err := applyDSCPFor(cfg, backendIPTables)
	if err == nil {
		t.Fatalf("a chain that vanished mid-apply must be reported")
	}
	if strings.Contains(err.Error(), "ipopt") || strings.Contains(err.Error(), "rejected") {
		t.Errorf("a vanished chain was blamed on a missing DSCP target: %v", err)
	}
	st := dscpApplied.Load()
	if st == nil || !reflect.DeepEqual(st.bins, []string{backendIPTables}) || !st.pending {
		t.Fatalf("a transient failure must keep the family wanted and pending so the monitor retries, got %+v", st)
	}
	if !ensureDSCPLocked(cfg, false) {
		t.Fatalf("the monitor did not retry the family that failed")
	}
	if st := dscpApplied.Load(); st == nil || st.pending {
		t.Errorf("a successful retry left the stamp pending: %+v", st)
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4_DSCP"}) {
		t.Errorf("after the retry POSTROUTING = %v", got)
	}
	if ensureDSCPLocked(cfg, false) {
		t.Errorf("the monitor kept restoring a stamp that was back in place")
	}
}

func TestDSCPPermanentRefusalOnReapplyStopsDemanding(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)
	cfg := dscpTestConfig(true, 7)
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	delete(f.chains[backendIPTables], dscpChainName)
	f.chains[backendIPTables]["POSTROUTING"] = nil
	f.rejectDSCP[backendIPTables] = true
	if !ensureDSCPLocked(cfg, false) {
		t.Fatalf("the monitor did not notice the missing stamp")
	}
	if st := dscpApplied.Load(); st != nil {
		t.Fatalf("a family the kernel now refuses is still demanded, so every tick would rebuild it: %+v", st)
	}
	f.calls = nil
	if ensureDSCPLocked(cfg, false) || len(f.mutations()) != 0 {
		t.Errorf("the monitor kept rebuilding a refused family: %v", f.mutations())
	}
}

func TestDSCPTimeoutOnReapplyIsRetried(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)
	cfg := dscpTestConfig(true, 7)
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	delete(f.chains[backendIPTables], dscpChainName)
	f.chains[backendIPTables]["POSTROUTING"] = nil
	realRun := run
	stall := true
	run = func(args ...string) (string, error) {
		if stall && len(args) > 5 && args[4] == "-N" && args[5] == dscpChainName {
			stall = false
			return "", fmt.Errorf("command [%s] gave up after 15s: %w", strings.Join(args, " "), context.DeadlineExceeded)
		}
		return realRun(args...)
	}
	if !ensureDSCPLocked(cfg, false) {
		t.Fatalf("the monitor did not notice the missing stamp")
	}
	if st := dscpApplied.Load(); st == nil || len(st.bins) != 1 {
		t.Fatalf("a timed-out command was taken as a permanent refusal: %+v", st)
	}
	if !ensureDSCPLocked(cfg, false) {
		t.Fatalf("the next tick did not retry")
	}
	if _, ok := f.chains[backendIPTables][dscpChainName]; !ok {
		t.Errorf("the stamp did not come back on the next tick")
	}
}

func TestDSCPUnreadableSweepOfNothingIsNotRetried(t *testing.T) {
	f := newFakeMangle(backendIPTables, backendIP6Tables)
	installFakeMangle(t, f)
	realRun := run
	run = func(args ...string) (string, error) {
		if args[0] == backendIP6Tables && len(args) > 4 && args[4] == "-S" {
			return "ip6tables v1.8.7 (nf_tables): Could not fetch rule set generation id: Invalid argument", errors.New("exit status 4")
		}
		return realRun(args...)
	}
	clearDSCPFor(dscpTestConfig(false, 0), backendIPTables)
	if dscpStale.Load() != nil {
		t.Fatalf("an unreadable family with nothing ever installed must not be retried on every tick")
	}
	f.calls = nil
	if ensureDSCPLocked(dscpTestConfig(false, 0), false) || len(f.calls) != 0 {
		t.Errorf("the monitor ran commands for a stamp that was never installed: %v", f.calls)
	}
}

func TestDSCPFamilyTheKernelCannotServeIsDroppedOnce(t *testing.T) {
	cases := []struct {
		name     string
		out      string
		absent   bool
		exitCode string
	}{
		{"IPv6 disabled at boot", "ip6tables v1.8.7 (legacy): can't initialize ip6tables table `mangle': Address family not supported by protocol\nPerhaps ip6tables or your kernel needs to be upgraded.", true, "exit status 3"},
		{"ip6_tables not loaded", "ip6tables v1.4.21: can't initialize ip6tables table `mangle': iptables who? (do you need to insmod?)\nPerhaps ip6tables or your kernel needs to be upgraded.", true, "exit status 3"},
		{"no IPv6 protocol", "ip6tables: Protocol not supported.", true, "exit status 1"},
		{"an unknown refusal", "ip6tables v1.8.7 (nf_tables): Could not fetch rule set generation id: Invalid argument", false, "exit status 4"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeMangle(backendIPTables, backendIP6Tables)
			installFakeMangle(t, f)
			realRun := run
			run = func(args ...string) (string, error) {
				if args[0] == backendIP6Tables {
					return tc.out, errors.New(tc.exitCode)
				}
				return realRun(args...)
			}

			cfg := dscpTestConfig(true, 7)
			err := applyDSCPFor(cfg, backendIPTables)
			if err == nil || !strings.Contains(err.Error(), "IPv6") {
				t.Fatalf("the refused family must be reported, got %v", err)
			}
			st := dscpApplied.Load()
			if st == nil || !reflect.DeepEqual(st.bins, []string{backendIPTables}) || st.pending {
				t.Fatalf("only the IPv4 stamp should be wanted, with nothing pending: %+v", st)
			}
			f.calls = nil
			if ensureDSCPLocked(cfg, false) {
				t.Errorf("the monitor retried a family the kernel cannot serve")
			}
			if m := f.mutations(); len(m) != 0 {
				t.Errorf("the monitor check changed the firewall: %v", m)
			}

			clearDSCPFor(dscpTestConfig(false, 7), backendIPTables)
			if _, ok := f.chains[backendIPTables][dscpChainName]; ok {
				t.Errorf("switching off left the IPv4 chain behind")
			}
			if stale := dscpStale.Load() != nil; stale == tc.absent {
				t.Errorf("stale record after switching off = %v, want %v", stale, !tc.absent)
			}
		})
	}
}

func TestDSCPTransientSeatFailureKeepsAnIntactChain(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	f.chains[backendIPTables]["POSTROUTING"] = []string{"-j B4"}
	installFakeMangle(t, f)
	cfg := dscpTestConfig(true, 7)
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	chain := append([]string(nil), f.chains[backendIPTables][dscpChainName]...)

	realRun := run
	stall := true
	run = func(args ...string) (string, error) {
		if stall && len(args) > 5 && args[4] == "-L" && args[5] == "POSTROUTING" {
			stall = false
			return "", fmt.Errorf("command [%s] gave up after 15s: %w", strings.Join(args, " "), context.DeadlineExceeded)
		}
		return realRun(args...)
	}
	if err := applyDSCPFor(cfg, backendIPTables); err == nil {
		t.Fatalf("a timed-out read must be reported")
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4_DSCP", "-j B4"}) {
		t.Errorf("a timed-out read tore the working jump down: POSTROUTING = %v", got)
	}
	if got := f.chains[backendIPTables][dscpChainName]; !reflect.DeepEqual(got, chain) {
		t.Errorf("a timed-out read tore the working chain down: %v", got)
	}
	if st := dscpApplied.Load(); st == nil || !st.pending {
		t.Fatalf("a timed-out read must leave the stamp pending for the monitor: %+v", st)
	}
	if !ensureDSCPLocked(cfg, false) {
		t.Fatalf("the monitor did not retry the pending stamp")
	}
	if st := dscpApplied.Load(); st == nil || st.pending {
		t.Errorf("a successful retry left the stamp pending: %+v", st)
	}
}

func TestDSCPCaptureJumpGoesBelowTheStamp(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)
	cfg := dscpTestConfig(true, 7)
	im := NewIPTablesManager(cfg, false)
	capture := Rule{manager: im, IPT: backendIPTables, Table: "mangle", Chain: "POSTROUTING", Action: "I",
		Spec: []string{"-j", dscpCaptureChain}, BelowDSCP: true}

	f.calls = nil
	if err := capture.Apply(); err != nil {
		t.Fatalf("capture.Apply without a stamp: %v", err)
	}
	for _, c := range f.calls {
		if strings.Contains(c, " -L ") {
			t.Errorf("with no stamp recorded the capture insert read the chain: %s", c)
		}
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4"}) {
		t.Fatalf("POSTROUTING = %v", got)
	}

	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	f.chains[backendIPTables]["POSTROUTING"] = []string{"-j B4_DSCP"}
	f.calls = nil
	if err := capture.Apply(); err != nil {
		t.Fatalf("capture.Apply under a stamp: %v", err)
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4_DSCP", "-j B4"}) {
		t.Errorf("a capture jump put back under a kept stamp went above it: POSTROUTING = %v", got)
	}
	f.calls = nil
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor after the capture insert: %v", err)
	}
	if m := f.mutations(); len(m) != 0 {
		t.Errorf("the stamp needed re-seating after the capture insert: %v", m)
	}

	f.chains[backendIPTables]["POSTROUTING"] = []string{"-j FOREIGN", "-j B4_DSCP"}
	if err := capture.Apply(); err != nil {
		t.Fatalf("capture.Apply under a foreign rule: %v", err)
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4_DSCP", "-j B4", "-j FOREIGN"}) {
		t.Errorf("with a foreign rule on top the stamp must go back on top before the capture jump: POSTROUTING = %v", got)
	}
	f.calls = nil
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor after the capture insert under a foreign rule: %v", err)
	}
	if m := f.mutations(); len(m) != 0 {
		t.Errorf("the stamp needed re-seating after the capture insert under a foreign rule: %v", m)
	}

	f.chains[backendIPTables]["POSTROUTING"] = []string{"-j FOREIGN"}
	if err := capture.Apply(); err != nil {
		t.Fatalf("capture.Apply with the stamp jump gone: %v", err)
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4", "-j FOREIGN"}) {
		t.Errorf("with the stamp jump gone the capture jump must go first and leave the stamp to the monitor: POSTROUTING = %v", got)
	}
}

func TestDSCPOnlyTheCaptureJumpGoesBelowTheStamp(t *testing.T) {
	stubBinaryPresence(t, map[string]bool{backendIPTables: true, backendIP6Tables: true, "ipset": true})
	cfg := dscpTestConfig(true, 7)
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = true
	manager := NewIPTablesManager(cfg, false)
	stubProbes(manager, backendIPTables, backendIP6Tables)
	m, err := manager.buildManifest()
	if err != nil {
		t.Fatalf("buildManifest: %v", err)
	}
	below := map[string]int{}
	for _, r := range m.Rules {
		capture := r.Table == "mangle" && r.Chain == "POSTROUTING" && reflect.DeepEqual(r.Spec, []string{"-j", dscpCaptureChain})
		if r.BelowDSCP != capture {
			t.Errorf("%s %s %s %v: BelowDSCP = %v, want %v", r.IPT, r.Table, r.Chain, r.Spec, r.BelowDSCP, capture)
		}
		if r.BelowDSCP {
			below[r.IPT]++
		}
	}
	if below[backendIPTables] != 1 || below[backendIP6Tables] != 1 {
		t.Errorf("capture jumps kept below the stamp = %v, want one per binary", below)
	}
}

func TestDSCPChainThatAlreadyExistsIsNotARefusal(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)
	cfg := dscpTestConfig(true, 7)
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	realRun := run
	stalls := 3
	run = func(args ...string) (string, error) {
		if stalls > 0 && len(args) > 5 && args[4] == "-S" && args[5] == dscpChainName {
			stalls--
			return "", fmt.Errorf("command [%s] gave up after 15s: %w", strings.Join(args, " "), context.DeadlineExceeded)
		}
		return realRun(args...)
	}
	err := applyDSCPFor(cfg, backendIPTables)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("the unreadable chain must be reported, got %v", err)
	}
	if st := dscpApplied.Load(); st == nil || !st.pending {
		t.Fatalf("a chain that already exists was taken as a refusal: %+v", st)
	}
	if _, ok := f.chains[backendIPTables][dscpChainName]; !ok {
		t.Fatalf("a chain that already exists was torn down")
	}
	if !ensureDSCPLocked(cfg, false) {
		t.Fatalf("the monitor did not retry the pending stamp")
	}
	if st := dscpApplied.Load(); st == nil || st.pending {
		t.Errorf("a successful retry left the stamp pending: %+v", st)
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4_DSCP"}) {
		t.Errorf("after the retry POSTROUTING = %v", got)
	}
}

func TestDSCPUnreadableRemovalOfAnInstalledStampIsRetried(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)
	cfg := dscpTestConfig(true, 7)
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	realRun := run
	stall := true
	run = func(args ...string) (string, error) {
		if stall && len(args) > 5 && args[4] == "-S" && args[5] == dscpChainName {
			return "", fmt.Errorf("command [%s] gave up after 15s: %w", strings.Join(args, " "), context.DeadlineExceeded)
		}
		return realRun(args...)
	}
	clearDSCPFor(dscpTestConfig(false, 7), backendIPTables)
	if dscpStale.Load() == nil {
		t.Fatalf("a stamp b4 installed but could not confirm removed was forgotten")
	}
	stall = false
	if !ensureDSCPLocked(dscpTestConfig(false, 7), false) {
		t.Fatalf("the monitor did not retry the removal")
	}
	if _, ok := f.chains[backendIPTables][dscpChainName]; ok {
		t.Errorf("the chain survived the retried removal")
	}
	if dscpStale.Load() != nil {
		t.Errorf("the retried removal left the stale record behind")
	}
}

func TestDSCPNftTimeoutKeepsTheTableAndIsRetried(t *testing.T) {
	resetDSCPState(t)
	stubBinaryPresence(t, map[string]bool{"nft": true})
	origRun, origStdin := run, runNftStdin
	t.Cleanup(func() { run, runNftStdin = origRun, origStdin })
	var calls []string
	run = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		return "", nil
	}
	runNftStdin = func(string) (string, error) {
		return "", fmt.Errorf("command [nft -f -] gave up after 15s: %w", context.DeadlineExceeded)
	}
	if err := applyDSCPFor(dscpTestConfig(true, 7), backendNFTables); err == nil {
		t.Fatalf("a timed-out load must be reported")
	}
	for _, c := range calls {
		if strings.Contains(c, "delete table") {
			t.Errorf("a timed-out load deleted the table: %s", c)
		}
	}
	if st := dscpApplied.Load(); st == nil || st.backend != backendNFTables || !st.pending {
		t.Fatalf("a timed-out load must stay wanted and pending so the monitor retries, got %+v", st)
	}

	loads := 0
	runNftStdin = func(string) (string, error) {
		loads++
		return "", nil
	}
	run = func(args ...string) (string, error) {
		if strings.Join(args, " ") == "nft list chain inet b4_dscp postrouting" {
			return "table inet b4_dscp {\n\tchain postrouting {\n\t\toifname \"lo\" return\n\t\tmeta mark & 0x20000000 == 0x20000000 return\n\t\tct direction reply return\n\t\tip dscp set 0x03\n\t\tip6 dscp set 0x03\n\t}\n}\n", nil
		}
		return "", nil
	}
	if !ensureDSCPLocked(dscpTestConfig(true, 7), false) || loads != 1 {
		t.Fatalf("the monitor trusted a table of the same shape left from before the timeout, loads=%d", loads)
	}
	if st := dscpApplied.Load(); st == nil || st.pending {
		t.Fatalf("a successful retry left the stamp pending: %+v", st)
	}
	if ensureDSCPLocked(dscpTestConfig(true, 7), false) || loads != 1 {
		t.Errorf("the monitor kept reloading a table that was in place, loads=%d", loads)
	}
}

func TestDSCPFailedRemovalIsRetried(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)
	cfg := dscpTestConfig(true, 7)
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	realRun := run
	refuse := true
	run = func(args ...string) (string, error) {
		if refuse && len(args) > 5 && args[4] == "-X" && args[5] == dscpChainName {
			return "iptables: Too many links.", errors.New("exit status 1")
		}
		return realRun(args...)
	}

	clearDSCPFor(dscpTestConfig(false, 7), backendIPTables)
	if dscpApplied.Load() != nil {
		t.Errorf("a switched-off stamp is still recorded as applied")
	}
	if dscpStale.Load() == nil {
		t.Fatalf("a chain that could not be deleted was forgotten")
	}
	refuse = false
	if !ensureDSCPLocked(dscpTestConfig(false, 7), false) {
		t.Fatalf("the monitor did not retry the removal")
	}
	if _, ok := f.chains[backendIPTables][dscpChainName]; ok {
		t.Errorf("the chain survived the retried removal")
	}
	if dscpStale.Load() != nil {
		t.Errorf("the retried removal left the stale record behind")
	}
}

func TestDSCPMonitorLeavesTheStampAloneUnderSkipSetup(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)
	cfg := dscpTestConfig(true, 7)
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	delete(f.chains[backendIPTables], dscpChainName)
	f.chains[backendIPTables]["POSTROUTING"] = nil
	f.calls = nil

	skip := dscpTestConfig(true, 7)
	skip.System.Tables.SkipSetup = true
	if ensureDSCPLocked(skip, false) || len(f.calls) != 0 {
		t.Errorf("with skip_setup on the monitor touched the firewall: %v", f.calls)
	}
}

func TestDSCPRefreshKeepsAnUnchangedStamp(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)
	cfg := dscpTestConfig(true, 7)
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	dscpKeepOnRefresh = true
	t.Cleanup(func() { dscpKeepOnRefresh = false })

	f.calls = nil
	clearDSCPUnlessKept(dscpTestConfig(true, 7), backendIPTables)
	if m := f.mutations(); len(m) != 0 {
		t.Errorf("a refresh with unchanged DSCP settings removed the stamp: %v", m)
	}
	if dscpApplied.Load() == nil {
		t.Errorf("a refresh with unchanged DSCP settings forgot the stamp")
	}

	clearDSCPUnlessKept(dscpTestConfig(true, 31), backendIPTables)
	if _, ok := f.chains[backendIPTables][dscpChainName]; ok {
		t.Errorf("a clear for DSCP settings other than the stamp's must remove it, even during a refresh")
	}

	dscpKeepOnRefresh = false
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	clearDSCPUnlessKept(cfg, backendIPTables)
	if _, ok := f.chains[backendIPTables][dscpChainName]; ok {
		t.Errorf("a clear outside a refresh must remove the stamp")
	}
}

func TestDSCPOnlyPathsStaySilentWhenOff(t *testing.T) {
	f := newFakeMangle(backendIPTables, backendIP6Tables)
	installFakeMangle(t, f)
	stubBinaryPresence(t, map[string]bool{"nft": true})

	cfg := dscpTestConfig(false, 7)
	if err := ApplyDSCPOnly(cfg); err != nil {
		t.Fatalf("ApplyDSCPOnly: %v", err)
	}
	ClearDSCPOnly(cfg)
	if len(f.calls) != 0 {
		t.Errorf("the TUN-mode apply and clear ran commands with the stamp off, backend detection included: %v", f.calls)
	}
	if dscpLast.Load() != nil {
		t.Errorf("ClearDSCPOnly must forget the last applied configuration")
	}
}

func TestClearDSCPWithNothingInstalledOnlyReads(t *testing.T) {
	f := newFakeMangle(backendIPTables, backendIP6Tables)
	installFakeMangle(t, f)

	clearDSCPFor(dscpTestConfig(false, 0), backendIPTables)
	if m := f.mutations(); len(m) != 0 {
		t.Errorf("a clear with no stamp in place changed the firewall: %v", m)
	}
	for _, c := range f.calls {
		if !strings.HasSuffix(c, "-S "+dscpChainName) {
			t.Errorf("unexpected command in a clear with no stamp in place: %s", c)
		}
	}
}

func TestClearDSCPSweepsLeftoversWhateverTheConfigSays(t *testing.T) {
	f := newFakeMangle(backendIPTables, backendIP6Tables)
	f.chains[backendIPTables]["POSTROUTING"] = []string{"-j B4_DSCP", "-j B4", "-j B4_DSCP"}
	f.chains[backendIPTables][dscpChainName] = []string{"-j DSCP --set-dscp 0x2e"}
	installFakeMangle(t, f)

	clearDSCPFor(dscpTestConfig(false, 0), backendIPTables)
	if _, ok := f.chains[backendIPTables][dscpChainName]; ok {
		t.Errorf("a stamp chain left by an earlier run survived the clear")
	}
	if got := f.chains[backendIPTables]["POSTROUTING"]; !reflect.DeepEqual(got, []string{"-j B4"}) {
		t.Errorf("POSTROUTING after the clear = %v, want only the capture jump", got)
	}
	for _, c := range f.mutations() {
		if strings.HasPrefix(c, backendIP6Tables) {
			t.Errorf("the clear changed ip6tables although it had no stamp: %s", c)
		}
	}
}

func TestDSCPNftRejectedScriptLeavesNoTable(t *testing.T) {
	resetDSCPState(t)
	stubBinaryPresence(t, map[string]bool{"nft": true})
	origRun, origStdin := run, runNftStdin
	t.Cleanup(func() { run, runNftStdin = origRun, origStdin })
	var calls []string
	run = func(args ...string) (string, error) {
		calls = append(calls, strings.Join(args, " "))
		if strings.Join(args, " ") == "nft list tables" {
			return "table inet b4_mangle\ntable inet b4_dscp\n", nil
		}
		return "", nil
	}
	runNftStdin = func(string) (string, error) {
		return "Error: syntax error", errors.New("command [nft -f -] failed: exit status 1 (Error: syntax error)")
	}

	if err := applyDSCPFor(dscpTestConfig(true, 7), backendNFTables); err == nil {
		t.Fatalf("a rejected script must be reported")
	}
	if dscpApplied.Load() != nil {
		t.Errorf("a rejected script left the stamp recorded as applied")
	}
	deleted := false
	for _, c := range calls {
		if c == "nft delete table inet b4_dscp" {
			deleted = true
		}
	}
	if !deleted {
		t.Errorf("the old table was not removed after the rejected script: %v", calls)
	}
}

func TestDSCPNftIntactChecksTheWholeChain(t *testing.T) {
	origRun := run
	t.Cleanup(func() { run = origRun })
	listing := ""
	run = func(args ...string) (string, error) {
		if strings.Join(args, " ") == "nft list chain inet b4_dscp postrouting" {
			if listing == "" {
				return "Error: No such file or directory", errors.New("exit status 1")
			}
			return listing, nil
		}
		return "", errors.New("unexpected")
	}
	st := &dscpState{backend: backendNFTables, stamps: 2}
	if dscpIntact(st) {
		t.Errorf("a missing table reads as intact")
	}

	chain := func(rules ...string) string {
		return "table inet b4_dscp {\n\tchain postrouting {\n\t\ttype filter hook postrouting priority 150; policy accept;\n\t\t" +
			strings.Join(rules, "\n\t\t") + "\n\t}\n}\n"
	}
	lo := `oifname "lo" return`
	client := "meta mark & 0x20000000 == 0x20000000 return"
	reply := "ct direction reply return"
	v4, v6 := "ip dscp set 0x07", "ip6 dscp set 0x07"
	cases := []struct {
		name    string
		listing string
		want    bool
	}{
		{"complete", chain(lo, client, reply, v4, v6), true},
		{"a named value", chain(lo, client, reply, `oifname "wan" ip dscp set ef`, `oifname "wan" ip6 dscp set ef`), true},
		{"one stamp missing", chain(lo, client, reply, v4), false},
		{"a stamp twice", chain(lo, client, reply, v4, v6, v4), false},
		{"client mark return missing", chain(lo, reply, v4, v6), false},
		{"reply return missing", chain(lo, client, v4, v6), false},
		{"a return after a stamp", chain(lo, client, v4, reply, v6), false},
	}
	for _, tc := range cases {
		listing = tc.listing
		if got := dscpIntact(st); got != tc.want {
			t.Errorf("%s: intact=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDSCPIptChainShape(t *testing.T) {
	chain := func(rules ...string) string {
		var b strings.Builder
		b.WriteString("-N B4_DSCP\n")
		for _, r := range rules {
			fmt.Fprintf(&b, "-A B4_DSCP %s\n", r)
		}
		return b.String()
	}
	lo := "-o lo -j RETURN"
	client := "-m mark --mark 0x20000000/0x20000000 -j RETURN"
	reply := "-m conntrack --ctdir REPLY -j RETURN"
	stamp := "-j DSCP --set-dscp 0x07"
	cases := []struct {
		name    string
		listing string
		stamps  int
		want    bool
	}{
		{"complete", chain(lo, client, reply, stamp), 1, true},
		{"one stamp per interface", chain(lo, client, reply, "-o eth0 "+stamp, "-o wg0 "+stamp), 2, true},
		{"a warning line is ignored", chain(lo, client, reply, stamp) + "# Warning: iptables-legacy tables present, use iptables-legacy to see them\n", 1, true},
		{"flushed", chain(), 1, false},
		{"client mark return missing", chain(lo, reply, stamp), 1, false},
		{"a return after the stamp", chain(lo, client, stamp, reply), 1, false},
		{"one interface stamp missing", chain(lo, client, reply, "-o eth0 "+stamp), 2, false},
		{"a stamp twice", chain(lo, client, reply, stamp, stamp), 1, false},
		{"a foreign rule", chain(lo, client, reply, "-j ACCEPT", stamp), 1, false},
	}
	for _, tc := range cases {
		if got := dscpIptChainShape(tc.listing, tc.stamps); got != tc.want {
			t.Errorf("%s: intact=%v, want %v", tc.name, got, tc.want)
		}
	}
}

func TestDSCPMonitorRestoresAMissingExemption(t *testing.T) {
	f := newFakeMangle(backendIPTables)
	installFakeMangle(t, f)
	cfg := dscpTestConfig(true, 7)
	if err := applyDSCPFor(cfg, backendIPTables); err != nil {
		t.Fatalf("applyDSCPFor: %v", err)
	}
	want := append([]string(nil), f.chains[backendIPTables][dscpChainName]...)

	f.chains[backendIPTables][dscpChainName] = append(want[:1:1], want[2:]...)
	if !ensureDSCPLocked(cfg, false) {
		t.Fatalf("the monitor took a chain without the client mark exemption for intact")
	}
	if got := f.chains[backendIPTables][dscpChainName]; !reflect.DeepEqual(got, want) {
		t.Errorf("after the restore %s = %v, want %v", dscpChainName, got, want)
	}

	f.chains[backendIPTables][dscpChainName] = []string{want[0], want[1], want[3], want[2]}
	if !ensureDSCPLocked(cfg, false) {
		t.Fatalf("the monitor took a stamp ahead of the reply exemption for intact")
	}
	if got := f.chains[backendIPTables][dscpChainName]; !reflect.DeepEqual(got, want) {
		t.Errorf("after the reorder restore %s = %v, want %v", dscpChainName, got, want)
	}
	if ensureDSCPLocked(cfg, false) {
		t.Errorf("the monitor kept restoring a chain that was back in shape")
	}
}

func TestSetDSCPKeepOnRefreshWithPlan(t *testing.T) {
	a := func() *config.SetConfig { return dscpPlanTestSet("a", 31, "10.1.2.0/24") }

	t.Run("iptables", func(t *testing.T) {
		h := dscpIptNewHost(t, backendIPTables)
		dscpSyncIsolate(t)
		appliedResetGlobals(t)
		clearRulesFn = func(c *config.Config) error {
			clearDSCPUnlessKept(c, backendIPTables)
			return nil
		}
		addRulesFn = func(c *config.Config) error {
			rulesAppliedCfg = c
			applyDSCPLogged(c, backendIPTables)
			return nil
		}
		torn := func(stage string) {
			t.Helper()
			for _, e := range h.events {
				if strings.Contains(e, " -X ") || strings.Contains(e, " -F ") || strings.HasPrefix(e, "ipset flush") {
					t.Errorf("%s: the refresh tore down what it keeps: %s", stage, e)
				}
			}
		}

		if err := AddRules(dscpIptTestConfig(7, true, nil, a())); err != nil {
			t.Fatal(err)
		}
		learned := dscpIptLearnedSet(routeSanitizeSetID("a"), false)
		h.sets[learned]["10.9.9.9"] = true

		h.events = nil
		changed := dscpIptTestConfig(9, true, nil, a())
		dscpSyncPass(changed, false, nil)
		if err := RefreshRules(changed); err != nil {
			t.Fatal(err)
		}
		torn("a global change next to a plan")
		if i := h.first("ipset destroy"); i >= 0 {
			t.Errorf("a refresh with the plan unchanged destroyed an ipset: %s", h.events[i])
		}
		if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptRender(dscpPlanFor(changed), false, true)); !slices.Equal(got, want) {
			t.Errorf("chain after the refresh = %q, want %q", got, want)
		}
		if !h.sets[learned]["10.9.9.9"] {
			t.Errorf("a refresh lost an address learned for the set: %v", h.entries(learned))
		}
		if st := dscpApplied.Load(); st == nil || st.cfg != changed {
			t.Errorf("the refresh did not record the new configuration: %+v", st)
		}

		h.events = nil
		dropped := dscpIptTestConfig(9, true, nil)
		if err := RefreshRules(dropped); err != nil {
			t.Fatal(err)
		}
		torn("the last set leaving")
		lastRule := h.last(" -D " + dscpChainName + " ")
		if lastRule < 0 {
			t.Fatalf("leaving the plan did not replace the chain: %q", h.events)
		}
		for i, e := range h.events {
			if strings.HasPrefix(e, "ipset destroy") && i < lastRule {
				t.Errorf("%s ran before the rules naming the set were gone", e)
			}
		}
		if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptSpecs(9, nil)); !slices.Equal(got, want) {
			t.Errorf("chain after the plan left = %q, want %q", got, want)
		}
		if len(h.sets) != 0 {
			t.Errorf("ipsets survived the plan leaving: %v", h.snapshot())
		}

		h.mangle.calls = nil
		if err := RefreshRules(dscpIptTestConfig(9, true, nil)); err != nil {
			t.Fatal(err)
		}
		if m := h.mangle.mutations(); len(m) != 0 {
			t.Errorf("an unchanged global stamp was rewritten after the plan left: %v", m)
		}
	})

	t.Run("nftables", func(t *testing.T) {
		f := installDSCPNftFake(t)
		dscpSyncIsolate(t)
		appliedResetGlobals(t)
		clearRulesFn = func(c *config.Config) error {
			clearDSCPUnlessKept(c, backendNFTables)
			return nil
		}
		addRulesFn = func(c *config.Config) error {
			rulesAppliedCfg = c
			applyDSCPLogged(c, backendNFTables)
			return nil
		}
		if err := AddRules(dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 7}, a())); err != nil {
			t.Fatal(err)
		}
		learned := dscpNftLearnedSet(routeSanitizeSetID("a"), false)
		f.learn(learned, "10.9.9.9")
		mark, commands := len(f.scripts), len(f.commands)
		changed := dscpSyncNftConfig(config.DSCPConfig{Enabled: true, Value: 9}, a())
		dscpSyncPass(changed, false, nil)
		if err := RefreshRules(changed); err != nil {
			t.Fatal(err)
		}
		for _, s := range f.since(mark) {
			if strings.Contains(s, "delete table") {
				t.Errorf("the refresh rebuilt the table:\n%s", s)
			}
		}
		if slices.Contains(f.commands[commands:], "nft delete table inet "+dscpNftTable) {
			t.Errorf("the refresh deleted the table: %q", f.commands[commands:])
		}
		if !slices.Contains(f.table.sets[learned], "10.9.9.9") {
			t.Errorf("a refresh lost an address learned for the set: %v", f.table.sets[learned])
		}
		for addr, want := range map[string]int{"10.9.9.9": 31, "10.1.2.3": 31, "192.0.2.1": 9} {
			if got, _ := f.stamp(netip.MustParseAddr(addr), "wan"); got != want {
				t.Errorf("after the refresh %s leaves with DSCP %d, want %d", addr, got, want)
			}
		}
	})
}

func TestSetDSCPTeardownDestroysReferencedSets(t *testing.T) {
	both := func() *config.Config {
		return dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8", "fd00::/16"), dscpPlanTestSet("b", 6, "10.1.0.0/16"))
	}
	off := dscpIptTestConfig(7, false, nil)
	destroyedAfterChains := func(t *testing.T, h *dscpIptHost, names []string) {
		t.Helper()
		lastChain := h.last(" -X " + dscpChainName)
		for _, name := range names {
			if i := h.first("ipset destroy " + name); i < 0 || i < lastChain {
				t.Errorf("%s must be destroyed after the chains naming it are gone (at %d, chains until %d): %q", name, i, lastChain, h.events)
			}
		}
		for _, bin := range []string{backendIPTables, backendIP6Tables} {
			if _, ok := h.mangle.chains[bin][dscpChainName]; ok {
				t.Errorf("%s still has %s", bin, dscpChainName)
			}
		}
		if len(h.sets) != 0 {
			t.Errorf("ipsets survived the teardown: %v", h.snapshot())
		}
	}

	t.Run("recorded, referenced and pending sets", func(t *testing.T) {
		h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
		if err := applyDSCPFor(both(), backendIPTables); err != nil {
			t.Fatal(err)
		}
		h.busy["b4d_s6_v4"] = true
		if err := applyDSCPFor(dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8", "fd00::/16")), backendIPTables); err != nil {
			t.Fatal(err)
		}
		if st := dscpApplied.Load(); st == nil || !slices.Equal(st.ipt.pending, []string{"b4d_s6_v4"}) {
			t.Fatalf("the departed value's ipset should be pending: %+v", st)
		}
		delete(h.busy, "b4d_s6_v4")
		names := slices.Sorted(maps.Keys(h.sets))
		h.events = nil
		clearDSCPFor(off, backendIPTables)
		destroyedAfterChains(t, h, names)
		if dscpApplied.Load() != nil || dscpStale.Load() != nil {
			t.Errorf("a complete teardown left records: applied %+v, stale %+v", dscpApplied.Load(), dscpStale.Load())
		}
	})

	t.Run("a chain left by an earlier run", func(t *testing.T) {
		h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
		if err := applyDSCPFor(both(), backendIPTables); err != nil {
			t.Fatal(err)
		}
		names := slices.Sorted(maps.Keys(h.sets))
		dscpApplied.Store(nil)
		dscpIptRecordMu.Lock()
		clear(dscpIptRecord)
		dscpIptRecordMu.Unlock()
		h.events = nil
		clearDSCPFor(off, backendIPTables)
		destroyedAfterChains(t, h, names)
	})

	t.Run("a set still in use is retried by the monitor", func(t *testing.T) {
		h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
		if err := applyDSCPFor(both(), backendIPTables); err != nil {
			t.Fatal(err)
		}
		h.busy["b4d_u_v4"] = true
		clearDSCPFor(off, backendIPTables)
		if _, ok := h.sets["b4d_u_v4"]; !ok || len(h.sets) != 1 {
			t.Fatalf("only the busy ipset should be left: %v", h.snapshot())
		}
		if s := dscpStale.Load(); s == nil || !slices.Equal(s.ipsets(), []string{"b4d_u_v4"}) {
			t.Fatalf("the busy ipset must stay recorded for a retry: %+v", s)
		}
		if ensureDSCPLocked(off, false) {
			t.Errorf("the monitor reported a retry that could not finish as done")
		}
		delete(h.busy, "b4d_u_v4")
		if !ensureDSCPLocked(off, false) {
			t.Errorf("the monitor did not finish the retried destroy")
		}
		if len(h.sets) != 0 || dscpStale.Load() != nil {
			t.Errorf("after the retry: ipsets %v, stale %+v", h.snapshot(), dscpStale.Load())
		}
	})

	t.Run("a leftover the live stamp took over is not retried", func(t *testing.T) {
		h := dscpIptNewHost(t, backendIPTables)
		cfg := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"))
		if err := applyDSCPFor(cfg, backendIPTables); err != nil {
			t.Fatal(err)
		}
		dscpStale.Store(&dscpState{cfg: cfg, backend: backendIPTablesLegacy, ipt: &dscpIptState{pending: []string{"b4d_u_v4", "b4d_gone_v4"}}})
		if !ensureDSCPLocked(cfg, false) {
			t.Errorf("the monitor did not finish the leftover removal")
		}
		if s := dscpStale.Load(); s != nil {
			t.Errorf("an ipset the live stamp uses stays queued for a destroy that cannot succeed: %+v", s)
		}
		if _, ok := h.sets["b4d_u_v4"]; !ok {
			t.Errorf("the leftover removal destroyed an ipset the live stamp uses")
		}
		h.events = nil
		if ensureDSCPLocked(cfg, false) || len(h.ipsetWrites()) != 0 {
			t.Errorf("the monitor kept retrying: %q", h.ipsetWrites())
		}
	})

	t.Run("the global stamp alone runs no ipset command", func(t *testing.T) {
		h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
		if err := applyDSCPFor(dscpIptTestConfig(7, true, nil), backendIPTables); err != nil {
			t.Fatal(err)
		}
		h.events = nil
		clearDSCPFor(off, backendIPTables)
		for _, e := range h.events {
			if strings.HasPrefix(e, "ipset") {
				t.Errorf("a teardown of the global stamp ran %s", e)
			}
		}
	})
}

func TestRoutingClearAllSweepsB4dDestroyOnly(t *testing.T) {
	resetDSCPState(t)
	dscpIptResetRecords(t)
	stubBinaryPresence(t, map[string]bool{
		backendIPTables: true, backendIP6Tables: false, backendIPTablesLegacy: false, backendIP6TablesLegacy: false,
		"ipset": true, "nft": false, "ip": false,
	})
	routeMu.Lock()
	origEngine, origCache := routeEngine, routeRuleCache
	routeEngine = nil
	routeMu.Unlock()
	t.Cleanup(func() {
		routeMu.Lock()
		routeEngine, routeRuleCache = origEngine, origCache
		routeMu.Unlock()
	})
	origRun := run
	t.Cleanup(func() { run = origRun })
	var calls []string
	run = func(args ...string) (string, error) {
		cmd := strings.Join(args, " ")
		calls = append(calls, cmd)
		switch cmd {
		case "ipset list -n":
			return "b4r_old_v4\nb4d_u_v4\nb4d_s31_v4\nb4d_l_a_d39_v6\nforeign\n", nil
		case "ipset destroy b4d_s31_v4":
			return "ipset v7.19: Set cannot be destroyed: it is in use by a kernel component", errors.New("exit status 1")
		}
		return "", nil
	}
	dscpIptRecordMu.Lock()
	dscpIptRecord["b4d_u_v4"] = map[string]bool{"10.0.0.0/8": true}
	dscpIptRecord["b4d_s31_v4"] = map[string]bool{"10.0.0.0/8": true}
	dscpIptRecordMu.Unlock()

	RoutingClearAll()

	for _, name := range []string{"b4d_u_v4", "b4d_s31_v4", "b4d_l_a_d39_v6"} {
		if !slices.Contains(calls, "ipset destroy "+name) {
			t.Errorf("the sweep did not destroy %s: %q", name, calls)
		}
		if slices.Contains(calls, "ipset flush "+name) {
			t.Errorf("the sweep flushed %s, which empties a set a live rule may still use", name)
		}
	}
	for _, want := range []string{"ipset flush b4r_old_v4", "ipset destroy b4r_old_v4"} {
		if !slices.Contains(calls, want) {
			t.Errorf("the routing sweep no longer runs %q", want)
		}
	}
	for _, c := range calls {
		if strings.Contains(c, "foreign") {
			t.Errorf("the sweep touched an ipset b4 does not own: %s", c)
		}
	}
	dscpWarned.Range(func(k, _ any) bool {
		if strings.Contains(k.(string), "b4d_s31_v4") {
			t.Errorf("an ipset still in use was reported as a failure: %s", k)
		}
		return true
	})
	dscpIptRecordMu.Lock()
	_, lingering := dscpIptRecord["b4d_u_v4"]
	_, kept := dscpIptRecord["b4d_s31_v4"]
	dscpIptRecordMu.Unlock()
	if lingering || !kept {
		t.Errorf("the record must forget the destroyed ipset and keep the one still in use: forgotten %v, kept %v", !lingering, kept)
	}
}

func TestSetDSCPBusyProbeWithTheGlobalOffIsRetried(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	cfg := dscpIptTestConfig(7, false, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"))
	h.probeErr[backendIPTables] = errors.New("iptables rejected the set match, which needs the xt_set kernel module and the iptables set extension (could not create probe chain B4_MODULE_TEST: command [iptables -w -t mangle -N B4_MODULE_TEST] failed: exit status 4 (Another app is currently holding the xtables lock. Stopped waiting after 1s.))")
	if err := applyDSCPFor(cfg, backendIPTables); err == nil || !strings.Contains(err.Error(), "the firewall monitor tries again") {
		t.Fatalf("a busy firewall during the probe must be reported as retried, got %v", err)
	}
	if st := dscpApplied.Load(); st == nil || !st.pending {
		t.Fatalf("with the global stamp off, a busy probe left nothing for the monitor to retry: %+v", st)
	}
	if _, ok := h.mangle.chains[backendIPTables][dscpChainName]; ok {
		t.Errorf("a chain without a stamp was installed while the probe was undecided: %q", h.chain(backendIPTables))
	}

	delete(h.probeErr, backendIPTables)
	if !ensureDSCPLocked(cfg, false) {
		t.Fatalf("the monitor did not retry the per-set rules")
	}
	if st := dscpApplied.Load(); st == nil || st.pending {
		t.Fatalf("the retry left the per-set rules pending: %+v", st)
	}
	if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptRender(dscpPlanFor(cfg), false, true)); !slices.Equal(got, want) {
		t.Errorf("chain after the retry = %q, want %q", got, want)
	}
	if ensureDSCPLocked(cfg, false) {
		t.Errorf("the monitor kept restoring per-set rules that were back in place")
	}
}
