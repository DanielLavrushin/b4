package nfq

import (
	"net"
	"net/netip"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
)

type dnsClaimKind uint8

const (
	claimPending dnsClaimKind = iota
	claimAnswer
	claimDrop
	claimQuiet
)

type dnsClaimBegin uint8

const (
	claimBeginPrimary dnsClaimBegin = iota
	claimBeginDuplicate
	claimBeginQuiet
	claimBeginRefused
)

const dnsClaimTTLExtra = 2 * time.Second

var dnsClaimMax = 8192

type dnsClaimKey struct {
	ver   uint8
	txid  uint16
	qtype uint16
	name  string
}

type dnsWaitKey struct {
	name  string
	qtype uint16
}

type dnsClaim struct {
	key             dnsClaimKey
	client          netip.Addr
	server          netip.Addr
	clientPort      uint16
	routerAsked     bool
	kind            dnsClaimKind
	resp            []byte
	waits           []<-chan struct{}
	ready           chan struct{}
	replySeen       bool
	delivered       bool
	injectDue       bool
	injectWhenReady bool
	copiedAt        time.Time
	expires         time.Time
	timer           *time.Timer
	counted         bool
	removed         bool
}

func (c *dnsClaim) live(now time.Time) bool {
	return now.Before(c.expires)
}

func (c *dnsClaim) unanswered() bool {
	return (c.kind == claimAnswer || c.kind == claimDrop) && !c.replySeen && !c.delivered
}

type dnsClaimTable struct {
	mu                sync.Mutex
	byKey             map[dnsClaimKey][]*dnsClaim
	waiting           map[dnsWaitKey]int
	order             []*dnsClaim
	head              int
	live              atomic.Int64
	expiredUnanswered atomic.Uint64
	timers            *sync.WaitGroup
}

const dnsClaimOrderCompactAt = 1024

func newDNSClaimTable(timers *sync.WaitGroup) *dnsClaimTable {
	return &dnsClaimTable{
		byKey:   map[dnsClaimKey][]*dnsClaim{},
		waiting: map[dnsWaitKey]int{},
		timers:  timers,
	}
}

func dnsClaimTTL(cfg *config.Config) time.Duration {
	return 2*cfg.DNSQueryTimeout() + dnsClaimTTLExtra
}

func dnsClaimKeyOf(ver uint8, msg []byte) (dnsClaimKey, bool) {
	txid, ok := dns.ParseTransactionID(msg)
	if !ok {
		return dnsClaimKey{}, false
	}
	qtype, ok := dns.QuestionType(msg)
	if !ok {
		return dnsClaimKey{}, false
	}
	name, ok := dns.ParseQueryDomain(msg)
	if !ok {
		return dnsClaimKey{}, false
	}
	return dnsClaimKey{ver: ver, txid: txid, qtype: qtype, name: strings.ToLower(name)}, true
}

func dnsClaimAddr(ip net.IP) netip.Addr {
	a, _ := netip.AddrFromSlice(ip)
	return a.Unmap()
}

func dnsHostAddr(a netip.Addr) bool {
	return a.IsValid() && hostAddress(a.AsSlice())
}

func (t *dnsClaimTable) begin(key dnsClaimKey, client, server netip.Addr, clientPort uint16, now time.Time, ttl time.Duration) (*dnsClaim, dnsClaimBegin) {
	clientHost, serverHost := dnsHostAddr(client), dnsHostAddr(server)
	t.mu.Lock()
	defer t.mu.Unlock()
	t.expireLocked(now)
	for _, c := range t.byKey[key] {
		if c.live(now) && c.client == client && c.clientPort == clientPort && c.server == server {
			return c, claimBeginDuplicate
		}
	}
	kind, outcome := claimPending, claimBeginPrimary
	if clientHost && !serverHost && t.waiting[dnsWaitKey{name: key.name, qtype: key.qtype}] > 0 {
		kind, outcome = claimQuiet, claimBeginQuiet
	}
	if int(t.live.Load()) >= dnsClaimMax {
		return nil, claimBeginRefused
	}
	c := &dnsClaim{
		key:         key,
		client:      client,
		server:      server,
		clientPort:  clientPort,
		routerAsked: serverHost,
		kind:        kind,
		ready:       make(chan struct{}),
		copiedAt:    now,
		expires:     now.Add(ttl),
	}
	t.byKey[key] = append(t.byKey[key], c)
	t.live.Add(1)
	if kind == claimQuiet {
		t.order = append(t.order, c)
	}
	return c, outcome
}

func (t *dnsClaimTable) keepLocked(c *dnsClaim) {
	t.order = append(t.order, c)
}

