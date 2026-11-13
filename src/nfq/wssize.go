package nfq

import (
	"encoding/binary"
	"fmt"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

const (
	wsSizeCapacity   = 20000
	wsSizeTTL        = 2 * time.Minute
	wsSizeSynTTL     = 35 * time.Second
	wsSizeSweepEvery = time.Second
	wsSizeWarnEvery  = 10 * time.Second
	wsSizeShrinkFrom = 1024
)

type wsSizeEnd uint8

const (
	wsSizeOngoing wsSizeEnd = iota
	wsSizeEndFirstData
	wsSizeEndHello
	wsSizeEndDeadline
	wsSizeEndNotHello
	wsSizeEndConflict
	wsSizeEndTooLong
	wsSizeEndTooMany
	wsSizeEndBudget
	wsSizeEndClosed
)

var wsSizeEndText = [...]string{
	wsSizeEndFirstData: "first data packet",
	wsSizeEndHello:     "ClientHello complete",
	wsSizeEndDeadline:  "deadline, the ClientHello did not complete in time",
	wsSizeEndNotHello:  "fallback, the payload is not a TLS ClientHello",
	wsSizeEndConflict:  "fallback, a retransmission carried different bytes",
	wsSizeEndTooLong:   "fallback, the ClientHello passes the size limit",
	wsSizeEndTooMany:   "fallback, too many payload packets before the ClientHello ended",
	wsSizeEndBudget:    "fallback, the shared ClientHello buffer is full",
	wsSizeEndClosed:    "FIN before the cutoff",
}

type wsSizeFlow struct {
	cfg           *config.Config
	set           *config.SetConfig
	isn           uint32
	syn           bool
	cutoff        bool
	expires       time.Time
	hello         *wsSizeHello
	helloDeadline time.Time
	helloPackets  int
	helloEnd      int
}

type wsSizeAlarm struct {
	count  uint64
	warned time.Time
}

func (a *wsSizeAlarm) raise(now time.Time) uint64 {
	a.count++
	if now.Sub(a.warned) < wsSizeWarnEvery {
		return 0
	}
	a.warned = now
	return a.count
}

type wsSizeCache struct {
	mu         sync.Mutex
	flows      map[streamKey]wsSizeFlow
	helloBytes int
	peak       int
	swept      time.Time
	sweeps     uint64
	refused    wsSizeAlarm
	starved    wsSizeAlarm
}

type wsSizeStep struct {
	set      *config.SetConfig
	opened   bool
	end      wsSizeEnd
	helloEnd int
	refused  uint64
	starved  uint64
	stalled  []streamKey
}

type wsSizeFields struct {
	window uint16
	scale  int
}

func newWSSizeCache() *wsSizeCache {
	return &wsSizeCache{flows: make(map[streamKey]wsSizeFlow)}
}

func wsSizeTracksHello(ws config.WSSizeConfig) bool {
	return ws.Window > 0 && !ws.StopsAtFirstData()
}

func usesWSSize(cfg *config.Config) bool {
	if cfg.Queue.Mode == "tun" {
		return false
	}
	for _, set := range cfg.Sets {
		if set != nil && set.Enabled && set.TCP.WSSize.Rewrites() {
			return true
		}
	}
	return false
}

func (w *Worker) rewritesWindow(cfg *config.Config) bool {
	if g := w.wssizeGate.Load(); g != nil && g.cfg == cfg {
		return g.on
	}
	on := usesWSSize(cfg)
	w.wssizeGate.Store(&streamGate{cfg: cfg, on: on})
	return on
}

func wsSizeTracing() bool {
	return log.Level(log.CurLevel.Load()) >= log.LevelTrace
}

func wsSizeTraceEnd(key streamKey, end wsSizeEnd, helloEnd int) {
	if end == wsSizeEndHello {
		log.Tracef("WSSize: ended for %s -> %s: %s at %d bytes", key.client, key.server, wsSizeEndText[end], helloEnd)
		return
	}
	log.Tracef("WSSize: ended for %s -> %s: %s", key.client, key.server, wsSizeEndText[end])
}

func wsSizeFieldText(name string, before, after int) string {
	if before == after {
		return fmt.Sprintf("%s %d kept", name, before)
	}
	return fmt.Sprintf("%s %d -> %d", name, before, after)
}

func wsSizeTraceOpened(key streamKey, set string, before, after wsSizeFields) {
	scale := "no Window Scale option"
	if before.scale >= 0 {
		scale = wsSizeFieldText("scale", before.scale, after.scale)
	}
	log.Tracef("WSSize: opened for %s -> %s (set: %s): %s, %s", key.client, key.server, set, wsSizeFieldText("window", int(before.window), int(after.window)), scale)
}

func (c *wsSizeCache) reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.flows, c.helloBytes, c.peak = make(map[streamKey]wsSizeFlow), 0, 0
	c.mu.Unlock()
}

