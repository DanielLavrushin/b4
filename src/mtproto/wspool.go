package mtproto

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	wsPoolMaxAge          = 75 * time.Second
	wsPoolCheckInterval   = 5 * time.Second
	wsPoolKeepWarm        = 10 * time.Minute
	wsPoolKeepWarmShared  = 3 * time.Minute
	wsAddressFailTTL      = time.Minute
	wsNativeProbeEvery    = 30 * time.Second
	wsRefillBackoffBase   = time.Second
	wsRefillBackoffMax    = 5 * time.Minute
	wsEndpointFailMaxTTL  = 30 * time.Minute
	wsPoolDefaultSize     = 4
	wsDCFailCooldown      = 30 * time.Second
	wsBlacklistTTL        = 5 * time.Minute
	wsDialTimeoutCooldown = 2 * time.Second
	tcpFailCooldown       = 30 * time.Second

	// The Cloudflare-proxied domains are a pool shared by every b4 install, so a
	// data center reached only through them is held at fewer spares than one on
	// Telegram's own edge. One warm conn already takes the dial off the client's
	// path, which is the whole point.
	wsPoolCFTarget = 1
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
	wsEndpointN  = map[string]int{}
	wsEndpointAt = map[string]time.Time{}
	wsProbeAt    = map[string]time.Time{}
	wsFrontOn    = map[string]string{}
	wsFrontNo    = map[string]time.Time{}

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

func (k wsKey) signed() int {
	if k.isMedia {
		return -k.dc
	}
	return k.dc
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
	wsMarkEndpoint(wsEndpointKey(ip, sni), wsEndpointFailTTL)
}

func wsAddressKey(ip string) string { return ip + "|*" }

func wsAddressFailed(ip string) {
	wsMarkEndpoint(wsAddressKey(ip), wsAddressFailTTL)
}

func wsEndpointTTL(base time.Duration, fails int) time.Duration {
	ttl := base
	for i := 0; i < fails && ttl < wsEndpointFailMaxTTL; i++ {
		ttl *= 2
	}
	if ttl > wsEndpointFailMaxTTL {
		ttl = wsEndpointFailMaxTTL
	}
	return ttl
}

func wsMarkEndpoint(k string, base time.Duration) {
	now := time.Now()
	wsStateMu.Lock()
	t, ok := wsEndpointTo[k]
	recorded := !ok || !now.Before(t)
	var ttl time.Duration
	if recorded {
		n := wsEndpointN[k]
		if last, seen := wsEndpointAt[k]; seen && now.Sub(last) > 2*wsEndpointFailMaxTTL {
			n = 0
		}
		ttl = wsEndpointTTL(base, n)
		wsEndpointN[k] = n + 1
		wsEndpointAt[k] = now
		wsEndpointTo[k] = now.Add(ttl)
	}
	wsStateMu.Unlock()
	if recorded {
		log.Debugf("%s WS endpoint %s timed out, deprioritised for %v", tg(""), k, ttl)
	}
}

func wsProbeAllowed(host string) bool {
	now := time.Now()
	wsStateMu.Lock()
	defer wsStateMu.Unlock()
	if last, ok := wsProbeAt[host]; ok && now.Sub(last) < wsNativeProbeEvery {
		return false
	}
	wsProbeAt[host] = now
	return true
}

func wsKeyCoolingLocked(k string, now time.Time) bool {
	t, ok := wsEndpointTo[k]
	if !ok {
		return false
	}
	if now.After(t) {
		delete(wsEndpointTo, k)
		return false
	}
	return true
}

func wsEndpointCooling(ip, sni string) bool {
	now := time.Now()
	wsStateMu.Lock()
	defer wsStateMu.Unlock()
	byName := wsKeyCoolingLocked(wsEndpointKey(ip, sni), now)
	byAddr := wsKeyCoolingLocked(wsAddressKey(ip), now)
	return byName || byAddr
}

