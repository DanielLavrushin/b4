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
			flush := append([]string{"ip"}, fam...)
			flush = append(flush, "route", "flush", "table", table)
			_, _ = run(flush...)
		}
	}
}
