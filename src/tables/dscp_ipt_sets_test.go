package tables

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type dscpIptHost struct {
	mangle      *fakeMangle
	sets        map[string]map[string]bool
	events      []string
	busy        map[string]bool
	refuseSets  map[string]bool
	probeErr    map[string]error
	probes      map[string]int
	failRestore bool
	refuseAdds  map[string]bool
}

func dscpIptNewHost(t *testing.T, bins ...string) *dscpIptHost {
	t.Helper()
	h := &dscpIptHost{
		mangle:     newFakeMangle(bins...),
		sets:       map[string]map[string]bool{},
		busy:       map[string]bool{},
		refuseSets: map[string]bool{},
		probeErr:   map[string]error{},
		probes:     map[string]int{},
	}
	installFakeMangle(t, h.mangle)
	stubBinaryPresence(t, map[string]bool{"ipset": true})
	if err := os.MkdirAll(filepath.Join(kmodSysRoot, "xt_set"), 0o755); err != nil {
		t.Fatal(err)
	}
	origRun, origStdin, origProbe, origNow := run, runStdin, ipsetMatchProbe, dscpIptNow
	run, runStdin = h.run, h.runStdin
	ipsetMatchProbe = func(_ *IPTablesManager, bin string) error {
		h.probes[bin]++
		return h.probeErr[bin]
	}
	t.Cleanup(func() { run, runStdin, ipsetMatchProbe, dscpIptNow = origRun, origStdin, origProbe, origNow })
	dscpIptResetRecords(t)
	return h
}

func dscpIptResetRecords(t *testing.T) {
	t.Helper()
	reset := func() {
		dscpIptProbeMu.Lock()
		clear(dscpIptProbes)
		dscpIptProbeMu.Unlock()
		dscpIptRecordMu.Lock()
		clear(dscpIptRecord)
		dscpIptRecordMu.Unlock()
	}
	reset()
	t.Cleanup(reset)
}

func (h *dscpIptHost) run(args ...string) (string, error) {
	h.events = append(h.events, strings.Join(args, " "))
	if len(args) > 0 && args[0] == "ipset" {
		return h.ipset(args[1:])
	}
	if len(args) > 5 && slices.Contains([]string{"-A", "-I", "-C"}, args[4]) {
		for i := 5; i+1 < len(args); i++ {
			if args[i] != "--match-set" {
				continue
			}
			if h.refuseSets[args[0]] {
				return "iptables v1.8.11 (nf_tables): Couldn't load match `set':No such file or directory", errors.New("exit status 2")
			}
			if h.sets[args[i+1]] == nil {
				return fmt.Sprintf("iptables v1.8.11 (nf_tables): Set %s doesn't exist.", args[i+1]), errors.New("exit status 2")
			}
		}
	}
	return h.mangle.run(args...)
}

func (h *dscpIptHost) ipset(args []string) (string, error) {
	missing := func() (string, error) {
		return "ipset v7.19: The set with the given name does not exist", errors.New("exit status 1")
	}
	if len(args) < 2 {
		return "", errors.New("exit status 1")
	}
	name := args[1]
	entries, exists := h.sets[name]
	switch args[0] {
	case "create":
		if exists && !slices.Contains(args, "-exist") {
			return "ipset v7.19: Set cannot be created: set with the same name already exists", errors.New("exit status 1")
		}
		if !exists {
			h.sets[name] = map[string]bool{}
		}
	case "flush":
		if !exists {
			return missing()
		}
		clear(entries)
	case "destroy":
		if !exists {
			return missing()
		}
		if h.busy[name] || h.referenced(name) {
			return "ipset v7.19: Set cannot be destroyed: it is in use by a kernel component", errors.New("exit status 1")
		}
		delete(h.sets, name)
	case "add", "del":
		if !exists || len(args) < 3 {
			return missing()
		}
		if args[0] == "add" {
			if h.refuseAdds[args[2]] {
				return "ipset v7.19: Hash is full, cannot add more elements", errors.New("exit status 1")
			}
			entries[args[2]] = true
		} else {
			delete(entries, args[2])
		}
	default:
		return "", errors.New("unsupported ipset command " + args[0])
	}
	return "", nil
}

func (h *dscpIptHost) runStdin(stdin string, args ...string) error {
	h.events = append(h.events, strings.Join(args, " ")+": "+strings.ReplaceAll(strings.TrimSpace(stdin), "\n", "; "))
	if strings.Join(args, " ") != "ipset restore -exist" {
		return errors.New("unexpected command")
	}
	if h.failRestore {
		return errors.New("command [ipset restore -exist] failed: exit status 1 (ipset v7.19: Error in line 1: Kernel error received: Cannot allocate memory)")
	}
	lines := strings.Split(strings.TrimSpace(stdin), "\n")
	for i, line := range lines {
		f := strings.Fields(line)
		if len(f) != 3 || (f[0] != "add" && f[0] != "del") || h.sets[f[1]] == nil {
			return fmt.Errorf("command [ipset restore -exist] failed: exit status 1 (ipset v7.19: Error in line %d: The set with the given name does not exist)", i+1)
		}
	}
	for _, line := range lines {
		f := strings.Fields(line)
		if f[0] == "add" {
			h.sets[f[1]][f[2]] = true
		} else {
			delete(h.sets[f[1]], f[2])
		}
	}
	return nil
}

func (h *dscpIptHost) referenced(name string) bool {
	for _, chains := range h.mangle.chains {
		for _, rules := range chains {
			for _, rule := range rules {
				if strings.Contains(rule+" ", " --match-set "+name+" ") {
					return true
				}
			}
		}
	}
	return false
}

func (h *dscpIptHost) chain(bin string) []string {
	return h.mangle.chains[bin][dscpChainName]
}

func (h *dscpIptHost) entries(name string) []string {
	return slices.Sorted(maps.Keys(h.sets[name]))
}

func (h *dscpIptHost) snapshot() map[string][]string {
	out := map[string][]string{}
	for name := range h.sets {
		out[name] = h.entries(name)
	}
	return out
}

func (h *dscpIptHost) first(part string) int {
	return slices.IndexFunc(h.events, func(e string) bool { return strings.Contains(e, part) })
}

func (h *dscpIptHost) last(part string) int {
	for i := len(h.events) - 1; i >= 0; i-- {
		if strings.Contains(h.events[i], part) {
			return i
		}
	}
	return -1
}

func (h *dscpIptHost) ipsetWrites() []string {
	var out []string
	for _, e := range h.events {
		for _, op := range []string{"ipset restore", "ipset flush", "ipset destroy", "ipset add", "ipset del"} {
			if strings.HasPrefix(e, op) {
				out = append(out, e)
			}
		}
	}
	return out
}

