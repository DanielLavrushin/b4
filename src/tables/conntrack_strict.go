package tables

import (
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const conntrackLiberalSysctl = "net.netfilter.nf_conntrack_tcp_be_liberal"

var conntrackStrictBites bool

var getSysctl = getSysctlOrProc

var skipSetupConntrackWarned atomic.Bool

func WarnSkipSetupStrictConntrack(cfg *config.Config) {
	if cfg == nil || !cfg.System.Tables.SkipSetup || cfg.Queue.IsDiscovery || cfg.Queue.Mode == "tun" || skipSetupConntrackWarned.Load() {
		return
	}
	sets := setsSendingMovedAckFakes(cfg)
	if len(sets) == 0 || strings.TrimSpace(getSysctl(conntrackLiberalSysctl)) != "0" {
		return
	}
	if !skipSetupConntrackWarned.CompareAndSwap(false, true) {
		return
	}
	log.Warnf("TABLES: --skip-tables leaves %s at 0, so this router tracks TCP windows strictly. The TCP Check fakes, and the TCP Timestamp fakes on connections without TCP timestamps, of sets %s acknowledge data the server never sent, and strict tracking marks them invalid: NAT does not translate them, and a rule that drops invalid packets on the router's output path drops them, so they may never reach the DPI. Set %s=1, as b4 does without --skip-tables, or pick another fake strategy for these sets", conntrackLiberalSysctl, strings.Join(sets, ", "), conntrackLiberalSysctl)
}

func setsSendingMovedAckFakes(cfg *config.Config) []string {
	var names []string
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled || !set.Faking.SNI {
			continue
		}
		switch set.Faking.Strategy {
		case "tcp_check", "timestamp":
			names = append(names, strconv.Quote(set.Name))
		}
	}
	return names
}

func forwardDropsInvalid() bool {
	for _, cmd := range []string{backendIPTables, backendIPTablesLegacy} {
		if !hasBinary(cmd) {
			continue
		}
		out, err := run(cmd, "-w", "-t", "filter", "-L", "FORWARD", "-n")
		if err != nil {
			continue
		}
		for _, line := range strings.Split(out, "\n") {
			if strings.HasPrefix(line, "DROP") && strings.Contains(line, "INVALID") {
				return true
			}
		}
	}
	return false
}

// conntrackStrictWouldBite reports whether putting this router's own strict TCP
// window tracking back would stop it forwarding. The router's firewall drops what
// strict tracking marks invalid, and on a box whose flow accelerator moves packets
// without keeping conntrack's window in step that is a large download stalling
// partway with no reset and no error.
func conntrackStrictWouldBite() bool {
	return conntrackStrictBites
}

func warnIfStrictConntrackBites(previous string) {
	if conntrackStrictBites || strings.TrimSpace(previous) != "0" {
		return
	}
	if !forwardDropsInvalid() {
		return
	}
	conntrackStrictBites = true
	log.Warnf("TABLES: this router tracks TCP windows strictly (%s is 0) and its own firewall drops what that marks invalid, which stalls a large download partway through with no reset and no error. b4 relaxes it while it runs, and keeps it relaxed when it stops rather than hand the network back in a state that cannot finish a download", conntrackLiberalSysctl)
}

func forgetConntrackStrictWarning() {
	conntrackStrictBites = false
}
