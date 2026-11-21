package warmtuple

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"syscall"

	"golang.org/x/sys/unix"
)

const directBindTries = 16

var errWarmRangePort = errors.New("the kernel picked a warm-tuple source port")

var directPortReserved = func(port uint16) bool { return port >= firstPort && port <= lastPort }

type directDialer func(ctx context.Context, source netip.Addr, target netip.AddrPort, mark uint32, register func(netip.AddrPort) (func(), error)) (net.Conn, func(), error)

func dialDirect(ctx context.Context, source netip.Addr, target netip.AddrPort, mark uint32, register func(netip.AddrPort) (func(), error)) (net.Conn, func(), error) {
	network := "tcp6"
	if target.Addr().Is4() {
		network = "tcp4"
	}
	for try := 1; ; try++ {
		var release func()
		d := net.Dialer{Control: func(_, _ string, raw syscall.RawConn) error {
			var sockErr error
			err := raw.Control(func(fd uintptr) {
				release, sockErr = bindDirect(int(fd), source, mark, register)
			})
			if err != nil {
				return err
			}
			return sockErr
		}}
		c, err := d.DialContext(ctx, network, target.String())
		if err == nil {
			return c, release, nil
		}
		if release != nil {
			release()
		}
		if !errors.Is(err, errWarmRangePort) || try == directBindTries {
			return nil, nil, err
		}
	}
}

func bindDirect(fd int, source netip.Addr, mark uint32, register func(netip.AddrPort) (func(), error)) (func(), error) {
	var sa unix.Sockaddr = &unix.SockaddrInet6{Addr: source.As16()}
	if source.Is4() {
		sa = &unix.SockaddrInet4{Addr: source.As4()}
	}
	if err := unix.Bind(fd, sa); err != nil {
		return nil, err
	}
	bound, err := unix.Getsockname(fd)
	if err != nil {
		return nil, err
	}
	var local netip.AddrPort
	switch a := bound.(type) {
	case *unix.SockaddrInet4:
		local = netip.AddrPortFrom(netip.AddrFrom4(a.Addr), uint16(a.Port))
	case *unix.SockaddrInet6:
		local = netip.AddrPortFrom(netip.AddrFrom16(a.Addr).Unmap(), uint16(a.Port))
	default:
		return nil, errors.New("the fallback socket has no IP address")
	}
	if directPortReserved(local.Port()) {
		return nil, errWarmRangePort
	}
	if mark != 0 {
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK, int(mark)); err != nil {
			return nil, err
		}
	}
	return register(local)
}

type directConn struct {
	net.Conn
	service *Service
	release func()
	once    sync.Once
}

func (c *directConn) Fallback() bool { return true }

func (c *directConn) Close() error {
	var err error
	c.once.Do(func() {
		err = c.Conn.Close()
		c.release()
		s := c.service
		s.mu.Lock()
		delete(s.directs, c)
		s.fallbacks--
		s.mu.Unlock()
	})
	return err
}

func (c *directConn) CloseWrite() error {
	if half, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return half.CloseWrite()
	}
	return c.Close()
}
