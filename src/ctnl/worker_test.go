package ctnl

import (
	"strings"
	"testing"
	"time"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func startWorker(t *testing.T) *Worker {
	t.Helper()
	w := NewWorker()
	t.Cleanup(w.Stop)
	return w
}

func deletesSent(k *fakeKernel) []netlink.Message {
	var out []netlink.Message
	for _, m := range k.requests() {
		if m.Header.Type == msgDelete {
			out = append(out, m)
		}
	}
	return out
}

func TestEveryRequestCarriesATupleAndTheFlushGuard(t *testing.T) {
	k := useFakeKernel(t, answer(unix.ENOENT, 0))
	w := startWorker(t)
	tuples := []struct {
		tuple Tuple
		dir   Direction
	}{
		{mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4), Original},
		{mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4).Reverse(), Reply},
		{mustTuple(t, unix.IPPROTO_UDP, clientV6, serverV6), Original},
		{mustTuple(t, unix.IPPROTO_TCP, clientV6, serverV6).Reverse(), Reply},
	}
	for _, c := range tuples {
		if !w.Enqueue(c.tuple, c.dir) {
			t.Fatalf("Enqueue(%s, %s) refused", c.tuple, c.dir)
		}
	}
	waitFor(t, "four deletes", func() bool { return Stats().Deleted == 4 })
	dels := deletesSent(k)
	if len(dels) != len(tuples) {
		t.Fatalf("%d deletes reached the kernel, want %d", len(dels), len(tuples))
	}
	for i, m := range k.requests() {
		d := decodeRequest(t, m)
		if d.which != attrTupleOrig && d.which != attrTupleReply {
			t.Fatalf("request %d carries no tuple", i)
		}
		if d.mark != flushGuardMark || d.markMask != flushGuardMask {
			t.Fatalf("request %d lacks the flush guard", i)
		}
	}
	for i, m := range dels {
		d := decodeRequest(t, m)
		want := tuples[i]
		if d.which != uint16(want.dir) || d.sport != want.tuple.src.Port() || d.dport != want.tuple.dst.Port() || d.proto != want.tuple.proto {
			t.Errorf("delete %d = %+v, want %s by %s", i, d, want.tuple, want.dir)
		}
		if m.Header.Flags != netlink.Request|netlink.Acknowledge {
			t.Errorf("delete %d flags %v, want NLM_F_REQUEST|NLM_F_ACK", i, m.Header.Flags)
		}
	}
}

func TestEnqueueRefusesWhatCannotEncode(t *testing.T) {
	k := useFakeKernel(t, answer(unix.ENOENT, 0))
	w := startWorker(t)
	tuple := mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4)
	for _, c := range []struct {
		name  string
		tuple Tuple
		dir   Direction
	}{
		{"the zero tuple", Tuple{}, Original},
		{"the zero tuple as a reply", Tuple{}, Reply},
		{"no direction", tuple, 0},
		{"CTA_TUPLE_MASTER", tuple, 14},
	} {
		if w.Enqueue(c.tuple, c.dir) {
			t.Errorf("%s: accepted", c.name)
		}
	}
	if !w.Enqueue(tuple, Original) {
		t.Fatal("a complete tuple was refused")
	}
	waitFor(t, "the one valid delete", func() bool { return Stats().Deleted == 1 })
	if got := Stats().Invalid; got != 4 {
		t.Fatalf("invalid = %d, want 4", got)
	}
	if n := len(deletesSent(k)); n != 1 {
		t.Fatalf("%d deletes reached the kernel, want only the complete one", n)
	}
	var nilWorker *Worker
	if nilWorker.Enqueue(tuple, Original) {
		t.Fatal("a nil worker accepted a job")
	}
	nilWorker.Stop()
}

