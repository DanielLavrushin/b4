package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveToFile_And_LoadFromFile(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("save and load roundtrip", func(t *testing.T) {
		path := filepath.Join(tmpDir, "config.json")
		cfg := NewConfig()
		cfg.Queue.StartNum = 999
		cfg.Queue.Threads = 8

		if err := cfg.SaveToFile(path); err != nil {
			t.Fatalf("SaveToFile failed: %v", err)
		}

		loaded := NewConfig()
		if err := loaded.LoadFromFile(path); err != nil {
			t.Fatalf("LoadFromFile failed: %v", err)
		}

		if loaded.Queue.StartNum != 999 {
			t.Errorf("expected StartNum=999, got %d", loaded.Queue.StartNum)
		}
		if loaded.Queue.Threads != 8 {
			t.Errorf("expected Threads=8, got %d", loaded.Queue.Threads)
		}
	})

	t.Run("empty path does nothing", func(t *testing.T) {
		cfg := NewConfig()
		if err := cfg.SaveToFile(""); err != nil {
			t.Errorf("expected nil error for empty path, got %v", err)
		}
		if err := cfg.LoadFromFile(""); err != nil {
			t.Errorf("expected nil error for empty path, got %v", err)
		}
	})

	t.Run("load from nonexistent file", func(t *testing.T) {
		cfg := NewConfig()
		err := cfg.LoadFromFile(filepath.Join(tmpDir, "nonexistent.json"))
		if err == nil {
			t.Error("expected error for nonexistent file")
		}
	})

	t.Run("load from directory fails", func(t *testing.T) {
		cfg := NewConfig()
		err := cfg.LoadFromFile(tmpDir)
		if err == nil {
			t.Error("expected error when path is directory")
		}
	})

	t.Run("load malformed json", func(t *testing.T) {
		path := filepath.Join(tmpDir, "bad.json")
		os.WriteFile(path, []byte("{invalid json"), 0644)

		cfg := NewConfig()
		err := cfg.LoadFromFile(path)
		if err == nil {
			t.Error("expected error for malformed json")
		}
	})

	t.Run("save preserves empty sets", func(t *testing.T) {
		path := filepath.Join(tmpDir, "empty_sets.json")
		cfg := NewConfig()
		cfg.Sets = []*SetConfig{}

		if err := cfg.SaveToFile(path); err != nil {
			t.Fatalf("SaveToFile failed: %v", err)
		}

		loaded := NewConfig()
		loaded.LoadFromFile(path)
		if len(loaded.Sets) != 0 {
			t.Errorf("expected Sets to remain empty, got %d sets", len(loaded.Sets))
		}
	})
}

