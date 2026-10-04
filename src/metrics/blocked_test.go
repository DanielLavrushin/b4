package metrics

import (
	"fmt"
	"testing"
	"time"
)

func TestBlockedListEvictsTheEntrySeenLongestAgo(t *testing.T) {
	r := newRig(t, rigStart, 0)
	m := r.m
	for i := 0; i < 100; i++ {
		m.RecordBlockedDNS("ads", "d001", "")
	}
	m.RecordBlockedDNS("ads", "d000", "")
	for i := 2; i < BlockedDomainsKept; i++ {
		m.RecordBlockedDNS("ads", fmt.Sprintf("d%03d", i), "")
	}
	r.step(time.Second)
	m.RecordBlockedDNS("ads", "d000", "")
	m.RecordBlockedDNS("ads", "d100", "")

	domains := m.Hello().Blocked.Domains
	if len(domains) != BlockedDomainsKept {
		t.Fatalf("kept %d domains, want %d", len(domains), BlockedDomainsKept)
	}
	seen := map[string]BlockedEntry{}
	for _, d := range domains {
		seen[d.Key] = d
	}
	if _, ok := seen["d001"]; ok {
		t.Fatal("d001 was seen longest ago and must be evicted, whatever its count")
	}
	if seen["d000"].Count != 2 || seen["d100"].Count != 1 {
		t.Fatalf("d000=%+v d100=%+v", seen["d000"], seen["d100"])
	}
	if domains[0].Key != "d100" || domains[1].Key != "d000" || domains[2].Key != "d099" {
		t.Fatalf("newest first: %v %v %v", domains[0].Key, domains[1].Key, domains[2].Key)
	}
	if domains[1].Last != ms(rigStart.Add(time.Second)) || domains[2].Last != ms(rigStart) {
		t.Fatalf("last seen %d / %d", domains[1].Last, domains[2].Last)
	}
}

func TestBlockedDevicesCapAndKey(t *testing.T) {
	r := newRig(t, rigStart, 0)
	for i := 0; i < BlockedDevicesKept+5; i++ {
		r.m.RecordBlockedDNS("ads", "ads.example", fmt.Sprintf("02:00:00:00:00:%02x", i))
	}
	bl := r.m.Hello().Blocked
	if len(bl.Devices) != BlockedDevicesKept || bl.Devices[0].Key != fmt.Sprintf("02:00:00:00:00:%02x", BlockedDevicesKept+4) {
		t.Fatalf("devices %d, first %+v", len(bl.Devices), bl.Devices[0])
	}
	if len(bl.Domains) != 1 || bl.Domains[0].Count != BlockedDevicesKept+5 {
		t.Fatalf("domains %+v", bl.Domains)
	}
	if bl.Rev != BlockedDevicesKept+5 {
		t.Fatalf("rev %d", bl.Rev)
	}
}

func TestBlockedNoteWithoutKeysLeavesTheListsAlone(t *testing.T) {
	r := newRig(t, rigStart, 0)
	r.m.RecordBlockedDNS("", "", "")
	fr := r.m.Hello()
	if fr.Blocked.Rev != 0 || fr.Totals.BlockedDNS != 1 {
		t.Fatalf("rev %d totals %+v", fr.Blocked.Rev, fr.Totals)
	}
}
