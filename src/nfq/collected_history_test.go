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

func historyRecorded(t *destStateTracker, key string) bool {
	t.watchMu.RLock()
	defer t.watchMu.RUnlock()
	return t.collectHistory.flows[key] != nil
}

func TestAnAnsweredFlowLeavesTheCollectHistory(t *testing.T) {
	for name, answer := range map[string]func(*testing.T, *collectCountRig){
		"the server sent data the queue saw": func(t *testing.T, r *collectCountRig) {
			r.hello(t, 42000)
			r.serverSends(t, 42000, 0x18, []byte{0x16, 3, 3, 0, 1, 2})
		},
		"the client acknowledged server bytes": func(t *testing.T, r *collectCountRig) {
			r.silent(t, 42000)
			r.w.ProcessPacket(clientPacketAcking(42000, afterHello(), silentServerAck+1400, 0x10, nil))
		},
		"conntrack shows server bytes the queue did not see": func(t *testing.T, r *collectCountRig) {
			r.silent(t, 42000)
			f := r.flow(t, 42000)
			f.ReplyPackets++
			f.ReplyBytes += 1400
			r.sweep(5 * time.Second)
		},
	} {
		t.Run(name, func(t *testing.T) {
			r := newCollectCountRig(t)
			r.silent(t, 42001)
			r.sweep(4 * time.Second)
			answer(t, r)
			r.sweep(6 * time.Second)
			answered, waiting := fmt.Sprintf(connKeyFormat, "10.0.0.1", 42000, "1.2.3.4", 443), fmt.Sprintf(connKeyFormat, "10.0.0.1", 42001, "1.2.3.4", 443)
			if r.watched(42000) {
				t.Fatal("setup: the answered connection must stop being watched")
			}
			if historyRecorded(r.w.destState, answered) {
				t.Fatal("a connection its server answered still holds a history slot, until a conntrack cleanup reaches it")
			}
			if !r.watched(42001) || !historyRecorded(r.w.destState, waiting) {
				t.Fatal("a silent connection that waits for the second one must keep its record, or a second watch could count it twice")
			}
		})
	}
}

func TestAFullHistoryFreesSpaceAsServersAnswer(t *testing.T) {
	tracker := &destStateTracker{}
	tcp := make([]byte, TCPHeaderMinLen)
	tcp[12], tcp[13] = 0x50, 0x18
	for i := range maxHelloWatches {
		tracker.WatchHello(historyWatch(fmt.Sprint(i), uint32(i+1)))
	}
	if len(tracker.watches) != maxHelloWatches || tracker.collectHistory.order.Len() != maxHelloWatches {
		t.Fatalf("setup: %d watches and %d records, want %d of each", len(tracker.watches), tracker.collectHistory.order.Len(), maxHelloWatches)
	}
	for i := range maxHelloWatches {
		tracker.NoteCollectedServerPacket(fmt.Sprint(i), tcp, 10, TCPHeaderMinLen+10)
	}
	if tracker.Watching() {
		t.Fatal("setup: every answered connection must stop being watched")
	}
	tracker.WatchHello(historyWatch("new", 100000))
	if !tracker.Watching() {
		t.Fatalf("a new connection of a Collect set is refused while the history holds %d answered connections", tracker.collectHistory.order.Len())
	}
}

func TestAHelloRetryIsWatchedAndCountedAtMostOnce(t *testing.T) {
	r := newCollectCountRig(t)
	retry := silentHello()
	hrr := []byte{0x16, 3, 3, 0, 4, 2, 0, 0, 0}
	key := fmt.Sprintf(connKeyFormat, "10.0.0.1", 42000, "1.2.3.4", 443)
	acks := func(seq uint32, flags byte) {
		raw := collectedReply(42000, seq, flags, nil)
		f := r.flow(t, 42000)
		f.ReplyPackets++
		f.ReplyBytes += uint64(len(raw))
		r.w.ProcessPacket(raw)
	}

	r.hello(t, 42000)
	r.serverSends(t, 42000, 0x18, hrr)
	if r.watched(42000) {
		t.Fatal("setup: the HelloRetryRequest must end the watch of the first ClientHello")
	}
	second := afterHello()
	r.w.ProcessPacket(clientPacketAcking(42000, second, silentServerAck+uint32(len(hrr)), 0x18, retry))
	if !r.watched(42000) {
		t.Fatal("the second ClientHello after a HelloRetryRequest must be watched once more")
	}
	watched := r.watchOf(t, 42000)
	acks(second+uint32(len(retry)), 0x10)
	count := func() int {
		r.w.destState.watchMu.RLock()
		defer r.w.destState.watchMu.RUnlock()
		return r.w.destState.collectRuns["1.2.3.4:443"].count
	}
	r.sweep(4 * time.Second)
	if n := count(); n != 1 || r.collected() != 0 {
		t.Fatalf("the silent second ClientHello must count once, as the first of two: count=%d candidates=%d", n, r.collected())
	}
	if h := r.watchOf(t, 42000); h.gen != watched.gen || !h.parked {
		t.Fatal("the counted second ClientHello must wait for a second silent connection")
	}
	if !historyRecorded(r.w.destState, key) {
		t.Fatal("the silent connection lost its history record, so a further ClientHello on it could be counted again")
	}
}
