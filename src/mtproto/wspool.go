package mtproto

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	wsPoolMaxAge          = 20 * time.Second
	wsPoolDefaultSize     = 4
	wsDCFailCooldown      = 30 * time.Second
	wsBlacklistTTL        = 5 * time.Minute
	wsDialTimeoutCooldown = 2 * time.Second
	tcpFailCooldown       = 30 * time.Second

	// The Cloudflare-proxied domains are a pool shared by every b4 install, so a
	// data center reached only through them is held at fewer spares than one on
	// Telegram's own edge. One warm conn already takes the dial off the client's
	// path, which is the whole point.
	wsPoolCFTarget = 2
	// A refill walks the transport list in the background, where nothing is
	// waiting on it, but a data center with no edge has ten Cloudflare domains
	// behind it and walking all of them at the dial timeout is minutes of work
	// against shared infrastructure for one spare.
	wsPoolMaxPlanAttempts = 3

	// wsEndpointFailTTL is how long an address that timed out is stepped over.
	// A timeout is the only failure worth remembering: it costs the client most
	// of the patience it has before Telegram calls the proxy broken, while an
	// HTTP status comes back in one round trip and costs nothing.
	wsEndpointFailTTL = 5 * time.Minute
	// wsDialMaxEndpoints bounds how many of a name's addresses one dial walks,
	// so a name behind a large address set cannot spend the whole budget.
	wsDialMaxEndpoints = 2
	// wsDialMinAttempt is the least time worth giving an address. Below this a
	// TLS handshake on a healthy path would be cut off mid-flight and the
	// address blamed for it.
	wsDialMinAttempt = 700 * time.Millisecond
	// wsResolveMaxWait caps how much of a dial's budget a name lookup may take.
	wsResolveMaxWait = time.Second

	// wsPoolDialTimeout is what a spare is given to connect. It is deliberately
	// far longer than the client-facing dial: nothing is waiting on a refill, and
	// a slow route that does eventually answer is worth holding, because the
	// client that gets handed it pays none of that wait. Cutting refills at the
	// client-facing timeout would starve the pool on exactly the slow networks
	// that need it most.
	wsPoolDialTimeout = 8 * time.Second
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
	wsBlacklist  = map[wsKey]time.Time{}
	wsCooldownTo = map[wsKey]time.Time{}
	wsEndpointTo = map[string]time.Time{} // "ip|sni" -> until

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

// Every writer of this state logs after releasing the lock, never under it. With
// immediate flushing on - the default - a log call is a blocking write to
// whatever the log file sits on, which on a router is a USB stick, and holding a
// lock every dial path takes across it puts that write in front of them all.
func wsIsBlacklisted(dc int) bool {
	k := wsKeyFromDC(dc)
	wsStateMu.Lock()
	t, ok := wsBlacklist[k]
	expired := ok && time.Now().After(t)
	if expired {
		delete(wsBlacklist, k)
	}
	wsStateMu.Unlock()
	if expired {
		log.Debugf("%s WS %s blacklist expired, WS re-enabled", tg(""), k)
		return false
	}
	return ok
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
	if allRedirect {
		wsBlacklist[k] = time.Now().Add(wsBlacklistTTL)
	}
	wsCooldownTo[k] = time.Now().Add(wsDCFailCooldown)
	wsStateMu.Unlock()
	if allRedirect {
		log.Warnf("%s WS %s blacklisted (all redirects), retry in %v", tg(""), k, wsBlacklistTTL)
	}
	log.Debugf("%s WS %s dial failure, cooldown %v (allRedirect=%v)", tg(""), k, wsDCFailCooldown, allRedirect)
}

func wsRecordSuccess(dc int) {
	k := wsKeyFromDC(dc)
	wsStateMu.Lock()
	_, wasCooled := wsCooldownTo[k]
	_, wasBlacklisted := wsBlacklist[k]
	delete(wsCooldownTo, k)
	delete(wsBlacklist, k)
	wsStateMu.Unlock()
	if wasCooled || wasBlacklisted {
		log.Debugf("%s WS %s recovered (cleared cooldown/blacklist)", tg(""), k)
	}
}

func wsEndpointKey(ip, sni string) string { return ip + "|" + sni }

// wsEndpointFailed records that a name's address swallowed the handshake. The
// key carries the server name as well as the address because that is what a
// censor filters on: one Cloudflare address fronts many names, and blaming the
// address for all of them would retire routes that still work.
func wsEndpointFailed(ip, sni string) {
	k := wsEndpointKey(ip, sni)
	wsStateMu.Lock()
	t, ok := wsEndpointTo[k]
	recorded := !ok || !time.Now().Before(t)
	if recorded {
		wsEndpointTo[k] = time.Now().Add(wsEndpointFailTTL)
	}
	wsStateMu.Unlock()
	if recorded {
		log.Debugf("%s WS endpoint %s timed out, deprioritised for %v", tg(""), k, wsEndpointFailTTL)
	}
}

