package tun

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/tables"
)

const (
	autoRouteTableFirst = 97
	autoTableFloor      = 61
	xrayUITproxyTable   = 77
	legacyRouteTable    = 9999
)

var legacyTunRuleMarks = []uint{0x80000, 0x100000, 0x800000}

func isReservedTable(table int) bool {
	return table <= 0 || (table >= 253 && table <= 255)
}

func captureTableFor(route int) int {
	if c := route - 1; !isReservedTable(c) {
		return c
	}
	c := route + 1
	for isReservedTable(c) {
		c++
	}
	return c
}

func pinnedSetTables(cfg *config.Config) []int {
	var out []int
	for _, set := range cfg.Sets {
		if set != nil && set.Routing.Table > 0 {
			out = append(out, set.Routing.Table)
		}
	}
	return out
}

type tableUsage struct {
	lookups []string
	pinned  map[int]bool
	name    func(int) (string, bool)
	routes  func(int) bool
}

func (u tableUsage) takenBy(table int) string {
	if isReservedTable(table) {
		return "reserved by the kernel"
	}
	if u.pinned[table] {
		return "pinned by a set's routing table"
	}
	if u.name != nil {
		if name, named := u.name(table); named {
			return fmt.Sprintf("named %q in rt_tables", name)
		}
	}
	id := strconv.Itoa(table)
	for _, l := range u.lookups {
		if l == id {
			return "looked up by an ip rule b4 did not add"
		}
	}
	if u.routes != nil && u.routes(table) {
		return "already holding routes"
	}
	return ""
}

func tableHoldsRoutes(table int) bool {
	out, err := run("ip", "route", "show", "table", strconv.Itoa(table))
	return err == nil && strings.TrimSpace(out) != ""
}

func readTableUsage(pinned []int) tableUsage {
	u := tableUsage{
		pinned: make(map[int]bool),
		name:   tables.RouteTableName,
		routes: tableHoldsRoutes,
	}
	if out, err := run("ip", "rule", "show"); err == nil {
		for _, line := range strings.Split(out, "\n") {
			if l := ruleFieldValue(line, "lookup"); l != "" {
				u.lookups = append(u.lookups, l)
			}
		}
	}
	for _, t := range pinned {
		u.pinned[t] = true
	}
	return u
}

func pickTunTables(explicit int, usesCapture bool, u tableUsage) (route, capture int, err error) {
	inUse := func(route int) int {
		if usesCapture {
			return captureTableFor(route)
		}
		return route
	}
	if explicit != 0 {
		t := inUse(explicit)
		if why := u.takenBy(t); why != "" {
			return 0, 0, fmt.Errorf("queue.tun.route_table %d puts TUN in routing table %d, which is %s; set a free id, or 0 to let b4 pick one", explicit, t, why)
		}
		return explicit, captureTableFor(explicit), nil
	}
	for route = autoRouteTableFirst; route-1 >= autoTableFloor; route-- {
		t := inUse(route)
		if t == xrayUITproxyTable || u.takenBy(t) != "" {
			continue
		}
		return route, captureTableFor(route), nil
	}
	return 0, 0, fmt.Errorf("every routing table from %d to %d is reserved or already in use; set queue.tun.route_table to a free table id", autoTableFloor, autoRouteTableFirst)
}

func (r *routeManager) pickTables() error {
	usesCapture := r.resolvedCapture == "ports"
	u := readTableUsage(r.pinnedTables)
	route, capture, err := pickTunTables(r.explicitTable, usesCapture, u)
	if err != nil {
		return err
	}
	r.routeTable, r.captureTable = route, capture
	if r.explicitTable != 0 {
		return nil
	}
	preferred := autoRouteTableFirst
	if usesCapture {
		preferred = captureTableFor(autoRouteTableFirst)
	}
	if used := r.activeTable(); used != preferred {
		log.Infof("TUN: routing table %d is %s, so TUN uses table %d", preferred, u.takenBy(preferred), used)
	}
	return nil
}

func (r *routeManager) activeTable() int {
	if r.resolvedCapture == "ports" {
		return r.captureTable
	}
	return r.routeTable
}

func markMatches(fw string, mark uint) bool {
	want := fmt.Sprintf("0x%x", mark)
	value, mask, masked := strings.Cut(fw, "/")
	return value == want && (!masked || mask == want)
}

func isTunRuleMark(fw string) bool {
	for _, m := range []uint{engine.TunSteerMark, engine.ClientMark, engine.ReinjectMarkBit} {
		if markMatches(fw, m) {
			return true
		}
	}
	return false
}

func isLegacyTunRuleMark(fw string, queueMark uint) bool {
	if queueMark != 0 && markMatches(fw, queueMark) {
		return true
	}
	for _, m := range legacyTunRuleMarks {
		if markMatches(fw, m) {
			return true
		}
	}
	return false
}

func isTunRulePriority(prio int) bool {
	switch prio {
	case clientLocalPrio, clientBypassPrio, reinjectLocalPrio, bypassRulePrio:
		return true
	}
	return prio >= capturePrioFloor && prio <= defaultCapturePrio
}

