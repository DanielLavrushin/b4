package ctnl

import (
	"errors"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const workerQueueLen = 256

type Counters struct {
	Queued      uint64 `json:"queued"`
	Deleted     uint64 `json:"deleted"`
	NotFound    uint64 `json:"not_found"`
	Busy        uint64 `json:"busy"`
	Failed      uint64 `json:"failed"`
	Dropped     uint64 `json:"dropped"`
	Unavailable uint64 `json:"unavailable"`
	Invalid     uint64 `json:"invalid"`
}

var stats struct {
	queued      atomic.Uint64
	deleted     atomic.Uint64
	notFound    atomic.Uint64
	busy        atomic.Uint64
	failed      atomic.Uint64
	dropped     atomic.Uint64
	unavailable atomic.Uint64
	invalid     atomic.Uint64
}

var (
	busyLogged   atomic.Bool
	failedLogged atomic.Bool
)

func Stats() Counters {
	return Counters{
		Queued:      stats.queued.Load(),
		Deleted:     stats.deleted.Load(),
		NotFound:    stats.notFound.Load(),
		Busy:        stats.busy.Load(),
		Failed:      stats.failed.Load(),
		Dropped:     stats.dropped.Load(),
		Unavailable: stats.unavailable.Load(),
		Invalid:     stats.invalid.Load(),
	}
}

type job struct {
	tuple      Tuple
	dir        Direction
	bestEffort bool
}

func (j job) count(c *atomic.Uint64) {
	if !j.bestEffort {
		c.Add(1)
	}
}

type Worker struct {
	jobs     chan job
	stop     chan struct{}
	done     chan struct{}
	stopOnce sync.Once
	conn     *netlink.Conn
}

func NewWorker() *Worker {
	w := &Worker{
		jobs: make(chan job, workerQueueLen),
		stop: make(chan struct{}),
		done: make(chan struct{}),
	}
	go w.run()
	return w
}

func (w *Worker) Enqueue(t Tuple, dir Direction) bool {
	return w.enqueue(job{tuple: t, dir: dir})
}

func (w *Worker) EnqueueBestEffort(t Tuple, dir Direction) bool {
	return w.enqueue(job{tuple: t, dir: dir, bestEffort: true})
}

func (w *Worker) enqueue(j job) bool {
	if w == nil {
		return false
	}
	if j.tuple.check() != nil || !j.dir.valid() {
		j.count(&stats.invalid)
		return false
	}
	if probeState.Load() == stateUnavailable {
		j.count(&stats.unavailable)
		return false
	}
	select {
	case w.jobs <- j:
		stats.queued.Add(1)
		return true
	default:
		j.count(&stats.dropped)
		return false
	}
}

func (w *Worker) Stop() {
	if w == nil {
		return
	}
	w.stopOnce.Do(func() { close(w.stop) })
	<-w.done
}

func (w *Worker) run() {
	defer close(w.done)
	defer w.closeConn()
	for {
		select {
		case <-w.stop:
			return
		case j := <-w.jobs:
			w.handle(j)
		}
	}
}

func (w *Worker) handle(j job) {
	if !Available() {
		j.count(&stats.unavailable)
		return
	}
	classify(j, w.delete(j))
}

func (w *Worker) delete(j job) error {
	m, err := request(msgDelete, j.tuple, j.dir)
	if err != nil {
		return err
	}
	if w.conn == nil {
		c, err := dial()
		if err != nil {
			return err
		}
		w.conn = c
	}
	_ = w.conn.SetDeadline(time.Now().Add(requestTimeout))
	_, err = w.conn.Execute(m)
	if err != nil && !errors.Is(err, unix.ENOENT) && !errors.Is(err, unix.EBUSY) {
		w.closeConn()
	}
	return err
}

func (w *Worker) closeConn() {
	if w.conn != nil {
		_ = w.conn.Close()
		w.conn = nil
	}
}

func classify(j job, err error) {
	switch {
	case err == nil:
		stats.deleted.Add(1)
	case errors.Is(err, unix.ENOENT):
		stats.notFound.Add(1)
	case errors.Is(err, unix.EBUSY):
		stats.busy.Add(1)
		if busyLogged.CompareAndSwap(false, true) {
			log.Warnf("The kernel refused to delete the conntrack entry of a dropped connection (%s) because the connection is offloaded (flow offloading), and b4 does not see the packets of offloaded connections. Later refusals are counted in the diagnostics, not logged", j.tuple)
		}
	case errors.Is(err, unix.EINVAL), errors.Is(err, unix.EOPNOTSUPP):
		j.count(&stats.failed)
		disable("the kernel rejected a conntrack delete: " + err.Error())
	default:
		j.count(&stats.failed)
		if failedLogged.CompareAndSwap(false, true) {
			log.Warnf("Deleting the conntrack entry of a dropped connection (%s) failed: %v. Later failures are counted in the diagnostics, not logged", j.tuple, err)
		} else {
			log.Tracef("conntrack delete %s (%s): %v", j.tuple, j.dir, err)
		}
	}
}