func dscpIptCanon(specs [][]string) []string {
	out := make([]string, len(specs))
	for i, spec := range specs {
		out[i] = fakeMangleCanon(spec)
	}
	return out
}

func dscpIptJoined(specs [][]string) []string {
	out := make([]string, len(specs))
	for i, spec := range specs {
		out[i] = strings.Join(spec, " ")
	}
	return out
}

func dscpIptTestConfig(global int, globalOn bool, ifaces []string, sets ...*config.SetConfig) *config.Config {
	cfg := dscpPlanTestConfig(sets...)
	cfg.System.Tables.DSCP = config.DSCPConfig{Enabled: globalOn, Value: global, Interfaces: ifaces}
	return cfg
}

func dscpIptApply(t *testing.T, cfg *config.Config, prev *dscpIptState) *dscpIptState {
	t.Helper()
	st, err := dscpIptApplyPlan(cfg, backendIPTables, dscpPlanFor(cfg), prev)
	if err != nil {
		t.Fatalf("dscpIptApplyPlan: %v", err)
	}
	return st
}

func dscpIptWarnings(part string) []string {
	var out []string
	dscpWarned.Range(func(k, _ any) bool {
		if msg := k.(string); strings.Contains(msg, part) {
			out = append(out, msg)
		}
		return true
	})
	return out
}

var dscpIptBase = []string{
	"-o lo -j RETURN",
	"-m mark --mark 0x20000000/0x20000000 -j RETURN",
	"-m conntrack --ctdir REPLY -j RETURN",
}

func TestSetDSCPIptRender(t *testing.T) {
	c := dscpPlanTestSet("c", 25, "172.16.0.0/12")
	c.Targets.DomainOnly = true
	plan := dscpPlanFor(dscpIptTestConfig(7, true, nil,
		dscpPlanTestSet("a", 31, "10.0.0.0/8", "10.1.2.0/24", "fd00::/16"),
		dscpPlanTestSet("b", 6, "10.1.0.0/16"),
		c,
	))
	la4, lb4 := dscpIptLearnedSet(routeSanitizeSetID("a"), false), dscpIptLearnedSet(routeSanitizeSetID("b"), false)
	la6, lb6 := dscpIptLearnedSet(routeSanitizeSetID("a"), true), dscpIptLearnedSet(routeSanitizeSetID("b"), true)

	want4 := append(slices.Clone(dscpIptBase),
		"-j DSCP --set-dscp 7",
		"-m set ! --match-set b4d_u_v4 dst -m set ! --match-set b4d_ul_v4 dst -j RETURN",
		"-m set --match-set b4d_s6_v4 dst -j DSCP --set-dscp 6",
		"-m set --match-set b4d_s25_v4 dst -j DSCP --set-dscp 25",
		"-m set --match-set b4d_s31_v4 dst -j DSCP --set-dscp 31",
		"-m set --match-set "+lb4+" dst -j DSCP --set-dscp 6",
		"-m set --match-set "+la4+" dst -j DSCP --set-dscp 31",
	)
	if got := dscpIptJoined(dscpIptRender(plan, false, true)); !slices.Equal(got, want4) {
		t.Errorf("IPv4 chain:\n got %q\nwant %q", got, want4)
	}
	want6 := append(slices.Clone(dscpIptBase),
		"-j DSCP --set-dscp 7",
		"-m set ! --match-set b4d_u_v6 dst -m set ! --match-set b4d_ul_v6 dst -j RETURN",
		"-m set --match-set b4d_s31_v6 dst -j DSCP --set-dscp 31",
		"-m set --match-set "+lb6+" dst -j DSCP --set-dscp 6",
		"-m set --match-set "+la6+" dst -j DSCP --set-dscp 31",
	)
	if got := dscpIptJoined(dscpIptRender(plan, true, true)); !slices.Equal(got, want6) {
		t.Errorf("IPv6 chain:\n got %q\nwant %q", got, want6)
	}
	if got := dscpIptRender(plan, false, false); !reflect.DeepEqual(got, dscpIptSpecs(7, nil)) {
		t.Errorf("a binary without ipset support must get exactly the global chain, got %q", dscpIptJoined(got))
	}

	type want struct {
		name    string
		learned bool
		keys    []string
	}
	wantSets := []want{
		{"b4d_u_v4", false, []string{"10.0.0.0/8", "172.16.0.0/12"}},
		{"b4d_ul_v4", true, nil},
		{"b4d_s6_v4", false, []string{"10.1.0.0/23", "10.1.3.0/24", "10.1.4.0/22", "10.1.8.0/21", "10.1.16.0/20", "10.1.32.0/19", "10.1.64.0/18", "10.1.128.0/17"}},
		{"b4d_s25_v4", false, []string{"172.16.0.0/12"}},
		{"b4d_s31_v4", false, []string{"10.0.0.0/16", "10.1.2.0/24", "10.2.0.0/15", "10.4.0.0/14", "10.8.0.0/13", "10.16.0.0/12", "10.32.0.0/11", "10.64.0.0/10", "10.128.0.0/9"}},
		{la4, true, nil},
		{lb4, true, nil},
	}
	var gotSets []want
	for _, w := range dscpIptWanted(plan, false) {
		if w.v6 {
			t.Errorf("%s is an IPv4 set but is marked IPv6", w.name)
		}
		gotSets = append(gotSets, want{w.name, w.learned, w.keys})
	}
	if !reflect.DeepEqual(gotSets, wantSets) {
		t.Errorf("IPv4 ipsets:\n got %v\nwant %v", gotSets, wantSets)
	}
	wantSets6 := []want{
		{"b4d_u_v6", false, []string{"fd00::/16"}},
		{"b4d_ul_v6", true, nil},
		{"b4d_s31_v6", false, []string{"fd00::/16"}},
		{la6, true, nil},
		{lb6, true, nil},
	}
	var gotSets6 []want
	for _, w := range dscpIptWanted(plan, true) {
		if !w.v6 {
			t.Errorf("%s is an IPv6 set but is marked IPv4", w.name)
		}
		gotSets6 = append(gotSets6, want{w.name, w.learned, w.keys})
	}
	if !reflect.DeepEqual(gotSets6, wantSets6) {
		t.Errorf("IPv6 ipsets:\n got %v\nwant %v", gotSets6, wantSets6)
	}

	scoped := dscpPlanFor(dscpIptTestConfig(7, false, []string{"eth0", "wg0"},
		dscpPlanTestSet("a", 31, "10.0.0.0/8"),
		dscpPlanTestSet("b", 6, "10.1.0.0/16"),
	))
	wantScoped := append(slices.Clone(dscpIptBase),
		"-m set ! --match-set b4d_u_v4 dst -m set ! --match-set b4d_ul_v4 dst -j RETURN",
		"-o eth0 -m set --match-set b4d_s6_v4 dst -j DSCP --set-dscp 6",
		"-o eth0 -m set --match-set b4d_s31_v4 dst -j DSCP --set-dscp 31",
		"-o wg0 -m set --match-set b4d_s6_v4 dst -j DSCP --set-dscp 6",
		"-o wg0 -m set --match-set b4d_s31_v4 dst -j DSCP --set-dscp 31",
		"-o eth0 -m set --match-set "+lb4+" dst -j DSCP --set-dscp 6",
		"-o eth0 -m set --match-set "+la4+" dst -j DSCP --set-dscp 31",
		"-o wg0 -m set --match-set "+lb4+" dst -j DSCP --set-dscp 6",
		"-o wg0 -m set --match-set "+la4+" dst -j DSCP --set-dscp 31",
	)
	if got := dscpIptJoined(dscpIptRender(scoped, false, true)); !slices.Equal(got, wantScoped) {
		t.Errorf("scoped chain with the global stamp off:\n got %q\nwant %q", got, wantScoped)
	}
	if got := dscpIptRender(scoped, false, false); dscpIptStamps(got) || len(got) != dscpReturnRules {
		t.Errorf("with the global stamp off a binary without ipset support has nothing to stamp, got %q", dscpIptJoined(got))
	}

	staticOnly := dscpPlanTestSet("d", 12, "10.5.0.0/16")
	staticOnly.Targets.DomainOnly = true
	v4only := dscpPlanFor(dscpIptTestConfig(7, true, nil, staticOnly))
	if got := dscpIptRender(v4only, true, true); !reflect.DeepEqual(got, dscpIptSpecs(7, nil)) {
		t.Errorf("a family with no static range and no learning set must not get a guard, got %q", dscpIptJoined(got))
	}
	if w := dscpIptWanted(v4only, true); w != nil {
		t.Errorf("a family with nothing to stamp wants no ipset, got %v", w)
	}

	long := dscpIptLearnedSet(routeSanitizeSetID(strings.Repeat("streaming-video-", 8)), true)
	if len(long) > 31 {
		t.Errorf("%s is %d characters, ipset names stop at 31", long, len(long))
	}
	if !dscpPlanFor(dscpIptTestConfig(7, true, nil)).empty() || dscpIptWanted(dscpPlanFor(dscpIptTestConfig(7, true, nil)), false) != nil {
		t.Errorf("a config without set DSCP must not want any ipset")
	}
}