func TestValidate(t *testing.T) {
	t.Run("default config is valid", func(t *testing.T) {
		cfg := NewConfig()
		if err := cfg.Validate(); err != nil {
			t.Errorf("default config should be valid: %v", err)
		}
	})

	t.Run("threads < 1 fails", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Threads = 0
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for threads=0")
		}
	})

	t.Run("queue mark inside per-set space fails", func(t *testing.T) {
		cases := []uint{0x4000, 0x100, 0x20000, 0x22345, 0x27DFF}
		for _, m := range cases {
			cfg := NewConfig()
			cfg.Queue.Mark = m
			if err := cfg.Validate(); err == nil {
				t.Errorf("expected error for Queue.Mark=%#x (collides with per-set range)", m)
			}
		}
	})

	t.Run("queue mark outside per-set space passes", func(t *testing.T) {
		cases := []uint{0x8000, 0x10000, 0x28000, 0x80000000}
		for _, m := range cases {
			cfg := NewConfig()
			cfg.Queue.Mark = m
			if err := cfg.Validate(); err != nil {
				t.Errorf("Queue.Mark=%#x should pass, got: %v", m, err)
			}
		}
	})

	t.Run("queue num out of range", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.StartNum = -1
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for negative queue num")
		}

		cfg.Queue.StartNum = 70000
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for queue num > 65535")
		}
	})

	t.Run("geosite categories without path", func(t *testing.T) {
		cfg := NewConfig()
		testSet := NewSetConfig()
		testSet.Id = "test-set"
		testSet.Targets.GeoSiteCategories = []string{"youtube"}
		cfg.Sets = []*SetConfig{&testSet}
		cfg.System.Geo.GeoSitePath = ""

		if err := cfg.Validate(); err == nil {
			t.Error("expected error when geosite categories set without path")
		}
	})

	t.Run("geoip categories without path", func(t *testing.T) {
		cfg := NewConfig()
		testSet := NewSetConfig()
		testSet.Id = "test-set"
		testSet.Targets.GeoIpCategories = []string{"ru"}
		cfg.Sets = []*SetConfig{&testSet}
		cfg.System.Geo.GeoIpPath = ""

		if err := cfg.Validate(); err == nil {
			t.Error("expected error when geoip categories set without path")
		}
	})

	t.Run("set TCP ConnBytesLimit > queue limit gets capped", func(t *testing.T) {
		cfg := NewConfig()

		testSet := NewSetConfig()
		testSet.Id = "test-set"
		testSet.TCP.ConnBytesLimit = cfg.Queue.TCPConnBytesLimit + 10
		cfg.Sets = []*SetConfig{&testSet}

		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if testSet.TCP.ConnBytesLimit != cfg.Queue.TCPConnBytesLimit {
			t.Errorf("expected TCP ConnBytesLimit to be capped to %d, got %d",
				cfg.Queue.TCPConnBytesLimit, testSet.TCP.ConnBytesLimit)
		}
	})

	t.Run("set UDP ConnBytesLimit > queue limit gets capped", func(t *testing.T) {
		cfg := NewConfig()

		testSet := NewSetConfig()
		testSet.Id = "test-set"
		testSet.UDP.ConnBytesLimit = cfg.Queue.UDPConnBytesLimit + 10
		cfg.Sets = []*SetConfig{&testSet}

		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if testSet.UDP.ConnBytesLimit != cfg.Queue.UDPConnBytesLimit {
			t.Errorf("expected UDP ConnBytesLimit to be capped to %d, got %d",
				cfg.Queue.UDPConnBytesLimit, testSet.UDP.ConnBytesLimit)
		}
	})
	t.Run("empty sets is valid", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Sets = []*SetConfig{}
		if err := cfg.Validate(); err != nil {
			t.Errorf("empty sets should be valid: %v", err)
		}
	})

	t.Run("set without id fails", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Validate()

		secondSet := NewSetConfig()
		secondSet.Id = ""
		cfg.Sets = append(cfg.Sets, &secondSet)

		if err := cfg.Validate(); err == nil {
			t.Error("expected error for set without id")
		}
	})

	t.Run("web server port enables/disables", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.WebServer.Port = 0
		cfg.Validate()
		if cfg.System.WebServer.IsEnabled {
			t.Error("port 0 should disable web server")
		}

		cfg.System.WebServer.Port = 8080
		cfg.Validate()
		if !cfg.System.WebServer.IsEnabled {
			t.Error("valid port should enable web server")
		}

		cfg.System.WebServer.Port = 70000
		cfg.Validate()
		if cfg.System.WebServer.IsEnabled {
			t.Error("port > 65535 should disable web server")
		}
	})
}

func TestValidateMarks(t *testing.T) {
	t.Run("auto-derived discovery marks", func(t *testing.T) {
		cfg := NewConfig()
		if err := cfg.Validate(); err != nil {
			t.Fatalf("default config should be valid: %v", err)
		}
		if cfg.System.Checker.DiscoveryFlowMark != cfg.Queue.Mark+1 {
			t.Errorf("expected DiscoveryFlowMark=%d, got %d", cfg.Queue.Mark+1, cfg.System.Checker.DiscoveryFlowMark)
		}
		if cfg.System.Checker.DiscoveryInjectedMark != cfg.Queue.Mark+2 {
			t.Errorf("expected DiscoveryInjectedMark=%d, got %d", cfg.Queue.Mark+2, cfg.System.Checker.DiscoveryInjectedMark)
		}
	})

	t.Run("explicit discovery marks preserved", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Checker.DiscoveryFlowMark = 0xAAAA
		cfg.System.Checker.DiscoveryInjectedMark = 0xBBBB
		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.System.Checker.DiscoveryFlowMark != 0xAAAA {
			t.Errorf("expected DiscoveryFlowMark=0xAAAA, got 0x%x", cfg.System.Checker.DiscoveryFlowMark)
		}
		if cfg.System.Checker.DiscoveryInjectedMark != 0xBBBB {
			t.Errorf("expected DiscoveryInjectedMark=0xBBBB, got 0x%x", cfg.System.Checker.DiscoveryInjectedMark)
		}
	})

	t.Run("mark collision rejected", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Mark = 100
		cfg.System.Checker.DiscoveryFlowMark = 100
		cfg.System.Checker.DiscoveryInjectedMark = 200
		if err := cfg.Validate(); err == nil {
			t.Error("expected error when queue mark equals discovery flow mark")
		}
	})

	t.Run("discovery marks collision rejected", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Checker.DiscoveryFlowMark = 500
		cfg.System.Checker.DiscoveryInjectedMark = 500
		if err := cfg.Validate(); err == nil {
			t.Error("expected error when discovery marks are equal")
		}
	})

	t.Run("mark too high for auto-derived discovery marks", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Mark = uint(^uint32(0))
		if err := cfg.Validate(); err == nil {
			t.Error("expected error for mark at uint32 max with auto-derived discovery marks")
		}
	})

	t.Run("mark at uint32 max with explicit discovery marks", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Mark = uint(^uint32(0))
		cfg.System.Checker.DiscoveryFlowMark = 1
		cfg.System.Checker.DiscoveryInjectedMark = 2
		if err := cfg.Validate(); err != nil {
			t.Errorf("expected valid config with explicit discovery marks at max mark: %v", err)
		}
	})
}

