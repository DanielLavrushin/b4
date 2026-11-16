package mtproto

import (
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const (
	relaySharedPortDC = 203
	relayLeadFor      = 2 * time.Minute
)

var (
	relayLeadMu    sync.Mutex
	relayLeadUntil = map[string]time.Time{}
)

func relayAddrFor(cfg *config.MTProtoConfig, dc int, target dialTarget) string {
	relay := strings.TrimSpace(cfg.DCRelay)
	if relay == "" {
		return ""
	}
	if target.valid() && dcAbs(dc) == relaySharedPortDC {
		return ""
	}
	addrs, err := ResolveDCAll(dc, false, relay)
	if err != nil || len(addrs) == 0 {
		return ""
	}
	return addrs[0]
}

func relayLeads(addr string) bool {
	if addr == "" {
		return false
	}
	relayLeadMu.Lock()
	defer relayLeadMu.Unlock()
	until, ok := relayLeadUntil[addr]
	if !ok {
		return false
	}
	if !time.Now().Before(until) {
		delete(relayLeadUntil, addr)
		return false
	}
	return true
}

func relayLeadNote(addr string) {
	if addr == "" {
		return
	}
	now := time.Now()
	relayLeadMu.Lock()
	until, had := relayLeadUntil[addr]
	if had && now.Before(until) {
		relayLeadMu.Unlock()
		return
	}
	relayLeadUntil[addr] = now.Add(relayLeadFor)
	relayLeadMu.Unlock()
	log.Infof("%s relay %s answered a session no other route carried; trying it first for %s", tg(""), addr, relayLeadFor)
}

func relayLeadClear(addr string) {
	if addr == "" {
		return
	}
	relayLeadMu.Lock()
	defer relayLeadMu.Unlock()
	delete(relayLeadUntil, addr)
}

func relayAnswered(info dialInfo) func() {
	if !info.plan.relay || !info.fromBehind || info.plan.addr == "" {
		return nil
	}
	return func() { relayLeadNote(info.plan.addr) }
}

func relayUnanswered(info dialInfo) func() {
	if !info.plan.relay || info.plan.addr == "" {
		return nil
	}
	return func() { relayLeadClear(info.plan.addr) }
}

func relayResetState() {
	relayLeadMu.Lock()
	defer relayLeadMu.Unlock()
	relayLeadUntil = map[string]time.Time{}
}
