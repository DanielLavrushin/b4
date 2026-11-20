package warmtuple

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

var (
	ErrCapacity = errors.New("all warm-tuple ports are leased")
	ErrClosed   = errors.New("warm-tuple manager is closed")
	ErrTarget   = errors.New("destination is outside the fixed warm-tuple selector")
	ErrWarm     = errors.New("the server failed the warm")

	ErrNoTimestamps = errors.New("the warm connection did not negotiate TCP timestamps, so the same-tuple reconnect over the warm's TIME_WAIT would fail with EADDRNOTAVAIL")
)

const tcpiOptTimestamps = 1

type warmError struct{ error }

func (e warmError) Unwrap() error { return e.error }

func (e warmError) Is(target error) bool { return target == ErrWarm }

type connectError struct{ error }

func (e connectError) Unwrap() error { return e.error }

type Config struct {
	WarmAge        time.Duration
	WarmMark       uint32
	Host           string
	Target         netip.AddrPort
	Source         netip.Addr
	FirstPort      uint16
	LastPort       uint16
	WarmTimeout    time.Duration
	ReuseTimeout   time.Duration
	RetryInterval  time.Duration
	ReconnectDelay time.Duration
	CompactHello   bool
	PassLifetime   time.Duration
	RootCAs        *x509.CertPool
}

type Manager struct {
	cfg         Config
	ctx         context.Context
	cancel      context.CancelFunc
	mu          sync.Mutex
	closed      bool
	leases      map[uint16]*lease
	warmed      map[uint16]time.Time
	next        uint16
	dial        func(context.Context, uint16, uint32) (net.Conn, error)
	warm        func(context.Context, net.Conn) error
	timestamped func(net.Conn) (bool, error)
	capture     func() (func(), error)
}

type lease struct {
	manager *Manager
	port    uint16
	conn    net.Conn
	once    sync.Once
}

type Conn struct {
	net.Conn
	Warmed        bool
	WarmDuration  time.Duration
	ReuseDuration time.Duration
	lease         *lease
	mu            sync.Mutex
	timer         *time.Timer
	closed        bool
}

func New(cfg Config) (*Manager, error) {
	cfg.Host = normalizeHost(cfg.Host)
	if !validHost(cfg.Host) || !cfg.Source.IsValid() || cfg.Source.IsUnspecified() ||
		!cfg.Target.IsValid() || cfg.Source.Is4() != cfg.Target.Addr().Is4() || cfg.Target.Addr().IsUnspecified() || cfg.Target.Port() == 0 {
		return nil, errors.New("a hostname, explicit source IP and matching target IP:port are required")
	}
	if cfg.FirstPort == 0 || cfg.LastPort < cfg.FirstPort || int(cfg.LastPort)-int(cfg.FirstPort) >= 32 {
		return nil, errors.New("source port range must contain 1 to 32 nonzero ports")
	}
	if cfg.WarmTimeout <= 0 || cfg.WarmTimeout > 60*time.Second ||
		cfg.ReuseTimeout <= 0 || cfg.ReuseTimeout > 60*time.Second ||
		cfg.RetryInterval < 20*time.Millisecond || cfg.RetryInterval > 500*time.Millisecond ||
		cfg.ReconnectDelay < 0 || cfg.ReconnectDelay >= cfg.ReuseTimeout ||
		cfg.PassLifetime < 0 || cfg.PassLifetime > 24*time.Hour {
		return nil, errors.New("invalid bounded warm, reuse, retry or pass lifetime")
	}
	if cfg.RootCAs != nil {
		cfg.RootCAs = cfg.RootCAs.Clone()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{cfg: cfg, ctx: ctx, cancel: cancel, leases: make(map[uint16]*lease), warmed: make(map[uint16]time.Time), next: cfg.FirstPort}
	m.dial, m.warm, m.timestamped = m.dialSocket, m.warmTLS, negotiatedTimestamps
	return m, nil
}

func normalizeHost(host string) string {
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

func validHost(host string) bool {
	if len(host) == 0 || len(host) > 253 || net.ParseIP(host) != nil {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
				return false
			}
		}
	}
	return true
}

func (m *Manager) reserve() (*lease, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return nil, ErrClosed
	}
	for n := 0; n <= int(m.cfg.LastPort)-int(m.cfg.FirstPort); n++ {
		port := m.next
		if m.next == m.cfg.LastPort {
			m.next = m.cfg.FirstPort
		} else {
			m.next++
		}
		if m.leases[port] == nil {
			l := &lease{manager: m, port: port}
			m.leases[port] = l
			return l, nil
		}
	}
	return nil, ErrCapacity
}

