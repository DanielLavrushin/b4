package tables

import (
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func testCfgPtr(cfg *config.Config) *atomic.Pointer[config.Config] {
	p := &atomic.Pointer[config.Config]{}
	p.Store(cfg)
	return p
}

func TestIPTablesManager_BuildNFQSpec(t *testing.T) {
	cfg := config.NewConfig()
	manager := NewIPTablesManager(&cfg, false)

	t.Run("single thread", func(t *testing.T) {
		spec := manager.buildNFQSpec(100, 1)

		expected := []string{"-j", "NFQUEUE", "--queue-num", "100", "--queue-bypass"}
		if len(spec) != len(expected) {
			t.Fatalf("expected %d elements, got %d", len(expected), len(spec))
		}
		for i, v := range expected {
			if spec[i] != v {
				t.Errorf("spec[%d] = %q, want %q", i, spec[i], v)
			}
		}
	})

	t.Run("multiple threads", func(t *testing.T) {
		spec := manager.buildNFQSpec(100, 4)

		expected := []string{"-j", "NFQUEUE", "--queue-balance", "100:103", "--queue-bypass"}
		if len(spec) != len(expected) {
			t.Fatalf("expected %d elements, got %d", len(expected), len(spec))
		}
		for i, v := range expected {
			if spec[i] != v {
				t.Errorf("spec[%d] = %q, want %q", i, spec[i], v)
			}
		}
	})

	t.Run("queue balance range calculation", func(t *testing.T) {
		spec := manager.buildNFQSpec(537, 8)

		// Should be 537:544 (537 + 8 - 1 = 544)
		if spec[3] != "537:544" {
			t.Errorf("expected queue-balance 537:544, got %s", spec[3])
		}
	})
}

func TestNFTablesManager_BuildNFQueueAction(t *testing.T) {
	t.Run("single thread", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.Queue.StartNum = 100
		cfg.Queue.Threads = 1
		manager := NewNFTablesManager(&cfg)

		action := manager.buildNFQueueAction()
		expected := "queue num 100 bypass"
		if action != expected {
			t.Errorf("got %q, want %q", action, expected)
		}
	})

	t.Run("multiple threads", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.Queue.StartNum = 100
		cfg.Queue.Threads = 4
		manager := NewNFTablesManager(&cfg)

		action := manager.buildNFQueueAction()
		expected := "queue num 100-103 bypass"
		if action != expected {
			t.Errorf("got %q, want %q", action, expected)
		}
	})
}

func TestNewIPTablesManager(t *testing.T) {
	cfg := config.NewConfig()

	t.Run("standard", func(t *testing.T) {
		manager := NewIPTablesManager(&cfg, false)
		if manager == nil {
			t.Fatal("expected non-nil manager")
		}
		if manager.cfg != &cfg {
			t.Error("manager.cfg not set correctly")
		}
		if manager.useLegacy {
			t.Error("useLegacy should be false")
		}
	})

	t.Run("legacy", func(t *testing.T) {
		manager := NewIPTablesManager(&cfg, true)
		if manager == nil {
			t.Fatal("expected non-nil manager")
		}
		if !manager.useLegacy {
			t.Error("useLegacy should be true")
		}
	})
}

func TestIPTablesManager_BinaryNames(t *testing.T) {
	cfg := config.NewConfig()

	t.Run("standard binaries", func(t *testing.T) {
		manager := NewIPTablesManager(&cfg, false)
		if manager.iptablesBin() != backendIPTables {
			t.Errorf("expected iptables, got %s", manager.iptablesBin())
		}
		if manager.ip6tablesBin() != backendIP6Tables {
			t.Errorf("expected ip6tables, got %s", manager.ip6tablesBin())
		}
	})

	t.Run("legacy binaries", func(t *testing.T) {
		manager := NewIPTablesManager(&cfg, true)
		if manager.iptablesBin() != backendIPTablesLegacy {
			t.Errorf("expected iptables-legacy, got %s", manager.iptablesBin())
		}
		if manager.ip6tablesBin() != backendIP6TablesLegacy {
			t.Errorf("expected ip6tables-legacy, got %s", manager.ip6tablesBin())
		}
	})
}

func TestNewNFTablesManager(t *testing.T) {
	cfg := config.NewConfig()
	manager := NewNFTablesManager(&cfg)

	if manager == nil {
		t.Fatal("expected non-nil manager")
	}
	if manager.cfg != &cfg {
		t.Error("manager.cfg not set correctly")
	}
}

