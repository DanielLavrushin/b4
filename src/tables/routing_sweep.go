package tables

import "strings"

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
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if line == "" || !routeRuleIsOwn(line) {
				continue
			}
			mark := routeRuleField(line, "fwmark")
			table := routeRuleField(line, "lookup")
			if mark == "" || table == "" {
				continue
			}
			rule := mark + " " + table
			if _, done := rulesSeen[rule]; !done {
				rulesSeen[rule] = struct{}{}
				routeDelRuleLoop(ipv6, mark, table)
			}
			if _, done := tablesSeen[table]; done {
				continue
			}
			tablesSeen[table] = struct{}{}
			prio, _ := routeRulePriority(line)
			routeSweepOwnRoutes(fam, prio == proxyRulePriority, table)
		}
	}
}

func routeSweepOwnRoutes(fam []string, proxy bool, table string) {
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
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		if fields[0] == "default" && !routeIPSupportsProto() {
			continue
		}
		dev := routeRuleField(line, "dev")
		if !routeLineBelongsToIface(line, dev) {
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
		del = append(del, routeProtoArgs()...)
		del = append(del, "table", table)
		_, _ = run(del...)
	}
}
