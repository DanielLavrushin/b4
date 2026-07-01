package nfq

import (
	"context"
	"os"
	"reflect"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dhcp"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/sni"
)

func NewWorkerWithQueue(cfg *config.Config, qnum uint16) *Worker {
	ctx, cancel := context.WithCancel(context.Background())

	w := &Worker{
		qnum:   qnum,
		ctx:    ctx,
		cancel: cancel,
	}

	w.cfg.Store(cfg)

	return w
}

func (p *Pool) EnableTUNSourceResolver(wanIP string) {
	if p.tunSrc == nil {
		if f, err := os.Open(conntrackPath); err != nil {
			log.Warnf("TUN: per-device source attribution unavailable (%s not readable: %v); device logging/filtering will show the uplink address in TUN mode", conntrackPath, err)
			return
		} else {
			f.Close()
		}
		p.tunSrc = newTunSrcResolver(wanIP)
	} else {
		p.tunSrc.setWAN(wanIP)
	}
	for _, w := range p.Workers {
		w.srcResolver = p.tunSrc
	}
	log.Infof("TUN: source attribution enabled (recovering LAN source from conntrack; uplink %s)", wanIP)
}

func (p *Pool) UpdateTUNSourceWAN(wanIP string) {
	if p.tunSrc == nil || wanIP == "" {
		return
	}
	p.tunSrc.setWAN(wanIP)
}

func NewPool(cfg *config.Config) *Pool {
	threads := cfg.Queue.Threads
	start := uint16(cfg.Queue.StartNum)
	if threads < 1 {
		threads = 1
	}

	matcher := buildMatcher(cfg)

	dhcpMgr := dhcp.NewManager()

	state := newRuntimeState()
	ws := make([]*Worker, 0, threads)
	for i := 0; i < threads; i++ {
		w := NewWorkerWithQueue(cfg, start+uint16(i))
		w.matcher.Store(matcher)
		w.ipToMac.Store(make(map[string]string))
		w.tlsCache = state.tlsCache
		w.connTracker = state.connState
		w.destState = state.destState
		ws = append(ws, w)
	}

	pool := &Pool{Workers: ws, Dhcp: dhcpMgr, stopCleanup: make(chan struct{}), state: state}

	dhcpMgr.OnUpdate(func(ipToMAC map[string]string) {
		for _, w := range pool.Workers {
			w.ipToMac.Store(ipToMAC)
		}
		log.Infof("DHCP: updated %d IP->MAC mappings", len(ipToMAC))
	})

	dhcpMgr.SetManualDevices(cfg.Queue.Devices.ManualEntries())
	dhcpMgr.Start()

	initialMappings := dhcpMgr.GetAllMappings()
	for _, w := range pool.Workers {
		w.ipToMac.Store(initialMappings)
	}
	log.Infof("DHCP: initial load %d IP->MAC mappings", len(initialMappings))

	go func() {
		cleanupTicker := time.NewTicker(30 * time.Second)
		defer cleanupTicker.Stop()
		escalationTicker := time.NewTicker(2 * time.Second)
		defer escalationTicker.Stop()
		for {
			select {
			case <-cleanupTicker.C:
				pool.state.connState.Cleanup()
				pool.state.tlsCache.Cleanup()
				pool.state.destState.Cleanup(300 * time.Second)
			case <-escalationTicker.C:
				metrics.GetMetricsCollector().UpdateEscalations(pool.GetEscalations())
			case <-pool.stopCleanup:
				return
			}
		}
	}()

	return pool
}

func (p *Pool) Start() error {
	for _, w := range p.Workers {
		if err := w.Start(); err != nil {
			for _, x := range p.Workers {
				x.Stop()
			}
			return err
		}
	}
	return nil
}

