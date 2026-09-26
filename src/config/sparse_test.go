package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestMarshalSparse_OmitsDefaults(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "test-set"
	set.Name = "Test"
	cfg.Sets = []*SetConfig{&set}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse failed: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	if queue, hasQueue := raw["queue"].(map[string]interface{}); hasQueue {
		for key, val := range queue {
			switch val.(type) {
			case []interface{}, map[string]interface{}:
			default:
				t.Errorf("sparse queue should only contain arrays/objects when defaults, found scalar: %s", key)
			}
		}
	}
	if _, ok := raw["version"]; !ok {
		t.Error("sparse output must always contain 'version'")
	}
	if _, ok := raw["sets"]; !ok {
		t.Error("sparse output must always contain 'sets'")
	}
}

func TestMarshalSparse_KeepsNonDefaults(t *testing.T) {
	cfg := NewConfig()
	cfg.Queue.StartNum = 999
	cfg.Queue.Threads = 16
	cfg.System.WebServer.Port = 9999

	set := NewSetConfig()
	set.Id = "test-set"
	set.Name = "Test"
	set.Fragmentation.Strategy = "ip"
	set.Faking.TTL = 42
	cfg.Sets = []*SetConfig{&set}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse failed: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	queue, ok := raw["queue"].(map[string]interface{})
	if !ok {
		t.Fatal("expected 'queue' in sparse output")
	}
	if queue["start_num"] != float64(999) {
		t.Errorf("expected start_num=999, got %v", queue["start_num"])
	}
	if queue["threads"] != float64(16) {
		t.Errorf("expected threads=16, got %v", queue["threads"])
	}

	sets, ok := raw["sets"].([]interface{})
	if !ok || len(sets) != 1 {
		t.Fatal("expected 1 set in sparse output")
	}
	setMap := sets[0].(map[string]interface{})

	if setMap["id"] != "test-set" {
		t.Error("set id must always be present")
	}
	if setMap["name"] != "Test" {
		t.Error("set name must always be present")
	}

	frag, ok := setMap["fragmentation"].(map[string]interface{})
	if !ok {
		t.Fatal("expected fragmentation in set")
	}
	if frag["strategy"] != "ip" {
		t.Errorf("expected strategy=ip, got %v", frag["strategy"])
	}
}

func TestSparseSaveLoadRoundtrip(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	cfg := NewConfig()
	cfg.Queue.StartNum = 888
	cfg.System.WebServer.Port = 3000
	cfg.System.Logging.Level = 3

	set := NewSetConfig()
	set.Id = "my-set"
	set.Name = "Custom"
	set.Enabled = true
	set.Fragmentation.Strategy = "disorder"
	set.Faking.TTL = 12
	set.TCP.Desync.TTL = 3
	set.Targets.SNIDomains = []string{"example.com", "test.org"}
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	data, _ := os.ReadFile(path)
	var raw map[string]interface{}
	json.Unmarshal(data, &raw)
	if _, ok := raw["queue"].(map[string]interface{})["ipv6_enabled"]; ok {
		t.Error("sparse config should not contain default ipv6_enabled field")
	}

	loaded := NewConfig()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	if loaded.Queue.StartNum != 888 {
		t.Errorf("expected StartNum=888, got %d", loaded.Queue.StartNum)
	}
	if loaded.System.WebServer.Port != 3000 {
		t.Errorf("expected WebServer.Port=3000, got %d", loaded.System.WebServer.Port)
	}
	if loaded.Queue.Threads != DefaultConfig.Queue.Threads {
		t.Errorf("expected Threads to be default %d, got %d", DefaultConfig.Queue.Threads, loaded.Queue.Threads)
	}
	if loaded.Queue.IPv4Enabled != DefaultConfig.Queue.IPv4Enabled {
		t.Errorf("expected IPv4Enabled to be default %v, got %v", DefaultConfig.Queue.IPv4Enabled, loaded.Queue.IPv4Enabled)
	}

	if len(loaded.Sets) != 1 {
		t.Fatalf("expected 1 set, got %d", len(loaded.Sets))
	}
	ls := loaded.Sets[0]
	if ls.Id != "my-set" {
		t.Errorf("expected set id=my-set, got %s", ls.Id)
	}
	if ls.Fragmentation.Strategy != "disorder" {
		t.Errorf("expected strategy=disorder, got %s", ls.Fragmentation.Strategy)
	}
	if ls.Faking.TTL != 12 {
		t.Errorf("expected Faking.TTL=12, got %d", ls.Faking.TTL)
	}
	if ls.TCP.Desync.TTL != 3 {
		t.Errorf("expected Desync.TTL=3, got %d", ls.TCP.Desync.TTL)
	}
	if ls.Faking.SNISeqLength != DefaultSetConfig.Faking.SNISeqLength {
		t.Errorf("expected default SNISeqLength=%d, got %d", DefaultSetConfig.Faking.SNISeqLength, ls.Faking.SNISeqLength)
	}
	if ls.UDP.FakeSeqLength != DefaultSetConfig.UDP.FakeSeqLength {
		t.Errorf("expected default FakeSeqLength=%d, got %d", DefaultSetConfig.UDP.FakeSeqLength, ls.UDP.FakeSeqLength)
	}
	if len(ls.Targets.SNIDomains) != 2 {
		t.Errorf("expected 2 SNI domains, got %d", len(ls.Targets.SNIDomains))
	}
}

