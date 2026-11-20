package tables

import (
	"strconv"
	"strings"
)

const routeIPv6DefaultMetric = "1024"

func routeSweepOwnRules() {
	if !hasBinary("ip") {
		return
	}
	for _, fam := range routeIPFamilyArgs() {
		ipv6 := len(fam) > 0
		show := append([]string{"ip"}, fam...)
		show = append(show, "rule", "show")
		out, err := run(show...)
		if err != nil {
			continue
		}
		rulesSeen := make(map[string]struct{})
		tablesSeen := make(map[string]struct{})
		lines := strings.Split(out, "\n")
		targets := make(map[string][]string)
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if lookup := routeRuleField(line, "lookup"); lookup != "" {
				targets[lookup] = append(targets[lookup], line)
			}
		}
		for _, line := range lines {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			if !ipv6 && routeRuleIsSourceCheck(line) {
				mark := routeRuleField(line, "fwmark")
				if _, done := rulesSeen[mark+" main"]; !done {
					rulesSeen[mark+" main"] = struct{}{}
					routeDelSourceCheckRule(mark)
				}
				continue
			}
			if !routeRuleIsOwn(line) {
				continue
			}
			mark := routeRuleField(line, "fwmark")
			table := routeRuleField(line, "lookup")
			if mark == "" || table == "" {
				continue
			}
			if _, done := tablesSeen[table]; !done {
				tablesSeen[table] = struct{}{}
				prio, _ := routeRulePriority(line)
				n, _ := strconv.Atoi(table)
				shared := len(routeRuleRefsIn(targets, n)) > 0
				routeSweepOwnRoutes(fam, prio == proxyRulePriority, shared, table)
			}
			rule := mark + " " + table
			if _, done := rulesSeen[rule]; !done {
				rulesSeen[rule] = struct{}{}
				routeDelRuleLoop(ipv6, mark, table)
			}
		}
	}
}

func routeSweepOwnRoutes(fam []string, proxy, shared bool, table string) {
	base := append([]string{"ip"}, fam...)
	if proxy {
		local := "0.0.0.0/0"
		if len(fam) > 0 {
			local = "::/0"
		}
		del := append(append([]string{}, base...), "route", "del", "local", local, "dev", "lo", "table", table)
		_, _ = run(del...)
		return
	}
	show := append(append([]string{}, base...), "route", "show", "table", table)
	out, err := run(show...)
	if err != nil {
		return
	}
	tagged := routeIPSupportsProto()
	ownProto := routeProtoArgs()
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		dev := routeRuleField(line, "dev")
		match := ownProto
		switch {
		case fields[0] == "default" && !tagged:
			continue
		case routeLineBelongsToIface(line, dev):
		case fields[0] == "default" && shared:
			continue
		case tagged && routeLineIsUntaggedLeftover(line, len(fam) > 0):
			match = nil
			if metric := routeRuleField(line, "metric"); fields[0] == "default" && metric != "" {
				match = []string{"metric", metric}
			}
		default:
			continue
		}
		del := append(append([]string{}, base...), "route", "del")
		switch fields[0] {
		case "blackhole":
			del = append(del, "blackhole", "default", "metric", routeKillSwitchMetric)
		case "default":
			if dev == "" {
				continue
			}
			del = append(del, "default", "dev", dev)
		default:
			continue
		}
		del = append(del, match...)
		del = append(del, "table", table)
		_, _ = run(del...)
	}
}

func routeLineIsUntaggedLeftover(line string, v6 bool) bool {
	fields := strings.Fields(line)
	if len(fields) < 2 || routeRuleField(line, "proto") != "" {
		return false
	}
	metric := routeRuleField(line, "metric")
	switch fields[0] {
	case "blackhole":
		return fields[1] == "default" && metric == routeKillSwitchMetric
	case "default":
		return routeRuleField(line, "dev") != "" && (metric == "" || v6 && metric == routeIPv6DefaultMetric)
	}
	return false
}