func (p *Pool) Stop() {
	if p.Dhcp != nil {
		p.Dhcp.Stop()
	}

	// Stop the connState cleanup goroutine
	select {
	case <-p.stopCleanup:
		// already closed
	default:
		close(p.stopCleanup)
	}

	var wg sync.WaitGroup
	for _, w := range p.Workers {
		wg.Add(1)
		worker := w
		go func() {
			defer wg.Done()
			worker.Stop()
		}()
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	timeout := 5 * time.Second

	select {
	case <-done:
		log.Infof("All NFQueue workers stopped")
	case <-time.After(timeout):
		log.Errorf("Timeout (%v) waiting for NFQueue workers to stop", timeout)
	}
}

func (w *Worker) getConfig() *config.Config {
	return w.cfg.Load().(*config.Config)
}

func (w *Worker) getMatcher() *sni.SuffixSet {
	return w.matcher.Load().(*sni.SuffixSet)
}

func (w *Worker) UpdateConfig(newCfg *config.Config) {
	w.cfg.Store(newCfg)
}

func buildMatcher(cfg *config.Config) *sni.SuffixSet {
	if len(cfg.Sets) > 0 {
		m := sni.NewSuffixSet(cfg.Sets)
		totalDomains := 0
		totalIPs := 0
		for _, set := range cfg.Sets {
			totalDomains += len(set.Targets.DomainsToMatch)
			totalIPs += len(set.Targets.IpsToMatch)
		}
		log.Infof("Built matcher with %d domains and %d IPs across %d sets",
			totalDomains, totalIPs, len(cfg.Sets))
		return m
	}
	log.Tracef("Built empty matcher")
	return sni.NewSuffixSet([]*config.SetConfig{})
}

func (p *Pool) UpdateConfig(newCfg *config.Config) error {
	p.configMu.Lock()
	defer p.configMu.Unlock()

	var oldMatcher *sni.SuffixSet
	reuse := false
	if len(p.Workers) > 0 {
		oldMatcher = p.Workers[0].getMatcher()
		if oldCfg := p.Workers[0].getConfig(); oldCfg != nil {
			reuse = reflect.DeepEqual(oldCfg.Sets, newCfg.Sets)
		}
	}

	matcher := oldMatcher
	if !reuse {
		matcher = buildMatcher(newCfg)
		if oldMatcher != nil {
			matcher.TransferLearnedIPs(oldMatcher)
		}
	}

	for _, w := range p.Workers {
		w.cfg.Store(newCfg)
		w.matcher.Store(matcher)
	}

	if !reuse && p.state != nil && p.state.destState != nil {
		p.state.destState.ResetEscalations()
	}

	if p.Dhcp != nil {
		p.Dhcp.SetManualDevices(newCfg.Queue.Devices.ManualEntries())
	}

	return nil
}

func (p *Pool) GetIPBlockCache() IPBlockCache {
	return p.state.destState
}

func (p *Pool) GetEscalations() []metrics.EscalationEntry {
	if p.state == nil || p.state.destState == nil {
		return nil
	}
	cfg := p.GetFirstWorkerConfig()
	snaps := p.state.destState.ListEscalations()
	out := make([]metrics.EscalationEntry, 0, len(snaps))
	for _, s := range snaps {
		toName := s.SetId
		if cfg != nil {
			if set := cfg.GetSetById(s.SetId); set != nil && set.Name != "" {
				toName = set.Name
			}
		}
		out = append(out, metrics.EscalationEntry{
			Host:      s.Host,
			ToSet:     toName,
			Hops:      s.Hops,
			SetAt:     s.SetAt,
			ExpiresAt: s.ExpiresAt,
		})
	}
	return out
}

func (p *Pool) ClearEscalations() {
	if p.state != nil && p.state.destState != nil {
		p.state.destState.ResetEscalations()
	}
}

func (p *Pool) ClearEscalation(host string) {
	if p.state != nil && p.state.destState != nil {
		p.state.destState.ClearEscalation(host)
	}
}

func (p *Pool) GetMatcher() *sni.SuffixSet {
	if len(p.Workers) == 0 {
		return nil
	}
	return p.Workers[0].getMatcher()
}

func (p *Pool) GetFirstWorkerConfig() *config.Config {
	if len(p.Workers) == 0 {
		return nil
	}
	return p.Workers[0].getConfig()
}

func (w *Worker) GetCacheStats() map[string]interface{} {
	matcher := w.getMatcher()
	return matcher.GetCacheStats()
}