func TestAppendIP(t *testing.T) {
	t.Run("appends new IPs", func(t *testing.T) {
		targets := &TargetsConfig{}
		targets.AppendIP([]string{"1.1.1.1", "8.8.8.8"})

		if len(targets.IPs) != 2 {
			t.Errorf("expected 2 IPs, got %d", len(targets.IPs))
		}
		if len(targets.IpsToMatch) != 2 {
			t.Errorf("expected 2 IpsToMatch, got %d", len(targets.IpsToMatch))
		}
	})

	t.Run("deduplicates IPs", func(t *testing.T) {
		targets := &TargetsConfig{
			IPs:        []string{"1.1.1.1"},
			IpsToMatch: []string{"1.1.1.1"},
		}
		targets.AppendIP([]string{"1.1.1.1", "8.8.8.8"})

		if len(targets.IPs) != 2 {
			t.Errorf("expected 2 IPs after dedup, got %d", len(targets.IPs))
		}
	})
}

func TestAppendSNI(t *testing.T) {
	t.Run("appends new SNI", func(t *testing.T) {
		targets := &TargetsConfig{}
		if err := targets.AppendSNI("example.com"); err != nil {
			t.Errorf("AppendSNI failed: %v", err)
		}

		if len(targets.SNIDomains) != 1 || targets.SNIDomains[0] != "example.com" {
			t.Error("SNI not appended to SNIDomains")
		}
		if len(targets.DomainsToMatch) != 1 {
			t.Error("SNI not appended to DomainsToMatch")
		}
	})

	t.Run("rejects duplicate in SNIDomains", func(t *testing.T) {
		targets := &TargetsConfig{
			SNIDomains: []string{"example.com"},
		}
		if err := targets.AppendSNI("example.com"); err == nil {
			t.Error("expected error for duplicate SNI")
		}
	})

	t.Run("rejects duplicate in DomainsToMatch", func(t *testing.T) {
		targets := &TargetsConfig{
			DomainsToMatch: []string{"example.com"},
		}
		if err := targets.AppendSNI("example.com"); err == nil {
			t.Error("expected error for duplicate in DomainsToMatch")
		}
	})
}

func TestGetSetById(t *testing.T) {
	cfg := NewConfig()
	set1 := NewSetConfig()
	set1.Id = "set-1"
	set2 := NewSetConfig()
	set2.Id = "set-2"
	cfg.Sets = []*SetConfig{&set1, &set2}

	t.Run("finds existing set", func(t *testing.T) {
		found := cfg.GetSetById("set-2")
		if found == nil || found.Id != "set-2" {
			t.Error("should find set-2")
		}
	})

	t.Run("returns nil for unknown id", func(t *testing.T) {
		if cfg.GetSetById("unknown") != nil {
			t.Error("should return nil for unknown id")
		}
	})
}

func TestGetTargetsForSet(t *testing.T) {
	t.Run("combines manual domains", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Targets.SNIDomains = []string{"a.com", "b.com"}

		domains, ips, err := cfg.GetTargetsForSet(&set)
		if err != nil {
			t.Fatalf("GetTargetsForSet failed: %v", err)
		}

		if len(domains) != 2 {
			t.Errorf("expected 2 domains, got %d", len(domains))
		}
		if len(ips) != 0 {
			t.Errorf("expected 0 ips, got %d", len(ips))
		}
	})

	t.Run("combines manual IPs", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Targets.IPs = []string{"1.1.1.1", "8.8.8.8"}

		domains, ips, err := cfg.GetTargetsForSet(&set)
		if err != nil {
			t.Fatalf("GetTargetsForSet failed: %v", err)
		}

		if len(domains) != 0 {
			t.Errorf("expected 0 domains, got %d", len(domains))
		}
		if len(ips) != 2 {
			t.Errorf("expected 2 ips, got %d", len(ips))
		}
	})
}

