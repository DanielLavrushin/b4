package nfq

import (
	"context"
	"errors"
	"net"
	"sort"
	"strconv"
	"sync"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netprobe"
)

const (
	dnsPinProbePort     = 443
	dnsPinProbeTimeout  = 4 * time.Second
	dnsPinProbeParallel = 4
)

type pinVerdict uint8

const (
	pinUnknown pinVerdict = iota
	pinAlive
	pinDead
)

type pinProber func(ctx context.Context, ip string, mark int) pinVerdict

type pinHealth struct {
	mu      sync.Mutex
	dead    map[string]struct{}
	lastRun time.Time
	running bool
	probe   pinProber
	ctx     context.Context
	cancel  context.CancelFunc
	wg      sync.WaitGroup
}

func newPinHealth(probe pinProber) *pinHealth {
	if probe == nil {
		probe = probePin
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &pinHealth{dead: make(map[string]struct{}), probe: probe, ctx: ctx, cancel: cancel}
}

func probePin(ctx context.Context, ip string, mark int) pinVerdict {
	conn, err := netprobe.Dialer(mark, dnsPinProbeTimeout, 0).DialContext(ctx, "tcp", net.JoinHostPort(ip, strconv.Itoa(dnsPinProbePort)))
	if err == nil {
		_ = conn.Close()
		return pinAlive
	}
	if errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET) {
		return pinAlive
	}
	if ctx.Err() != nil {
		return pinUnknown
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return pinDead
	}
	return pinUnknown
}

func (h *pinHealth) isDead(ip string) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	_, dead := h.dead[ip]
	return dead
}

func (h *pinHealth) due(retest time.Duration) bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	return !h.running && time.Since(h.lastRun) > retest
}

func (h *pinHealth) check(pins []string, mark int) {
	if h == nil || len(pins) == 0 {
		return
	}
	h.mu.Lock()
	if h.running || h.ctx.Err() != nil {
		h.mu.Unlock()
		return
	}
	h.running = true
	h.lastRun = time.Now()
	h.wg.Add(1)
	h.mu.Unlock()

	log.Tracef("DNS pins: checking %d pinned addresses on TCP %d", len(pins), dnsPinProbePort)
	go func() {
		defer h.wg.Done()
		h.runRound(pins, mark)
	}()
}

func (h *pinHealth) runRound(pins []string, mark int) {
	verdicts := make([]pinVerdict, len(pins))
	sem := make(chan struct{}, dnsPinProbeParallel)
	var wg sync.WaitGroup
	for i, pin := range pins {
		if h.ctx.Err() != nil {
			break
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, pin string) {
			defer wg.Done()
			defer func() { <-sem }()
			verdicts[i] = h.probe(h.ctx, pin, mark)
		}(i, pin)
	}
	wg.Wait()

	alive := 0
	for _, v := range verdicts {
		if v == pinAlive {
			alive++
		}
	}

	h.mu.Lock()
	defer h.mu.Unlock()
	h.running = false
	if h.ctx.Err() != nil {
		return
	}
	if alive == 0 {
		log.Tracef("DNS pins: none of %d pinned addresses answered, keeping the previous verdicts", len(pins))
		return
	}
	for i, pin := range pins {
		_, wasDead := h.dead[pin]
		switch verdicts[i] {
		case pinDead:
			if !wasDead {
				h.dead[pin] = struct{}{}
				log.Infof("DNS pin: %s does not answer on TCP %d from the router, preferring the other pinned addresses until it does", pin, dnsPinProbePort)
			}
		case pinAlive:
			if wasDead {
				delete(h.dead, pin)
				log.Infof("DNS pin: %s answers again, back in the answers", pin)
			}
		}
	}
}

func (h *pinHealth) stop() {
	if h == nil {
		return
	}
	h.mu.Lock()
	h.cancel()
	h.mu.Unlock()
	h.wg.Wait()
}

func pinnedAddresses(cfg *config.Config) []string {
	if cfg == nil {
		return nil
	}
	seen := make(map[string]struct{})
	out := make([]string, 0, 16)
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled || set.Routing.Enabled {
			continue
		}
		for _, ips := range set.DNS.Pins {
			for _, pin := range ips {
				ip := net.ParseIP(pin)
				if ip == nil {
					continue
				}
				if ip.To4() == nil && !cfg.Queue.IPv6Enabled {
					continue
				}
				key := ip.String()
				if _, dup := seen[key]; dup {
					continue
				}
				seen[key] = struct{}{}
				out = append(out, key)
			}
		}
	}
	sort.Strings(out)
	return out
}

func (p *Pool) checkDNSPins(cfg *config.Config) {
	if p == nil || p.state == nil || p.state.pinHealth == nil || cfg == nil || cfg.Queue.IsDiscovery {
		return
	}
	p.state.pinHealth.check(pinnedAddresses(cfg), int(cfg.MainInjectedMark()))
}
