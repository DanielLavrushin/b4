package warmtuple

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

const firstPort, lastPort = 63000, 63999
const idleRetention = 2 * time.Minute
const tupleCooling = 10 * time.Minute
const coolingPrune = 2048
const maxFallbacks = 1024
const fallbackDialTimeout = 10 * time.Second
const fallbackWarnInterval = time.Minute
const failureRetention = 10 * time.Minute
const maxFailures = 4096
const skipWarnInterval = 10 * time.Minute
const tcpTimestampsSysctl = "/proc/sys/net/ipv4/tcp_timestamps"

var ErrFallbackCapacity = errors.New("every fallback relay slot is in use")

var errRecentWarmFailure = errors.New("the warm to this address and name failed less than 10 minutes ago")

var errTCPTimestampsOff = errors.New("net.ipv4.tcp_timestamps is 0; reusing a warmed tuple needs TCP timestamps (the reconnect fails with EADDRNOTAVAIL)")

type failureKey struct {
	target netip.Addr
	host   string
}

type serviceKey struct {
	owner, set, host, route string
	source                  netip.Addr
	target                  netip.AddrPort
}

type coolKey struct {
	port   uint16
	source netip.Addr
	target netip.AddrPort
}

type serviceLease struct {
	key     serviceKey
	manager *Manager
	epoch   uint64
	busy    bool
	idle    time.Time
	port    uint16
}

type warnKey struct{ set, kind string }

type warnLimiter struct {
	count atomic.Int64
	last  atomic.Int64
}

func (w *warnLimiter) note(now time.Time, every time.Duration) (int64, bool) {
	n := w.count.Add(1)
	last := w.last.Load()
	if last != 0 && now.UnixNano()-last < int64(every) {
		return n, false
	}
	return n, w.last.CompareAndSwap(last, now.UnixNano())
}

type Service struct {
	clients    chan struct{}
	ctx        context.Context
	stop       context.CancelFunc
	mu         sync.Mutex
	cfg        *config.Config
	epoch      uint64
	flows      *Flows
	closed     bool
	next       uint16
	leases     map[uint16]*serviceLease
	cooling    map[coolKey]time.Time
	pending    map[*serviceLease]context.CancelFunc
	directs    map[*directConn]struct{}
	fallbacks  int
	failures   map[failureKey]time.Time
	warnings   map[warnKey]*warnLimiter
	source     func(context.Context, netip.AddrPort) (netip.Addr, error)
	route      func(context.Context, netip.AddrPort, netip.Addr) (string, error)
	newManager func(Config) (*Manager, error)
	direct     directDialer
	timestamps func() bool
}

type serviceConn struct {
	net.Conn
	service  *Service
	lease    *serviceLease
	once     sync.Once
	received atomic.Uint64
}

func NewService(flows *Flows) *Service {
	ctx, stop := context.WithCancel(context.Background())
	return &Service{clients: make(chan struct{}, 256), ctx: ctx, stop: stop, flows: flows, next: firstPort, leases: make(map[uint16]*serviceLease), cooling: make(map[coolKey]time.Time),
		pending: make(map[*serviceLease]context.CancelFunc), directs: make(map[*directConn]struct{}), failures: make(map[failureKey]time.Time), warnings: make(map[warnKey]*warnLimiter),
		source: routeSource, route: routeFingerprint, newManager: New, direct: dialDirect, timestamps: func() bool { return tcpTimestampsOn(tcpTimestampsSysctl) }}
}

func tcpTimestampsOn(path string) bool {
	value, err := os.ReadFile(path)
	return err != nil || strings.TrimSpace(string(value)) != "0"
}

func (s *Service) skipWarm() error {
	if !s.timestamps() {
		return errTCPTimestampsOff
	}
	return nil
}

func routeSource(ctx context.Context, target netip.AddrPort) (netip.Addr, error) {
	d := net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
		var sockErr error
		err := raw.Control(func(fd uintptr) {
			sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(config.SelfDialMark))
		})
		if err != nil {
			return err
		}
		return sockErr
	}}
	network := "udp6"
	if target.Addr().Is4() {
		network = "udp4"
	}
	c, err := d.DialContext(ctx, network, target.String())
	if err != nil {
		return netip.Addr{}, err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap(), nil
}

func collectedOnlyUpdate(previous, next *config.Config) bool {
	return previous != nil && next != nil && reflect.DeepEqual(previous.WithCollectedIPs(nil), next.WithCollectedIPs(nil))
}

