package metrics

import (
	"net/netip"
	"slices"
	"testing"
	"time"
)

func topAddress(fr Frame, key string) (TopEntry, bool) {
	if fr.TopAddresses == nil {
		return TopEntry{}, false
	}
	for _, a := range fr.TopAddresses.Items {
		if a.Key == key {
			return a, true
		}
	}
	return TopEntry{}, false
}

var (
	telegramDC = netip.MustParseAddr("149.154.167.51")
	cdnAddr    = netip.MustParseAddr("142.250.74.14")
)

func TestTopAddressesCountConnectionsWithoutAName(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	m.ObserveFlowTo(flowKey(1), telegramDC, "tg", "")
	m.ObserveFlowTo(flowKey(1), telegramDC, "tg", "")
	m.ObserveFlowTo(flowKey(2), telegramDC, "", "")
	m.ObserveFlowTo(flowKey(3), cdnAddr, "", "")

	r.step(time.Second)
	r.step(time.Second)
	if items := m.Hello().TopAddresses.Items; len(items) != 0 {
		t.Fatalf("a connection waits %d ticks for its name before it counts: %+v", addrSettleTicks, items)
	}
	r.step(time.Second)

	fr := m.Hello()
	if a, ok := topAddress(fr, "149.154.167.51"); !ok || a.Count != 2 || !slices.Equal(a.Sets, []string{"tg"}) || a.Last != ms(rigStart) {
		t.Fatalf("two connections without a name, each once, with the set the first matched: %+v", fr.TopAddresses.Items)
	}
	if a, ok := topAddress(fr, "142.250.74.14"); !ok || a.Count != 1 || a.Sets != nil {
		t.Fatalf("a connection outside every set has no set: %+v", fr.TopAddresses.Items)
	}
	if fr.Totals.Conns != 3 {
		t.Fatalf("holding an address does not count a connection again: %+v", fr.Totals)
	}
}

func TestTopAddressesSkipConnectionsThatNameThemselves(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	m.ObserveFlowTo(flowKey(1), cdnAddr, "", "")
	r.step(time.Second)
	m.ObserveFlowTo(flowKey(1), cdnAddr, "", "cdn.example")
	r.stepEach(3*time.Second, time.Second)

	fr := m.Hello()
	if _, ok := topAddress(fr, "142.250.74.14"); ok {
		t.Fatalf("a connection that named itself in time is a domain, not an address: %+v", fr.TopAddresses.Items)
	}
	if d := topDomain(t, fr, "cdn.example"); d.Count != 1 {
		t.Fatalf("its domain counts: %+v", d)
	}
	m.addr.mu.Lock()
	left := len(m.addr.items)
	m.addr.mu.Unlock()
	if left != 0 {
		t.Fatalf("an address whose every connection named itself is forgotten, %d left", left)
	}

	m.ObserveFlowTo(flowKey(2), cdnAddr, "", "")
	r.stepEach(4*time.Second, time.Second)
	m.ObserveFlowTo(flowKey(2), cdnAddr, "", "late.example")
	if a, ok := topAddress(m.Hello(), "142.250.74.14"); !ok || a.Count != 1 {
		t.Fatalf("a name that arrives after the wait leaves the address counted: %+v", a)
	}
}

func TestTopAddressesKeepANewerConnectionWhenAnOldOneNamesItself(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	m.ObserveFlowTo(flowKey(1), cdnAddr, "", "")
	r.stepEach(64*time.Second, time.Second)
	m.ObserveFlowTo(flowKey(2), cdnAddr, "", "")
	m.ObserveFlowTo(flowKey(1), cdnAddr, "", "late.example")
	r.stepEach(3*time.Second, time.Second)
	if a, ok := topAddress(m.Hello(), "142.250.74.14"); !ok || a.Count != 2 {
		t.Fatalf("a name that arrives a minute later takes nothing from a newer connection: %+v", a)
	}
}

func TestTopAddressesRecordedByTheProxies(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	m.RecordAddress(netip.MustParseAddr("::ffff:149.154.167.51"), "tg")
	m.RecordAddress(telegramDC, "tg")
	m.RecordAddress(netip.Addr{}, "tg")

	items := m.Hello().TopAddresses.Items
	if len(items) != 1 || items[0].Key != "149.154.167.51" || items[0].Count != 2 || !slices.Equal(items[0].Sets, []string{"tg"}) {
		t.Fatalf("counted at once, IPv4-mapped addresses as IPv4: %+v", items)
	}
}

