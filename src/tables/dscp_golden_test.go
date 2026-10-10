package tables

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var updateDSCPGolden = flag.Bool("update-dscp-golden", false, "rewrite testdata/dscp_golden with the firewall commands this run records")

type goldenGlobal struct {
	name string
	dscp config.DSCPConfig
}

var goldenGlobals = []goldenGlobal{
	{name: "off"},
	{name: "on", dscp: config.DSCPConfig{Enabled: true, Value: 7}},
	{name: "on-ifaces", dscp: config.DSCPConfig{Enabled: true, Value: 7, Interfaces: []string{"eth0", "wan"}}},
}

var goldenIptBinaries = []string{backendIPTables, backendIP6Tables, backendIPTablesLegacy, backendIP6TablesLegacy}

var goldenTripwires = []string{backendIPTables, backendIP6Tables, backendIPTablesLegacy, backendIP6TablesLegacy, "ip", "modprobe", "insmod", "sysctl", "conntrack"}

var goldenIptBuiltins = map[string][]string{
	"filter": {"INPUT", "FORWARD", "OUTPUT"},
	"nat":    {"PREROUTING", "INPUT", "OUTPUT", "POSTROUTING"},
	"mangle": {"PREROUTING", "INPUT", "FORWARD", "OUTPUT", "POSTROUTING"},
	"raw":    {"PREROUTING", "OUTPUT"},
}

const (
	goldenNoChain = "No chain/target/match by that name."
	goldenBadRule = "Bad rule (does a matching rule exist in that chain?)."
)

const goldenNftStub = `missing() {
	printf 'Error: No such file or directory\n' >&2
	exit 1
}
while [ $# -gt 0 ]; do
	case "$1" in
	-*) shift ;;
	*) break ;;
	esac
done
t="$d/nft.t.$3.$4"
c="$d/nft.c.$3.$4.$5"
case "$1 $2" in
"list tables")
	for f in "$d"/nft.t.*; do
		if [ -s "$f" ]; then
			IFS= read -r line < "$f"
			printf '%s\n' "$line"
		fi
	done
	;;
"list table") [ -s "$t" ] || missing ;;
"list chain") [ -s "$c" ] || missing ;;
"add table" | "create table") printf 'table %s %s\n' "$3" "$4" > "$t" ;;
"add chain" | "create chain")
	[ -s "$t" ] || missing
	printf 'chain\n' > "$c"
	;;
"delete table")
	[ -s "$t" ] || missing
	: > "$t"
	for f in "$d/nft.c.$3.$4."*; do
		if [ -e "$f" ]; then
			: > "$f"
		fi
	done
	;;
"delete chain")
	[ -s "$c" ] || missing
	: > "$c"
	;;
"flush table") [ -s "$t" ] || missing ;;
"flush chain" | "add rule" | "insert rule") [ -s "$c" ] || missing ;;
"add set" | "add element" | "flush set" | "delete set") [ -s "$t" ] || missing ;;
esac
exit 0
`

const goldenIPSetStub = `if [ "$1" = restore ]; then
	while IFS= read -r line || [ -n "$line" ]; do
		printf '  %s\n' "$line" >> "$d/exec.log"
	done
fi
exit 0
`

type goldenIptChain struct {
	name    string
	builtin bool
	rules   []string
}

type goldenIptTable struct {
	chains []*goldenIptChain
}

type goldenNftTable struct {
	order  []string
	chains map[string][]string
}

type goldenHost struct {
	dir     string
	read    int
	lines   []string
	ipt     map[string]map[string]*goldenIptTable
	nft     map[string]*goldenNftTable
	ipsets  []string
	sysctls map[string]string
}

func TestDSCPEmissionUnchangedWithoutSetDSCP(t *testing.T) {
	for _, backend := range []string{backendIPTables, backendIPTablesLegacy, backendNFTables} {
		for _, global := range goldenGlobals {
			name := backend + "-" + global.name
			t.Run(name, func(t *testing.T) {
				got := goldenEmission(t, backend, global)
				goldenCompare(t, filepath.Join("testdata", "dscp_golden", name+".golden"), got)
			})
		}
	}
}

