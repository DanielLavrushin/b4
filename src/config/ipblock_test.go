package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestIPBlockDetectResolvedDefaults(t *testing.T) {
	var zero IPBlockDetectConfig

	if zero.HealDNS {
		t.Errorf("HealDNS = true, want DNS curation off unless asked for")
	}
	if got := zero.ResolvedSynThreshold(); got != DefaultIPBlockSynThreshold {
		t.Errorf("ResolvedSynThreshold = %d, want %d", got, DefaultIPBlockSynThreshold)
	}
	if got := zero.ResolvedHealTTL(); got != DefaultIPBlockHealTTLSec {
		t.Errorf("ResolvedHealTTL = %d, want %d", got, DefaultIPBlockHealTTLSec)
	}

	var nilCfg *IPBlockDetectConfig
	if got := nilCfg.ResolvedSynThreshold(); got != DefaultIPBlockSynThreshold {
		t.Errorf("nil ResolvedSynThreshold = %d, want %d", got, DefaultIPBlockSynThreshold)
	}
}

func TestIPBlockDetectSynDetectDefaultsOn(t *testing.T) {
	set := NewSetConfig()
	if !set.TCP.IPBlockDetect.SynDetect {
		t.Errorf("a new set should watch unanswered SYNs, which is the case the feature is documented for")
	}

	if err := json.Unmarshal([]byte(`{"id":"s1"}`), &set); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !set.TCP.IPBlockDetect.SynDetect {
		t.Errorf("a config that omits syn_detect should keep the default of on")
	}

	if err := json.Unmarshal([]byte(`{"tcp":{"ip_block_detect":{"syn_detect":false}}}`), &set); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if set.TCP.IPBlockDetect.SynDetect {
		t.Errorf("an explicit syn_detect=false should be honoured")
	}
}

func TestValidateIPBlockDetectFillsBlanks(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "s1"
	set.TCP.IPBlockDetect.Enabled = true
	set.TCP.IPBlockDetect.SynThreshold = 0
	set.TCP.IPBlockDetect.HealTTLSec = 0
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	ibd := cfg.Sets[0].TCP.IPBlockDetect
	if ibd.SynThreshold != DefaultIPBlockSynThreshold {
		t.Errorf("syn_threshold = %d, want %d", ibd.SynThreshold, DefaultIPBlockSynThreshold)
	}
	if ibd.HealTTLSec != DefaultIPBlockHealTTLSec {
		t.Errorf("heal_ttl_sec = %d, want %d", ibd.HealTTLSec, DefaultIPBlockHealTTLSec)
	}
}

func TestIPHealthRetestIntervalIsGlobal(t *testing.T) {
	var zero IPHealthConfig
	if got := zero.RetestInterval(); got != DefaultIPHealthRetestSec*time.Second {
		t.Errorf("RetestInterval = %s, want %ds", got, DefaultIPHealthRetestSec)
	}

	var nilCfg *IPHealthConfig
	if got := nilCfg.RetestInterval(); got != DefaultIPHealthRetestSec*time.Second {
		t.Errorf("nil RetestInterval = %s, want %ds", got, DefaultIPHealthRetestSec)
	}

	cfg := NewConfig()
	cfg.System.IPHealth.RetestIntervalSec = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.System.IPHealth.RetestIntervalSec != DefaultIPHealthRetestSec {
		t.Errorf("retest_interval_sec = %d, want validation to fill the default", cfg.System.IPHealth.RetestIntervalSec)
	}

	cfg.System.IPHealth.RetestIntervalSec = 90
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if got := cfg.System.IPHealth.RetestInterval(); got != 90*time.Second {
		t.Errorf("RetestInterval = %s, want an explicit 90s kept", got)
	}
}

func TestIPHealthSparseRoundtrip(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "s1"
	cfg.Sets = []*SetConfig{&set}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if system, ok := raw["system"].(map[string]any); ok {
		if _, present := system["ip_health"]; present {
			t.Errorf("ip_health was written while it holds the default; the config file omits defaults")
		}
	}

	cfg.System.IPHealth.RetestIntervalSec = 900
	data, err = MarshalSparse(&cfg)
	if err != nil {
		t.Fatalf("MarshalSparse: %v", err)
	}
	raw = map[string]any{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	system, ok := raw["system"].(map[string]any)
	if !ok {
		t.Fatal("system section missing")
	}
	ipHealth, ok := system["ip_health"].(map[string]any)
	if !ok {
		t.Fatal("ip_health missing after being set away from the default")
	}
	if got := ipHealth["retest_interval_sec"]; got != float64(900) {
		t.Errorf("retest_interval_sec = %v, want 900", got)
	}
}
