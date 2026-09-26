package mtproto

import (
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	failOpenWorkerPort  = 443
	failOpenMemory      = 30 * time.Minute
	failOpenMemoryPrune = 1024
)

var (
	failOpenMu     sync.Mutex
	failOpenDirect = map[string]time.Time{}
)

func failOpenPrefersDirect(ip string) bool {
	failOpenMu.Lock()
	defer failOpenMu.Unlock()
	until, ok := failOpenDirect[ip]
	if !ok {
		return false
	}
	if time.Now().After(until) {
		delete(failOpenDirect, ip)
		return false
	}
	return true
}

func failOpenRemember(ip string, direct bool) {
	if ip == "" {
		return
	}
	failOpenMu.Lock()
	defer failOpenMu.Unlock()
	_, had := failOpenDirect[ip]
	if !direct {
		if had {
			delete(failOpenDirect, ip)
			log.Debugf("%s fail-open to %s: a direct connection failed or carried nothing, the Worker is tried first again", tg(""), ip)
		}
		return
	}
	now := time.Now()
	if !had && len(failOpenDirect) >= failOpenMemoryPrune {
		for k, until := range failOpenDirect {
			if now.After(until) {
				delete(failOpenDirect, k)
			}
		}
	}
	failOpenDirect[ip] = now.Add(failOpenMemory)
	if !had {
		log.Debugf("%s fail-open to %s goes direct first for %s", tg(""), ip, failOpenMemory)
	}
}
