package config

import (
	"testing"
)

func TestNewSetConfig_DeepCopy(t *testing.T) {
	set1 := NewSetConfig()
	set2 := NewSetConfig()

	set1.TCP.Win.Values = append(set1.TCP.Win.Values, 999)
	set1.Targets.GeoSiteCategories = append(set1.Targets.GeoSiteCategories, "test")
	set1.Faking.SNIMutation.FakeSNIs = append(set1.Faking.SNIMutation.FakeSNIs, "test.com")

	if len(set2.TCP.Win.Values) != 4 {
		t.Error("WinValues leaked between instances")
	}
	if len(set2.Targets.GeoSiteCategories) != 0 {
		t.Error("GeoSiteCategories leaked between instances")
	}
	if len(set2.Faking.SNIMutation.FakeSNIs) != 3 {
		t.Error("FakeSNIs leaked between instances")
	}

	set1.Fragmentation.StrategyPool = append(set1.Fragmentation.StrategyPool, "combo")
	if len(set2.Fragmentation.StrategyPool) != 0 {
		t.Error("StrategyPool leaked between instances")
	}
}

func TestResolveRange(t *testing.T) {
	if v := ResolveRange(5, 0); v != 5 {
		t.Errorf("ResolveRange(5,0) = %d, want 5", v)
	}
	if v := ResolveRange(5, 5); v != 5 {
		t.Errorf("ResolveRange(5,5) = %d, want 5", v)
	}
	if v := ResolveRange(5, 3); v != 5 {
		t.Errorf("ResolveRange(5,3) = %d, want 5 (max<min)", v)
	}
	for i := 0; i < 100; i++ {
		v := ResolveRange(5, 10)
		if v < 5 || v > 10 {
			t.Fatalf("ResolveRange(5,10) = %d, out of [5,10]", v)
		}
	}
}

func TestResolveStrategyPool(t *testing.T) {
	if v := ResolveStrategyPool(nil, "tcp"); v != "tcp" {
		t.Errorf("empty pool should return fallback, got %q", v)
	}
	if v := ResolveStrategyPool([]string{}, "tcp"); v != "tcp" {
		t.Errorf("empty pool should return fallback, got %q", v)
	}
	if v := ResolveStrategyPool([]string{"combo"}, "tcp"); v != "combo" {
		t.Errorf("single-item pool should return that item, got %q", v)
	}
	pool := []string{"combo", "disorder", "tcp"}
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		seen[ResolveStrategyPool(pool, "none")] = true
	}
	for _, s := range pool {
		if !seen[s] {
			t.Errorf("strategy %q never picked from pool in 100 tries", s)
		}
	}
}

func TestTrustedDNSServer(t *testing.T) {
	cfg := NewConfig()
	if _, ok := cfg.TrustedDNSServer(); ok {
		t.Fatal("no trusted DNS server by default, Discovery uses the built-in resolvers")
	}

	cfg.System.Checker.DNSServer = "tcp+udp://127.0.0.1:53053"
	ep, ok := cfg.TrustedDNSServer()
	if !ok || ep.String() != "tcp+udp://127.0.0.1:53053" {
		t.Fatalf("got %+v %v", ep, ok)
	}

	cfg.System.Checker.DNSServer = "dns.google"
	if err := cfg.Validate(); err != nil {
		t.Fatalf("a bad trusted DNS server must not stop b4 from starting: %v", err)
	}
	if cfg.System.Checker.DNSServer != "" {
		t.Fatalf("a bad trusted DNS server is dropped with a warning, got %q", cfg.System.Checker.DNSServer)
	}
}
