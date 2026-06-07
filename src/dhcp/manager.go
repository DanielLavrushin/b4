package dhcp

import (
	"context"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type Manager struct {
	available     bool
	ipToMAC       map[string]string
	macToIP       map[string]string
	hostnames     map[string]string // MAC → hostname
	manualDevices []config.Device
	mu            sync.RWMutex
	callbacks     []LeaseUpdateCallback
	ctx           context.Context
	cancel        context.CancelFunc
	refreshCh     chan struct{}
}

type DetectionResult struct {
	Available bool
	Source    string
	Path     string
}

func Detect() DetectionResult {
	if _, err := os.Stat(arpPath); err == nil {
		if entries, err := parseARP(); err == nil && len(entries) > 0 {
			return DetectionResult{
				Available: true,
				Source:    "arp",
				Path:     arpPath,
			}
		}
	}
	return DetectionResult{Available: false}
}

func NewManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())

	m := &Manager{
		ipToMAC:   make(map[string]string),
		macToIP:   make(map[string]string),
		hostnames: make(map[string]string),
		ctx:       ctx,
		cancel:    cancel,
		refreshCh: make(chan struct{}, 1),
	}

	if _, err := os.Stat(arpPath); err == nil {
		m.available = true
		log.Infof("DHCP: using ARP table at %s", arpPath)
	} else {
		log.Tracef("DHCP: ARP table not available: %v", err)
	}

	return m
}

func (m *Manager) Start() {
	m.refresh()

	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-m.ctx.Done():
				return
			case <-ticker.C:
				m.refresh()
			case <-m.refreshCh:
				m.refresh()
			}
		}
	}()

	m.mu.RLock()
	hasManual := len(m.manualDevices) > 0
	m.mu.RUnlock()
	switch {
	case m.available:
		log.Infof("DHCP manager started (source: arp)")
	case hasManual:
		log.Infof("DHCP manager started (manual devices only)")
	default:
		log.Infof("DHCP manager started (no DHCP sources available)")
	}
}

func (m *Manager) Stop() {
	m.cancel()
}

func (m *Manager) refresh() {
	m.mu.RLock()
	hasManual := len(m.manualDevices) > 0
	m.mu.RUnlock()

	var entries []ARPEntry
	if m.available {
		var err error
		entries, err = parseARP()
		if err != nil {
			log.Tracef("DHCP: ARP parse error: %v", err)
		}
	}

	if len(entries) == 0 && !hasManual {
		log.Tracef("DHCP: no ARP entries and no manual devices")
		return
	}

	leaseHostnames := enrichHostnames()

	m.mu.Lock()
	m.ipToMAC = make(map[string]string, len(entries))
	m.macToIP = make(map[string]string, len(entries))
	m.hostnames = make(map[string]string)

	for _, entry := range entries {
		mac := normalizeMAC(entry.MAC)
		m.ipToMAC[entry.IP] = mac
		m.macToIP[mac] = entry.IP
		if hostname, ok := leaseHostnames[mac]; ok {
			m.hostnames[mac] = hostname
		}
		log.Tracef("DHCP: %s -> %s (dev: %s)", entry.IP, mac, entry.Device)
	}

	for ip, mac := range m.ipToMAC {
		if m.macToIP[mac] != ip {
			delete(m.ipToMAC, ip)
			log.Tracef("DHCP: removed stale ARP entry %s -> %s", ip, mac)
		}
	}

	for _, d := range m.manualDevices {
		if d.IP == "" || d.MAC == "" {
			continue
		}
		mac := normalizeMAC(d.MAC)
		m.ipToMAC[d.IP] = mac
		m.macToIP[mac] = d.IP
		if d.Name != "" {
			m.hostnames[mac] = d.Name
		}
		log.Tracef("DHCP: manual device %s -> %s", d.IP, mac)
	}

	count := len(m.ipToMAC)
	m.mu.Unlock()

	log.Infof("DHCP: loaded %d entries from ARP table", count)
	m.notifyCallbacks()
}

func (m *Manager) TriggerRefresh() {
	select {
	case m.refreshCh <- struct{}{}:
	default:
	}
}

func (m *Manager) OnUpdate(cb LeaseUpdateCallback) {
	m.callbacks = append(m.callbacks, cb)
}

func (m *Manager) notifyCallbacks() {
	m.mu.RLock()
	snapshot := make(map[string]string, len(m.ipToMAC))
	for k, v := range m.ipToMAC {
		snapshot[k] = v
	}
	m.mu.RUnlock()

	for _, cb := range m.callbacks {
		cb(snapshot)
	}
}

func (m *Manager) GetMACForIP(ip string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.ipToMAC[ip]
}

func (m *Manager) GetIPForMAC(mac string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.macToIP[normalizeMAC(mac)]
}

func (m *Manager) GetAllMappings() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]string, len(m.ipToMAC))
	for k, v := range m.ipToMAC {
		result[k] = v
	}
	return result
}

func (m *Manager) IsAvailable() bool {
	if m.available {
		return true
	}
	m.mu.RLock()
	hasManual := len(m.manualDevices) > 0
	m.mu.RUnlock()
	return hasManual
}

func (m *Manager) SourceInfo() (name, path string) {
	m.mu.RLock()
	hasManual := len(m.manualDevices) > 0
	m.mu.RUnlock()

	if m.available && hasManual {
		return "arp+manual", arpPath
	}
	if m.available {
		return "arp", arpPath
	}
	if hasManual {
		return "manual", ""
	}
	return "", ""
}

func (m *Manager) RouterIPs() []string {
	return LocalRouterIPs()
}

func (m *Manager) GetHostnameForMAC(mac string) string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.hostnames[normalizeMAC(mac)]
}

func (m *Manager) GetAllHostnames() map[string]string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	result := make(map[string]string, len(m.hostnames))
	for k, v := range m.hostnames {
		result[k] = v
	}
	return result
}

func (m *Manager) SetManualDevices(devices []config.Device) {
	m.mu.Lock()
	m.manualDevices = devices
	m.mu.Unlock()
	m.TriggerRefresh()
}

func normalizeMAC(mac string) string {
	mac = strings.ToUpper(mac)
	mac = strings.ReplaceAll(mac, "-", ":")
	return mac
}
