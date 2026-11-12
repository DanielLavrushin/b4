package warmtuple

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
)

type Service struct {
	mu         sync.Mutex
	cfg        *config.Config
	matcher    *sni.SuffixSet
	flows      *Flows
	closed     bool
	next       int
	ports      map[uint16]time.Time
	pending    map[*Manager]context.CancelFunc
	active     map[*serviceConn]struct{}
	lookup     func(context.Context, string) ([]netip.Addr, error)
	source     func(netip.AddrPort) (netip.Addr, error)
	newManager func(Config) (*Manager, error)
}

type serviceConn struct {
	net.Conn
	service *Service
	manager *Manager
	port    uint16
	once    sync.Once
}

type Status struct {
	Active       int `json:"active"`
	Pending      int `json:"pending"`
	CoolingPorts int `json:"cooling_ports"`
	CaptureFlows int `json:"capture_flows"`
}

func (s *Service) Status() Status {
	s.mu.Lock()
	defer s.mu.Unlock()
	st := Status{Active: len(s.active), Pending: len(s.pending)}
	for _, until := range s.ports {
		if !until.IsZero() && time.Now().Before(until) {
			st.CoolingPorts++
		}
	}
	if s.flows != nil {
		st.CaptureFlows = s.flows.Active()
	}
	return st
}

func NewService(cfg *config.Config, flows *Flows) *Service {
	s := &Service{cfg: cfg, matcher: sni.NewSuffixSet(cfg.Sets), flows: flows,
		ports: make(map[uint16]time.Time), pending: make(map[*Manager]context.CancelFunc), active: make(map[*serviceConn]struct{}),
		lookup: func(ctx context.Context, host string) ([]netip.Addr, error) {
			return net.DefaultResolver.LookupNetIP(ctx, "ip4", host)
		},
		source: routeSource, newManager: New}
	return s
}

func routeSource(target netip.AddrPort) (netip.Addr, error) {
	c, err := net.DialUDP("udp4", nil, net.UDPAddrFromAddrPort(target))
	if err != nil {
		return netip.Addr{}, err
	}
	defer c.Close()
	return c.LocalAddr().(*net.UDPAddr).AddrPort().Addr().Unmap(), nil
}

func (s *Service) UpdateConfig(cfg *config.Config) {
	m := sni.NewSuffixSet(cfg.Sets)
	s.mu.Lock()
	s.cfg, s.matcher = cfg, m
	for _, cancel := range s.pending {
		cancel()
	}
	s.mu.Unlock()
}

func (s *Service) Close() error {
	s.mu.Lock()
	s.closed = true
	for _, cancel := range s.pending {
		cancel()
	}
	var active []*serviceConn
	for c := range s.active {
		active = append(active, c)
	}
	s.mu.Unlock()
	for _, c := range active {
		c.Close()
	}
	return nil
}

func (s *Service) takePort(cfg config.Socks5WarmTupleConfig) (uint16, error) {
	for port, until := range s.ports {
		if !until.IsZero() && !time.Now().Before(until) {
			delete(s.ports, port)
		}
	}
	if s.next < cfg.FirstPort || s.next > cfg.LastPort {
		s.next = cfg.FirstPort
	}
	now := time.Now()
	for n := cfg.FirstPort; n <= cfg.LastPort; n++ {
		port := uint16(s.next)
		s.next++
		if s.next > cfg.LastPort {
			s.next = cfg.FirstPort
		}
		if until, ok := s.ports[port]; !ok || (!until.IsZero() && !now.Before(until)) {
			s.ports[port] = time.Time{}
			return port, nil
		}
	}
	return 0, ErrCapacity
}

func (s *Service) releasePort(port uint16) {
	s.mu.Lock()
	s.ports[port] = time.Now().Add(2 * time.Minute)
	s.mu.Unlock()
}

func addressWarmMayWin(cfg *config.Config, matcher *sni.SuffixSet, named *config.SetConfig, sourceMAC string) bool {
	for _, set := range cfg.Sets {
		if !set.Enabled || !set.TCP.WarmTuple.Enabled || set.Targets.DomainOnly || len(set.Targets.IpsToMatch) == 0 ||
			!set.MatchesTCPDPort(443) || !set.MatchesIPVersion(4) || !sni.SetMatchesSource(set, sourceMAC) {
			continue
		}
		if named == nil || matcher.AddressHolds(set, 1, named, sourceMAC) {
			return true
		}
	}
	return false
}

