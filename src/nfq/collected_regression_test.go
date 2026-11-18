package nfq

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
)

func TestCollectedExactReplyAccounting(t *testing.T) {
	for _, name := range []string{"observed-acks", "unobserved-tls", "unobserved-ack", "observed-tls", "late-progress"} {
		t.Run(name, func(t *testing.T) {
			w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
			flow := silentFlow().flow
			flow.ID = 71
			lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return flow, true, nil }
			w.ProcessPacket(silentHelloFrom(42000))
			w.destState.watchMu.RLock()
			var key string
			for _, h := range w.destState.watches {
				key = h.replyKey
			}
			w.destState.watchMu.RUnlock()
			if key == "" {
				t.Fatal("missing watch")
			}
			tcp := make([]byte, TCPHeaderMinLen)
			tcp[12] = 0x50
			tcp[13] = 0x10
			for i := 0; i < 21; i++ {
				length := 52 + 4*(i%4)
				flow.ReplyPackets++
				flow.ReplyBytes += uint64(length)
				w.destState.NoteCollectedServerPacket(key, tcp, 0, length)
			}
			switch name {
			case "unobserved-tls", "observed-tls":
				flow.ReplyPackets++
				flow.ReplyBytes += 255
				if flow.ReplyBytes > 80*flow.ReplyPackets {
					t.Fatal("fixture must reproduce the old average-header false positive")
				}
				if name == "observed-tls" {
					w.destState.NoteCollectedServerPacket(key, tcp, 203, 255)
				}
			case "unobserved-ack":
				flow.ReplyPackets++
				flow.ReplyBytes += 52
			case "late-progress":
				p.collectedInstall = func(context.Context, *config.Config) error {
					w.destState.NoteCollectedServerPacket(key, tcp, 203, 255)
					flow.ReplyPackets++
					flow.ReplyBytes += 255
					return nil
				}
			}
			sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
			want := name == "observed-acks"
			if got := len(p.collected) == 1; got != want {
				t.Fatalf("candidate=%v want=%v", got, want)
			}
			if got := len(resetsTo(sent())) == 1; got != want {
				t.Fatalf("reset=%v want=%v", got, want)
			}
			if got := len(p.collectedPublished) == 1; got != want {
				t.Fatalf("published membership=%v want=%v", got, want)
			}
		})
	}
}

func TestCollectedDeadlineDispatchDoesNotWaitForLegacySweep(t *testing.T) {
	tracker := &destStateTracker{}
	now := time.Now()
	h := &helloWatch{collect: true, key: "flow", replyKey: "wire", deadline: now.Add(1500 * time.Millisecond)}
	tracker.WatchHello(h)
	if len(tracker.DueSilent(now.Add(2*time.Second))) != 0 {
		t.Fatal("legacy sweep consumed Collect")
	}
	if len(tracker.DueCollected(h.deadline.Add(-time.Nanosecond), 16)) != 0 {
		t.Fatal("deadline fired early")
	}
	if due := tracker.DueCollected(h.deadline, 16); len(due) != 1 || due[0].key != h.key {
		t.Fatalf("deadline missed: %+v", due)
	}
	if _, ok := tracker.NextCollectedDeadline(); ok {
		t.Fatal("due entry left scheduled")
	}
	tracker.EndWatch(*h)
	if tracker.Watching() {
		t.Fatal("finished watch leaked")
	}
}

func TestCollectedDeadlineHeapBoundedAndCancellationSafe(t *testing.T) {
	tracker := &destStateTracker{}
	now := time.Now()
	for i := 0; i < maxHelloWatches+10; i++ {
		key := fmt.Sprint(i)
		tracker.WatchHello(&helloWatch{collect: true, key: key, replyKey: "wire" + key, deadline: now.Add(time.Duration(i) * time.Millisecond)})
	}
	if len(tracker.collectDeadlines) != maxHelloWatches {
		t.Fatal("deadline heap exceeded watch bound")
	}
	for i := 0; i < maxHelloWatches; i += 2 {
		tracker.Unwatch(fmt.Sprint(i))
	}
	if len(tracker.collectDeadlines) != maxHelloWatches/2 {
		t.Fatal("cancellation left heap entries")
	}
	for i, h := range tracker.collectDeadlines {
		if h.collectIndex != i || tracker.watches[h.key] != h {
			t.Fatal("heap index/generation mismatch")
		}
	}
	due := tracker.DueCollected(now.Add(10*time.Second), maxSilenceLookups)
	if len(due) != maxSilenceLookups {
		t.Fatal("batch budget was not respected")
	}
	for _, h := range due {
		tracker.EndWatch(h)
	}
	if len(tracker.collectDeadlines) != maxHelloWatches/2-maxSilenceLookups {
		t.Fatal("due processing damaged pending heap")
	}
}

func TestCollectedDeadlineReplacementAndTimer(t *testing.T) {
	tracker := &destStateTracker{}
	tracker.WatchHello(&helloWatch{collect: true, key: "same", replyKey: "old", helloAck: 1, deadline: time.Now().Add(time.Hour)})
	next := &helloWatch{collect: true, key: "same", replyKey: "new", helloAck: 2, deadline: time.Now().Add(25 * time.Millisecond)}
	tracker.WatchHello(next)
	if len(tracker.collectDeadlines) != 1 || len(tracker.replyWatches) != 1 {
		t.Fatal("replacement leaked old watch")
	}
	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	ready := tracker.resetCollectedTimer(timer)
	select {
	case fired := <-ready:
		if fired.Before(next.deadline) {
			t.Fatal("timer fired before deadline")
		}
	case <-time.After(time.Second):
		t.Fatal("Collect deadline still waits for two-second sweep")
	}
	tracker.Unwatch(next.key)
	if tracker.resetCollectedTimer(timer) != nil {
		t.Fatal("empty queue left timer armed")
	}
}

