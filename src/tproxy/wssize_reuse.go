package tproxy

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/socks5"
	"github.com/daniellavrushin/b4/warmtuple"
	"golang.org/x/sys/unix"
)

type WSReuseDialer interface {
	Dial(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error)
	Fallback(context.Context, *config.SetConfig, string, string, netip.AddrPort) (net.Conn, error)
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
	dest := netip.AddrPortFrom(target.Unmap(), uint16(port))
	warmCtx, stopWatch := watchWarmClient(l.ctx, client)
	var backend net.Conn
	if release, ok := l.Warm.AcquireClient(); ok {
		backend, err = l.Warm.Dial(warmCtx, set, owner, sniffed.host, dest)
		release()
	} else if backend, err = l.Warm.Fallback(warmCtx, set, owner, sniffed.host, dest); err == nil {
		l.noteWarmSlotsFull()
	}
	stopWatch()
	listening := stop()
	if err != nil {
		l.noteWarmFailure(sniffed.host, err)
		return
	}
	defer backend.Close()
	if !listening {
		return
	}
	flag := "wssize-reuse"
	if fallback, ok := backend.(interface{ Fallback() bool }); ok && fallback.Fallback() {
		flag = "wssize-reuse-fallback"
	}
	from := metrics.DeviceOf(client.RemoteAddr())
	log.LogConnectionStr("TCP", set.Name, sniffed.host, client.RemoteAddr().String(), "", backend.RemoteAddr().String(), from.MAC, config.TLSVersionString(sniffed.tlsVersion), flag)
	if err := writePrefix(backend, sniffed.prefix); err != nil {
		return
	}
	_ = socks5.Relay(client, backend)
}

const warmWarnInterval = time.Minute

type warnLimiter struct {
	count atomic.Int64
	last  atomic.Int64
}

func (w *warnLimiter) note(now time.Time) (int64, bool) {
	n := w.count.Add(1)
	last := w.last.Load()
	if last != 0 && now.UnixNano()-last < int64(warmWarnInterval) {
		return n, false
	}
	return n, w.last.CompareAndSwap(last, now.UnixNano())
}

func (l *Listener) noteWarmSlotsFull() {
	if n, warn := l.warmSlotsFull.note(time.Now()); warn {
		log.Warnf("WSSIZE reuse [%s]: every warm slot is busy, %d connections relayed with the set's strategy without reuse so far", l.SetName, n)
		return
	}
	log.Tracef("WSSIZE reuse [%s]: every warm slot is busy, a connection is relayed with the set's strategy without reuse", l.SetName)
}

func (l *Listener) noteWarmFailure(host string, err error) {
	limiter := &l.dialFailed
	switch {
	case errors.Is(err, warmtuple.ErrFallbackCapacity):
		limiter = &l.fallbacksFull
	case errors.Is(err, warmtuple.ErrCapacity):
		limiter = &l.warmPortsLeased
	}
	if n, warn := limiter.note(time.Now()); warn {
		log.Warnf("WSSIZE reuse [%s]: %s: %v, %d connections closed without a relay so far", l.SetName, host, err, n)
		return
	}
	log.Tracef("WSSIZE reuse [%s]: %s: %v", l.SetName, host, err)
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
