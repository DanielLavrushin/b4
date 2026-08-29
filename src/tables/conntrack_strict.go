package tables

import (
	"strings"

	"github.com/daniellavrushin/b4/log"
)

const conntrackLiberalSysctl = "net.netfilter.nf_conntrack_tcp_be_liberal"

var conntrackStrictBites bool

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
