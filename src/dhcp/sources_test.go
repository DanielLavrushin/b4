package dhcp

import (
	"os"
	"testing"
)

func TestParseARPExcludesLocalInterfaces(t *testing.T) {
	content := `IP address       HW type     Flags       HW address            Mask     Device
192.168.31.1     0x1         0x2         a4:ba:70:a9:4c:b3     *        eth0
192.168.31.1     0x1         0x2         a4:ba:70:a9:4c:b3     *        wlan0
192.168.31.26    0x1         0x2         30:bb:7d:e0:92:bb     *        br-lan
192.168.31.99    0x1         0x2         a4:ba:70:a9:4c:b3     *        br-lan
192.168.31.50    0x1         0x2         de:ad:be:ef:00:01     *        br-lan
`
	f, err := os.CreateTemp("", "arp-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(content)
	f.Close()

	orig := localAddrs
	localAddrs = func() (map[string]struct{}, map[string]struct{}) {
		return map[string]struct{}{"A4:BA:70:A9:4C:B3": {}},
			map[string]struct{}{"192.168.31.50": {}}
	}
	defer func() { localAddrs = orig }()

	entries, err := parseARPFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after excluding local interfaces, got %d: %+v", len(entries), entries)
	}
	if entries[0].IP != "192.168.31.26" || entries[0].MAC != "30:BB:7D:E0:92:BB" {
		t.Errorf("unexpected entry: %+v", entries[0])
	}
}

func TestLocalRouterIPsKeepsPrivateOnly(t *testing.T) {
	orig := localAddrs
	localAddrs = func() (map[string]struct{}, map[string]struct{}) {
		return map[string]struct{}{},
			map[string]struct{}{
				"192.168.31.1": {},
				"127.0.0.1":    {},
				"8.8.8.8":      {},
				"::1":          {},
			}
	}
	defer func() { localAddrs = orig }()

	got := LocalRouterIPs()
	if len(got) != 1 || got[0] != "192.168.31.1" {
		t.Fatalf("expected only the private LAN IP, got %v", got)
	}
}

func TestParseDnsmasqHostnames(t *testing.T) {
	content := `1712345678 aa:bb:cc:dd:ee:ff 192.168.1.10 my-phone 01:aa:bb:cc:dd:ee:ff
1712345679 11:22:33:44:55:66 192.168.1.20 * 01:11:22:33:44:55:66
1712345680 aa:00:bb:11:cc:22 192.168.1.30 laptop 01:aa:00:bb:11:cc:22
`
	f, err := os.CreateTemp("", "dnsmasq-leases-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(content)
	f.Close()

	result := parseDnsmasqHostnames(f.Name())

	if len(result) != 2 {
		t.Fatalf("expected 2 hostnames, got %d: %v", len(result), result)
	}
	if result["AA:BB:CC:DD:EE:FF"] != "my-phone" {
		t.Errorf("expected my-phone, got %q", result["AA:BB:CC:DD:EE:FF"])
	}
	if result["AA:00:BB:11:CC:22"] != "laptop" {
		t.Errorf("expected laptop, got %q", result["AA:00:BB:11:CC:22"])
	}
}

func TestParseDnsmasqHostnames_NotFound(t *testing.T) {
	result := parseDnsmasqHostnames("/nonexistent/path")
	if result != nil {
		t.Errorf("expected nil for missing file, got %v", result)
	}
}

func TestParseISCHostnames(t *testing.T) {
	content := `lease 192.168.1.10 {
  hardware ethernet aa:bb:cc:dd:ee:ff;
  client-hostname "my-phone";
}
lease 192.168.1.20 {
  hardware ethernet 11:22:33:44:55:66;
}
lease 192.168.1.30 {
  hardware ethernet aa:00:bb:11:cc:22;
  client-hostname "laptop";
}
`
	f, err := os.CreateTemp("", "isc-leases-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(f.Name())
	f.WriteString(content)
	f.Close()

	result := parseISCHostnames(f.Name())

	if len(result) != 2 {
		t.Fatalf("expected 2 hostnames, got %d: %v", len(result), result)
	}
	if result["AA:BB:CC:DD:EE:FF"] != "my-phone" {
		t.Errorf("expected my-phone, got %q", result["AA:BB:CC:DD:EE:FF"])
	}
	if result["AA:00:BB:11:CC:22"] != "laptop" {
		t.Errorf("expected laptop, got %q", result["AA:00:BB:11:CC:22"])
	}
}

func TestParseISCHostnames_NotFound(t *testing.T) {
	result := parseISCHostnames("/nonexistent/path")
	if result != nil {
		t.Errorf("expected nil for missing file, got %v", result)
	}
}
