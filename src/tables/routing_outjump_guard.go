package tables

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func routeIsOutChainName(target string) bool {
	return strings.HasPrefix(target, routeChainPrefix) && strings.HasSuffix(target, "_out")
}

func iptListOutput(cmd string) (string, bool) {
	out, err := run(cmd, "-w", "-t", "mangle", "-L", "OUTPUT", "--line-numbers", "-n")
	if err != nil {
		return "", false
	}
	return out, true
}

func iptOutGuard(listing string, queueMark uint32) int {
	want := fmt.Sprintf("mark match 0x%x/0x%x", queueMark, queueMark)
	for _, line := range strings.Split(listing, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[1] != "ACCEPT" {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil || n <= 0 {
			continue
		}
		if strings.Contains(strings.ToLower(line), want) {
			return n
		}
	}
	return 0
}

func iptOutJumpsDisplaced(listing string, queueMark uint32) bool {
	guard := iptOutGuard(listing, queueMark)
	if guard == 0 {
		return false
	}
	for _, r := range iptListedRules(listing) {
		if routeIsOutChainName(r.target) && r.n > guard {
			return true
		}
	}
	return false
}

func iptOutJumpTargets(listing string) []string {
	var targets []string
	seen := map[string]bool{}
	for _, r := range iptListedRules(listing) {
		if routeIsOutChainName(r.target) && !seen[r.target] {
			seen[r.target] = true
			targets = append(targets, r.target)
		}
	}
	return targets
}

func routeEnsureOutJumpPrecedence(be routeBackend, cfg *config.Config) {
	ib, ok := be.(*routeIptBackend)
	if !ok || cfg == nil {
		return
	}
	queueMark := routeQueueBypassMark(cfg)
	for _, cmd := range ib.iptBoth() {
		if !hasBinary(cmd) {
			continue
		}
		listing, readable := iptListOutput(cmd)
		if !readable || !iptOutJumpsDisplaced(listing, queueMark) {
			continue
		}
		log.Infof("Routing: %s lists b4's queue-mark ACCEPT above a set's jump in mangle OUTPUT, so the packets b4 injects for the set's destinations would leave by the main route instead of the set's egress; lifting the sets' jumps back above it", cmd)
		targets := iptOutJumpTargets(listing)
		for i := len(targets) - 1; i >= 0; i-- {
			chain := targets[i]
			standing := iptJumpLineNumbers(cmd, "mangle", "OUTPUT", func(t string) bool { return t == chain })
			if len(standing) == 0 {
				continue
			}
			if !runLogged("routing: lift jump OUTPUT->"+chain, cmd, "-w", "-t", "mangle", "-I", "OUTPUT", "1", "-j", chain) {
				continue
			}
			iptDropJumpsAt(cmd, "mangle", "OUTPUT", iptShiftedBy(standing, 1, 1))
		}
	}
}
