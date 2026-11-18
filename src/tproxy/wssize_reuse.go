package tproxy

import (
	"context"
	"net"
	"net/netip"
	"sync"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/socks5"
	"golang.org/x/sys/unix"
)

type WSReuseDialer interface {
	Dial(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error)
	AcquireClient() (func(), bool)
}

func (l *Listener) serveWSReuse(client net.Conn, ip net.IP, port int, given *sniffResult) {
	set := l.set.Load()
	if l.Warm == nil || !set.ReusesWSSize() || port != 443 {
		return
	}
	stop := context.AfterFunc(l.ctx, func() { _ = client.Close() })
	defer stop()
	sniffed := sniffResult{}
	if given != nil {
		sniffed = *given
	} else {
		sniffed = sniffClient(client, time.Second, 3*time.Second, sniffMaxBytes)
	}
	if sniffed.host == "" || sniffed.tlsVersion == 0 {
		log.Tracef("WSSIZE reuse [%s]: missing TLS SNI from %s", l.SetName, client.RemoteAddr())
		return
	}
	target, ok := netip.AddrFromSlice(ip)
	if !ok {
		return
	}
	owner, _, err := net.SplitHostPort(client.RemoteAddr().String())
	if err != nil {
		return
	}
	warmCtx, stopWatch := watchWarmClient(l.ctx, client)
	backend, err := l.Warm.Dial(warmCtx, set, owner, sniffed.host, netip.AddrPortFrom(target.Unmap(), uint16(port)))
	stopWatch()
	listening := stop()
	if err != nil {
		log.Warnf("WSSIZE reuse [%s]: %s: %v", l.SetName, sniffed.host, err)
		return
	}
	defer backend.Close()
	if !listening {
		return
	}
	from := metrics.DeviceOf(client.RemoteAddr())
	log.LogConnectionStr("TCP", set.Name, sniffed.host, client.RemoteAddr().String(), "", backend.RemoteAddr().String(), from.MAC, config.TLSVersionString(sniffed.tlsVersion), "wssize-reuse")
	if err := writePrefix(backend, sniffed.prefix); err != nil {
		return
	}
	_ = socks5.Relay(client, backend)
}

var warmClientPoll = unix.Poll

func watchWarmClient(parent context.Context, client net.Conn) (context.Context, func()) {
	ctx, cancel := context.WithCancel(parent)
	socket, ok := client.(syscall.Conn)
	if !ok {
		return ctx, cancel
	}
	raw, err := socket.SyscallConn()
	if err != nil {
		cancel()
		return ctx, cancel
	}
	done, exited := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(exited)
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-ctx.Done():
				return
			case <-tick.C:
				closed := false
				err := raw.Control(func(fd uintptr) {
					fds := []unix.PollFd{{Fd: int32(fd), Events: unix.POLLRDHUP | unix.POLLHUP | unix.POLLERR}}
					n, e := warmClientPoll(fds, 0)
					closed = e == nil && n > 0 && fds[0].Revents&(unix.POLLRDHUP|unix.POLLHUP|unix.POLLNVAL) != 0
				})
				if err != nil || closed {
					cancel()
					return
				}
			}
		}
	}()
	var once sync.Once
	return ctx, func() { once.Do(func() { close(done); <-exited; cancel() }) }
}