func TestLoadTargets(t *testing.T) {
	t.Run("skips disabled sets", func(t *testing.T) {
		cfg := NewConfig()

		enabled := NewSetConfig()
		enabled.Id = "enabled"
		enabled.Enabled = true
		enabled.Targets.SNIDomains = []string{"a.com"}

		disabled := NewSetConfig()
		disabled.Id = "disabled"
		disabled.Enabled = false
		disabled.Targets.SNIDomains = []string{"b.com"}

		cfg.Sets = []*SetConfig{&enabled, &disabled}

		sets, domainCount, _, err := cfg.LoadTargets()
		if err != nil {
			t.Fatalf("LoadTargets failed: %v", err)
		}

		if len(sets) != 1 {
			t.Errorf("expected 1 enabled set, got %d", len(sets))
		}
		if domainCount != 1 {
			t.Errorf("expected 1 domain from enabled set, got %d", domainCount)
		}
	})

	t.Run("aggregates counts from multiple sets", func(t *testing.T) {
		cfg := NewConfig()

		set1 := NewSetConfig()
		set1.Id = "set1"
		set1.Enabled = true
		set1.Targets.SNIDomains = []string{"a.com", "b.com"}
		set1.Targets.IPs = []string{"1.1.1.1"}

		set2 := NewSetConfig()
		set2.Id = "set2"
		set2.Enabled = true
		set2.Targets.SNIDomains = []string{"c.com"}
		set2.Targets.IPs = []string{"8.8.8.8", "8.8.4.4"}

		cfg.Sets = []*SetConfig{&set1, &set2}

		_, domainCount, ipCount, err := cfg.LoadTargets()
		if err != nil {
			t.Fatalf("LoadTargets failed: %v", err)
		}

		if domainCount != 3 {
			t.Errorf("expected 3 total domains, got %d", domainCount)
		}
		if ipCount != 3 {
			t.Errorf("expected 3 total ips, got %d", ipCount)
		}
	})
}

func TestApplyLogLevel(t *testing.T) {
	levels := []string{"debug", "trace", "info", "error", "silent", "unknown"}
	for _, level := range levels {
		t.Run(level, func(t *testing.T) {
			cfg := NewConfig()
			cfg.ApplyLogLevel(level) // just verify no panic
		})
	}
}

func TestHasGlobalMSSClamp(t *testing.T) {
	t.Run("disabled returns false", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Validate()
		ok, _ := cfg.HasGlobalMSSClamp()
		if ok {
			t.Error("expected false when MSS clamp is disabled")
		}
	})

	t.Run("enabled returns true with size", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.MSSClamp.Enabled = true
		cfg.Queue.MSSClamp.Size = 88
		cfg.Validate()

		ok, size := cfg.HasGlobalMSSClamp()
		if !ok {
			t.Error("expected true for enabled global MSS clamp")
		}
		if size != 88 {
			t.Errorf("expected size 88, got %d", size)
		}
	})

	t.Run("size zero returns false", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Validate()
		// Set size to 0 after Validate() since Validate() clamps size to min 10
		cfg.Queue.MSSClamp.Enabled = true
		cfg.Queue.MSSClamp.Size = 0

		ok, _ := cfg.HasGlobalMSSClamp()
		if ok {
			t.Error("expected false when size is 0")
		}
	})
}

