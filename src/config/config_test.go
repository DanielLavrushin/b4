package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestNewConfig_ReferenceDNSDoesNotAliasDefault(t *testing.T) {
	want := append([]string(nil), DefaultConfig.System.Checker.ReferenceDNS...)

	cfg := NewConfig()
	cfg.System.Checker.ReferenceDNS[0] = "10.0.0.1"

	if DefaultConfig.System.Checker.ReferenceDNS[0] != want[0] {
		t.Fatalf("DefaultConfig.ReferenceDNS changed through a NewConfig copy: %v", DefaultConfig.System.Checker.ReferenceDNS)
	}
}

func TestLoadWithMigration_CustomReferenceDNSSurvivesSparseSave(t *testing.T) {
	want := append([]string(nil), DefaultConfig.System.Checker.ReferenceDNS...)
	custom := []string{"1.0.0.1", "1.1.1.1", "8.8.8.8", "9.9.9.9", "8.8.4.4"}
	if len(custom) != len(want) {
		t.Fatalf("test list must match the default length %d", len(want))
	}

	path := filepath.Join(t.TempDir(), "b4.json")
	body := fmt.Sprintf(`{"version":%d,"system":{"checker":{"reference_dns":["%s"]}}}`, CurrentConfigVersion, strings.Join(custom, `","`))
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := NewConfig()
	if _, err := cfg.LoadWithMigration(path); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.System.Checker.ReferenceDNS, custom) {
		t.Fatalf("loaded reference_dns = %v, want %v", cfg.System.Checker.ReferenceDNS, custom)
	}
	if !reflect.DeepEqual(DefaultConfig.System.Checker.ReferenceDNS, want) {
		t.Fatalf("loading a config rewrote DefaultConfig.ReferenceDNS to %v", DefaultConfig.System.Checker.ReferenceDNS)
	}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	var saved struct {
		System struct {
			Checker struct {
				ReferenceDNS []string `json:"reference_dns"`
			} `json:"checker"`
		} `json:"system"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(saved.System.Checker.ReferenceDNS, custom) {
		t.Fatalf("sparse save dropped the custom reference_dns, got %v in:\n%s", saved.System.Checker.ReferenceDNS, data)
	}
}
