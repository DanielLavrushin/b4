package nfq

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

type ipHealthState uint8

const (
	ipHealthUnknown ipHealthState = iota
	ipHealthLive
	ipHealthSuspect
	ipHealthDead
)

const (
	maxIPHealthEntries    = 8192
	ipHealthIdleTTL       = 10 * time.Minute
	ipHealthSynWindow     = 2 * time.Minute
	ipHealthProbeTimeout  = 4 * time.Second
	ipHealthProbeAttempts = 2
	ipHealthProbeWorkers  = 2
	ipHealthProbeQueue    = 64
	ipHealthDeadBurst     = 12
	ipHealthBurstWindow   = 60 * time.Second
	ipHealthSuspendFor    = 10 * time.Minute
)

type ipHealthEntry struct {
	state     ipHealthState
	syns      int
	firstSyn  time.Time
	seen      time.Time
	changedAt time.Time
	probing   bool
	port      uint16
	mark      int
}

type ipHealthProbe struct {
	ip   string
	port uint16
	mark int
}

type ipHealthStore struct {
	mu           sync.Mutex
	entries      map[string]*ipHealthEntry
	deadRecent   []time.Time
	suspendUntil time.Time

	probes  chan ipHealthProbe
	stopCh  chan struct{}
	wg      sync.WaitGroup
	started bool
	stopped bool

	dial func(ip string, port uint16, mark int) bool
}

func newIPHealthStore() *ipHealthStore {
	return &ipHealthStore{
		entries: make(map[string]*ipHealthEntry),
		dial:    probeTCPReachable,
	}
}