func TestWorkerClassifiesTheKernelAnswer(t *testing.T) {
	logs := captureLog(t)
	k := useFakeKernel(t, answer(unix.ENOENT, unix.ENOENT))
	w := startWorker(t)
	tuple := mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4)
	send := func(reply unix.Errno, want func(c Counters) bool, what string) {
		t.Helper()
		k.setReply(answer(unix.ENOENT, reply))
		if !w.Enqueue(tuple, Original) {
			t.Fatalf("%s: refused", what)
		}
		waitFor(t, what, func() bool { return want(Stats()) })
	}
	send(0, func(c Counters) bool { return c.Deleted == 1 }, "a delete")
	send(unix.ENOENT, func(c Counters) bool { return c.NotFound == 1 }, "an entry already gone")
	send(unix.EBUSY, func(c Counters) bool { return c.Busy == 1 }, "an offloaded entry")
	send(unix.EBUSY, func(c Counters) bool { return c.Busy == 2 }, "a second offloaded entry")
	send(unix.ENOMEM, func(c Counters) bool { return c.Failed == 1 }, "a failure")
	send(unix.ENOMEM, func(c Counters) bool { return c.Failed == 2 }, "a second failure")
	if !Available() {
		t.Fatal("EBUSY and ENOMEM must leave the feature on")
	}
	if n := strings.Count(logs.String(), "refused to delete the conntrack entry"); n != 1 {
		t.Fatalf("EBUSY logged %d times, want once:\n%s", n, logs.String())
	}
	if n := strings.Count(logs.String(), "failed:"); n != 1 {
		t.Fatalf("a failure logged %d times, want once:\n%s", n, logs.String())
	}
	send(unix.EINVAL, func(c Counters) bool { return c.Failed == 3 }, "a rejected delete")
	if Available() {
		t.Fatal("EINVAL must switch the feature off")
	}
	if st := CurrentStatus(); !strings.Contains(st.Reason, "rejected a conntrack delete") {
		t.Fatalf("reason %q, want the rejected delete", st.Reason)
	}
	before := len(deletesSent(k))
	if w.Enqueue(tuple, Original) {
		t.Fatal("a job was accepted after the feature went off")
	}
	if got := Stats().Unavailable; got != 1 {
		t.Fatalf("unavailable = %d, want 1", got)
	}
	if n := strings.Count(logs.String(), "Conntrack netlink is unavailable"); n != 1 {
		t.Fatalf("the switch-off was warned %d times, want once:\n%s", n, logs.String())
	}
	if !strings.Contains(logs.String(), "IP block detection checks such an address only once the device gives up") {
		t.Errorf("the warning must say what IP block detection loses without conntrack netlink:\n%s", logs.String())
	}
	time.Sleep(20 * time.Millisecond)
	if after := len(deletesSent(k)); after != before {
		t.Fatalf("%d deletes sent after the switch-off", after-before)
	}
}

func TestABestEffortDeleteIsSentLikeAnyOther(t *testing.T) {
	captureLog(t)
	k := useFakeKernel(t, answer(unix.ENOENT, 0))
	w := startWorker(t)
	tuple := mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4)
	if !w.EnqueueBestEffort(tuple, Original) {
		t.Fatal("refused")
	}
	waitFor(t, "the delete", func() bool { return Stats().Deleted == 1 })
	dels := deletesSent(k)
	if len(dels) != 1 {
		t.Fatalf("%d deletes reached the kernel, want 1", len(dels))
	}
	d := decodeRequest(t, dels[0])
	if d.which != attrTupleOrig || d.sport != tuple.src.Port() || d.dport != tuple.dst.Port() || d.mark != flushGuardMark || d.markMask != flushGuardMask {
		t.Fatalf("delete %+v, want %s by the original tuple with the flush guard", d, tuple)
	}
	if w.EnqueueBestEffort(Tuple{}, Original) || w.EnqueueBestEffort(tuple, 14) {
		t.Fatal("a best-effort job without a complete tuple was accepted")
	}

	k.setReply(answer(unix.ENOENT, unix.EBUSY))
	if !w.EnqueueBestEffort(tuple, Original) {
		t.Fatal("refused")
	}
	waitFor(t, "the offloaded entry", func() bool { return Stats().Busy == 1 })
	k.setReply(answer(unix.ENOENT, unix.ENOMEM))
	if !w.EnqueueBestEffort(tuple, Original) {
		t.Fatal("refused")
	}
	waitFor(t, "the failed delete", func() bool { return len(deletesSent(k)) == 3 })
	time.Sleep(20 * time.Millisecond)
	if c := Stats(); c.Failed != 0 || c.Invalid != 0 || c.Queued != 3 {
		t.Fatalf("counters %+v: a best-effort job backs up another hold, so neither its failure nor a refusal is a drop left unheld; an offloaded entry escapes every hold and is counted", c)
	}
	if !Available() {
		t.Fatal("ENOMEM must leave the feature on")
	}
}

func TestABestEffortJobIsNeverCountedAsUnavailable(t *testing.T) {
	captureLog(t)
	k := useFakeKernel(t, answer(unix.EINVAL, 0))
	w := startWorker(t)
	tuple := mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4)
	if !w.EnqueueBestEffort(tuple, Original) {
		t.Fatal("before the self-test ran the job must be queued")
	}
	waitFor(t, "the self-test", func() bool { return CurrentStatus().Tested })
	time.Sleep(20 * time.Millisecond)
	if w.EnqueueBestEffort(tuple, Original) {
		t.Fatal("a best-effort job was accepted after the self-test failed")
	}
	if got := Stats().Unavailable; got != 0 {
		t.Fatalf("unavailable = %d, want 0 for best-effort jobs", got)
	}
	if w.Enqueue(tuple, Original) {
		t.Fatal("a job was accepted after the self-test failed")
	}
	if got := Stats().Unavailable; got != 1 {
		t.Fatalf("unavailable = %d, want 1: a counted job still counts", got)
	}
	if n := len(deletesSent(k)); n != 0 {
		t.Fatalf("%d deletes sent although the self-test failed", n)
	}
}