func wsEndpointRecovered(ip, sni string) {
	cleared := false
	wsStateMu.Lock()
	for _, k := range []string{wsEndpointKey(ip, sni), wsAddressKey(ip)} {
		if _, ok := wsEndpointTo[k]; ok {
			cleared = true
		}
		delete(wsEndpointTo, k)
		delete(wsEndpointN, k)
		delete(wsEndpointAt, k)
	}
	wsStateMu.Unlock()
	if cleared {
		log.Debugf("%s WS endpoint %s answered again, cooldown cleared", tg(""), wsEndpointKey(ip, sni))
	}
}

const wsFrontRefuseTTL = 30 * time.Minute

func wsFrontName(v string) string {
	v = strings.TrimSpace(v)
	if strings.EqualFold(v, "off") {
		return ""
	}
	return v
}

func wsFrontPreferred(host, front string) bool {
	if front == "" {
		return false
	}
	wsStateMu.Lock()
	defer wsStateMu.Unlock()
	return wsFrontOn[host] == front
}

func wsFrontRecord(host, front string, on bool) {
	wsStateMu.Lock()
	was := wsFrontOn[host] == front
	if on {
		wsFrontOn[host] = front
		delete(wsFrontNo, host+"|"+front)
	} else {
		delete(wsFrontOn, host)
	}
	wsStateMu.Unlock()
	switch {
	case on && !was:
		log.Infof("%s Telegram's edge %s answers under the name %s while its own names do not; its sessions use that name from now on", tg(""), host, front)
	case !on && was:
		log.Infof("%s Telegram's edge %s answers under its own names again", tg(""), host)
	}
}

func wsFrontRefused(host, front string) bool {
	k := host + "|" + front
	wsStateMu.Lock()
	defer wsStateMu.Unlock()
	t, ok := wsFrontNo[k]
	if !ok {
		return false
	}
	if time.Now().After(t) {
		delete(wsFrontNo, k)
		return false
	}
	return true
}

func wsFrontRefuse(host, front string) {
	k := host + "|" + front
	wsStateMu.Lock()
	_, had := wsFrontNo[k]
	wsFrontNo[k] = time.Now().Add(wsFrontRefuseTTL)
	wsStateMu.Unlock()
	if !had {
		log.Infof("%s the name %s does not lead to Telegram's edge %s on this network; not tried again for %v", tg(""), front, host, wsFrontRefuseTTL)
	}
}

func nativeRoutes(dc, absDC int, dialHost, front string) []transportPlan {
	plans := nativeEdgePlans(dc, absDC, dialHost)
	if !wsFrontPreferred(dialHost, front) || wsFrontRefused(dialHost, front) {
		return plans
	}
	for i := range plans {
		plans[i].frontSNI = front
	}
	return plans
}

func wsNativeDown(dc int, dialHost, front string) bool {
	absDC := dc
	if absDC < 0 {
		absDC = -absDC
	}
	if !wsEdgeServesDC(absDC) {
		return false
	}
	if wsCooldownActive(dc) {
		return true
	}
	for _, p := range nativeRoutes(dc, absDC, dialHost, front) {
		if !wsEndpointCooling(p.dialHost, p.tlsName()) {
			return false
		}
	}
	return true
}

func wsResetState() {
	wsStateMu.Lock()
	wsBlacklist = map[wsKey]time.Time{}
	wsCooldownTo = map[wsKey]time.Time{}
	wsEndpointTo = map[string]time.Time{}
	wsEndpointN = map[string]int{}
	wsEndpointAt = map[string]time.Time{}
	wsProbeAt = map[string]time.Time{}
	wsFrontOn = map[string]string{}
	wsFrontNo = map[string]time.Time{}
	wsStateMu.Unlock()
	log.Debugf("%s WS cooldown/blacklist state reset", tg(""))
}

var sharedWSPool atomic.Pointer[wsPool]

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
	mu          sync.Mutex
	idle        map[wsKey][]wsPoolEntry
	refilling   map[wsKey]bool
	lastUsed    map[wsKey]time.Time
	refillFails map[wsKey]int
	refillAfter map[wsKey]time.Time
	target      int
	maxAge      time.Duration

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
	FrontSNI       string
}