func (c *wsSizeCache) cleanup(now time.Time) {
	if c == nil {
		return
	}
	trace := wsSizeTracing()
	c.mu.Lock()
	stalled := c.cleanupLocked(now, trace)
	c.mu.Unlock()
	for _, key := range stalled {
		wsSizeTraceEnd(key, wsSizeEndDeadline, 0)
	}
}

func (c *wsSizeCache) cleanupLocked(now time.Time, trace bool) []streamKey {
	var stalled []streamKey
	for k, f := range c.flows {
		overdue := !f.helloDeadline.IsZero() && !now.Before(f.helloDeadline)
		expired := !now.Before(f.expires)
		if !overdue && !expired {
			continue
		}
		if trace && overdue && !f.cutoff {
			stalled = append(stalled, k)
		}
		if f.syn && !expired {
			c.releaseHello(&f)
			f.cutoff, f.helloEnd = true, 0
			c.flows[k] = f
			continue
		}
		c.dropLocked(k)
	}
	if len(c.flows) == 0 && c.peak >= wsSizeShrinkFrom {
		c.flows, c.peak = make(map[streamKey]wsSizeFlow), 0
	}
	return stalled
}

func (c *wsSizeCache) rewrite(raw []byte, at int, selected *config.SetConfig, cfg *config.Config, now time.Time) bool {
	if c == nil || len(raw) < at+20 {
		return false
	}
	if raw[0]>>4 == 4 && (at < 20 || int(binary.BigEndian.Uint16(raw[2:4])) != len(raw)) {
		return false
	}
	if raw[0]>>4 == 6 && int(binary.BigEndian.Uint16(raw[4:6]))+40 != len(raw) {
		return false
	}
	key, ok := streamKeyOf(raw, at)
	if !ok {
		return false
	}
	if raw[0]>>4 == 6 && at != 40 {
		return false
	}
	tcp := raw[at:]
	hlen := int(tcp[12]>>4) * 4
	if hlen < 20 || hlen > len(tcp) {
		return false
	}
	if tcp[13]&0x04 != 0 {
		return false
	}
	trace := wsSizeTracing()
	c.mu.Lock()
	step := c.stepLocked(key, tcp, hlen, selected, cfg, now, trace)
	c.mu.Unlock()
	if step.refused > 0 {
		log.Warnf("WSSize: the connection table is full (%d records), new connections go out with their own window (%d so far)", wsSizeCapacity, step.refused)
	}
	if step.starved > 0 {
		log.Warnf("WSSize: the ClientHello buffers are full (%d bytes), the rewrite stops before the ClientHello ends (%d connections so far)", wsSizeHelloBudget, step.starved)
	}
	var before, after wsSizeFields
	if step.set != nil {
		before, after = wsSizeApply(raw, at, hlen, step.set.TCP.WSSize)
	}
	if trace {
		for _, stalled := range step.stalled {
			wsSizeTraceEnd(stalled, wsSizeEndDeadline, 0)
		}
		if step.opened {
			wsSizeTraceOpened(key, step.set.Name, before, after)
		}
		if step.end != wsSizeOngoing {
			wsSizeTraceEnd(key, step.end, step.helloEnd)
		}
	}
	return after != before
}

