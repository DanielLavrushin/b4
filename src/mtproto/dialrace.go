package mtproto

import (
	"fmt"
	"net"
	"time"
)

const (
	dialRaceStagger     = 500 * time.Millisecond
	dialRaceMaxInFlight = 3
	dialRaceWorkerAfter = 1500 * time.Millisecond
)

type raceAttempt struct {
	plan    transportPlan
	conn    net.Conn
	pooled  bool
	err     error
	timeout time.Duration
	elapsed time.Duration
	group   int
	early   bool
}

type dialRace struct {
	stagger     time.Duration
	maxInFlight int
	minAttempt  time.Duration
	deadline    time.Time
	workerAfter time.Duration
	earlyOK     func(p transportPlan) bool
	timeoutFor  func(p transportPlan) time.Duration
	dial        func(p transportPlan, timeout time.Duration, fresh bool) (net.Conn, bool, error)
	started     func(p transportPlan)
	failed      func(a raceAttempt)
	accept      func(a raceAttempt) error
	spare       func(a raceAttempt)
}

type raceOutcome struct {
	winner          *raceAttempt
	attempts        []string
	untried         int
	nativeTried     int
	nativeRedirects int
}

type raceItem struct {
	plan  transportPlan
	group int
	fresh bool
}

func raceClass(p transportPlan) int {
	switch {
	case p.kind != transportWS:
		return 2
	case p.isWorker:
		return 1
	default:
		return 0
	}
}

func raceGroups(plans []transportPlan) ([]raceItem, []int) {
	items := make([]raceItem, 0, len(plans))
	var sizes []int
	for i, p := range plans {
		if i == 0 || raceClass(p) != raceClass(plans[i-1]) {
			sizes = append(sizes, 0)
		}
		g := len(sizes) - 1
		sizes[g]++
		items = append(items, raceItem{plan: p, group: g})
	}
	return items, sizes
}

func (r *dialRace) run(plans []transportPlan) raceOutcome {
	var out raceOutcome
	pending, outstanding := raceGroups(plans)
	groupBegan := map[int]time.Time{}
	results := make(chan raceAttempt, 2*len(plans))
	inFlight := 0
	nativeBusy := false
	nativeDead := false
	earlyInFlight := 0
	var nextStart time.Time

	hasFallback := false
	for _, p := range plans {
		if !p.native {
			hasFallback = true
			break
		}
	}
	current := func() int {
		for g, n := range outstanding {
			if n > 0 {
				return g
			}
		}
		return -1
	}
	workerEarly := func(it raceItem) bool {
		if r.workerAfter <= 0 || earlyInFlight > 0 || raceClass(it.plan) != 1 || it.group != current()+1 {
			return false
		}
		if _, ok := groupBegan[current()]; !ok {
			return false
		}
		return r.earlyOK == nil || r.earlyOK(it.plan)
	}
	earlyAt := func() time.Time {
		return groupBegan[current()].Add(r.workerAfter)
	}
	eligible := func(it raceItem) bool {
		if it.group != current() && !(workerEarly(it) && !time.Now().Before(earlyAt())) {
			return false
		}
		return !it.plan.native || (!nativeBusy && !nativeDead)
	}
	canStart := func(it raceItem) bool {
		if !eligible(it) {
			return false
		}
		return inFlight < r.maxInFlight || (it.group != current() && inFlight <= r.maxInFlight)
	}
	earlyWake := func() time.Duration {
		for _, it := range pending {
			if workerEarly(it) {
				return time.Until(earlyAt())
			}
		}
		return -1
	}
	prune := func() {
		if !nativeDead {
			return
		}
		kept := pending[:0]
		for _, it := range pending {
			if it.plan.native {
				out.untried++
				outstanding[it.group]--
				continue
			}
			kept = append(kept, it)
		}
		pending = kept
	}
	startable := func() bool {
		for _, it := range pending {
			if canStart(it) {
				return true
			}
		}
		return false
	}
	launch := func(it raceItem, timeout time.Duration) {
		fresh := it.fresh
		early := it.group != current()
		if early {
			earlyInFlight++
		}
		if _, ok := groupBegan[it.group]; !ok {
			groupBegan[it.group] = time.Now()
		}
		inFlight++
		if it.plan.native {
			nativeBusy = true
		}
		if r.started != nil {
			r.started(it.plan)
		}
		go func() {
			begin := time.Now()
			conn, pooled, err := r.dial(it.plan, timeout, fresh)
			results <- raceAttempt{plan: it.plan, conn: conn, pooled: pooled, err: err, timeout: timeout, elapsed: time.Since(begin), group: it.group, early: early}
		}()
	}
	tryStart := func() {
		now := time.Now()
		if now.Before(nextStart) || !startable() {
			return
		}
		remaining := r.deadline.Sub(now)
		if remaining < r.minAttempt {
			out.untried += len(pending)
			for _, it := range pending {
				outstanding[it.group]--
			}
			pending = nil
			return
		}
		pick := -1
		for i, it := range pending {
			if !canStart(it) {
				continue
			}
			if it.group != current() {
				pick = i
				break
			}
			if pick < 0 {
				pick = i
			}
		}
		if pick >= 0 {
			it := pending[pick]
			pending = append(pending[:pick], pending[pick+1:]...)
			timeout := r.timeoutFor(it.plan)
			if timeout > remaining {
				timeout = remaining
			}
			launch(it, timeout)
			nextStart = now.Add(r.stagger)
			return
		}
	}
	tryStart()
	for inFlight > 0 {
		var wake <-chan time.Time
		var timer *time.Timer
		if startable() {
			d := time.Until(nextStart)
			if d < 0 {
				d = 0
			}
			timer = time.NewTimer(d)
			wake = timer.C
		} else if d := earlyWake(); d > 0 {
			timer = time.NewTimer(d)
			wake = timer.C
		}
		select {
		case a := <-results:
			if timer != nil {
				timer.Stop()
			}
			inFlight--
			outstanding[a.group]--
			if a.early {
				earlyInFlight--
			}
			if a.plan.native {
				nativeBusy = false
			}
			if a.err == nil {
				err := r.accept(a)
				if err == nil {
					out.winner = &a
					go r.drain(results, inFlight)
					return out
				}
				out.attempts = append(out.attempts, fmt.Sprintf("%s: %s", a.plan.describe(), shortErr(err)))
				if a.pooled {
					pending = append([]raceItem{{plan: a.plan, group: a.group, fresh: true}}, pending...)
					outstanding[a.group]++
				}
			} else {
				out.attempts = append(out.attempts, fmt.Sprintf("%s: %s", a.plan.describe(), shortErr(a.err)))
				if a.plan.native && a.plan.kind == transportWS {
					out.nativeTried++
					if isWSRedirect(a.err) {
						out.nativeRedirects++
					} else if isDialTimeout(a.err) && isConnectStage(a.err) && hasFallback {
						nativeDead = true
					}
				}
				if r.failed != nil {
					r.failed(a)
				}
			}
			prune()
			nextStart = time.Time{}
			tryStart()
		case <-wake:
			tryStart()
		}
	}
	out.untried += len(pending)
	return out
}

func (r *dialRace) drain(results <-chan raceAttempt, n int) {
	for i := 0; i < n; i++ {
		a := <-results
		if a.err == nil {
			if r.spare != nil {
				r.spare(a)
			} else if a.conn != nil {
				_ = a.conn.Close()
			}
			continue
		}
		if r.failed != nil {
			r.failed(a)
		}
	}
}
