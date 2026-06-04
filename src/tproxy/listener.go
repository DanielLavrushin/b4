package tproxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/socks5"
	"golang.org/x/sys/unix"
)

func markedDialer(timeout time.Duration, bypassMark uint32) net.Dialer {
	d := net.Dialer{Timeout: timeout}
	socks5.ApplyBypassMark(&d, bypassMark)
	return d
}

type DomainResolver interface {
	DomainFor(ip net.IP) string
}

type MTProtoBridge interface {
	Handle(client net.Conn, origIP net.IP, origPort int) (bool, net.Conn)
}

type Listener struct {
	SetID     string
	SetName   string
	BindAddr  string
	BindAddr6 string
	Port      int
	Upstream  socks5.ClientConfig
	UseDomain bool
	FailOpen  bool
	Resolver  DomainResolver
	MTProtoWS bool
	UDP       bool
	Bridge    MTProtoBridge

	ctx    context.Context
	cancel context.CancelFunc
	lnV4   net.Listener
	lnV6   net.Listener

	udpV4       *net.UDPConn
	udpV6       *net.UDPConn
	udpMu       sync.Mutex
	udpSessions map[string]*udpSession

	activeConns atomic.Int64
}

func (l *Listener) Start(parent context.Context) error {
	if l.Port < 1 || l.Port > 65535 {
		return fmt.Errorf("invalid tproxy port: %d", l.Port)
	}
	bind4 := l.BindAddr
	if bind4 == "" {
		bind4 = "0.0.0.0"
	}
	bind6 := l.BindAddr6
	if bind6 == "" {
		bind6 = "::"
	}
	addr4 := net.JoinHostPort(bind4, fmt.Sprintf("%d", l.Port))
	addr6 := net.JoinHostPort(bind6, fmt.Sprintf("%d", l.Port))

	l.ctx, l.cancel = context.WithCancel(parent)

	lnV4, err := listenTransparent(l.ctx, "tcp4", addr4, false)
	if err != nil {
		l.cancel()
		return fmt.Errorf("tproxy v4 listen %s: %w", addr4, err)
	}
	l.lnV4 = lnV4
	go l.acceptLoop(lnV4, "v4")
	log.Infof("tproxy: listening on %s (v4) for set %q -> %s:%d", addr4, l.SetName, l.Upstream.Host, l.Upstream.Port)

	if l.UDP && !l.MTProtoWS {
		l.startUDP(addr4, addr6)
	}

	lnV6, err := listenTransparent(l.ctx, "tcp6", addr6, true)
	if err != nil {
		log.Tracef("tproxy: v6 listener disabled for set %q: %v", l.SetName, err)
		return nil
	}
	l.lnV6 = lnV6
	go l.acceptLoop(lnV6, "v6")
	log.Infof("tproxy: listening on %s (v6) for set %q -> %s:%d", addr6, l.SetName, l.Upstream.Host, l.Upstream.Port)

	return nil
}

func listenTransparent(ctx context.Context, network, addr string, v6 bool) (net.Listener, error) {
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var ctlErr error
			err := c.Control(func(fd uintptr) {
				if e := unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); e != nil {
					ctlErr = fmt.Errorf("set SO_REUSEADDR: %w", e)
					return
				}
				if v6 {
					if e := unix.SetsockoptInt(int(fd), unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 1); e != nil {
						ctlErr = fmt.Errorf("set IPV6_V6ONLY: %w", e)
						return
					}
					if e := unix.SetsockoptInt(int(fd), unix.SOL_IPV6, unix.IPV6_TRANSPARENT, 1); e != nil {
						ctlErr = fmt.Errorf("set IPV6_TRANSPARENT: %w", e)
						return
					}
				} else {
					if e := unix.SetsockoptInt(int(fd), unix.SOL_IP, unix.IP_TRANSPARENT, 1); e != nil {
						ctlErr = fmt.Errorf("set IP_TRANSPARENT: %w", e)
						return
					}
				}
			})
			if err != nil {
				return err
			}
			return ctlErr
		},
	}
	return lc.Listen(ctx, network, addr)
}