func reuseSet(cfg *config.Config, id string, target netip.AddrPort) *config.SetConfig {
	if cfg == nil || !cfg.IPFamilyEnabled(!target.Addr().Is4()) || !cfg.IsTCPPort(443) {
		return nil
	}
	for _, set := range cfg.Sets {
		if set.Id == id && set.ReusesWSSize() && set.MatchesTCPDPort(443) && containsTarget(set, target.Addr()) {
			return set
		}
	}
	return nil
}

func (s *Service) keepsLease(l *serviceLease) bool {
	return l.epoch == s.epoch && reuseSet(s.cfg, l.key.set, l.key.target) != nil
}

func (s *Service) UpdateConfig(cfg *config.Config) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cfg == cfg {
		return
	}
	if !collectedOnlyUpdate(s.cfg, cfg) {
		s.epoch++
		clear(s.failures)
	}
	s.cfg = cfg
	for l, cancel := range s.pending {
		if !s.keepsLease(l) {
			cancel()
		}
	}
	now := time.Now()
	for _, l := range s.leases {
		if !l.busy && !s.keepsLease(l) {
			s.discardLocked(l, now)
		}
	}
}

func (s *Service) discardLocked(l *serviceLease, now time.Time) {
	if s.leases[l.port] == l {
		delete(s.leases, l.port)
	}
	if len(s.cooling) >= coolingPrune {
		for key, until := range s.cooling {
			if !now.Before(until) {
				delete(s.cooling, key)
			}
		}
	}
	s.cooling[coolKey{port: l.port, source: l.key.source, target: l.key.target}] = now.Add(tupleCooling)
	_ = l.manager.Close()
}

func (s *Service) reserveLocked(key serviceKey, set *config.SetConfig, source netip.Addr) (*serviceLease, error) {
	now := time.Now()
	for _, l := range s.leases {
		if !l.busy && (!s.keepsLease(l) || now.Sub(l.idle) >= idleRetention || l.manager.agedOut(l.port, now)) {
			s.discardLocked(l, now)
		}
	}
	for _, l := range s.leases {
		if !l.busy && l.key == key && s.keepsLease(l) {
			l.busy = true
			return l, nil
		}
	}
	for i := firstPort; i <= lastPort; i++ {
		port := s.next
		s.next++
		if s.next > lastPort {
			s.next = firstPort
		}
		if s.leases[port] != nil || now.Before(s.cooling[coolKey{port: port, source: source, target: key.target}]) {
			continue
		}
		m, err := s.newManager(Config{Host: key.host, Target: key.target, Source: source, FirstPort: port, LastPort: port,
			WarmAge: time.Duration(set.TCP.WSSizeReuse.MaxAgeSeconds) * time.Second, WarmMark: config.SelfDialMark,
			WarmTimeout: 15 * time.Second, ReuseTimeout: 5 * time.Second, RetryInterval: 50 * time.Millisecond, ReconnectDelay: time.Second, CompactHello: true})
		if err != nil {
			return nil, err
		}
		l := &serviceLease{key: key, manager: m, epoch: s.epoch, busy: true, port: port}
		s.leases[port] = l
		return l, nil
	}
	return nil, ErrCapacity
}

