package nfq

import (
	"encoding/binary"
	"errors"
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nflog"
	"github.com/daniellavrushin/b4/sock"
)

const (
	maxDNSClaimHolds    = 512
	nfHookForward       = 2
	nfHookLocalOut      = 3
	dnsCopyLostLogEvery = 5 * time.Minute
	dnsCopyFullLogEvery = time.Minute
)

var (
	dnsClaimInjectAfter = 20 * time.Millisecond
	dnsClaimHoldSlots   = make(chan struct{}, maxDNSClaimHolds)

	errNoDNSCopyListener = errors.New("b4 has no DNS query copy listener")
	errDNSCopyClosed     = errors.New("the DNS query copy listener is stopped")
	errDNSCopyEnded      = errors.New("the NFLOG socket stopped delivering copies")

	dnsCopyCurrent    atomic.Pointer[dnsCopyListener]
	dnsCopyStopWarned atomic.Bool
)

type dnsCopySource interface {
	Run(fn func(nflog.Packet)) error
	Drain(fn func(nflog.Packet)) error
	Lost() uint64
	Close() error
}

var openDNSCopySource = func(group uint16) (dnsCopySource, error) {
	c, err := nflog.Open(group, nfqReadBufferBytes)
	if err != nil {
		return nil, err
	}
	return c, nil
}

type dnsCopyBinding struct {
	src     dnsCopySource
	group   uint16
	closing atomic.Bool
	done    chan struct{}
}

type dnsCopyStats struct {
	copies                 atomic.Uint64
	ignored                atomic.Uint64
	claimsAnswer           atomic.Uint64
	claimsDrop             atomic.Uint64
	claimsQuiet            atomic.Uint64
	claimsRefused          atomic.Uint64
	repliesReplaced        atomic.Uint64
	repliesReplaceFallback atomic.Uint64
	repliesDropped         atomic.Uint64
	repliesQuiet           atomic.Uint64
	repliesFailedOpen      atomic.Uint64
	injections             atomic.Uint64
}

type dnsCopyListener struct {
	w      *Worker
	claims *dnsClaimTable
	wg     sync.WaitGroup

	openMu sync.Mutex
	source atomic.Pointer[dnsCopyBinding]
	closed atomic.Bool
	errMu  sync.Mutex
	err    string

	mu      sync.Mutex
	running atomic.Bool
	enabled atomic.Bool
	v4      atomic.Bool
	v6      atomic.Bool
	mark    atomic.Uint32

	lostBase           atomic.Uint64
	lostReported       atomic.Uint64
	lostLoggedAt       atomic.Int64
	refusedLoggedAt    atomic.Int64
	keyRefusedLoggedAt atomic.Int64

	stats dnsCopyStats
}

type DNSQueryCopyCounters struct {
	Group                  int
	Bound                  bool
	Alive                  bool
	Error                  string
	Copies                 uint64
	CopiesLost             uint64
	CopiesIgnored          uint64
	ClaimsAnswer           uint64
	ClaimsDrop             uint64
	ClaimsQuiet            uint64
	ClaimsRefused          uint64
	ClaimsLive             uint64
	RepliesReplaced        uint64
	RepliesReplaceFallback uint64
	RepliesDropped         uint64
	RepliesQuiet           uint64
	RepliesFailedOpen      uint64
	Injections             uint64
	ExpiredUnanswered      uint64
}

func DNSQueryCopyStats() (DNSQueryCopyCounters, bool) {
	l := dnsCopyCurrent.Load()
	if l == nil {
		return DNSQueryCopyCounters{}, false
	}
	return l.counters(), true
}

func newDNSCopyListener(w *Worker) *dnsCopyListener {
	l := &dnsCopyListener{w: w}
	l.claims = newDNSClaimTable(&l.wg)
	return l
}

