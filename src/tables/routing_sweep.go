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
			if _, done := tablesSeen[table]; done {
				continue
			}
			tablesSeen[table] = struct{}{}
			routeDelRuleLoop(ipv6, mark, table)
			flush := append([]string{"ip"}, fam...)
			flush = append(flush, "route", "flush", "table", table)
			_, _ = run(flush...)
		}
	}
}
