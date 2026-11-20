package nfq

import (
	"encoding/binary"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type flowJob struct {
	run    func() func()
	finish func(after func())
}

type flowQueue struct {
	jobs []flowJob
}

type flowInjector struct {
	mu    sync.Mutex
	flows map[string]*flowQueue
	live  atomic.Int64
}

func newFlowInjector() *flowInjector {
	return &flowInjector{flows: make(map[string]*flowQueue)}
}

func (f *flowInjector) busy(key string) bool {
	if f == nil || f.live.Load() == 0 {
		return false
	}
	f.mu.Lock()
	_, ok := f.flows[key]
	f.mu.Unlock()
	return ok
}

func (f *flowInjector) submit(key string, force bool, jobs ...flowJob) bool {
	f.mu.Lock()
	q := f.flows[key]
	if q != nil {
		q.jobs = append(q.jobs, jobs...)
		f.mu.Unlock()
		return true
	}
	if !force {
		f.mu.Unlock()
		return false
	}
	q = &flowQueue{jobs: append([]flowJob(nil), jobs...)}
	f.flows[key] = q
	f.live.Add(1)
	f.mu.Unlock()
	go f.drain(key, q)
	return true
}

func (f *flowInjector) drain(key string, q *flowQueue) {
	for {
		f.mu.Lock()
		job := q.jobs[0]
		f.mu.Unlock()

		after := job.run()

		f.mu.Lock()
		q.jobs[0] = flowJob{}
		q.jobs = q.jobs[1:]
		last := len(q.jobs) == 0
		if last && f.flows[key] == q {
			delete(f.flows, key)
			f.live.Add(-1)
		}
		f.mu.Unlock()

		job.finish(after)
		if last {
			return
		}
	}
}

func (f *flowInjector) detach(key string) {
	if f == nil || f.live.Load() == 0 {
		return
	}
	f.mu.Lock()
	if _, ok := f.flows[key]; ok {
		delete(f.flows, key)
		f.live.Add(-1)
	}
	f.mu.Unlock()
}

func (f *flowInjector) pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.flows)
}

func (w *Worker) flowJob(run func() func()) flowJob {
	w.wg.Add(1)
	return flowJob{
		run: run,
		finish: func(after func()) {
			if after == nil {
				injectRelease()
				w.wg.Done()
				return
			}
			go func() {
				defer func() {
					injectRelease()
					w.wg.Done()
				}()
				after()
			}()
		},
	}
}

func (w *Worker) tcpInjector(ver uint8) func(*config.SetConfig, []byte, net.IP, bool) func() {
	if ver == IPv4 {
		return w.injectTCP
	}
	return w.injectTCPv6
}

func (w *Worker) followsInjection(connKey string, tcp, payload []byte) bool {
	if w.injector == nil || w.sock == nil || len(payload) == 0 || tcp[13]&0x06 != 0 {
		return false
	}
	return w.injector.busy(connKey) || w.tlsCache.LanedContinuation(connKey, binary.BigEndian.Uint32(tcp[4:8]))
}

func (w *Worker) resendInOrder(vc *verdictCtx, pkt *pktInfo, tcp []byte, connKey string, payload []byte) bool {
	if vc.own != nil || vc.copy != nil || vc.ifaceFiltered || !w.followsInjection(connKey, tcp, payload) {
		return false
	}
	if h := vc.takeHeld(); h != nil {
		vc.held = h
		return false
	}
	if pkt.ver != IPv4 && !w.sock.IPv6Ready() {
		return false
	}
	if !injectAcquire() {
		return false
	}
	raw := pkt.raw
	if vc.modified != nil {
		raw = vc.modified
	}
	raw = append([]byte(nil), raw...)
	dst := append(net.IP(nil), pkt.dst...)
	if !vc.drop() {
		injectRelease()
		return true
	}
	log.Tracef("TCP to %s:%d leaves after the packets queued ahead of it on %s", pkt.dstStr, binary.BigEndian.Uint16(tcp[2:4]), connKey)
	w.sendInOrder(connKey, raw, dst, pkt.ver, 1)
	return true
}

func (w *Worker) sendInOrder(connKey string, raw []byte, dst net.IP, ver uint8, copies int) {
	w.injector.submit(connKey, true, w.flowJob(func() func() {
		w.sendCopies(raw, dst, ver, copies)
		return nil
	}))
}

func postDesyncLater(send func()) func() {
	return func() {
		time.Sleep(postDesyncDelay)
		send()
	}
}

const postDesyncDelay = 50 * time.Millisecond
