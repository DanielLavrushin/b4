package tables

import (
	"sort"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/config"
)

type routeDeviceGate struct {
	enabled   bool
	blacklist bool
	macs      []string
}

func routeDeviceGateFor(cfg *config.Config) routeDeviceGate {
	var macs []string
	for _, m := range cfg.Queue.Devices.SelectedMACs() {
		if m = strings.ToUpper(strings.TrimSpace(m)); m != "" {
			macs = append(macs, m)
		}
	}
	if !cfg.Queue.Devices.Enabled || len(macs) == 0 {
		return routeDeviceGate{}
	}
	return routeDeviceGate{
		enabled:   true,
		blacklist: cfg.Queue.Devices.WhiteIsBlack,
		macs:      macs,
	}
}

func setSourceDeviceMACs(set *config.SetConfig) []string {
	if set == nil {
		return nil
	}
	var macs []string
	seen := make(map[string]struct{})
	for _, m := range set.Targets.SourceDevices {
		if m = strings.ToUpper(strings.TrimSpace(m)); m != "" {
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				macs = append(macs, m)
			}
		}
	}
	return macs
}

func routeSetDeviceGate(cfg *config.Config, set *config.SetConfig) routeDeviceGate {
	global := routeDeviceGateFor(cfg)
	perSet := setSourceDeviceMACs(set)
	if len(perSet) == 0 {
		return global
	}
	if set.Targets.SourceDevicesExclude {
		switch {
		case global.isWhitelist():
			return routeDeviceGate{enabled: true, blacklist: false, macs: subtractMACs(global.macs, perSet)}
		case global.isBlacklist():
			return routeDeviceGate{enabled: true, blacklist: true, macs: unionMACs(global.macs, perSet)}
		}
		return routeDeviceGate{enabled: true, blacklist: true, macs: perSet}
	}
	macs := perSet
	switch {
	case global.isWhitelist():
		macs = intersectMACs(global.macs, perSet)
	case global.isBlacklist():
		macs = subtractMACs(perSet, global.macs)
	}
	return routeDeviceGate{enabled: true, blacklist: false, macs: macs}
}

func intersectMACs(a, b []string) []string {
	in := make(map[string]struct{}, len(a))
	for _, m := range a {
		in[m] = struct{}{}
	}
	out := make([]string, 0, len(b))
	for _, m := range b {
		if _, ok := in[m]; ok {
			out = append(out, m)
		}
	}
	return out
}

func unionMACs(a, b []string) []string {
	seen := make(map[string]struct{}, len(a)+len(b))
	out := make([]string, 0, len(a)+len(b))
	for _, m := range a {
		if _, ok := seen[m]; !ok {
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	for _, m := range b {
		if _, ok := seen[m]; !ok {
			seen[m] = struct{}{}
			out = append(out, m)
		}
	}
	return out
}

func subtractMACs(a, deny []string) []string {
	blocked := make(map[string]struct{}, len(deny))
	for _, m := range deny {
		blocked[m] = struct{}{}
	}
	out := make([]string, 0, len(a))
	for _, m := range a {
		if _, ok := blocked[m]; !ok {
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
	macs := append([]string{}, g.macs...)
	sort.Strings(macs)
	return mode + ":" + strings.Join(macs, ",")
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

func iptDeleteJumpLines(cmd, table, parent, logMsg string, nums []int) {
	for i := len(nums) - 1; i >= 0; i-- {
		runLogged(logMsg, cmd, "-w", "-t", table, "-D", parent, strconv.Itoa(nums[i]))
	}
}

func iptDeleteJumpsTo(cmd, table, parent, target string) {
	nums := iptJumpLineNumbers(cmd, table, parent, func(t string) bool { return t == target })
	iptDeleteJumpLines(cmd, table, parent, "routing: delete jump "+parent+"->"+target, nums)
}

func iptEmitGatedJump(cmd, table, parent, target string, insertTop bool, gate routeDeviceGate) {
	op := "-A"
	var pos []string
	if insertTop {
		op = "-I"
		pos = []string{"1"}
	}
	emit := func(macMatch ...string) {
		args := append([]string{cmd, "-w", "-t", table, op, parent}, pos...)
		args = append(args, macMatch...)
		args = append(args, "-j", target)
		runLogged("routing: add jump "+parent+"->"+target, args...)
	}
	if gate.isWhitelist() {
		for _, mac := range gate.macs {
			emit("-m", "mac", "--mac-source", mac)
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
	emit := func(macMatch ...string) {
		args := []string{"nft", op, "rule", "inet", routeNftTable, parent}
		args = append(args, macMatch...)
		args = append(args, "jump", target)
		runLogged("routing: add jump "+parent+"->"+target, args...)
	}
	if gate.isWhitelist() {
		for _, mac := range gate.macs {
			emit("ether", "saddr", strings.ToLower(mac))
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
		for _, mac := range gate.macs {
			runLogged("routing: device blacklist skip "+chain,
				"nft", "add", "rule", "inet", routeNftTable, chain,
				"ether", "saddr", strings.ToLower(mac), "return")
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
		for _, mac := range gate.macs {
			runLogged("routing: device blacklist skip "+chain,
				cmd, "-w", "-t", table, "-A", chain,
				"-m", "mac", "--mac-source", mac, "-j", "RETURN")
		}
	}
}

func routeEnsureGatedPreJump(be routeBackend, chain string, gate routeDeviceGate) {
	if !gate.enabled {
		be.ensureJumpRule("PREROUTING", chain, true)
		return
	}
	if be.name() == backendNFTables {
		deleteNftJumpRules(routeNftTable, routeNftPrerouting, chain)
		nftEmitGatedJump(routeNftPrerouting, chain, false, gate)
		return
	}
	if ib, ok := be.(*routeIptBackend); ok {
		for _, cmd := range ib.iptBoth() {
			if !hasBinary(cmd) {
				continue
			}
			iptDeleteJumpsTo(cmd, "mangle", "PREROUTING", chain)
			iptEmitGatedJump(cmd, "mangle", "PREROUTING", chain, false, gate)
		}
		return
	}
	be.ensureJumpRule("PREROUTING", chain, true)
}
