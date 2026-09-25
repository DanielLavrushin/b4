package mtproto

import (
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

// A Cloudflare Worker relay stops forwarding partway through a session and then
// holds the WebSocket open in silence - no close frame, no reset - so the relay
// has nothing to fail on and the client waits out its own receive timeout,
// reconnects, and is handed the same dead route again. Measured against a real
// Worker from a censored network: not one of 73 sessions carried past 17 KB,
// while Telegram's own WebSocket edge carried 1.1 MB on the same box at the same
// time. Record the Worker that did it and rank it below the other transports for
// a while, so the reconnect lands somewhere else.
const workerStallCooldown = 10 * time.Minute

const (
	workerStallStrikes = 2
	workerStallWindow  = 5 * time.Minute
)

type workerStrike struct {
	first time.Time
	count int
}

var (
	workerStallMu    sync.Mutex
	workerStallUntil = map[string]time.Time{}
	workerStrikes    = map[string]workerStrike{}
)

func workerInCooldown(domain string) bool {
	if domain == "" {
		return false
	}
	workerStallMu.Lock()
	defer workerStallMu.Unlock()
	t, ok := workerStallUntil[domain]
	if !ok {
		return false
	}
	if time.Now().After(t) {
		delete(workerStallUntil, domain)
		return false
	}
	return true
}

func workerRecordStall(domain string) {
	if domain == "" {
		return
	}
	now := time.Now()
	workerStallMu.Lock()
	st := workerStrikes[domain]
	if st.count == 0 || now.Sub(st.first) > workerStallWindow {
		st = workerStrike{first: now}
	}
	st.count++
	cool := st.count >= workerStallStrikes
	if cool {
		delete(workerStrikes, domain)
	} else {
		workerStrikes[domain] = st
	}
	workerStallMu.Unlock()
	if cool {
		workerDemote(domain)
		return
	}
	log.Debugf("%s worker %s went quiet on a session (%d of %d within %s before it is ranked down)",
		tg(""), domain, st.count, workerStallStrikes, workerStallWindow)
}

func workerDemote(domain string) {
	if domain == "" {
		return
	}
	workerStallMu.Lock()
	_, had := workerStallUntil[domain]
	workerStallUntil[domain] = time.Now().Add(workerStallCooldown)
	workerStallMu.Unlock()
	if !had {
		log.Infof("%s worker %s stopped relaying mid-session; ranking it below other transports for %s",
			tg(""), domain, workerStallCooldown)
	}
}

func workerResetStall() {
	workerStallMu.Lock()
	defer workerStallMu.Unlock()
	workerStallUntil = map[string]time.Time{}
	workerStrikes = map[string]workerStrike{}
}
