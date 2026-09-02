package tables

import (
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	gateDegradedManualIP = "a manually added device has no usable IP address"
	gateDegradedCombined = "no device is left after combining the set filter with the global device filter"
)

type routeDeviceGate struct {
	enabled   bool
	blacklist bool
	matches   []config.DeviceMatch
	degraded  string
}

func routeDeviceGateFor(cfg *config.Config) routeDeviceGate {
	var matches []config.DeviceMatch
	seen := make(map[string]struct{})
	unresolved := 0
	for i := range cfg.Queue.Devices.Devices {
		d := &cfg.Queue.Devices.Devices[i]
		if !d.Selected {
			continue
		}
		m, ok := d.Match()
		if !ok {
			if d.IsManual {
				unresolved++
			}
			continue
		}
		matches = config.AppendDeviceMatch(matches, seen, m)
	}
	if !cfg.Queue.Devices.Enabled {
		return routeDeviceGate{}
	}
	if len(matches) == 0 {
		if unresolved > 0 {
			return routeDeviceGate{degraded: gateDegradedManualIP}
		}
		return routeDeviceGate{}
	}
	return routeDeviceGate{
		enabled:   true,
		blacklist: cfg.Queue.Devices.WhiteIsBlack,
		matches:   matches,
	}.withDegraded(unresolved)
}

func setSourceDeviceMatches(cfg *config.Config, set *config.SetConfig) ([]config.DeviceMatch, int) {
	if set == nil {
		return nil, 0
	}
	var matches []config.DeviceMatch
	seen := make(map[string]struct{})
	unresolved := 0
	for _, raw := range set.Targets.SourceDevices {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		m, ok := cfg.Queue.Devices.MatchForMAC(raw)
		if !ok {
			unresolved++
			continue
		}
		matches = config.AppendDeviceMatch(matches, seen, m)
	}
	return matches, unresolved
}

func (g routeDeviceGate) withDegraded(unresolved int) routeDeviceGate {
	switch {
	case unresolved > 0:
		g.degraded = gateDegradedManualIP
	case g.isWhitelist() && len(g.matches) == 0:
		g.degraded = gateDegradedCombined
	}
	return g
}

func routeSetDeviceGate(cfg *config.Config, set *config.SetConfig) routeDeviceGate {
	global := routeDeviceGateFor(cfg)
	perSet, unresolved := setSourceDeviceMatches(cfg, set)
	if len(perSet) == 0 {
		if unresolved == 0 {
			return global
		}
		if set.Targets.SourceDevicesExclude {
			global.degraded = gateDegradedManualIP
			return global
		}
		return routeDeviceGate{enabled: true, degraded: gateDegradedManualIP}
	}
	if set.Targets.SourceDevicesExclude {
		switch {
		case global.isWhitelist():
			return routeDeviceGate{enabled: true, blacklist: false, matches: subtractMatches(global.matches, perSet)}.withDegraded(unresolved)
		case global.isBlacklist():
			return routeDeviceGate{enabled: true, blacklist: true, matches: unionMatches(global.matches, perSet)}.withDegraded(unresolved)
		}
		return routeDeviceGate{enabled: true, blacklist: true, matches: perSet}.withDegraded(unresolved)
	}
	matches := perSet
	switch {
	case global.isWhitelist():
		matches = intersectMatches(global.matches, perSet)
	case global.isBlacklist():
		matches = subtractMatches(perSet, global.matches)
	}
	return routeDeviceGate{enabled: true, blacklist: false, matches: matches}.withDegraded(unresolved)
}

func intersectMatches(a, b []config.DeviceMatch) []config.DeviceMatch {
	in := make(map[string]struct{}, len(a))
	for _, m := range a {
		in[m.Key()] = struct{}{}
	}
	out := make([]config.DeviceMatch, 0, len(b))
	for _, m := range b {
		if _, ok := in[m.Key()]; ok {
			out = append(out, m)
		}
	}
	return out
}

func unionMatches(a, b []config.DeviceMatch) []config.DeviceMatch {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]config.DeviceMatch, 0, len(a)+len(b))
	for _, m := range a {
		out = config.AppendDeviceMatch(out, seen, m)
	}
	for _, m := range b {
		out = config.AppendDeviceMatch(out, seen, m)
	}
	return out
}

func subtractMatches(a, deny []config.DeviceMatch) []config.DeviceMatch {
	blocked := make(map[string]struct{}, len(deny))
	for _, m := range deny {
		blocked[m.Key()] = struct{}{}
	}
	out := make([]config.DeviceMatch, 0, len(a))
	for _, m := range a {
		if _, ok := blocked[m.Key()]; !ok {
			out = append(out, m)
		}
	}
	return out
}