var narrowingRuleSelectors = map[string]bool{
	"not": true, "iif": true, "oif": true, "to": true, "tos": true, "dsfield": true,
	"ipproto": true, "sport": true, "dport": true, "uidrange": true, "l3mdev": true, "tun_id": true,
}

func ruleMatchesAllTraffic(fields []string) bool {
	if len(fields) < 3 || fields[1] != "from" || fields[2] != "all" {
		return false
	}
	for _, f := range fields[3:] {
		if narrowingRuleSelectors[f] {
			return false
		}
	}
	return true
}

func isBuiltinTable(lookup string) bool {
	if n, err := strconv.Atoi(lookup); err == nil {
		return isReservedTable(n)
	}
	switch lookup {
	case "main", "local", "default", "unspec":
		return true
	}
	return false
}

func ownedTunTables(explicit int) map[string]bool {
	owned := map[string]bool{
		strconv.Itoa(legacyRouteTable):                  true,
		strconv.Itoa(captureTableFor(legacyRouteTable)): true,
	}
	if explicit > 0 && !isReservedTable(explicit) {
		owned[strconv.Itoa(explicit)] = true
		owned[strconv.Itoa(captureTableFor(explicit))] = true
	}
	return owned
}

func isStaleTunRule(line string, owned map[string]bool, queueMark uint) bool {
	fw := ruleFieldValue(line, "fwmark")
	lookup := ruleFieldValue(line, "lookup")
	if fw == "" || lookup == "" || !ruleMatchesAllTraffic(strings.Fields(line)) {
		return false
	}
	if owned[lookup] {
		return isTunRuleMark(fw) || isLegacyTunRuleMark(fw, queueMark)
	}
	prio, ok := rulePriority(line)
	if !ok || !isTunRulePriority(prio) || !isTunRuleMark(fw) {
		return false
	}
	if lookup == "main" {
		return strings.Contains(line, "suppress_prefixlength 0")
	}
	return !isBuiltinTable(lookup)
}

type staleRule struct {
	table string
	args  []string
}

func planTunRuleSweep(ruleShow string, owned map[string]bool, queueMark uint) (dels []staleRule, flush []string) {
	doomed := make(map[string]bool)
	var kept []string
	for _, line := range strings.Split(ruleShow, "\n") {
		lookup := ruleFieldValue(line, "lookup")
		if lookup == "" {
			continue
		}
		if !isStaleTunRule(line, owned, queueMark) {
			kept = append(kept, lookup)
			continue
		}
		args := []string{"ip", "rule", "del"}
		if prio, ok := rulePriority(line); ok {
			args = append(args, "priority", strconv.Itoa(prio))
		}
		args = append(args, "fwmark", ruleFieldValue(line, "fwmark"), "lookup", lookup)
		dels = append(dels, staleRule{table: lookup, args: args})
		if !isBuiltinTable(lookup) {
			doomed[lookup] = true
		}
	}
	for _, l := range kept {
		delete(doomed, l)
	}
	for l := range doomed {
		flush = append(flush, l)
	}
	sort.Strings(flush)
	return dels, flush
}

func sweepTunPolicyRouting(explicit int, queueMark uint) int {
	out, err := run("ip", "rule", "show")
	if err != nil {
		return 0
	}
	dels, flush := planTunRuleSweep(out, ownedTunTables(explicit), queueMark)
	failed := make(map[string]bool)
	removed := 0
	for _, d := range dels {
		if _, err := run(d.args...); err != nil {
			failed[d.table] = true
			log.Tracef("TUN: stale policy rule not removed: %v", err)
			continue
		}
		removed++
	}
	if len(flush) == 0 {
		return removed
	}
	after, err := run("ip", "rule", "show")
	if err != nil {
		return removed
	}
	stillLooked := make(map[string]bool)
	for _, line := range strings.Split(after, "\n") {
		if l := ruleFieldValue(line, "lookup"); l != "" {
			stillLooked[l] = true
		}
	}
	for _, table := range flush {
		if failed[table] || stillLooked[table] {
			continue
		}
		if _, err := run("ip", "route", "flush", "table", table); err != nil {
			log.Tracef("TUN: route table %s not flushed: %v", table, err)
		}
	}
	return removed
}

func routingError(what string, table int, err error) error {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "to 'fwmark'"):
		return fmt.Errorf("%s: this 'ip' cannot match a masked fwmark, which busybox supports only from 1.33; install full iproute2 (ip-full on Entware and OpenWrt, iproute2 on Alpine): %w", what, err)
	case strings.Contains(msg, "to 'table"):
		return fmt.Errorf("%s: this 'ip' does not accept table %d (busybox takes at most 1023, and releases before 1.26 mishandle ids above 255); set queue.tun.route_table to 0 so b4 picks a table: %w", what, table, err)
	case strings.Contains(msg, "Usage: ip"):
		return fmt.Errorf("%s: this busybox 'ip' was built without 'ip rule'; install full iproute2 (ip-full on Entware and OpenWrt, iproute2 on Alpine): %w", what, err)
	}
	return fmt.Errorf("%s: %w", what, err)
}
