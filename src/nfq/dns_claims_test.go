package nfq

import (
	"net/netip"
	"strconv"
	"sync"
	"testing"
	"time"
)

func claimKey(name string, txid uint16) dnsClaimKey {
	return dnsClaimKey{ver: IPv4, txid: txid, qtype: dnsTypeA, name: name}
}

func TestAFullClaimTableRefusesWithoutWalkingItsClaims(t *testing.T) {
	thisHost(t, rigRouter, rigWAN)
	var timers sync.WaitGroup
	client, server := netip.MustParseAddr(rigDevice), netip.MustParseAddr(rigOutside)
	now := time.Now()
	const tries = 2000
	keys := make([]dnsClaimKey, dnsClaimMax+tries)
	for i := range keys {
		keys[i] = claimKey("n"+strconv.Itoa(i)+".example", uint16(i))
	}

	fresh := newDNSClaimTable(&timers)
	start := time.Now()
	for _, k := range keys[dnsClaimMax:] {
		fresh.begin(k, client, server, 40000, now, time.Minute)
	}
	below := time.Since(start)

	full := newDNSClaimTable(&timers)
	for _, k := range keys[:dnsClaimMax] {
		full.begin(k, client, server, 40000, now, time.Minute)
	}
	start = time.Now()
	for _, k := range keys[dnsClaimMax:] {
		if _, outcome := full.begin(k, client, server, 40000, now, time.Minute); outcome != claimBeginRefused {
			t.Fatalf("a copy past the cap of %d live claims got outcome %d, want it refused", dnsClaimMax, outcome)
		}
	}
	atCap := time.Since(start)
	if atCap > 10*below+20*time.Millisecond {
		t.Errorf("%d copies refused at the cap took %s, against %s for as many claims below it: every copy the NFLOG reader decides waits on this, under the lock each DNS answer's drain needs", tries, atCap, below)
	}
}

func TestARetransmittedCopyJoinsItsClaimWhenItsQueryOrTheTableIsFull(t *testing.T) {
	thisHost(t, rigRouter, rigWAN)
	var timers sync.WaitGroup
	tbl := newDNSClaimTable(&timers)
	client, server := netip.MustParseAddr(rigDevice), netip.MustParseAddr(rigOutside)
	now := time.Now()
	key := claimKey("www.ads.example", 0x4242)
	first, outcome := tbl.begin(key, client, server, 20000, now, time.Minute)
	if outcome != claimBeginPrimary {
		t.Fatalf("first copy: outcome %d", outcome)
	}
	for i := 1; i < dnsClaimKeyMax; i++ {
		if _, outcome := tbl.begin(key, client, server, uint16(20000+i), now, time.Minute); outcome != claimBeginPrimary {
			t.Fatalf("copy %d of the query: outcome %d", i, outcome)
		}
	}
	if _, outcome := tbl.begin(key, client, server, 30000, now, time.Minute); outcome != claimBeginKeyFull {
		t.Fatalf("a copy past %d claims of one query got outcome %d, want it refused for its query", dnsClaimKeyMax, outcome)
	}
	if c, outcome := tbl.begin(key, client, server, 20000, now, time.Minute); outcome != claimBeginDuplicate || c != first {
		t.Fatalf("a retransmission into a query at its limit got outcome %d, want it joined to its claim, which still settles the reply", outcome)
	}

	prev := dnsClaimMax
	dnsClaimMax = int(tbl.live.Load())
	t.Cleanup(func() { dnsClaimMax = prev })
	if c, outcome := tbl.begin(key, client, server, 20000, now, time.Minute); outcome != claimBeginDuplicate || c != first {
		t.Fatalf("a retransmission into a full table got outcome %d, want it joined to its claim rather than counted as refused", outcome)
	}
	if _, outcome := tbl.begin(claimKey("other.example", 1), client, server, 40000, now, time.Minute); outcome != claimBeginRefused {
		t.Fatalf("a new query into a full table got outcome %d, want it refused", outcome)
	}
}