func goldenEmission(t *testing.T, backend string, global goldenGlobal) string {
	t.Helper()
	h := newGoldenHost(t)
	goldenIsolate(t, h)
	h.seedLeftovers(backend)

	cfg := goldenConfig(backend, global.dscp)
	changed := goldenConfig(backend, global.dscp)
	changed.System.Tables.DSCP.Value = 31

	var b strings.Builder
	step := func(name, result string) {
		b.WriteString("== " + name)
		if result != "" {
			b.WriteString(" -> " + result)
		}
		b.WriteString("\n")
		for _, line := range h.take() {
			b.WriteString(line + "\n")
		}
	}
	quiet := func(after string, c *config.Config) {
		t.Helper()
		for _, periodic := range []bool{false, true} {
			dscpSyncPass(c, periodic, nil)
			if lines := h.take(); len(lines) > 0 {
				t.Errorf("a SyncDSCP pass (periodic %v) after %s ran commands for a configuration without a set DSCP:\n%s", periodic, after, strings.Join(lines, "\n"))
			}
		}
	}
	step("ClearRules before AddRules", goldenResult(ClearRules(cfg)))
	quiet("ClearRules before AddRules", cfg)
	step("AddRules", goldenResult(AddRules(cfg)))
	quiet("AddRules", cfg)
	quiet("AddRules", changed)
	step("ensureDSCPLocked with the stamp in place", strconv.FormatBool(goldenEnsure(cfg)))
	h.dropLastStamp(backend)
	step("ensureDSCPLocked after a stamp rule went missing", strconv.FormatBool(goldenEnsure(cfg)))
	step("RefreshRules with the same settings", goldenResult(RefreshRules(goldenConfig(backend, global.dscp))))
	step("RefreshRules with DSCP value 31", goldenResult(RefreshRules(changed)))
	quiet("RefreshRules", changed)
	step("ClearRules", goldenResult(ClearRules(changed)))
	quiet("ClearRules", changed)
	step("ApplyDSCPOnly", goldenResult(ApplyDSCPOnly(cfg)))
	quiet("ApplyDSCPOnly", cfg)
	ClearDSCPOnly(cfg)
	step("ClearDSCPOnly", "")
	RoutingClearAll()
	step("RoutingClearAll", "")
	return b.String()
}

func goldenConfig(backend string, dscp config.DSCPConfig) *config.Config {
	cfg := config.NewConfig()
	cfg.System.Tables.Engine = backend
	cfg.System.Tables.DSCP = dscp
	cfg.System.Tables.DSCP.Interfaces = slices.Clone(dscp.Interfaces)
	cfg.Sets = goldenSets()
	return &cfg
}

func goldenSets() []*config.SetConfig {
	iface := goldenSet("golden-iface", "198.51.100.0/24")
	iface.Routing.Enabled, iface.Routing.Mode, iface.Routing.EgressInterface = true, config.RoutingModeInterface, "wg0"
	proxy := goldenSet("golden-proxy", "203.0.113.0/24")
	proxy.Routing.Enabled, proxy.Routing.Mode = true, config.RoutingModeProxy
	proxy.Routing.Upstream.Host, proxy.Routing.Upstream.Port = "192.0.2.10", 1080
	block := goldenSet("golden-block", "192.0.2.128/25")
	block.Routing.Enabled, block.Routing.Mode = true, config.RoutingModeBlock
	mss := goldenSet("golden-mss", "100.64.10.0/24")
	mss.MSSClamp.Enabled, mss.MSSClamp.Size = true, 88
	return []*config.SetConfig{iface, proxy, block, mss}
}

func goldenSet(id, target string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = id, id, true
	set.Targets.IPs = []string{target}
	set.Targets.IpsToMatch = []string{target}
	return &set
}

func goldenResult(err error) string {
	if err != nil {
		return "error: " + err.Error()
	}
	return "ok"
}

func goldenEnsure(cfg *config.Config) bool {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	return ensureDSCPLocked(cfg, false)
}

