package mtproto

import (
	"context"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	wsPoolMaxAge          = 20 * time.Second
	wsPoolDefaultSize     = 4
	wsDCFailCooldown      = 30 * time.Second
	wsDialTimeoutCooldown = 2 * time.Second
	tcpFailCooldown       = 30 * time.Second
)

type wsKey struct {
	dc      int
	isMedia bool
}

func (k wsKey) String() string {
	s := strconv.Itoa(k.dc)
	if k.isMedia {
		s += "m"
	}
	return s
}

var (
	wsStateMu    sync.Mutex
	wsBlacklist  = map[wsKey]bool{}
	wsCooldownTo = map[wsKey]time.Time{}

	tcpStateMu    sync.Mutex
	tcpCooldownTo = map[string]time.Time{} // keyed by host:port

	dialLogMu sync.Mutex
	dialLogAt = map[int]time.Time{} // per-DC last full ERROR emit; throttles spam from known-broken DCs
)

const dialLogInterval = 60 * time.Second

// shouldLogDialError returns true if this is the first error for `dc` in the
// last dialLogInterval. Subsequent identical failures are silenced (caller can
// log at Debug instead) so a permanently-broken DC doesn't spam errors.log.
func shouldLogDialError(dc int) bool {
	dialLogMu.Lock()
	defer dialLogMu.Unlock()
	now := time.Now()
	if last, ok := dialLogAt[dc]; ok && now.Sub(last) < dialLogInterval {
		return false
	}
	dialLogAt[dc] = now
	return true
}

// per-addr TCP cooldown: skip an upstream IP/port that just timed out so
// every retrying client doesn't burn another tcpDialTimeout against it.
func tcpAddrInCooldown(addr string) bool {
	tcpStateMu.Lock()
	defer tcpStateMu.Unlock()
	t, ok := tcpCooldownTo[addr]
	if !ok {
		return false
	}
	if time.Now().After(t) {
		delete(tcpCooldownTo, addr)
		return false
	}
	return true
}

func tcpRecordFailure(addr string) {
	tcpStateMu.Lock()
	defer tcpStateMu.Unlock()
	tcpCooldownTo[addr] = time.Now().Add(tcpFailCooldown)
}

func tcpRecordSuccess(addr string) {
	tcpStateMu.Lock()
	defer tcpStateMu.Unlock()
	delete(tcpCooldownTo, addr)
}

func tcpResetState() {
	tcpStateMu.Lock()
	defer tcpStateMu.Unlock()
	tcpCooldownTo = map[string]time.Time{}
}

func wsKeyFromDC(dc int) wsKey {
	abs := dc
	if abs < 0 {
		abs = -abs
	}
	return wsKey{dc: abs, isMedia: dc < 0}
}

func wsIsBlacklisted(dc int) bool {
	k := wsKeyFromDC(dc)
	wsStateMu.Lock()
	defer wsStateMu.Unlock()
	return wsBlacklist[k]
}

func wsCooldownActive(dc int) bool {
	k := wsKeyFromDC(dc)
	wsStateMu.Lock()
	defer wsStateMu.Unlock()
	t, ok := wsCooldownTo[k]
	if !ok {
		return false
	}
	if time.Now().After(t) {
		delete(wsCooldownTo, k)
		return false
	}
	return true
}

func wsRecordFailure(dc int, allRedirect bool) {
	k := wsKeyFromDC(dc)
	wsStateMu.Lock()
	defer wsStateMu.Unlock()
	if allRedirect {
		wsBlacklist[k] = true
		log.Warnf("MTProto WS %s blacklisted (all redirects)", k)
	}
	wsCooldownTo[k] = time.Now().Add(wsDCFailCooldown)
}

func wsRecordSuccess(dc int) {
	k := wsKeyFromDC(dc)
	wsStateMu.Lock()
	defer wsStateMu.Unlock()
	delete(wsCooldownTo, k)
	delete(wsBlacklist, k)
}

func wsResetState() {
	wsStateMu.Lock()
	defer wsStateMu.Unlock()
	wsBlacklist = map[wsKey]bool{}
	wsCooldownTo = map[wsKey]time.Time{}
}

type wsPoolEntry struct {
	conn    *wsConn
	created time.Time
}

type wsPool struct {
	mu        sync.Mutex
	idle      map[wsKey][]wsPoolEntry
	refilling map[wsKey]bool
	target    int
	maxAge    time.Duration

	cfg    *MTProtoUpstream
	mark   uint
	ctx    context.Context
	cancel context.CancelFunc
}

// MTProtoUpstream is the minimal upstream config the pool needs (subset of config.MTProtoConfig).
// Passed by value to detach pool from live config mutation.
type MTProtoUpstream struct {
	WSEndpointHost string
	WSCustomDomain string
}

