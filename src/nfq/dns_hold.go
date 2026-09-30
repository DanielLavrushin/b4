package nfq

import (
	"time"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/log"
)

const maxDNSRouteHolds = 256

var (
	dnsRouteHoldMax   = 250 * time.Millisecond
	dnsRouteHoldSlots = make(chan struct{}, maxDNSRouteHolds)
)

func (w *Worker) holdForRoutes(vc *verdictCtx, waits []<-chan struct{}, domain string, release func()) bool {
	if len(waits) == 0 || vc == nil || vc.q == nil {
		return false
	}
	w.holdsLive.Add(1)
	if w.holdsReleased() {
		w.holdsLive.Add(-1)
		return false
	}
	select {
	case dnsRouteHoldSlots <- struct{}{}:
	default:
		w.holdsLive.Add(-1)
		return false
	}
	stop := w.holdStopSignal()
	limit := dnsRouteHoldMax
	w.wg.Add(1)
	go func() {
		defer func() {
			<-dnsRouteHoldSlots
			w.holdsLive.Add(-1)
			w.wg.Done()
		}()
		start := time.Now()
		timer := time.NewTimer(limit)
		defer timer.Stop()
		landed := waitForRoutes(waits, timer.C, stop)
		release()
		held := time.Since(start).Round(time.Millisecond)
		if landed {
			log.Tracef("DNS answer for %s held %s until the set update for its new addresses finished", dns.SafeName(domain), held)
		} else {
			log.Tracef("DNS answer for %s released after %s while the set update for its new addresses was still running", dns.SafeName(domain), held)
		}
	}()
	return true
}

func (w *Worker) waitRoutesInline(waits []<-chan struct{}) {
	if len(waits) == 0 {
		return
	}
	timer := time.NewTimer(dnsRouteHoldMax)
	defer timer.Stop()
	waitForRoutes(waits, timer.C, w.holdStopSignal())
}

func (w *Worker) holdStopSignal() <-chan struct{} {
	if w.holdStop != nil {
		return w.holdStop
	}
	if w.ctx != nil {
		return w.ctx.Done()
	}
	return nil
}

func (w *Worker) holdsReleased() bool {
	select {
	case <-w.holdStop:
		return true
	default:
		return false
	}
}

func (w *Worker) releaseHolds() {
	if w.holdStop == nil {
		return
	}
	w.holdStopOnce.Do(func() { close(w.holdStop) })
	deadline := time.Now().Add(dnsRouteHoldMax)
	for w.holdsLive.Load() > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

func waitForRoutes(waits []<-chan struct{}, timeout <-chan time.Time, stop <-chan struct{}) bool {
	for _, ch := range waits {
		select {
		case <-ch:
		case <-timeout:
			return false
		case <-stop:
			return false
		}
	}
	return true
}