func (l *lease) track(c net.Conn) error {
	m := l.manager
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		c.Close()
		return ErrClosed
	}
	l.conn = c
	m.mu.Unlock()
	return nil
}

func (l *lease) release() {
	l.once.Do(func() {
		m := l.manager
		m.mu.Lock()
		delete(m.leases, l.port)
		m.mu.Unlock()
	})
}

func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.leases)
}

func (m *Manager) Close() error {
	m.mu.Lock()
	m.closed = true
	m.cancel()
	var connections []net.Conn
	for _, l := range m.leases {
		if l.conn != nil {
			connections = append(connections, l.conn)
		}
	}
	m.mu.Unlock()
	for _, c := range connections {
		c.Close()
	}
	return nil
}

func (m *Manager) DialContext(ctx context.Context, dest string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(dest)
	if err != nil || normalizeHost(host) != m.cfg.Host || port != strconv.Itoa(int(m.cfg.Target.Port())) {
		return nil, ErrTarget
	}
	ctx, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(m.ctx, cancel)
	defer stop()
	defer cancel()
	l, err := m.reserve()
	if err != nil {
		return nil, err
	}
	returned := false
	defer func() {
		if !returned {
			l.release()
		}
	}()
	started := time.Now()
	m.mu.Lock()
	_, stamped := m.warmed[l.port]
	m.mu.Unlock()
	warmed := false
	if !stamped {
		var stamp time.Time
		stamp, err = m.runWarm(ctx, l)
		if err == nil {
			m.mu.Lock()
			if _, ok := m.warmed[l.port]; !ok {
				m.warmed[l.port] = stamp
			}
			m.mu.Unlock()
			warmed = true
		}
	}
	warmDuration := time.Since(started)
	if err != nil {
		return nil, fmt.Errorf("warm: %w", err)
	}
	started = time.Now()
	passCtx, passCancel := context.WithTimeout(ctx, m.cfg.ReuseTimeout)
	defer passCancel()
	if warmed {
		settle := time.NewTimer(m.cfg.ReconnectDelay)
		select {
		case <-passCtx.Done():
			settle.Stop()
			return nil, fmt.Errorf("same-tuple reconnect: %w", passCtx.Err())
		case <-settle.C:
		}
	}
	raw, err := m.connect(passCtx, l.port, config.SelfDialRelayMark)
	if err != nil {
		return nil, fmt.Errorf("same-tuple reconnect: %w", err)
	}
	c := &Conn{Conn: raw, lease: l, Warmed: warmed, WarmDuration: warmDuration, ReuseDuration: time.Since(started)}
	if err := l.track(c); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		c.Close()
		return nil, ctx.Err()
	}
	c.mu.Lock()
	if !c.closed && m.cfg.PassLifetime > 0 {
		c.timer = time.AfterFunc(m.cfg.PassLifetime, func() { c.Close() })
	}
	c.mu.Unlock()
	returned = true
	return c, nil
}

func (m *Manager) agedOut(port uint16, now time.Time) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	stamp, ok := m.warmed[port]
	return ok && (m.cfg.WarmAge <= 0 || now.Sub(stamp) >= m.cfg.WarmAge)
}