func goldenCompare(t *testing.T, path, got string) {
	t.Helper()
	if *updateDSCPGolden {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("there is no golden to compare with (%v); record it with: go test ./tables/ -run TestDSCPEmissionUnchangedWithoutSetDSCP -update-dscp-golden", err)
	}
	if want := string(raw); got != want {
		t.Errorf("%s: a config without a set DSCP no longer sends the firewall the same commands. %s", path, goldenFirstDifference(got, want))
	}
}

func goldenFirstDifference(got, want string) string {
	g, w := strings.Split(got, "\n"), strings.Split(want, "\n")
	i := 0
	for i < len(g) && i < len(w) && g[i] == w[i] {
		i++
	}
	section := "before the first step"
	for _, line := range w[:min(i+1, len(w))] {
		if strings.HasPrefix(line, "== ") {
			section = line
		}
	}
	window := func(lines []string) string {
		end := min(i+5, len(lines))
		if i >= end {
			return "(end of output)"
		}
		return strings.Join(lines[i:end], "\n")
	}
	return fmt.Sprintf("The first difference is on line %d, in %q.\ngot:\n%s\nwant:\n%s", i+1, section, window(g), window(w))
}

func goldenIsolate(t *testing.T, h *goldenHost) {
	t.Helper()
	resetDSCPState(t)
	for _, m := range []*sync.Map{&hasBinaryCache, &iptWaitSupport, &rawQueueProven, &rawFallbackWarned} {
		goldenKeepSyncMap(t, m)
	}
	for _, name := range append(slices.Clone(goldenIptBinaries), "ipset", "nft", "ip") {
		hasBinaryCache.Store(name, true)
	}
	for _, bin := range goldenIptBinaries {
		iptWaitSupport.Store(bin, true)
	}

	sys := filepath.Join(h.dir, "sys")
	for _, module := range append(slices.Clone(kernelModuleList), "xt_hashlimit", "xt_DSCP", "iptable_raw", "ip6table_raw") {
		if err := os.MkdirAll(filepath.Join(sys, kmodCanonical(module)), 0o755); err != nil {
			t.Fatal(err)
		}
	}

	origRun, origStdin, origNftStdin := run, runStdin, runNftStdin
	origSys, origSnapshot := kmodSysRoot, sysctlSnapPath
	origAdd, origClear := addRulesFn, clearRulesFn
	origSteering, origStrict := activeSteering, conntrackStrictBites
	origLevel := log.CurLevel.Load()
	run, runStdin, runNftStdin = h.run, h.runStdin, h.runNftStdin
	kmodSysRoot, sysctlSnapPath = sys, filepath.Join(h.dir, "sysctl.json")
	addRulesFn, clearRulesFn = addRules, clearRules
	activeSteering, conntrackStrictBites = nil, false
	log.SetLevel(log.LevelInfo)
	t.Cleanup(func() {
		run, runStdin, runNftStdin = origRun, origStdin, origNftStdin
		kmodSysRoot, sysctlSnapPath = origSys, origSnapshot
		addRulesFn, clearRulesFn = origAdd, origClear
		activeSteering, conntrackStrictBites = origSteering, origStrict
		log.CurLevel.Store(origLevel)
	})

	rulesMu.Lock()
	origApplied, origBackend, origClosed := rulesAppliedCfg, rulesAppliedBackend, dscpSyncClosed.Load()
	rulesAppliedCfg, rulesAppliedBackend = nil, ""
	dscpSyncClosed.Store(false)
	rulesMu.Unlock()
	t.Cleanup(func() {
		rulesMu.Lock()
		rulesAppliedCfg, rulesAppliedBackend = origApplied, origBackend
		dscpSyncClosed.Store(origClosed)
		rulesMu.Unlock()
	})

	queueProbeMu.Lock()
	origProbes := queueProbeCache
	queueProbeCache = map[string]queueCaps{}
	queueProbeMu.Unlock()
	t.Cleanup(func() {
		queueProbeMu.Lock()
		queueProbeCache = origProbes
		queueProbeMu.Unlock()
	})

	dnsQueryPlacementMu.Lock()
	origPlacement := dnsQueryFromRaw
	dnsQueryFromRaw = nil
	dnsQueryPlacementMu.Unlock()
	t.Cleanup(func() {
		dnsQueryPlacementMu.Lock()
		dnsQueryFromRaw = origPlacement
		dnsQueryPlacementMu.Unlock()
	})

	routeMu.Lock()
	origEngine, origCache := routeEngine, routeRuleCache
	routeEngine, routeRuleCache = nil, map[string]routeState{}
	routeClearSyncRetry()
	routeMu.Unlock()
	t.Cleanup(func() {
		routeMu.Lock()
		routeEngine, routeRuleCache = origEngine, origCache
		routeMu.Unlock()
	})
}