func newWSPool(cfg MTProtoUpstream, mark uint, target int) *wsPool {
	if target <= 0 {
		target = wsPoolDefaultSize
	}
	ctx, cancel := context.WithCancel(context.Background())
	p := &wsPool{
		idle:        map[wsKey][]wsPoolEntry{},
		refilling:   map[wsKey]bool{},
		lastUsed:    map[wsKey]time.Time{},
		refillFails: map[wsKey]int{},
		refillAfter: map[wsKey]time.Time{},
		target:      target,
		maxAge:      wsPoolMaxAge,
		cfg:         &cfg,
		mark:        mark,
		ctx:         ctx,
		cancel:      cancel,
	}
	go p.maintain()
	return p
}

func (p *wsPool) maintain() {
	t := time.NewTicker(wsPoolCheckInterval)
	defer t.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case now := <-t.C:
			p.sweep(now)
		}
	}
}

func (p *wsPool) sweep(now time.Time) {
	var stale []*wsConn
	var warm []int
	p.mu.Lock()
	for k, bucket := range p.idle {
		kept := make([]wsPoolEntry, 0, len(bucket))
		for _, e := range bucket {
			if e.conn.closed.Load() || now.Sub(e.created) > p.maxAge {
				stale = append(stale, e.conn)
				continue
			}
			kept = append(kept, e)
		}
		if len(kept) == 0 {
			delete(p.idle, k)
		} else {
			p.idle[k] = kept
		}
	}
	for k, used := range p.lastUsed {
		if now.Sub(used) > p.keepWarmFor(k) {
			delete(p.lastUsed, k)
			continue
		}
		if len(p.idle[k]) < p.targetFor(k) {
			warm = append(warm, k.signed())
		}
	}
	p.mu.Unlock()
	for _, c := range stale {
		go func(c *wsConn) { _ = c.Close() }(c)
	}
	for _, dc := range warm {
		p.scheduleRefill(dc)
	}
}

func (p *wsPool) keepWarmFor(k wsKey) time.Duration {
	if p.nativeServes(k) {
		return wsPoolKeepWarm
	}
	return wsPoolKeepWarmShared
}

func (p *wsPool) nativeServes(k wsKey) bool {
	return wsEdgeServesDC(k.dc) && !wsNativeDown(k.signed(), wsNativeDialHost(p.cfg.WSEndpointHost), p.cfg.FrontSNI)
}

func (p *wsPool) offer(dc int, c *wsConn, plan transportPlan) bool {
	if p == nil || c == nil || plan.isWorker {
		return false
	}
	k := wsKeyFromDC(dc)
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.ctx.Err() != nil || len(p.idle[k]) >= p.targetFor(k) {
		return false
	}
	if !plan.native && p.nativeServes(k) {
		return false
	}
	p.idle[k] = append(p.idle[k], wsPoolEntry{conn: c, created: time.Now(), plan: plan})
	delete(p.refillFails, k)
	delete(p.refillAfter, k)
	return true
}

func (p *wsPool) noteSuccess(dc int) {
	if p == nil {
		return
	}
	k := wsKeyFromDC(dc)
	p.mu.Lock()
	delete(p.refillFails, k)
	delete(p.refillAfter, k)
	p.mu.Unlock()
}

func (p *wsPool) popIdle(k wsKey) (wsPoolEntry, bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	bucket := p.idle[k]
	if len(bucket) == 0 {
		return wsPoolEntry{}, false
	}
	e := bucket[0]
	if len(bucket) == 1 {
		delete(p.idle, k)
	} else {
		p.idle[k] = bucket[1:]
	}
	return e, true
}

func (p *wsPool) idleCount(k wsKey) int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.idle[k])
}

func wsRefillBackoff(fails int) time.Duration {
	d := wsRefillBackoffBase
	for i := 1; i < fails && d < wsRefillBackoffMax; i++ {
		d *= 2
	}
	if d > wsRefillBackoffMax {
		d = wsRefillBackoffMax
	}
	return d
}

