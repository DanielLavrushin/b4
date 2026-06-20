package tun

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func ClearStaleArtifacts(cfg *config.Config) {
	if _, err := exec.LookPath("iptables"); err != nil {
		return
	}

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
		for {
			if _, err := run("iptables", "-t", "mangle", "-D", base, "-j", tunCaptureChain); err != nil {
				break
			}
			cleared = true
		}
	}
	run("iptables", "-t", "mangle", "-F", tunCaptureChain)
	run("iptables", "-t", "mangle", "-X", tunCaptureChain)

	for {
		if _, err := run("iptables", "-t", "raw", "-D", "OUTPUT", "-m", "mark", "--mark", reinjectMarkMatch(), "-j", "CT", "--notrack"); err != nil {
			break
		}
		cleared = true
	}

	for _, dir := range []string{"-i", "-o"} {
		for {
			if _, err := run("iptables", "-D", "FORWARD", dir, device, "-j", "ACCEPT"); err != nil {
				break
			}
			cleared = true
		}
	}

	if clearTunSNAT(device) {
		cleared = true
	}

	if clearOwnedRoutingTable(steerMarkMatch(), captureTable) {
		cleared = true
	}
	if clearOwnedRoutingTable(fmt.Sprintf("0x%x", cfg.MainInjectedMark()), routeTable) {
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

func steerMarkMatch() string {
	return fmt.Sprintf("0x%x/0x%x", defaultSteerMark, defaultSteerMark)
}

func clearTunSNAT(device string) bool {
	out, err := run("iptables", "-t", "nat", "-S", "POSTROUTING")
	if err != nil {
		return false
	}
	cleared := false
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "-A POSTROUTING") || !strings.Contains(line, "-o "+device) || !strings.Contains(line, "SNAT") {
			continue
		}
		spec := strings.Fields(strings.TrimPrefix(line, "-A POSTROUTING"))
		if _, err := run(append([]string{"iptables", "-t", "nat", "-D", "POSTROUTING"}, spec...)...); err == nil {
			cleared = true
		}
	}
	return cleared
}

func clearOwnedRoutingTable(fwmark string, table int) bool {
	tableStr := strconv.Itoa(table)
	out, err := run("ip", "rule", "show")
	if err != nil {
		return false
	}
	bare := strings.SplitN(fwmark, "/", 2)[0]
	owned := false
	for _, line := range strings.Split(out, "\n") {
		if ruleFieldValue(line, "lookup") != tableStr {
			continue
		}
		fw := ruleFieldValue(line, "fwmark")
		if fw == fwmark || fw == bare || strings.HasPrefix(fw, bare+"/") {
			owned = true
			break
		}
	}
	if !owned {
		return false
	}
	run("ip", "route", "flush", "table", tableStr)
	for {
		if _, err := run("ip", "rule", "del", "fwmark", fwmark, "lookup", tableStr); err != nil {
			break
		}
	}
	return true
}
