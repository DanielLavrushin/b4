package nfq

import (
	"encoding/binary"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
)

const wsSizeCapacity = 20000
const wsSizeTTL = 2 * time.Minute

type wsSizeFlow struct {
	cfg           *config.Config
	set           *config.SetConfig
	isn           uint32
	syn           bool
	cutoff        bool
	expires       time.Time
	hello         *wsSizeHello
	helloDeadline time.Time
	helloPackets  int
	helloEnd      int
}

type wsSizeCache struct {
	mu         sync.Mutex
	flows      map[streamKey]wsSizeFlow
	helloBytes int
}

func newWSSizeCache() *wsSizeCache {
	return &wsSizeCache{flows: make(map[streamKey]wsSizeFlow)}
}

func (c *wsSizeCache) cleanup(now time.Time) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupLocked(now)
}

func (c *wsSizeCache) cleanupLocked(now time.Time) {
	for k, f := range c.flows {
		if !now.Before(f.expires) {
			c.dropLocked(k)
		} else if !f.helloDeadline.IsZero() && !now.Before(f.helloDeadline) {
			c.releaseHello(&f)
			f.cutoff, f.helloEnd = true, 0
			c.flows[k] = f
		}
	}
}

func (c *wsSizeCache) closeIncoming(raw []byte, at int) {
	if c == nil || len(raw) < at+20 || raw[at+13]&5 == 0 {
		return
	}
	k, ok := streamKeyOf(raw, at)
	if !ok {
		return
	}
	k.client, k.server = k.server, k.client
	c.mu.Lock()
	c.dropLocked(k)
	c.mu.Unlock()
}

func (c *wsSizeCache) rewrite(raw []byte, at int, selected *config.SetConfig, cfg *config.Config, now time.Time) bool {
	if c == nil || len(raw) < at+20 {
		return false
	}
	if raw[0]>>4 == 4 && (at < 20 || int(binary.BigEndian.Uint16(raw[2:4])) != len(raw)) {
		return false
	}
	if raw[0]>>4 == 6 && int(binary.BigEndian.Uint16(raw[4:6]))+40 != len(raw) {
		return false
	}
	key, ok := streamKeyOf(raw, at)
	if !ok {
		return false
	}
	if raw[0]>>4 == 6 && at != 40 {
		return false
	}
	tcp := raw[at:]
	hlen := int(tcp[12]>>4) * 4
	if hlen < 20 || hlen > len(tcp) {
		return false
	}
	flags := tcp[13]
	syn := flags&0x12 == 2
	isn := binary.BigEndian.Uint32(tcp[4:8])
	c.mu.Lock()
	defer c.mu.Unlock()
	f, exists := c.flows[key]
	if exists && (f.cfg != cfg || !now.Before(f.expires)) {
		c.dropLocked(key)
		exists = false
	}
	if syn && (!exists || f.isn != isn || !f.syn) {
		c.dropLocked(key)
		if selected == nil || !selected.Enabled || !selected.TCP.WSSize.Enabled || selected.Routing.Enabled || selected.TCP.Duplicate.Enabled {
			return false
		}
		ws := selected.TCP.WSSize
		if ws.Window < 0 || ws.Window > 65535 || ws.Scale < 0 || ws.Scale > 14 || (ws.Cutoff != "" && ws.Cutoff != "client_hello") {
			return false
		}
		if len(c.flows) >= wsSizeCapacity {
			c.cleanupLocked(now)
		}
		if len(c.flows) >= wsSizeCapacity {
			return false
		}
		f = wsSizeFlow{cfg: cfg, set: selected, isn: isn, syn: true}
		if ws.Cutoff == "client_hello" {
			f.helloDeadline = now.Add(wsSizeHelloTimeout)
		}
		exists = true
	}
	if !exists {
		return false
	}
	active := !f.cutoff
	if !syn {
		f.syn = false
	}
	if f.set.TCP.WSSize.Cutoff == "client_hello" {
		active = c.rewriteHello(&f, tcp[hlen:], isn, syn, now)
	} else if len(tcp) > hlen {
		f.cutoff = true
	}
	f.expires = now.Add(wsSizeTTL)
	c.flows[key] = f
	if flags&5 != 0 {
		c.dropLocked(key)
	}
	if !active {
		return false
	}
	binary.BigEndian.PutUint16(tcp[14:16], uint16(f.set.TCP.WSSize.Window))
	for i := 20; i < hlen; {
		kind := tcp[i]
		if kind == 0 {
			break
		}
		if kind == 1 {
			i++
			continue
		}
		if i+1 >= hlen {
			break
		}
		n := int(tcp[i+1])
		if n < 2 || i+n > hlen {
			break
		}
		if kind == 3 && n == 3 {
			if int(tcp[i+2]) > f.set.TCP.WSSize.Scale {
				tcp[i+2] = byte(f.set.TCP.WSSize.Scale)
			}
			break
		}
		i += n
	}
	if raw[0]>>4 == 4 {
		sock.FixTCPChecksum(raw)
	} else {
		sock.FixTCPChecksumV6(raw)
	}
	return true
}
