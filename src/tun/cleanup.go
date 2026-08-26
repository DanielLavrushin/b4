package tun

import (
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func ClearStaleArtifacts(cfg *config.Config) {
	device := cfg.Queue.TUN.DeviceName
	if device == "" {
		device = defaultDeviceName
	}
	routeTable := cfg.Queue.TUN.RouteTable
	if routeTable == 0 {
		routeTable = defaultRouteTable
	}
	captureTable := routeTable - 1
	if captureTable <= 0 {
		captureTable = routeTable + 1
	}

	cleared := false

	for _, base := range []string{"PREROUTING", "OUTPUT"} {
		for _, ch := range []string{tunGateChain, tunCaptureChain} {
			for {
				if _, err := run("iptables", "-t", "mangle", "-D", base, "-j", ch); err != nil {
					break
				}
				cleared = true
			}
		}
	}
	run("iptables", "-t", "mangle", "-F", tunGateChain)
	run("iptables", "-t", "mangle", "-X", tunGateChain)
	run("iptables", "-t", "mangle", "-F", tunCaptureChain)
	run("iptables", "-t", "mangle", "-X", tunCaptureChain)

	for _, mk := range []string{reinjectPlainMarkMatch(), reinjectMarkMatch(), clientMarkMatch()} {
		for {
			if _, err := run("iptables", "-t", "raw", "-D", "OUTPUT", "-m", "mark", "--mark", mk, "-j", "CT", "--notrack"); err != nil {
				break
			}
			cleared = true
		}
	}

	for _, dir := range []string{"-i", "-o"} {
		for _, target := range []string{tunForwardChain, "ACCEPT"} {
			for {
				if _, err := run("iptables", "-D", "FORWARD", dir, device, "-j", target); err != nil {
					break
				}
				cleared = true
			}
		}
	}
	run("iptables", "-t", "filter", "-F", tunForwardChain)
	run("iptables", "-t", "filter", "-X", tunForwardChain)

	if clearTunSNAT(device) {
		cleared = true
	}

	if clearOwnedRoutingTable(captureTable) {
		cleared = true
	}
	if clearOwnedRoutingTable(routeTable) {
		cleared = true
	}

	if interfaceExists(device) && isTunDevice(device) {
		run("ip", "link", "del", device)
		cleared = true
	}

	if cleared {
		log.Infof("TUN: cleared stale TUN-engine artifacts left by a previous run")
	}
}

func clearTunSNAT(device string) bool {
	out, err := run("iptables", "-t", "nat", "-S", "POSTROUTING")
	if err != nil {
		return false
	}
	cleared := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-A POSTROUTING") {
			continue
		}
		target := ruleFieldValue(line, "-j")
		if ruleFieldValue(line, "-o") != device || (target != "SNAT" && target != tunSNATChain) {
			continue
		}
		spec := strings.Fields(strings.TrimPrefix(line, "-A POSTROUTING"))
		if _, err := run(append([]string{"iptables", "-t", "nat", "-D", "POSTROUTING"}, spec...)...); err == nil {
			cleared = true
		}
	}
	run("iptables", "-t", "nat", "-F", tunSNATChain)
	run("iptables", "-t", "nat", "-X", tunSNATChain)
	return cleared
}

func clearOwnedRoutingTable(table int) bool {
	tableStr := strconv.Itoa(table)
	out, err := run("ip", "rule", "show")
	if err != nil {
		return false
	}
	marks := make(map[string]struct{})
	for _, line := range strings.Split(out, "\n") {
		if ruleFieldValue(line, "lookup") != tableStr {
			continue
		}
		if fw := ruleFieldValue(line, "fwmark"); fw != "" {
			marks[fw] = struct{}{}
		}
	}
	if len(marks) == 0 {
		return false
	}
	run("ip", "route", "flush", "table", tableStr)
	for fw := range marks {
		for {
			if _, err := run("ip", "rule", "del", "fwmark", fw, "lookup", tableStr); err != nil {
				break
			}
		}
	}
	return true
}
