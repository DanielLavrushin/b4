package mtproto

import (
	"net"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	bridgeRejectTTL     = 6 * time.Hour
	bridgeUnresolvedTTL = 30 * time.Minute
	bridgeRejectMax     = 256
)

type bridgeReject struct {
	dc         int
	unresolved bool
	until      time.Time
}

var (
	bridgeRejectMu sync.Mutex
	bridgeRejects  = map[string]bridgeReject{}
)

type bridgeDCChoice struct {
	dc      int
	src     string
	guessed bool
}

func bridgeSiblingDC(dc int) int {
	sign := 1
	if dc < 0 {
		sign, dc = -1, -dc
	}
	switch dc {
	case 1:
		return sign * 3
	case 3:
		return sign * 1
	case 2:
		return sign * 4
	case 4:
		return sign * 2
	}
	return 0
}

func dcAbs(dc int) int {
	if dc < 0 {
		return -dc
	}
	return dc
}

func bridgeLearnedDC(ip net.IP, dc int) (next int, around bool, found bool) {
	if ip == nil {
		return dc, false, false
	}
	now := time.Now()
	bridgeRejectMu.Lock()
	defer bridgeRejectMu.Unlock()
	r, ok := bridgeRejects[ip.String()]
	if !ok {
		return dc, false, false
	}
	if now.After(r.until) {
		delete(bridgeRejects, ip.String())
		return dc, false, false
	}
	if r.unresolved {
		return dc, true, true
	}
	if r.dc != dcAbs(dc) {
		return dc, false, false
	}
	return bridgeSiblingDC(dc), false, true
}

func bridgeRejectDC(ip net.IP, dc int) {
	if ip == nil {
		return
	}
	key := ip.String()
	alt := bridgeSiblingDC(dc)
	now := time.Now()

	bridgeRejectMu.Lock()
	r, had := bridgeRejects[key]
	if had && now.After(r.until) {
		had = false
	}
	var learned, lone, both bool
	switch {
	case !had && alt != 0:
		bridgeRejectPruneLocked(now)
		bridgeRejects[key] = bridgeReject{dc: dcAbs(dc), until: now.Add(bridgeRejectTTL)}
		learned = true
	case !had:
		bridgeRejectPruneLocked(now)
		bridgeRejects[key] = bridgeReject{dc: dcAbs(dc), unresolved: true, until: now.Add(bridgeUnresolvedTTL)}
		lone = true
	case r.unresolved, r.dc == dcAbs(dc):
	default:
		bridgeRejects[key] = bridgeReject{dc: r.dc, unresolved: true, until: now.Add(bridgeUnresolvedTTL)}
		both = true
	}
	bridgeRejectMu.Unlock()

	switch {
	case learned:
		log.Warnf("%s %s answered -444 as DC %d, so it belongs to another data center; later sessions to it go to DC %d for %s",
			tg(""), key, dcAbs(dc), dcAbs(alt), bridgeRejectTTL)
	case lone:
		log.Warnf("%s %s answered -444 as DC %d, which has no other data center at its site; later sessions to it bypass the data center routes for %s",
			tg(""), key, dcAbs(dc), bridgeUnresolvedTTL)
	case both:
		log.Warnf("%s %s answered -444 as DC %d and as DC %d; later sessions to it bypass the data center routes for %s",
			tg(""), key, r.dc, dcAbs(dc), bridgeUnresolvedTTL)
	}
}

func bridgeRejectPruneLocked(now time.Time) {
	for k, r := range bridgeRejects {
		if now.After(r.until) {
			delete(bridgeRejects, k)
		}
	}
	for len(bridgeRejects) >= bridgeRejectMax {
		var oldest string
		var at time.Time
		for k, r := range bridgeRejects {
			if oldest == "" || r.until.Before(at) {
				oldest, at = k, r.until
			}
		}
		delete(bridgeRejects, oldest)
	}
}

func bridgeChooseDC(tag string, origIP net.IP, handshakeDC int) (bridgeDCChoice, bool) {
	var c bridgeDCChoice
	if mapped, found := dcForIP(origIP); found {
		c.dc, c.src = mapped, "ip"
	} else if validTransparentDC(handshakeDC) {
		c.dc, c.src = handshakeDC, "handshake"
	} else if mapped, found := dcForIPRange(origIP); found {
		c.dc, c.src = mapped, "ip-range"
	} else {
		return c, false
	}
	confirmed := c.src == "handshake" || (validTransparentDC(handshakeDC) && dcAbs(handshakeDC) == dcAbs(c.dc))
	c.guessed = !confirmed
	if c.guessed {
		next, around, found := bridgeLearnedDC(origIP, c.dc)
		switch {
		case around:
			log.Debugf("%s bridge %s refused the data centers its address suggests -> no DC route", tag, origIP)
			return c, false
		case found:
			log.Debugf("%s bridge %s answered -444 as DC%d before -> using DC%d", tag, origIP, c.dc, next)
			c.dc = next
			c.src += "+learned"
		}
	}
	if signed, media := applyHandshakeMedia(c.dc, handshakeDC); media {
		log.Debugf("%s bridge DC%d is the media cluster per handshake -> using DC%d (src=%s+handshake-media)", tag, c.dc, signed, c.src)
		c.dc = signed
		c.src += "+handshake-media"
	}
	if rng, inRange := dcForIPRange(origIP); inRange && validTransparentDC(handshakeDC) && rng != handshakeDC {
		log.Debugf("%s bridge DC ambiguity for %s: ip-range=DC%d handshake=DC%d -> using DC%d (src=%s)", tag, origIP, rng, handshakeDC, c.dc, c.src)
	}
	return c, true
}

func bridgeResetRejects() {
	bridgeRejectMu.Lock()
	bridgeRejects = map[string]bridgeReject{}
	bridgeRejectMu.Unlock()
}

func (c bridgeDCChoice) errHandler(dial dialInfo, label string, ip net.IP) func(int32) bool {
	demote := transportErrHandler(dial, c.dc, label)
	if !c.guessed {
		return demote
	}
	return func(code int32) bool {
		if code != tgErrInvalidDC {
			return demote(code)
		}
		log.Debugf("%s upstream answered -444 for DC %d, a data center b4 took from the address; cutting the session without ranking the route down",
			label, c.dc)
		bridgeRejectDC(ip, c.dc)
		return true
	}
}