func TestNewMonitor(t *testing.T) {
	t.Run("default interval", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.MonitorInterval = 0 // Will use fallback

		monitor := NewMonitor(testCfgPtr(&cfg))

		if monitor == nil {
			t.Fatal("expected non-nil monitor")
		}
		if monitor.interval < 1e9 { // 1 second in nanoseconds
			t.Error("interval should be at least 1 second")
		}
	})

	t.Run("custom interval", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.MonitorInterval = 30

		monitor := NewMonitor(testCfgPtr(&cfg))

		if monitor.interval.Seconds() != 30 {
			t.Errorf("expected 30s interval, got %v", monitor.interval)
		}
	})
}

func TestManifest_Apply_Empty(t *testing.T) {
	m := Manifest{}
	err := m.Apply()
	if err != nil {
		t.Errorf("empty manifest should apply without error: %v", err)
	}
}

func TestSysctlSetting(t *testing.T) {
	// Just test struct creation - actual apply/revert requires root
	s := SysctlSetting{
		Name:    "net.test.setting",
		Desired: "1",
		Revert:  "0",
	}

	if s.Name != "net.test.setting" {
		t.Error("Name not set")
	}
	if s.Desired != "1" {
		t.Error("Desired not set")
	}
	if s.Revert != "0" {
		t.Error("Revert not set")
	}
}

func TestRule_Struct(t *testing.T) {

	r := Rule{
		IPT:   "iptables",
		Table: "mangle",
		Chain: "B4",
		Spec:  []string{"-p", "tcp", "--dport", "443"},
	}

	if r.IPT != "iptables" {
		t.Error("IPT not set")
	}
	if r.Table != "mangle" {
		t.Error("Table not set")
	}
	if r.Chain != "B4" {
		t.Error("Chain not set")
	}
	if len(r.Spec) != 4 {
		t.Error("Spec not set correctly")
	}
}

func TestChain_Struct(t *testing.T) {

	c := Chain{
		IPT:   "iptables",
		Table: "mangle",
		Name:  "B4",
	}

	if c.IPT != "iptables" {
		t.Error("IPT not set")
	}
	if c.Table != "mangle" {
		t.Error("Table not set")
	}
	if c.Name != "B4" {
		t.Error("Name not set")
	}
}

func TestAddRules_SkipSetup(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Tables.SkipSetup = true

	err := AddRules(&cfg)
	if err != nil {
		t.Errorf("AddRules with SkipSetup should return nil: %v", err)
	}
}

func TestClearRules_SkipSetup(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Tables.SkipSetup = true

	err := ClearRules(&cfg)
	if err != nil {
		t.Errorf("ClearRules with SkipSetup should return nil: %v", err)
	}
}

func TestMonitor_StartStop_Disabled(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Tables.SkipSetup = true

	monitor := NewMonitor(testCfgPtr(&cfg))

	// Should not panic or block
	monitor.Start()
	monitor.Stop()
}

func TestMonitor_StartStop_IntervalZero(t *testing.T) {
	cfg := config.NewConfig()
	cfg.System.Tables.MonitorInterval = 0

	monitor := NewMonitor(testCfgPtr(&cfg))

	// interval <= 0 disables monitor
	monitor.Start()
	monitor.Stop()
}

func TestHasBinary(t *testing.T) {
	// "sh" should exist on any unix system
	if !hasBinary("sh") {
		t.Error("sh should be found")
	}

	// Non-existent binary
	if hasBinary("nonexistent_binary_xyz123") {
		t.Error("nonexistent binary should not be found")
	}
}

func TestNFTablesConstants(t *testing.T) {
	if nftTableName != "b4_mangle" {
		t.Errorf("nftTableName = %q, want b4_mangle", nftTableName)
	}
	if nftChainName != "b4_chain" {
		t.Errorf("nftChainName = %q, want b4_chain", nftChainName)
	}
}

func TestIPTablesManager_BuildManifest_NoIPTables(t *testing.T) {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = false
	cfg.Queue.IPv6Enabled = false

	manager := NewIPTablesManager(&cfg, false)
	_, err := manager.buildManifest()

	if err == nil {
		t.Error("expected error when no iptables binaries enabled")
	}
}

func TestLoadSysctlSnapshot_NoFile(t *testing.T) {
	// Temporarily change path to non-existent file
	origPath := sysctlSnapPath
	sysctlSnapPath = "/tmp/nonexistent_test_snapshot.json"
	defer func() { sysctlSnapPath = origPath }()

	snap := loadSysctlSnapshot()
	if snap == nil {
		t.Error("should return empty map, not nil")
	}
	if len(snap) != 0 {
		t.Error("should return empty map for non-existent file")
	}
}

