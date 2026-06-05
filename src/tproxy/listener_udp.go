package tproxy

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/socks5"
)

const (
	udpBufSize      = 64 * 1024
	udpIdleTimeout  = 60 * time.Second
	udpReaperPeriod = 15 * time.Second
)

type udpRelay interface {
	Write(p []byte) (int, error)
	Read(p []byte) (int, error)
	SetReadDeadline(t time.Time) error
	Close() error
}

type directUDP struct{ conn *net.UDPConn }

func (d *directUDP) Write(p []byte) (int, error)       { return d.conn.Write(p) }
func (d *directUDP) Read(p []byte) (int, error)        { return d.conn.Read(p) }
func (d *directUDP) SetReadDeadline(t time.Time) error { return d.conn.SetReadDeadline(t) }
func (d *directUDP) Close() error                      { return d.conn.Close() }

type udpSession struct {
	relay  udpRelay
	reply  *net.UDPConn
	client *net.UDPAddr
	last   atomic.Int64
}

func (l *Listener) startUDP(addr4, addr6 string) {
	l.udpSessions = make(map[string]*udpSession)

	if uc, err := listenTransparentUDP(l.ctx, "udp4", addr4, false); err == nil {
		l.udpV4 = uc
		go l.udpReadLoop(uc, false)
		log.Infof("tproxy: UDP listening on %s (v4) for set %q -> %s:%d", addr4, l.SetName, l.Upstream.Host, l.Upstream.Port)
	} else {
		log.Errorf("tproxy: UDP v4 listen %s failed for set %q: %v", addr4, l.SetName, err)
	}

	if uc, err := listenTransparentUDP(l.ctx, "udp6", addr6, true); err == nil {
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
		s.relay.Close()
		s.reply.Close()
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

	l.udpMu.Lock()
	sess, ok := l.udpSessions[key]
	l.udpMu.Unlock()

	if !ok {
		newSess, err := l.newUDPSession(src, dst, v6)
		if err != nil {
			log.Tracef("tproxy: UDP session setup failed for %s on set %q: %v", dst, l.SetName, err)
			return
		}
		l.udpMu.Lock()
		if existing, dup := l.udpSessions[key]; dup {
			l.udpMu.Unlock()
			newSess.relay.Close()
			newSess.reply.Close()
			sess = existing
		} else if l.ctx.Err() != nil {
			l.udpMu.Unlock()
			newSess.relay.Close()
			newSess.reply.Close()
			return
		} else {
			l.udpSessions[key] = newSess
			l.udpMu.Unlock()
			go l.udpReplyLoop(key, newSess)
			sess = newSess
		}
	}

	sess.last.Store(time.Now().UnixNano())
	if _, err := sess.relay.Write(payload); err != nil {
		log.Tracef("tproxy: UDP relay write failed for %s on set %q: %v", dst, l.SetName, err)
		l.closeUDPSession(key)
	}
}

func (l *Listener) newUDPSession(src, dst *net.UDPAddr, v6 bool) (*udpSession, error) {
	reply, err := openReplySocket(l.ctx, dst, v6)
	if err != nil {
		return nil, fmt.Errorf("reply socket: %w", err)
	}

	var relay udpRelay
	dialCtx, cancel := context.WithTimeout(l.ctx, 10*time.Second)
	up, derr := socks5.DialUpstreamUDP(dialCtx, l.Upstream, dst.IP, dst.Port)
	cancel()
	if derr != nil {
		if !l.FailOpen {
			reply.Close()
			return nil, derr
		}
		direct, ferr := l.dialDirectUDP(dst)
		if ferr != nil {
			reply.Close()
			return nil, fmt.Errorf("upstream %v; fail-open %w", derr, ferr)
		}
		relay = direct
	} else {
		relay = up
	}

	domain := ""
	if l.Resolver != nil {
		domain = l.Resolver.DomainFor(dst.IP)
	}
	log.LogConnectionStr("UDP", l.SetName, domain, src.String(), "",
		net.JoinHostPort(dst.IP.String(), fmt.Sprintf("%d", dst.Port)),
		"", "", "proxy")

	sess := &udpSession{relay: relay, reply: reply, client: src}
	sess.last.Store(time.Now().UnixNano())
	return sess, nil
}

func (l *Listener) udpReplyLoop(key string, sess *udpSession) {
	buf := make([]byte, udpBufSize)
	for {
		_ = sess.relay.SetReadDeadline(time.Now().Add(udpIdleTimeout))
		n, err := sess.relay.Read(buf)
		if err != nil {
			l.closeUDPSession(key)
			return
		}
		sess.last.Store(time.Now().UnixNano())
		if _, err := sess.reply.WriteToUDP(buf[:n], sess.client); err != nil {
			l.closeUDPSession(key)
			return
		}
	}
}

func (l *Listener) closeUDPSession(key string) {
	l.udpMu.Lock()
	sess, ok := l.udpSessions[key]
	if ok {
		delete(l.udpSessions, key)
	}
	l.udpMu.Unlock()
	if ok {
		sess.relay.Close()
		sess.reply.Close()
	}
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
			var stale []string
			l.udpMu.Lock()
			for k, s := range l.udpSessions {
				if s.last.Load() < cutoff {
					stale = append(stale, k)
				}
			}
			l.udpMu.Unlock()
			for _, k := range stale {
				l.closeUDPSession(k)
			}
		}
	}
}

func (l *Listener) dialDirectUDP(dst *net.UDPAddr) (*directUDP, error) {
	d := markedDialer(10*time.Second, l.Upstream.BypassMark)
	c, err := d.DialContext(l.ctx, "udp", dst.String())
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