func (c *wsSizeCache) stepLocked(key streamKey, tcp []byte, hlen int, selected *config.SetConfig, cfg *config.Config, now time.Time, trace bool) wsSizeStep {
	var step wsSizeStep
	flags := tcp[13]
	syn := flags&0x12 == 2
	isn := binary.BigEndian.Uint32(tcp[4:8])
	f, exists := c.flows[key]
	if exists && (f.cfg != cfg || !now.Before(f.expires)) {
		c.dropLocked(key)
		exists = false
	}
	if syn && (!exists || f.isn != isn || !f.syn) {
		c.dropLocked(key)
		if selected == nil || !selected.Enabled || !selected.TCP.WSSize.Rewrites() || selected.Routing.Enabled || selected.TCP.Duplicate.Enabled {
			return step
		}
		ws := selected.TCP.WSSize
		if ws.Window < 0 || ws.Window > config.MaxWSSizeWindow || ws.Scale < 0 || ws.Scale > config.MaxWSSizeScale {
			return step
		}
		if len(c.flows) >= wsSizeCapacity && now.Sub(c.swept) >= wsSizeSweepEvery {
			c.swept = now
			c.sweeps++
			step.stalled = c.cleanupLocked(now, trace)
		}
		if len(c.flows) >= wsSizeCapacity {
			step.refused = c.refused.raise(now)
			return step
		}
		f = wsSizeFlow{cfg: cfg, set: selected, isn: isn, syn: true}
		exists = true
		step.opened = true
	}
	if !exists {
		return step
	}
	if !syn {
		f.syn = false
	}
	payload := tcp[hlen:]
	active, tracked := true, wsSizeTracksHello(f.set.TCP.WSSize)
	if tracked {
		active, step.end = c.rewriteHello(&f, payload, isn, syn, now)
		step.helloEnd = f.helloEnd
		if step.end == wsSizeEndBudget {
			step.starved = c.starved.raise(now)
		}
	} else if !syn && len(payload) > 0 {
		step.end = wsSizeEndFirstData
	}
	over := step.end == wsSizeEndFirstData || (tracked && !f.syn && f.cutoff && f.helloEnd == 0)
	if flags&0x01 != 0 {
		if step.end == wsSizeOngoing && !f.cutoff {
			step.end = wsSizeEndClosed
		}
		over = true
	}
	if over {
		c.releaseHello(&f)
		delete(c.flows, key)
	} else {
		f.expires = now.Add(wsSizeTTL)
		if f.syn {
			f.expires = now.Add(wsSizeSynTTL)
		}
		c.flows[key] = f
		c.peak = max(c.peak, len(c.flows))
	}
	if active || syn {
		step.set = f.set
	}
	return step
}

func wsSizeScaleAt(tcp []byte, hlen int) int {
	for i := 20; i < hlen; {
		kind := tcp[i]
		if kind == 0 {
			break
		}
		if kind == 1 {
			i++
			continue
		}
		if i+1 >= hlen {
			break
		}
		n := int(tcp[i+1])
		if n < 2 || i+n > hlen {
			break
		}
		if kind == 3 && n == 3 {
			return i + 2
		}
		i += n
	}
	return 0
}

func wsSizeApply(raw []byte, at, hlen int, ws config.WSSizeConfig) (wsSizeFields, wsSizeFields) {
	tcp := raw[at:]
	before := wsSizeFields{window: binary.BigEndian.Uint16(tcp[14:16]), scale: -1}
	scaleAt := wsSizeScaleAt(tcp, hlen)
	if scaleAt > 0 {
		before.scale = int(tcp[scaleAt])
	}
	after := before
	if ws.Window > 0 {
		after.window = uint16(ws.Window)
	}
	if ws.Scale > 0 && before.scale > ws.Scale {
		after.scale = ws.Scale
	}
	if after == before {
		return before, after
	}
	binary.BigEndian.PutUint16(tcp[14:16], after.window)
	if after.scale != before.scale {
		tcp[scaleAt] = byte(after.scale)
	}
	if raw[0]>>4 == 4 {
		sock.FixTCPChecksum(raw)
	} else {
		sock.FixTCPChecksumV6(raw)
	}
	return before, after
}