func (l *dnsCopyListener) open() (uint16, error) {
	if l == nil {
		return 0, errNoDNSCopyListener
	}
	l.openMu.Lock()
	defer l.openMu.Unlock()
	if l.closed.Load() {
		return 0, errDNSCopyClosed
	}
	if b := l.source.Load(); b != nil {
		if l.running.Load() {
			return b.group, nil
		}
		l.retire(b)
	}
	group := uint16(l.w.getConfig().Queue.StartNum)
	src, err := openDNSCopySource(group)
	if err != nil {
		l.setErr(err)
		return 0, err
	}
	b := &dnsCopyBinding{src: src, group: group, done: make(chan struct{})}
	l.source.Store(b)
	l.running.Store(true)
	l.setErr(nil)
	l.wg.Add(1)
	go l.read(b)
	return group, nil
}

func (l *dnsCopyListener) read(b *dnsCopyBinding) {
	defer l.wg.Done()
	defer close(b.done)
	err := b.src.Run(l.handle)
	if b.closing.Load() {
		return
	}
	if err == nil {
		err = errDNSCopyEnded
	}
	l.running.Store(false)
	l.setErr(err)
	if dnsCopyStopWarned.CompareAndSwap(false, true) {
		log.Warnf("DNS: the NFLOG copy of DNS queries stopped (%v); the replies to DNS queries are not replaced with b4's answers until the firewall monitor starts it again at its next check, or holds the queries in b4's queue if it cannot", err)
	}
}

func (l *dnsCopyListener) retire(b *dnsCopyBinding) {
	b.closing.Store(true)
	_ = b.src.Close()
	<-b.done
	l.lostBase.Add(b.src.Lost())
	l.source.CompareAndSwap(b, nil)
}

func (l *dnsCopyListener) setErr(err error) {
	l.errMu.Lock()
	defer l.errMu.Unlock()
	if err == nil {
		l.err = ""
		return
	}
	l.err = err.Error()
}

func (l *dnsCopyListener) alive() bool {
	return l != nil && l.source.Load() != nil && l.running.Load()
}

func (l *dnsCopyListener) setFamilies(v4, v6 bool) {
	if l == nil {
		return
	}
	l.v4.Store(v4)
	l.v6.Store(v6)
}

func (l *dnsCopyListener) active(ver uint8) bool {
	if l == nil || !l.enabled.Load() {
		return false
	}
	switch ver {
	case IPv4:
		return l.v4.Load()
	case IPv6:
		return l.v6.Load()
	}
	return false
}

func (l *dnsCopyListener) enable(mark uint32) {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.mark.Store(mark)
	l.enabled.Store(true)
	l.mu.Unlock()
}

func (l *dnsCopyListener) disable() {
	if l == nil {
		return
	}
	l.mu.Lock()
	l.enabled.Store(false)
	l.mu.Unlock()
}

func (l *dnsCopyListener) close() {
	if l == nil {
		return
	}
	l.disable()
	l.openMu.Lock()
	l.closed.Store(true)
	b := l.source.Swap(nil)
	l.openMu.Unlock()
	if b != nil {
		b.closing.Store(true)
		_ = b.src.Close()
		<-b.done
		l.lostBase.Add(b.src.Lost())
	}
	l.claims.reset()
	l.wg.Wait()
	l.running.Store(false)
}

func (l *dnsCopyListener) drain() {
	if l == nil || !l.enabled.Load() {
		return
	}
	if b := l.source.Load(); b != nil {
		_ = b.src.Drain(l.handle)
	}
}

func (l *dnsCopyListener) sweep(now time.Time) {
	if l == nil {
		return
	}
	l.claims.sweep(now)
	l.warnLost(now)
}

func (l *dnsCopyListener) lost() uint64 {
	n := l.lostBase.Load()
	if b := l.source.Load(); b != nil {
		n += b.src.Lost()
	}
	return n
}

