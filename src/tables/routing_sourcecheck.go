package tables

import (
	"os"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/daniellavrushin/b4/log"
)

const (
	proxySourceCheckRulePriority = 2
	proxySourceCheckMinPrefix    = 8
	srcValidMarkSysctl           = "/proc/sys/net/ipv4/conf/all/src_valid_mark"
)

var (
	routeAddSourceCheckRule = routeAddSourceCheckRuleExec
	routeDelSourceCheckRule = routeDelSourceCheckRuleExec
	routeSrcValidMarkOn     = routeSrcValidMarkOnExec
	routeSourceCheckWarned  atomic.Bool
)

func proxySourceCheckRuleArgs(verb, markStrMask string) []string {
	args := []string{"ip", "rule", verb, "fwmark", markStrMask, "iif", "lo", "lookup", "main"}
	if verb != "add" {
		return args
	}
	return append(args,
		"suppress_prefixlength", strconv.Itoa(proxySourceCheckMinPrefix-1),
		"priority", strconv.Itoa(proxySourceCheckRulePriority))
}

func routeSrcValidMarkOnExec() bool {
	b, err := os.ReadFile(srcValidMarkSysctl)
	return err == nil && strings.TrimSpace(string(b)) == "1"
}

func routeAddSourceCheckRuleExec(mark uint32) {
	out, err := run(proxySourceCheckRuleArgs("add", routeSetMarkRule(mark))...)
	if err == nil {
		return
	}
	reason := strings.TrimSpace(out)
	if reason == "" {
		reason = err.Error()
	}
	if !routeSrcValidMarkOn() {
		log.Tracef("routing: the source-check rule for mark 0x%x was rejected (%s); it only matters while net.ipv4.conf.all.src_valid_mark is 1", mark, reason)
		return
	}
	if routeSourceCheckWarned.Swap(true) {
		return
	}
	log.Warnf("Routing: net.ipv4.conf.all.src_valid_mark is 1, so the kernel checks where a connection diverted to a transparent-proxy set came from against that set's local-delivery table and drops it as a martian source; this iproute2 rejected the rule that sends that check to the main table (%s), so the Telegram bridge and sets with an upstream proxy get no traffic from LAN or VPN clients. Install full iproute2, or set net.ipv4.conf.all.src_valid_mark back to 0 if nothing on the router needs it", reason)
}

func routeDelSourceCheckRuleExec(markStrMask string) {
	for i := 0; i < 100; i++ {
		if _, err := run(proxySourceCheckRuleArgs("del", markStrMask)...); err != nil {
			return
		}
	}
}

func routeRuleIsSourceCheck(line string) bool {
	if routeRuleField(line, "iif") != "lo" || routeRuleField(line, "lookup") != "main" {
		return false
	}
	prio, ok := routeRulePriority(line)
	if !ok || prio != proxySourceCheckRulePriority {
		return false
	}
	fw := routeRuleField(line, "fwmark")
	slash := strings.IndexByte(fw, '/')
	if slash < 0 {
		return false
	}
	mask, err := strconv.ParseUint(strings.TrimPrefix(fw[slash+1:], "0x"), 16, 32)
	return err == nil && uint32(mask) == routeSetMarkMask
}