func TestEOPNOTSUPPSwitchesTheFeatureOff(t *testing.T) {
	captureLog(t)
	useFakeKernel(t, answer(unix.ENOENT, unix.EOPNOTSUPP))
	w := startWorker(t)
	if !w.Enqueue(mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4), Original) {
		t.Fatal("refused")
	}
	waitFor(t, "the switch-off", func() bool { return !Available() })
}

func TestWorkerSkipsJobsWhileTheSelfTestFails(t *testing.T) {
	captureLog(t)
	k := useFakeKernel(t, answer(unix.EINVAL, 0))
	w := startWorker(t)
	if !w.Enqueue(mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4), Original) {
		t.Fatal("before the self-test ran the job must be queued")
	}
	waitFor(t, "the skipped job", func() bool { return Stats().Unavailable == 1 })
	if n := len(deletesSent(k)); n != 0 {
		t.Fatalf("%d deletes sent although the self-test failed", n)
	}
}

func TestWorkerRedialsAfterAFailure(t *testing.T) {
	captureLog(t)
	k := useFakeKernel(t, answer(unix.ENOENT, unix.ENOENT))
	w := startWorker(t)
	tuple := mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4)
	for i := 1; i <= 3; i++ {
		if !w.Enqueue(tuple, Original) {
			t.Fatal("refused")
		}
		waitFor(t, "a not-found delete", func() bool { return Stats().NotFound == uint64(i) })
	}
	if n := k.dialCount(); n != 2 {
		t.Fatalf("%d dials, want 2: one for the self-test, one kept for the deletes", n)
	}
	k.setReply(answer(unix.ENOENT, unix.ENOMEM))
	if !w.Enqueue(tuple, Original) {
		t.Fatal("refused")
	}
	waitFor(t, "the failure", func() bool { return Stats().Failed == 1 })
	k.setReply(answer(unix.ENOENT, 0))
	if !w.Enqueue(tuple, Original) {
		t.Fatal("refused")
	}
	waitFor(t, "the delete after the failure", func() bool { return Stats().Deleted == 1 })
	if n := k.dialCount(); n != 3 {
		t.Fatalf("%d dials, want 3: the socket that failed is not reused", n)
	}
}

func TestEnqueueNeverBlocks(t *testing.T) {
	k := useFakeKernel(t, answer(unix.ENOENT, 0))
	k.hold = make(chan struct{})
	w := startWorker(t)
	tuple := mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4)
	total := workerQueueLen + 50
	accepted := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < total; i++ {
			if w.Enqueue(tuple, Original) {
				accepted++
			}
		}
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(k.hold)
		t.Fatal("Enqueue blocked on a full queue")
	}
	if accepted < workerQueueLen || accepted > workerQueueLen+1 {
		t.Fatalf("accepted %d of %d with a stalled kernel, want the queue length %d (plus the job in hand)", accepted, total, workerQueueLen)
	}
	c := Stats()
	if c.Queued != uint64(accepted) || c.Dropped != uint64(total-accepted) {
		t.Fatalf("queued %d dropped %d, want %d and %d", c.Queued, c.Dropped, accepted, total-accepted)
	}
	close(k.hold)
	waitFor(t, "the backlog", func() bool { return Stats().Deleted == uint64(accepted) })
}

func TestStopJoinsTheWorker(t *testing.T) {
	k := useFakeKernel(t, answer(unix.ENOENT, 0))
	k.hold = make(chan struct{})
	w := NewWorker()
	if !w.Enqueue(mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4), Original) {
		t.Fatal("refused")
	}
	waitFor(t, "the worker to pick up the job", func() bool { return len(k.requests()) > 0 })
	stopped := make(chan struct{})
	go func() {
		w.Stop()
		close(stopped)
	}()
	select {
	case <-stopped:
		t.Fatal("Stop returned while the worker was still inside a request")
	case <-time.After(20 * time.Millisecond):
	}
	close(k.hold)
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return")
	}
	select {
	case <-w.done:
	default:
		t.Fatal("Stop returned before the worker goroutine ended")
	}
	w.Stop()
	w.Enqueue(mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4), Original)
}