func (l *dnsCopyListener) warnLost(now time.Time) {
	lost, reported := l.lost(), l.lostReported.Load()
	if lost <= reported {
		return
	}
	if last := l.lostLoggedAt.Load(); last != 0 && now.Sub(time.Unix(0, last)) < dnsCopyLostLogEvery {
		return
	}
	l.lostReported.Store(lost)
	l.lostLoggedAt.Store(now.UnixNano())
	log.Warnf("DNS: %d copies of DNS queries were lost; the answers to those queries were not replaced with b4's answers", lost-reported)
}

func logDue(at *atomic.Int64, every time.Duration) bool {
	now := time.Now().UnixNano()
	last := at.Load()
	if last != 0 && time.Duration(now-last) < every {
		return false
	}
	return at.CompareAndSwap(last, now)
}

func (l *dnsCopyListener) warnFull() {
	if !logDue(&l.refusedLoggedAt, dnsCopyFullLogEvery) {
		return
	}
	log.Warnf("DNS: b4 already has %d DNS answers to deliver, the most it keeps, so the replies to the DNS queries copied since then were not replaced with b4's answers", dnsClaimMax)
}

func (l *dnsCopyListener) warnKeyFull(key dnsClaimKey, client, server netip.Addr) {
	if !logDue(&l.keyRefusedLoggedAt, dnsCopyFullLogEvery) {
		return
	}
	log.Warnf("DNS: b4 already has %d DNS answers to deliver for the %s query for %s with ID %d, sent from different sockets, the most it keeps for one query, so the replies to the copies of it since then were not replaced with b4's answers; the last came from %s to %s", dnsClaimKeyMax, dnsTypeName(key.qtype), dns.SafeName(key.name), key.txid, client, server)
}

func (l *dnsCopyListener) counters() DNSQueryCopyCounters {
	c := DNSQueryCopyCounters{
		Alive:                  l.running.Load(),
		Copies:                 l.stats.copies.Load(),
		CopiesLost:             l.lost(),
		CopiesIgnored:          l.stats.ignored.Load(),
		ClaimsAnswer:           l.stats.claimsAnswer.Load(),
		ClaimsDrop:             l.stats.claimsDrop.Load(),
		ClaimsQuiet:            l.stats.claimsQuiet.Load(),
		ClaimsRefused:          l.stats.claimsRefused.Load(),
		RepliesReplaced:        l.stats.repliesReplaced.Load(),
		RepliesReplaceFallback: l.stats.repliesReplaceFallback.Load(),
		RepliesDropped:         l.stats.repliesDropped.Load(),
		RepliesQuiet:           l.stats.repliesQuiet.Load(),
		RepliesFailedOpen:      l.stats.repliesFailedOpen.Load(),
		Injections:             l.stats.injections.Load(),
		ExpiredUnanswered:      l.claims.expiredUnanswered.Load(),
	}
	if live := l.claims.live.Load(); live > 0 {
		c.ClaimsLive = uint64(live)
	}
	if b := l.source.Load(); b != nil {
		c.Bound, c.Group = true, int(b.group)
	}
	l.errMu.Lock()
	c.Error = l.err
	l.errMu.Unlock()
	return c
}

func (l *dnsCopyListener) ignoresMark(m uint32, cfg *config.Config) bool {
	return selfInjectedMark(m, l.mark.Load(), cfg) ||
		m == uint32(cfg.DiscoveryFlowMark()) || m == uint32(cfg.DiscoveryInjectedMark()) ||
		config.IsTelegramBridgeMark(m) || config.IsSelfDialSkipMark(m)
}

func (l *dnsCopyListener) localQueueMarked(p nflog.Packet) bool {
	q := l.mark.Load()
	return p.Hook == nfHookLocalOut && q != 0 && p.Mark&q == q
}

func dnsCopyIface(p nflog.Packet) (uint32, ifaceRole) {
	if p.Hook == nfHookForward {
		return p.InDev, ifaceArriving
	}
	if p.OutDev != 0 {
		return p.OutDev, ifaceLeaving
	}
	return p.InDev, ifaceArriving
}