func TestDetectFirewallBackend_ConfigOverride(t *testing.T) {
	t.Run("force nftables", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Engine = backendNFTables
		if got := detectFirewallBackend(&cfg); got != backendNFTables {
			t.Errorf("expected nftables, got %s", got)
		}
	})

	t.Run("force nft shorthand", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Engine = "nft"
		if got := detectFirewallBackend(&cfg); got != backendNFTables {
			t.Errorf("expected nftables, got %s", got)
		}
	})

	t.Run("force iptables", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Engine = backendIPTables
		if got := detectFirewallBackend(&cfg); got != backendIPTables {
			t.Errorf("expected iptables, got %s", got)
		}
	})

	t.Run("force iptables-legacy", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Engine = backendIPTablesLegacy
		if got := detectFirewallBackend(&cfg); got != backendIPTablesLegacy {
			t.Errorf("expected iptables-legacy, got %s", got)
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Engine = "NFTables"
		if got := detectFirewallBackend(&cfg); got != "nftables" {
			t.Errorf("expected nftables, got %s", got)
		}
	})

	t.Run("unknown value falls through to auto-detect", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Engine = "bogus"
		// Should not return "bogus" - falls through to auto-detection
		got := detectFirewallBackend(&cfg)
		if got == "bogus" {
			t.Error("unknown engine value should not be returned as-is")
		}
	})

	t.Run("empty string means auto-detect", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Engine = ""
		// Should not panic, should return some valid backend
		got := detectFirewallBackend(&cfg)
		if got != backendNFTables && got != backendIPTables && got != backendIPTablesLegacy {
			t.Errorf("unexpected backend: %s", got)
		}
	})
}

func TestChunkPorts(t *testing.T) {
	t.Run("small list", func(t *testing.T) {
		ports := []string{"80", "443", "8080"}
		chunks := chunkPorts(ports, 15)
		if len(chunks) != 1 {
			t.Fatalf("expected 1 chunk, got %d", len(chunks))
		}
		if len(chunks[0]) != 3 {
			t.Errorf("expected 3 ports in chunk, got %d", len(chunks[0]))
		}
	})

	t.Run("exact boundary", func(t *testing.T) {
		ports := make([]string, 15)
		for i := range ports {
			ports[i] = "80"
		}
		chunks := chunkPorts(ports, 15)
		if len(chunks) != 1 {
			t.Fatalf("expected 1 chunk, got %d", len(chunks))
		}
	})

	t.Run("split into multiple chunks", func(t *testing.T) {
		ports := make([]string, 20)
		for i := range ports {
			ports[i] = "80"
		}
		chunks := chunkPorts(ports, 15)
		if len(chunks) != 2 {
			t.Fatalf("expected 2 chunks, got %d", len(chunks))
		}
		if len(chunks[0]) != 15 {
			t.Errorf("first chunk should have 15 ports, got %d", len(chunks[0]))
		}
		if len(chunks[1]) != 5 {
			t.Errorf("second chunk should have 5 ports, got %d", len(chunks[1]))
		}
	})

	t.Run("empty list", func(t *testing.T) {
		chunks := chunkPorts([]string{}, 15)
		if len(chunks) != 1 {
			t.Fatalf("expected 1 chunk, got %d", len(chunks))
		}
		if len(chunks[0]) != 0 {
			t.Errorf("chunk should be empty, got %d", len(chunks[0]))
		}
	})
}

