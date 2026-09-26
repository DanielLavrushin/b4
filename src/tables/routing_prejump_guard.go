package tables

import (
	"strconv"
	"strings"
)

type iptListedRule struct {
	n      int
	target string
	socket bool
}

func iptListedRules(out string) []iptListedRule {
	var rules []iptListedRule
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil || n <= 0 {
			continue
		}
		r := iptListedRule{n: n, target: f[1]}
		for _, x := range f[2:] {
			if x == "socket" {
				r.socket = true
				break
			}
		}
		rules = append(rules, r)
	}
	return rules
}

func iptIsOwnTarget(target string) bool {
	return strings.HasPrefix(target, routeChainPrefix) || strings.HasPrefix(target, "B4")
}

func (r iptListedRule) foreignSocketRule() bool {
	return r.socket && !iptIsOwnTarget(r.target)
}

func iptPreGuard(rules []iptListedRule) (int, string) {
	for _, r := range rules {
		if r.target == captureChainPre || r.foreignSocketRule() {
			return r.n, r.target
		}
	}
	return 0, ""
}

func iptListPrerouting(cmd string) ([]iptListedRule, bool) {
	out, err := run(cmd, "-w", "-t", "mangle", "-L", "PREROUTING", "--line-numbers", "-n")
	if err != nil {
		return nil, false
	}
	return iptListedRules(out), true
}

func iptPreJumpsDisplaced(cmd string) (bool, string) {
	rules, ok := iptListPrerouting(cmd)
	if !ok {
		return false, ""
	}
	guard, blocker := iptPreGuard(rules)
	if guard == 0 {
		return false, ""
	}
	for _, r := range rules {
		if routeIsPreChainName(r.target) && r.n > guard {
			return true, blocker
		}
	}
	return false, ""
}

func RoutingLiftShadowedJumps() {
	rulesMu.Lock()
	defer rulesMu.Unlock()
	routePhaseMu.Lock()
	defer routePhaseMu.Unlock()
	if cfg := routingSyncedConfig(); cfg != nil {
		RoutingEnsureJumpPrecedence(cfg)
	}
}

func RoutingPreJumpShadowedBy(setID string) string {
	routeMu.Lock()
	st, ok := routeRuleCache[setID]
	ib, isIpt := routeEngine.(*routeIptBackend)
	routeMu.Unlock()
	if !ok || !isIpt || st.chainPre == "" {
		return ""
	}
	for _, cmd := range ib.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		rules, readable := iptListPrerouting(cmd)
		if !readable {
			continue
		}
		foreign := 0
		var foreignTarget string
		for _, r := range rules {
			if foreign == 0 && r.foreignSocketRule() {
				foreign, foreignTarget = r.n, r.target
			}
			if r.target == st.chainPre && foreign > 0 && r.n > foreign {
				return foreignTarget
			}
		}
	}
	return ""
}