func (t *dnsClaimTable) expireLocked(now time.Time) {
	for t.head < len(t.order) {
		c := t.order[t.head]
		if !c.removed && c.live(now) {
			break
		}
		t.order[t.head] = nil
		t.head++
		if !c.removed {
			t.removeLocked(c)
			t.noteExpiredLocked(c)
		}
	}
	switch {
	case t.head == len(t.order):
		t.order, t.head = t.order[:0], 0
	case t.head >= dnsClaimOrderCompactAt && 2*t.head >= len(t.order):
		n := copy(t.order, t.order[t.head:])
		clear(t.order[n:])
		t.order, t.head = t.order[:n], 0
	}
}

func (t *dnsClaimTable) noteExpiredLocked(c *dnsClaim) {
	if !c.unanswered() {
		return
	}
	t.expiredUnanswered.Add(1)
	log.Debugf("DNS: the copied query for %s from %s to %s expired with no reply seen and no answer delivered", dns.SafeName(c.key.name), c.client, c.server)
}

func (t *dnsClaimTable) replyLocked(key dnsClaimKey, src, dst netip.Addr, dport uint16, srcHost, dstHost bool, now time.Time) *dnsClaim {
	var nat *dnsClaim
	for _, c := range t.byKey[key] {
		if c.kind == claimPending || !c.live(now) || c.server != src || c.clientPort != dport {
			continue
		}
		if c.client == dst {
			return c
		}
		if nat == nil && dstHost && !srcHost {
			nat = c
		}
	}
	return nat
}

func (t *dnsClaimTable) seamLocked(key dnsClaimKey, server, client netip.Addr, port uint16, now time.Time) (open, settled *dnsClaim) {
	for _, c := range t.byKey[key] {
		if (c.kind != claimPending && c.kind != claimAnswer) || !c.live(now) || c.server != server || c.client != client || c.clientPort != port {
			continue
		}
		if c.resp == nil {
			return c, nil
		}
		if settled == nil {
			settled = c
		}
	}
	return nil, settled
}

func (t *dnsClaimTable) seamByIDLocked(ver uint8, txid uint16, server, client netip.Addr, port uint16, now time.Time) (open, settled *dnsClaim) {
	for key := range t.byKey {
		if key.ver != ver || key.txid != txid {
			continue
		}
		o, s := t.seamLocked(key, server, client, port, now)
		if o != nil {
			return o, nil
		}
		if settled == nil {
			settled = s
		}
	}
	return nil, settled
}

func (t *dnsClaimTable) countLocked(c *dnsClaim) {
	if c.counted {
		return
	}
	c.counted = true
	t.waiting[dnsWaitKey{name: c.key.name, qtype: c.key.qtype}]++
}

func (t *dnsClaimTable) uncountLocked(c *dnsClaim) {
	if !c.counted {
		return
	}
	c.counted = false
	k := dnsWaitKey{name: c.key.name, qtype: c.key.qtype}
	if t.waiting[k] <= 1 {
		delete(t.waiting, k)
		return
	}
	t.waiting[k]--
}

func (t *dnsClaimTable) answerOf(c *dnsClaim) ([]byte, []<-chan struct{}) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return c.resp, c.waits
}

func (t *dnsClaimTable) releaseLocked(c *dnsClaim) {
	if c.removed {
		return
	}
	c.removed = true
	t.live.Add(-1)
	if c.timer != nil && c.timer.Stop() {
		t.timers.Done()
	}
	c.timer = nil
	t.uncountLocked(c)
	select {
	case <-c.ready:
	default:
		close(c.ready)
	}
}

func (t *dnsClaimTable) removeLocked(c *dnsClaim) {
	bucket := t.byKey[c.key]
	for i, x := range bucket {
		if x != c {
			continue
		}
		copy(bucket[i:], bucket[i+1:])
		bucket[len(bucket)-1] = nil
		bucket = bucket[:len(bucket)-1]
		break
	}
	if len(bucket) == 0 {
		delete(t.byKey, c.key)
	} else {
		t.byKey[c.key] = bucket
	}
	t.releaseLocked(c)
}

func (t *dnsClaimTable) sweep(now time.Time) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sweepLocked(now)
}

func (t *dnsClaimTable) sweepLocked(now time.Time) {
	for key, bucket := range t.byKey {
		kept := bucket[:0]
		for _, c := range bucket {
			if c.live(now) {
				kept = append(kept, c)
				continue
			}
			t.releaseLocked(c)
			t.noteExpiredLocked(c)
		}
		if len(kept) == len(bucket) {
			continue
		}
		clear(bucket[len(kept):])
		if len(kept) == 0 {
			delete(t.byKey, key)
		} else {
			t.byKey[key] = kept
		}
	}
	t.expireLocked(now)
}

func (t *dnsClaimTable) reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	for key, bucket := range t.byKey {
		for _, c := range bucket {
			t.releaseLocked(c)
		}
		delete(t.byKey, key)
	}
	clear(t.order)
	t.order, t.head = t.order[:0], 0
}