func (l *dnsCopyListener) handle(p nflog.Packet) {
	l.stats.copies.Add(1)
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.enabled.Load() {
		l.stats.ignored.Add(1)
		return
	}
	w := l.w
	if w.ctx != nil && w.ctx.Err() != nil {
		return
	}
	cfg := w.getConfig()
	if p.Prefix != config.DNSQueryCopyPrefix || l.ignoresMark(p.Mark, cfg) || l.localQueueMarked(p) || len(p.Payload) == 0 || !w.ifaceAllowed(dnsCopyIface(p)) {
		l.stats.ignored.Add(1)
		return
	}
	pkt, ok := w.parseIPHeaders(p.Payload)
	if !ok || pkt.proto != 17 || len(pkt.raw) < pkt.ihl+UDPHeaderLen || !l.active(pkt.ver) {
		l.stats.ignored.Add(1)
		return
	}
	udp := pkt.raw[pkt.ihl:]
	sport := binary.BigEndian.Uint16(udp[0:2])
	payload := udp[UDPHeaderLen:]
	if sport == 53 || binary.BigEndian.Uint16(udp[2:4]) != 53 || len(payload) < 12 || payload[2]&0x80 != 0 {
		l.stats.ignored.Add(1)
		return
	}
	atomic.AddUint64(&w.packetsProcessed, 1)
	l.decide(pkt, sport, payload)
}

func (l *dnsCopyListener) decide(pkt *pktInfo, sport uint16, payload []byte) {
	key, ok := dnsClaimKeyOf(pkt.ver, payload)
	if !ok {
		l.stats.ignored.Add(1)
		return
	}
	client, server := dnsClaimAddr(pkt.src), dnsClaimAddr(pkt.dst)
	c, outcome := l.claims.begin(key, client, server, sport, time.Now(), dnsClaimTTL(l.w.getConfig()))
	switch outcome {
	case claimBeginDuplicate:
		return
	case claimBeginQuiet:
		l.stats.claimsQuiet.Add(1)
		return
	case claimBeginRefused:
		l.stats.claimsRefused.Add(1)
		l.warnFull()
		return
	case claimBeginKeyFull:
		l.stats.claimsRefused.Add(1)
		l.warnKeyFull(key, client, server)
		return
	}
	vc := &verdictCtx{verdict: engine.VerdictAccept, copy: &dnsCopyDecision{claim: c}}
	l.w.processDnsPacket(vc, pkt, sport, 53, payload)
	l.finish(vc.copy)
}

func (l *dnsCopyListener) finish(d *dnsCopyDecision) {
	c := d.claim
	t := l.claims
	t.mu.Lock()
	if c.removed {
		t.mu.Unlock()
		return
	}
	switch d.kind {
	case claimAnswer, claimDrop:
		c.kind = d.kind
		c.waits = d.waits
		t.keepLocked(c)
		if c.routerAsked && !c.replySeen {
			t.countLocked(c)
		}
		if c.kind == claimAnswer && c.routerAsked {
			l.armInjectionLocked(c)
		}
	default:
		t.removeLocked(c)
	}
	t.mu.Unlock()
	switch d.kind {
	case claimAnswer:
		l.stats.claimsAnswer.Add(1)
	case claimDrop:
		l.stats.claimsDrop.Add(1)
	}
}

func (l *dnsCopyListener) armInjectionLocked(c *dnsClaim) {
	l.wg.Add(1)
	c.timer = time.AfterFunc(max(time.Until(c.copiedAt.Add(dnsClaimInjectAfter)), 0), func() {
		defer l.wg.Done()
		l.injectDue(c)
	})
}

func (l *dnsCopyListener) injectableLocked(c *dnsClaim) bool {
	return !c.removed && c.injectDue && c.routerAsked && c.kind == claimAnswer && !c.delivered &&
		c.resp != nil && (!c.replySeen || c.injectWhenReady) && l.enabled.Load()
}

func (l *dnsCopyListener) injectDue(c *dnsClaim) {
	t := l.claims
	t.mu.Lock()
	c.injectDue = true
	fire := l.injectableLocked(c)
	if fire {
		l.claimInjectedLocked(c)
	}
	t.mu.Unlock()
	if fire {
		l.inject(c)
	}
}