func (g routeDeviceGate) isWhitelist() bool { return g.enabled && !g.blacklist }
func (g routeDeviceGate) isBlacklist() bool { return g.enabled && g.blacklist }

func (g routeDeviceGate) key() string {
	if !g.enabled {
		return ""
	}
	mode := "w"
	if g.blacklist {
		mode = "b"
	}
	keys := make([]string, 0, len(g.matches))
	for _, m := range g.matches {
		keys = append(keys, m.Key())
	}
	sort.Strings(keys)
	return mode + ":" + strings.Join(keys, ",")
}

func routeWarnDeviceGate(setName string, gate routeDeviceGate) {
	if gate.degraded == "" {
		return
	}
	if gate.isWhitelist() && len(gate.matches) == 0 {
		log.Warnf("Routing: set '%s' is limited to source devices but %s, so the set matches no device and its traffic keeps using the normal route", setName, gate.degraded)
		return
	}
	log.Warnf("Routing: set '%s' skips part of its source device filter because %s", setName, gate.degraded)
}

func routeInjectedSourceMatches(gate routeDeviceGate) []config.DeviceMatch {
	if !gate.isWhitelist() || len(gate.matches) == 0 {
		return nil
	}
	out := make([]config.DeviceMatch, 0, len(gate.matches))
	for _, m := range gate.matches {
		if !m.IsIP() {
			return nil
		}
		out = append(out, m)
	}
	return out
}

func routeInjectedSourcesForFamily(sources []config.DeviceMatch, v6 bool) []config.DeviceMatch {
	out := make([]config.DeviceMatch, 0, len(sources))
	for _, m := range sources {
		if m.IsIP() && m.V6 == v6 {
			out = append(out, m)
		}
	}
	return out
}

func nftMatchArgs(m config.DeviceMatch) []string {
	if m.IsIP() {
		if m.V6 {
			return []string{"ip6", "saddr", m.IP}
		}
		return []string{"ip", "saddr", m.IP}
	}
	return []string{"ether", "saddr", strings.ToLower(m.MAC)}
}

func iptMatchArgs(m config.DeviceMatch, v6 bool) ([]string, bool) {
	if m.IsIP() {
		if m.V6 != v6 {
			return nil, false
		}
		return []string{"-s", m.IP}, true
	}
	return []string{"-m", "mac", "--mac-source", m.MAC}, true
}

func iptCmdFor(v6, legacy bool) string {
	switch {
	case v6 && legacy:
		return backendIP6TablesLegacy
	case v6:
		return backendIP6Tables
	case legacy:
		return backendIPTablesLegacy
	default:
		return backendIPTables
	}
}

func iptCmdIsV6(cmd string) bool {
	return cmd == backendIP6Tables || cmd == backendIP6TablesLegacy
}

func iptBuiltinParents(table string) []string {
	switch table {
	case "nat":
		return []string{"PREROUTING", "INPUT", "OUTPUT", "POSTROUTING"}
	case "filter":
		return []string{"INPUT", "FORWARD", "OUTPUT"}
	default:
		return []string{"PREROUTING", "INPUT", "FORWARD", "OUTPUT", "POSTROUTING"}
	}
}

func iptJumpLineNumbers(cmd, table, parent string, match func(target string) bool) []int {
	out, err := run(cmd, "-w", "-t", table, "-L", parent, "-n", "--line-numbers")
	if err != nil {
		return nil
	}
	var nums []int
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, convErr := strconv.Atoi(fields[0])
		if convErr != nil {
			continue
		}
		if match(fields[1]) {
			nums = append(nums, n)
		}
	}
	return nums
}

const iptDeleteRounds = 64

func iptDeleteMatchingJumps(cmd, table, parent, logMsg string, match func(target string) bool) {
	for round := 0; round < iptDeleteRounds; round++ {
		nums := iptJumpLineNumbers(cmd, table, parent, match)
		if len(nums) == 0 {
			return
		}
		last := nums[len(nums)-1]
		if _, err := run(cmd, "-w", "-t", table, "-D", parent, strconv.Itoa(last)); err != nil {
			log.Warnf("%s: %s -t %s -D %s %d failed, so b4 stopped deleting by position rather than remove a rule it never read: %v",
				logMsg, cmd, table, parent, last, err)
			return
		}
	}
	log.Warnf("%s: %s -t %s %s still lists rules b4 wants gone after %d deletions, so it stopped rather than keep cutting",
		logMsg, cmd, table, parent, iptDeleteRounds)
}

func iptDeleteJumpsTo(cmd, table, parent, target string) {
	for i := 0; i < iptDeleteRounds; i++ {
		if _, err := run(cmd, "-w", "-t", table, "-D", parent, "-j", target); err != nil {
			break
		}
	}
	iptDeleteMatchingJumps(cmd, table, parent, "routing: delete jump "+parent+"->"+target,
		func(t string) bool { return t == target })
}

