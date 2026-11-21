package nfq

import (
	"fmt"
	"net"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/metrics"
)

func TestAFullPoolIsLoggedOnce(t *testing.T) {
	r := newCollectCountRig(t)
	logs := captureLog(t)
	warnings := func() int {
		return strings.Count(logs.String(), "new addresses are not collected until some expire or are removed")
	}
	r.p.configMu.Lock()
	r.p.collected = make(map[netip.Addr]collectedCandidate)
	for i := range maxCollectedIPs {
		r.p.collected[netip.AddrFrom4([4]byte{8, 1, byte(i >> 8), byte(i)})] = collectedCandidate{setID: "yt-images", port: 443, at: r.start}
	}
	r.p.configMu.Unlock()
	before := metrics.GetMetricsCollector().Hello().Collected.Rev

	r.silent(t, 42000)
	r.sweep(4 * time.Second)
	r.silent(t, 42001)
	r.sweep(5 * time.Second)
	r.silent(t, 42002)
	r.sweep(6 * time.Second)

	r.p.configMu.Lock()
	_, added := r.p.collected[netip.MustParseAddr("1.2.3.4")]
	refused := r.p.collectedRefused
	r.p.configMu.Unlock()
	if added || r.collected() != maxCollectedIPs {
		t.Fatalf("setup: a full pool must refuse the address: added=%v candidates=%d", added, r.collected())
	}
	if got := warnings(); got != 1 {
		t.Fatalf("two refused candidates logged %d warnings, want one per full state: %q", got, logs.String())
	}
	frame := metrics.GetMetricsCollector().Hello().Collected
	if refused != 2 || frame.Refused != 2 || frame.Rev <= before {
		t.Fatalf("the refusals must reach the dashboard frame with a new revision: pool refused=%d frame refused=%d rev %d after %d", refused, frame.Refused, frame.Rev, before)
	}

	if n, err := r.p.ClearCollectedIPs("8.1.0.0"); n != 1 || err != nil {
		t.Fatalf("setup: removing one address failed: %d %v", n, err)
	}
	r.silent(t, 42003)
	r.sweep(7 * time.Second)
	r.p.configMu.Lock()
	_, added = r.p.collected[netip.MustParseAddr("1.2.3.4")]
	r.p.configMu.Unlock()
	if !added {
		t.Fatal("setup: the address must be collected once the pool has room")
	}
	second := net.ParseIP("1.2.3.5")
	seedEarlierSilentConnection(r.w, second, 443)
	r.w.ProcessPacket(silentHelloTo(second, 42004))
	sweepAcknowledgedCollectedHellos(r.w, r.start.Add(8*time.Second))
	if got := warnings(); got != 2 {
		t.Fatalf("a pool that filled up again must warn again, got %d warnings", got)
	}
}

func TestAFullHistoryIsLoggedOnce(t *testing.T) {
	logs := captureLog(t)
	warnings := func() int {
		return strings.Count(logs.String(), "new connections of Collect sets are not watched until older ones close")
	}
	tracker := &destStateTracker{}
	remember := func(key string, id uint32) bool {
		h := historyWatch(key, id)
		tracker.WatchHello(h)
		watched := tracker.Watching()
		tracker.Unwatch(h.key)
		return watched
	}
	for i := range maxHelloWatches {
		remember(fmt.Sprint(i), uint32(i+1))
	}
	if remember("overflow-1", 10001) || remember("overflow-2", 10002) {
		t.Fatal("setup: a full history must refuse new connections")
	}
	if got := warnings(); got != 1 {
		t.Fatalf("two refused connections logged %d warnings, want one per full state", got)
	}
	old := lookupFlow
	t.Cleanup(func() { lookupFlow = old })
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return ctnl.Flow{}, false, nil }
	tracker.cleanupCollectedHistory()
	for i := range maxSilenceLookups {
		if !remember(fmt.Sprintf("refill-%d", i), uint32(20000+i)) {
			t.Fatalf("setup: the history must take connections again after a cleanup, refused the %d-th", i)
		}
	}
	if remember("overflow-3", 10003) {
		t.Fatal("setup: the refilled history must be full again")
	}
	if got := warnings(); got != 2 {
		t.Fatalf("a history that filled up again must warn again, got %d warnings", got)
	}
}

func TestAFullCountIsLoggedOnce(t *testing.T) {
	logs := captureLog(t)
	warnings := func() int {
		return strings.Count(logs.String(), "silent connections to other addresses are not counted until some counts end")
	}
	tracker := &destStateTracker{collectRuns: make(map[string]collectRun)}
	now := time.Now()
	for i := range maxCollectedRuns {
		tracker.collectRuns[fmt.Sprintf("8.1.%d.%d:443", i>>8, i&0xff)] = collectRun{count: 1, until: now.Add(time.Minute)}
	}
	count := func(key string) bool {
		tracker.watchMu.Lock()
		defer tracker.watchMu.Unlock()
		_, ok := tracker.countCollectRunLocked(key, false, now, now, time.Minute)
		return ok
	}
	if count("1.2.3.4:443") || count("1.2.3.5:443") {
		t.Fatal("setup: a full count map must refuse new addresses")
	}
	if got := warnings(); got != 1 {
		t.Fatalf("two refused addresses logged %d warnings, want one per full state", got)
	}
	tracker.watchMu.Lock()
	delete(tracker.collectRuns, "8.1.0.0:443")
	tracker.watchMu.Unlock()
	if !count("1.2.3.4:443") {
		t.Fatal("setup: a freed slot must take a new address")
	}
	if count("1.2.3.6:443") {
		t.Fatal("setup: the count map must be full again")
	}
	if got := warnings(); got != 2 {
		t.Fatalf("a count map that filled up again must warn again, got %d warnings", got)
	}
}
