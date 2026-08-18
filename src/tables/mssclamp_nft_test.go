package tables

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func nftSpecs(t *testing.T, cfg *config.Config) []string {
	t.Helper()
	dir := t.TempDir()
	logPath := filepath.Join(dir, "argv.log")
	stub := "#!/bin/sh\nprintf '%s\\n' \"$*\" >> " + logPath + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(dir, "nft"), []byte(stub), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+":"+os.Getenv("PATH"))
	if err := NewNFTablesManager(cfg).ApplyMSSClamp(); err != nil {
		t.Fatalf("ApplyMSSClamp: %v", err)
	}
	raw, err := os.ReadFile(logPath)
	if err != nil {
		return nil
	}
	var out []string
	for _, line := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(line, "add rule") {
			out = append(out, line)
		}
	}
	return out
}

func TestNftMSSClampManualDevices(t *testing.T) {
	const manualMAC = "02:B4:0A:4D:01:2C"

	t.Run("a set scoped to a manual device matches its address in both directions", func(t *testing.T) {
		cfg := mssCfgWithDevices(mssManualDevice(manualMAC, "10.77.1.44", 0))
		cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, nil, []string{manualMAC})}

		rules := nftSpecs(t, cfg)
		all := strings.Join(rules, "\n")
		if strings.Contains(strings.ToUpper(all), "02:B4") {
			t.Fatalf("a placeholder MAC reached an nft rule: %v", rules)
		}
		if !strings.Contains(all, "ip saddr 10.77.1.44 tcp dport 443") {
			t.Errorf("expected an outgoing rule scoped to the device address, got %v", rules)
		}
		if !strings.Contains(all, "ip daddr 10.77.1.44 tcp sport 443") {
			t.Errorf("expected a reply rule scoped to the device address, got %v", rules)
		}
		if strings.Contains(all, "ether") {
			t.Errorf("a manual device must never produce an ether match, got %v", rules)
		}
	})

	t.Run("a discovered device keeps its ether matches", func(t *testing.T) {
		cfg := mssCfgWithDevices(config.Device{MAC: "AA:BB:CC:DD:EE:FF"})
		cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, nil, []string{"AA:BB:CC:DD:EE:FF"})}

		all := strings.Join(nftSpecs(t, cfg), "\n")
		if !strings.Contains(all, "ether saddr AA:BB:CC:DD:EE:FF") || !strings.Contains(all, "ether daddr AA:BB:CC:DD:EE:FF") {
			t.Errorf("expected the pre-existing ether matches, got %q", all)
		}
	})

	t.Run("a per-device clamp on a manual device is scoped by address", func(t *testing.T) {
		cfg := mssCfgWithDevices(mssManualDevice(manualMAC, "10.77.1.44", 900))

		all := strings.Join(nftSpecs(t, cfg), "\n")
		if strings.Contains(strings.ToUpper(all), "02:B4") {
			t.Fatalf("a placeholder MAC reached an nft rule: %v", all)
		}
		if !strings.Contains(all, "ip saddr 10.77.1.44 tcp dport 443") || !strings.Contains(all, "ip daddr 10.77.1.44 tcp sport 443") {
			t.Errorf("expected both directions scoped to the device, got %q", all)
		}
	})

	t.Run("a manual device without an address contributes nothing", func(t *testing.T) {
		cfg := mssCfgWithDevices(mssManualDevice(manualMAC, "", 900))
		if rules := nftSpecs(t, cfg); len(rules) != 0 {
			t.Errorf("expected no rule for an unusable manual device, got %v", rules)
		}
	})

	t.Run("an ipv6 manual device produces no ipv4 set rule", func(t *testing.T) {
		cfg := mssCfgWithDevices(mssManualDevice("02:B4:00:00:00:05", "2001:db8::5", 0))
		cfg.Queue.IPv6Enabled = true
		cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, []string{"203.0.113.7"}, []string{"02:B4:00:00:00:05"})}

		all := strings.Join(nftSpecs(t, cfg), "\n")
		if strings.Contains(all, "b4_mss_0_v4") {
			t.Errorf("a set whose only source is an ipv6 device must not emit ipv4 set rules, got %q", all)
		}
	})
}
