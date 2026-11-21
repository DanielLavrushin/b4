package tproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
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
	pass, isPass := backend.(interface{ Answered() })
	if isPass && singleClientHello(sniffed.prefix) {
		l.relayWatchedPass(client, backend, pass.Answered, set, sniffed, owner, dest)
		return
	}
	l.logReuseRelay(set, client, backend, sniffed)
	if err := writePrefix(backend, sniffed.prefix); err != nil {
		return
	}
	relayed := client
	if isPass {
		relayed = &passClient{Conn: client, answered: pass.Answered}
	}
	_ = socks5.Relay(relayed, backend)
}

func (l *Listener) logReuseRelay(set *config.SetConfig, client, backend net.Conn, sniffed sniffResult) {
	flag := "wssize-reuse"
	if fallback, ok := backend.(interface{ Fallback() bool }); ok && fallback.Fallback() {
		flag = "wssize-reuse-fallback"
	}
	from := metrics.DeviceOf(client.RemoteAddr())
	log.LogConnectionStr("TCP", set.Name, sniffed.host, client.RemoteAddr().String(), "", backend.RemoteAddr().String(), from.MAC, config.TLSVersionString(sniffed.tlsVersion), flag)
}

func (l *Listener) relayWatchedPass(client, backend net.Conn, answered func(), set *config.SetConfig, sniffed sniffResult, owner string, dest netip.AddrPort) {
	if err := writePrefix(backend, sniffed.prefix); err != nil {
		l.logReuseRelay(set, client, backend, sniffed)
		return
	}
	window := passAnswerTimeout
	first, verdict := awaitPassAnswer(client, backend, window)
	if verdict != passSilent {
		l.logReuseRelay(set, client, backend, sniffed)
		if verdict == passRelay {
			_ = socks5.Relay(&passClient{Conn: client, answered: answered}, &replayConn{Conn: backend, pending: first})
		}
		return
	}
	_ = backend.Close()
	ctx, stop := watchWarmClient(l.ctx, client)
	fallback, err := l.Warm.Fallback(ctx, set, owner, sniffed.host, dest)
	stop()
	if err != nil {
		l.logReuseRelay(set, client, backend, sniffed)
		l.noteWarmFailure(sniffed.host, fmt.Errorf("no answer to the hello within %s on the reused tuple; relay with the set's strategy: %w", window, err))
		return
	}
	defer fallback.Close()
	l.noteSilentPass(sniffed.host, window)
	l.logReuseRelay(set, client, fallback, sniffed)
	if err := writePrefix(fallback, sniffed.prefix); err != nil {
		return
	}
	_ = socks5.Relay(client, fallback)
}

func singleClientHello(b []byte) bool {
	if !isClientHelloRecord(b) || len(b) < tlsRecordHeader+4 {
		return false
	}
	record := int(b[3])<<8 | int(b[4])
	hello := int(b[6])<<16 | int(b[7])<<8 | int(b[8])
	return len(b) == tlsRecordHeader+record && record == 4+hello
}

type passVerdict int

const (
	passRelay passVerdict = iota
	passSilent
	passClientLeft
)

func awaitPassAnswer(client, backend net.Conn, window time.Duration) ([]byte, passVerdict) {
	gone, stop := watchWarmClient(context.Background(), client)
	defer stop()
	_ = backend.SetReadDeadline(time.Now().Add(window))
	read, woke := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(woke)
		select {
		case <-gone.Done():
			_ = backend.SetReadDeadline(time.Now())
		case <-read:
		}
	}()
	buf := make([]byte, sniffMaxBytes)
	n, err := backend.Read(buf)
	close(read)
	<-woke
	_ = backend.SetReadDeadline(time.Time{})
	switch {
	case n > 0:
		return append([]byte(nil), buf[:n]...), passRelay
	case !errors.Is(err, os.ErrDeadlineExceeded):
		return nil, passRelay
	case gone.Err() != nil:
		return nil, passClientLeft
	}
	return nil, passSilent
}

type passClient struct {
	net.Conn
	answered func()
	left     atomic.Bool
	once     sync.Once
}

func (c *passClient) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	if err != nil {
		c.left.Store(true)
	}
	return n, err
}

func (c *passClient) Write(p []byte) (int, error) {
	open := !c.left.Load()
	n, err := c.Conn.Write(p)
	if n > 0 && open {
		c.once.Do(c.answered)
	}
	return n, err
}

func (c *passClient) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

const warmWarnInterval = time.Minute

var passAnswerTimeout = 5 * time.Second

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

func (l *Listener) noteSilentPass(host string, window time.Duration) {
	if n, warn := l.silentPasses.note(time.Now()); warn {
		log.Warnf("WSSIZE reuse [%s]: %s: no answer to the hello within %s on a reused tuple; the tuple is dropped and the connection relayed with the set's strategy, %d connections so far", l.SetName, host, window, n)
		return
	}
	log.Tracef("WSSIZE reuse [%s]: %s: no answer to the hello within %s on a reused tuple, relayed with the set's strategy", l.SetName, host, window)
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
