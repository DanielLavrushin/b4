package nfq

import (
	"encoding/binary"
	"math"
	"net"
	"runtime"
	"testing"
	"time"
)

func returnsWithin(d time.Duration, f func()) bool {
	done := make(chan struct{})
	go func() {
		f()
		close(done)
	}()
	_, ok := receive(done, d)
	return ok
}

func TestCollectedHooksSkipTheWatchLockWithOnlyBlockWatches(t *testing.T) {
	set := silenceSet()
	w := newInjectingWorker(t, &set)
	w.destState.WatchHello(&helloWatch{key: "10.0.0.1:42000->1.2.3.4:443", setID: set.Id, helloAck: 1, deadline: time.Now().Add(time.Minute)})
	raw := collectedReply(42000, silentServerAck, 0x10, nil)
	w.destState.watchMu.Lock()
	server := returnsWithin(time.Second, func() {
		w.HandleIncoming(&verdictCtx{}, IPv4, raw, 20, net.IP(raw[12:16]), "10.0.0.1", 42000, "1.2.3.4", 443, nil)
	})
	client := returnsWithin(time.Second, func() {
		w.destState.NoteCollectedClientPacket("10.0.0.1:42000->1.2.3.4:443", clientPacket(42000, 5000, 0x18, []byte{1, 2, 3})[20:])
	})
	w.destState.watchMu.Unlock()
	if !server || !client {
		t.Fatalf("collected hooks waited on the watch lock with only a Block watch: server=%v client=%v", server, client)
	}
}

func TestIncomingCollectHookAllocatesNothingWithoutACollectWatch(t *testing.T) {
	set := newPassiveSet()
	w := newInjectingWorker(t, &set)
	raw := collectedReply(42000, silentServerAck, 0x10, nil)
	src := net.IP(raw[12:16])
	call := func() { w.HandleIncoming(&verdictCtx{}, IPv4, raw, 20, src, "10.0.0.1", 42000, "1.2.3.4", 443, nil) }
	with := allocsPerCall(2000, call)
	saved := w.destState
	w.destState = nil
	without := allocsPerCall(2000, call)
	w.destState = saved
	if math.Abs(with-without) >= 0.5 {
		t.Fatalf("an incoming packet allocated %.2f times with the tracker and %.2f without it", with, without)
	}
}

func allocsPerCall(runs int, f func()) float64 {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	f()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range runs {
		f()
	}
	runtime.ReadMemStats(&after)
	return float64(after.Mallocs-before.Mallocs) / float64(runs)
}

func TestUnpinnedPacketsSkipThePinnedDecisionLookup(t *testing.T) {
	set := newPassiveSet()
	w := newInjectingWorker(t, &set)
	ack := clientPacket(42000, 5000, 0x10, nil)
	binary.BigEndian.PutUint16(ack[22:24], 8080)
	key := "10.0.0.1:42000->1.2.3.4:8080"
	w.tlsCache.Store(key, "example.com", 0x0304, 4000, 7, 100)
	old := time.Now().Add(-10 * time.Second)
	w.tlsCache.mu.Lock()
	w.tlsCache.conns[key].lastSeen = old
	w.tlsCache.mu.Unlock()
	w.ProcessPacket(ack)
	w.tlsCache.mu.RLock()
	got := w.tlsCache.conns[key].lastSeen
	w.tlsCache.mu.RUnlock()
	if !got.Equal(old) {
		t.Fatal("a packet with nothing pinned looked up the pinned decision")
	}
}
