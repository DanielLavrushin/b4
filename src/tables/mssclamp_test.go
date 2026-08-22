package tables

import (
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func mssClampSet(id string, size int, ips, macs []string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = id
	set.Enabled = true
	set.MSSClamp.Enabled = true
	set.MSSClamp.Size = size
	set.Targets.IPs = ips
	set.Targets.IpsToMatch = ips
	set.Targets.SourceDevices = macs
	return &set
}

func stubBinaries(t *testing.T, names ...string) {
	t.Helper()
	for _, name := range names {
		prev, had := hasBinaryCache.Load(name)
		t.Cleanup(func() {
			if had {
				hasBinaryCache.Store(name, prev)
			} else {
				hasBinaryCache.Delete(name)
			}
		})
		hasBinaryCache.Store(name, true)
	}
}

func mssSpecs(t *testing.T, cfg *config.Config, ipt string) []string {
	t.Helper()
	stubBinaries(t, backendIPTables, backendIP6Tables, "ipset")
	manager := NewIPTablesManager(cfg, false)
	_, rules := manager.buildMSSManifest("PREROUTING")
	var out []string
	for _, r := range rules {
		if r.IPT != ipt {
			continue
		}
		out = append(out, strings.Join(r.Spec, " "))
	}
	return out
}

func TestBuildMSSManifestSetScope(t *testing.T) {
	t.Run("a device-scoped set with no IPs clamps that device", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, nil, []string{"AA:BB:CC:DD:EE:FF"})}

		specs := mssSpecs(t, &cfg, "iptables")
		if len(specs) != 1 {
			t.Fatalf("expected 1 rule, got %d: %v", len(specs), specs)
		}
		if !strings.Contains(specs[0], "--mac-source AA:BB:CC:DD:EE:FF") {
			t.Errorf("expected MAC scope, got %q", specs[0])
		}
		if strings.Contains(specs[0], "--match-set") {
			t.Errorf("a set with no IPs has no ipset to match, got %q", specs[0])
		}
	})

	t.Run("a set with both scopes narrows to its addresses", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Sets = []*config.SetConfig{
			mssClampSet("s1", 88, []string{"203.0.113.7"}, []string{"AA:BB:CC:DD:EE:FF"}),
		}

		specs := mssSpecs(t, &cfg, "iptables")
		if len(specs) != 1 {
			t.Fatalf("expected 1 rule, got %d: %v", len(specs), specs)
		}
		if !strings.Contains(specs[0], "--match-set") || !strings.Contains(specs[0], "--mac-source") {
			t.Errorf("expected both scopes, got %q", specs[0])
		}
	})

	t.Run("an IPv4-scoped set emits no unscoped IPv6 rule", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.Queue.IPv4Enabled = true
		cfg.Queue.IPv6Enabled = true
		cfg.Sets = []*config.SetConfig{
			mssClampSet("s1", 88, []string{"203.0.113.7"}, []string{"AA:BB:CC:DD:EE:FF"}),
		}

		for _, spec := range mssSpecs(t, &cfg, "ip6tables") {
			if !strings.Contains(spec, "--match-set") {
				t.Errorf("a set scoped to IPv4 addresses must not clamp all IPv6 traffic from its devices, got %q", spec)
			}
		}
	})
}

func mssManualDevice(mac, ip string, clamp int) config.Device {
	return config.Device{MAC: mac, IP: ip, MSSClamp: clamp, IsManual: true}
}

func mssCfgWithDevices(devices ...config.Device) *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.Devices.Devices = devices
	return &cfg
}

func joinSpecs(specs []string) string { return strings.Join(specs, "\n") }