func (l *dnsCopyListener) claimInjectedLocked(c *dnsClaim) {
	c.delivered = true
	l.claims.uncountLocked(c)
}

func (l *dnsCopyListener) inject(c *dnsClaim) {
	l.w.waitRoutesInline(c.waits, nil)
	l.w.sendDNSPacket(c.key.ver, c.server.AsSlice(), c.client.AsSlice(), c.clientPort, c.resp)
	l.stats.injections.Add(1)
}

func (l *dnsCopyListener) deliver(ver byte, originalDst, clientIP net.IP, clientPort uint16, query, resp []byte) bool {
	t := l.claims
	if t.live.Load() == 0 {
		return false
	}
	key, keyed := dnsClaimKeyOf(ver, resp)
	if !keyed && query != nil {
		key, keyed = dnsClaimKeyOf(ver, query)
	}
	if !keyed && len(resp) < 12 {
		return false
	}
	server, client := dnsClaimAddr(originalDst), dnsClaimAddr(clientIP)
	swallow := l.active(ver)
	now := time.Now()
	t.mu.Lock()
	var open, settled *dnsClaim
	if keyed {
		open, settled = t.seamLocked(key, server, client, clientPort, now)
	} else {
		open, settled = t.seamByIDLocked(ver, binary.BigEndian.Uint16(resp[0:2]), server, client, clientPort, now)
	}
	if open == nil {
		t.mu.Unlock()
		return swallow && settled != nil
	}
	open.resp = append([]byte(nil), resp...)
	close(open.ready)
	fire := swallow && l.injectableLocked(open)
	if fire {
		l.claimInjectedLocked(open)
	}
	t.mu.Unlock()
	if fire {
		l.inject(open)
	}
	return swallow
}

func (w *Worker) dnsCopyMayClaim(raw []byte) bool {
	l := w.dnsCopy
	if l == nil || len(raw) == 0 {
		return false
	}
	active := l.active(raw[0] >> 4)
	if !active && l.claims.live.Load() == 0 {
		return false
	}
	l4 := IPv6HeaderLen
	if raw[0]>>4 == IPv4 {
		l4 = int(raw[0]&0x0f) * 4
		if l4 < IPv4HeaderMinLen || len(raw) < l4 || raw[9] != 17 || binary.BigEndian.Uint16(raw[6:8])&0x3fff != 0 {
			return false
		}
	} else if len(raw) < IPv6HeaderLen || raw[6] != 17 {
		return false
	}
	if len(raw) < l4+UDPHeaderLen || binary.BigEndian.Uint16(raw[l4:l4+2]) != 53 || binary.BigEndian.Uint16(raw[l4+2:l4+4]) == 53 {
		return false
	}
	if active {
		l.drain()
	}
	return l.claims.live.Load() > 0
}

func (w *Worker) takeClaimHold() bool {
	w.holdsLive.Add(1)
	if w.holdsReleased() {
		w.holdsLive.Add(-1)
		return false
	}
	select {
	case dnsClaimHoldSlots <- struct{}{}:
		return true
	default:
		w.holdsLive.Add(-1)
		return false
	}
}

