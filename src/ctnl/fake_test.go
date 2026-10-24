package ctnl

import (
	"bytes"
	"io"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/mdlayher/netlink"
	"github.com/mdlayher/netlink/nltest"
	"golang.org/x/sys/unix"
)

func resetState() {
	probeMu.Lock()
	probeReason, warnedReason = "", ""
	probeState.Store(stateUntested)
	probeMu.Unlock()
	stats.queued.Store(0)
	stats.deleted.Store(0)
	stats.notFound.Store(0)
	stats.busy.Store(0)
	stats.failed.Store(0)
	stats.dropped.Store(0)
	stats.unavailable.Store(0)
	stats.invalid.Store(0)
	busyLogged.Store(false)
	failedLogged.Store(false)
}

type fakeKernel struct {
	mu    sync.Mutex
	sent  []netlink.Message
	dials int
	reply func(m netlink.Message) unix.Errno
	hold  chan struct{}
}

func useFakeKernel(t *testing.T, reply func(m netlink.Message) unix.Errno) *fakeKernel {
	t.Helper()
	k := &fakeKernel{reply: reply}
	resetState()
	orig := dial
	dial = func() (*netlink.Conn, error) {
		k.mu.Lock()
		k.dials++
		k.mu.Unlock()
		return nltest.Dial(func(reqs []netlink.Message) ([]netlink.Message, error) {
			if len(reqs) == 0 {
				return nil, io.EOF
			}
			k.mu.Lock()
			k.sent = append(k.sent, reqs...)
			hold, reply := k.hold, k.reply
			k.mu.Unlock()
			if hold != nil {
				<-hold
			}
			return nltest.Error(int(reply(reqs[0])), reqs)
		}), nil
	}
	t.Cleanup(func() {
		dial = orig
		resetState()
	})
	return k
}

func (k *fakeKernel) requests() []netlink.Message {
	k.mu.Lock()
	defer k.mu.Unlock()
	return append([]netlink.Message(nil), k.sent...)
}

func (k *fakeKernel) dialCount() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.dials
}

func (k *fakeKernel) setReply(reply func(m netlink.Message) unix.Errno) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.reply = reply
}

func answer(get, del unix.Errno) func(m netlink.Message) unix.Errno {
	return func(m netlink.Message) unix.Errno {
		if m.Header.Type == msgGet {
			return get
		}
		return del
	}
}

type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

func captureLog(t *testing.T) *syncBuffer {
	t.Helper()
	buf := &syncBuffer{}
	previous := log.Level(log.CurLevel.Load())
	log.Init(io.Discard, log.LevelInfo, true)
	log.StartCapture(buf)
	t.Cleanup(func() {
		log.StopCapture(buf)
		log.Init(os.Stderr, previous, true)
	})
	return buf
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s (counters %+v)", what, Stats())
		}
		time.Sleep(time.Millisecond)
	}
}