func TestBuildMSSManifestManualDevices(t *testing.T) {
	const manualMAC = "02:B4:0A:4D:01:2C"

	t.Run("a set scoped to a manual device matches its address", func(t *testing.T) {
		cfg := mssCfgWithDevices(mssManualDevice(manualMAC, "10.77.1.44", 0))
		cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, nil, []string{manualMAC})}

		specs := mssSpecs(t, cfg, "iptables")
		all := joinSpecs(specs)
		if strings.Contains(strings.ToUpper(all), "02:B4") {
			t.Fatalf("a placeholder MAC reached a clamp rule: %v", specs)
		}
		if !strings.Contains(all, "-s 10.77.1.44 -p tcp --dport 443") {
			t.Errorf("expected an outgoing rule scoped to the device address, got %v", specs)
		}
		if !strings.Contains(all, "-d 10.77.1.44 -p tcp --sport 443") {
			t.Errorf("expected a reply rule scoped to the device address, got %v", specs)
		}
	})

	t.Run("a manual device with no address contributes nothing", func(t *testing.T) {
		cfg := mssCfgWithDevices(mssManualDevice(manualMAC, "", 0))
		cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, nil, []string{manualMAC})}

		if specs := mssSpecs(t, cfg, "iptables"); len(specs) != 0 {
			t.Errorf("expected no rule for an unusable manual device, got %v", specs)
		}
	})

	t.Run("both scopes are kept in each direction", func(t *testing.T) {
		cfg := mssCfgWithDevices(mssManualDevice(manualMAC, "10.77.1.44", 0))
		cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, []string{"203.0.113.7"}, []string{manualMAC})}

		specs := mssSpecs(t, cfg, "iptables")
		if len(specs) != 2 {
			t.Fatalf("expected an outgoing and a reply rule, got %v", specs)
		}
		for _, spec := range specs {
			if !strings.Contains(spec, "--match-set") {
				t.Errorf("a set with addresses must stay narrowed to them, got %q", spec)
			}
		}
		if !strings.Contains(joinSpecs(specs), "--match-set b4_mss_0_v4 src") {
			t.Errorf("the reply rule must match the set on the source side, got %v", specs)
		}
	})

	t.Run("a discovered device keeps its MAC scope and gains no reply rule", func(t *testing.T) {
		cfg := mssCfgWithDevices(config.Device{MAC: "AA:BB:CC:DD:EE:FF"})
		cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, nil, []string{"AA:BB:CC:DD:EE:FF"})}

		specs := mssSpecs(t, cfg, "iptables")
		if len(specs) != 1 || !strings.Contains(specs[0], "--mac-source AA:BB:CC:DD:EE:FF") {
			t.Errorf("expected exactly the pre-existing MAC rule, got %v", specs)
		}
	})

	t.Run("an ipv6 manual device stays out of the ipv4 binary", func(t *testing.T) {
		cfg := mssCfgWithDevices(mssManualDevice("02:B4:00:00:00:05", "2001:db8::5", 0))
		cfg.Queue.IPv6Enabled = true
		cfg.Sets = []*config.SetConfig{mssClampSet("s1", 88, nil, []string{"02:B4:00:00:00:05"})}

		if specs := mssSpecs(t, cfg, "iptables"); len(specs) != 0 {
			t.Errorf("a v6 device must emit nothing into iptables, got %v", specs)
		}
		specs := mssSpecs(t, cfg, "ip6tables")
		if !strings.Contains(joinSpecs(specs), "-s 2001:db8::5") {
			t.Errorf("expected the v6 device in ip6tables, got %v", specs)
		}
	})
}

func TestBuildMSSManifestPerDeviceClamps(t *testing.T) {
	unscopedReply := "-p tcp --sport 443 --tcp-flags SYN,RST SYN -j TCPMSS --set-mss"

	t.Run("a manual device clamp is scoped in both directions", func(t *testing.T) {
		cfg := mssCfgWithDevices(mssManualDevice("02:B4:0A:4D:01:2C", "10.77.1.44", 900))

		specs := mssSpecs(t, cfg, "iptables")
		all := joinSpecs(specs)
		if strings.Contains(strings.ToUpper(all), "02:B4") {
			t.Fatalf("a placeholder MAC reached a clamp rule: %v", specs)
		}
		if !strings.Contains(all, "-s 10.77.1.44 -p tcp --dport 443") || !strings.Contains(all, "-d 10.77.1.44 -p tcp --sport 443") {
			t.Errorf("expected both directions scoped to the device, got %v", specs)
		}
		for _, spec := range specs {
			if strings.HasPrefix(spec, unscopedReply) {
				t.Errorf("a precisely scoped device needs no catch-all reply clamp, got %q", spec)
			}
		}
	})

	t.Run("a discovered device still needs the catch-all reply clamp", func(t *testing.T) {
		cfg := mssCfgWithDevices(config.Device{MAC: "AA:BB:CC:DD:EE:FF", MSSClamp: 900})

		specs := mssSpecs(t, cfg, "iptables")
		found := false
		for _, spec := range specs {
			if strings.HasPrefix(spec, unscopedReply) {
				found = true
			}
		}
		if !found {
			t.Errorf("a MAC-scoped device cannot scope its reply, so the catch-all must stay, got %v", specs)
		}
	})

	t.Run("an unusable manual device installs no catch-all reply clamp", func(t *testing.T) {
		cfg := mssCfgWithDevices(mssManualDevice("02:B4:00:00:00:01", "", 900))

		if specs := mssSpecs(t, cfg, "iptables"); len(specs) != 0 {
			t.Errorf("a device that produces no rule must not drag in a catch-all clamp, got %v", specs)
		}
	})
}

func TestBuildMSSManifestCatchAllReplySize(t *testing.T) {
	cfg := mssCfgWithDevices(
		mssManualDevice("02:B4:0A:4D:01:2C", "10.77.1.44", 88),
		config.Device{MAC: "AA:BB:CC:DD:EE:FF", MSSClamp: 1200},
	)

	specs := mssSpecs(t, cfg, "iptables")
	catchAll := ""
	for _, spec := range specs {
		if strings.HasPrefix(spec, "-p tcp --sport 443") {
			catchAll = spec
		}
	}
	if catchAll == "" {
		t.Fatalf("a MAC-scoped device still needs the catch-all reply clamp, got %v", specs)
	}
	if !strings.HasSuffix(catchAll, "--set-mss 1200") {
		t.Errorf("the catch-all must use the smallest size it actually covers, not one already handled by a scoped rule: %q", catchAll)
	}
	if !strings.Contains(joinSpecs(specs), "-d 10.77.1.44 -p tcp --sport 443") {
		t.Errorf("the manual device keeps its own scoped reply rule, got %v", specs)
	}
}
