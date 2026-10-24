package nfq

import (
	"errors"
	"fmt"
	"testing"

	"github.com/florianl/go-nfqueue"
)

type queueBindRig struct {
	steps   []string
	opened  []*nfqueue.Nfqueue
	refused map[*nfqueue.Nfqueue]bool
}

func stubQueueBinding(t *testing.T, refuse ...int) *queueBindRig {
	t.Helper()
	r := &queueBindRig{refused: map[*nfqueue.Nfqueue]bool{}}
	name := func(q *nfqueue.Nfqueue) string {
		for i, o := range r.opened {
			if o == q {
				return fmt.Sprintf("queue %d", i+1)
			}
		}
		return "an unknown queue"
	}
	prevOpen, prevRegister, prevClose := queueOpen, queueRegister, queueClose
	queueOpen = func(_ *Worker, extraFlags uint32) (*nfqueue.Nfqueue, error) {
		q := &nfqueue.Nfqueue{}
		r.opened = append(r.opened, q)
		for _, n := range refuse {
			if n == len(r.opened) {
				r.refused[q] = true
			}
		}
		r.steps = append(r.steps, fmt.Sprintf("open %s, socket owner %v", name(q), extraFlags&nfqueue.NfQaCfgFlagUIDGid != 0))
		return q, nil
	}
	queueRegister = func(_ *Worker, q *nfqueue.Nfqueue, _ uint) error {
		r.steps = append(r.steps, "register "+name(q))
		if r.refused[q] {
			return errors.New("operation not supported")
		}
		return nil
	}
	queueClose = func(q *nfqueue.Nfqueue) error {
		r.steps = append(r.steps, "close "+name(q))
		return nil
	}
	t.Cleanup(func() {
		queueOpen, queueRegister, queueClose = prevOpen, prevRegister, prevClose
	})
	return r
}

func TestTheQueueAsksForTheSocketOwnerOfEachPacket(t *testing.T) {
	r := stubQueueBinding(t)
	q, err := (&Worker{qnum: 537}).bindQueue(0x8000)
	if err != nil {
		t.Fatalf("bindQueue: %v", err)
	}
	want := []string{"open queue 1, socket owner true", "register queue 1"}
	if fmt.Sprint(r.steps) != fmt.Sprint(want) || q != r.opened[0] {
		t.Fatalf("steps %q, want %q", r.steps, want)
	}
}

func TestAKernelThatRefusesTheSocketOwnerGetsTheQueueWithoutIt(t *testing.T) {
	r := stubQueueBinding(t, 1)
	q, err := (&Worker{qnum: 537}).bindQueue(0x8000)
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

func TestASecondRefusalKeepsTheQueueAsBefore(t *testing.T) {
	r := stubQueueBinding(t, 1, 2)
	q, err := (&Worker{qnum: 537}).bindQueue(0x8000)
	if err != nil {
		t.Fatalf("bindQueue returned %v: a failed registration without the socket owner is not an error at start, as before", err)
	}
	if q != r.opened[1] {
		t.Fatal("bindQueue did not keep the queue registered without the socket owner")
	}
}