func TestRouteSanitizeSetID(t *testing.T) {
	t.Run("alphanumeric passthrough", func(t *testing.T) {
		result := routeSanitizeSetID("mySet1")
		if len(result) == 0 {
			t.Fatal("expected non-empty result")
		}
		if result[:6] != "myset1" {
			t.Errorf("expected prefix 'myset1', got %q", result)
		}
	})

	t.Run("special chars stripped", func(t *testing.T) {
		result := routeSanitizeSetID("my-Set!@#2")
		if len(result) == 0 {
			t.Fatal("expected non-empty result")
		}
		for _, c := range result {
			if !((c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_') {
				t.Errorf("unexpected char %c in sanitized ID", c)
			}
		}
	})

	t.Run("empty input returns default with suffix", func(t *testing.T) {
		result := routeSanitizeSetID("")
		if len(result) == 0 {
			t.Fatal("expected non-empty result")
		}
		if result[:7] != "default" {
			t.Errorf("expected 'default' prefix, got %q", result)
		}
	})

	t.Run("truncated to 20 chars", func(t *testing.T) {
		result := routeSanitizeSetID("abcdefghijklmnopqrstuvwxyz0123456789")
		if len(result) > 20 {
			t.Errorf("expected max 20 chars, got %d: %q", len(result), result)
		}
	})

	t.Run("deterministic", func(t *testing.T) {
		a := routeSanitizeSetID("test_set")
		b := routeSanitizeSetID("test_set")
		if a != b {
			t.Errorf("expected deterministic output, got %q and %q", a, b)
		}
	})

	t.Run("different inputs produce different outputs", func(t *testing.T) {
		a := routeSanitizeSetID("set_a")
		b := routeSanitizeSetID("set_b")
		if a == b {
			t.Errorf("expected different outputs for different inputs, both got %q", a)
		}
	})
}

func TestRouteBuildSetNames(t *testing.T) {
	v4, v6 := routeBuildSetNames("test")
	if v4 == "" || v6 == "" {
		t.Fatal("expected non-empty set names")
	}
	if v4[len(v4)-3:] != "_v4" {
		t.Errorf("v4 set should end with '_v4', got %q", v4)
	}
	if v6[len(v6)-3:] != "_v6" {
		t.Errorf("v6 set should end with '_v6', got %q", v6)
	}
	if v4[:4] != "b4r_" {
		t.Errorf("v4 set should start with 'b4r_', got %q", v4)
	}
}

func TestRouteBuildChainNames(t *testing.T) {
	pre, out, nat := routeBuildChainNames("test")
	if pre == "" || out == "" || nat == "" {
		t.Fatal("expected non-empty chain names")
	}
	if pre[len(pre)-4:] != "_pre" {
		t.Errorf("pre chain should end with '_pre', got %q", pre)
	}
	if out[len(out)-4:] != "_out" {
		t.Errorf("out chain should end with '_out', got %q", out)
	}
	if nat[len(nat)-4:] != "_nat" {
		t.Errorf("nat chain should end with '_nat', got %q", nat)
	}
}

func TestRouteNormalizedSources(t *testing.T) {
	t.Run("nil input", func(t *testing.T) {
		result := routeNormalizedSources(nil)
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("empty input", func(t *testing.T) {
		result := routeNormalizedSources([]string{})
		if result != nil {
			t.Errorf("expected nil, got %v", result)
		}
	})

	t.Run("deduplication", func(t *testing.T) {
		result := routeNormalizedSources([]string{"eth0", "eth1", "eth0"})
		if len(result) != 2 {
			t.Fatalf("expected 2, got %d: %v", len(result), result)
		}
	})

	t.Run("sorted output", func(t *testing.T) {
		result := routeNormalizedSources([]string{"wlan0", "eth0", "br0"})
		if result[0] != "br0" || result[1] != "eth0" || result[2] != "wlan0" {
			t.Errorf("expected sorted, got %v", result)
		}
	})

	t.Run("whitespace trimmed", func(t *testing.T) {
		result := routeNormalizedSources([]string{" eth0 ", "", "  "})
		if len(result) != 1 || result[0] != "eth0" {
			t.Errorf("expected [eth0], got %v", result)
		}
	})
}

func TestRouteQueueBypassMark(t *testing.T) {
	t.Run("nil config", func(t *testing.T) {
		if got := routeQueueBypassMark(nil); got != 0x8000 {
			t.Errorf("expected 0x8000, got 0x%x", got)
		}
	})

	t.Run("zero mark uses default", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.Queue.Mark = 0
		if got := routeQueueBypassMark(&cfg); got != 0x8000 {
			t.Errorf("expected 0x8000, got 0x%x", got)
		}
	})

	t.Run("custom mark", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.Queue.Mark = 0x1234
		if got := routeQueueBypassMark(&cfg); got != 0x1234 {
			t.Errorf("expected 0x1234, got 0x%x", got)
		}
	})
}

func TestRouteCollectEntries(t *testing.T) {
	t.Run("nil set", func(t *testing.T) {
		v4, v6 := routeCollectEntries(nil)
		if v4 != nil || v6 != nil {
			t.Error("expected nil for nil set")
		}
	})

	t.Run("empty IPs", func(t *testing.T) {
		set := &config.SetConfig{}
		v4, v6 := routeCollectEntries(set)
		if v4 != nil || v6 != nil {
			t.Error("expected nil for empty IPs")
		}
	})

	t.Run("IPv4 addresses", func(t *testing.T) {
		set := &config.SetConfig{}
		set.Targets.IpsToMatch = []string{"1.2.3.4", "5.6.7.8"}
		v4, v6 := routeCollectEntries(set)
		if len(v4) != 2 {
			t.Errorf("expected 2 v4, got %d", len(v4))
		}
		if len(v6) != 0 {
			t.Errorf("expected 0 v6, got %d", len(v6))
		}
	})

	t.Run("IPv6 addresses", func(t *testing.T) {
		set := &config.SetConfig{}
		set.Targets.IpsToMatch = []string{"2001:db8::1", "fe80::1"}
		v4, v6 := routeCollectEntries(set)
		if len(v4) != 0 {
			t.Errorf("expected 0 v4, got %d", len(v4))
		}
		if len(v6) != 2 {
			t.Errorf("expected 2 v6, got %d", len(v6))
		}
	})

	t.Run("CIDR notation", func(t *testing.T) {
		set := &config.SetConfig{}
		set.Targets.IpsToMatch = []string{"10.0.0.0/24", "2001:db8::/32"}
		v4, v6 := routeCollectEntries(set)
		if len(v4) != 1 {
			t.Errorf("expected 1 v4, got %d", len(v4))
		}
		if len(v6) != 1 {
			t.Errorf("expected 1 v6, got %d", len(v6))
		}
	})

	t.Run("deduplication", func(t *testing.T) {
		set := &config.SetConfig{}
		set.Targets.IpsToMatch = []string{"1.2.3.4", "1.2.3.4", "1.2.3.4"}
		v4, _ := routeCollectEntries(set)
		if len(v4) != 1 {
			t.Errorf("expected 1 deduplicated, got %d", len(v4))
		}
	})

	t.Run("invalid entries skipped", func(t *testing.T) {
		set := &config.SetConfig{}
		set.Targets.IpsToMatch = []string{"not-an-ip", "", "  ", "1.2.3.4"}
		v4, v6 := routeCollectEntries(set)
		if len(v4) != 1 {
			t.Errorf("expected 1 v4, got %d", len(v4))
		}
		if len(v6) != 0 {
			t.Errorf("expected 0 v6, got %d", len(v6))
		}
	})

	t.Run("mixed v4 and v6", func(t *testing.T) {
		set := &config.SetConfig{}
		set.Targets.IpsToMatch = []string{"1.2.3.4", "2001:db8::1", "10.0.0.1"}
		v4, v6 := routeCollectEntries(set)
		if len(v4) != 2 {
			t.Errorf("expected 2 v4, got %d", len(v4))
		}
		if len(v6) != 1 {
			t.Errorf("expected 1 v6, got %d", len(v6))
		}
	})
}

func TestRoutingRulesPresent(t *testing.T) {
	origCache := routeRuleCache
	defer func() { routeRuleCache = origCache }()

	t.Run("nil config", func(t *testing.T) {
		if !RoutingRulesPresent(nil) {
			t.Error("expected true for nil config")
		}
	})

	t.Run("empty cache means nothing to verify", func(t *testing.T) {
		routeRuleCache = make(map[string]routeState)
		cfg := config.NewConfig()
		if !RoutingRulesPresent(&cfg) {
			t.Error("expected true when no routing rules are cached")
		}
	})
}

func TestRoutingLearnIP(t *testing.T) {
	origCache := routeRuleCache
	origLearn := routeLearnLast
	defer func() {
		routeRuleCache = origCache
		routeLearnLast = origLearn
	}()

	newSet := func(mode string) *config.SetConfig {
		s := &config.SetConfig{Id: "s1"}
		s.Routing.Enabled = true
		s.Routing.Mode = mode
		s.Routing.EgressInterface = "wg0"
		return s
	}

	t.Run("block mode is skipped", func(t *testing.T) {
		routeRuleCache = make(map[string]routeState)
		routeLearnLast = make(map[string]time.Time)
		cfg := config.NewConfig()
		RoutingLearnIP(&cfg, newSet(config.RoutingModeBlock), net.ParseIP("1.2.3.4"))
		if len(routeLearnLast) != 0 {
			t.Error("block-mode set must not be learned into the routing ipset")
		}
	})

	t.Run("set with no installed rule is a no-op", func(t *testing.T) {
		routeRuleCache = make(map[string]routeState)
		routeLearnLast = make(map[string]time.Time)
		cfg := config.NewConfig()
		RoutingLearnIP(&cfg, newSet(config.RoutingModeInterface), net.ParseIP("1.2.3.4"))
		if len(routeLearnLast) != 0 {
			t.Error("set absent from routeRuleCache should be a no-op")
		}
	})

	t.Run("nil args are safe", func(t *testing.T) {
		RoutingLearnIP(nil, nil, nil)
	})
}

func TestRouteResolveIDs(t *testing.T) {
	origCache := routeRuleCache
	origAuto := routeIfaceAuto
	defer func() {
		routeRuleCache = origCache
		routeIfaceAuto = origAuto
	}()

	t.Run("explicit mark and table", func(t *testing.T) {
		routeRuleCache = make(map[string]routeState)
		routeIfaceAuto = make(map[string]routeState)

		cfg := config.NewConfig()
		set := &config.SetConfig{}
		set.Routing.FWMark = 0x100
		set.Routing.Table = 200
		set.Routing.EgressInterface = "eth0"

		mark, table := routeResolveIDs(&cfg, set)
		if mark != 0x100 || table != 200 {
			t.Errorf("expected mark=0x100 table=200, got mark=0x%x table=%d", mark, table)
		}
	})

	t.Run("auto allocation deterministic per interface", func(t *testing.T) {
		routeRuleCache = make(map[string]routeState)
		routeIfaceAuto = make(map[string]routeState)

		cfg := config.NewConfig()
		set := &config.SetConfig{}
		set.Routing.EgressInterface = "wg0"

		mark1, table1 := routeResolveIDs(&cfg, set)
		if mark1 == 0 || table1 == 0 {
			t.Fatalf("expected non-zero, got mark=0x%x table=%d", mark1, table1)
		}

		routeRuleCache = make(map[string]routeState)
		routeIfaceAuto = make(map[string]routeState)
		mark2, table2 := routeResolveIDs(&cfg, set)
		if mark1 != mark2 || table1 != table2 {
			t.Errorf("expected deterministic: mark1=0x%x mark2=0x%x table1=%d table2=%d", mark1, mark2, table1, table2)
		}
	})

	t.Run("reuses cached iface auto", func(t *testing.T) {
		routeRuleCache = make(map[string]routeState)
		routeIfaceAuto = map[string]routeState{
			"tun0": {mark: 0x555, table: 150},
		}

		cfg := config.NewConfig()
		set := &config.SetConfig{}
		set.Routing.EgressInterface = "tun0"

		mark, table := routeResolveIDs(&cfg, set)
		if mark != 0x555 || table != 150 {
			t.Errorf("expected cached mark=0x555 table=150, got mark=0x%x table=%d", mark, table)
		}
	})

	t.Run("different interfaces get different IDs", func(t *testing.T) {
		routeRuleCache = make(map[string]routeState)
		routeIfaceAuto = make(map[string]routeState)

		cfg := config.NewConfig()
		setA := &config.SetConfig{}
		setA.Routing.EgressInterface = "eth0"
		setB := &config.SetConfig{}
		setB.Routing.EgressInterface = "wg0"

		markA, tableA := routeResolveIDs(&cfg, setA)
		markB, tableB := routeResolveIDs(&cfg, setB)
		if markA == markB {
			t.Errorf("expected different marks, both got 0x%x", markA)
		}
		if tableA == tableB {
			t.Errorf("expected different tables, both got %d", tableA)
		}
	})
}

func TestRouteAddIPsToSets(t *testing.T) {
	t.Run("classifies v4 and v6", func(t *testing.T) {
		var v4calls, v6calls [][]string
		mock := &mockRouteBackend{
			addElementsFn: func(setName string, ips []string, ttl int) {
				if setName == "set_v4" {
					v4calls = append(v4calls, ips)
				} else {
					v6calls = append(v6calls, ips)
				}
			},
		}
		st := routeState{setV4: "set_v4", setV6: "set_v6"}
		ips := []net.IP{
			net.ParseIP("1.2.3.4"),
			net.ParseIP("2001:db8::1"),
			net.ParseIP("5.6.7.8"),
		}
		routeAddIPsToSets(mock, st, 3600, ips, true, true)
		if len(v4calls) != 1 || len(v4calls[0]) != 2 {
			t.Errorf("expected 2 v4 IPs in 1 call, got %v", v4calls)
		}
		if len(v6calls) != 1 || len(v6calls[0]) != 1 {
			t.Errorf("expected 1 v6 IP in 1 call, got %v", v6calls)
		}
	})

	t.Run("skips v4 when disabled", func(t *testing.T) {
		calls := 0
		mock := &mockRouteBackend{
			addElementsFn: func(setName string, ips []string, ttl int) { calls++ },
		}
		st := routeState{setV4: "set_v4", setV6: "set_v6"}
		ips := []net.IP{net.ParseIP("1.2.3.4")}
		routeAddIPsToSets(mock, st, 3600, ips, false, true)
		if calls != 0 {
			t.Errorf("expected 0 calls when v4 disabled, got %d", calls)
		}
	})

	t.Run("deduplicates IPs", func(t *testing.T) {
		var gotIPs []string
		mock := &mockRouteBackend{
			addElementsFn: func(setName string, ips []string, ttl int) { gotIPs = ips },
		}
		st := routeState{setV4: "set_v4", setV6: "set_v6"}
		ips := []net.IP{
			net.ParseIP("1.2.3.4"),
			net.ParseIP("1.2.3.4"),
			net.ParseIP("1.2.3.4"),
		}
		routeAddIPsToSets(mock, st, 3600, ips, true, true)
		if len(gotIPs) != 1 {
			t.Errorf("expected 1 deduplicated IP, got %d", len(gotIPs))
		}
	})
}

func TestRouteAddIPsToSets_StaticNoTTL(t *testing.T) {
	var gotTTL int
	mock := &mockRouteBackend{
		addElementsFn: func(setName string, ips []string, ttl int) { gotTTL = ttl },
	}
	st := routeState{setV4: "set_v4", setV6: "set_v6"}
	ips := []net.IP{net.ParseIP("1.2.3.4")}
	routeAddIPsToSets(mock, st, 0, ips, true, true)
	if gotTTL != 0 {
		t.Errorf("expected TTL 0 for static IPs, got %d", gotTTL)
	}
}

func TestRoutePeriodicReResolve_SkipsWhenEmpty(t *testing.T) {
	routeMu.Lock()
	oldCache := routeRuleCache
	routeRuleCache = make(map[string]routeState)
	routeMu.Unlock()
	defer func() {
		routeMu.Lock()
		routeRuleCache = oldCache
		routeMu.Unlock()
	}()

	cfg := &config.Config{}
	RoutingPeriodicReResolve(cfg)
}

func TestRoutePeriodicReResolve_PerSetScheduling(t *testing.T) {
	routeMu.Lock()
	oldCache := routeRuleCache
	oldResolve := routeLastReResolve
	routeRuleCache = map[string]routeState{
		"set-short": {setV4: "sv4_short"},
		"set-long":  {setV4: "sv4_long"},
	}
	shortInitial := time.Now().Add(-10 * time.Minute)
	longInitial := time.Now().Add(time.Hour)
	routeLastReResolve = map[string]time.Time{
		"set-short": shortInitial,
		"set-long":  longInitial,
	}
	routeMu.Unlock()
	defer func() {
		routeMu.Lock()
		routeRuleCache = oldCache
		routeLastReResolve = oldResolve
		routeMu.Unlock()
	}()

	cfg := &config.Config{
		Sets: []*config.SetConfig{
			{
				Id:      "set-short",
				Enabled: true,
				Routing: config.RoutingConfig{
					Enabled:         true,
					EgressInterface: "wg0",
					IPTTLSeconds:    600,
				},
				Targets: config.TargetsConfig{
					SNIDomains: []string{"short.invalid"},
				},
			},
			{
				Id:      "set-long",
				Enabled: true,
				Routing: config.RoutingConfig{
					Enabled:         true,
					EgressInterface: "wg0",
					IPTTLSeconds:    86400,
				},
				Targets: config.TargetsConfig{
					SNIDomains: []string{"long.invalid"},
				},
			},
		},
	}

	RoutingPeriodicReResolve(cfg)

	routeMu.Lock()
	shortUpdated := !routeLastReResolve["set-short"].Equal(shortInitial)
	longUpdated := !routeLastReResolve["set-long"].Equal(longInitial)
	routeMu.Unlock()

	if !shortUpdated {
		t.Error("set-short should have been scheduled for re-resolve (its interval elapsed)")
	}
	if longUpdated {
		t.Error("set-long should NOT have been re-resolved (its interval has not elapsed)")
	}
}

func TestDiscoveryQueueAction(t *testing.T) {
	t.Run("single thread", func(t *testing.T) {
		action := discoveryQueueAction(200, 1)
		if len(action) != 3 {
			t.Fatalf("expected 3 elements, got %d", len(action))
		}
		if action[0] != "--queue-num" || action[1] != "200" || action[2] != "--queue-bypass" {
			t.Errorf("unexpected action: %v", action)
		}
	})

	t.Run("multiple threads", func(t *testing.T) {
		action := discoveryQueueAction(200, 4)
		if len(action) != 3 {
			t.Fatalf("expected 3 elements, got %d", len(action))
		}
		if action[0] != "--queue-balance" || action[1] != "200:203" || action[2] != "--queue-bypass" {
			t.Errorf("unexpected action: %v", action)
		}
	})
}

func TestDiscoveryIptBackend_BinaryNames(t *testing.T) {
	t.Run("standard", func(t *testing.T) {
		b := &discoveryIptBackend{legacy: false}
		if b.ipt4() != backendIPTables {
			t.Errorf("expected %s, got %s", backendIPTables, b.ipt4())
		}
		if b.ipt6() != backendIP6Tables {
			t.Errorf("expected %s, got %s", backendIP6Tables, b.ipt6())
		}
		if b.name() != backendIPTables {
			t.Errorf("expected %s, got %s", backendIPTables, b.name())
		}
	})

	t.Run("legacy", func(t *testing.T) {
		b := &discoveryIptBackend{legacy: true}
		if b.ipt4() != backendIPTablesLegacy {
			t.Errorf("expected %s, got %s", backendIPTablesLegacy, b.ipt4())
		}
		if b.ipt6() != backendIP6TablesLegacy {
			t.Errorf("expected %s, got %s", backendIP6TablesLegacy, b.ipt6())
		}
	})
}

func TestDiscoveryNftBackend_Name(t *testing.T) {
	b := &discoveryNftBackend{}
	if b.name() != backendNFTables {
		t.Errorf("expected %s, got %s", backendNFTables, b.name())
	}
}

func TestDiscoveryConstants(t *testing.T) {
	if discoveryChainIPT != "B4_DISCOVERY" {
		t.Errorf("discoveryChainIPT = %q, want B4_DISCOVERY", discoveryChainIPT)
	}
	if discoveryChainNFT != "b4_discovery" {
		t.Errorf("discoveryChainNFT = %q, want b4_discovery", discoveryChainNFT)
	}
}

type mockRouteBackend struct {
	addElementsFn func(setName string, ips []string, ttlSec int)
}

func (m *mockRouteBackend) name() string                                          { return "mock" }
func (m *mockRouteBackend) available() bool                                       { return true }
func (m *mockRouteBackend) ensureBase() error                                     { return nil }
func (m *mockRouteBackend) ensureIPSet(name string, v6 bool) error                { return nil }
func (m *mockRouteBackend) ensureChain(chain string, isMangle bool) error         { return nil }
func (m *mockRouteBackend) flushChain(chain string, isMangle bool)                {}
func (m *mockRouteBackend) deleteChain(chain string, isMangle bool)               {}
func (m *mockRouteBackend) addBypassRule(chain string, mark uint32)               {}
func (m *mockRouteBackend) addMarkRule(chain string, v6 bool, setName string, mark uint32, sourceIface string, tagHostConntrack bool) {
}
func (m *mockRouteBackend) ensureJumpRule(baseChain, targetChain string, isMangle bool)  {}
func (m *mockRouteBackend) deleteJumpRules(baseChain, targetChain string, isMangle bool) {}
func (m *mockRouteBackend) addMasqueradeRule(chain string, mark uint32, iface string, v6 bool) {
}
func (m *mockRouteBackend) flushIPSet(name string)   {}
func (m *mockRouteBackend) destroyIPSet(name string) {}
func (m *mockRouteBackend) clearAll()                {}
func (m *mockRouteBackend) addElements(setName string, ips []string, ttlSec int) {
	if m.addElementsFn != nil {
		m.addElementsFn(setName, ips, ttlSec)
	}
}

func TestMonitor_BackendPropagation(t *testing.T) {
	t.Run("auto-detect backend stored", func(t *testing.T) {
		cfg := config.NewConfig()
		monitor := NewMonitor(testCfgPtr(&cfg))
		// Backend should be one of the valid values
		if monitor.backend != "nftables" && monitor.backend != "iptables" && monitor.backend != backendIPTablesLegacy {
			t.Errorf("unexpected backend in monitor: %s", monitor.backend)
		}
	})

	t.Run("config engine override propagates to monitor", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Engine = backendIPTables
		monitor := NewMonitor(testCfgPtr(&cfg))
		if monitor.backend != backendIPTables {
			t.Errorf("expected %s, got %s", backendIPTables, monitor.backend)
		}
	})

	t.Run("legacy engine override propagates to monitor", func(t *testing.T) {
		cfg := config.NewConfig()
		cfg.System.Tables.Engine = backendIPTablesLegacy
		monitor := NewMonitor(testCfgPtr(&cfg))
		if monitor.backend != backendIPTablesLegacy {
			t.Errorf("expected %s, got %s", backendIPTablesLegacy, monitor.backend)
		}
	})
}