func TestCollectDeviceMSSClamps(t *testing.T) {
	t.Run("works regardless of devices enabled", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Devices.Enabled = false
		cfg.Queue.Devices.Devices = []Device{
			{MAC: "AA:BB:CC:DD:EE:FF", MSSClamp: 88},
		}
		cfg.Validate()

		result := cfg.CollectDeviceMSSClamps()
		if len(result) != 1 || len(result[88]) != 1 {
			t.Error("expected MSS clamps to work even with devices disabled")
		}
	})

	t.Run("collects and groups by size", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Devices.Enabled = false
		cfg.Queue.Devices.Devices = []Device{
			{MAC: "AA:BB:CC:DD:EE:01", MSSClamp: 88},
			{MAC: "AA:BB:CC:DD:EE:02", MSSClamp: 88},
			{MAC: "AA:BB:CC:DD:EE:03", MSSClamp: 200},
		}
		cfg.Validate()

		result := cfg.CollectDeviceMSSClamps()
		if len(result[88]) != 2 {
			t.Errorf("expected 2 MACs for size 88, got %d", len(result[88]))
		}
		if len(result[200]) != 1 {
			t.Errorf("expected 1 MAC for size 200, got %d", len(result[200]))
		}
	})

	t.Run("skips empty mac and zero size", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Validate()
		cfg.Queue.Devices.Enabled = true
		cfg.Queue.Devices.Devices = []Device{
			{MAC: "", MSSClamp: 88},
			{MAC: "AA:BB:CC:DD:EE:FF", MSSClamp: 0},
		}

		result := cfg.CollectDeviceMSSClamps()
		if len(result) != 0 {
			t.Error("expected empty when MAC is empty or size is 0")
		}
	})
}

func TestCollectSetMSSClamps_ExcludeSkipsMACs(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "s1"
	set.Enabled = true
	set.MSSClamp.Enabled = true
	set.MSSClamp.Size = 88
	set.Targets.IpsToMatch = []string{"1.2.3.4"}
	set.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	set.Targets.SourceDevicesExclude = true
	cfg.Sets = []*SetConfig{&set}

	entries := cfg.CollectSetMSSClamps()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	if len(entries[0].MACs) != 0 {
		t.Errorf("excluded source devices must not become MAC scope, got %v", entries[0].MACs)
	}
	if len(entries[0].IPv4) != 1 {
		t.Errorf("expected IPv4 scope to remain, got %v", entries[0].IPv4)
	}
}

func TestMSSClampFingerprint(t *testing.T) {
	t.Run("stable ordering", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.MSSClamp.Enabled = true
		cfg.Queue.MSSClamp.Size = 88
		cfg.Queue.Devices.Enabled = true
		cfg.Queue.Devices.Devices = []Device{
			{MAC: "BB:BB:CC:DD:EE:FF", MSSClamp: 100},
			{MAC: "AA:BB:CC:DD:EE:FF", MSSClamp: 100},
		}
		cfg.Validate()

		fp1 := cfg.MSSClampFingerprint()
		fp2 := cfg.MSSClampFingerprint()
		if fp1 != fp2 {
			t.Errorf("fingerprint not stable: %q vs %q", fp1, fp2)
		}
	})

	t.Run("changes when config changes", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.MSSClamp.Enabled = true
		cfg.Queue.MSSClamp.Size = 88
		cfg.Validate()

		fp1 := cfg.MSSClampFingerprint()

		cfg.Queue.MSSClamp.Size = 100
		fp2 := cfg.MSSClampFingerprint()

		if fp1 == fp2 {
			t.Error("fingerprint should change when size changes")
		}
	})

	t.Run("includes global", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.MSSClamp.Enabled = true
		cfg.Queue.MSSClamp.Size = 88
		cfg.Validate()

		fp := cfg.MSSClampFingerprint()
		if fp == "" {
			t.Error("fingerprint should not be empty for global MSS clamp")
		}
		if !contains(fp, "global:88") {
			t.Errorf("fingerprint should contain 'global:88', got %q", fp)
		}
	})

	t.Run("includes per-device", func(t *testing.T) {
		cfg := NewConfig()
		cfg.Queue.Devices.Enabled = true
		cfg.Queue.Devices.Devices = []Device{
			{MAC: "AA:BB:CC:DD:EE:FF", MSSClamp: 88},
		}
		cfg.Validate()

		fp := cfg.MSSClampFingerprint()
		if !contains(fp, "dev:88:AA:BB:CC:DD:EE:FF") {
			t.Errorf("fingerprint should contain device entry, got %q", fp)
		}
	})
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsSubstr(s, substr))
}