func TestCollectedRecordedReplyPatterns(t *testing.T) {
	cases := []struct {
		name         string
		answered     bool
		payloadAt    int
		replyLengths []int
	}{
		{name: "capture-1", answered: true, payloadAt: 21, replyLengths: []int{60, 64, 64, 72, 72, 80, 80, 80, 80, 80, 80, 80, 72, 64, 72, 72, 72, 52, 64, 64, 255, 52, 52}},
		{name: "capture-2", answered: false, payloadAt: 0, replyLengths: []int{60}},
		{name: "capture-3", answered: true, payloadAt: 19, replyLengths: []int{60, 64, 64, 72, 64, 72, 52, 64, 64, 64, 64, 64, 64, 72, 72, 72, 64, 52, 255, 52, 52}},
		{name: "capture-4", answered: true, payloadAt: 15, replyLengths: []int{60, 64, 64, 72, 72, 80, 64, 72, 64, 64, 64, 64, 64, 52, 255, 64, 52, 52, 52}},
		{name: "capture-5", answered: false, payloadAt: 0, replyLengths: []int{60, 64, 64, 72, 72, 64, 64, 52, 52, 52}},
		{name: "capture-6", answered: false, payloadAt: 0, replyLengths: []int{60, 64, 64, 52, 64, 64, 72, 72, 72, 64, 52}},
		{name: "capture-7", answered: true, payloadAt: 16, replyLengths: []int{60, 64, 64, 72, 72, 80, 72, 80, 72, 80, 80, 72, 72, 64, 52, 255, 64, 64, 64, 64, 64, 52, 52, 52, 52}},
		{name: "capture-8", answered: true, payloadAt: 18, replyLengths: []int{60, 64, 64, 64, 72, 80, 80, 64, 64, 64, 72, 64, 64, 64, 64, 64, 52, 255, 52, 52, 52, 52, 52}},
		{name: "capture-9", answered: false, payloadAt: 0, replyLengths: []int{60, 64, 64, 72, 72, 72, 72, 72, 72, 64, 52}},
	}
	for _, fixture := range cases {
		t.Run(fixture.name, func(t *testing.T) {
			w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
			flow := silentFlow().flow
			flow.ID, flow.ReplyPackets, flow.ReplyBytes = 71, 1, uint64(fixture.replyLengths[0])
			lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return flow, true, nil }
			w.ProcessPacket(silentHelloFrom(42000))
			var key string
			w.destState.watchMu.RLock()
			for _, watch := range w.destState.watches {
				key = watch.replyKey
			}
			w.destState.watchMu.RUnlock()
			tcp := make([]byte, TCPHeaderMinLen)
			tcp[12] = 0x50
			tcp[13] = 0x10
			for i, length := range fixture.replyLengths[1:] {
				flow.ReplyPackets++
				flow.ReplyBytes += uint64(length)
				payload := 0
				if i+2 == fixture.payloadAt {
					payload = 203
				}
				if flow.ReplyPackets < 20 {
					w.destState.NoteCollectedServerPacket(key, tcp, payload, length)
				}
			}
			if fixture.name == "capture-2" {
				w.sweepCollectedHellos(w.getConfig(), time.Now().Add(4*time.Second))
			} else {
				sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
			}
			want := !fixture.answered && fixture.name != "capture-2"
			if got := len(p.collected) == 1; got != want {
				t.Fatalf("candidate=%v want=%v", got, want)
			}
			if got := len(resetsTo(sent())) == 1; got != want {
				t.Fatalf("reset=%v want=%v", got, want)
			}
		})
	}
}

func TestCollectedConcurrentDeadlineCancellation(t *testing.T) {
	tracker := &destStateTracker{}
	wake := tracker.CollectedWake()
	finished := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		timer := time.NewTimer(time.Hour)
		defer timer.Stop()
		for {
			due := tracker.resetCollectedTimer(timer)
			select {
			case <-finished:
				return
			case <-wake:
			case now := <-due:
				for _, watch := range tracker.DueCollected(now, maxSilenceLookups) {
					tracker.EndWatch(watch)
				}
			}
		}
	}()
	var workers sync.WaitGroup
	for worker := 0; worker < 4; worker++ {
		workers.Add(1)
		go func(worker int) {
			defer workers.Done()
			tcp := make([]byte, TCPHeaderMinLen)
			tcp[12] = 0x50
			tcp[13] = 0x10
			for i := 0; i < 512; i++ {
				key := fmt.Sprintf("worker-%d-%d", worker, i%32)
				wire := "wire-" + key
				tracker.WatchHello(&helloWatch{collect: true, key: key, replyKey: wire, helloAck: uint32(i + 1), deadline: time.Now().Add(time.Duration(i%3) * time.Microsecond)})
				tracker.NoteCollectedServerPacket(wire, tcp, i%2, 52+i%2)
				if i%3 == 0 {
					tracker.Unwatch(key)
				}
			}
		}(worker)
	}
	workers.Wait()
	close(finished)
	<-done
	tracker.watchMu.Lock()
	keys := make([]string, 0, len(tracker.watches))
	for key := range tracker.watches {
		keys = append(keys, key)
	}
	tracker.watchMu.Unlock()
	for _, key := range keys {
		tracker.Unwatch(key)
	}
	if tracker.Watching() || len(tracker.collectDeadlines) != 0 || len(tracker.replyWatches) != 0 {
		t.Fatal("concurrent cancellation leaked watches or deadlines")
	}
}