func TestTopAddressesEvictHeldBeforeCounted(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	for i := 0; i < TopAddressesKept-1; i++ {
		m.RecordAddress(netip.AddrFrom4([4]byte{10, 1, byte(i >> 8), byte(i)}), "")
	}
	m.ObserveFlowTo(flowKey(1), cdnAddr, "", "")
	m.ObserveFlowTo(flowKey(2), telegramDC, "", "")

	m.addr.mu.Lock()
	_, held := m.addr.items[cdnAddr]
	_, newest := m.addr.items[telegramDC]
	_, oldest := m.addr.items[netip.AddrFrom4([4]byte{10, 1, 0, 0})]
	kept := len(m.addr.items)
	m.addr.mu.Unlock()
	if kept != TopAddressesKept || held || !newest || !oldest {
		t.Fatalf("kept=%d held=%v newest=%v oldest=%v", kept, held, newest, oldest)
	}

	m.RecordAddress(netip.MustParseAddr("192.0.2.1"), "")
	m.addr.mu.Lock()
	_, newest = m.addr.items[telegramDC]
	_, oldest = m.addr.items[netip.AddrFrom4([4]byte{10, 1, 0, 0})]
	m.addr.mu.Unlock()
	if newest || !oldest {
		t.Fatalf("a held address goes before any counted one: newest=%v oldest=%v", newest, oldest)
	}
}

func TestTopAddressesSendTheMostConnectedFirst(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	for i := 0; i < TopAddressesSent+5; i++ {
		for n := 0; n <= i%4; n++ {
			m.RecordAddress(netip.AddrFrom4([4]byte{198, 51, 100, byte(i)}), "")
		}
	}
	items := m.Hello().TopAddresses.Items
	if len(items) != TopAddressesSent || items[0].Key != "198.51.100.23" || items[0].Count != 4 || items[1].Key != "198.51.100.19" {
		t.Fatalf("highest count first, the most recent first among equals: %d %+v", len(items), items[:2])
	}
}

func TestTopAddressesResetWithTheCounters(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	m.RecordAddress(telegramDC, "tg")
	m.ObserveFlowTo(flowKey(1), cdnAddr, "", "")
	before := m.Hello().TopAddresses

	m.ResetCounters()
	r.stepEach(4*time.Second, time.Second)
	after := m.Hello().TopAddresses
	if len(after.Items) != 0 || after.Rev <= before.Rev {
		t.Fatalf("cleared with a new rev, a connection held before the reset is not counted: %+v (was %+v)", after, before)
	}
}

func TestTickCarriesTopAddressesOnlyWhenTheyMoved(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	var sent SentRevs
	m.ObserveFlowTo(flowKey(1), telegramDC, "", "")
	for i := 1; i < addrSettleTicks; i++ {
		r.step(time.Second)
		if fr := m.Tick(&sent); fr.TopAddresses != nil {
			t.Fatalf("tick %d: a held connection does not move the list", i)
		}
	}
	r.step(time.Second)
	fr := m.Tick(&sent)
	if fr.TopAddresses == nil || sent.TopAddresses != fr.TopAddresses.Rev || len(fr.TopAddresses.Items) != 1 {
		t.Fatalf("the settled connection moves the list: %+v %+v", fr.TopAddresses, sent)
	}
	r.step(time.Second)
	if fr = m.Tick(&sent); fr.TopAddresses != nil {
		t.Fatal("nothing settled since the last tick")
	}
}

func TestHoldingAKnownAddressDoesNotAllocate(t *testing.T) {
	r := newRig(t, rigStart, 0)
	r.m.RecordAddress(telegramDC, "tg")
	n := testing.AllocsPerRun(200, func() {
		r.m.addr.hold(telegramDC, "tg", 1, 0)
		r.m.addr.drop(telegramDC, 1)
	})
	if n != 0 {
		t.Fatalf("a known address costs %v allocations", n)
	}
}

func TestTopAddressesWriteIPv6Compactly(t *testing.T) {
	r := newRig(t, rigStart, 0)
	r.m.RecordAddress(netip.MustParseAddr("2001:0db8:0000:0000:0000:0000:0000:0001"), "")
	if items := r.m.Hello().TopAddresses.Items; len(items) != 1 || items[0].Key != "2001:db8::1" {
		t.Fatalf("IPv6 in its short form: %+v", items)
	}
}