func containsSubstr(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

func TestSelectedMACs(t *testing.T) {
	t.Run("returns only selected non-manual devices", func(t *testing.T) {
		dc := DevicesConfig{
			Devices: []Device{
				{MAC: "AA:BB:CC:DD:EE:01", Selected: true},
				{MAC: "AA:BB:CC:DD:EE:02", Selected: false},
				{MAC: "02:B4:C0:A8:01:01", Selected: true, IsManual: true},
			},
		}
		macs := dc.SelectedMACs()
		if len(macs) != 1 || macs[0] != "AA:BB:CC:DD:EE:01" {
			t.Errorf("expected [AA:BB:CC:DD:EE:01], got %v", macs)
		}
	})

	t.Run("returns nil when none selected", func(t *testing.T) {
		dc := DevicesConfig{
			Devices: []Device{
				{MAC: "AA:BB:CC:DD:EE:01", Selected: false},
			},
		}
		if macs := dc.SelectedMACs(); macs != nil {
			t.Errorf("expected nil, got %v", macs)
		}
	})
}

func TestFindByMAC(t *testing.T) {
	dc := DevicesConfig{
		Devices: []Device{
			{MAC: "AA:BB:CC:DD:EE:01", Name: "phone"},
			{MAC: "AA:BB:CC:DD:EE:02", Name: "laptop"},
		},
	}

	t.Run("finds existing device case-insensitive", func(t *testing.T) {
		d := dc.FindByMAC("aa:bb:cc:dd:ee:01")
		if d == nil || d.Name != "phone" {
			t.Error("expected to find phone")
		}
	})

	t.Run("returns nil for unknown MAC", func(t *testing.T) {
		if d := dc.FindByMAC("FF:FF:FF:FF:FF:FF"); d != nil {
			t.Error("expected nil")
		}
	})
}

func TestManualEntries(t *testing.T) {
	dc := DevicesConfig{
		Devices: []Device{
			{MAC: "AA:BB:CC:DD:EE:01", Selected: true},
			{MAC: "02:B4:C0:A8:01:01", IP: "192.168.1.1", IsManual: true},
			{MAC: "02:B4:C0:A8:01:02", IP: "192.168.1.2", IsManual: true},
		},
	}
	entries := dc.ManualEntries()
	if len(entries) != 2 {
		t.Errorf("expected 2 manual entries, got %d", len(entries))
	}
	for _, e := range entries {
		if !e.IsManual {
			t.Error("non-manual entry returned")
		}
	}
}

func TestBuildSetPortRanges(t *testing.T) {
	t.Run("parses TCP port filter", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "test"
		set.TCP.DPortFilter = "80,443,5222-5225"
		cfg.Sets = []*SetConfig{&set}

		cfg.BuildSetPortRanges()

		if len(set.TCPPortRanges) != 3 {
			t.Fatalf("expected 3 TCP port ranges, got %d", len(set.TCPPortRanges))
		}
		if set.TCPPortRanges[0].Min != 80 || set.TCPPortRanges[0].Max != 80 {
			t.Errorf("expected port 80, got %d-%d", set.TCPPortRanges[0].Min, set.TCPPortRanges[0].Max)
		}
		if set.TCPPortRanges[1].Min != 443 || set.TCPPortRanges[1].Max != 443 {
			t.Errorf("expected port 443, got %d-%d", set.TCPPortRanges[1].Min, set.TCPPortRanges[1].Max)
		}
		if set.TCPPortRanges[2].Min != 5222 || set.TCPPortRanges[2].Max != 5225 {
			t.Errorf("expected range 5222-5225, got %d-%d", set.TCPPortRanges[2].Min, set.TCPPortRanges[2].Max)
		}
	})

	t.Run("parses UDP port filter", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "test"
		set.UDP.DPortFilter = "443,1000-2000"
		cfg.Sets = []*SetConfig{&set}

		cfg.BuildSetPortRanges()

		if len(set.UDPPortRanges) != 2 {
			t.Fatalf("expected 2 UDP port ranges, got %d", len(set.UDPPortRanges))
		}
	})

	t.Run("empty filter produces no ranges", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "test"
		cfg.Sets = []*SetConfig{&set}

		cfg.BuildSetPortRanges()

		if len(set.TCPPortRanges) != 0 || len(set.UDPPortRanges) != 0 {
			t.Error("expected no port ranges for empty filters")
		}
	})
}

