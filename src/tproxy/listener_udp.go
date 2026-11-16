package tproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/socks5"
)

const (
	udpBufSize       = 64 * 1024
	udpIdleTimeout   = 60 * time.Second
	udpReaperPeriod  = 15 * time.Second
	udpSetupTimeout  = 10 * time.Second
	udpUpstreamRetry = 5 * time.Second
	udpRefusalNotice = 60 * time.Second
	udpPendingQueue  = 16
	udpMaxSetups     = 32

	udpSessionLimitMin    = 64
	udpSessionLimitMax    = 1024
	udpOpenFilesPerBudget = 8
)

var errUDPUpstreamDown = errors.New("the upstream refused the last UDP session, waiting before the next attempt")

var (
	udpSessionLimit atomic.Int64
	udpSessionsOpen atomic.Int64
)

func udpSessionLimitFor(openFiles uint64) int64 {
	limit := int64(openFiles / udpOpenFilesPerBudget)
	return max(udpSessionLimitMin, min(limit, udpSessionLimitMax))
}

func udpSessionCap() int64 {
	if limit := udpSessionLimit.Load(); limit > 0 {
		return limit
	}
	var open syscall.Rlimit
	if err := syscall.Getrlimit(syscall.RLIMIT_NOFILE, &open); err != nil {
		open.Cur = 0
	}
	udpSessionLimit.CompareAndSwap(0, udpSessionLimitFor(open.Cur))
	return udpSessionLimit.Load()
}

func udpReserveSession() bool {
	limit := udpSessionCap()
	for {
		open := udpSessionsOpen.Load()
		if open >= limit {
			return false
		}
		if udpSessionsOpen.CompareAndSwap(open, open+1) {
			return true
		}
	}
}

type udpRelay interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	SetReadDeadline(t time.Time) error
	Close() error
}

type udpReply interface {
	WriteToUDP(b []byte, addr *net.UDPAddr) (int, error)
	Close() error
}

type directUDP struct{ conn *net.UDPConn }

func (d *directUDP) Write(p []byte) (int, error)       { return d.conn.Write(p) }
func (d *directUDP) Read(p []byte) (int, error)        { return d.conn.Read(p) }
func (d *directUDP) SetReadDeadline(t time.Time) error { return d.conn.SetReadDeadline(t) }
func (d *directUDP) Close() error                      { return d.conn.Close() }

var udpOpenReply = func(ctx context.Context, dst *net.UDPAddr, v6 bool, mark uint32) (udpReply, error) {
	conn, err := openReplySocket(ctx, dst, v6, mark)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

var udpDialUpstream = func(ctx context.Context, cfg socks5.ClientConfig, dst *net.UDPAddr) (udpRelay, error) {
	up, err := socks5.DialUpstreamUDP(ctx, cfg, dst.IP, dst.Port)
	if err != nil {
		return nil, err
	}
	return up, nil
}

var udpDialDirect = func(ctx context.Context, dst *net.UDPAddr, mark uint32) (udpRelay, error) {
	d := markedDialer(udpSetupTimeout, mark)
	c, err := d.DialContext(ctx, "udp", dst.String())
	if err != nil {
		return nil, err
	}
	uc, ok := c.(*net.UDPConn)
	if !ok {
		c.Close()
		return nil, fmt.Errorf("direct dial not *net.UDPConn")
	}
	return &directUDP{conn: uc}, nil
}

type udpSession struct {
	client *net.UDPAddr
	last   atomic.Int64

	mu      sync.Mutex
	relay   udpRelay
	reply   udpReply
	release func()
	queue   [][]byte
	ready   bool
	closed  bool
	slot    sync.Once
}

func (s *udpSession) freeSlot() {
	s.slot.Do(func() { udpSessionsOpen.Add(-1) })
}

func (s *udpSession) send(payload []byte) error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return net.ErrClosed
	}
	if !s.ready {
		if len(s.queue) < udpPendingQueue {
			s.queue = append(s.queue, payload)
		}
		s.mu.Unlock()
		return nil
	}
	relay := s.relay
	s.mu.Unlock()
	_, err := relay.Write(payload)
	return err
}

func (s *udpSession) open(relay udpRelay, reply udpReply, release func()) bool {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return false
	}
	s.relay, s.reply, s.release = relay, reply, release
	for len(s.queue) > 0 && !s.closed {
		queued := s.queue
		s.queue = nil
		s.mu.Unlock()
		for _, payload := range queued {
			if _, err := relay.Write(payload); err != nil {
				break
			}
		}
		s.mu.Lock()
	}
	s.ready = true
	s.mu.Unlock()
	return true
}

func (s *udpSession) close() {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return
	}
	s.closed = true
	relay, reply, release := s.relay, s.reply, s.release
	s.queue = nil
	s.mu.Unlock()
	if relay == nil {
		return
	}
	relay.Close()
	reply.Close()
	if release != nil {
		release()
	}
	s.freeSlot()
}