func (l *Listener) Stop() error {
	if l.cancel != nil {
		l.cancel()
	}
	l.stopUDP()
	var firstErr error
	if l.lnV4 != nil {
		if err := l.lnV4.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if l.lnV6 != nil {
		if err := l.lnV6.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (l *Listener) Active() int64 {
	return l.activeConns.Load()
}

func (l *Listener) acceptLoop(ln net.Listener, family string) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			if l.ctx.Err() != nil {
				return
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Tracef("tproxy: accept error on set %q (%s): %v", l.SetName, family, err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		go l.handle(conn)
	}
}

func (l *Listener) handle(client net.Conn) {
	l.activeConns.Add(1)
	defer l.activeConns.Add(-1)
	defer client.Close()

	tcpAddr, ok := client.LocalAddr().(*net.TCPAddr)
	if !ok || tcpAddr == nil || tcpAddr.IP == nil {
		log.Tracef("tproxy: missing original dst on set %q", l.SetName)
		return
	}
	origIP := tcpAddr.IP
	origPort := tcpAddr.Port

	if l.MTProtoWS {
		src := ""
		if r := client.RemoteAddr(); r != nil {
			src = r.String()
		}
		log.LogConnectionStr("TCP", l.SetName, "", src, "",
			net.JoinHostPort(origIP.String(), fmt.Sprintf("%d", origPort)),
			"", "", "mtproto-ws")
		if l.Bridge != nil {
			if handled, failover := l.Bridge.Handle(client, origIP, origPort); handled {
				return
			} else if failover != nil {
				client = failover
			}
		}
		l.failOpenDirect(client, origIP, origPort)
		return
	}

	domain := ""
	if l.Resolver != nil {
		domain = l.Resolver.DomainFor(origIP)
	}
	targetHost := origIP.String()
	if l.UseDomain && domain != "" {
		targetHost = domain
	}

	src := ""
	if r := client.RemoteAddr(); r != nil {
		src = r.String()
	}
	log.LogConnectionStr("TCP", l.SetName, domain, src, "",
		net.JoinHostPort(origIP.String(), fmt.Sprintf("%d", origPort)),
		"", "", "proxy")

	dialCtx, cancel := context.WithTimeout(l.ctx, 15*time.Second)
	upstream, err := socks5.DialUpstream(dialCtx, l.Upstream, targetHost, origPort)
	cancel()
	if err != nil {
		log.Tracef("tproxy: upstream dial failed for %s:%d on set %q: %v", targetHost, origPort, l.SetName, err)
		if !l.FailOpen {
			return
		}
		// Fail-open direct dial must also carry the bypass mark, otherwise
		// the OUTPUT mark rule will catch it (daddr is in our set) and
		// re-redirect it to ourselves — infinite loop.
		failoverDialer := markedDialer(10*time.Second, l.Upstream.BypassMark)
		failoverCtx, failoverCancel := context.WithTimeout(l.ctx, 10*time.Second)
		direct, derr := failoverDialer.DialContext(failoverCtx, "tcp", net.JoinHostPort(origIP.String(), fmt.Sprintf("%d", origPort)))
		failoverCancel()
		if derr != nil {
			log.Tracef("tproxy: fail-open direct dial failed: %v", derr)
			return
		}
		upstream = direct
	}
	defer upstream.Close()

	pipe(client, upstream)
}

func (l *Listener) failOpenDirect(client net.Conn, origIP net.IP, origPort int) {
	dialer := markedDialer(10*time.Second, l.Upstream.BypassMark)
	ctx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
	direct, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(origIP.String(), fmt.Sprintf("%d", origPort)))
	cancel()
	if err != nil {
		log.Tracef("tproxy: mtproto-ws fail-open direct dial failed for %s:%d: %v", origIP, origPort, err)
		return
	}
	defer direct.Close()
	pipe(client, direct)
}

func pipe(a, b net.Conn) {
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(a, b)
		if c, ok := a.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(b, a)
		if c, ok := b.(interface{ CloseWrite() error }); ok {
			_ = c.CloseWrite()
		}
	}()
	wg.Wait()
}
