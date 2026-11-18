package nfq

import (
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/ctnl"
)

func historyWatch(key string, id uint32) *helloWatch {
	return &helloWatch{collect: true, key: key, replyKey: key, flowID: id, client: net.ParseIP("10.0.0.1"), dst: net.ParseIP("1.2.3.4"), cport: 42000, port: 443, deadline: time.Now().Add(time.Second)}
}

func TestCollectedHistoryNewConnectionCanWatch(t *testing.T) {
	tracker := &destStateTracker{}
	old := historyWatch("same-tuple", 71)
	tracker.WatchHello(old)
	tracker.Unwatch(old.key)
	tracker.WatchHello(historyWatch(old.key, 71))
	if tracker.Watching() {
		t.Fatal("same conntrack connection rearmed")
	}
	tracker.ForgetConnection(old.key)
	tracker.WatchHello(historyWatch(old.key, 71))
	if tracker.Watching() {
		t.Fatal("forgetting local state erased live conntrack history")
	}
	tracker.WatchHello(historyWatch(old.key, 72))
	if !tracker.Watching() || tracker.watches[old.key].flowID != 72 || tracker.collectHistory.order.Len() != 1 {
		t.Fatal("replacement conntrack connection could not be observed")
	}
	tracker.WatchHello(historyWatch("other-tuple", 73))
	if len(tracker.watches) != 2 {
		t.Fatal("another connection to the same IP was suppressed")
	}
}

func TestCollectedHistoryBoundAndRoundRobinCleanup(t *testing.T) {
	tracker := &destStateTracker{}
	for i := range maxHelloWatches {
		h := historyWatch(fmt.Sprint(i), uint32(i+1))
		tracker.WatchHello(h)
		tracker.Unwatch(h.key)
	}
	tracker.WatchHello(historyWatch("overflow", 10000))
	if tracker.Watching() || tracker.collectHistory.order.Len() != maxHelloWatches {
		t.Fatal("full history evicted evidence or admitted another watch")
	}
	old := lookupFlow
	t.Cleanup(func() { lookupFlow = old })
	lookups := 0
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) {
		lookups++
		return ctnl.Flow{}, false, nil
	}
	tracker.cleanupCollectedHistory()
	if lookups != maxSilenceLookups || len(tracker.collectHistory.flows) != maxHelloWatches-maxSilenceLookups {
		t.Fatalf("cleanup exceeded budget or failed: lookups=%d remaining=%d", lookups, len(tracker.collectHistory.flows))
	}
	tracker.WatchHello(historyWatch("overflow", 10000))
	if !tracker.Watching() {
		t.Fatal("confirmed dead records did not release capacity")
	}
}

func TestCollectedHistoryRetainsLiveAndUnknownRecords(t *testing.T) {
	for _, mode := range []string{"live", "error", "no-id", "no-state", "closed", "replaced"} {
		t.Run(mode, func(t *testing.T) {
			tracker := &destStateTracker{}
			tracker.WatchHello(historyWatch("flow", 71))
			tracker.Unwatch("flow")
			old := lookupFlow
			t.Cleanup(func() { lookupFlow = old })
			lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) {
				flow := ctnl.Flow{ID: 71, HasState: true, TCPState: ctnl.TCPEstablished}
				switch mode {
				case "error":
					return ctnl.Flow{}, false, errors.New("unavailable")
				case "no-id":
					flow.ID = 0
				case "no-state":
					flow.HasState = false
				case "closed":
					flow.TCPState = 7
				case "replaced":
					flow.ID = 72
				}
				return flow, true, nil
			}
			tracker.Cleanup(0)
			tracker.cleanupCollectedHistory()
			retained := len(tracker.collectHistory.flows) == 1
			if retained != (mode != "closed" && mode != "replaced") {
				t.Fatalf("retained=%v for %s", retained, mode)
			}
		})
	}
}

func TestCollectedHistoryCleanupCannotRemoveReplacement(t *testing.T) {
	tracker := &destStateTracker{}
	tracker.WatchHello(historyWatch("flow", 71))
	tracker.Unwatch("flow")
	old := lookupFlow
	t.Cleanup(func() { lookupFlow = old })
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) {
		tracker.WatchHello(historyWatch("flow", 72))
		tracker.Unwatch("flow")
		return ctnl.Flow{}, false, nil
	}
	tracker.cleanupCollectedHistory()
	entry := tracker.collectHistory.flows["flow"]
	if entry == nil || entry.Value.(*helloWatch).flowID != 72 {
		t.Fatal("late cleanup removed the replacement connection")
	}
}

func TestCollectedHistoryConcurrentObservationAndCleanup(t *testing.T) {
	tracker := &destStateTracker{}
	old := lookupFlow
	t.Cleanup(func() { lookupFlow = old })
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return ctnl.Flow{}, false, nil }
	var workers sync.WaitGroup
	for worker := range 4 {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			for i := range 256 {
				h := historyWatch(fmt.Sprintf("%d/%d", worker, i%16), uint32(worker*1000+i+1))
				tracker.WatchHello(h)
				tracker.Unwatch(h.key)
				tracker.cleanupCollectedHistory()
			}
		}(worker)
	}
	workers.Wait()
	for tracker.collectHistory.order.Len() > 0 {
		tracker.cleanupCollectedHistory()
	}
	if tracker.Watching() || len(tracker.collectHistory.flows) != 0 || len(tracker.collectDeadlines) != 0 {
		t.Fatal("history/watch/deadline records leaked")
	}
}

func TestCollectedHistoryLookupErrorStopsBatch(t *testing.T) {
	tracker := &destStateTracker{}
	for i := range 20 {
		h := historyWatch(fmt.Sprint(i), uint32(i+1))
		tracker.WatchHello(h)
		tracker.Unwatch(h.key)
	}
	old := lookupFlow
	t.Cleanup(func() { lookupFlow = old })
	calls := 0
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) {
		calls++
		return ctnl.Flow{}, false, errors.New("unavailable")
	}
	tracker.cleanupCollectedHistory()
	if calls != 1 || len(tracker.collectHistory.flows) != 20 {
		t.Fatalf("failed conntrack retried or lost evidence: calls=%d records=%d", calls, len(tracker.collectHistory.flows))
	}
}