func (l *Listener) startUDP(addr4, addr6 string) {
	l.udpSessions = make(map[string]*udpSession)

	if uc, err := listenTransparentUDP(l.ctx, "udp4", addr4, false, l.Upstream.BypassMark); err == nil {
		l.udpV4 = uc
		go l.udpReadLoop(uc, false)
		log.Infof("tproxy: UDP listening on %s (v4) for set %q -> %s:%d", addr4, l.SetName, l.Upstream.Host, l.Upstream.Port)
	} else {
		log.Errorf("tproxy: UDP v4 listen %s failed for set %q: %v", addr4, l.SetName, err)
	}

	if uc, err := listenTransparentUDP(l.ctx, "udp6", addr6, true, l.Upstream.BypassMark); err == nil {
		l.udpV6 = uc
		go l.udpReadLoop(uc, true)
		log.Infof("tproxy: UDP listening on %s (v6) for set %q -> %s:%d", addr6, l.SetName, l.Upstream.Host, l.Upstream.Port)
	} else {
		log.Tracef("tproxy: UDP v6 listener disabled for set %q: %v", l.SetName, err)
	}

	go l.udpReaper()
}

func (l *Listener) stopUDP() {
	if l.udpV4 != nil {
		_ = l.udpV4.Close()
	}
	if l.udpV6 != nil {
		_ = l.udpV6.Close()
	}
	l.udpMu.Lock()
	sessions := l.udpSessions
	l.udpSessions = make(map[string]*udpSession)
	l.udpMu.Unlock()
	for _, s := range sessions {
		s.close()
	}
}

func (l *Listener) udpReadLoop(conn *net.UDPConn, v6 bool) {
	buf := make([]byte, udpBufSize)
	oob := make([]byte, 1024)
	for {
		n, oobn, _, src, err := conn.ReadMsgUDP(buf, oob)
		if err != nil {
			if l.ctx.Err() != nil {
				return
			}
			if isClosedErr(err) {
				return
			}
			log.Tracef("tproxy: UDP read error on set %q: %v", l.SetName, err)
			time.Sleep(50 * time.Millisecond)
			continue
		}
		dst, perr := parseOrigDst(oob[:oobn], v6)
		if perr != nil {
			log.Tracef("tproxy: UDP missing original dst on set %q: %v", l.SetName, perr)
			continue
		}
		payload := make([]byte, n)
		copy(payload, buf[:n])
		l.dispatchUDP(src, dst, payload, v6)
	}
}

func (l *Listener) dispatchUDP(src, dst *net.UDPAddr, payload []byte, v6 bool) {
	key := src.String() + "|" + dst.String()
	now := time.Now()

	l.udpMu.Lock()
	sess, ok := l.udpSessions[key]
	if !ok {
		if l.ctx.Err() != nil {
			l.udpMu.Unlock()
			return
		}
		reason := l.udpRefusesNewLocked(now)
		if reason == "" && !udpReserveSession() {
			reason = udpRefusedLimit
		}
		if reason != "" {
			l.udpMu.Unlock()
			l.noteUDPRefused(now, reason, dst)
			return
		}
		sess = &udpSession{client: src, queue: [][]byte{payload}}
		sess.last.Store(now.UnixNano())
		l.udpSessions[key] = sess
		l.udpSetups++
		l.udpMu.Unlock()
		go l.setupUDPSession(key, sess, src, dst, v6)
		return
	}
	l.udpMu.Unlock()

	sess.last.Store(now.UnixNano())
	if err := sess.send(payload); err != nil {
		log.Tracef("tproxy: UDP relay write failed for %s on set %q: %v", dst, l.SetName, err)
		l.closeUDPSession(key, sess)
	}
}

const (
	udpRefusedLimit    = "limit"
	udpRefusedSetups   = "setups"
	udpRefusedUpstream = "upstream"
)

func (l *Listener) udpRefusesNewLocked(now time.Time) string {
	switch {
	case l.udpSetups >= udpMaxSetups:
		return udpRefusedSetups
	case !l.FailOpen && now.UnixNano() < l.udpDownUntil.Load():
		return udpRefusedUpstream
	}
	return ""
}

func (l *Listener) noteUDPRefused(now time.Time, reason string, dst *net.UDPAddr) {
	if reason != udpRefusedLimit {
		log.Tracef("tproxy: set %q dropped a new UDP flow to %s (%s)", l.SetName, dst, reason)
		return
	}
	dropped := l.udpRefused.Add(1)
	last := l.udpRefusedAt.Load()
	if now.UnixNano()-last < int64(udpRefusalNotice) || !l.udpRefusedAt.CompareAndSwap(last, now.UnixNano()) {
		return
	}
	log.Warnf("tproxy: set %q dropped a new UDP flow to %s: %d UDP sessions are open through upstream proxies, the most this router's open-file allowance leaves room for. Flows already open keep running, and new ones are taken again as old ones close after %v without traffic (%d dropped so far)",
		l.SetName, dst, udpSessionsOpen.Load(), udpIdleTimeout, dropped)
}