func wsEndpointCooling(ip, sni string) bool {
	k := wsEndpointKey(ip, sni)
	wsStateMu.Lock()
	defer wsStateMu.Unlock()
	t, ok := wsEndpointTo[k]
	if !ok {
		return false
	}
	if time.Now().After(t) {
		delete(wsEndpointTo, k)
		return false
	}
	return true
}

func wsEndpointRecovered(ip, sni string) {
	k := wsEndpointKey(ip, sni)
	wsStateMu.Lock()
	_, cleared := wsEndpointTo[k]
	delete(wsEndpointTo, k)
	wsStateMu.Unlock()
	if cleared {
		log.Debugf("%s WS endpoint %s answered again, cooldown cleared", tg(""), k)
	}
}

func wsResetState() {
	wsStateMu.Lock()
	wsBlacklist = map[wsKey]time.Time{}
	wsCooldownTo = map[wsKey]time.Time{}
	wsEndpointTo = map[string]time.Time{}
	wsStateMu.Unlock()
	log.Debugf("%s WS cooldown/blacklist state reset", tg(""))
}

type wsPoolEntry struct {
	conn    *wsConn
	created time.Time
	plan    transportPlan
}

// wsPoolConn is a spare handed to a caller together with the route it was dialled
// on. Without the route a pooled session is logged as "ws-pool" and nothing says
// which name carried it, so an upstream that rejects the session cannot be told
// apart from one that carried it.
type wsPoolConn struct {
	conn *wsConn
	plan transportPlan
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
	CFProxyEnabled bool
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
// or nil if the pool is empty, together with the route it was dialled on. On hit
// and miss it schedules an async refill so the next caller can also hit. The
// returned conn has had no obfuscated init sent yet - caller must run
// completeObfuscation on it.
func (p *wsPool) get(dc int) *wsPoolConn {
	if p == nil {
		return nil
	}
	k := wsKeyFromDC(dc)
	if wsIsBlacklisted(dc) {
		return nil
	}

	p.mu.Lock()
	bucket := p.idle[k]
	now := time.Now()
	var picked *wsPoolConn
	var age time.Duration
	dropped := 0
	for len(bucket) > 0 {
		e := bucket[0]
		bucket = bucket[1:]
		// stale check: TG may FIN/RST an idle conn server-side; handing such a conn
		// to a client produces an up=N down=0 session (RPC sent, never answered).
		// This is the path that breaks auth.importAuthorization on secondary DCs
		// and makes foreign-channel media downloads hang.
		if e.conn.closed.Load() || now.Sub(e.created) > p.maxAge || !e.conn.alive() {
			go func(c *wsConn) { _ = c.Close() }(e.conn)
			dropped++
			continue
		}
		picked = &wsPoolConn{conn: e.conn, plan: e.plan}
		age = now.Sub(e.created)
		break
	}
	remaining := len(bucket)
	p.idle[k] = bucket
	p.mu.Unlock()

	if picked != nil {
		log.Tracef("%s WS pool %s hit on %s, age %dms, %d spare(s) left, %d stale discarded",
			tg(""), k, picked.plan.describe(), age.Milliseconds(), remaining, dropped)
	} else {
		log.Tracef("%s WS pool %s miss, %d stale discarded", tg(""), k, dropped)
	}

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
	// A data center with no route configured would otherwise start a refill
	// goroutine on every pool miss and dial nothing.
	if len(wsPlansForDC(dc, p.cfg)) == 0 {
		return
	}

	p.mu.Lock()
	need := p.targetFor(k) - len(p.idle[k])
	p.mu.Unlock()
	if need <= 0 {
		return
	}

	// parallel dials so the pool fills in ~one RTT instead of need*RTT;
	// individual failures don't abort siblings, matching tg-ws-proxy
	type result struct {
		conn *wsPoolConn
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
	routes := map[string]int{}
	for i := 0; i < need; i++ {
		r := <-results
		if r.err != nil || r.conn == nil {
			if r.err != nil {
				log.Tracef("%s WS pool refill %s slot failed: %v", tg(""), k, r.err)
			}
			continue
		}
		if p.ctx.Err() != nil {
			_ = r.conn.conn.Close()
			continue
		}
		p.mu.Lock()
		p.idle[k] = append(p.idle[k], wsPoolEntry{conn: r.conn.conn, created: time.Now(), plan: r.conn.plan})
		p.mu.Unlock()
		routes[r.conn.plan.describe()]++
		added++
	}
	if added > 0 {
		names := make([]string, 0, len(routes))
		for r, n := range routes {
			names = append(names, fmt.Sprintf("%s x%d", r, n))
		}
		sort.Strings(names)
		log.Debugf("%s WS pool %s refilled +%d (target=%d) on %s", tg(""), k, added, p.targetFor(k), strings.Join(names, ", "))
	}
}

// dialFresh opens a raw WS connection (TLS + Upgrade) to a TG edge for `dc`.
// Returns the first plan to succeed together with that plan, or the last error.
func (p *wsPool) dialFresh(dc int) (*wsPoolConn, error) {
	plans := wsPlansForDC(dc, p.cfg)
	if len(plans) > wsPoolMaxPlanAttempts {
		plans = plans[:wsPoolMaxPlanAttempts]
	}
	var lastErr error
	for _, pl := range plans {
		host := pl.dialHost
		if host == "" {
			host = pl.sni
		}
		conn, err := dialWS(host, pl.sni, pl.wsPath, wsPoolDialTimeout, p.mark)
		if err != nil {
			lastErr = err
			continue
		}
		if wsc, ok := conn.(*wsConn); ok {
			// The refill runs with nothing waiting on it, which makes it the
			// cheapest place to notice that a cooled-down edge is answering
			// again: without this the cooldown only ever lapses on a timer,
			// and a client pays the shortened-timeout path in the meantime.
			if pl.native {
				wsRecordSuccess(dc)
			}
			return &wsPoolConn{conn: wsc, plan: pl}, nil
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
	override := ""
	cfProxy := false
	if cfg != nil {
		override = cfg.WSEndpointHost
		cfProxy = cfg.CFProxyEnabled
	}
	if wsEdgeServesDC(absDC) {
		dh := wsNativeDialHost(override)
		plans = append(plans, nativeEdgePlans(dc, absDC, dh)...)
	}
	if cfg != nil && cfg.WSCustomDomain != "" {
		plans = append(plans, transportPlan{
			kind:   transportWS,
			dc:     dc,
			sni:    kwsCustom(absDC, cfg.WSCustomDomain),
			cfBase: cfg.WSCustomDomain,
		})
	}
	// kws2 and kws4 are the only names Telegram's own WebSocket edge answers, so
	// every other data center had nothing here and could never be pooled. DC 203
	// is where Russian accounts live: it went through the Cloudflare-proxied
	// domains on a cold dial, at the full dial timeout, on every session, and a
	// single blocked domain there is longer than Telegram waits before calling
	// the proxy misconfigured.
	if !wsEdgeServesDC(absDC) && cfProxy {
		for _, base := range cfBalancerInst.domainsForDC(dc) {
			plans = append(plans, transportPlan{
				kind:   transportWS,
				dc:     dc,
				sni:    kwsCustom(absDC, base),
				cfBase: base,
			})
		}
	}
	return plans
}

// targetFor is how many spares a key is held at. Keys served by Telegram's own
// edge get the configured size; keys that exist only behind the shared
// Cloudflare domains get fewer, because those are the same handful of names
// every b4 install dials.
func (p *wsPool) targetFor(k wsKey) int {
	if wsEdgeServesDC(k.dc) || p.target < wsPoolCFTarget {
		return p.target
	}
	return wsPoolCFTarget
}

func kwsHost(dc int, suffix string) string {
	return "kws" + strconv.Itoa(dc) + suffix + ".web.telegram.org"
}

// nativeEdgePlans builds Telegram's own WebSocket edge plans for a signed DC.
//
// kwsN-1 is the media cluster and kwsN the primary one, and the two clusters do
// not accept each other's sessions symmetrically. Measured against 149.154.167.220
// with a complete req_pq_multi and req_DH_params exchange: the primary cluster
// answers server_DH_params_ok to a media session, while the media cluster answers
// a primary session with the four-byte transport error -444, on both DC 2 and DC 4,
// every time. The dc the cluster checks is the one inside the client's
// RSA-encrypted p_q_inner_data, which the proxy cannot read or correct, so a
// primary session that lands on kwsN-1 is rejected and the client is told the
// proxy is misconfigured and switches it off.
//
// Both names resolve to the same address, so kwsN-1 was never a route around a
// blocked kwsN - only a way to fail the session. A media session still falls back
// to the primary name, because that direction is accepted.
func nativeEdgePlans(dc, absDC int, dialHost string) []transportPlan {
	primary := transportPlan{kind: transportWS, dc: dc, sni: kwsHost(absDC, ""), dialHost: dialHost, native: true}
	if dc >= 0 {
		return []transportPlan{primary}
	}
	media := transportPlan{kind: transportWS, dc: dc, sni: kwsHost(absDC, "-1"), dialHost: dialHost, native: true}
	return []transportPlan{media, primary}
}

func kwsCustom(dc int, domain string) string {
	return "kws" + strconv.Itoa(dc) + "." + domain
}

// wsWarmupDCs are the data centers worth holding connections open for before a
// client has asked for one. 2 and 4 are the pair Telegram's own edge serves;
// 203 is added whenever a route to it exists, because that is where Russian
// accounts live and it has no edge of its own, so without a spare every session
// there begins with a cold dial. Any other data center is warmed on demand: the
// first session misses the pool and schedules a refill, and the ones after it
// hit.
func wsWarmupDCs(cfg *config.MTProtoConfig) []int {
	dcs := []int{2, 4}
	if cfg.CFProxyEnabled || strings.TrimSpace(cfg.WSCustomDomain) != "" {
		dcs = append(dcs, 203)
	}
	return dcs
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
