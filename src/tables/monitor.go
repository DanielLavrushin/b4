package tables

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type Monitor struct {
	cfg      *config.Config
	stop     chan struct{}
	wg       sync.WaitGroup
	interval time.Duration
	backend  string
}

func NewMonitor(cfg *config.Config) *Monitor {
	interval := time.Duration(cfg.System.Tables.MonitorInterval) * time.Second
	if interval < time.Second {
		interval = 10 * time.Second
	}

	return &Monitor{
		cfg:      cfg,
		stop:     make(chan struct{}),
		interval: interval,
		backend:  detectFirewallBackend(),
	}
}

func (m *Monitor) Start() {
	if m.cfg.System.Tables.SkipSetup || m.cfg.System.Tables.MonitorInterval <= 0 {
		log.Infof("Tables monitor disabled")
		return
	}

	m.wg.Add(1)
	go m.monitorLoop()
	log.Infof("Started tables monitor (backend: %s, interval: %v)", m.backend, m.interval)
}

func (m *Monitor) Stop() {
	if m.cfg.System.Tables.SkipSetup || m.cfg.System.Tables.MonitorInterval <= 0 {
		return
	}

	close(m.stop)
	m.wg.Wait()
	log.Infof("Stopped tables monitor")
}

func (m *Monitor) monitorLoop() {
	defer m.wg.Done()

	time.Sleep(5 * time.Second)

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			if !m.checkRules() {
				log.Warnf("Tables rules missing, restoring...")
				if err := m.restoreRules(); err != nil {
					log.Errorf("Failed to restore tables rules: %v", err)
				} else {
					log.Infof("Tables rules restored successfully")
				}
			}
		}
	}
}

func (m *Monitor) checkRules() bool {
	if m.backend == "nftables" {
		return m.checkNFTablesRules()
	}
	return m.checkIPTablesRules()
}

func (m *Monitor) checkIPTablesRules() bool {
	ipts := []string{}
	if m.cfg.Queue.IPv4Enabled && hasBinary("iptables") {
		ipts = append(ipts, "iptables")
	}
	if m.cfg.Queue.IPv6Enabled && hasBinary("ip6tables") {
		ipts = append(ipts, "ip6tables")
	}
	if len(ipts) == 0 {
		return true
	}

	for _, ipt := range ipts {
		if _, err := run(ipt, "-w", "-t", "mangle", "-S", "B4"); err != nil {
			log.Tracef("Monitor: B4 chain missing")
			return false
		}

		if m.cfg.Queue.Devices.Enabled && len(m.cfg.Queue.Devices.Mac) > 0 {
			out, _ := run(ipt, "-w", "-t", "mangle", "-S", "FORWARD")
			if !strings.Contains(out, "B4") {
				log.Tracef("Monitor: FORWARD->B4 rule missing")
				return false
			}
		} else {
			if _, err := run(ipt, "-w", "-t", "mangle", "-C", "POSTROUTING", "-j", "B4"); err != nil {
				log.Tracef("Monitor: POSTROUTING->B4 rule missing")
				return false
			}
		}

		out, _ := run(ipt, "-w", "-t", "mangle", "-S", "PREROUTING")
		if !strings.Contains(out, "sport 53") || !strings.Contains(out, "sport 443") {
			log.Tracef("Monitor: PREROUTING response rules missing")
			return false
		}

		markHex := fmt.Sprintf("0x%x", m.cfg.Queue.Mark)
		if m.cfg.Queue.Mark == 0 {
			markHex = "0x8000"
		}

		out, _ = run(ipt, "-w", "-t", "mangle", "-S", "OUTPUT")
		if !strings.Contains(out, markHex) {
			log.Tracef("Monitor: OUTPUT mark accept rule missing")
			return false
		}
		if !strings.Contains(out, "-j B4") {
			log.Tracef("Monitor: OUTPUT->B4 jump rule missing")
			return false
		}
	}

	return true
}

func (m *Monitor) checkNFTablesRules() bool {
	nft := NewNFTablesManager(m.cfg)

	if !nft.tableExists() {
		log.Tracef("Monitor: nftables table missing")
		return false
	}

	if !nft.chainExists(nftChainName) {
		log.Tracef("Monitor: b4_chain missing")
		return false
	}

	if m.cfg.Queue.Devices.Enabled && len(m.cfg.Queue.Devices.Mac) > 0 {
		if !nft.chainExists("forward") {
			log.Tracef("Monitor: forward chain missing")
			return false
		}
		out, _ := nft.runNft("list", "chain", "inet", nftTableName, "forward")
		if !strings.Contains(out, nftChainName) {
			log.Tracef("Monitor: forward->b4_chain jump missing")
			return false
		}
	} else {
		if !nft.chainExists("postrouting") {
			log.Tracef("Monitor: postrouting chain missing")
			return false
		}
		out, _ := nft.runNft("list", "chain", "inet", nftTableName, "postrouting")
		if !strings.Contains(out, nftChainName) {
			log.Tracef("Monitor: postrouting->b4_chain jump missing")
			return false
		}
	}

	if !nft.chainExists("prerouting") {
		log.Tracef("Monitor: prerouting chain missing")
		return false
	}
	out, _ := nft.runNft("list", "chain", "inet", nftTableName, "prerouting")
	if !strings.Contains(out, "sport 53") || !strings.Contains(out, "sport 443") {
		log.Tracef("Monitor: prerouting response rules missing")
		return false
	}

	if !nft.chainExists("output") {
		log.Tracef("Monitor: output chain missing")
		return false
	}

	out, _ = nft.runNft("list", "chain", "inet", nftTableName, "output")
	if !strings.Contains(out, "accept") || !strings.Contains(out, nftChainName) {
		log.Tracef("Monitor: output mark accept rule missing")
		return false
	}

	return true
}

func (m *Monitor) restoreRules() error {
	return AddRules(m.cfg)
}

func (m *Monitor) ForceRestore() error {
	log.Infof("Manual rule restoration triggered")
	return m.restoreRules()
}
