package nfq

import (
	"context"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dhcp"
	"github.com/daniellavrushin/b4/log"
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

func NewPool(cfg *config.Config) *Pool {
	threads := cfg.Queue.Threads
	start := uint16(cfg.Queue.StartNum)
	if threads < 1 {
		threads = 1
	}

	matcher := buildMatcher(cfg)

	dhcpMgr := dhcp.NewManager()

	ws := make([]*Worker, 0, threads)
	for i := 0; i < threads; i++ {
		w := NewWorkerWithQueue(cfg, start+uint16(i))
		w.matcher.Store(matcher)
		w.ipToMac.Store(make(map[string]string))
		ws = append(ws, w)
	}

	pool := &Pool{Workers: ws, Dhcp: dhcpMgr}

	dhcpMgr.OnUpdate(func(ipToMAC map[string]string) {
		for _, w := range pool.Workers {
			w.ipToMac.Store(ipToMAC)
		}
		log.Infof("DHCP: updated %d IP->MAC mappings", len(ipToMAC))
	})

	dhcpMgr.Start()

	initialMappings := dhcpMgr.GetAllMappings()
	for _, w := range pool.Workers {
		w.ipToMac.Store(initialMappings)
	}
	log.Infof("DHCP: initial load %d IP->MAC mappings", len(initialMappings))

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			connState.Cleanup()
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

	matcher := buildMatcher(newCfg)

	for _, w := range p.Workers {
		w.cfg.Store(newCfg)
		w.matcher.Store(matcher)
	}
	return nil
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