func TestSetDSCPIptShape(t *testing.T) {
	listing := func(rules ...string) string {
		var b strings.Builder
		b.WriteString("-N B4_DSCP\n")
		for _, r := range rules {
			fmt.Fprintf(&b, "-A B4_DSCP %s\n", r)
		}
		return b.String()
	}
	lo, client, reply := dscpIptBase[0], dscpIptBase[1], dscpIptBase[2]
	global := "-j DSCP --set-dscp 0x07"
	guard := "-m set ! --match-set b4d_u_v4 dst -m set ! --match-set b4d_ul_v4 dst -j RETURN"
	s31 := "-m set --match-set b4d_s31_v4 dst -j DSCP --set-dscp 0x1f"
	learned := "-m set --match-set b4d_l_a_v4 dst -j DSCP --set-dscp 0x1f"
	full := dscpIptShape{globals: 1, guard: true, sets: 2}
	cases := []struct {
		name    string
		listing string
		shape   dscpIptShape
		want    bool
	}{
		{"iptables -S of the rendered chain", listing(lo, client, reply, global, guard, s31, learned), full, true},
		{"per-set only", listing(lo, client, reply, guard, s31, learned), dscpIptShape{guard: true, sets: 2}, true},
		{"interfaces", listing(lo, client, reply, "-o eth0 "+global, "-o wg0 "+global, guard, "-o eth0 "+s31, "-o wg0 "+s31), dscpIptShape{globals: 2, guard: true, sets: 2}, true},
		{"global only", listing(lo, client, reply, global), dscpIptShape{globals: 1}, true},
		{"a warning line is ignored", listing(lo, client, reply, global, guard, s31, learned) + "# Warning: iptables-legacy tables present, use iptables-legacy to see them\n", full, true},
		{"missing guard", listing(lo, client, reply, global, s31, learned), full, false},
		{"set stamps without a guard", listing(lo, client, reply, global, s31, learned), dscpIptShape{globals: 1, sets: 2}, false},
		{"a guard where none belongs", listing(lo, client, reply, global, guard), dscpIptShape{globals: 1}, false},
		{"two guards", listing(lo, client, reply, global, guard, guard, s31, learned), full, false},
		{"a return after the set stamps", listing(lo, client, reply, global, guard, s31, reply, learned), full, false},
		{"the guard after a set stamp", listing(lo, client, reply, global, s31, guard, learned), full, false},
		{"a global stamp after the guard", listing(lo, client, reply, guard, global, s31, learned), full, false},
		{"a set stamp missing", listing(lo, client, reply, global, guard, s31), full, false},
		{"a set stamp twice", listing(lo, client, reply, global, guard, s31, learned, learned), full, false},
		{"the global stamp missing", listing(lo, client, reply, guard, s31, learned), full, false},
		{"client mark return missing", listing(lo, reply, global, guard, s31, learned), full, false},
		{"a foreign target", listing(lo, client, reply, global, guard, "-j ACCEPT", s31, learned), full, false},
		{"a foreign set", listing(lo, client, reply, global, guard, "-m set --match-set other dst -j DSCP --set-dscp 0x1f", learned), full, false},
		{"flushed", listing(), full, false},
	}
	for _, tc := range cases {
		if got := dscpIptPlanChainShape(tc.listing, tc.shape); got != tc.want {
			t.Errorf("%s: shape=%v, want %v", tc.name, got, tc.want)
		}
	}

	plan := dscpPlanFor(dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.1.0.0/16")))
	if got := dscpIptShapeOf(dscpIptRender(plan, false, true)); got != full {
		t.Errorf("shape of the rendered chain = %+v, want %+v", got, full)
	}

	h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
	cfg := dscpIptTestConfig(7, true, []string{"eth0"}, dscpPlanTestSet("a", 31, "10.0.0.0/8", "fd00::/16"))
	st := dscpIptApply(t, cfg, nil)
	for _, bin := range []string{backendIPTables, backendIP6Tables} {
		if !dscpIptPlanIntact(bin, st.chains[bin]) {
			t.Errorf("%s: the listing of the installed chain fails its own shape:\n%s", bin, strings.Join(h.chain(bin), "\n"))
		}
	}
	h.mangle.chains[backendIPTables]["POSTROUTING"] = []string{"-j B4", "-j B4_DSCP"}
	if dscpIptPlanIntact(backendIPTables, st.chains[backendIPTables]) {
		t.Errorf("a stamp jump below the capture jump reads as intact")
	}
}

func TestSetDSCPIptReplaceOrder(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	global := dscpIptTestConfig(7, true, nil)
	st := dscpIptApply(t, global, nil)
	if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptSpecs(7, nil)); !slices.Equal(got, want) {
		t.Fatalf("global chain = %q, want %q", got, want)
	}
	la := dscpIptLearnedSet(routeSanitizeSetID("a"), false)
	insert := func(n int, spec string) string {
		return fmt.Sprintf("iptables -w -t mangle -I B4_DSCP %d %s", n, spec)
	}
	del := func(n int) string { return fmt.Sprintf("iptables -w -t mangle -D B4_DSCP %d", n) }
	steps := []struct {
		name  string
		cfg   *config.Config
		specs []string
		dels  []string
	}{
		{
			"global to per-set",
			dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.1.2.0/24")),
			append(slices.Clone(dscpIptBase), "-j DSCP --set-dscp 7",
				"-m set ! --match-set b4d_u_v4 dst -m set ! --match-set b4d_ul_v4 dst -j RETURN",
				"-m set --match-set b4d_s31_v4 dst -j DSCP --set-dscp 31",
				"-m set --match-set "+la+" dst -j DSCP --set-dscp 31"),
			[]string{del(11), del(8), del(8), del(8)},
		},
		{
			"value change",
			dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 6, "10.1.2.0/24")),
			append(slices.Clone(dscpIptBase), "-j DSCP --set-dscp 7",
				"-m set ! --match-set b4d_u_v4 dst -m set ! --match-set b4d_ul_v4 dst -j RETURN",
				"-m set --match-set b4d_s6_v4 dst -j DSCP --set-dscp 6",
				"-m set --match-set "+la+" dst -j DSCP --set-dscp 6"),
			[]string{del(11), del(11), del(11), del(11), del(8), del(8), del(8)},
		},
		{
			"per-set to global",
			global,
			append(slices.Clone(dscpIptBase), "-j DSCP --set-dscp 7"),
			[]string{del(8), del(8), del(8), del(8), del(5), del(5), del(5)},
		},
	}
	for _, step := range steps {
		h.mangle.calls = nil
		st = dscpIptApply(t, step.cfg, st)
		var want []string
		for i, spec := range step.specs {
			want = append(want, insert(i+1, spec))
		}
		want = append(want, step.dels...)
		if got := h.mangle.mutations(); !slices.Equal(got, want) {
			t.Errorf("%s: commands\n got %q\nwant %q", step.name, got, want)
		}
		if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptRender(dscpPlanFor(step.cfg), false, true)); !slices.Equal(got, want) {
			t.Errorf("%s: chain after the replace = %q, want %q", step.name, got, want)
		}
		if got := h.chain(backendIPTables); len(got) != len(step.specs) {
			t.Errorf("%s: %d rules left, want %d", step.name, len(got), len(step.specs))
		}
	}
	if len(st.sets) != 0 || len(st.pending) != 0 {
		t.Errorf("back on the global stamp alone the state still holds ipsets: sets %v, pending %v", st.sets, st.pending)
	}
	for name := range h.sets {
		t.Errorf("ipset %s survived the return to the global stamp alone", name)
	}
	if got := h.mangle.chains[backendIPTables]["POSTROUTING"]; !slices.Equal(got, []string{"-j B4_DSCP"}) {
		t.Errorf("POSTROUTING = %v, want exactly one jump", got)
	}
}

