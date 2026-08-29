package tables

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/log"
)

var (
	rtTablesFile = "/etc/iproute2/rt_tables"
	rtTablesDir  = "/etc/iproute2/rt_tables.d"
)

var routeRtTableNames = routeRtTableNamesExec

var (
	rtNamesMu     sync.Mutex
	rtNamesCache  map[int]string
	rtNamesLoaded bool
)

func routeRtTableNamesCached() map[int]string {
	rtNamesMu.Lock()
	defer rtNamesMu.Unlock()
	if !rtNamesLoaded {
		rtNamesCache = routeRtTableNames()
		rtNamesLoaded = true
	}
	return rtNamesCache
}

func routeForgetRtTableNames() {
	rtNamesMu.Lock()
	rtNamesCache = nil
	rtNamesLoaded = false
	rtNamesMu.Unlock()
}

func routeRtTableNamesExec() map[int]string {
	names := make(map[int]string)
	files := []string{rtTablesFile}
	if entries, err := os.ReadDir(rtTablesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".conf") {
				continue
			}
			files = append(files, filepath.Join(rtTablesDir, e.Name()))
		}
	}
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		routeParseRtTables(string(data), names)
	}
	return names
}

func routeParseRtTables(data string, into map[int]string) {
	for _, line := range strings.Split(data, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		id, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		into[id] = fields[1]
	}
}

func routeTableName(table int) (string, bool) {
	name, ok := routeRtTableNamesCached()[table]
	return name, ok
}

func routeRuleField(line, key string) string {
	fields := strings.Fields(line)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == key {
			return fields[i+1]
		}
	}
	return ""
}

func routeRuleLookupTargets() map[string][]string {
	targets := make(map[string][]string)
	for _, fam := range routeIPFamilyArgs() {
		args := append([]string{"ip"}, fam...)
		args = append(args, "rule", "show")
		out, err := run(args...)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if lookup := routeRuleField(line, "lookup"); lookup != "" {
				targets[lookup] = append(targets[lookup], line)
			}
		}
	}
	return targets
}

func routeTableRuleRefs(table int) []string {
	return routeRuleRefsIn(routeRuleLookupTargets(), table)
}

func routeRuleRefsIn(targets map[string][]string, table int) []string {
	var refs []string
	add := func(lines []string) {
		for _, line := range lines {
			if routeRuleIsOwn(line) {
				continue
			}
			refs = append(refs, line)
		}
	}
	add(targets[strconv.Itoa(table)])
	if name, ok := routeTableName(table); ok {
		add(targets[name])
	}
	return refs
}

func routeRulePriority(line string) (int, bool) {
	fields := strings.Fields(line)
	if len(fields) == 0 {
		return 0, false
	}
	prio, err := strconv.Atoi(strings.TrimSuffix(fields[0], ":"))
	if err != nil {
		return 0, false
	}
	return prio, true
}

func routeRuleIsOwn(line string) bool {
	fw := routeRuleField(line, "fwmark")
	slash := strings.IndexByte(fw, '/')
	if slash < 0 {
		return false
	}
	mask, err := strconv.ParseUint(strings.TrimPrefix(fw[slash+1:], "0x"), 16, 32)
	if err != nil || uint32(mask) != routeSetMarkMask {
		return false
	}
	prio, ok := routeRulePriority(line)
	if !ok {
		return false
	}
	if prio == proxyRulePriority {
		return true
	}
	table, err := strconv.Atoi(routeRuleField(line, "lookup"))
	if err != nil {
		return false
	}
	return prio == routePolicyRuleBase+table
}

func routeIPFamilyArgs() [][]string {
	return [][]string{nil, {"-6"}}
}

func routeLineIsProxyLocal(line string) bool {
	fields := strings.Fields(line)
	if len(fields) < 2 || fields[0] != "local" {
		return false
	}
	switch fields[1] {
	case "default", "0.0.0.0/0", "::/0":
	default:
		return false
	}
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "dev" {
			return fields[i+1] == "lo"
		}
	}
	return false
}

func routeTablesHoldingRoutes() map[string]bool {
	held := make(map[string]bool)
	for _, fam := range routeIPFamilyArgs() {
		args := append([]string{"ip"}, fam...)
		args = append(args, "route", "show", "table", "all")
		out, err := run(args...)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || routeLineIsProxyLocal(line) {
				continue
			}
			if tbl := routeRuleField(line, "table"); tbl != "" {
				held[tbl] = true
			}
		}
	}
	return held
}

func routeTableHoldsForeignRoutes(table int) bool {
	return routeTableHeldIn(routeTablesHoldingRoutes(), table)
}

func routeTableHeldIn(held map[string]bool, table int) bool {
	if held[strconv.Itoa(table)] {
		return true
	}
	if name, ok := routeTableName(table); ok {
		return held[name]
	}
	return false
}

const (
	proxyTableSpareFirst = 300
	proxyTableSpareLast  = 399
)

func routeProxyTableCandidates() []int {
	out := make([]int, 0, 3+(proxyTableSpareLast-proxyTableSpareFirst+1))
	out = append(out, proxyLocalDeliveryTable, proxyLocalDeliveryTable-1, proxyLocalDeliveryTable-2)
	for t := proxyTableSpareFirst; t <= proxyTableSpareLast; t++ {
		out = append(out, t)
	}
	return out
}

var routePickProxyTable = routePickProxyTableExec

func routePickProxyTableExec() int {
	if !hasBinary("ip") {
		return proxyLocalDeliveryTable
	}
	targets := routeRuleLookupTargets()
	held := routeTablesHoldingRoutes()

	for _, table := range routeProxyTableCandidates() {
		preferred := table == proxyLocalDeliveryTable
		note := func(format string, args ...any) {
			if preferred {
				log.Warnf(format, args...)
				return
			}
			log.Tracef(format, args...)
		}

		if name, named := routeTableName(table); named {
			note("Routing: routing table %d is named %q in %s, so b4 will not claim it for transparent proxying; the local-delivery route it installs there would swallow every rule that already looks up %q",
				table, name, rtTablesFile, name)
			continue
		}
		if refs := routeRuleRefsIn(targets, table); len(refs) > 0 {
			note("Routing: routing table %d is already the target of %d routing rule(s) b4 did not add (%s), so it is not free for transparent proxying",
				table, len(refs), strings.Join(refs, "; "))
			continue
		}
		if routeTableHeldIn(held, table) {
			note("Routing: routing table %d already holds routes b4 did not add, so it is not free for transparent proxying", table)
			continue
		}
		if !preferred {
			log.Infof("Routing: transparent proxying uses routing table %d because %d is in use", table, proxyLocalDeliveryTable)
		}
		return table
	}

	log.Errorf("Routing: every routing table b4 could use for transparent proxying is already taken, so sets with an upstream proxy install no rules; free one of %d-%d or %d-%d (see %s and 'ip rule show')",
		proxyLocalDeliveryTable-2, proxyLocalDeliveryTable, proxyTableSpareFirst, proxyTableSpareLast, rtTablesFile)
	return 0
}

func RouteTableName(table int) (string, bool) {
	return routeTableName(table)
}

func ProxyRulePriority() int { return proxyRulePriority }
