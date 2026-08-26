package tables

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type Monitor struct {
	cfgPtr   *atomic.Pointer[config.Config]
	stop     chan struct{}
	wg       sync.WaitGroup
	interval time.Duration
	backend  string

	started bool

	keeper *RoutingKeeper

	linkWatcher *linkWatcher
}

type ifaceSnapshot struct {
	v4 string
	v6 string
}

var (
	dnsResponsePortMatch = "sport 53"
	dnsRequestPortMatch  = "dport 53"
)

func NewMonitor(cfgPtr *atomic.Pointer[config.Config]) *Monitor {
	cfg := cfgPtr.Load()
	interval := time.Duration(cfg.System.Tables.MonitorInterval) * time.Second
	if interval < time.Second {
		interval = 10 * time.Second
	}

	return &Monitor{
		cfgPtr:      cfgPtr,
		stop:        make(chan struct{}),
		interval:    interval,
		backend:     detectFirewallBackend(cfg),
		keeper:      NewRoutingKeeper(),
		linkWatcher: newLinkWatcher(cfgPtr),
	}
}

func (m *Monitor) Start() {
	if m.started {
		return
	}
	cfg := m.cfgPtr.Load()
	if cfg.System.Tables.SkipSetup || cfg.System.Tables.MonitorInterval <= 0 {
		log.Infof("Tables monitor disabled")
		return
	}

	m.started = true
	m.wg.Add(1)
	go m.monitorLoop()
	log.Infof("Started tables monitor (backend: %s, interval: %v)", m.backend, m.interval)

	if m.linkWatcher != nil {
		if err := m.linkWatcher.Start(); err != nil {
			log.Warnf("Link watcher failed to start, falling back to periodic monitoring only: %v", err)
			m.linkWatcher = nil
		} else {
			log.Infof("Started link watcher (RTNETLINK RTMGRP_LINK)")
		}
	}
}

func (m *Monitor) Stop() {
	if !m.started {
		return
	}

	if m.linkWatcher != nil {
		m.linkWatcher.Stop()
	}
	close(m.stop)
	m.wg.Wait()
	log.Infof("Stopped tables monitor")
}

func (m *Monitor) monitorLoop() {
	defer m.wg.Done()

	select {
	case <-m.stop:
		return
	case <-time.After(5 * time.Second):
	}

	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()

	m.keeper.Resnapshot(m.cfgPtr.Load())

	for {
		select {
		case <-m.stop:
			return
		case <-ticker.C:
			cfg := m.cfgPtr.Load()
			if !m.checkRules(cfg) {
				log.Warnf("Tables rules missing, restoring...")
				if err := m.restoreRules(cfg); err != nil {
					log.Errorf("Failed to restore tables rules: %v", err)
				} else {
					log.Infof("Tables rules restored successfully")
				}
				m.keeper.Resnapshot(cfg)
			}

			m.keeper.Reconcile(cfg)
		}
	}
}

func (m *Monitor) checkRules(cfg *config.Config) bool {
	if m.backend == backendNFTables {
		return m.checkNFTablesRules(cfg)
	}
	return m.checkIPTablesRules(cfg)
}