func (p *wsPool) close() {
	if p == nil {
		return
	}
	p.cancel()
	p.mu.Lock()
	var conns []*wsConn
	for k, b := range p.idle {
		for _, e := range b {
			conns = append(conns, e.conn)
		}
		delete(p.idle, k)
	}
	p.mu.Unlock()
	for _, c := range conns {
		go func(c *wsConn) { _ = c.Close() }(c)
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

	now := time.Now()
	p.mu.Lock()
	p.lastUsed[k] = now
	p.mu.Unlock()

	var picked *wsPoolConn
	var age time.Duration
	dropped := 0
	for {
		e, ok := p.popIdle(k)
		if !ok {
			break
		}
		if e.conn.closed.Load() || now.Sub(e.created) > p.maxAge || !e.conn.alive() {
			go func(c *wsConn) { _ = c.Close() }(e.conn)
			dropped++
			continue
		}
		picked = &wsPoolConn{conn: e.conn, plan: e.plan}
		age = now.Sub(e.created)
		break
	}
	remaining := p.idleCount(k)

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
	if p.refilling[k] || time.Now().Before(p.refillAfter[k]) {
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
	plans := wsPlansForDC(dc, p.cfg)
	if len(plans) == 0 {
		return
	}

	dh := wsNativeDialHost(p.cfg.WSEndpointHost)
	nativeDown := wsNativeDown(dc, dh, p.cfg.FrontSNI)
	p.mu.Lock()
	need := p.targetFor(k) - len(p.idle[k])
	p.mu.Unlock()
	if need <= 0 {
		return
	}

	slotPlans := plans
	var probe []transportPlan
	if nativeDown {
		slotPlans = withoutNative(plans)
		if len(slotPlans) == 0 {
			slotPlans = plans
		} else if wsProbeAllowed(dh) {
			probe = onlyNative(plans)
		}
	}

	type result struct {
		conn *wsPoolConn
		err  error
	}
	slots := need
	if len(probe) > 0 {
		slots++
	}
	results := make(chan result, slots)
	for i := 0; i < slots; i++ {
		ps, timeout := slotPlans, wsPoolDialTimeout
		if i == need {
			ps, timeout = probe, wsDialTimeout
		}
		go func() {
			if p.ctx.Err() != nil {
				results <- result{}
				return
			}
			c, err := p.dialFresh(dc, ps, timeout)
			results <- result{conn: c, err: err}
		}()
	}
	added := 0
	routes := map[string]int{}
	for i := 0; i < slots; i++ {
		r := <-results
		if r.err != nil || r.conn == nil {
			if r.err != nil {
				log.Tracef("%s WS pool refill %s slot failed: %v", tg(""), k, r.err)
			}
			continue
		}
		p.mu.Lock()
		keep := p.ctx.Err() == nil && len(p.idle[k]) < p.targetFor(k)
		if keep {
			p.idle[k] = append(p.idle[k], wsPoolEntry{conn: r.conn.conn, created: time.Now(), plan: r.conn.plan})
		}
		p.mu.Unlock()
		if !keep {
			go func(c *wsConn) { _ = c.Close() }(r.conn.conn)
			continue
		}
		routes[r.conn.plan.describe()]++
		added++
	}

	p.mu.Lock()
	var backoff time.Duration
	if added == 0 {
		f := p.refillFails[k] + 1
		p.refillFails[k] = f
		backoff = wsRefillBackoff(f)
		p.refillAfter[k] = time.Now().Add(backoff)
	} else {
		delete(p.refillFails, k)
		delete(p.refillAfter, k)
	}
	target := p.targetFor(k)
	p.mu.Unlock()

	if added > 0 {
		names := make([]string, 0, len(routes))
		for r, n := range routes {
			names = append(names, fmt.Sprintf("%s x%d", r, n))
		}
		sort.Strings(names)
		log.Debugf("%s WS pool %s refilled +%d (target=%d) on %s", tg(""), k, added, target, strings.Join(names, ", "))
	} else if p.ctx.Err() == nil {
		log.Tracef("%s WS pool %s refill got nothing, next try in %v", tg(""), k, backoff)
	}
}

// dialFresh opens a raw WS connection (TLS + Upgrade) to a TG edge for `dc`.
// Returns the first plan to succeed together with that plan, or the last error.
func (p *wsPool) dialFresh(dc int, plans []transportPlan, timeout time.Duration) (*wsPoolConn, error) {
	if len(plans) > wsPoolMaxPlanAttempts {
		plans = plans[:wsPoolMaxPlanAttempts]
	}
	var lastErr error
	for _, pl := range plans {
		c, err := p.dialPlan(dc, pl, timeout)
		if err == nil {
			return c, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = net.ErrClosed
	}
	return nil, lastErr
}

func (p *wsPool) dialPlan(dc int, pl transportPlan, timeout time.Duration) (*wsPoolConn, error) {
	c, err := p.dialOnce(dc, pl, timeout)
	front := p.cfg.FrontSNI
	if err == nil || !pl.native || front == "" || isConnectStage(err) {
		return c, err
	}
	twin := pl
	switch {
	case pl.frontSNI != "":
		if isFrontMiss(err) {
			wsFrontRefuse(pl.dialHost, pl.frontSNI)
			wsFrontRecord(pl.dialHost, pl.frontSNI, false)
		}
		twin.frontSNI = ""
	case isTLSStage(err) && !wsFrontRefused(pl.dialHost, front) && wsProbeAllowed(pl.dialHost+"|front"):
		twin.frontSNI = front
	default:
		return nil, err
	}
	c2, err2 := p.dialOnce(dc, twin, timeout)
	if err2 != nil {
		if isFrontMiss(err2) {
			wsFrontRefuse(pl.dialHost, front)
		}
		log.Tracef("%s WS pool %s answered neither as %s nor as %s: %v", tg(""), pl.dialHost, pl.tlsName(), twin.tlsName(), err2)
		return nil, err
	}
	wsFrontRecord(twin.dialHost, front, twin.frontSNI != "")
	return c2, nil
}

func (p *wsPool) dialOnce(dc int, pl transportPlan, timeout time.Duration) (*wsPoolConn, error) {
	host := pl.dialHost
	if host == "" {
		host = pl.sni
	}
	conn, err := dialWSAs(host, pl.tlsName(), pl.sni, pl.wsPath, timeout, planDialMark(pl, p.mark))
	if err != nil {
		return nil, err
	}
	wsc, ok := conn.(*wsConn)
	if !ok {
		_ = conn.Close()
		return nil, net.ErrClosed
	}
	if pl.native {
		wsRecordSuccess(dc)
	}
	return &wsPoolConn{conn: wsc, plan: pl}, nil
}

func withoutNative(plans []transportPlan) []transportPlan {
	out := make([]transportPlan, 0, len(plans))
	for _, pl := range plans {
		if !pl.native {
			out = append(out, pl)
		}
	}
	return out
}

func onlyNative(plans []transportPlan) []transportPlan {
	out := make([]transportPlan, 0, 2)
	for _, pl := range plans {
		if pl.native {
			out = append(out, pl)
		}
	}
	return out
}

func wsPlansForDC(dc int, cfg *MTProtoUpstream) []transportPlan {
	absDC := dc
	if absDC < 0 {
		absDC = -absDC
	}
	var plans []transportPlan
	override := ""
	front := ""
	cfProxy := false
	if cfg != nil {
		override = cfg.WSEndpointHost
		front = cfg.FrontSNI
		cfProxy = cfg.CFProxyEnabled
	}
	edge := wsEdgeServesDC(absDC)
	dh := wsNativeDialHost(override)
	if edge {
		plans = append(plans, nativeRoutes(dc, absDC, dh, front)...)
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
	if cfProxy && (!edge || wsNativeDown(dc, dh, front)) {
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
	if p.target < wsPoolCFTarget {
		return p.target
	}
	if wsEdgeServesDC(k.dc) && !wsNativeDown(k.signed(), wsNativeDialHost(p.cfg.WSEndpointHost), p.cfg.FrontSNI) {
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