func TestMatchesTCPDPort(t *testing.T) {
	t.Run("no filter matches all ports", func(t *testing.T) {
		set := &SetConfig{}
		if !set.MatchesTCPDPort(443) {
			t.Error("expected match when no port filter set")
		}
		if !set.MatchesTCPDPort(80) {
			t.Error("expected match when no port filter set")
		}
	})

	t.Run("exact port match", func(t *testing.T) {
		set := &SetConfig{
			TCPPortRanges: []PortRange{{Min: 443, Max: 443}},
		}
		if !set.MatchesTCPDPort(443) {
			t.Error("expected match for port 443")
		}
		if set.MatchesTCPDPort(80) {
			t.Error("expected no match for port 80")
		}
	})

	t.Run("port range match", func(t *testing.T) {
		set := &SetConfig{
			TCPPortRanges: []PortRange{{Min: 5222, Max: 5225}},
		}
		if !set.MatchesTCPDPort(5222) {
			t.Error("expected match for port 5222")
		}
		if !set.MatchesTCPDPort(5224) {
			t.Error("expected match for port 5224")
		}
		if set.MatchesTCPDPort(5226) {
			t.Error("expected no match for port 5226")
		}
	})
}

func TestMatchesUDPDPort(t *testing.T) {
	t.Run("no filter matches all ports", func(t *testing.T) {
		set := &SetConfig{}
		if !set.MatchesUDPDPort(443) {
			t.Error("expected match when no port filter set")
		}
	})

	t.Run("exact port match", func(t *testing.T) {
		set := &SetConfig{
			UDPPortRanges: []PortRange{{Min: 443, Max: 443}},
		}
		if !set.MatchesUDPDPort(443) {
			t.Error("expected match for port 443")
		}
		if set.MatchesUDPDPort(80) {
			t.Error("expected no match for port 80")
		}
	})
}

func TestHasIPOrDomainTargets(t *testing.T) {
	t.Run("no targets", func(t *testing.T) {
		set := &SetConfig{}
		if set.HasIPOrDomainTargets() {
			t.Error("expected false when no targets")
		}
	})

	t.Run("has IPs", func(t *testing.T) {
		set := &SetConfig{}
		set.Targets.IpsToMatch = []string{"1.1.1.1"}
		if !set.HasIPOrDomainTargets() {
			t.Error("expected true when IPs set")
		}
	})

	t.Run("has domains", func(t *testing.T) {
		set := &SetConfig{}
		set.Targets.DomainsToMatch = []string{"example.com"}
		if !set.HasIPOrDomainTargets() {
			t.Error("expected true when domains set")
		}
	})

	t.Run("port-only set has no targets", func(t *testing.T) {
		set := &SetConfig{
			TCPPortRanges: []PortRange{{Min: 443, Max: 443}},
		}
		if set.HasIPOrDomainTargets() {
			t.Error("expected false for port-only set")
		}
	})
}

func TestResetToDefaults(t *testing.T) {
	set := NewSetConfig()
	set.Id = "custom"
	set.Name = "my-set"
	set.Targets.SNIDomains = []string{"keep.me"}
	set.Fragmentation.SNIPosition = 99

	set.ResetToDefaults()

	if set.Id != "custom" {
		t.Error("Id should be preserved")
	}
	if set.Name != "my-set" {
		t.Error("Name should be preserved")
	}
	if len(set.Targets.SNIDomains) != 1 || set.Targets.SNIDomains[0] != "keep.me" {
		t.Error("Targets should be preserved")
	}
	if set.Fragmentation.SNIPosition != DefaultSetConfig.Fragmentation.SNIPosition {
		t.Errorf("SNIPosition should reset to default %d, got %d",
			DefaultSetConfig.Fragmentation.SNIPosition, set.Fragmentation.SNIPosition)
	}
}

func TestCollectDuplicateIPs_Dedup(t *testing.T) {
	cfg := &Config{
		Sets: []*SetConfig{
			{
				Enabled: true,
				TCP: TCPConfig{
					Duplicate: DuplicateConfig{Enabled: true},
				},
			},
		},
	}
	cfg.Sets[0].Targets.IpsToMatch = []string{"1.2.3.4", "5.6.7.8", "1.2.3.4", "2001:db8::1", "2001:db8::1"}

	v4, v6 := cfg.CollectDuplicateIPs()
	if len(v4) != 2 {
		t.Errorf("expected 2 unique IPv4, got %d: %v", len(v4), v4)
	}
	if len(v6) != 1 {
		t.Errorf("expected 1 unique IPv6, got %d: %v", len(v6), v6)
	}
}

func TestEscalateTo_SanitizeMissing(t *testing.T) {
	cfg := NewConfig()
	a := NewSetConfig()
	a.Id = "a"
	a.Name = "A"
	a.Escalate.To = "missing-id"
	cfg.Sets = []*SetConfig{&a}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Sets[0].Escalate.To != "" {
		t.Fatalf("missing target should be cleared, got %q", cfg.Sets[0].Escalate.To)
	}
}