func (s *Service) DialContext(ctx context.Context, dest, sourceMAC string) (net.Conn, string, bool, string, error) {
	host, portText, err := net.SplitHostPort(dest)
	if err != nil {
		return nil, "", false, "", nil
	}
	host = normalizeHost(host)
	if !validHost(host) {
		return nil, "", false, "", nil
	}
	s.mu.Lock()
	cfg, matcher := s.cfg, s.matcher
	keep := sni.SetFilter(func(set *config.SetConfig) bool { return set.MatchesTCPDPort(443) })
	_, named := matcher.MatchSNIWithSourceTLS(host, sourceMAC, 0, 4, keep)
	nameWarm := named != nil && named.TCP.WarmTuple.Enabled
	if portText != "443" {
		s.mu.Unlock()
		if nameWarm {
			return nil, named.Id, true, "", errors.New("warm tuple supports TCP443 only")
		}
		return nil, "", false, "", nil
	}
	if !nameWarm && !addressWarmMayWin(cfg, matcher, named, sourceMAC) {
		s.mu.Unlock()
		return nil, "", false, "", nil
	}
	if s.closed {
		s.mu.Unlock()
		return nil, "", true, "", ErrClosed
	}
	options := cfg.System.Socks5.WarmTuple.Effective()
	s.mu.Unlock()
	ctx, cancel := context.WithTimeout(ctx, time.Duration(options.WarmTimeoutMs+options.ReuseTimeoutMs+5000)*time.Millisecond)
	defer cancel()
	var targetIP netip.Addr
	if fixed, ok := options.Targets[host]; ok {
		targetIP, err = netip.ParseAddr(fixed)
	} else {
		var ips []netip.Addr
		lookupCtx, lookupCancel := context.WithTimeout(ctx, 5*time.Second)
		ips, err = s.lookup(lookupCtx, host)
		lookupCancel()
		for _, ip := range ips {
			if ip.Is4() {
				targetIP = ip
				break
			}
		}
	}
	if err != nil || !targetIP.Is4() || targetIP.IsUnspecified() || targetIP.IsMulticast() {
		if !nameWarm {
			return nil, "", false, "", nil
		}
		return nil, named.Id, true, "", fmt.Errorf("resolve warm target %s: no usable IPv4 (%v)", host, err)
	}
	resolved := net.JoinHostPort(targetIP.String(), portText)
	_, address, prefix := matcher.MatchIPWithSourceDetail(net.IP(targetIP.AsSlice()), sourceMAC,
		func(set *config.SetConfig) bool { return keep(set) && !set.Targets.DomainOnly })
	set := named
	if address != nil && (named == nil || matcher.AddressHolds(address, prefix, named, sourceMAC)) {
		set = address
	}
	s.mu.Lock()
	changed := s.closed || s.cfg != cfg
	s.mu.Unlock()
	if changed {
		return nil, "", true, "", errors.New("warm tuple configuration changed")
	}
	if set == nil || !set.TCP.WarmTuple.Enabled {
		return nil, "", false, resolved, nil
	}
	if s.flows == nil {
		return nil, set.Id, true, resolved, errors.New("warm tuple capture service is unavailable")
	}
	target := netip.AddrPortFrom(targetIP, 443)
	var source netip.Addr
	if options.SourceAddress != "" {
		source, err = netip.ParseAddr(options.SourceAddress)
	} else {
		source, err = s.source(target)
	}
	if err != nil {
		return nil, set.Id, true, resolved, fmt.Errorf("warm source: %w", err)
	}
	s.mu.Lock()
	if s.closed || s.cfg != cfg {
		s.mu.Unlock()
		return nil, set.Id, true, resolved, errors.New("warm tuple configuration changed")
	}
	port, err := s.takePort(options)
	s.mu.Unlock()
	if err != nil {
		return nil, set.Id, true, resolved, err
	}
	returned := false
	defer func() {
		if !returned {
			s.releasePort(port)
		}
	}()
	m, err := s.newManager(Config{Host: host, Target: target, Source: source, FirstPort: port, LastPort: port,
		WarmTimeout:   time.Duration(options.WarmTimeoutMs) * time.Millisecond,
		ReuseTimeout:  time.Duration(options.ReuseTimeoutMs) * time.Millisecond,
		RetryInterval: 50 * time.Millisecond, ReconnectDelay: time.Duration(options.ReconnectDelayMs) * time.Millisecond,
		CompactHello: options.CompactHello})
	if err != nil {
		return nil, set.Id, true, resolved, err
	}
	defer func() {
		if !returned {
			m.Close()
		}
	}()
	unregister, err := s.flows.Register(netip.AddrPortFrom(source, port), target, cfg, set, host)
	if err != nil {
		return nil, set.Id, true, resolved, err
	}
	defer unregister()
	s.mu.Lock()
	if s.closed || s.cfg != cfg {
		s.mu.Unlock()
		return nil, set.Id, true, resolved, errors.New("warm tuple configuration changed")
	}
	s.pending[m] = cancel
	s.mu.Unlock()
	originalWarm := m.warm
	m.warm = func(ctx context.Context, c net.Conn) error {
		err := originalWarm(ctx, c)
		if err == nil {
			unregister()
		}
		return err
	}
	raw, err := m.DialContext(ctx, net.JoinHostPort(host, strconv.Itoa(int(target.Port()))))
	s.mu.Lock()
	delete(s.pending, m)
	if err == nil && (s.closed || s.cfg != cfg) {
		raw.Close()
		err = errors.New("warm tuple configuration changed")
	}
	var c *serviceConn
	if err == nil {
		c = &serviceConn{Conn: raw, service: s, manager: m, port: port}
		s.active[c] = struct{}{}
		returned = true
	}
	s.mu.Unlock()
	if err != nil {
		log.Debugf("warm tuple %s -> %s: %v", host, target, err)
		return nil, set.Id, true, resolved, err
	}
	if passed, ok := raw.(*Conn); ok {
		log.Infof("warm tuple ready: host=%s tuple=%s->%s warm_ms=%d reuse_ms=%d", host, raw.LocalAddr(), raw.RemoteAddr(), passed.WarmDuration.Milliseconds(), passed.ReuseDuration.Milliseconds())
	}
	return c, set.Id, true, resolved, nil
}

func (c *serviceConn) Close() error {
	var err error
	c.once.Do(func() {
		err = c.Conn.Close()
		c.manager.Close()
		c.service.mu.Lock()
		delete(c.service.active, c)
		c.service.mu.Unlock()
		c.service.releasePort(c.port)
	})
	return err
}

func (c *serviceConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Close()
}