func (l *Listener) setupUDPSession(key string, sess *udpSession, src, dst *net.UDPAddr, v6 bool) {
	from := metrics.DeviceOf(src)
	relay, reply, action, release, err := l.openUDPSession(src, dst, v6)

	l.udpMu.Lock()
	l.udpSetups--
	l.udpMu.Unlock()

	if err != nil {
		l.udpFailWarn.Do(func() {
			log.Warnf("tproxy: set %q cannot relay UDP through %s:%d: %v. Matched UDP (including QUIC/HTTP-3) is being dropped; most SOCKS5 proxies are TCP-only, so turn 'Route UDP through upstream' off unless the proxy supports UDP ASSOCIATE",
				l.SetName, l.Upstream.Host, l.Upstream.Port, err)
		})
		log.Tracef("tproxy: UDP session setup failed for %s on set %q: %v", dst, l.SetName, err)
		l.closeUDPSession(key, sess)
		sess.freeSlot()
		return
	}
	if !sess.open(relay, reply, release) {
		relay.Close()
		reply.Close()
		if release != nil {
			release()
		}
		sess.freeSlot()
		return
	}

	domain := l.udpLogDomain(src, dst)
	log.LogConnectionStr("UDP", l.SetName, domain, src.String(), "",
		net.JoinHostPort(dst.IP.String(), fmt.Sprintf("%d", dst.Port)),
		from.MAC, "", action)
	l.countConnection(from)
	l.countTarget(from, domain, dst.IP)
	l.udpReplyLoop(key, sess, relay, reply)
}

func (l *Listener) openUDPSession(src, dst *net.UDPAddr, v6 bool) (udpRelay, udpReply, string, func(), error) {
	reply, err := udpOpenReply(l.ctx, dst, v6, l.Upstream.BypassMark)
	if err != nil {
		return nil, nil, "", nil, fmt.Errorf("reply socket: %w", err)
	}
	relay, action, release, err := l.openUDPRelay(src, dst)
	if err != nil {
		reply.Close()
		return nil, nil, "", nil, err
	}
	return relay, reply, action, release, nil
}

func (l *Listener) openUDPRelay(src, dst *net.UDPAddr) (udpRelay, string, func(), error) {
	keys := relayKeys("udp", dst.Port, dst.IP.String())
	if l.guard.fromUpstreamHost(src.IP) && (l.coversEverything() || l.relaying(keys)) {
		l.warnUpstreamLoop("host " + src.IP.String())
		direct, err := udpDialDirect(l.ctx, dst, l.Upstream.BypassMark)
		if err != nil {
			return nil, "", nil, err
		}
		return direct, "direct", nil, nil
	}
	derr := errUDPUpstreamDown
	if time.Now().UnixNano() >= l.udpDownUntil.Load() {
		release := l.holdRelay(keys)
		dialCtx, cancel := context.WithTimeout(l.ctx, udpSetupTimeout)
		var up udpRelay
		up, derr = udpDialUpstream(dialCtx, l.Upstream, dst)
		cancel()
		if derr == nil {
			return up, "proxy", release, nil
		}
		release()
		l.udpDownUntil.Store(time.Now().Add(udpUpstreamRetry).UnixNano())
	}
	if !l.FailOpen {
		return nil, "", nil, derr
	}
	direct, ferr := udpDialDirect(l.ctx, dst, l.Upstream.BypassMark)
	if ferr != nil {
		return nil, "", nil, fmt.Errorf("upstream %v; fail-open %w", derr, ferr)
	}
	return direct, "proxy", nil, nil
}

func (l *Listener) udpLogDomain(src, dst *net.UDPAddr) string {
	if l.Names == nil {
		return ""
	}
	names := l.Names.ObservedNames(src.IP, dst.IP)
	if len(names.Own) > 0 {
		return names.Own[0]
	}
	return l.Names.LearnedName(dst.IP, l.SetID)
}

func (l *Listener) udpReplyLoop(key string, sess *udpSession, relay udpRelay, reply udpReply) {
	buf := make([]byte, udpBufSize)
	for {
		_ = relay.SetReadDeadline(time.Now().Add(udpIdleTimeout))
		n, err := relay.Read(buf)
		if err != nil {
			l.closeUDPSession(key, sess)
			return
		}
		sess.last.Store(time.Now().UnixNano())
		if _, err := reply.WriteToUDP(buf[:n], sess.client); err != nil {
			l.closeUDPSession(key, sess)
			return
		}
	}
}

func (l *Listener) closeUDPSession(key string, sess *udpSession) {
	l.udpMu.Lock()
	cur, ok := l.udpSessions[key]
	if ok && cur == sess {
		delete(l.udpSessions, key)
	}
	l.udpMu.Unlock()
	sess.close()
}

func (l *Listener) udpReaper() {
	ticker := time.NewTicker(udpReaperPeriod)
	defer ticker.Stop()
	for {
		select {
		case <-l.ctx.Done():
			return
		case <-ticker.C:
			cutoff := time.Now().Add(-udpIdleTimeout).UnixNano()
			stale := map[string]*udpSession{}
			l.udpMu.Lock()
			for k, s := range l.udpSessions {
				if s.last.Load() < cutoff {
					stale[k] = s
				}
			}
			l.udpMu.Unlock()
			for k, s := range stale {
				l.closeUDPSession(k, s)
			}
		}
	}
}