func TestSetDSCPIptReplaceUnrecordedChain(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	replaced := func(specs [][]string, old int) []string {
		var out []string
		for i, spec := range specs {
			out = append(out, fmt.Sprintf("iptables -w -t mangle -I B4_DSCP %d %s", i+1, strings.Join(spec, " ")))
		}
		n := len(specs)
		for range old - dscpReturnRules {
			out = append(out, fmt.Sprintf("iptables -w -t mangle -D B4_DSCP %d", n+dscpReturnRules+1))
		}
		for range dscpReturnRules {
			out = append(out, fmt.Sprintf("iptables -w -t mangle -D B4_DSCP %d", n+1))
		}
		return out
	}
	if err := applyDSCPIpt(dscpIptTestConfig(7, true, nil), backendIPTables, 7, nil); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		name string
		cfg  *config.Config
		old  int
	}{
		{"per-set over today's global chain", dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.1.2.0/24")), len(dscpIptSpecs(7, nil))},
		{"a value change over a per-set chain", dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 6, "10.1.2.0/24")), 7},
		{"today's global chain over a per-set chain", dscpIptTestConfig(7, true, nil), 7},
	}
	for _, step := range steps {
		h.mangle.calls = nil
		st := dscpIptApply(t, step.cfg, nil)
		specs := dscpIptRender(dscpPlanFor(step.cfg), false, true)
		if got, want := h.mangle.mutations(), replaced(specs, step.old); !slices.Equal(got, want) {
			t.Errorf("%s with no record of it:\n got %q\nwant %q", step.name, got, want)
		}
		if got, want := h.chain(backendIPTables), dscpIptCanon(specs); !slices.Equal(got, want) {
			t.Errorf("%s: chain = %q, want %q", step.name, got, want)
		}
		if !reflect.DeepEqual(st.chains[backendIPTables], specs) {
			t.Errorf("%s: recorded %q, want %q", step.name, dscpIptJoined(st.chains[backendIPTables]), dscpIptJoined(specs))
		}
	}
}

