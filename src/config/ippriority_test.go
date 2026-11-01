package config

import (
	"encoding/json"
	"path/filepath"
	"testing"
)

func ipPrioritySets() (*SetConfig, *SetConfig) {
	off := NewSetConfig()
	off.Id = "off"
	off.Name = "Off"
	off.Targets.IPs = []string{"37.18.153.136/29"}
	on := NewSetConfig()
	on.Id = "on"
	on.Name = "On"
	on.Targets.IPs = []string{"37.18.153.136/29"}
	on.Targets.IPPriority = true
	return &off, &on
}

func sparseTargets(t *testing.T, raw json.RawMessage) map[string]any {
	t.Helper()
	var set map[string]any
	if err := json.Unmarshal(raw, &set); err != nil {
		t.Fatal(err)
	}
	targets, _ := set["targets"].(map[string]any)
	return targets
}

func TestIPPriorityDefaultsToOff(t *testing.T) {
	if DefaultSetConfig.Targets.IPPriority {
		t.Fatal("ip_priority must default to off so that no existing set changes behaviour")
	}
	if set := NewSetConfig(); set.Targets.IPPriority {
		t.Fatal("NewSetConfig must start with ip_priority off")
	}
}

func TestIPPrioritySparseRoundTrip(t *testing.T) {
	off, on := ipPrioritySets()
	cfg := NewConfig()
	cfg.Sets = []*SetConfig{off, on}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	var raw struct {
		Sets []json.RawMessage `json:"sets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if len(raw.Sets) != 2 {
		t.Fatalf("got %d sets", len(raw.Sets))
	}
	if _, found := sparseTargets(t, raw.Sets[0])["ip_priority"]; found {
		t.Error("a set with ip_priority off must not write it")
	}
	if v, found := sparseTargets(t, raw.Sets[1])["ip_priority"]; !found || v != true {
		t.Errorf("a set with ip_priority on must write it as true, got %v (found %v)", v, found)
	}

	path := filepath.Join(t.TempDir(), "b4.json")
	if err := cfg.SaveToFile(path); err != nil {
		t.Fatalf("SaveToFile: %v", err)
	}
	loaded := NewConfig()
	if err := loaded.LoadFromFile(path); err != nil {
		t.Fatalf("LoadFromFile: %v", err)
	}
	if len(loaded.Sets) != 2 {
		t.Fatalf("loaded %d sets", len(loaded.Sets))
	}
	if loaded.Sets[0].Targets.IPPriority {
		t.Error("the set saved with ip_priority off loaded with it on")
	}
	if !loaded.Sets[1].Targets.IPPriority {
		t.Error("the set saved with ip_priority on loaded with it off")
	}
}

func TestIPPrioritySparseSetExport(t *testing.T) {
	off, on := ipPrioritySets()
	for _, c := range []struct {
		set  *SetConfig
		want bool
	}{{off, false}, {on, true}} {
		full, err := SetToMap(c.set)
		if err != nil {
			t.Fatal(err)
		}
		sparse, err := SparsifySetMap(full)
		if err != nil {
			t.Fatal(err)
		}
		targets, _ := sparse["targets"].(map[string]any)
		v, found := targets["ip_priority"]
		if found != c.want || (found && v != true) {
			t.Errorf("set %s: exported ip_priority = %v (found %v)", c.set.Id, v, found)
		}
	}
}

func TestIPPriorityDecodesIntoADefaultSeededSet(t *testing.T) {
	set := NewSetConfig()
	if err := json.Unmarshal([]byte(`{"id":"ggc","name":"GGC","targets":{"ip":["37.18.153.136/29"],"ip_priority":true}}`), &set); err != nil {
		t.Fatal(err)
	}
	if !set.Targets.IPPriority {
		t.Error("ip_priority true was not decoded")
	}
	if set.Enabled != DefaultSetConfig.Enabled || set.Faking.SNI != DefaultSetConfig.Faking.SNI {
		t.Errorf("decoding a sparse set must keep the seeded defaults: enabled %v, faking.sni %v", set.Enabled, set.Faking.SNI)
	}
	if set.Targets.SNIDomains == nil || set.Targets.SourceDevices == nil {
		t.Error("decoding a sparse targets object must keep the seeded empty lists")
	}

	plain := NewSetConfig()
	if err := json.Unmarshal([]byte(`{"id":"plain","targets":{"ip":["37.18.153.136/29"]}}`), &plain); err != nil {
		t.Fatal(err)
	}
	if plain.Targets.IPPriority {
		t.Error("a set that does not mention ip_priority must decode with it off")
	}
}

func TestIPPrioritySurvivesCloneAndReset(t *testing.T) {
	_, on := ipPrioritySets()
	cfg := NewConfig()
	cfg.Sets = []*SetConfig{on}

	clone := cfg.Clone()
	if len(clone.Sets) != 1 || !clone.Sets[0].Targets.IPPriority {
		t.Error("Clone dropped ip_priority")
	}

	on.Faking.TTL = 3
	on.ResetToDefaults()
	if !on.Targets.IPPriority {
		t.Error("resetting a set's strategy must keep its targets, ip_priority included")
	}
}