func goldenKeepSyncMap(t *testing.T, m *sync.Map) {
	t.Helper()
	saved := map[any]any{}
	m.Range(func(k, v any) bool {
		saved[k] = v
		return true
	})
	m.Clear()
	t.Cleanup(func() {
		m.Clear()
		for k, v := range saved {
			m.Store(k, v)
		}
	})
}

func newGoldenHost(t *testing.T) *goldenHost {
	t.Helper()
	h := &goldenHost{
		dir:     t.TempDir(),
		ipt:     map[string]map[string]*goldenIptTable{},
		nft:     map[string]*goldenNftTable{},
		sysctls: map[string]string{"net.netfilter.nf_conntrack_checksum": "1", conntrackLiberalSysctl: "0"},
	}
	bin := filepath.Join(h.dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	stubs := map[string]string{"nft": goldenNftStub, "ipset": goldenIPSetStub}
	for _, name := range goldenTripwires {
		stubs[name] = "exit 1\n"
	}
	head := "#!/bin/sh\nd='" + h.dir + "'\n" + `printf 'exec %s %s\n' "${0##*/}" "$*" >> "$d/exec.log"` + "\n"
	for name, body := range stubs {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(head+body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", bin)
	return h
}

func (h *goldenHost) drain() {
	raw, err := os.ReadFile(filepath.Join(h.dir, "exec.log"))
	if err != nil || len(raw) <= h.read {
		return
	}
	chunk := strings.TrimSuffix(string(raw[h.read:]), "\n")
	h.read = len(raw)
	h.lines = append(h.lines, strings.Split(chunk, "\n")...)
}

func (h *goldenHost) record(head, payload string) {
	h.drain()
	h.lines = append(h.lines, head)
	if payload == "" {
		return
	}
	for _, line := range strings.Split(strings.TrimSuffix(payload, "\n"), "\n") {
		h.lines = append(h.lines, "  "+line)
	}
}

func (h *goldenHost) take() []string {
	h.drain()
	out := h.lines
	h.lines = nil
	return out
}

func (h *goldenHost) run(args ...string) (string, error) {
	h.record(strings.Join(args, " "), "")
	if len(args) == 0 {
		return "", errors.New("exit status 1")
	}
	switch name := args[0]; {
	case isIPTablesBinary(name):
		return h.iptables(name, args[1:])
	case name == "ipset":
		return h.ipset(args[1:])
	case name == "nft":
		return goldenNftExec(h.nft, args[1:])
	case name == "ip":
		return h.ip(args[1:])
	case name == "sh":
		return h.shell(args[1:])
	case name == "modprobe" || name == "insmod":
		return "", nil
	}
	return args[0] + ": the golden host does not model this command", errors.New("exit status 127")
}

func (h *goldenHost) runStdin(stdin string, args ...string) error {
	h.record("stdin "+strings.Join(args, " "), stdin)
	return nil
}

func (h *goldenHost) runNftStdin(script string) (string, error) {
	h.record("stdin nft -f -", script)
	next := goldenNftClone(h.nft)
	for _, line := range strings.Split(script, "\n") {
		if f := strings.Fields(line); len(f) > 0 {
			if out, err := goldenNftExec(next, f); err != nil {
				return out, err
			}
		}
	}
	h.nft = next
	return "", nil
}

func goldenV4Binary(backend string) string {
	if backend == backendIPTablesLegacy {
		return backendIPTablesLegacy
	}
	return backendIPTables
}

func (h *goldenHost) seedLeftovers(backend string) {
	if backend == backendNFTables {
		h.nft["inet "+routeNftTable] = &goldenNftTable{order: []string{"prerouting"}, chains: map[string][]string{"prerouting": {"meta mark set 0x10000"}}}
		return
	}
	mangle := h.iptTable(goldenV4Binary(backend), "mangle")
	mangle.chains = append(mangle.chains, &goldenIptChain{name: "b4r_stale_pre", rules: []string{"-j MARK --set-xmark 0x10000/0xff0000"}})
	pre := mangle.chain("PREROUTING")
	pre.rules = append(pre.rules, "-j b4r_stale_pre")
	h.ipsets = append(h.ipsets, "b4r_stale_v4")
}

func (h *goldenHost) dropLastStamp(backend string) {
	if backend == backendNFTables {
		if tb := h.nft["inet "+dscpNftTable]; tb != nil {
			if rules := tb.chains[dscpNftChain]; len(rules) > 0 {
				tb.chains[dscpNftChain] = rules[:len(rules)-1]
			}
		}
		return
	}
	if c := h.iptTable(goldenV4Binary(backend), "mangle").chain(dscpChainName); c != nil && len(c.rules) > 0 {
		c.rules = c.rules[:len(c.rules)-1]
	}
}

func (h *goldenHost) iptTable(bin, name string) *goldenIptTable {
	builtins, ok := goldenIptBuiltins[name]
	if !ok {
		return nil
	}
	if h.ipt[bin] == nil {
		h.ipt[bin] = map[string]*goldenIptTable{}
	}
	tb := h.ipt[bin][name]
	if tb == nil {
		tb = &goldenIptTable{}
		for _, chain := range builtins {
			tb.chains = append(tb.chains, &goldenIptChain{name: chain, builtin: true})
		}
		h.ipt[bin][name] = tb
	}
	return tb
}

func (h *goldenHost) iptables(bin string, args []string) (string, error) {
	table := "filter"
	var rest []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-w" || args[i] == "--wait":
		case args[i] == "-t" && i+1 < len(args):
			i++
			table = args[i]
		default:
			rest = append(rest, args[i])
		}
	}
	tool := strings.TrimSuffix(bin, "-legacy")
	fail := func(msg string) (string, error) {
		return tool + ": " + msg, errors.New("exit status 1")
	}
	if len(rest) == 0 {
		return fail("no command specified")
	}
	if rest[0] == "--version" {
		return tool + " v1.8.7 (legacy)\n", nil
	}
	tb := h.iptTable(bin, table)
	if tb == nil {
		return fail("can't initialize " + tool + " table `" + table + "': Table does not exist (do you need to insmod?)")
	}
	if slices.ContainsFunc(rest, goldenIptListFlag) {
		chain := ""
		for _, arg := range rest {
			if !strings.HasPrefix(arg, "-") {
				chain = arg
				break
			}
		}
		if chain != "" && tb.chain(chain) == nil {
			return fail(goldenNoChain)
		}
		return tb.list(strings.HasPrefix(bin, "ip6"), chain, slices.Contains(rest, "--line-numbers")), nil
	}
	op, name, spec := rest[0], "", []string(nil)
	if len(rest) > 1 {
		name, spec = rest[1], rest[2:]
	}
	c := tb.chain(name)
	switch op {
	case "-S":
		if name != "" && c == nil {
			return fail(goldenNoChain)
		}
		return tb.save(name), nil
	case "-N":
		if c != nil {
			return fail("Chain already exists.")
		}
		tb.chains = append(tb.chains, &goldenIptChain{name: name})
	case "-F":
		if c == nil {
			return fail(goldenNoChain)
		}
		c.rules = nil
	case "-X":
		switch {
		case c == nil || c.builtin:
			return fail(goldenNoChain)
		case len(c.rules) > 0:
			return fail("Directory not empty.")
		case tb.references(name) > 0:
			return fail("Too many links.")
		}
		tb.chains = slices.DeleteFunc(tb.chains, func(x *goldenIptChain) bool { return x == c })
	case "-A", "-I":
		if c == nil {
			return fail(goldenNoChain)
		}
		pos := len(c.rules) + 1
		if op == "-I" {
			pos = 1
			if len(spec) > 0 {
				if n, err := strconv.Atoi(spec[0]); err == nil {
					pos, spec = n, spec[1:]
				}
			}
		}
		if pos < 1 || pos > len(c.rules)+1 {
			return fail("Index of insertion too big.")
		}
		c.rules = slices.Insert(c.rules, pos-1, goldenIptCanon(spec))
	case "-D":
		if c == nil {
			return fail(goldenNoChain)
		}
		at := -1
		if len(spec) == 1 {
			if n, err := strconv.Atoi(spec[0]); err == nil {
				if n < 1 || n > len(c.rules) {
					return fail("Index of deletion too big.")
				}
				at = n - 1
			}
		}
		if at < 0 {
			at = slices.Index(c.rules, goldenIptCanon(spec))
		}
		if at < 0 {
			return fail(goldenBadRule)
		}
		c.rules = slices.Delete(c.rules, at, at+1)
	case "-C":
		if c == nil {
			return fail(goldenNoChain)
		}
		if !slices.Contains(c.rules, goldenIptCanon(spec)) {
			return fail(goldenBadRule)
		}
	case "-Z", "-P":
	default:
		return fail("unknown option \"" + op + "\"")
	}
	return "", nil
}

func goldenIptListFlag(arg string) bool {
	if arg == "-L" || arg == "--list" {
		return true
	}
	return len(arg) > 2 && arg[0] == '-' && arg[1] != '-' && strings.Contains(arg, "L") && strings.Trim(arg[1:], "nvxL") == ""
}

func goldenIptCanon(spec []string) string {
	out := slices.Clone(spec)
	for i := 0; i+1 < len(out); i++ {
		if out[i] == "--set-dscp" {
			if n, err := strconv.ParseUint(out[i+1], 0, 8); err == nil {
				out[i+1] = fmt.Sprintf("0x%02x", n)
			}
		}
	}
	return strings.Join(out, " ")
}

func goldenIptTarget(rule string) string {
	f := strings.Fields(rule)
	for i := 0; i+1 < len(f); i++ {
		if f[i] == "-j" {
			return f[i+1]
		}
	}
	return ""
}

func (tb *goldenIptTable) chain(name string) *goldenIptChain {
	for _, c := range tb.chains {
		if c.name == name {
			return c
		}
	}
	return nil
}

func (tb *goldenIptTable) references(name string) int {
	n := 0
	for _, c := range tb.chains {
		for _, rule := range c.rules {
			if goldenIptTarget(rule) == name {
				n++
			}
		}
	}
	return n
}

func (tb *goldenIptTable) save(only string) string {
	var b strings.Builder
	for _, c := range tb.chains {
		switch {
		case only != "" && c.name != only:
		case c.builtin:
			fmt.Fprintf(&b, "-P %s ACCEPT\n", c.name)
		default:
			fmt.Fprintf(&b, "-N %s\n", c.name)
		}
	}
	for _, c := range tb.chains {
		if only != "" && c.name != only {
			continue
		}
		for _, rule := range c.rules {
			fmt.Fprintf(&b, "-A %s %s\n", c.name, rule)
		}
	}
	return b.String()
}

func (tb *goldenIptTable) list(v6 bool, only string, numbered bool) string {
	var b strings.Builder
	first := true
	for _, c := range tb.chains {
		if only != "" && c.name != only {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		if c.builtin {
			fmt.Fprintf(&b, "Chain %s (policy ACCEPT)\n", c.name)
		} else {
			fmt.Fprintf(&b, "Chain %s (%d references)\n", c.name, tb.references(c.name))
		}
		if numbered {
			b.WriteString("num  ")
		}
		b.WriteString("target     prot opt source               destination\n")
		for i, rule := range c.rules {
			if numbered {
				fmt.Fprintf(&b, "%-4d ", i+1)
			}
			b.WriteString(goldenIptListLine(v6, rule) + "\n")
		}
	}
	return b.String()
}

func goldenIptListLine(v6 bool, rule string) string {
	target, prot, opt, src, dst := "", "all", "--", "0.0.0.0/0", "0.0.0.0/0"
	if v6 {
		opt, src, dst = "", "::/0", "::/0"
	}
	var extra []string
	module, bang := "", ""
	f := strings.Fields(rule)
	for i := 0; i < len(f); i++ {
		arg, val := f[i], ""
		if i+1 < len(f) {
			val = f[i+1]
		}
		switch arg {
		case "!":
			bang = "!"
			continue
		case "-p":
			prot = val
			i++
		case "-s":
			src = bang + val
			i++
		case "-d":
			dst = bang + val
			i++
		case "-i", "-o":
			i++
		case "-m":
			module = val
			i++
		case "-j":
			target = val
			i++
		case "--mark":
			extra = append(extra, module+" match "+bang+val)
			i++
		case "--sport":
			extra = append(extra, prot+" spt:"+bang+val)
			i++
		case "--dport":
			extra = append(extra, prot+" dpt:"+bang+val)
			i++
		case "--sports", "--dports":
			extra = append(extra, "multiport "+strings.TrimPrefix(arg, "--")+" "+bang+val)
			i++
		case "--mac-source":
			extra = append(extra, "MAC "+bang+val)
			i++
		case "--match-set":
			dir := ""
			if i+2 < len(f) {
				dir = f[i+2]
			}
			extra = append(extra, "match-set "+bang+val+" "+dir)
			i += 2
		case "--tcp-flags":
			comp := ""
			if i+2 < len(f) {
				comp = f[i+2]
			}
			extra = append(extra, "tcp flags:"+bang+val+"/"+comp)
			i += 2
		case "--queue-num":
			extra = append(extra, "NFQUEUE num "+val)
			i++
		case "--queue-balance":
			extra = append(extra, "NFQUEUE balance "+val)
			i++
		case "--queue-bypass":
			extra = append(extra, "bypass")
		case "--set-dscp":
			extra = append(extra, "DSCP set "+val)
			i++
		case "--set-mss":
			extra = append(extra, "TCPMSS set "+val)
			i++
		default:
			extra = append(extra, arg)
		}
		bang = ""
	}
	return strings.TrimRight(fmt.Sprintf("%-10s %-4s %-3s %-20s %-20s %s", target, prot, opt, src, dst, strings.Join(extra, " ")), " ")
}

func (h *goldenHost) ipset(args []string) (string, error) {
	if len(args) < 2 {
		return "", nil
	}
	missing := func() (string, error) {
		return "ipset v7.19: The set with the given name does not exist", errors.New("exit status 1")
	}
	name := args[1]
	exists := slices.Contains(h.ipsets, name)
	switch args[0] {
	case "create":
		if exists && !slices.Contains(args, "-exist") {
			return "ipset v7.19: Set cannot be created: set with the same name already exists", errors.New("exit status 1")
		}
		if !exists {
			h.ipsets = append(h.ipsets, name)
		}
	case "destroy":
		if !exists {
			return missing()
		}
		h.ipsets = slices.DeleteFunc(h.ipsets, func(s string) bool { return s == name })
	case "list", "save":
		if name == "-n" || name == "-name" {
			if len(h.ipsets) == 0 {
				return "", nil
			}
			return strings.Join(h.ipsets, "\n") + "\n", nil
		}
		if !exists {
			return missing()
		}
		return "Name: " + name + "\nType: hash:net\nMembers:\n", nil
	case "flush", "add", "del", "test":
		if !exists {
			return missing()
		}
	}
	return "", nil
}

func (h *goldenHost) ip(args []string) (string, error) {
	v6 := len(args) > 0 && args[0] == "-6"
	if v6 {
		args = args[1:]
	}
	if len(args) < 2 || args[0] != "rule" || (args[1] != "show" && args[1] != "list") {
		return "", nil
	}
	if v6 {
		return "0:\tfrom all lookup local\n32766:\tfrom all lookup main\n", nil
	}
	return "0:\tfrom all lookup local\n32766:\tfrom all lookup main\n32767:\tfrom all lookup default\n", nil
}

func (h *goldenHost) shell(args []string) (string, error) {
	if len(args) < 2 || args[0] != "-c" {
		return "", nil
	}
	f := strings.Fields(args[1])
	if len(f) < 3 || f[0] != "sysctl" {
		return "", nil
	}
	switch f[1] {
	case "-n":
		return h.sysctls[f[2]] + "\n", nil
	case "-w":
		if name, value, ok := strings.Cut(f[2], "="); ok {
			h.sysctls[name] = value
		}
	}
	return "", nil
}

func goldenNftClone(src map[string]*goldenNftTable) map[string]*goldenNftTable {
	dst := make(map[string]*goldenNftTable, len(src))
	for key, tb := range src {
		chains := make(map[string][]string, len(tb.chains))
		for name, rules := range tb.chains {
			chains[name] = slices.Clone(rules)
		}
		dst[key] = &goldenNftTable{order: slices.Clone(tb.order), chains: chains}
	}
	return dst
}

func goldenNftExec(tables map[string]*goldenNftTable, f []string) (string, error) {
	for len(f) > 0 && strings.HasPrefix(f[0], "-") {
		f = f[1:]
	}
	if len(f) < 2 {
		return "", nil
	}
	missing := func() (string, error) {
		return "Error: No such file or directory", errors.New("exit status 1")
	}
	key, chain := "", ""
	if len(f) > 3 {
		key = f[2] + " " + f[3]
	}
	if len(f) > 4 {
		chain = f[4]
	}
	tb := tables[key]
	hasChain := false
	if tb != nil {
		_, hasChain = tb.chains[chain]
	}
	switch f[0] + " " + f[1] {
	case "list tables":
		names := make([]string, 0, len(tables))
		for name := range tables {
			names = append(names, "table "+name+"\n")
		}
		sort.Strings(names)
		return strings.Join(names, ""), nil
	case "list table":
		if tb == nil {
			return missing()
		}
		return goldenNftRender(key, tb, tb.order), nil
	case "list chain":
		if !hasChain {
			return missing()
		}
		return goldenNftRender(key, tb, []string{chain}), nil
	case "add table", "create table":
		if tb == nil {
			tables[key] = &goldenNftTable{chains: map[string][]string{}}
		}
	case "delete table":
		if tb == nil {
			return missing()
		}
		delete(tables, key)
	case "flush table":
		if tb == nil {
			return missing()
		}
		for _, name := range tb.order {
			tb.chains[name] = nil
		}
	case "add chain", "create chain":
		if tb == nil {
			return missing()
		}
		if !hasChain {
			tb.order = append(tb.order, chain)
			tb.chains[chain] = nil
		}
	case "delete chain":
		if !hasChain {
			return missing()
		}
		delete(tb.chains, chain)
		tb.order = slices.DeleteFunc(tb.order, func(name string) bool { return name == chain })
	case "flush chain":
		if !hasChain {
			return missing()
		}
		tb.chains[chain] = nil
	case "add rule", "insert rule":
		if !hasChain {
			return missing()
		}
		rule := strings.Join(f[5:], " ")
		if f[0] == "insert" {
			tb.chains[chain] = append([]string{rule}, tb.chains[chain]...)
		} else {
			tb.chains[chain] = append(tb.chains[chain], rule)
		}
	}
	return "", nil
}

func goldenNftRender(key string, tb *goldenNftTable, chains []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "table %s {\n", key)
	for _, name := range chains {
		fmt.Fprintf(&b, "\tchain %s {\n", name)
		for _, rule := range tb.chains[name] {
			fmt.Fprintf(&b, "\t\t%s\n", rule)
		}
		b.WriteString("\t}\n")
	}
	b.WriteString("}\n")
	return b.String()
}
