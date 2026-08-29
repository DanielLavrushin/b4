package tables

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	routeCTMarkMinSamples    = 8
	routeCTMarkConfirmations = 2
)

var (
	routeCTMarkHeld   atomic.Bool
	routeCTMarkSilent atomic.Int32
	routeCTMarkLoaded atomic.Bool
	routeCTMarkFile   atomic.Value
)

const routeCTMarkStateName = ".conntrack-mark"

func init() {
	routeCTMarkHeld.Store(true)
}

func routeCTMarkIsHeld() bool { return routeCTMarkHeld.Load() }

func routeCTMarkStatePath(cfg *config.Config) string {
	if cfg == nil || strings.TrimSpace(cfg.ConfigPath) == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(cfg.ConfigPath), routeCTMarkStateName)
}

// routeLoadCTMarkVerdict reads back what an earlier run found out about this
// router. Whether the box keeps a connection mark is a property of its firmware,
// not of one b4 process, and re-learning it costs every connection a set matches
// until the check has enough to go on.
func routeLoadCTMarkVerdict(cfg *config.Config) {
	path := routeCTMarkStatePath(cfg)
	if path == "" {
		return
	}
	routeCTMarkFile.Store(path)
	if !routeCTMarkLoaded.CompareAndSwap(false, true) {
		return
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return
	}
	if strings.TrimSpace(string(b)) == "0" {
		routeCTMarkHeld.Store(false)
		log.Infof("Routing: an earlier run found that this router does not keep the connection mark b4 writes, so a set marks every packet it matches rather than only the first")
	}
}

func routeSaveCTMarkVerdict() {
	path, _ := routeCTMarkFile.Load().(string)
	if path == "" {
		return
	}
	if err := os.WriteFile(path, []byte("0\n"), 0o600); err != nil {
		log.Tracef("Routing: could not remember the connection-mark verdict in %s: %v", path, err)
	}
}

func routeForgetCTMarkVerdict() {
	routeCTMarkHeld.Store(true)
	routeCTMarkSilent.Store(0)
	routeCTMarkLoaded.Store(false)
}

func routeCountChainHits(out string, want func(line string) bool) (uint64, bool) {
	var total uint64
	seen := false
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if !want(line) {
			continue
		}
		total += n
		seen = true
	}
	return total, seen
}

// routeCheckCTMarkFromDump reads the verdict out of the table listing the routing
// check already fetched, rather than going back to the kernel for one chain.
func routeCheckCTMarkFromDump(chains map[string]iptChainInfo) {
	if !routeCTMarkHeld.Load() {
		return
	}
	for name, info := range chains {
		if !strings.HasPrefix(name, "b4r_") || !strings.HasSuffix(name, "_pre") {
			continue
		}
		routeCheckCTMarkIn(strings.Join(info.rules, "\n"))
	}
}

func routeCheckCTMarkIn(out string) {
	if !routeCTMarkHeld.Load() {
		return
	}

	claimed, sawClaim := routeCountChainHits(out, func(l string) bool {
		return strings.Contains(l, "CONNMARK") && strings.Contains(l, "ctstate NEW")
	})
	restored, sawRestore := routeCountChainHits(out, func(l string) bool {
		return strings.Contains(l, "CONNMARK") && strings.Contains(l, "restore")
	})
	if !sawClaim || !sawRestore {
		return
	}
	if restored > 0 {
		routeCTMarkSilent.Store(0)
		return
	}
	if claimed < routeCTMarkMinSamples {
		return
	}
	if routeCTMarkSilent.Add(1) < routeCTMarkConfirmations {
		return
	}

	if routeCTMarkHeld.CompareAndSwap(true, false) {
		routeSaveCTMarkVerdict()
		log.Warnf("Routing: this router does not keep the connection mark b4 writes. %d connections were claimed for a set and not one packet after the first came back carrying that claim, which means something else on the box owns the connection mark and overwrites it. A connection would leave by the set's interface and finish by the ordinary uplink, so b4 marks every packet the set matches instead of only the first", claimed)
	}
}