func TestSetDSCPIptKeepWhenEqual(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
	build := func() *config.Config {
		return dscpIptTestConfig(7, true, []string{"eth0"},
			dscpPlanTestSet("a", 31, "10.0.0.0/8", "fd00::/16"),
			dscpPlanTestSet("b", 6, "10.1.0.0/16"),
		)
	}
	st := dscpIptApply(t, build(), nil)
	before := h.snapshot()
	chains := map[string][]string{backendIPTables: h.chain(backendIPTables), backendIP6Tables: h.chain(backendIP6Tables)}

	for round := range 2 {
		h.mangle.calls, h.events = nil, nil
		next := dscpIptApply(t, build(), st)
		if m := h.mangle.mutations(); len(m) != 0 {
			t.Errorf("round %d: an unchanged plan rewrote the chain: %v", round, m)
		}
		if w := h.ipsetWrites(); len(w) != 0 {
			t.Errorf("round %d: an unchanged plan wrote to the ipsets: %v", round, w)
		}
		if !reflect.DeepEqual(h.snapshot(), before) {
			t.Errorf("round %d: ipset contents changed:\n got %v\nwant %v", round, h.snapshot(), before)
		}
		checked := 0
		for _, e := range h.events {
			if strings.Contains(e, " -C "+dscpChainName+" ") {
				checked++
			}
		}
		if want := len(st.chains[backendIPTables]) + len(st.chains[backendIP6Tables]); checked != want {
			t.Errorf("round %d: %d rules checked with -C, want %d", round, checked, want)
		}
		for bin, want := range chains {
			if got := h.chain(bin); !slices.Equal(got, want) {
				t.Errorf("round %d: %s chain = %q, want %q", round, bin, got, want)
			}
		}
		if !reflect.DeepEqual(next.chains, st.chains) || !slices.Equal(next.sets, st.sets) || !slices.Equal(next.bins, st.bins) || next.retry {
			t.Errorf("round %d: state moved without a change: %+v vs %+v", round, next, st)
		}
		st = next
	}
}

func TestSetDSCPIptFlushOnUnknownChain(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	cfg := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"))
	st := dscpIptApply(t, cfg, nil)
	refill := func(specs [][]string) []string {
		out := []string{"iptables -w -t mangle -F B4_DSCP"}
		for _, spec := range specs {
			out = append(out, "iptables -w -t mangle -A B4_DSCP "+strings.Join(spec, " "))
		}
		return out
	}
	specs := st.chains[backendIPTables]

	h.mangle.chains[backendIPTables][dscpChainName] = append(h.chain(backendIPTables), "-j ACCEPT")
	h.mangle.calls = nil
	dscpIptApply(t, cfg, nil)
	if got, want := h.mangle.mutations(), refill(specs); !slices.Equal(got, want) {
		t.Errorf("with a foreign rule and no record of the installed rules:\n got %q\nwant %q", got, want)
	}

	h.mangle.chains[backendIPTables][dscpChainName] = append(h.chain(backendIPTables), "-j ACCEPT")
	h.mangle.calls = nil
	st = dscpIptApply(t, cfg, st)
	if got, want := h.mangle.mutations(), refill(specs); !slices.Equal(got, want) {
		t.Errorf("with a foreign rule in the chain:\n got %q\nwant %q", got, want)
	}
	if got, want := h.chain(backendIPTables), dscpIptCanon(specs); !slices.Equal(got, want) {
		t.Errorf("chain after the refill = %q, want %q", got, want)
	}

	changed := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 6, "10.0.0.0/8"))
	live := h.chain(backendIPTables)
	h.mangle.chains[backendIPTables][dscpChainName] = append(live[:1:1], live[2:]...)
	h.mangle.calls = nil
	st = dscpIptApply(t, changed, st)
	newSpecs := dscpIptRender(dscpPlanFor(changed), false, true)
	if got, want := h.mangle.mutations(), refill(newSpecs); !slices.Equal(got, want) {
		t.Errorf("with a rule missing from the old chain, positional deletes would cut the wrong rules:\n got %q\nwant %q", got, want)
	}

	realRun := run
	failed := false
	run = func(args ...string) (string, error) {
		if !failed && len(args) > 5 && args[4] == "-D" && args[5] == dscpChainName {
			failed = true
			return "iptables: Index of deletion too big.", errors.New("exit status 1")
		}
		return realRun(args...)
	}
	t.Cleanup(func() { run = realRun })
	half, err := dscpIptApplyPlan(cfg, backendIPTables, dscpPlanFor(cfg), st)
	if err == nil || !strings.Contains(err.Error(), "the firewall monitor tries again") {
		t.Fatalf("a failed delete must be reported as retried, got %v", err)
	}
	if _, known := half.chains[backendIPTables]; known || !half.retry || !slices.Equal(half.bins, []string{backendIPTables}) {
		t.Fatalf("a replace that stopped half way must leave the binary wanted with its rules unknown: %+v", half)
	}
	h.mangle.calls = nil
	dscpIptApply(t, cfg, half)
	if got, want := h.mangle.mutations(), refill(specs); !slices.Equal(got, want) {
		t.Errorf("the retry after a half-done replace:\n got %q\nwant %q", got, want)
	}
	if got, want := h.chain(backendIPTables), dscpIptCanon(specs); !slices.Equal(got, want) {
		t.Errorf("chain after the retry = %q, want %q", got, want)
	}
}