func TestSparseSaveLoadRoundtrip_BoolDefaults(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "bool-test"
	set.Name = "Bool"
	set.Enabled = false
	set.Fragmentation.ReverseOrder = false
	set.UDP.FilterSTUN = false
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	loaded := NewConfig()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	ls := loaded.Sets[0]
	if ls.Enabled != false {
		t.Error("expected Enabled=false after roundtrip")
	}
	if ls.Fragmentation.ReverseOrder != false {
		t.Error("expected ReverseOrder=false after roundtrip")
	}
	if ls.UDP.FilterSTUN != false {
		t.Error("expected FilterSTUN=false after roundtrip")
	}
}

func TestSparseSaveLoadRoundtrip_TrueBoolDefaultsSurvive(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "config.json")

	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "bool-defaults"
	set.Name = "BoolDefaults"
	set.Faking.TTL = 8
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	loaded := NewConfig()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	ls := loaded.Sets[0]
	if ls.Faking.SNI != DefaultSetConfig.Faking.SNI {
		t.Errorf("expected Faking.SNI=%v (default), got %v", DefaultSetConfig.Faking.SNI, ls.Faking.SNI)
	}
	if ls.Fragmentation.ReverseOrder != DefaultSetConfig.Fragmentation.ReverseOrder {
		t.Errorf("expected ReverseOrder=%v (default), got %v", DefaultSetConfig.Fragmentation.ReverseOrder, ls.Fragmentation.ReverseOrder)
	}
	if ls.Fragmentation.MiddleSNI != DefaultSetConfig.Fragmentation.MiddleSNI {
		t.Errorf("expected MiddleSNI=%v (default), got %v", DefaultSetConfig.Fragmentation.MiddleSNI, ls.Fragmentation.MiddleSNI)
	}
	if ls.Fragmentation.Combo.FirstByteSplit != DefaultSetConfig.Fragmentation.Combo.FirstByteSplit {
		t.Errorf("expected FirstByteSplit=%v (default), got %v", DefaultSetConfig.Fragmentation.Combo.FirstByteSplit, ls.Fragmentation.Combo.FirstByteSplit)
	}
	if ls.Fragmentation.Combo.ExtensionSplit != DefaultSetConfig.Fragmentation.Combo.ExtensionSplit {
		t.Errorf("expected ExtensionSplit=%v (default), got %v", DefaultSetConfig.Fragmentation.Combo.ExtensionSplit, ls.Fragmentation.Combo.ExtensionSplit)
	}
	if ls.Enabled != DefaultSetConfig.Enabled {
		t.Errorf("expected Enabled=%v (default), got %v", DefaultSetConfig.Enabled, ls.Enabled)
	}
	if ls.UDP.FilterSTUN != DefaultSetConfig.UDP.FilterSTUN {
		t.Errorf("expected FilterSTUN=%v (default), got %v", DefaultSetConfig.UDP.FilterSTUN, ls.UDP.FilterSTUN)
	}
}

