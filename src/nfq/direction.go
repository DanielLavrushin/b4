package nfq

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
)

const (
	nfHookPreRouting  = 0
	nfHookLocalOut    = 3
	nfHookPostRouting = 4
)

const (
	queueDirectionExactV4 uint32 = 1 << iota
	queueDirectionExactV6
)

const queueDirectionSettle = 30 * time.Second

type queueDirectionState struct {
	exact    atomic.Uint32
	mu       sync.Mutex
	changed  time.Time
	settled  bool
	reported string
}

func newQueueDirectionState(now time.Time) *queueDirectionState {
	return &queueDirectionState{changed: now}
}

func (d *queueDirectionState) set(v4, v6 bool, now time.Time) {
	if d == nil {
		return
	}
	var bits uint32
	if v4 {
		bits |= queueDirectionExactV4
	}
	if v6 {
		bits |= queueDirectionExactV6
	}
	d.exact.Store(bits)
	d.noteChange(now)
}

func (d *queueDirectionState) exactFor(v6 bool) bool {
	if d == nil {
		return false
	}
	bit := queueDirectionExactV4
	if v6 {
		bit = queueDirectionExactV6
	}
	return d.exact.Load()&bit != 0
}

func (d *queueDirectionState) noteChange(now time.Time) {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.changed = now
	d.settled = false
	d.mu.Unlock()
}

func (d *queueDirectionState) settle(now time.Time) bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.settled || now.Sub(d.changed) < queueDirectionSettle {
		return false
	}
	d.settled = true
	return true
}

func (d *queueDirectionState) report(warning string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if warning == d.reported {
		return false
	}
	d.reported = warning
	return warning != ""
}

func hookDirection(hook uint8) engine.Direction {
	switch hook {
	case nfHookPreRouting:
		return engine.DirReply
	case nfHookForward, nfHookLocalOut, nfHookPostRouting:
		return engine.DirOriginal
	}
	return engine.DirUnknown
}

func (w *Worker) packetDirection(cfg *config.Config, vc *verdictCtx, pkt *pktInfo) engine.Direction {
	if !vc.hooked || pkt.proto != 6 {
		return vc.dir
	}
	if !cfg.Queue.IsDiscovery && (cfg.System.Tables.SkipSetup || !w.direction.exactFor(pkt.ver == IPv6)) {
		return engine.DirUnknown
	}
	return hookDirection(vc.hook)
}

func (vc *verdictCtx) tcpIncoming(cfg *config.Config, sport uint16) bool {
	return vc.dir == engine.DirReply || (vc.dir == engine.DirUnknown && cfg.IsTCPPort(sport))
}

func (p *Pool) SetQueueDirectionExact(v4, v6 bool) {
	if p == nil || p.state == nil {
		return
	}
	p.state.direction.set(v4, v6, time.Now())
}

func (p *Pool) reportQueueDirection(now time.Time) {
	if p == nil || p.state == nil {
		return
	}
	d := p.state.direction
	if !d.settle(now) {
		return
	}
	warning := queueDirectionWarning(p.GetFirstWorkerConfig(), d.exactFor(false), d.exactFor(true))
	if !d.report(warning) {
		return
	}
	log.Warnf("%s", warning)
	metrics.GetMetricsCollector().Event(metrics.LevelWarning, metrics.EventDirectionByPort, map[string]string{"error": warning}, warning)
}

func queueDirectionWarning(cfg *config.Config, v4, v6 bool) string {
	if cfg == nil || cfg.Queue.IsDiscovery {
		return ""
	}
	ports, overlap := cfg.TCPPortsOverlapEphemeral()
	if !overlap {
		return ""
	}
	skip := cfg.System.Tables.SkipSetup
	var families []string
	if cfg.IPFamilyEnabled(false) && (skip || !v4) {
		families = append(families, "IPv4")
	}
	if cfg.Queue.Mode != "tun" && cfg.IPFamilyEnabled(true) && (skip || !v6) {
		families = append(families, "IPv6")
	}
	if len(families) == 0 {
		return ""
	}
	cause := "b4's capture rules do not select TCP packets by conntrack direction, so b4"
	remedy := "Filters below 32768 avoid it, and so does the kernel's conntrack match (xt_conntrack for iptables, nft_ct for nftables)"
	if skip {
		cause = "With skip_setup b4"
		remedy = "Filters below 32768 avoid it"
	}
	return fmt.Sprintf("%s TCP: the sets' TCP port filters cover %s, where devices pick the source ports of their own connections (32768-60999 on Linux and Android, 49152-65535 on Windows, macOS and iOS). %s tells a client's packets from a server's replies by port and cannot tell them apart there: connections from those source ports pass through unprocessed. %s",
		strings.Join(families, " and "), ports, cause, remedy)
}