func TestSetDSCPIptReconcileAddBeforeDelete(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	h.sets["b4d_s31_v4"] = map[string]bool{"192.0.2.0/24": true}
	first := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8", "192.168.50.7/32"))
	st := dscpIptApply(t, first, nil)
	if h.first("ipset flush b4d_s31_v4") < 0 {
		t.Errorf("an ipset an earlier run left behind was not emptied before use: %v", h.events)
	}
	if got, want := h.entries("b4d_s31_v4"), []string{"10.0.0.0/8", "192.168.50.7"}; !slices.Equal(got, want) {
		t.Errorf("b4d_s31_v4 = %v, want %v", got, want)
	}
	if got, want := h.entries("b4d_u_v4"), []string{"10.0.0.0/8", "192.168.50.7"}; !slices.Equal(got, want) {
		t.Errorf("b4d_u_v4 = %v, want %v", got, want)
	}
	for _, create := range []string{
		"ipset create b4d_s31_v4 hash:net family inet maxelem 262144",
		"ipset create b4d_u_v4 hash:net family inet maxelem 262144",
		"ipset create b4d_ul_v4 hash:net family inet timeout 3600",
		"ipset create " + dscpIptLearnedSet(routeSanitizeSetID("a"), false) + " hash:net family inet timeout 3600",
	} {
		if h.first(create) < 0 {
			t.Errorf("missing %q in %v", create, h.events)
		}
	}

	second := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 6, "10.1.0.0/16", "192.168.50.7"))
	h.events = nil
	st = dscpIptApply(t, second, st)
	firstRule, lastRule := h.first(" -I "+dscpChainName+" "), h.last(" -D "+dscpChainName+" ")
	if firstRule < 0 || lastRule < firstRule {
		t.Fatalf("the value change did not replace the chain: %v", h.events)
	}
	for _, add := range []string{"add b4d_s6_v4 10.1.0.0/16", "add b4d_u_v4 10.1.0.0/16"} {
		if i := h.first(add); i < 0 || i > firstRule {
			t.Errorf("%q must be loaded before the rules change (at %d, rules from %d): %v", add, i, firstRule, h.events)
		}
	}
	for _, after := range []string{"del b4d_u_v4 10.0.0.0/8", "ipset destroy b4d_s31_v4"} {
		if i := h.first(after); i < lastRule {
			t.Errorf("%q must run after the rules changed (at %d, rules until %d): %v", after, i, lastRule, h.events)
		}
	}
	if i := h.first("b4d_u_v4 192.168.50.7"); i >= 0 {
		t.Errorf("an unchanged host written once as /32 and once bare was rewritten: %s", h.events[i])
	}
	if got, want := h.entries("b4d_u_v4"), []string{"10.1.0.0/16", "192.168.50.7"}; !slices.Equal(got, want) {
		t.Errorf("b4d_u_v4 = %v, want %v", got, want)
	}
	if got, want := h.entries("b4d_s6_v4"), []string{"10.1.0.0/16", "192.168.50.7"}; !slices.Equal(got, want) {
		t.Errorf("b4d_s6_v4 = %v, want %v", got, want)
	}
	if _, ok := h.sets["b4d_s31_v4"]; ok {
		t.Errorf("the ipset of a value no set uses any more survived")
	}

	h.mangle.chains[backendIPTables][dscpChainName] = nil
	delete(h.sets, "b4d_s6_v4")
	st = dscpIptApply(t, second, st)
	if got, want := h.entries("b4d_s6_v4"), []string{"10.1.0.0/16", "192.168.50.7"}; !slices.Equal(got, want) {
		t.Errorf("an ipset destroyed behind b4's back came back as %v, want %v", got, want)
	}

	h.failRestore = true
	h.events = nil
	third := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 6, "10.1.0.0/16", "192.168.50.7", "10.9.0.0/16"))
	dscpIptApply(t, third, st)
	for _, add := range []string{"ipset add b4d_s6_v4 10.9.0.0/16 -exist", "ipset add b4d_u_v4 10.9.0.0/16 -exist"} {
		if h.first(add) < 0 {
			t.Errorf("a refused restore did not fall back to %q: %v", add, h.events)
		}
	}
	if got, want := h.entries("b4d_s6_v4"), []string{"10.1.0.0/16", "10.9.0.0/16", "192.168.50.7"}; !slices.Equal(got, want) {
		t.Errorf("b4d_s6_v4 after the fallback = %v, want %v", got, want)
	}
}

func TestSetDSCPIptFallbackStopsAfterFailuresInARow(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	h.failRestore = true
	h.refuseAdds = map[string]bool{}
	singles := func(name string) []string {
		var tried []string
		for _, e := range h.events {
			if f := strings.Fields(e); len(f) > 3 && f[0] == "ipset" && f[1] == "add" && f[2] == name {
				tried = append(tried, f[3])
			}
		}
		return tried
	}
	recorded := func(name string) []string {
		dscpIptRecordMu.Lock()
		defer dscpIptRecordMu.Unlock()
		return slices.Sorted(maps.Keys(dscpIptRecord[name]))
	}

	const name = "b4d_s31_v4"
	h.sets[name] = map[string]bool{}
	keys := make([]string, 60)
	var added []string
	for i := range keys {
		keys[i] = fmt.Sprintf("10.%d.0.0/16", i)
		if (i >= 2 && i <= 16) || (i >= 18 && i <= 32) || i >= 34 {
			h.refuseAdds[keys[i]] = true
		} else {
			added = append(added, keys[i])
		}
	}
	slices.Sort(added)
	err := dscpIptAdd(name, keys)
	if err == nil || !strings.Contains(err.Error(), "56 of 60 entries could not be added") {
		t.Fatalf("the refused and the untried entries must count as failed, got %v", err)
	}
	if got := singles(name); !slices.Equal(got, keys[:50]) {
		t.Errorf("15 failures in a row twice and then %d must stop the fallback after entry 50, it tried %d: %v", dscpIptSingleStreak, len(got), got)
	}
	if got := recorded(name); !slices.Equal(got, added) {
		t.Errorf("the entries added one by one before the fallback stopped must be recorded, got %v, want %v", got, added)
	}
	if got := h.entries(name); !slices.Equal(got, added) {
		t.Errorf("%s holds %v, want %v", name, got, added)
	}

	h.failRestore = false
	clear(h.refuseAdds)
	h.events = nil
	if err := dscpIptAdd(name, keys); err != nil {
		t.Fatalf("the next change must add what failed or was never tried: %v", err)
	}
	if len(h.events) != 1 || strings.Count(h.events[0], "add "+name+" ") != len(keys)-len(added) {
		t.Errorf("the retry must restore exactly the %d entries that are not recorded, got %q", len(keys)-len(added), h.events)
	}
	if got := recorded(name); len(got) != len(keys) {
		t.Errorf("after the retry %d of %d entries are recorded", len(got), len(keys))
	}

	const big = "b4d_s6_v4"
	h.sets[big] = map[string]bool{}
	h.failRestore = true
	h.events = nil
	many := make([]string, dscpIptBatch+20)
	for i := range many {
		many[i] = fmt.Sprintf("10.100.%d.%d", i/256, i%256)
		h.refuseAdds[many[i]] = true
	}
	err = dscpIptAdd(big, many)
	if want := fmt.Sprintf("%d of %d entries could not be added", len(many), len(many)); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("every entry of a set that takes none must count as failed, got %v", err)
	}
	restores := 0
	for _, e := range h.events {
		if strings.HasPrefix(e, "ipset restore -exist: ") {
			restores++
		}
	}
	if got := singles(big); restores != 2 || !slices.Equal(got, slices.Concat(many[:dscpIptSingleStreak], many[dscpIptBatch:dscpIptBatch+dscpIptSingleStreak])) {
		t.Errorf("each of the 2 batches must try its restore once and then at most %d entries one by one, got %d restores and %d single adds", dscpIptSingleStreak, restores, len(got))
	}
	if got := recorded(big); len(got) != 0 {
		t.Errorf("no entry was added, yet %d are recorded", len(got))
	}
}

