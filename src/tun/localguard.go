package tun

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
)

func isLocalLookup(lookup string) bool {
	return lookup == "local" || lookup == "255"
}

func isLocalGuardPriority(prio int) bool {
	return prio >= capturePrioFloor-1 && prio <= defaultCapturePrio-1
}

func localLookupPriority(ruleShow string) (int, bool) {
	lowest, found := 0, false
	for _, line := range strings.Split(ruleShow, "\n") {
		if !isLocalLookup(ruleFieldValue(line, "lookup")) || ruleFieldValue(line, "fwmark") != "" {
			continue
		}
		if !ruleMatchesAllTraffic(strings.Fields(line)) {
			continue
		}
		prio, ok := rulePriority(line)
		if !ok {
			continue
		}
		if !found || prio < lowest {
			lowest, found = prio, true
		}
	}
	return lowest, found
}

func localGuardNeeded(ruleShow string, capturePrio int) bool {
	prio, ok := localLookupPriority(ruleShow)
	return !ok || prio >= capturePrio
}

func localGuardPresent(ruleShow, steer string, prio int) bool {
	for _, line := range strings.Split(ruleShow, "\n") {
		p, ok := rulePriority(line)
		if !ok || p != prio || !isLocalLookup(ruleFieldValue(line, "lookup")) {
			continue
		}
		if ruleFieldValue(line, "fwmark") == steer && ruleMatchesAllTraffic(strings.Fields(line)) {
			return true
		}
	}
	return false
}

func describeLocalLookup(ruleShow string) string {
	if prio, ok := localLookupPriority(ruleShow); ok {
		return fmt.Sprintf("this system looks up its local routing table at ip rule priority %d", prio)
	}
	return "no ip rule looks up the local routing table for every packet"
}

func localLookupRuleArgs(action, mark string, prio int) []string {
	args := []string{"ip", "rule", action, "fwmark", mark, "lookup", "local"}
	if prio > 0 {
		args = append(args, "priority", strconv.Itoa(prio))
	}
	return args
}

func (r *routeManager) localGuardArgs(action string, prio int) []string {
	return localLookupRuleArgs(action, r.steerMarkStr(), prio)
}

func (r *routeManager) ensureLocalGuard() {
	out, err := run("ip", "rule", "show")
	if err != nil {
		return
	}
	r.syncLocalGuard(out)
}

func (r *routeManager) syncLocalGuard(ruleShow string) {
	if !localGuardNeeded(ruleShow, r.capturePrio) {
		return
	}
	prio := r.capturePrio - 1
	if localGuardPresent(ruleShow, r.steerMarkStr(), prio) {
		r.localGuardAdded = true
		return
	}
	where := describeLocalLookup(ruleShow)
	if _, err := run(r.localGuardArgs("add", prio)...); err != nil {
		if !r.localGuardWarned {
			log.Warnf("TUN: %s, after the capture rule at priority %d, and the rule that keeps packets for this device's own addresses out of %s could not be added (%v), so DNS answers to lookups this device makes itself do not arrive", where, r.capturePrio, r.tunName, err)
			r.localGuardWarned = true
		}
		return
	}
	r.localGuardWarned = false
	if r.localGuardAdded {
		log.Infof("TUN: reconcile restored the rule that looks up the local routing table ahead of the capture rule (ip rule priority %d)", prio)
		return
	}
	r.localGuardAdded = true
	log.Infof("TUN: %s, after the capture rule at priority %d, so packets for this device's own addresses would be steered into %s; added a rule at priority %d that looks up the local routing table first for the packets b4 steers", where, r.capturePrio, r.tunName, prio)
}

func (r *routeManager) removeLocalGuard() {
	for {
		if _, err := run(r.localGuardArgs("del", 0)...); err != nil {
			break
		}
	}
	r.localGuardAdded = false
}

type bypassGuard struct {
	mark   string
	before int
}

func bypassGuards() [2]bypassGuard {
	return [2]bypassGuard{
		{mark: clientMarkMatch(), before: clientLocalPrio},
		{mark: reinjectMarkMatch(), before: reinjectLocalPrio},
	}
}

func isBypassGuardRule(prio int, fw string) bool {
	return (prio == clientLocalPrio-1 && markMatches(fw, engine.ClientMark)) ||
		(prio == reinjectLocalPrio-1 && markMatches(fw, engine.ReinjectMarkBit))
}

func (r *routeManager) ensureBypassGuards() {
	out, err := run("ip", "rule", "show")
	if err != nil {
		return
	}
	r.syncBypassGuards(out)
}

func (r *routeManager) syncBypassGuards(ruleShow string) {
	for i, g := range bypassGuards() {
		if !localGuardNeeded(ruleShow, g.before) {
			continue
		}
		prio := g.before - 1
		if localGuardPresent(ruleShow, g.mark, prio) {
			r.bypassGuardAdded[i] = true
			continue
		}
		where := describeLocalLookup(ruleShow)
		if _, err := run(localLookupRuleArgs("add", g.mark, prio)...); err != nil {
			if !r.bypassGuardWarned[i] {
				log.Warnf("TUN: %s, after b4's rule at priority %d, and the rule that keeps b4's packets with mark %s for this device's own addresses on this device could not be added (%v), so they leave by %s instead", where, g.before, g.mark, err, r.outIface)
				r.bypassGuardWarned[i] = true
			}
			continue
		}
		r.bypassGuardWarned[i] = false
		if r.bypassGuardAdded[i] {
			log.Infof("TUN: reconcile restored the rule that looks up the local routing table for mark %s (ip rule priority %d)", g.mark, prio)
			continue
		}
		r.bypassGuardAdded[i] = true
		log.Infof("TUN: %s, after b4's rule at priority %d, so b4's packets with mark %s for this device's own addresses would leave by %s; added a rule at priority %d that looks up the local routing table first for them", where, g.before, g.mark, r.outIface, prio)
	}
}

func (r *routeManager) removeBypassGuards() {
	for _, g := range bypassGuards() {
		for {
			if _, err := run(localLookupRuleArgs("del", g.mark, 0)...); err != nil {
				break
			}
		}
	}
	r.bypassGuardAdded = [2]bool{}
}
