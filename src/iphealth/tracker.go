package iphealth

import (
	"errors"
	"net"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

type state uint8

const (
	stateUnknown state = iota
	stateLive
	stateSuspect
	stateDead
)

const (
	maxEntries    = 8192
	idleTTL       = 10 * time.Minute
	synWindow     = 2 * time.Minute
	probeTimeout  = 4 * time.Second
	probeAttempts = 2
	probeWorkers  = 2
	probeQueue    = 64
	deadBurst     = 12
	burstWindow   = 60 * time.Second
	suspendFor    = 10 * time.Minute

	defaultSynThreshold = 3
	defaultDeadTTL      = 300 * time.Second
)

type Prober func(ip string, port uint16, mark int) bool

type entry struct {
	state     state
	syns      int
	firstSyn  time.Time
	seen      time.Time
	changedAt time.Time
	probing   bool
	port      uint16
	mark      int
}

type probeRequest struct {
	ip   string
	port uint16
	mark int
}

type Tracker struct {
	mu           sync.Mutex
	entries      map[string]*entry
	deadRecent   []time.Time
	suspendUntil time.Time

	probes  chan probeRequest
	stopCh  chan struct{}
	wg      sync.WaitGroup
	started bool
	stopped bool

	probe Prober
}

func NewTracker(probe Prober) *Tracker {
	if probe == nil {
		probe = DefaultProber
	}
	return &Tracker{
		entries: make(map[string]*entry),
		probe:   probe,
	}
}

func DefaultProber(ip string, port uint16, mark int) bool {
	if port == 0 {
		port = 443
	}
	addr := net.JoinHostPort(ip, strconv.Itoa(int(port)))
	d := netprobe.Dialer(mark, probeTimeout, 0)
	for i := 0; i < probeAttempts; i++ {
		conn, err := d.Dial("tcp", addr)
		if err == nil {
			_ = conn.Close()
			return true
		}
		if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
			return true
		}
	}
	return false
}

func (t *Tracker) suspendedLocked(now time.Time) bool {
	return !t.suspendUntil.IsZero() && now.Before(t.suspendUntil)
}

func (t *Tracker) RecordSyn(ip string, port uint16, mark, threshold int, timeout time.Duration) {
	if t == nil || ip == "" {
		return
	}
	if threshold <= 0 {
		threshold = defaultSynThreshold
	}

	now := time.Now()
	t.mu.Lock()
	if t.suspendedLocked(now) {
		t.mu.Unlock()
		return
	}

	e := t.entries[ip]
	if e == nil {
		t.evictLocked(now)
		if len(t.entries) >= maxEntries {
			t.mu.Unlock()
			return
		}
		e = &entry{firstSyn: now}
		t.entries[ip] = e
	}
	e.seen = now
	e.port = port
	e.mark = mark

	if e.probing || e.state == stateDead {
		t.mu.Unlock()
		return
	}

	if e.syns == 0 || now.Sub(e.firstSyn) > synWindow {
		e.firstSyn = now
		e.syns = 0
	}
	e.syns++

	crossed := e.syns >= threshold ||
		(e.syns > 1 && timeout > 0 && now.Sub(e.firstSyn) > timeout)
	if !crossed {
		t.mu.Unlock()
		return
	}

	e.state = stateSuspect
	e.changedAt = now
	e.probing = true
	syns := e.syns
	t.mu.Unlock()

	log.Tracef("IP health: %s answered none of %d SYNs, probing", ip, syns)
	t.enqueue(probeRequest{ip: ip, port: port, mark: mark})
}

func (t *Tracker) RecordAlive(ip string) {
	if t == nil || ip == "" {
		return
	}

	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()

	e := t.entries[ip]
	if e == nil {
		return
	}
	wasDead := e.state == stateDead
	e.state = stateLive
	e.syns = 0
	e.probing = false
	e.seen = now
	e.changedAt = now
	if wasDead {
		log.Infof("IP health: %s responded again, no longer treated as unreachable", ip)
	}
}

func (t *Tracker) IsDead(ip string) bool {
	if t == nil || ip == "" {
		return false
	}

	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.suspendedLocked(now) {
		return false
	}
	e := t.entries[ip]
	return e != nil && e.state == stateDead
}

func (t *Tracker) DeadIPs() []string {
	if t == nil {
		return nil
	}

	now := time.Now()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.suspendedLocked(now) {
		return nil
	}
	out := make([]string, 0, 8)
	for ip, e := range t.entries {
		if e.state == stateDead {
			out = append(out, ip)
		}
	}
	return out
}

