package warmtuple

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"reflect"
	"strconv"
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

type Service struct {
	clients    chan struct{}
	mu         sync.Mutex
	cfg        *config.Config
	epoch      uint64
	flows      *Flows
	closed     bool
	next       uint16
	leases     map[uint16]*serviceLease
	cooling    map[coolKey]time.Time
	pending    map[*serviceLease]context.CancelFunc
	source     func(context.Context, netip.AddrPort) (netip.Addr, error)
	route      func(context.Context, netip.AddrPort, netip.Addr) (string, error)
	newManager func(Config) (*Manager, error)
}

type serviceConn struct {
	net.Conn
	service  *Service
	lease    *serviceLease
	once     sync.Once
	received atomic.Uint64
}

func NewService(flows *Flows) *Service {
	return &Service{clients: make(chan struct{}, 256), flows: flows, next: firstPort, leases: make(map[uint16]*serviceLease), cooling: make(map[coolKey]time.Time), pending: make(map[*serviceLease]context.CancelFunc), source: routeSource, route: routeFingerprint, newManager: New}
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
	egress, err := s.route(ctx, target, source)
	if err != nil {
		return nil, fmt.Errorf("warm egress: %w", err)
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
	key := serviceKey{owner: owner, set: set.Id, host: host, target: target, source: source, route: egress}
	l, err := s.reserveLocked(key, set, source)
	if err != nil {
		s.mu.Unlock()
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	s.pending[l] = cancel
	l.manager.capture = func() (func(), error) {
		return s.flows.Register(netip.AddrPortFrom(source, l.port), target, cfg, set, host)
	}
	s.mu.Unlock()
	defer cancel()
	raw, err := l.manager.DialContext(ctx, net.JoinHostPort(host, strconv.Itoa(int(target.Port()))))
	s.mu.Lock()
	delete(s.pending, l)
	if err == nil && (s.closed || !s.keepsLease(l)) {
		_ = raw.Close()
		err = errors.New("warm configuration changed")
	}
	if err != nil {
		s.discardLocked(l, time.Now())
		s.mu.Unlock()
		return nil, err
	}
	s.mu.Unlock()
	log.Tracef("WSSIZE reuse [%s]: %s -> %s host=%s warmed=%t warm=%s reconnect=%s", set.Name, raw.LocalAddr(), raw.RemoteAddr(), host, raw.(*Conn).Warmed, raw.(*Conn).WarmDuration, raw.(*Conn).ReuseDuration)
	return &serviceConn{Conn: raw, service: s, lease: l}, nil
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
	for _, cancel := range s.pending {
		cancel()
	}
	managers := make([]*Manager, 0, len(s.leases))
	for _, l := range s.leases {
		managers = append(managers, l.manager)
	}
	s.leases = make(map[uint16]*serviceLease)
	s.mu.Unlock()
	for _, m := range managers {
		_ = m.Close()
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