func iptEmitGatedJump(cmd, table, parent, target string, insertTop bool, gate routeDeviceGate) {
	at := 0
	if insertTop {
		at = 1
	}
	iptEmitGatedJumpAt(cmd, table, parent, target, at, gate)
}

func iptEmitGatedJumpAt(cmd, table, parent, target string, at int, gate routeDeviceGate) {
	op := "-A"
	var pos []string
	if at > 0 {
		op = "-I"
		pos = []string{strconv.Itoa(at)}
	}
	emit := func(deviceMatch ...string) {
		args := append([]string{cmd, "-w", "-t", table, op, parent}, pos...)
		args = append(args, deviceMatch...)
		args = append(args, "-j", target)
		runLogged("routing: add jump "+parent+"->"+target, args...)
	}
	if gate.isWhitelist() {
		v6 := iptCmdIsV6(cmd)
		for _, m := range gate.matches {
			args, ok := iptMatchArgs(m, v6)
			if !ok {
				continue
			}
			emit(args...)
		}
		return
	}
	emit()
}

func nftEmitGatedJump(parent, target string, insertTop bool, gate routeDeviceGate) {
	op := "add"
	if insertTop {
		op = "insert"
	}
	emit := func(deviceMatch ...string) {
		args := []string{"nft", op, "rule", "inet", routeNftTable, parent}
		args = append(args, deviceMatch...)
		args = append(args, "jump", target)
		runLogged("routing: add jump "+parent+"->"+target, args...)
	}
	if gate.isWhitelist() {
		for _, m := range gate.matches {
			emit(nftMatchArgs(m)...)
		}
		return
	}
	emit()
}

func routeAddBlacklistGate(be routeBackend, table, chain string, ipv4, ipv6 bool, gate routeDeviceGate) {
	if !gate.isBlacklist() {
		return
	}
	if be.name() == backendNFTables {
		for _, m := range gate.matches {
			if m.IsIP() && ((m.V6 && !ipv6) || (!m.V6 && !ipv4)) {
				continue
			}
			args := append([]string{"nft", "add", "rule", "inet", routeNftTable, chain}, nftMatchArgs(m)...)
			args = append(args, "return")
			runLogged("routing: device blacklist skip "+chain, args...)
		}
		return
	}
	legacy := isLegacyIptBackend(be)
	fams := make([]bool, 0, 2)
	if ipv4 {
		fams = append(fams, false)
	}
	if ipv6 {
		fams = append(fams, true)
	}
	for _, v6 := range fams {
		cmd := iptCmdFor(v6, legacy)
		if !hasBinary(cmd) {
			continue
		}
		for _, m := range gate.matches {
			args, ok := iptMatchArgs(m, v6)
			if !ok {
				continue
			}
			full := append([]string{cmd, "-w", "-t", table, "-A", chain}, args...)
			full = append(full, "-j", "RETURN")
			runLogged("routing: device blacklist skip "+chain, full...)
		}
	}
}

func routeEnsureGatedPreJump(be routeBackend, chain string, gate routeDeviceGate) {
	if be.name() == backendNFTables {
		standing := nftJumpHandles(routeNftTable, routeNftPrerouting, chain)
		if !gate.enabled {
			runLogged("routing: add jump "+routeNftPrerouting+"->"+chain,
				"nft", "add", "rule", "inet", routeNftTable, routeNftPrerouting, "jump", chain)
		} else {
			nftEmitGatedJump(routeNftPrerouting, chain, false, gate)
		}
		nftDropJumpHandles(routeNftTable, routeNftPrerouting, standing)
		return
	}
	ib, ok := be.(*routeIptBackend)
	if !ok {
		be.ensureJumpRule("PREROUTING", chain, true, false)
		return
	}
	for _, cmd := range ib.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		at, readable, standing := iptPreJumpPlacement(cmd, chain)
		if !readable {
			at = 1
			log.Tracef("routing: %s could not read mangle PREROUTING, so %s goes in at the top; that keeps it above %s, and a later pass puts the sets back in the configured order",
				cmd, chain, captureChainPre)
		}
		if at > 0 && !gate.isWhitelist() && len(standing) == 1 && standing[0] == at {
			continue
		}
		iptEmitGatedJumpAt(cmd, "mangle", "PREROUTING", chain, at, gate)
		added := len(iptJumpLineNumbers(cmd, "mangle", "PREROUTING", func(t string) bool { return t == chain })) - len(standing)
		if added <= 0 {
			continue
		}
		iptDropJumpsAt(cmd, "mangle", "PREROUTING", iptShiftedBy(standing, at, added))
	}
}

