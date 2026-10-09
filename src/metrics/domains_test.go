package metrics

import (
	"fmt"
	"net/netip"
	"slices"
	"testing"
	"time"
)

func topDomain(t testing.TB, fr Frame, key string) TopEntry {
	t.Helper()
	if fr.TopDomains == nil {
		t.Fatal("frame has no top_domains")
	}
	for _, d := range fr.TopDomains.Items {
		if d.Key == key {
			return d
		}
	}
	t.Fatalf("%q missing from top_domains: %+v", key, fr.TopDomains.Items)
	return TopEntry{}
}

func TestTopDomainsCountEachFlowOnce(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m

	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "")
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "video", "video.example")
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "video", "video.example")
	m.ObserveFlowTo(flowKey(2), netip.Addr{}, "video", "video.example")
	m.ObserveFlowTo(flowKey(3), netip.Addr{}, "", "other.example")
	m.ObserveFlowTo(flowKey(3), netip.Addr{}, "", "other.example")

	fr := m.Hello()
	if d := topDomain(t, fr, "video.example"); d.Count != 2 || !slices.Equal(d.Sets, []string{"video"}) || d.Last != ms(rigStart) {
		t.Fatalf("a flow is named once, on the first packet that carries its host: %+v", d)
	}
	if d := topDomain(t, fr, "other.example"); d.Count != 1 || d.Sets != nil {
		t.Fatalf("a flow outside every set has no set: %+v", d)
	}
	if fr.Totals.Conns != 3 {
		t.Fatalf("naming a flow does not count it again: %+v", fr.Totals)
	}
}

func TestTopDomainsTakeTheSetTheFlowWasCountedIn(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m

	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "by-ip", "")
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "cdn.example")

	if d := topDomain(t, m.Hello(), "cdn.example"); !slices.Equal(d.Sets, []string{"by-ip"}) {
		t.Fatalf("the domain follows the set the flow was classified into: %+v", d)
	}
}

func TestTopDomainsListRecentSetsFirst(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	for i, set := range []string{"a", "b", "a", "", "c", "d", "c"} {
		m.ObserveFlowTo(flowKey(i+1), netip.Addr{}, set, "multi.example")
	}

	d := topDomain(t, m.Hello(), "multi.example")
	if d.Count != 7 || !slices.Equal(d.Sets, []string{"c", "d", "a"}) {
		t.Fatalf("distinct sets, most recent first, capped at %d; a flow outside every set keeps them: %+v", TopSetsKept, d)
	}
}

func TestTopDomainsSendTheMostConnectedFirst(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	for i := 0; i < TopDomainsSent+5; i++ {
		for n := 0; n <= i%4; n++ {
			m.RecordDomain(fmt.Sprintf("d%02d.example", i), "")
		}
	}

	items := m.Hello().TopDomains.Items
	if len(items) != TopDomainsSent {
		t.Fatalf("sent %d domains, want %d", len(items), TopDomainsSent)
	}
	if items[0].Key != "d23.example" || items[0].Count != 4 || items[1].Key != "d19.example" {
		t.Fatalf("highest count first, the most recent first among equals: %+v %+v", items[0], items[1])
	}
	for i := 1; i < len(items); i++ {
		if items[i].Count > items[i-1].Count {
			t.Fatalf("not sorted by count at %d: %+v", i, items)
		}
	}
}

func TestTopDomainsEvictTheLowestCountSeenLongestAgo(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	m.RecordDomain("busy.example", "")
	m.RecordDomain("busy.example", "")
	for i := 1; i < TopDomainsKept; i++ {
		m.RecordDomain(fmt.Sprintf("once%03d.example", i), "")
	}
	m.RecordDomain("busy.example", "")
	m.RecordDomain("new.example", "")

	m.dom.mu.Lock()
	kept := len(m.dom.items)
	_, busy := m.dom.items["busy.example"]
	_, oldest := m.dom.items["once001.example"]
	_, next := m.dom.items["once002.example"]
	_, fresh := m.dom.items["new.example"]
	m.dom.mu.Unlock()
	if kept != TopDomainsKept || !busy || oldest || !next || !fresh {
		t.Fatalf("kept=%d busy=%v once001=%v once002=%v new=%v", kept, busy, oldest, next, fresh)
	}
}

func TestTopDomainsNormaliseAndSkipAddresses(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	for _, host := range []string{"Video.Example.", "video.example", "", ".", "93.184.216.34", "2001:db8::1", "[2001:db8::1]"} {
		m.RecordDomain(host, "")
	}

	items := m.Hello().TopDomains.Items
	if len(items) != 1 || items[0].Key != "video.example" || items[0].Count != 2 {
		t.Fatalf("one lower-case name without the root dot, addresses skipped: %+v", items)
	}
}

func TestTopDomainsResetWithTheCounters(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "")
	m.RecordDomain("video.example", "video")
	before := m.Hello().TopDomains

	m.ResetCounters()
	after := m.Hello().TopDomains
	if len(after.Items) != 0 || after.Rev <= before.Rev {
		t.Fatalf("cleared with a new rev: %+v (was %+v)", after, before)
	}

	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "late.example")
	if items := m.Hello().TopDomains.Items; len(items) != 0 {
		t.Fatalf("a flow counted before the reset is not named after it: %+v", items)
	}
}

func TestTickCarriesTopDomainsOnlyWhenTheyMoved(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	var sent SentRevs

	if fr := m.Tick(&sent); fr.TopDomains != nil {
		t.Fatal("nothing named since rev 0")
	}
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "video.example")
	fr := m.Tick(&sent)
	if fr.TopDomains == nil || sent.TopDomains != fr.TopDomains.Rev {
		t.Fatalf("a new name moves the list: %+v %+v", fr.TopDomains, sent)
	}
	m.ObserveFlowTo(flowKey(1), netip.Addr{}, "", "video.example")
	r.step(time.Second)
	if fr = m.Tick(&sent); fr.TopDomains != nil {
		t.Fatal("a flow already named does not move the list")
	}
}

func TestNamingAKnownDomainDoesNotAllocate(t *testing.T) {
	r := newRig(t, rigStart, 0)
	r.m.RecordDomain("video.example", "video")
	if n := testing.AllocsPerRun(200, func() { r.m.RecordDomain("video.example", "video") }); n != 0 {
		t.Fatalf("a known domain costs %v allocations", n)
	}
}
