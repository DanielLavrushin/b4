package nfq

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"syscall"
	"testing"

	"github.com/florianl/go-nfqueue"
	"github.com/mdlayher/netlink"
)

var (
	errKernelRefusesFlag = &netlink.OpError{Op: "receive", Err: syscall.EOPNOTSUPP}
	errQueuedPacketFirst = errors.New("netlink validate: mismatched sequence in netlink reply")
)

type queueBindRig struct {
	steps  []string
	opened []*nfqueue.Nfqueue
	closed map[*nfqueue.Nfqueue]bool
}

func stubQueueBinding(t *testing.T, failure func(attempt int) error) *queueBindRig {
	t.Helper()
	r := &queueBindRig{closed: map[*nfqueue.Nfqueue]bool{}}
	name := func(q *nfqueue.Nfqueue) string {
		for i, o := range r.opened {
			if o == q {
				return fmt.Sprintf("queue %d", i+1)
			}
		}
		return "an unknown queue"
	}
	prevOpen, prevRegister, prevClose, prevPause := queueOpen, queueRegister, queueClose, queueBindPause
	queueBindPause = 0
	queueOpen = func(_ *Worker, extraFlags uint32) (*nfqueue.Nfqueue, error) {
		q := &nfqueue.Nfqueue{}
		r.opened = append(r.opened, q)
		r.steps = append(r.steps, fmt.Sprintf("open %s, socket owner %v", name(q), extraFlags&nfqueue.NfQaCfgFlagUIDGid != 0))
		return q, nil
	}
	queueRegister = func(_ *Worker, q *nfqueue.Nfqueue, _ uint) error {
		r.steps = append(r.steps, "register "+name(q))
		return failure(len(r.opened))
	}
	queueClose = func(q *nfqueue.Nfqueue) error {
		r.steps = append(r.steps, "close "+name(q))
		r.closed[q] = true
		return nil
	}
	t.Cleanup(func() {
		queueOpen, queueRegister, queueClose, queueBindPause = prevOpen, prevRegister, prevClose, prevPause
	})
	return r
}

func failingOn(failures map[int]error) func(int) error {
	return func(attempt int) error { return failures[attempt] }
}

func bindWorker(t *testing.T) *Worker {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	return &Worker{qnum: 537, ctx: ctx, cancel: cancel}
}

func TestTheQueueAsksForTheSocketOwnerOfEachPacket(t *testing.T) {
	r := stubQueueBinding(t, failingOn(nil))
	q, err := bindWorker(t).bindQueue(0x8000)
	if err != nil {
		t.Fatalf("bindQueue: %v", err)
	}
	want := []string{"open queue 1, socket owner true", "register queue 1"}
	if fmt.Sprint(r.steps) != fmt.Sprint(want) || q != r.opened[0] {
		t.Fatalf("steps %q, want %q", r.steps, want)
	}
}

func TestAKernelThatRefusesTheSocketOwnerGetsTheQueueWithoutIt(t *testing.T) {
	r := stubQueueBinding(t, failingOn(map[int]error{1: errKernelRefusesFlag}))
	q, err := bindWorker(t).bindQueue(0x8000)
	if err != nil {
		t.Fatalf("bindQueue: %v", err)
	}
	want := []string{
		"open queue 1, socket owner true",
		"register queue 1",
		"close queue 1",
		"open queue 2, socket owner false",
		"register queue 2",
	}
	if fmt.Sprint(r.steps) != fmt.Sprint(want) {
		t.Fatalf("steps %q, want %q: the first queue holds the binding until it is closed, so it must close before the second registers", r.steps, want)
	}
	if q != r.opened[1] {
		t.Fatal("bindQueue kept the queue whose registration failed")
	}
}

func TestAPacketQueuedDuringTheBindKeepsTheSocketOwner(t *testing.T) {
	r := stubQueueBinding(t, failingOn(map[int]error{1: errQueuedPacketFirst, 2: errQueuedPacketFirst}))
	q, err := bindWorker(t).bindQueue(0x8000)
	if err != nil {
		t.Fatalf("bindQueue: %v", err)
	}
	want := []string{
		"open queue 1, socket owner true",
		"register queue 1",
		"close queue 1",
		"open queue 2, socket owner true",
		"register queue 2",
		"close queue 2",
		"open queue 3, socket owner true",
		"register queue 3",
	}
	if fmt.Sprint(r.steps) != fmt.Sprint(want) {
		t.Fatalf("steps %q, want %q", r.steps, want)
	}
	if q != r.opened[2] {
		t.Fatal("bindQueue did not keep the queue that registered")
	}
}

func TestARefusalThenARaceStaysWithoutTheSocketOwner(t *testing.T) {
	r := stubQueueBinding(t, failingOn(map[int]error{1: errKernelRefusesFlag, 2: errQueuedPacketFirst}))
	if _, err := bindWorker(t).bindQueue(0x8000); err != nil {
		t.Fatalf("bindQueue: %v", err)
	}
	if got := r.steps[len(r.steps)-2]; got != "open queue 3, socket owner false" {
		t.Fatalf("the last open was %q, a kernel that refused the flag once refuses it again", got)
	}
}

func TestAQueueThatNeverBindsFailsAndLeavesNothingBound(t *testing.T) {
	r := stubQueueBinding(t, func(int) error { return errQueuedPacketFirst })
	q, err := bindWorker(t).bindQueue(0x8000)
	if !errors.Is(err, errQueuedPacketFirst) {
		t.Fatalf("bindQueue returned %v, want the registration error", err)
	}
	if !strings.Contains(err.Error(), "537") {
		t.Fatalf("the error %q does not name the queue", err)
	}
	if q != nil {
		t.Fatal("bindQueue returned a queue nothing reads")
	}
	if len(r.opened) != queueBindAttempts {
		t.Fatalf("%d attempts, want %d", len(r.opened), queueBindAttempts)
	}
	for i, o := range r.opened {
		if !r.closed[o] {
			t.Fatalf("queue %d stays bound with no reader, and flows through it stall", i+1)
		}
	}
}

func TestAStoppedWorkerStopsRetryingTheBind(t *testing.T) {
	r := stubQueueBinding(t, func(int) error { return errQueuedPacketFirst })
	w := bindWorker(t)
	w.cancel()
	if _, err := w.bindQueue(0x8000); !errors.Is(err, errQueuedPacketFirst) {
		t.Fatalf("bindQueue returned %v, want the registration error", err)
	}
	if len(r.opened) >= queueBindAttempts {
		t.Fatalf("%d attempts after the worker stopped", len(r.opened))
	}
}