func (s *Service) Dial(ctx context.Context, set *config.SetConfig, owner, host string, target netip.AddrPort) (net.Conn, error) {
	host = normalizeHost(host)
	if !validHost(host) || target.Port() != 443 || !set.ReusesWSSize() {
		return nil, ErrTarget
	}
	source, err := s.source(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("warm route: %w", err)
	}
	if err := s.skipWarm(); err != nil {
		return s.fallBackAfter(ctx, set.Id, host, source, target, err)
	}
	egress, err := s.route(ctx, target, source)
	if err != nil {
		return s.fallBackAfter(ctx, set.Id, host, source, target, fmt.Errorf("warm egress: %w", err))
	}
	s.mu.Lock()
	if s.closed || s.cfg == nil || s.flows == nil {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	cfg := s.cfg
	set = reuseSet(cfg, set.Id, target)
	if set == nil {
		s.mu.Unlock()
		return nil, ErrTarget
	}
	failed := failureKey{target: target.Addr(), host: host}
	if time.Now().Before(s.failures[failed]) {
		s.mu.Unlock()
		return s.fallBackAfter(ctx, set.Id, host, source, target, errRecentWarmFailure)
	}
	key := serviceKey{owner: owner, set: set.Id, host: host, target: target, source: source, route: egress}
	l, err := s.reserveLocked(key, set, source)
	if err != nil {
		s.mu.Unlock()
		if errors.Is(err, ErrCapacity) {
			return s.fallBackAfter(ctx, set.Id, host, source, target, err)
		}
		return nil, err
	}
	warmCtx, cancel := context.WithCancel(ctx)
	s.pending[l] = cancel
	l.manager.capture = func() (func(), error) {
		return s.flows.Register(netip.AddrPortFrom(source, l.port), target, cfg, set, host)
	}
	s.mu.Unlock()
	defer cancel()
	raw, err := l.manager.DialContext(warmCtx, net.JoinHostPort(host, strconv.Itoa(int(target.Port()))))
	s.mu.Lock()
	delete(s.pending, l)
	live := warmCtx.Err() == nil && !s.closed
	if err == nil && (s.closed || !s.keepsLease(l)) {
		_ = raw.Close()
		err = errors.New("warm configuration changed")
		live = false
	}
	if err != nil {
		now := time.Now()
		s.discardLocked(l, now)
		if live && errors.Is(err, ErrWarm) {
			s.rememberFailureLocked(failed, now)
		}
		s.mu.Unlock()
		if live {
			return s.fallBackAfter(ctx, set.Id, host, source, target, err)
		}
		return nil, err
	}
	s.mu.Unlock()
	log.Tracef("WSSIZE reuse [%s]: %s -> %s host=%s warmed=%t warm=%s reconnect=%s", set.Name, raw.LocalAddr(), raw.RemoteAddr(), host, raw.(*Conn).Warmed, raw.(*Conn).WarmDuration, raw.(*Conn).ReuseDuration)
	return &serviceConn{Conn: raw, service: s, lease: l}, nil
}

func (s *Service) Fallback(ctx context.Context, set *config.SetConfig, _, host string, target netip.AddrPort) (net.Conn, error) {
	host = normalizeHost(host)
	if !validHost(host) || target.Port() != 443 || !set.ReusesWSSize() {
		return nil, ErrTarget
	}
	source, err := s.source(ctx, target)
	if err != nil {
		return nil, fmt.Errorf("warm route: %w", err)
	}
	c, _, err := s.dialFallback(ctx, set.Id, host, source, target)
	return c, err
}

func (s *Service) fallBackAfter(ctx context.Context, id, host string, source netip.Addr, target netip.AddrPort, cause error) (net.Conn, error) {
	if ctx.Err() != nil {
		return nil, cause
	}
	c, set, err := s.dialFallback(ctx, id, host, source, target)
	if err != nil {
		return nil, fmt.Errorf("%v; relay with the set's strategy: %w", cause, err)
	}
	s.noteFallback(set, host, target, cause)
	return c, nil
}

func (s *Service) rememberFailureLocked(key failureKey, now time.Time) {
	if _, known := s.failures[key]; !known && len(s.failures) >= maxFailures {
		for k, until := range s.failures {
			if !now.Before(until) {
				delete(s.failures, k)
			}
		}
		if len(s.failures) >= maxFailures {
			var earliest failureKey
			var first time.Time
			for k, until := range s.failures {
				if first.IsZero() || until.Before(first) {
					earliest, first = k, until
				}
			}
			delete(s.failures, earliest)
		}
	}
	s.failures[key] = now.Add(failureRetention)
}

func (s *Service) dialFallback(ctx context.Context, id, host string, source netip.Addr, target netip.AddrPort) (net.Conn, *config.SetConfig, error) {
	s.mu.Lock()
	if s.closed || s.cfg == nil || s.flows == nil {
		s.mu.Unlock()
		return nil, nil, ErrClosed
	}
	cfg := s.cfg
	set := reuseSet(cfg, id, target)
	if set == nil {
		s.mu.Unlock()
		return nil, nil, ErrTarget
	}
	if s.fallbacks >= maxFallbacks {
		s.mu.Unlock()
		return nil, nil, ErrFallbackCapacity
	}
	s.fallbacks++
	s.mu.Unlock()
	dialCtx, cancel := context.WithTimeout(ctx, fallbackDialTimeout)
	defer cancel()
	stop := context.AfterFunc(s.ctx, cancel)
	defer stop()
	raw, release, err := s.direct(dialCtx, source, target, config.SelfDialMark, func(local netip.AddrPort) (func(), error) {
		return s.flows.Register(local, target, cfg, set, host)
	})
	s.mu.Lock()
	defer s.mu.Unlock()
	if err == nil && s.closed {
		_ = raw.Close()
		release()
		err = ErrClosed
	}
	if err != nil {
		s.fallbacks--
		return nil, nil, err
	}
	c := &directConn{Conn: raw, service: s, release: release}
	s.directs[c] = struct{}{}
	return c, set, nil
}

func (s *Service) limiter(key warnKey) *warnLimiter {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.warnings[key]
	if w == nil {
		w = &warnLimiter{}
		s.warnings[key] = w
	}
	return w
}

func (s *Service) noteFallback(set *config.SetConfig, host string, target netip.AddrPort, cause error) {
	switch {
	case errors.Is(cause, errRecentWarmFailure):
		log.Tracef("WSSIZE reuse [%s]: %s (%s): %v, relayed with the set's strategy without reuse", set.Name, host, target.Addr(), cause)
	case errors.Is(cause, errTCPTimestampsOff):
		if n, warn := s.limiter(warnKey{set: set.Id, kind: "timestamps"}).note(time.Now(), skipWarnInterval); warn {
			log.Warnf("WSSIZE reuse [%s]: %v, so the set uses its strategy without reuse; %d connections relayed that way so far", set.Name, cause, n)
			return
		}
		log.Tracef("WSSIZE reuse [%s]: %s: %v, relayed with the set's strategy without reuse", set.Name, host, cause)
	case errors.Is(cause, ErrWarm):
		if n, warn := s.limiter(warnKey{set: set.Id, kind: "warm"}).note(time.Now(), fallbackWarnInterval); warn {
			log.Warnf("WSSIZE reuse [%s]: warm to %s (%s) failed: %v; connections to that address and name use the set's strategy without reuse for 10 minutes, %d failed warms so far", set.Name, target.Addr(), host, cause, n)
			return
		}
		log.Tracef("WSSIZE reuse [%s]: warm to %s (%s) failed: %v; relayed with the set's strategy without reuse for 10 minutes", set.Name, target.Addr(), host, cause)
	case errors.Is(cause, ErrCapacity):
		if n, warn := s.limiter(warnKey{set: set.Id, kind: "capacity"}).note(time.Now(), fallbackWarnInterval); warn {
			log.Warnf("WSSIZE reuse [%s]: %s: %v, %d connections relayed with the set's strategy without reuse so far", set.Name, host, cause, n)
			return
		}
		log.Tracef("WSSIZE reuse [%s]: %s: %v, relayed with the set's strategy without reuse", set.Name, host, cause)
	default:
		log.Warnf("WSSIZE reuse [%s]: %s: %v; relayed with the set's strategy without reuse", set.Name, host, cause)
	}
}

func (c *serviceConn) Close() error {
	var err error
	c.once.Do(func() {
		err = c.Conn.Close()
		s := c.service
		s.mu.Lock()
		defer s.mu.Unlock()
		c.lease.busy = false
		c.lease.idle = time.Now()
		if s.closed || !s.keepsLease(c.lease) || c.received.Load() == 0 {
			s.discardLocked(c.lease, c.lease.idle)
		}
	})
	return err
}

func (c *serviceConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Close()
}

func (s *Service) Close() error {
	s.mu.Lock()
	s.closed = true
	s.stop()
	for _, cancel := range s.pending {
		cancel()
	}
	managers := make([]*Manager, 0, len(s.leases))
	for _, l := range s.leases {
		managers = append(managers, l.manager)
	}
	s.leases = make(map[uint16]*serviceLease)
	directs := make([]*directConn, 0, len(s.directs))
	for c := range s.directs {
		directs = append(directs, c)
	}
	s.mu.Unlock()
	for _, m := range managers {
		_ = m.Close()
	}
	for _, c := range directs {
		_ = c.Close()
	}
	return nil
}

func containsTarget(set *config.SetConfig, ip netip.Addr) bool {
	for _, value := range set.TargetIPs() {
		if prefix, err := netip.ParsePrefix(value); err == nil && prefix.Contains(ip) {
			return true
		}
		if addr, err := netip.ParseAddr(value); err == nil && addr.Unmap() == ip {
			return true
		}
	}
	return false
}

func (s *Service) AcquireClient() (func(), bool) {
	select {
	case s.clients <- struct{}{}:
		var once sync.Once
		return func() { once.Do(func() { <-s.clients }) }, true
	default:
		return nil, false
	}
}

func (c *serviceConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if n > 0 {
		c.received.Add(uint64(n))
	}
	return n, err
}