func newWSPool(cfg MTProtoUpstream, mark uint, target int) *wsPool {
	if target <= 0 {
		target = wsPoolDefaultSize
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &wsPool{
		idle:      map[wsKey][]wsPoolEntry{},
		refilling: map[wsKey]bool{},
		target:    target,
		maxAge:    wsPoolMaxAge,
		cfg:       &cfg,
		mark:      mark,
		ctx:       ctx,
		cancel:    cancel,
	}
}

func (p *wsPool) close() {
	p.cancel()
	p.mu.Lock()
	defer p.mu.Unlock()
	for k, b := range p.idle {
		for _, e := range b {
			_ = e.conn.Close()
		}
		delete(p.idle, k)
	}
}

// get returns a pre-warmed *wsConn for the given signed DC (negative = media),
// or nil if the pool is empty. On hit and miss it schedules an async refill so
// the next caller can also hit. The returned conn has had no obfuscated init
// sent yet - caller must run completeObfuscation on it.
func (p *wsPool) get(dc int) *wsConn {
	if p == nil {
		return nil
	}
	k := wsKeyFromDC(dc)
	if !wsEdgeServesDC(k.dc) || wsIsBlacklisted(dc) {
		return nil
	}

	p.mu.Lock()
	bucket := p.idle[k]
	now := time.Now()
	var picked *wsConn
	for len(bucket) > 0 {
		e := bucket[0]
		bucket = bucket[1:]
		// stale check: TG may FIN/RST an idle conn server-side; handing such a conn
		// to a client produces an up=N down=0 session (RPC sent, never answered).
		// This is the path that breaks auth.importAuthorization on secondary DCs
		// and makes foreign-channel media downloads hang.
		if e.conn.closed.Load() || now.Sub(e.created) > p.maxAge || !e.conn.alive() {
			go func(c *wsConn) { _ = c.Close() }(e.conn)
			continue
		}
		picked = e.conn
		break
	}
	p.idle[k] = bucket
	p.mu.Unlock()

	p.scheduleRefill(dc)
	return picked
}

func (p *wsPool) scheduleRefill(dc int) {
	if p == nil {
		return
	}
	k := wsKeyFromDC(dc)
	p.mu.Lock()
	if p.refilling[k] {
		p.mu.Unlock()
		return
	}
	p.refilling[k] = true
	p.mu.Unlock()

	go p.refill(dc)
}

func (p *wsPool) refill(dc int) {
	k := wsKeyFromDC(dc)
	defer func() {
		p.mu.Lock()
		p.refilling[k] = false
		p.mu.Unlock()
	}()

	if p.ctx.Err() != nil {
		return
	}
	if wsIsBlacklisted(dc) {
		return
	}

	p.mu.Lock()
	need := p.target - len(p.idle[k])
	p.mu.Unlock()
	if need <= 0 {
		return
	}

	// parallel dials so the pool fills in ~one RTT instead of need*RTT;
	// individual failures don't abort siblings, matching tg-ws-proxy
	type result struct {
		conn *wsConn
		err  error
	}
	results := make(chan result, need)
	for i := 0; i < need; i++ {
		go func() {
			if p.ctx.Err() != nil {
				results <- result{}
				return
			}
			c, err := p.dialFresh(dc)
			results <- result{conn: c, err: err}
		}()
	}
	added := 0
	for i := 0; i < need; i++ {
		r := <-results
		if r.err != nil || r.conn == nil {
			if r.err != nil {
				log.Tracef("MTProto WS pool refill %s slot failed: %v", k, r.err)
			}
			continue
		}
		if p.ctx.Err() != nil {
			_ = r.conn.Close()
			continue
		}
		p.mu.Lock()
		p.idle[k] = append(p.idle[k], wsPoolEntry{conn: r.conn, created: time.Now()})
		p.mu.Unlock()
		added++
	}
	if added > 0 {
		log.Debugf("MTProto WS pool %s refilled +%d (target=%d)", k, added, p.target)
	}
}

// dialFresh opens a raw WS connection (TLS + Upgrade) to a TG edge for `dc`.
// Tries both kwsN[-1] domains in the order matching media-vs-primary preference.
// Returns the first one to succeed, or the last error.
func (p *wsPool) dialFresh(dc int) (*wsConn, error) {
	plans := wsPlansForDC(dc, p.cfg)
	var lastErr error
	for _, pl := range plans {
		host := pl.dialHost
		if host == "" {
			host = pl.sni
		}
		conn, err := dialWS(host, pl.sni, pl.wsPath, wsDialTimeout, p.mark)
		if err != nil {
			lastErr = err
			continue
		}
		if wsc, ok := conn.(*wsConn); ok {
			return wsc, nil
		}
		_ = conn.Close()
	}
	if lastErr == nil {
		lastErr = net.ErrClosed
	}
	return nil, lastErr
}

func wsPlansForDC(dc int, cfg *MTProtoUpstream) []transportPlan {
	absDC := dc
	if absDC < 0 {
		absDC = -absDC
	}
	var plans []transportPlan
	edgeIP := ""
	if cfg != nil {
		edgeIP = cfg.WSEndpointHost
	}
	if edgeIP == "" {
		edgeIP = telegramWSEdgeIP
	}
	if wsEdgeServesDC(absDC) {
		primary := transportPlan{kind: transportWS, dc: dc, sni: kwsHost(absDC, ""), dialHost: edgeIP}
		media := transportPlan{kind: transportWS, dc: dc, sni: kwsHost(absDC, "-1"), dialHost: edgeIP}
		if dc < 0 {
			plans = append(plans, media, primary)
		} else {
			plans = append(plans, primary, media)
		}
	}
	if cfg != nil && cfg.WSCustomDomain != "" {
		plans = append(plans, transportPlan{
			kind: transportWS,
			dc:   dc,
			sni:  kwsCustom(absDC, cfg.WSCustomDomain),
		})
	}
	return plans
}

func kwsHost(dc int, suffix string) string {
	return "kws" + strconv.Itoa(dc) + suffix + ".web.telegram.org"
}

func kwsCustom(dc int, domain string) string {
	return "kws" + strconv.Itoa(dc) + "." + domain
}

func (p *wsPool) warmup(dcs []int) {
	if p == nil {
		return
	}
	for _, dc := range dcs {
		p.scheduleRefill(dc)
		p.scheduleRefill(-dc)
	}
}
