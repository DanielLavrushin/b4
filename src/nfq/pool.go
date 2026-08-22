package nfq

import (
	"context"
	"os"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dhcp"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/sock"
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
		w.pendingHello = state.pendingHello
		w.hostHints = state.hostHints
		w.ipHealth = state.ipHealth
		w.goodIPs = state.goodIPs
		ws = append(ws, w)
	}

	pool := &Pool{Workers: ws, Dhcp: dhcpMgr, stopCleanup: make(chan struct{}), state: state}
	pool.startDNSTCP()
	pool.publishDNSTCPReady()

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
				retest := pool.retestInterval()
				pool.state.connState.Cleanup()
				pool.state.tlsCache.Cleanup()
				pool.state.destState.Cleanup(retest)
				pool.state.hostHints.Cleanup()
				pool.state.ipHealth.Cleanup(retest)
				pool.state.goodIPs.Cleanup()
			case <-escalationTicker.C:
				pool.state.pendingHello.Cleanup()
				m := metrics.GetMetricsCollector()
				m.UpdateEscalations(pool.GetEscalations())
				m.UpdateInjectStats(InjectOverloaded(), sock.SendDropped())
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

var DNSTCPReadyFunc func(v4, v6 bool)

func (p *Pool) publishDNSTCPReady() {
	if DNSTCPReadyFunc == nil {
		return
	}
	v4, v6 := p.DNSTCPReady()
	DNSTCPReadyFunc(v4, v6)
}

func (p *Pool) startDNSTCP() {
	if len(p.Workers) == 0 {
		return
	}
	cfg := p.Workers[0].getConfig()
	if cfg.Queue.IsDiscovery || !cfg.DNSTCPInterceptEnabled() {
		return
	}
	port := cfg.DNSTCPListenPort()
	srv := newDNSTCPServer(p.Workers[0], port)
	if err := srv.Start(); err != nil {
		log.Warnf("DNS: TCP listener could not bind port %d, DNS over TCP is left with the upstream resolver and no redirect rules are installed: %v", port, err)
		return
	}
	p.dnsTCP = srv
}

func (p *Pool) DNSTCPReady() (v4 bool, v6 bool) {
	if p.dnsTCP == nil {
		return false, false
	}
	return p.dnsTCP.ReadyV4(), p.dnsTCP.ReadyV6()
}

func (p *Pool) Stop() {
	if p.dnsTCP != nil {
		p.dnsTCP.Stop()
		p.dnsTCP = nil
	}

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

	if p.state != nil {
		p.state.ipHealth.Stop()
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

func logDomainOverlaps(sets []*config.SetConfig) {
	overlaps := sni.FindDomainOverlaps(sets)
	if len(overlaps) == 0 {
		return
	}

	const maxReported = 10
	shown := overlaps
	if len(shown) > maxReported {
		shown = shown[:maxReported]
	}
	for _, o := range shown {
		log.Warnf("Domain %q is targeted by %d enabled sets (%s): b4 applies the first match ('%s') and ignores the others for this domain, unless one of them has a matching source-device filter",
			o.Entry, len(o.SetNames), strings.Join(o.SetNames, ", "), o.SetNames[0])
	}
	if len(overlaps) > len(shown) {
		log.Warnf("... and %d more domain(s) targeted by several enabled sets", len(overlaps)-len(shown))
	}
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
		logDomainOverlaps(cfg.Sets)
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
		p.state.destState.PruneEscalations(func(setId string) bool {
			target := newCfg.GetSetById(setId)
			return target != nil && target.Enabled
		})
	}

	if p.Dhcp != nil {
		p.Dhcp.SetManualDevices(newCfg.Queue.Devices.ManualEntries())
	}

	p.reconcileDNSTCP(newCfg)

	return nil
}

func (p *Pool) reconcileDNSTCP(newCfg *config.Config) {
	wanted := !newCfg.Queue.IsDiscovery && newCfg.DNSTCPInterceptEnabled()

	if p.dnsTCP != nil {
		samePort := p.dnsTCP.port == newCfg.DNSTCPListenPort()
		sameFamilies := p.dnsTCP.ReadyV4() == newCfg.Queue.IPv4Enabled &&
			p.dnsTCP.ReadyV6() == newCfg.Queue.IPv6Enabled
		if wanted && samePort && sameFamilies {
			return
		}
		p.dnsTCP.Stop()
		p.dnsTCP = nil
	}

	if wanted {
		p.startDNSTCP()
	}
	p.publishDNSTCPReady()
}

func (p *Pool) GetIPBlockCache() IPBlockCache {
	return p.state.destState
}

func (p *Pool) retestInterval() time.Duration {
	cfg := p.GetFirstWorkerConfig()
	if cfg == nil {
		return config.DefaultIPHealthRetestSec * time.Second
	}
	return cfg.System.IPHealth.RetestInterval()
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