func TestSetDSCPIptCapabilityCached(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
	now := time.Unix(1_790_000_000, 0)
	dscpIptNow = func() time.Time { return now }
	h.probeErr[backendIP6Tables] = errTestNoIPSetKernel
	cfg := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8", "fd00::/16"))
	probed := func(v4, v6 int) {
		t.Helper()
		if h.probes[backendIPTables] != v4 || h.probes[backendIP6Tables] != v6 {
			t.Fatalf("probes = %v, want %s %d and %s %d", h.probes, backendIPTables, v4, backendIP6Tables, v6)
		}
	}

	dscpIptApply(t, dscpIptTestConfig(7, true, nil), nil)
	probed(0, 0)

	st := dscpIptApply(t, cfg, nil)
	probed(1, 1)
	if dscpIptShapeOf(st.chains[backendIP6Tables]).guard || !dscpIptShapeOf(st.chains[backendIPTables]).guard {
		t.Fatalf("only ip6tables failed the probe: %v", st.chains)
	}

	now = now.Add(5 * time.Minute)
	st = dscpIptApply(t, cfg, st)
	probed(1, 1)

	now = now.Add(6 * time.Minute)
	delete(h.probeErr, backendIP6Tables)
	st = dscpIptApply(t, cfg, st)
	probed(1, 2)
	if !dscpIptShapeOf(st.chains[backendIP6Tables]).guard {
		t.Errorf("a binary that passed the probe again still has no per-set rules: %q", st.chains[backendIP6Tables])
	}
	if _, ok := st.incapable[backendIP6Tables]; ok {
		t.Errorf("a binary that passed the probe is still listed as incapable")
	}

	now = now.Add(time.Hour)
	dscpIptApply(t, cfg, st)
	probed(1, 2)

	stubBinaryPresence(t, map[string]bool{"ipset": false})
	if err := dscpIptCapability(NewIPTablesManager(cfg, false), backendIPTables); err == nil || !strings.Contains(err.Error(), "ipset") {
		t.Errorf("a missing ipset command must be named, got %v", err)
	}
	probed(1, 2)
}

func TestSetDSCPIptCapabilityBusyNotCached(t *testing.T) {
	cfg := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"))
	cases := []struct {
		name string
		err  error
	}{
		{"lock", errors.New("iptables rejected the set match, which needs the xt_set kernel module and the iptables set extension (could not create probe chain B4_MODULE_TEST: command [iptables -w -t mangle -N B4_MODULE_TEST] failed: exit status 4 (Another app is currently holding the xtables lock. Stopped waiting after 1s.))")},
		{"timeout", errors.New("iptables rejected the set match, which needs the xt_set kernel module and the iptables set extension (command [iptables -w -t mangle -A B4_MODULE_TEST -m set --match-set b4_ipset_probe dst -j RETURN] gave up after 15s: context deadline exceeded)")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := dscpIptNewHost(t, backendIPTables)
			h.probeErr[backendIPTables] = tc.err
			st, err := dscpIptApplyPlan(cfg, backendIPTables, dscpPlanFor(cfg), nil)
			if err == nil || !strings.Contains(err.Error(), "the firewall monitor tries again") || !st.retry {
				t.Fatalf("a busy firewall during the probe must be retried, got err %v, retry %v", err, st.retry)
			}
			if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptSpecs(7, nil)); !slices.Equal(got, want) {
				t.Errorf("chain while the probe is undecided = %q, want the global chain %q", got, want)
			}
			if len(st.incapable) != 0 || len(dscpIptWarnings("cannot use ipsets")) != 0 || len(h.sets) != 0 {
				t.Errorf("a busy firewall was taken for missing ipset support: incapable %v, warnings %q, ipsets %v", st.incapable, dscpIptWarnings("cannot use ipsets"), h.snapshot())
			}
			delete(h.probeErr, backendIPTables)
			st = dscpIptApply(t, cfg, st)
			if h.probes[backendIPTables] != 2 || !dscpIptShapeOf(st.chains[backendIPTables]).guard || st.retry {
				t.Errorf("the next apply must probe again and install the per-set rules: probes %d, chain %q, retry %v", h.probes[backendIPTables], dscpIptJoined(st.chains[backendIPTables]), st.retry)
			}
		})
	}
}

func TestSetDSCPIptEnsureBusyNotCached(t *testing.T) {
	cases := []struct {
		name string
		out  string
		err  error
	}{
		{"out of memory", "ipset v7.19: Kernel error received: Cannot allocate memory", errors.New("exit status 1")},
		{"fork out of memory", "", errors.New("fork/exec /usr/sbin/ipset: cannot allocate memory")},
		{"timeout", "", fmt.Errorf("command [ipset create b4d_s31_v4 hash:net family inet maxelem 262144] gave up after 15s: %w", context.DeadlineExceeded)},
	}
	for _, tc := range cases {
		for _, globalOn := range []bool{true, false} {
			t.Run(fmt.Sprintf("%s, global %v", tc.name, globalOn), func(t *testing.T) {
				h := dscpIptNewHost(t, backendIPTables)
				cfg := dscpIptTestConfig(7, globalOn, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"))
				base := run
				failed := false
				run = func(args ...string) (string, error) {
					if !failed && strings.HasPrefix(strings.Join(args, " "), "ipset create b4d_s31_v4 ") {
						failed = true
						return tc.out, tc.err
					}
					return base(args...)
				}
				t.Cleanup(func() { run = base })

				if err := applyDSCPFor(cfg, backendIPTables); err == nil || !strings.Contains(err.Error(), "the firewall monitor tries again") {
					t.Fatalf("a passing ipset failure must be reported as retried, got %v", err)
				}
				st := dscpApplied.Load()
				if !failed || st == nil || !st.pending || st.ipt == nil || len(st.ipt.incapable) != 0 || len(dscpIptWarnings("cannot use ipsets")) != 0 {
					t.Fatalf("a passing ipset failure was taken for missing ipset support or left nothing to retry: state %+v, warnings %q", st, dscpIptWarnings("cannot use ipsets"))
				}

				if !ensureDSCPLocked(cfg, false) {
					t.Fatal("the monitor did not retry the per-set rules")
				}
				if st := dscpApplied.Load(); st == nil || st.pending {
					t.Fatalf("the retry left the per-set rules pending: %+v", st)
				}
				if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptRender(dscpPlanFor(cfg), false, true)); !slices.Equal(got, want) {
					t.Errorf("chain after the retry = %q, want %q", got, want)
				}
			})
		}
	}
}

