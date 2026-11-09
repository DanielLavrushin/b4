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
)

type Config struct {
	Host          string
	Target        netip.AddrPort
	Source        netip.Addr
	FirstPort     uint16
	LastPort      uint16
	WarmTimeout   time.Duration
	ReuseTimeout  time.Duration
	RetryInterval time.Duration
	PassLifetime  time.Duration
	RootCAs       *x509.CertPool
}

type Manager struct {
	cfg    Config
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	leases map[uint16]*lease
	dial   func(context.Context, uint16, uint32) (net.Conn, error)
	warm   func(context.Context, net.Conn) error
}

type lease struct {
	manager *Manager
	port    uint16
	conn    net.Conn
	once    sync.Once
}

type Conn struct {
	net.Conn
	WarmDuration  time.Duration
	ReuseDuration time.Duration
	lease         *lease
	mu            sync.Mutex
	timer         *time.Timer
	closed        bool
}

func New(cfg Config) (*Manager, error) {
	cfg.Host = normalizeHost(cfg.Host)
	if !validHost(cfg.Host) || !cfg.Source.Is4() || cfg.Source.IsUnspecified() ||
		!cfg.Target.IsValid() || !cfg.Target.Addr().Is4() || cfg.Target.Addr().IsUnspecified() || cfg.Target.Port() == 0 {
		return nil, errors.New("a hostname, explicit source IPv4 and target IPv4:port are required")
	}
	if cfg.FirstPort == 0 || cfg.LastPort < cfg.FirstPort || int(cfg.LastPort)-int(cfg.FirstPort) >= 32 {
		return nil, errors.New("source port range must contain 1 to 32 nonzero ports")
	}
	if cfg.WarmTimeout <= 0 || cfg.WarmTimeout > 3*time.Second ||
		cfg.ReuseTimeout <= 0 || cfg.ReuseTimeout > 5*time.Second ||
		cfg.RetryInterval < 20*time.Millisecond || cfg.RetryInterval > 500*time.Millisecond ||
		cfg.PassLifetime <= 0 || cfg.PassLifetime > 5*time.Minute {
		return nil, errors.New("invalid bounded warm, reuse, retry or pass lifetime")
	}
	if cfg.RootCAs != nil {
		cfg.RootCAs = cfg.RootCAs.Clone()
	}
	ctx, cancel := context.WithCancel(context.Background())
	m := &Manager{cfg: cfg, ctx: ctx, cancel: cancel, leases: make(map[uint16]*lease)}
	m.dial, m.warm = m.dialSocket, m.warmTLS
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
	for p := int(m.cfg.FirstPort); p <= int(m.cfg.LastPort); p++ {
		port := uint16(p)
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
	err = m.runWarm(ctx, l)
	warmDuration := time.Since(started)
	if err != nil {
		return nil, fmt.Errorf("warm: %w", err)
	}
	started = time.Now()
	passCtx, passCancel := context.WithTimeout(ctx, m.cfg.ReuseTimeout)
	defer passCancel()
	settle := time.NewTimer(m.cfg.RetryInterval)
	select {
	case <-passCtx.Done():
		settle.Stop()
		return nil, fmt.Errorf("same-tuple reconnect: %w", passCtx.Err())
	case <-settle.C:
	}
	raw, err := m.connect(passCtx, l.port, config.SelfDialRelayMark)
	if err != nil {
		return nil, fmt.Errorf("same-tuple reconnect: %w", err)
	}
	c := &Conn{Conn: raw, lease: l, WarmDuration: warmDuration, ReuseDuration: time.Since(started)}
	if err := l.track(c); err != nil {
		return nil, err
	}
	if ctx.Err() != nil {
		c.Close()
		return nil, ctx.Err()
	}
	c.mu.Lock()
	if !c.closed {
		c.timer = time.AfterFunc(m.cfg.PassLifetime, func() { c.Close() })
	}
	c.mu.Unlock()
	returned = true
	return c, nil
}

func (m *Manager) runWarm(ctx context.Context, l *lease) error {
	ctx, cancel := context.WithTimeout(ctx, m.cfg.WarmTimeout)
	defer cancel()
	c, err := m.connect(ctx, l.port, 0)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := l.track(c); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { c.Close() })
	defer stop()
	return m.warm(ctx, c)
}

func (m *Manager) connect(ctx context.Context, port uint16, mark uint32) (net.Conn, error) {
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
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
			return nil, err
		}
		timer := time.NewTimer(m.cfg.RetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
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
	return d.DialContext(ctx, "tcp4", m.cfg.Target.String())
}

func (m *Manager) warmTLS(ctx context.Context, raw net.Conn) error {
	if deadline, ok := ctx.Deadline(); ok {
		if err := raw.SetDeadline(deadline); err != nil {
			return err
		}
	}
	c := tls.Client(raw, &tls.Config{
		ServerName: m.cfg.Host, RootCAs: m.cfg.RootCAs,
		MinVersion: tls.VersionTLS13, MaxVersion: tls.VersionTLS13,
		NextProtos: []string{"http/1.1"},
	})
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
