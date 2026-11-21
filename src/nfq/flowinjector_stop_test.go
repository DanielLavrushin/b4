package nfq

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestQueuedInjectionsDoNotRunOnceTheWorkerStops(t *testing.T) {
	cfg := config.NewConfig()
	w := newTestWorker(t, &cfg)
	inj := newFlowInjector()
	before := InjectInflight()
	if !injectAcquireN(3) {
		t.Fatal("no injection slots")
	}
	started, release := make(chan struct{}), make(chan struct{})
	var ran atomic.Int32
	inj.submit("10.0.0.1:1111->1.2.3.4:443", true, w.flowJob(func() func() {
		close(started)
		<-release
		ran.Add(1)
		return nil
	}))
	<-started
	for range 2 {
		inj.submit("10.0.0.1:1111->1.2.3.4:443", true, w.flowJob(func() func() {
			ran.Add(1)
			return nil
		}))
	}
	w.cancel()
	close(release)
	done := make(chan struct{})
	go func() { w.wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("the worker's wait group was not released")
	}
	if got := ran.Load(); got != 1 {
		t.Errorf("%d queued injections ran after the worker stopped, want only the one already running", got-1)
	}
	if n := InjectInflight(); n != before {
		t.Errorf("injection slots in flight: %d, want %d", n, before)
	}
}