func routePreJumpsAlreadyOrdered(be routeBackend, ordered []*config.SetConfig) bool {
	ib, ok := be.(*routeIptBackend)
	if !ok {
		return false
	}
	want := make([]string, 0, len(ordered))
	for _, set := range ordered {
		st, cached := routeRuleCache[set.Id]
		if !cached || st.chainPre == "" {
			return false
		}
		want = append(want, st.chainPre)
	}
	checked := false
	for _, cmd := range ib.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		out, err := run(cmd, "-w", "-t", "mangle", "-L", "PREROUTING", "--line-numbers", "-n")
		if err != nil {
			return false
		}
		capture := 0
		var seen []int
		var order []string
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 2 {
				continue
			}
			n, convErr := strconv.Atoi(f[0])
			if convErr != nil || n <= 0 {
				continue
			}
			if f[1] == captureChainPre && capture == 0 {
				capture = n
				continue
			}
			if routeIsPreChainName(f[1]) {
				order = append(order, f[1])
				seen = append(seen, n)
			}
		}
		if capture == 0 || len(order) != len(want) {
			return false
		}
		for i := range want {
			if order[i] != want[i] || seen[i] > capture {
				return false
			}
		}
		checked = true
	}
	return checked
}

func iptPreJumpPlacement(cmd, chain string) (at int, readable bool, standing []int) {
	out, err := run(cmd, "-w", "-t", "mangle", "-L", "PREROUTING", "--line-numbers", "-n")
	if err != nil {
		return 0, false, nil
	}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, convErr := strconv.Atoi(f[0])
		if convErr != nil || n <= 0 {
			continue
		}
		switch {
		case f[1] == captureChainPre && at == 0:
			at = n
		case f[1] == chain:
			standing = append(standing, n)
		}
	}
	return at, true, standing
}

func iptShiftedBy(positions []int, at, added int) []int {
	moved := make([]int, 0, len(positions))
	for _, p := range positions {
		if at > 0 && p >= at {
			p += added
		}
		moved = append(moved, p)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(moved)))
	return moved
}

func iptDropJumpsAt(cmd, table, parent string, positions []int) {
	for _, n := range positions {
		if _, err := run(cmd, "-w", "-t", table, "-D", parent, strconv.Itoa(n)); err != nil {
			log.Warnf("routing: %s -t %s -D %s %d failed, so b4 stopped removing superseded jumps rather than cut a rule it never read: %v",
				cmd, table, parent, n, err)
			return
		}
	}
}

func routeEnsurePreJumpPrecedence(be routeBackend, cfg *config.Config) {
	ib, ok := be.(*routeIptBackend)
	if !ok || cfg == nil {
		return
	}
	wrong := false
	for _, cmd := range ib.iptBoth() {
		if hasBinary(cmd) && iptPreJumpsBelowCapture(cmd) {
			wrong = true
			break
		}
	}
	if !wrong {
		return
	}
	log.Warnf("Routing: a set's prerouting jump sits below %s, so the capture engine takes the reply packets of a diverted connection before the set can hand them to its listener; lifting the routing jumps back above it", captureChainPre)
	for _, set := range routeOrderedRoutingSets(cfg) {
		st, ok := routeRuleCache[set.Id]
		if !ok || st.chainPre == "" {
			continue
		}
		routeEnsureGatedPreJump(be, st.chainPre, routeSetDeviceGate(cfg, set))
	}
}

func routeSetIsSourceScoped(set *config.SetConfig) bool {
	if set == nil {
		return false
	}
	return set.RoutingSourceScoped()
}

var routeNoDestinationWarned sync.Map

func routeWarnNoDestination(set *config.SetConfig) {
	scope := "no domain or IP target"
	if routeSetIsSourceScoped(set) {
		scope = "source devices selected but no domain or IP target"
	}
	if prev, ok := routeNoDestinationWarned.Load(set.Id); ok && prev == scope {
		return
	}
	routeNoDestinationWarned.Store(set.Id, scope)
	log.Warnf("Routing: set '%s' has %s, so there is no destination to steer and no rule is installed; add a destination, or turn on 'Match any IP address' under Targets to send everything from those devices", set.Name, scope)
}

func routeForgetNoDestinationWarning(setID string) {
	routeNoDestinationWarned.Delete(setID)
}

var routeIncompleteWarned sync.Map

func routeWarnIncomplete(set *config.SetConfig, reason string) {
	if prev, ok := routeIncompleteWarned.Load(set.Id); ok && prev == reason {
		return
	}
	routeIncompleteWarned.Store(set.Id, reason)
	log.Warnf("Routing: set '%s' has routing turned on but %s, so no routing rule is installed and the set routes nothing", set.Name, reason)
}

func routeForgetIncompleteWarning(setID string) {
	routeIncompleteWarned.Delete(setID)
}