func TestEscalateTo_SanitizeSelfReference(t *testing.T) {
	cfg := NewConfig()
	a := NewSetConfig()
	a.Id = "a"
	a.Name = "A"
	a.Escalate.To = "a"
	cfg.Sets = []*SetConfig{&a}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Sets[0].Escalate.To != "" {
		t.Fatal("self-reference should be cleared")
	}
}

func TestEscalateTo_SanitizeDisabledTarget(t *testing.T) {
	cfg := NewConfig()
	a := NewSetConfig()
	a.Id = "a"
	a.Name = "A"
	a.Escalate.To = "b"
	b := NewSetConfig()
	b.Id = "b"
	b.Name = "B"
	b.Enabled = false
	cfg.Sets = []*SetConfig{&a, &b}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Sets[0].Escalate.To != "" {
		t.Fatal("escalate.to to a disabled set should be cleared")
	}
}

func TestEscalateTo_SanitizeBreaksCycle(t *testing.T) {
	cfg := NewConfig()
	a := NewSetConfig()
	a.Id = "a"
	a.Name = "A"
	a.Escalate.To = "b"
	b := NewSetConfig()
	b.Id = "b"
	b.Name = "B"
	b.Escalate.To = "a"
	cfg.Sets = []*SetConfig{&a, &b}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if hasEscalationCycle(&cfg) {
		t.Fatal("Validate should have broken the 2-cycle")
	}
}

func TestEscalateTo_SanitizeBreaksThreeCycle(t *testing.T) {
	cfg := NewConfig()
	a := NewSetConfig()
	a.Id = "a"
	a.Name = "A"
	a.Escalate.To = "b"
	b := NewSetConfig()
	b.Id = "b"
	b.Name = "B"
	b.Escalate.To = "c"
	c := NewSetConfig()
	c.Id = "c"
	c.Name = "C"
	c.Escalate.To = "a"
	cfg.Sets = []*SetConfig{&a, &b, &c}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if hasEscalationCycle(&cfg) {
		t.Fatal("Validate should have broken the 3-cycle A->B->C->A")
	}

	if got := cfg.GetSetById("a").Escalate.To; got != "b" {
		t.Fatalf("A->B should survive, got A->%q", got)
	}
	if got := cfg.GetSetById("b").Escalate.To; got != "c" {
		t.Fatalf("B->C should survive, got B->%q", got)
	}
	if got := cfg.GetSetById("c").Escalate.To; got != "" {
		t.Fatalf("cycle should be broken at C, got C->%q", got)
	}
}

func hasEscalationCycle(cfg *Config) bool {
	for _, s := range cfg.Sets {
		if s.Escalate.To == "" {
			continue
		}
		seen := map[string]bool{s.Id: true}
		cur := s
		for cur.Escalate.To != "" {
			if seen[cur.Escalate.To] {
				return true
			}
			seen[cur.Escalate.To] = true
			cur = cfg.GetSetById(cur.Escalate.To)
			if cur == nil {
				break
			}
		}
	}
	return false
}

func TestEscalateTo_SanitizeKeepsValid(t *testing.T) {
	cfg := NewConfig()
	a := NewSetConfig()
	a.Id = "a"
	a.Name = "A"
	a.Escalate.To = "b"
	b := NewSetConfig()
	b.Id = "b"
	b.Name = "B"
	cfg.Sets = []*SetConfig{&a, &b}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Sets[0].Escalate.To != "b" {
		t.Fatalf("valid escalation should be preserved, got %q", cfg.Sets[0].Escalate.To)
	}
}

func TestEscalateTo_RoundtripAndDefault(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "escalate.json")

	cfg := NewConfig()
	a := NewSetConfig()
	a.Id = "a"
	a.Name = "A"
	a.Escalate.To = "b"
	b := NewSetConfig()
	b.Id = "b"
	b.Name = "B"
	cfg.Sets = []*SetConfig{&a, &b}
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}

	loaded := NewConfig()
	if _, err := loaded.LoadWithMigration(path); err != nil {
		t.Fatalf("LoadWithMigration: %v", err)
	}
	if got := loaded.GetSetById("a"); got == nil || got.Escalate.To != "b" {
		t.Fatalf("EscalateTo not preserved: %+v", got)
	}
	if got := loaded.GetSetById("b"); got == nil || got.Escalate.To != "" {
		t.Fatalf("EscalateTo default should be empty, got %q", got.Escalate.To)
	}
}