func probeTCPReachable(ip string, port uint16, mark int) bool {
	if port == 0 {
		port = 443
	}
	addr := net.JoinHostPort(ip, strconv.Itoa(int(port)))
	d := netprobe.Dialer(mark, ipHealthProbeTimeout, 0)
	for i := 0; i < ipHealthProbeAttempts; i++ {
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

func (s *ipHealthStore) suspendedLocked(now time.Time) bool {
	return !s.suspendUntil.IsZero() && now.Before(s.suspendUntil)
}

func (s *ipHealthStore) RecordSyn(ip string, port uint16, mark, threshold int, timeout time.Duration) {
	if s == nil || ip == "" {
		return
	}
	if threshold <= 0 {
		threshold = 3
	}

	now := time.Now()
	s.mu.Lock()
	if s.suspendedLocked(now) {
		s.mu.Unlock()
		return
	}

	entry := s.entries[ip]
	if entry == nil {
		s.evictLocked(now)
		if len(s.entries) >= maxIPHealthEntries {
			s.mu.Unlock()
			return
		}
		entry = &ipHealthEntry{firstSyn: now}
		s.entries[ip] = entry
	}
	entry.seen = now
	entry.port = port
	entry.mark = mark

	if entry.probing || entry.state == ipHealthDead {
		s.mu.Unlock()
		return
	}

	if entry.syns == 0 || now.Sub(entry.firstSyn) > ipHealthSynWindow {
		entry.firstSyn = now
		entry.syns = 0
	}
	entry.syns++

	crossed := entry.syns >= threshold ||
		(entry.syns > 1 && timeout > 0 && now.Sub(entry.firstSyn) > timeout)
	if !crossed {
		s.mu.Unlock()
		return
	}

	entry.state = ipHealthSuspect
	entry.changedAt = now
	entry.probing = true
	syns := entry.syns
	s.mu.Unlock()

	log.Tracef("IP health: %s answered none of %d SYNs, probing", ip, syns)
	s.enqueueProbe(ipHealthProbe{ip: ip, port: port, mark: mark})
}

func (s *ipHealthStore) RecordAlive(ip string) {
	if s == nil || ip == "" {
		return
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := s.entries[ip]
	if entry == nil {
		return
	}
	wasDead := entry.state == ipHealthDead
	entry.state = ipHealthLive
	entry.syns = 0
	entry.probing = false
	entry.seen = now
	entry.changedAt = now
	if wasDead {
		log.Infof("IP health: %s responded again, no longer treated as unreachable", ip)
	}
}

func (s *ipHealthStore) IsDead(ip string) bool {
	if s == nil || ip == "" {
		return false
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.suspendedLocked(now) {
		return false
	}
	entry := s.entries[ip]
	return entry != nil && entry.state == ipHealthDead
}

func (s *ipHealthStore) DeadIPs() []string {
	if s == nil {
		return nil
	}

	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.suspendedLocked(now) {
		return nil
	}
	out := make([]string, 0, 8)
	for ip, entry := range s.entries {
		if entry.state == ipHealthDead {
			out = append(out, ip)
		}
	}
	return out
}

func (s *ipHealthStore) finishProbe(ip string, alive bool) {
	now := time.Now()
	s.mu.Lock()
	entry := s.entries[ip]
	if entry == nil || !entry.probing {
		s.mu.Unlock()
		return
	}
	entry.probing = false
	entry.seen = now
	entry.changedAt = now

	if alive {
		entry.state = ipHealthLive
		entry.syns = 0
		s.mu.Unlock()
		log.Tracef("IP health: %s reachable from the router, treating the client stall as transient", ip)
		return
	}

	entry.state = ipHealthDead
	burst := s.recordDeadLocked(now)
	s.mu.Unlock()

	if burst {
		log.Warnf("IP health: %d destinations went unreachable within %s, that reads as an uplink outage rather than per-IP blocks - suspending IP block detection for %s", ipHealthDeadBurst, ipHealthBurstWindow, ipHealthSuspendFor)
		return
	}
	log.Warnf("IP health: %s does not answer from the router either, marking it unreachable", ip)
}

func (s *ipHealthStore) abandonProbe(ip string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry := s.entries[ip]; entry != nil && entry.probing {
		entry.probing = false
		entry.state = ipHealthUnknown
		entry.syns = 0
	}
}

func (s *ipHealthStore) recordDeadLocked(now time.Time) bool {
	cutoff := now.Add(-ipHealthBurstWindow)
	live := s.deadRecent[:0]
	for _, at := range s.deadRecent {
		if at.After(cutoff) {
			live = append(live, at)
		}
	}
	s.deadRecent = append(live, now)

	if len(s.deadRecent) < ipHealthDeadBurst {
		return false
	}

	s.suspendUntil = now.Add(ipHealthSuspendFor)
	s.deadRecent = s.deadRecent[:0]
	for _, entry := range s.entries {
		if entry.state == ipHealthDead {
			entry.state = ipHealthUnknown
			entry.syns = 0
		}
	}
	return true
}

func (s *ipHealthStore) Cleanup(deadTTL time.Duration) {
	if s == nil {
		return
	}
	if deadTTL <= 0 {
		deadTTL = 300 * time.Second
	}

	now := time.Now()
	var reprobe []ipHealthProbe

	s.mu.Lock()
	for ip, entry := range s.entries {
		if entry.probing {
			continue
		}
		if entry.state == ipHealthDead {
			if now.Sub(entry.changedAt) > deadTTL {
				entry.state = ipHealthSuspect
				entry.probing = true
				entry.changedAt = now
				reprobe = append(reprobe, ipHealthProbe{ip: ip, port: entry.port, mark: entry.mark})
			}
			continue
		}
		if now.Sub(entry.seen) > ipHealthIdleTTL {
			delete(s.entries, ip)
		}
	}
	s.mu.Unlock()

	for _, p := range reprobe {
		log.Tracef("IP health: re-testing %s after %s marked unreachable", p.ip, deadTTL)
		s.enqueueProbe(p)
	}
}

func (s *ipHealthStore) evictLocked(now time.Time) {
	if len(s.entries) < maxIPHealthEntries {
		return
	}
	for ip, entry := range s.entries {
		if entry.state != ipHealthDead && !entry.probing && now.Sub(entry.seen) > ipHealthIdleTTL {
			delete(s.entries, ip)
		}
	}
	for len(s.entries) >= maxIPHealthEntries {
		oldest := ""
		var oldestAt time.Time
		for ip, entry := range s.entries {
			if oldest == "" || entry.seen.Before(oldestAt) {
				oldest = ip
				oldestAt = entry.seen
			}
		}
		if oldest == "" {
			return
		}
		delete(s.entries, oldest)
	}
}

func (s *ipHealthStore) enqueueProbe(p ipHealthProbe) {
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		s.abandonProbe(p.ip)
		return
	}
	if !s.started {
		s.started = true
		s.stopCh = make(chan struct{})
		s.probes = make(chan ipHealthProbe, ipHealthProbeQueue)
		for i := 0; i < ipHealthProbeWorkers; i++ {
			s.wg.Add(1)
			go s.probeLoop(s.probes, s.stopCh)
		}
	}
	probes := s.probes
	s.mu.Unlock()

	select {
	case probes <- p:
	default:
		s.abandonProbe(p.ip)
	}
}

func (s *ipHealthStore) probeLoop(probes <-chan ipHealthProbe, stop <-chan struct{}) {
	defer s.wg.Done()
	for {
		select {
		case <-stop:
			return
		case p := <-probes:
			s.finishProbe(p.ip, s.dial(p.ip, p.port, p.mark))
		}
	}
}

func (s *ipHealthStore) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	if s.stopped {
		s.mu.Unlock()
		return
	}
	s.stopped = true
	stop := s.stopCh
	s.mu.Unlock()

	if stop != nil {
		close(stop)
	}
	s.wg.Wait()
}

func (s *ipHealthStore) Len() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.entries)
}