func (w *Worker) dnsCopyGate(vc *verdictCtx, pkt *pktInfo, dport, txid uint16, domain string, payload []byte) bool {
	l := w.dnsCopy
	if l == nil || vc.q == nil || vc.copy != nil || dport == 53 || len(payload) < 3 || payload[2]&0x80 == 0 {
		return false
	}
	if l.active(pkt.ver) {
		l.drain()
	}
	if domain == "" || l.claims.live.Load() == 0 {
		return false
	}
	qtype, ok := dns.QuestionType(payload)
	if !ok {
		return false
	}
	key := dnsClaimKey{ver: pkt.ver, txid: txid, qtype: qtype, name: strings.ToLower(domain)}
	src, dst := dnsClaimAddr(pkt.src), dnsClaimAddr(pkt.dst)
	srcHost, dstHost := dnsHostAddr(src), dnsHostAddr(dst)

	t := l.claims
	t.mu.Lock()
	c := t.replyLocked(key, src, dst, dport, srcHost, dstHost, time.Now())
	if c == nil {
		t.mu.Unlock()
		return false
	}
	c.replySeen = true
	t.uncountLocked(c)
	kind, resp, waits := c.kind, c.resp, c.waits
	held, injectLater := false, false
	if kind == claimAnswer && resp == nil {
		held = w.takeClaimHold()
		if !held && c.routerAsked && !w.holdsReleased() {
			c.injectWhenReady = true
			injectLater = true
		}
	}
	t.mu.Unlock()

	switch {
	case kind == claimQuiet:
		vc.accept()
		l.stats.repliesQuiet.Add(1)
	case kind == claimDrop:
		vc.drop()
		l.stats.repliesDropped.Add(1)
	case resp != nil:
		w.replaceClaimedReply(vc, pkt, dport, resp, waits, domain)
	case held:
		w.holdClaimedReply(vc, pkt, dport, c, domain)
	case injectLater:
		vc.drop()
		l.stats.repliesDropped.Add(1)
	default:
		vc.accept()
		l.stats.repliesFailedOpen.Add(1)
	}
	return true
}

func (w *Worker) replaceClaimedReply(vc *verdictCtx, pkt *pktInfo, dport uint16, resp []byte, waits []<-chan struct{}, domain string) {
	raw := sock.ReplaceUDPPayload(pkt.raw, pkt.ihl, resp)
	ver := pkt.ver
	server := append(net.IP(nil), pkt.src...)
	client := append(net.IP(nil), pkt.dst...)
	release := func() { w.releaseClaimedReply(vc, raw, ver, server, client, dport, resp) }
	if !w.holdForRoutes(vc, waits, domain, release) {
		release()
	}
}

func (w *Worker) releaseClaimedReply(vc *verdictCtx, raw []byte, ver byte, server, client net.IP, dport uint16, resp []byte) {
	l := w.dnsCopy
	if raw != nil && vc.replace(raw) {
		l.stats.repliesReplaced.Add(1)
		return
	}
	if !vc.drop() {
		return
	}
	w.sendDNSPacket(ver, server, client, dport, resp)
	l.stats.repliesReplaceFallback.Add(1)
}

func (w *Worker) holdClaimedReply(vc *verdictCtx, pkt *pktInfo, dport uint16, c *dnsClaim, domain string) {
	l := w.dnsCopy
	raw := append([]byte(nil), pkt.raw...)
	ihl, ver := pkt.ihl, pkt.ver
	server := append(net.IP(nil), pkt.src...)
	client := append(net.IP(nil), pkt.dst...)
	stop := w.holdStopSignal()
	w.wg.Add(1)
	go func() {
		defer func() {
			<-dnsClaimHoldSlots
			w.holdsLive.Add(-1)
			w.wg.Done()
		}()
		start := time.Now()
		expiry := time.NewTimer(time.Until(c.expires))
		defer expiry.Stop()
		var resp []byte
		var waits []<-chan struct{}
		select {
		case <-c.ready:
			resp, waits = l.claims.answerOf(c)
		case <-expiry.C:
		case <-stop:
		}
		if resp == nil {
			vc.accept()
			l.stats.repliesFailedOpen.Add(1)
			log.Tracef("DNS reply for %s went through unchanged after %s: b4's own answer never came", dns.SafeName(domain), time.Since(start).Round(time.Millisecond))
			return
		}
		limit := time.NewTimer(dnsRouteHoldMax)
		defer limit.Stop()
		waitForRoutes(waits, limit.C, stop, nil)
		w.releaseClaimedReply(vc, sock.ReplaceUDPPayload(raw, ihl, resp), ver, server, client, dport, resp)
	}()
}