func TestExpiredClaimsLeaveTheTableAsNewCopiesArrive(t *testing.T) {
	injectAfter(t, time.Minute)
	r := newCopyRig(t, nil)
	tbl := r.l.claims
	device, router := netip.MustParseAddr(rigDevice), netip.MustParseAddr(rigRouter)
	now := time.Now()
	ttl := 10 * time.Second
	for i := range 3 {
		c, outcome := tbl.begin(claimKey("ads.example", uint16(0x300+i)), device, router, uint16(41000+i), now.Add(time.Duration(i)*time.Second), ttl)
		if outcome != claimBeginPrimary {
			t.Fatalf("claim %d: outcome %d", i, outcome)
		}
		r.l.finish(&dnsCopyDecision{claim: c, kind: claimDrop})
	}
	pass := func(at time.Time, txid uint16) {
		t.Helper()
		c, outcome := tbl.begin(claimKey("other.example", txid), device, router, 42000, at, ttl)
		if outcome != claimBeginPrimary {
			t.Fatalf("a copy at %s got outcome %d", at.Sub(now), outcome)
		}
		r.l.finish(&dnsCopyDecision{claim: c})
	}

	pass(now.Add(ttl/2), 0x310)
	if live := tbl.live.Load(); live != 3 {
		t.Fatalf("%d live claims before any expired, want 3", live)
	}
	pass(now.Add(ttl+500*time.Millisecond), 0x311)
	if live, expired := tbl.live.Load(), tbl.expiredUnanswered.Load(); live != 2 || expired != 1 {
		t.Fatalf("live %d, expired_unanswered %d once the first claim expired, want 2 and 1 without waiting for the cleanup tick", live, expired)
	}
	pass(now.Add(ttl+2500*time.Millisecond), 0x312)
	if live, expired := tbl.live.Load(), tbl.expiredUnanswered.Load(); live != 0 || expired != 3 {
		t.Fatalf("live %d, expired_unanswered %d once every claim expired, want 0 and 3", live, expired)
	}
}

func TestOnlyKeptClaimsWaitInTheExpiryQueue(t *testing.T) {
	injectAfter(t, time.Minute)
	r := newCopyRig(t, nil)
	tbl := r.l.claims
	device, outside := netip.MustParseAddr(rigDevice), netip.MustParseAddr(rigOutside)
	now := time.Now()

	kept, _ := tbl.begin(claimKey("ads.example", 1), device, outside, 40000, now, time.Second)
	r.l.finish(&dnsCopyDecision{claim: kept, kind: claimDrop})
	for i := range 5000 {
		c, outcome := tbl.begin(claimKey("other.example", uint16(2+i)), device, outside, 40001, now, time.Second)
		if outcome != claimBeginPrimary {
			t.Fatalf("copy %d: outcome %d", i, outcome)
		}
		r.l.finish(&dnsCopyDecision{claim: c})
	}
	tbl.mu.Lock()
	queued := len(tbl.order) - tbl.head
	tbl.mu.Unlock()
	if queued != 1 {
		t.Fatalf("%d claims wait for expiry after 5000 passed copies and one kept claim, want only the kept one: a passed copy left there pins its memory for the whole TTL", queued)
	}

	tbl.begin(claimKey("other.example", 9), device, outside, 40002, now.Add(2*time.Second), time.Second)
	tbl.mu.Lock()
	queued, head := len(tbl.order)-tbl.head, tbl.head
	tbl.mu.Unlock()
	if queued != 0 || head != 0 || !kept.removed {
		t.Fatalf("after the kept claim expired: %d queued from head %d, removed %v", queued, head, kept.removed)
	}
}

func TestAnExpiredDeviceClaimStopsKeepingRouterLookupsQuiet(t *testing.T) {
	injectAfter(t, time.Minute)
	r := newCopyRig(t, nil)
	tbl := r.l.claims
	device, router := netip.MustParseAddr(rigDevice), netip.MustParseAddr(rigRouter)
	wan, upstream := netip.MustParseAddr(rigWAN), netip.MustParseAddr(rigUpstream)
	now := time.Now()
	ttl := 12 * time.Second

	c, _ := tbl.begin(claimKey("pin.example.com", 0x401), device, router, 40000, now, ttl)
	r.l.finish(&dnsCopyDecision{claim: c, kind: claimAnswer})
	if _, outcome := tbl.begin(claimKey("pin.example.com", 0x7401), wan, upstream, 50000, now.Add(time.Second), ttl); outcome != claimBeginQuiet {
		t.Fatalf("outcome %d: the router's upstream lookup while b4 still owes the device an answer must stay quiet", outcome)
	}
	if _, outcome := tbl.begin(claimKey("pin.example.com", 0x7402), wan, upstream, 50001, now.Add(ttl+2*time.Second), ttl); outcome != claimBeginPrimary {
		t.Fatalf("outcome %d: a device claim that expired unanswered still kept the router's own lookup of the name quiet, so it got the real address instead of the pin", outcome)
	}
}