func TestMarshalSparse_OmitsDerivedDiscoveryMarks(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "mark-test"
	set.Name = "Marks"
	set.Enabled = true
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate failed: %v", err)
	}

	if cfg.System.Checker.DiscoveryFlowMark == 0 {
		t.Fatal("expected Validate to populate DiscoveryFlowMark")
	}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse failed: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	if sys, ok := raw["system"].(map[string]interface{}); ok {
		if checker, ok := sys["checker"].(map[string]interface{}); ok {
			if _, exists := checker["discovery_flow_mark"]; exists {
				t.Error("discovery_flow_mark should be omitted when it equals mark+1")
			}
			if _, exists := checker["discovery_injected_mark"]; exists {
				t.Error("discovery_injected_mark should be omitted when it equals mark+2")
			}
		}
	}
}

func TestMarshalSparse_KeepsCustomDiscoveryMarks(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "mark-test"
	set.Name = "Marks"
	set.Enabled = true
	cfg.Sets = []*SetConfig{&set}
	cfg.System.Checker.DiscoveryFlowMark = 50000
	cfg.System.Checker.DiscoveryInjectedMark = 60000

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse failed: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	sys := raw["system"].(map[string]interface{})
	checker := sys["checker"].(map[string]interface{})
	if checker["discovery_flow_mark"] != float64(50000) {
		t.Errorf("expected custom discovery_flow_mark=50000, got %v", checker["discovery_flow_mark"])
	}
	if checker["discovery_injected_mark"] != float64(60000) {
		t.Errorf("expected custom discovery_injected_mark=60000, got %v", checker["discovery_injected_mark"])
	}
}

func TestMarshalSparse_DefaultArraysOmitted(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "arr-test"
	set.Name = "Arrays"
	cfg.Sets = []*SetConfig{&set}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse failed: %v", err)
	}

	var raw map[string]interface{}
	json.Unmarshal(data, &raw)

	sets := raw["sets"].([]interface{})
	setMap := sets[0].(map[string]interface{})

	if targets, ok := setMap["targets"].(map[string]interface{}); ok {
		arrayFields := []string{"sni_domains", "ip", "geosite_categories", "geoip_categories", "asns", "source_devices"}
		for _, field := range arrayFields {
			if _, exists := targets[field]; exists {
				t.Errorf("targets.%s should be omitted when default", field)
			}
		}
	}

	if routing, ok := setMap["routing"].(map[string]interface{}); ok {
		if _, exists := routing["source_interfaces"]; exists {
			t.Error("routing.source_interfaces should be omitted when default")
		}
	}
}

func TestMarshalSparse_ASNs(t *testing.T) {
	cfg := NewConfig()
	empty := NewSetConfig()
	empty.Id = "empty"
	nilASNs := NewSetConfig()
	nilASNs.Id = "nil"
	nilASNs.Targets.ASNs = nil
	withASNs := NewSetConfig()
	withASNs.Id = "with"
	withASNs.Targets.ASNs = []string{"15169", "13335"}
	cfg.Sets = []*SetConfig{&empty, &nilASNs, &withASNs}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse failed: %v", err)
	}
	var raw struct {
		Sets []map[string]json.RawMessage `json:"sets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	for i, id := range []string{"empty", "nil"} {
		var targets map[string]json.RawMessage
		_ = json.Unmarshal(raw.Sets[i]["targets"], &targets)
		if _, ok := targets["asns"]; ok {
			t.Errorf("set %s: an empty ASN list must be omitted", id)
		}
	}
	var targets struct {
		ASNs []string `json:"asns"`
	}
	if err := json.Unmarshal(raw.Sets[2]["targets"], &targets); err != nil {
		t.Fatal(err)
	}
	if len(targets.ASNs) != 2 || targets.ASNs[0] != "15169" || targets.ASNs[1] != "13335" {
		t.Fatalf("asns = %v", targets.ASNs)
	}

	path := filepath.Join(t.TempDir(), "b4.json")
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatal(err)
	}
	loaded := NewConfig()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatal(err)
	}
	if loaded.Sets[0].Targets.ASNs == nil || len(loaded.Sets[0].Targets.ASNs) != 0 {
		t.Fatalf("a loaded set without asns must default to an empty list, got %#v", loaded.Sets[0].Targets.ASNs)
	}
	if len(loaded.Sets[2].Targets.ASNs) != 2 {
		t.Fatalf("asns did not round-trip: %v", loaded.Sets[2].Targets.ASNs)
	}
}