func (t *Tracker) finishProbe(ip string, alive bool) {
	now := time.Now()
	t.mu.Lock()
	e := t.entries[ip]
	if e == nil || !e.probing {
		t.mu.Unlock()
		return
	}
	e.probing = false
	e.seen = now
	e.changedAt = now

	if alive {
		e.state = stateLive
		e.syns = 0
		t.mu.Unlock()
		log.Tracef("IP health: %s reachable from the router, treating the client stall as transient", ip)
		return
	}

	e.state = stateDead
	burst := t.recordDeadLocked(now)
	t.mu.Unlock()

	if burst {
		log.Warnf("IP health: %d destinations went unreachable within %s, that reads as an uplink outage rather than per-IP blocks - suspending IP block detection for %s", deadBurst, burstWindow, suspendFor)
		return
	}
	log.Warnf("IP health: %s does not answer from the router either, marking it unreachable", ip)
}

func (t *Tracker) abandonProbe(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if e := t.entries[ip]; e != nil && e.probing {
		e.probing = false
		e.state = stateUnknown
		e.syns = 0
	}
}

func (t *Tracker) recordDeadLocked(now time.Time) bool {
	cutoff := now.Add(-burstWindow)
	live := t.deadRecent[:0]
	for _, at := range t.deadRecent {
		if at.After(cutoff) {
			live = append(live, at)
		}
	}
	t.deadRecent = append(live, now)

	if len(t.deadRecent) < deadBurst {
		return false
	}

	t.suspendUntil = now.Add(suspendFor)
	t.deadRecent = t.deadRecent[:0]
	for _, e := range t.entries {
		if e.state == stateDead {
			e.state = stateUnknown
			e.syns = 0
		}
	}
	return true
}

func (t *Tracker) Cleanup(deadTTL time.Duration) {
	if t == nil {
		return
	}
	if deadTTL <= 0 {
		deadTTL = defaultDeadTTL
	}

	now := time.Now()
	var reprobe []probeRequest

	t.mu.Lock()
	for ip, e := range t.entries {
		if e.probing {
			continue
		}
		if e.state == stateDead {
			if now.Sub(e.changedAt) > deadTTL {
				e.state = stateSuspect
				e.probing = true
				e.changedAt = now
				reprobe = append(reprobe, probeRequest{ip: ip, port: e.port, mark: e.mark})
			}
			continue
		}
		if now.Sub(e.seen) > idleTTL {
			delete(t.entries, ip)
		}
	}
	t.mu.Unlock()

	for _, p := range reprobe {
		log.Tracef("IP health: re-testing %s after %s marked unreachable", p.ip, deadTTL)
		t.enqueue(p)
	}
}

func (t *Tracker) evictLocked(now time.Time) {
	if len(t.entries) < maxEntries {
		return
	}
	for ip, e := range t.entries {
		if e.state != stateDead && !e.probing && now.Sub(e.seen) > idleTTL {
			delete(t.entries, ip)
		}
	}
	for len(t.entries) >= maxEntries {
		oldest := ""
		var oldestAt time.Time
		for ip, e := range t.entries {
			if oldest == "" || e.seen.Before(oldestAt) {
				oldest = ip
				oldestAt = e.seen
			}
		}
		if oldest == "" {
			return
		}
		delete(t.entries, oldest)
	}
}

func (t *Tracker) enqueue(p probeRequest) {
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		t.abandonProbe(p.ip)
		return
	}
	if !t.started {
		t.started = true
		t.stopCh = make(chan struct{})
		t.probes = make(chan probeRequest, probeQueue)
		for i := 0; i < probeWorkers; i++ {
			t.wg.Add(1)
			go t.probeLoop(t.probes, t.stopCh)
		}
	}
	probes := t.probes
	t.mu.Unlock()

	select {
	case probes <- p:
	default:
		t.abandonProbe(p.ip)
	}
}

func (t *Tracker) probeLoop(probes <-chan probeRequest, stop <-chan struct{}) {
	defer t.wg.Done()
	for {
		select {
		case <-stop:
			return
		case p := <-probes:
			t.finishProbe(p.ip, t.probe(p.ip, p.port, p.mark))
		}
	}
}

func (t *Tracker) Stop() {
	if t == nil {
		return
	}
	t.mu.Lock()
	if t.stopped {
		t.mu.Unlock()
		return
	}
	t.stopped = true
	stop := t.stopCh
	t.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	t.wg.Wait()
}

func (t *Tracker) Len() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.entries)
}