func TestSetDSCPIptGlobalOnlyWhenIncapable(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables, backendIP6Tables)
	h.probeErr[backendIP6Tables] = errors.New("ip6tables rejected the set match, which needs the xt_set kernel module and the iptables set extension (exit status 2)")
	cfg := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8", "fd00::/16"))
	st := dscpIptApply(t, cfg, nil)
	if got, want := h.chain(backendIP6Tables), dscpIptCanon(dscpIptSpecs(7, nil)); !slices.Equal(got, want) {
		t.Errorf("ip6tables chain = %q, want the global chain %q", got, want)
	}
	if !dscpIptShapeOf(st.chains[backendIPTables]).guard {
		t.Errorf("iptables lost its per-set rules because ip6tables failed: %q", st.chains[backendIPTables])
	}
	for name := range h.sets {
		if strings.HasSuffix(name, "_v6") {
			t.Errorf("ipset %s was created for a binary that cannot match it", name)
		}
	}
	if !slices.Equal(st.bins, []string{backendIPTables, backendIP6Tables}) || st.incapable[backendIP6Tables] == "" || st.incapable[backendIPTables] != "" {
		t.Errorf("state: bins %v, incapable %v", st.bins, st.incapable)
	}
	warnings := dscpIptWarnings("IPv6")
	if len(warnings) != 1 || !strings.Contains(warnings[0], "xt_set") || !strings.Contains(warnings[0], "ipset") {
		t.Fatalf("want one warning naming ipset and xt_set, got %q", warnings)
	}
	h.mangle.calls = nil
	st = dscpIptApply(t, cfg, st)
	if got := dscpIptWarnings("IPv6"); !slices.Equal(got, warnings) {
		t.Errorf("a second apply added warnings: %q", got)
	}
	if m := h.mangle.mutations(); len(m) != 0 {
		t.Errorf("an unchanged apply rewrote the chains: %v", m)
	}

	st = dscpIptApply(t, dscpIptTestConfig(7, false, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8", "fd00::/16")), st)
	if _, ok := h.mangle.chains[backendIP6Tables][dscpChainName]; ok {
		t.Errorf("with the global stamp off, a binary without ipset support kept a chain with nothing to stamp")
	}
	if got := h.mangle.chains[backendIP6Tables]["POSTROUTING"]; len(got) != 0 {
		t.Errorf("ip6tables POSTROUTING still jumps somewhere: %v", got)
	}
	if !slices.Equal(st.bins, []string{backendIPTables}) {
		t.Errorf("bins = %v, want only %s", st.bins, backendIPTables)
	}
}

func TestSetDSCPIptRejectedSetRuleFallsBackToGlobal(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	h.refuseSets[backendIPTables] = true
	cfg := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"))
	st := dscpIptApply(t, cfg, nil)
	if got, want := h.chain(backendIPTables), dscpIptCanon(dscpIptSpecs(7, nil)); !slices.Equal(got, want) {
		t.Errorf("chain after a refused set rule = %q, want the global chain %q", got, want)
	}
	if st.incapable[backendIPTables] == "" || len(st.sets) != 0 || len(st.pending) != 0 || len(h.sets) != 0 {
		t.Errorf("a refused set rule must leave no ipset behind: incapable %v, sets %v, pending %v, host %v", st.incapable, st.sets, st.pending, h.snapshot())
	}
	if w := dscpIptWarnings("IPv4"); len(w) != 1 || !strings.Contains(w[0], "Couldn't load match") {
		t.Errorf("want one warning quoting the refusal, got %q", w)
	}
	h.mangle.calls, h.events = nil, nil
	dscpIptApply(t, cfg, st)
	if m := h.mangle.mutations(); len(m) != 0 || h.probes[backendIPTables] != 1 || h.first("ipset create") >= 0 {
		t.Errorf("the refusal was not remembered: mutations %v, probes %v, events %v", m, h.probes, h.events)
	}
}

func TestSetDSCPIptDestroyAfterRulesPending(t *testing.T) {
	h := dscpIptNewHost(t, backendIPTables)
	both := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"), dscpPlanTestSet("b", 6, "10.1.0.0/16"))
	onlyA := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"))
	lb := dscpIptLearnedSet(routeSanitizeSetID("b"), false)
	st := dscpIptApply(t, both, nil)
	if !slices.Contains(st.sets, "b4d_s6_v4") || !slices.Contains(st.sets, lb) {
		t.Fatalf("sets in use: %v", st.sets)
	}

	h.busy["b4d_s6_v4"] = true
	h.events = nil
	st = dscpIptApply(t, onlyA, st)
	lastRule := h.last(" -D " + dscpChainName + " ")
	for _, name := range []string{"b4d_s6_v4", lb} {
		if i := h.first("ipset destroy " + name); i < 0 || i < lastRule {
			t.Errorf("%s must be destroyed after the rules naming it are gone (at %d, rules until %d): %v", name, i, lastRule, h.events)
		}
	}
	if _, ok := h.sets[lb]; ok {
		t.Errorf("%s survived its set leaving the plan", lb)
	}
	if !slices.Equal(st.pending, []string{"b4d_s6_v4"}) || slices.Contains(st.sets, "b4d_s6_v4") || slices.Contains(st.sets, lb) {
		t.Fatalf("pending %v, sets %v", st.pending, st.sets)
	}

	h.busy = map[string]bool{}
	h.mangle.calls = nil
	st = dscpIptApply(t, onlyA, st)
	if _, ok := h.sets["b4d_s6_v4"]; ok || len(st.pending) != 0 {
		t.Errorf("the pending destroy was not retried: pending %v", st.pending)
	}
	if m := h.mangle.mutations(); len(m) != 0 {
		t.Errorf("retrying a destroy rewrote the chain: %v", m)
	}

	st = dscpIptApply(t, both, st)
	h.busy["b4d_s6_v4"] = true
	st = dscpIptApply(t, onlyA, st)
	h.busy = map[string]bool{}
	back := dscpIptTestConfig(7, true, nil, dscpPlanTestSet("a", 31, "10.0.0.0/8"), dscpPlanTestSet("b", 6, "10.2.0.0/16"))
	st = dscpIptApply(t, back, st)
	if got, want := h.entries("b4d_s6_v4"), []string{"10.2.0.0/16"}; !slices.Equal(got, want) {
		t.Errorf("an ipset that came back while its destroy was pending holds %v, want %v", got, want)
	}
	if len(st.pending) != 0 {
		t.Errorf("an ipset back in use is still pending: %v", st.pending)
	}

	if pending := dscpIptDestroySets([]string{"b4d_s99_v4"}); len(pending) != 0 {
		t.Errorf("a set that does not exist must count as destroyed, got pending %v", pending)
	}
}