func (m *Monitor) checkIPTablesRules(cfg *config.Config) bool {
	legacy := m.backend == backendIPTablesLegacy
	ipt4 := backendIPTables
	ipt6 := backendIP6Tables
	if legacy {
		ipt4 = backendIPTablesLegacy
		ipt6 = backendIP6TablesLegacy
	}
	ipts := []string{}
	if cfg.Queue.IPv4Enabled && hasBinary(ipt4) {
		ipts = append(ipts, ipt4)
	}
	if cfg.Queue.IPv6Enabled && hasBinary(ipt6) {
		ipts = append(ipts, ipt6)
	}
	if len(ipts) == 0 {
		return true
	}

	for _, ipt := range ipts {
		if _, err := run(ipt, "-w", "-t", "mangle", "-S", "B4"); err != nil {
			log.Tracef("Monitor: B4 chain missing")
			return false
		}

		if cfg.Queue.Devices.Enabled && len(cfg.Queue.Devices.SelectedMACs()) > 0 {
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

		if _, err := run(ipt, "-w", "-t", "mangle", "-S", "B4_PREROUTING"); err != nil {
			log.Tracef("Monitor: B4_PREROUTING chain missing")
			return false
		}
		if _, err := run(ipt, "-w", "-t", "mangle", "-C", "PREROUTING", "-j", "B4_PREROUTING"); err != nil {
			log.Tracef("Monitor: PREROUTING->B4_PREROUTING jump missing")
			return false
		}
		out, _ := run(ipt, "-w", "-t", "mangle", "-S", "B4_PREROUTING")
		hasDNSResponse := strings.Contains(out, dnsResponsePortMatch) && strings.Contains(out, "NFQUEUE")
		hasDNSRequest := strings.Contains(out, dnsRequestPortMatch) && strings.Contains(out, "NFQUEUE")
		hasTCP := strings.Contains(out, "tcp") && strings.Contains(out, "NFQUEUE")
		if !hasDNSResponse || !hasDNSRequest || !hasTCP {
			log.Tracef("Monitor: B4_PREROUTING rules missing (dnsReq=%v, dnsResp=%v, tcp=%v)", hasDNSRequest, hasDNSResponse, hasTCP)
			return false
		}

		markHex := fmt.Sprintf("0x%x", cfg.Queue.Mark)
		if cfg.Queue.Mark == 0 {
			markHex = "0x8000"
		}

		out, _ = run(ipt, "-w", "-t", "mangle", "-S", "OUTPUT")
		if !strings.Contains(out, dnsResponsePortMatch) || !strings.Contains(out, "NFQUEUE") {
			log.Tracef("Monitor: OUTPUT DNS response rule missing")
			return false
		}
		if !strings.Contains(out, markHex) {
			log.Tracef("Monitor: OUTPUT mark accept rule missing")
			return false
		}
		if !strings.Contains(out, "-j B4") {
			log.Tracef("Monitor: OUTPUT->B4 jump rule missing")
			return false
		}

		if cfg.System.Tables.Masquerade.Enabled {
			postOut, _ := run(ipt, "-w", "-t", "nat", "-S", "POSTROUTING")
			masqOut, _ := run(ipt, "-w", "-t", "nat", "-S", masqChainName)
			if !masqueradeRulesPresent(postOut, masqOut) {
				log.Tracef("Monitor: masquerade rules missing (POSTROUTING jump or %s chain)", masqChainName)
				return false
			}
		}

		if dnsTCPEnabledFamily(cfg, ipt == ipt6) && hasNATRedirectSupport(ipt, cfg.DNSTCPListenPort()) {
			if !dnsTCPRulesPresent(ipt, cfg.DNSTCPListenPort()) {
				log.Tracef("Monitor[%s]: DNS-over-TCP redirect rules missing (PREROUTING/OUTPUT jump or %s chain)", ipt, dnsTCPChainName)
				return false
			}
		}

		global, _ := cfg.HasGlobalMSSClamp()
		deviceClamps := cfg.CollectDeviceMSSClamps()
		if global || len(deviceClamps) > 0 {
			out, _ := run(ipt, "-w", "-t", "mangle", "-S", "OUTPUT")
			fwdOut, _ := run(ipt, "-w", "-t", "mangle", "-S", "FORWARD")
			if !strings.Contains(out, "TCPMSS") && !strings.Contains(fwdOut, "TCPMSS") {
				log.Tracef("Monitor: MSS clamp rule missing")
				return false
			}
		}
	}

	return true
}

func (m *Monitor) checkNFTablesRules(cfg *config.Config) bool {
	nft := NewNFTablesManager(cfg)

	if !nft.tableExists() {
		log.Tracef("Monitor: nftables table missing")
		return false
	}

	if !nft.chainExists(nftChainName) {
		log.Tracef("Monitor: b4_chain missing")
		return false
	}

	if cfg.Queue.Devices.Enabled && len(cfg.Queue.Devices.SelectedMACs()) > 0 {
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
	hasDNSResponse := strings.Contains(out, dnsResponsePortMatch) && strings.Contains(out, "queue")
	hasDNSRequest := strings.Contains(out, dnsRequestPortMatch) && strings.Contains(out, "queue")
	hasTCP := strings.Contains(out, "tcp sport") && strings.Contains(out, "queue")
	if !hasDNSResponse || !hasDNSRequest || !hasTCP {
		log.Tracef("Monitor: prerouting rules missing (dnsReq=%v, dnsResp=%v, tcp=%v)", hasDNSRequest, hasDNSResponse, hasTCP)
		return false
	}

	if !nft.chainExists("output") {
		log.Tracef("Monitor: output chain missing")
		return false
	}

	out, _ = nft.runNft("list", "chain", "inet", nftTableName, "output")
	if !strings.Contains(out, dnsResponsePortMatch) || !strings.Contains(out, "queue") {
		log.Tracef("Monitor: output DNS response rule missing")
		return false
	}
	if !strings.Contains(out, "accept") || !strings.Contains(out, nftChainName) {
		log.Tracef("Monitor: output mark accept rule missing")
		return false
	}

	if cfg.System.Tables.Masquerade.Enabled {
		if !nft.natTableExists() {
			log.Tracef("Monitor: nftables nat table missing")
			return false
		}
		natOut, _ := nft.runNft("list", "table", "ip", nftNatTableName)
		if !strings.Contains(natOut, "masquerade") {
			log.Tracef("Monitor: masquerade rule missing")
			return false
		}
	}

	for _, v6 := range []bool{false, true} {
		if !dnsTCPEnabledFamily(cfg, v6) {
			continue
		}
		if (v6 && !cfg.Queue.IPv6Enabled) || (!v6 && !cfg.Queue.IPv4Enabled) {
			continue
		}
		family, table := nftDNSTCPTable(v6)
		if !nft.dnsTCPFamilyExists(v6) {
			log.Tracef("Monitor: DNS-over-TCP nat table missing (%s)", family)
			return false
		}
		dnsOut, err := nft.runNft("list", "table", family, table)
		if err == nil && !strings.Contains(dnsOut, "redirect") {
			log.Tracef("Monitor: DNS-over-TCP redirect rule missing (%s)", family)
			return false
		}
	}

	global, _ := cfg.HasGlobalMSSClamp()
	deviceClamps := cfg.CollectDeviceMSSClamps()
	if global || len(deviceClamps) > 0 {
		out, _ = nft.runNft("list", "chain", "inet", nftTableName, "output")
		forwardOut := ""
		if nft.chainExists("forward") {
			forwardOut, _ = nft.runNft("list", "chain", "inet", nftTableName, "forward")
		}
		if !strings.Contains(out, "maxseg") && !strings.Contains(forwardOut, "maxseg") {
			log.Tracef("Monitor: MSS clamp rule missing")
			return false
		}
	}

	return true
}

func (m *Monitor) restoreRules(cfg *config.Config) error {
	return AddRules(cfg)
}

func (m *Monitor) ForceRestore() error {
	log.Infof("Manual rule restoration triggered")
	return m.restoreRules(m.cfgPtr.Load())
}
