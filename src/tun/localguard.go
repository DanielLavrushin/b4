package tun

import (
	"fmt"
	"strconv"
	"strings"

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

func (r *routeManager) localGuardArgs(action string, prio int) []string {
	args := []string{"ip", "rule", action, "fwmark", r.steerMarkStr(), "lookup", "local"}
	if prio > 0 {
		args = append(args, "priority", strconv.Itoa(prio))
	}
	return args
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
