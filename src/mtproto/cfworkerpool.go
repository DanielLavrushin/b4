package mtproto

import (
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	// A Worker holds an open TCP session to Telegram for the whole life of the
	// WebSocket, and Telegram drops that session when nothing has crossed it.
	// Measured against a real Worker from a Russian network: 90.1s idle with no
	// handshake sent (what a pooled conn looks like), 91.7s with one. WS pings do
	// not extend it - they keep the b4-to-Cloudflare leg warm and never reach the
	// leg that actually times out. Retire a spare at half that, so a conn handed
	// to a client has room to live.
	cfWorkerPoolMaxAge = 45 * time.Second
	// One spare per key. Every pooled conn spends a request from the Worker's
	// free-tier daily quota, and a single warm conn already removes the TLS
	// handshake, the WS upgrade and the Worker's own connect out to Telegram
	// from the critical path.
	cfWorkerPoolTarget = 1
)

type cfWorkerKey struct {
	domain string
	dc     int
	path   string
}

type cfWorkerEntry struct {
	conn    *wsConn
	created time.Time
	expiry  *time.Timer
}

type cfWorkerPool struct {
	mu        sync.Mutex
	idle      map[cfWorkerKey][]cfWorkerEntry
	refilling map[cfWorkerKey]bool
	closed    bool
	mark      uint
}

func newCFWorkerPool(mark uint) *cfWorkerPool {
	return &cfWorkerPool{
		idle:      map[cfWorkerKey][]cfWorkerEntry{},
		refilling: map[cfWorkerKey]bool{},
		mark:      mark,
	}
}

// planWorkerKey keys on the Worker URL as well as the domain. The URL carries
// the destination address the Worker will open its TCP session to, and the
// bridge takes that from the address the client was dialling, so two clients on
// the same DC can be headed for different addresses - handing either of them a
// conn warmed for the other lands the session on the wrong endpoint.
func planWorkerKey(p transportPlan) cfWorkerKey {
	absDC := p.dc
	if absDC < 0 {
		absDC = -absDC
	}
	return cfWorkerKey{domain: p.sni, dc: absDC, path: p.wsPath}
}

func (p *cfWorkerPool) get(pl transportPlan) *wsConn {
	if p == nil {
		return nil
	}
	k := planWorkerKey(pl)
	now := time.Now()

	p.mu.Lock()
	bucket := p.idle[k]
	var picked *wsConn
	for len(bucket) > 0 {
		e := bucket[0]
		bucket = bucket[1:]
		if e.expiry != nil {
			e.expiry.Stop()
		}
		if e.conn.closed.Load() || now.Sub(e.created) > cfWorkerPoolMaxAge || !e.conn.alive() {
			go func(c *wsConn) { _ = c.Close() }(e.conn)
			continue
		}
		picked = e.conn
		break
	}
	if len(bucket) == 0 {
		delete(p.idle, k)
	} else {
		p.idle[k] = bucket
	}
	p.mu.Unlock()
	return picked
}

// warm tops the bucket back up to one spare. It runs only after a dial through
// this Worker has actually succeeded, so a Worker that is down or misconfigured
// never gets a background dial loop pointed at it - which is what makes a
// separate refill backoff unnecessary here.
func (p *cfWorkerPool) warm(pl transportPlan) {
	if p == nil {
		return
	}
	k := planWorkerKey(pl)
	p.mu.Lock()
	if p.closed || p.refilling[k] || len(p.idle[k]) >= cfWorkerPoolTarget {
		p.mu.Unlock()
		return
	}
	p.refilling[k] = true
	p.mu.Unlock()
	go p.refill(k, pl)
}

func (p *cfWorkerPool) refill(k cfWorkerKey, pl transportPlan) {
	defer func() {
		p.mu.Lock()
		delete(p.refilling, k)
		p.mu.Unlock()
	}()

	conn, err := dialWS(pl.dialHost, pl.sni, pl.wsPath, wsPoolDialTimeout, p.mark)
	if err != nil {
		log.Tracef("%s CF worker pool warm %s DC %d failed: %v", tg(""), k.domain, k.dc, err)
		return
	}
	wsc, ok := conn.(*wsConn)
	if !ok {
		_ = conn.Close()
		return
	}

	p.mu.Lock()
	if p.closed || len(p.idle[k]) >= cfWorkerPoolTarget {
		p.mu.Unlock()
		_ = wsc.Close()
		return
	}
	e := cfWorkerEntry{conn: wsc, created: time.Now()}
	e.expiry = time.AfterFunc(cfWorkerPoolMaxAge, func() { p.expire(k, wsc) })
	p.idle[k] = append(p.idle[k], e)
	p.mu.Unlock()
	log.Debugf("%s CF worker pool warmed %s DC %d", tg(""), k.domain, k.dc)
}

// expire retires a conn that has been idle long enough for Cloudflare to be
// about to cut it. Without this the Worker would keep an unused TCP session to
// Telegram open until the next client happened to ask for one.
func (p *cfWorkerPool) expire(k cfWorkerKey, conn *wsConn) {
	p.mu.Lock()
	bucket := p.idle[k]
	kept := bucket[:0]
	found := false
	for _, e := range bucket {
		if e.conn == conn {
			found = true
			continue
		}
		kept = append(kept, e)
	}
	if len(kept) == 0 {
		delete(p.idle, k)
	} else {
		p.idle[k] = kept
	}
	p.mu.Unlock()
	if found {
		_ = conn.Close()
		log.Tracef("%s CF worker pool retired idle conn %s DC %d", tg(""), k.domain, k.dc)
	}
}

func (p *cfWorkerPool) close() {
	if p == nil {
		return
	}
	p.mu.Lock()
	p.closed = true
	for k, bucket := range p.idle {
		for _, e := range bucket {
			if e.expiry != nil {
				e.expiry.Stop()
			}
			_ = e.conn.Close()
		}
		delete(p.idle, k)
	}
	p.mu.Unlock()
}