func (m *Manager) runWarm(ctx context.Context, l *lease) (time.Time, error) {
	if m.capture != nil {
		release, err := m.capture()
		if err != nil {
			return time.Time{}, err
		}
		defer release()
	}
	ctx, cancel := context.WithTimeout(ctx, m.cfg.WarmTimeout)
	defer cancel()
	started := time.Now()
	c, err := m.connect(ctx, l.port, m.cfg.WarmMark)
	if err != nil {
		var refused connectError
		if errors.As(err, &refused) {
			err = warmError{err}
		}
		return started, err
	}
	defer c.Close()
	if err := l.track(c); err != nil {
		return started, err
	}
	if negotiated, err := m.timestamped(c); err == nil && !negotiated {
		return started, warmError{ErrNoTimestamps}
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	if err := m.warm(ctx, c); err != nil {
		return started, warmError{err}
	}
	return started, nil
}

func (m *Manager) connect(ctx context.Context, port uint16, mark uint32) (net.Conn, error) {
	var last error
	for {
		if ctx.Err() != nil {
			return nil, m.heldPortError(ctx, port, last)
		}
		c, err := m.dial(ctx, port, mark)
		if err == nil {
			local, lok := c.LocalAddr().(*net.TCPAddr)
			remote, rok := c.RemoteAddr().(*net.TCPAddr)
			if !lok || !rok || local.AddrPort() != netip.AddrPortFrom(m.cfg.Source, port) || remote.AddrPort() != m.cfg.Target {
				c.Close()
				return nil, errors.New("kernel returned a different TCP tuple")
			}
			return c, nil
		}
		if !errors.Is(err, unix.EADDRINUSE) && !errors.Is(err, unix.EADDRNOTAVAIL) {
			return nil, connectError{err}
		}
		last = err
		timer := time.NewTimer(m.cfg.RetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, m.heldPortError(ctx, port, last)
		case <-timer.C:
		}
	}
}

func (m *Manager) heldPortError(ctx context.Context, port uint16, last error) error {
	switch {
	case last == nil:
		return ctx.Err()
	case errors.Is(last, unix.EADDRNOTAVAIL):
		return fmt.Errorf("source port %d to %s is still held by an earlier connection on this tuple (EADDRNOTAVAIL): TIME_WAIT without TCP timestamps, or the previous connection has not closed: %w; last attempt: %w", port, m.cfg.Target, ctx.Err(), last)
	}
	return fmt.Errorf("%w; last attempt: %w", ctx.Err(), last)
}

func negotiatedTimestamps(c net.Conn) (bool, error) {
	sc, ok := c.(syscall.Conn)
	if !ok {
		return false, errors.New("the warm connection has no socket")
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return false, err
	}
	var info *unix.TCPInfo
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		info, sockErr = unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
	}); err != nil {
		return false, err
	}
	if sockErr != nil {
		return false, sockErr
	}
	return info.Options&tcpiOptTimestamps != 0, nil
}

func (m *Manager) dialSocket(ctx context.Context, port uint16, mark uint32) (net.Conn, error) {
	d := net.Dialer{
		LocalAddr: net.TCPAddrFromAddrPort(netip.AddrPortFrom(m.cfg.Source, port)),
		Control: func(_, _ string, raw syscall.RawConn) error {
			var sockErr error
			err := raw.Control(func(fd uintptr) {
				sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1)
				if sockErr == nil && mark != 0 {
					sockErr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
				}
			})
			if err != nil {
				return err
			}
			return sockErr
		},
	}
	network := "tcp6"
	if m.cfg.Target.Addr().Is4() {
		network = "tcp4"
	}
	return d.DialContext(ctx, network, m.cfg.Target.String())
}

func (m *Manager) warmTLS(ctx context.Context, raw net.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := raw.SetDeadline(deadline); err != nil {
			return err
		}
	}
	tlsConfig := &tls.Config{
		ServerName: m.cfg.Host, RootCAs: m.cfg.RootCAs,
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		NextProtos: []string{"http/1.1"},
	}
	if m.cfg.CompactHello {
		tlsConfig.CurvePreferences = []tls.CurveID{tls.X25519}
	}
	c := tls.Client(raw, tlsConfig)
	defer c.Close()
	if err := c.HandshakeContext(ctx); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, "https://"+m.cfg.Host+"/generate_204", nil)
	if err != nil {
		return err
	}
	if err := req.Write(c); err != nil {
		return err
	}
	resp, err := http.ReadResponse(bufio.NewReader(io.LimitReader(c, 64*1024)), req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		return fmt.Errorf("warm HTTP status %d, expected 204", resp.StatusCode)
	}
	if err := c.CloseWrite(); err != nil {
		return err
	}
	half, ok := raw.(interface{ CloseWrite() error })
	if !ok {
		return errors.New("warm socket cannot send TCP FIN")
	}
	if err := half.CloseWrite(); err != nil {
		return err
	}
	n, err := io.Copy(io.Discard, io.LimitReader(c, 64*1024))
	if err != nil || n != 0 {
		return fmt.Errorf("warm TLS shutdown: bytes=%d error=%v", n, err)
	}
	n, err = io.Copy(io.Discard, io.LimitReader(raw, 64*1024))
	if err != nil || n == 64*1024 {
		return fmt.Errorf("warm TCP shutdown: bytes=%d error=%v", n, err)
	}
	return nil
}

func (c *Conn) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	if c.timer != nil {
		c.timer.Stop()
	}
	c.mu.Unlock()
	err := c.Conn.Close()
	c.lease.release()
	return err
}

func (c *Conn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Close()
}
