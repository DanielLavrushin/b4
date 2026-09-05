package tables

import (
	"bufio"
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
	routeCTMarkHeld    atomic.Bool
	routeCTMarkSilent  atomic.Int32
	routeCTMarkLoaded  atomic.Bool
	routeCTMarkSettled atomic.Bool
	routeCTMarkFile    atomic.Value
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
	routeCTMarkSettled.Store(false)
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

func routeCheckCTMarkFromDump(chains map[string]iptChainInfo) {
	if !routeCTMarkHeld.Load() || routeCTMarkSettled.Load() {
		return
	}
	var all []string
	for name, info := range chains {
		if !strings.HasPrefix(name, "b4r_") || !strings.HasSuffix(name, "_pre") {
			continue
		}
		all = append(all, info.rules...)
	}
	if len(all) == 0 {
		return
	}
	routeCheckCTMarkIn(strings.Join(all, "\n"))
}

// routeCheckCTMarkIn takes one observation of every routed chain at once. Counting
// each chain on its own would let two quiet sets stand in for two quiet ticks and
// settle the question inside a single pass, which is the very thing the second
// confirmation exists to prevent.
func routeCheckCTMarkIn(out string) {
	if !routeCTMarkHeld.Load() || routeCTMarkSettled.Load() {
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
		routeCTMarkConfirm()
		return
	}
	if claimed < routeCTMarkMinSamples {
		return
	}

	tagged, answered, readable := conntrackKeepsRouteTag()
	if !readable || !answered {
		return
	}
	if tagged {
		routeCTMarkConfirm()
		return
	}
	if routeCTMarkSilent.Add(1) < routeCTMarkConfirmations {
		return
	}

	if routeCTMarkHeld.CompareAndSwap(true, false) {
		routeSaveCTMarkVerdict()
		log.Warnf("Routing: this router does not keep the connection mark b4 writes. b4 claimed %d connections for a set and not one connection that has answered still carries that claim, which means something else on the box owns the connection mark and overwrites it after b4 has written it. A connection would leave by the set's interface and finish by the ordinary uplink, so b4 marks every packet the set matches instead of only the first", claimed)
	}
}

func routeCTMarkConfirm() {
	routeCTMarkSilent.Store(0)
	routeCTMarkSettled.Store(true)
}

var conntrackPath = "/proc/net/nf_conntrack"

const conntrackUnreplied = "[UNREPLIED]"

func conntrackSawReply(line string) bool {
	return !strings.Contains(line, conntrackUnreplied)
}

func conntrackKeepsRouteTag() (tagged bool, answered bool, readable bool) {
	f, err := os.Open(conntrackPath)
	if err != nil {
		return false, false, false
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		if !conntrackSawReply(line) {
			continue
		}
		answered = true
		if m, ok := conntrackMarkOf(line); ok && m&uint64(hostRouteCTMark) != 0 {
			return true, true, true
		}
	}
	if sc.Err() != nil {
		return false, false, false
	}
	return false, answered, true
}

func conntrackMarkOf(line string) (uint64, bool) {
	for _, f := range strings.Fields(line) {
		if !strings.HasPrefix(f, "mark=") {
			continue
		}
		n, err := strconv.ParseUint(strings.TrimPrefix(f, "mark="), 10, 64)
		if err != nil {
			return 0, false
		}
		return n, true
	}
	return 0, false
}
