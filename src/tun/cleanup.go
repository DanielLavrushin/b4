package tun

import (
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func ClearStaleArtifacts(cfg *config.Config) {
	device := cfg.Queue.TUN.DeviceName
	if device == "" {
		device = defaultDeviceName
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
	destroyDupSet()

	for _, markStr := range []string{reinjectMarkMatch(), clientMarkMatch()} {
		for {
			if _, err := run("iptables", "-t", "raw", "-D", "OUTPUT", "-m", "mark", "--mark", markStr, "-j", "CT", "--notrack"); err != nil {
				break
			}
			cleared = true
		}
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

	if sweepTunPolicyRouting(cfg.Queue.TUN.RouteTable, cfg.Queue.Mark) > 0 {
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
		spec, ok := ownTunNatRule(line, device)
		if !ok {
			continue
		}
		if _, err := run(append([]string{"iptables", "-t", "nat", "-D", "POSTROUTING"}, spec...)...); err == nil {
			cleared = true
		}
	}
	return cleared
}

func ownTunNatRule(line, device string) ([]string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, "-A POSTROUTING ") {
		return nil, false
	}
	spec := strings.Fields(strings.TrimPrefix(line, "-A POSTROUTING"))
	if len(spec) < 4 || spec[0] != "-o" || spec[1] != device || spec[2] != "-j" {
		return nil, false
	}
	switch {
	case len(spec) == 4 && spec[3] == "ACCEPT":
		return spec, true
	case len(spec) == 6 && spec[3] == "SNAT" && spec[4] == "--to-source":
		return spec, true
	}
	return nil, false
}
