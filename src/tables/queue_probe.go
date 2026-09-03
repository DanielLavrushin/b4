package tables

import (
	"fmt"
	"strings"
	"sync"

	"github.com/daniellavrushin/b4/config"
)

var (
	queueProbeMu    sync.Mutex
	queueProbeCache = map[string]queueCaps{}
)

type queueCaps struct {
	probed    bool
	queue     bool
	ctPackets bool
	queueErr  error
}

func nfqueueActionExpr(startNum, threads int) string {
	if threads > 1 {
		return fmt.Sprintf("queue num %d-%d bypass", startNum, startNum+threads-1)
	}
	return fmt.Sprintf("queue num %d bypass", startNum)
}

func probeQueueNft(queueAction string) queueCaps {
	loadKernelModules()

	queueProbeMu.Lock()
	defer queueProbeMu.Unlock()

	const probeTable = "_b4_queue_probe"
	_, _ = run("nft", "delete", "table", "inet", probeTable)
	if _, err := run("nft", "add", "table", "inet", probeTable); err != nil {
		return queueCaps{}
	}
	defer func() { _, _ = run("nft", "delete", "table", "inet", probeTable) }()
	if _, err := run("nft", "add", "chain", "inet", probeTable, "test"); err != nil {
		return queueCaps{}
	}

	caps := queueCaps{probed: true}

	queueRule := append([]string{"nft", "add", "rule", "inet", probeTable, "test", "counter"}, strings.Fields(queueAction)...)
	if out, err := run(queueRule...); err != nil {
		caps.queueErr = err
		kmodNoteRejected("nft_queue", "nft", out)
	} else {
		caps.queue = true
	}

	if out, err := run("nft", "add", "rule", "inet", probeTable, "test",
		"ct", "original", "packets", "<", "20", "counter", "accept"); err == nil {
		caps.ctPackets = true
	} else {
		kmodNoteRejected("nft_ct", "nft", out)
	}

	return caps
}

func probeQueueNftCached(queueAction string) queueCaps {
	queueProbeMu.Lock()
	cached, ok := queueProbeCache[queueAction]
	queueProbeMu.Unlock()
	if ok {
		return cached
	}

	caps := probeQueueNft(queueAction)
	if caps.probed {
		queueProbeMu.Lock()
		queueProbeCache[queueAction] = caps
		queueProbeMu.Unlock()
	}
	return caps
}

func nfqueueMissingNft(cfg *config.Config) (missing []string, probed bool) {
	if !hasBinary("nft") {
		return nil, false
	}
	caps := probeQueueNft(nfqueueActionExpr(cfg.Queue.StartNum, cfg.Queue.Threads))
	if !caps.probed {
		return nil, false
	}
	if !caps.queue {
		missing = append(missing, "nft_queue")
	}
	if !caps.ctPackets {
		missing = append(missing, "nft_ct")
	}
	return missing, true
}

func nfqueueMissingIpt(legacy bool) (missing []string, probed bool) {
	loadKernelModules()

	ipt := backendIPTables
	if legacy {
		ipt = backendIPTablesLegacy
	}
	if !hasBinary(ipt) {
		return nil, false
	}

	queueProbeMu.Lock()
	defer queueProbeMu.Unlock()

	const probeChain = "B4_QUEUE_PROBE"
	_, _ = run(ipt, "-w", "-t", "mangle", "-F", probeChain)
	_, _ = run(ipt, "-w", "-t", "mangle", "-X", probeChain)
	if _, err := run(ipt, "-w", "-t", "mangle", "-N", probeChain); err != nil {
		return nil, false
	}
	defer func() {
		_, _ = run(ipt, "-w", "-t", "mangle", "-F", probeChain)
		_, _ = run(ipt, "-w", "-t", "mangle", "-X", probeChain)
	}()

	if out, err := run(ipt, "-w", "-t", "mangle", "-A", probeChain,
		"-j", "NFQUEUE", "--queue-num", "0", "--queue-bypass"); err != nil {
		missing = append(missing, "xt_NFQUEUE")
		kmodNoteRejected("xt_NFQUEUE", ipt, out)
	}
	if out, err := run(ipt, "-w", "-t", "mangle", "-A", probeChain,
		"-p", "tcp", "-m", "connbytes", "--connbytes-dir", "original",
		"--connbytes-mode", "packets", "--connbytes", "0:10", "-j", "ACCEPT"); err != nil {
		missing = append(missing, "xt_connbytes")
		kmodNoteRejected("xt_connbytes", ipt, out)
	}
	return missing, true
}

func ProbeNFQueueCapability(cfg *config.Config) (available bool, missing []string, packages []string) {
	var miss []string
	var probed bool
	backend := detectFirewallBackend(cfg)
	if backend == backendNFTables {
		miss, probed = nfqueueMissingNft(cfg)
	} else {
		miss, probed = nfqueueMissingIpt(backend == backendIPTablesLegacy)
	}
	return probed && !containsFatalQueueModule(miss), miss, kmodPkgsFor(miss)
}

func containsFatalQueueModule(missing []string) bool {
	for _, m := range missing {
		if m == "nft_queue" || m == "xt_NFQUEUE" {
			return true
		}
	}
	return false
}
