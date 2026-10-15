package dhcp

import (
	"context"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestNormalizeMAC(t *testing.T) {
	tests := []struct {
		input, want string
	}{
		{"aa:bb:cc:dd:ee:ff", "AA:BB:CC:DD:EE:FF"},
		{"AA:BB:CC:DD:EE:FF", "AA:BB:CC:DD:EE:FF"},
		{"aa-bb-cc-dd-ee-ff", "AA:BB:CC:DD:EE:FF"},
		{"Aa-Bb-Cc-Dd-Ee-Ff", "AA:BB:CC:DD:EE:FF"},
	}
	for _, tt := range tests {
		if got := normalizeMAC(tt.input); got != tt.want {
			t.Errorf("normalizeMAC(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func newTestManager() *Manager {
	ctx, cancel := context.WithCancel(context.Background())
	return &Manager{
		ipToMAC:   make(map[string]string),
		macToIP:   make(map[string]string),
		hostnames: make(map[string]string),
		ctx:       ctx,
		cancel:    cancel,
		refreshCh: make(chan struct{}, 1),
	}
}

func TestManagerGetMACForIP(t *testing.T) {
	m := newTestManager()
	m.ipToMAC["192.168.1.10"] = "AA:BB:CC:DD:EE:FF"

	if got := m.GetMACForIP("192.168.1.10"); got != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("got %q, want AA:BB:CC:DD:EE:FF", got)
	}
	if got := m.GetMACForIP("10.0.0.1"); got != "" {
		t.Errorf("got %q for unknown IP, want empty", got)
	}
}

func TestManagerGetIPForMAC(t *testing.T) {
	m := newTestManager()
	m.macToIP["AA:BB:CC:DD:EE:FF"] = "192.168.1.10"

	if got := m.GetIPForMAC("aa:bb:cc:dd:ee:ff"); got != "192.168.1.10" {
		t.Errorf("got %q, want 192.168.1.10", got)
	}
	if got := m.GetIPForMAC("aa-bb-cc-dd-ee-ff"); got != "192.168.1.10" {
		t.Errorf("got %q for dash-separated MAC, want 192.168.1.10", got)
	}
}

func TestManagerGetAllMappings(t *testing.T) {
	m := newTestManager()
	m.ipToMAC["192.168.1.10"] = "AA:BB:CC:DD:EE:FF"
	m.ipToMAC["192.168.1.20"] = "11:22:33:44:55:66"

	all := m.GetAllMappings()
	if len(all) != 2 {
		t.Fatalf("got %d mappings, want 2", len(all))
	}

	all["192.168.1.99"] = "MUTATED"
	if _, ok := m.ipToMAC["192.168.1.99"]; ok {
		t.Error("GetAllMappings returned reference to internal map")
	}
}

func TestManagerHostnames(t *testing.T) {
	m := newTestManager()
	m.hostnames["AA:BB:CC:DD:EE:FF"] = "my-phone"

	if got := m.GetHostnameForMAC("aa:bb:cc:dd:ee:ff"); got != "my-phone" {
		t.Errorf("got %q, want my-phone", got)
	}

	all := m.GetAllHostnames()
	if len(all) != 1 || all["AA:BB:CC:DD:EE:FF"] != "my-phone" {
		t.Errorf("GetAllHostnames unexpected: %v", all)
	}
}

func TestDuplicateMACDeduplication(t *testing.T) {
	m := newTestManager()

	entries := []ARPEntry{
		{IP: "192.168.50.21", MAC: "DC:56:E7:48:99:EC", Device: "br0"},
		{IP: "192.168.50.13", MAC: "DC:56:E7:48:99:EC", Device: "br0"},
	}

	for _, entry := range entries {
		mac := normalizeMAC(entry.MAC)
		m.ipToMAC[entry.IP] = mac
		m.macToIP[mac] = entry.IP
	}

	for ip, mac := range m.ipToMAC {
		if m.macToIP[mac] != ip {
			delete(m.ipToMAC, ip)
		}
	}

	if len(m.ipToMAC) != 1 {
		t.Fatalf("expected 1 entry after dedup, got %d: %v", len(m.ipToMAC), m.ipToMAC)
	}

	mac := "DC:56:E7:48:99:EC"
	survivingIP := m.macToIP[mac]
	if m.ipToMAC[survivingIP] != mac {
		t.Errorf("surviving IP %s not in ipToMAC", survivingIP)
	}
}

func TestCallbackReceivesSnapshot(t *testing.T) {
	m := newTestManager()
	m.ipToMAC["10.0.0.1"] = "AA:BB:CC:DD:EE:FF"

	var received map[string]string
	m.OnUpdate(func(snapshot map[string]string) {
		received = snapshot
	})

	m.notifyCallbacks()

	if received == nil || received["10.0.0.1"] != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("callback did not receive expected snapshot: %v", received)
	}

	received["10.0.0.1"] = "MUTATED"
	if m.ipToMAC["10.0.0.1"] != "AA:BB:CC:DD:EE:FF" {
		t.Error("callback snapshot mutation affected internal state")
	}
}

func TestManualDevicesMerge(t *testing.T) {
	m := newTestManager()
	m.ipToMAC["192.168.1.1"] = "AA:BB:CC:DD:EE:FF"
	m.macToIP["AA:BB:CC:DD:EE:FF"] = "192.168.1.1"

	m.SetManualDevices([]config.Device{
		{MAC: "02:B4:C0:A8:0A:01", IP: "192.168.10.1", Name: "server"},
	})

	// drain the refresh channel so we can call refresh manually
	select {
	case <-m.refreshCh:
	default:
	}

	m.mu.Lock()
	m.ipToMAC = map[string]string{"192.168.1.1": "AA:BB:CC:DD:EE:FF"}
	m.macToIP = map[string]string{"AA:BB:CC:DD:EE:FF": "192.168.1.1"}
	m.hostnames = make(map[string]string)
	m.addManualDevicesLocked()
	m.mu.Unlock()

	if mac := m.GetMACForIP("192.168.10.1"); mac != "02:B4:C0:A8:0A:01" {
		t.Errorf("manual device MAC = %q, want 02:B4:C0:A8:0A:01", mac)
	}
	if h := m.GetHostnameForMAC("02:B4:C0:A8:0A:01"); h != "server" {
		t.Errorf("manual device hostname = %q, want server", h)
	}
	if mac := m.GetMACForIP("192.168.1.1"); mac != "AA:BB:CC:DD:EE:FF" {
		t.Errorf("ARP device should still exist, got %q", mac)
	}
}

func TestAManualDeviceIsFoundByTheAddressFormItsPacketsCarry(t *testing.T) {
	m := newTestManager()
	m.manualDevices = []config.Device{
		{MAC: "02:B4:00:00:00:02", IP: "2001:DB8:0:0:0:0:0:0002", IsManual: true},
		{MAC: "02:B4:C0:A8:0A:05", IP: " ::ffff:192.168.10.5 ", IsManual: true},
		{MAC: "02:B4:00:00:00:09", IP: "not an address", IsManual: true},
	}
	m.mu.Lock()
	m.addManualDevicesLocked()
	m.mu.Unlock()

	for ip, want := range map[string]string{"2001:db8::2": "02:B4:00:00:00:02", "192.168.10.5": "02:B4:C0:A8:0A:05"} {
		if got := m.GetMACForIP(ip); got != want {
			t.Errorf("GetMACForIP(%q) = %q, want %q: b4 looks a client up by the form net.IP prints, not by the form typed into the settings", ip, got, want)
		}
	}
	if got := m.GetIPForMAC("02:B4:00:00:00:02"); got != "2001:db8::2" {
		t.Errorf("GetIPForMAC = %q, want the address in the form b4 matches it by", got)
	}
	if got := len(m.GetAllMappings()); got != 2 {
		t.Errorf("%d mappings, want 2: an entry that is not an address matches no packet", got)
	}
}

func TestIsAvailableWithManualDevices(t *testing.T) {
	m := newTestManager()
	if m.IsAvailable() {
		t.Error("empty manager should not be available")
	}

	m.SetManualDevices([]config.Device{
		{MAC: "02:B4:C0:A8:01:01", IP: "192.168.1.1", IsManual: true},
	})
	if !m.IsAvailable() {
		t.Error("manager with manual devices should be available")
	}
}

func TestSourceInfoWithManualDevices(t *testing.T) {
	m := newTestManager()
	name, _ := m.SourceInfo()
	if name != "" {
		t.Errorf("empty manager source = %q, want empty", name)
	}

	m.mu.Lock()
	m.manualDevices = []config.Device{{MAC: "02:B4:01:01:01:01", IP: "1.1.1.1"}}
	m.mu.Unlock()
	name, _ = m.SourceInfo()
	if name != "manual" {
		t.Errorf("manual-only source = %q, want manual", name)
	}

	m.available = true
	name, _ = m.SourceInfo()
	if name != "arp+manual" {
		t.Errorf("arp+manual source = %q, want arp+manual", name)
	}
}
