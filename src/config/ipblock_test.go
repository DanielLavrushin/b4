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

func TestIPHealthBanResolvedDefaults(t *testing.T) {
	var zero IPHealthConfig
	if !zero.BansEnabled() {
		t.Error("the zero value must leave bans on: bans_off is the switch")
	}
	if got := zero.BanTTL(); got != DefaultIPBanTTLSec*time.Second {
		t.Errorf("BanTTL = %s, want %ds", got, DefaultIPBanTTLSec)
	}
	if got := zero.RepeatBanTTL(); got != DefaultIPRepeatBanTTLSec*time.Second {
		t.Errorf("RepeatBanTTL = %s, want %ds", got, DefaultIPRepeatBanTTLSec)
	}

	var nilCfg *IPHealthConfig
	if !nilCfg.BansEnabled() || nilCfg.BanTTL() != 24*time.Hour || nilCfg.RepeatBanTTL() != 7*24*time.Hour {
		t.Error("a nil ip_health section resolves to bans on with the default durations")
	}

	off := IPHealthConfig{BansOff: true}
	if off.BansEnabled() {
		t.Error("bans_off must turn bans off")
	}

	cfg := NewConfig()
	if cfg.System.IPHealth.BansOff || cfg.System.IPHealth.BanTTLSec != DefaultIPBanTTLSec || cfg.System.IPHealth.RepeatBanTTLSec != DefaultIPRepeatBanTTLSec {
		t.Errorf("default config ip_health = %+v", cfg.System.IPHealth)
	}
}

func TestIPHealthBanValidationFillsAndClamps(t *testing.T) {
	cases := []struct {
		first, repeat         int
		wantFirst, wantRepeat int
	}{
		{0, 0, DefaultIPBanTTLSec, DefaultIPRepeatBanTTLSec},
		{-5, -5, DefaultIPBanTTLSec, DefaultIPRepeatBanTTLSec},
		{60, 100, MinIPBanTTLSec, MinIPBanTTLSec},
		{3600, 86400, 3600, 86400},
		{MaxIPBanTTLSec + 1, MaxIPRepeatBanTTLSec + 1, MaxIPBanTTLSec, MaxIPRepeatBanTTLSec},
		{MaxIPRepeatBanTTLSec, MaxIPBanTTLSec, MaxIPBanTTLSec, MaxIPBanTTLSec},
	}
	for _, c := range cases {
		raw := IPHealthConfig{BanTTLSec: c.first, RepeatBanTTLSec: c.repeat}
		if got := raw.BanTTL(); got != time.Duration(c.wantFirst)*time.Second {
			t.Errorf("BanTTL(%d) = %s, want %ds", c.first, got, c.wantFirst)
		}
		if got := raw.RepeatBanTTL(); got != time.Duration(c.wantRepeat)*time.Second {
			t.Errorf("RepeatBanTTL(%d) = %s, want %ds", c.repeat, got, c.wantRepeat)
		}

		cfg := NewConfig()
		cfg.System.IPHealth.BanTTLSec = c.first
		cfg.System.IPHealth.RepeatBanTTLSec = c.repeat
		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if got := cfg.System.IPHealth.BanTTLSec; got != c.wantFirst {
			t.Errorf("validated ban_ttl_sec %d -> %d, want %d", c.first, got, c.wantFirst)
		}
		if got := cfg.System.IPHealth.RepeatBanTTLSec; got != c.wantRepeat {
			t.Errorf("validated repeat_ban_ttl_sec %d -> %d, want %d", c.repeat, got, c.wantRepeat)
		}
	}
}

func TestIPHealthBanSettingsAreSparse(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "s1"
	cfg.Sets = []*SetConfig{&set}
	cfg.System.IPHealth.BanTTLSec = 0
	cfg.System.IPHealth.RepeatBanTTLSec = 0
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	ipHealthOf := func() map[string]any {
		t.Helper()
		data, err := MarshalSparse(&cfg)
		if err != nil {
			t.Fatalf("MarshalSparse: %v", err)
		}
		var raw map[string]any
		if err := json.Unmarshal(data, &raw); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		system, _ := raw["system"].(map[string]any)
		ipHealth, _ := system["ip_health"].(map[string]any)
		return ipHealth
	}

	if got := ipHealthOf(); got != nil {
		t.Errorf("ip_health = %v, want it left out while every ban setting holds its default", got)
	}

	cfg.System.IPHealth.BansOff = true
	cfg.System.IPHealth.BanTTLSec = 3600
	got := ipHealthOf()
	if got["bans_off"] != true || got["ban_ttl_sec"] != float64(3600) {
		t.Errorf("ip_health = %v, want bans_off and ban_ttl_sec written", got)
	}
	if _, present := got["repeat_ban_ttl_sec"]; present {
		t.Errorf("repeat_ban_ttl_sec was written while it holds the default: %v", got)
	}

	var loaded Config
	if err := json.Unmarshal([]byte(`{"system":{"ip_health":{"bans_off":true}}}`), &loaded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	ApplyConfigDefaults(&loaded)
	if loaded.System.IPHealth.BansEnabled() || loaded.System.IPHealth.BanTTLSec != DefaultIPBanTTLSec || loaded.System.IPHealth.RepeatBanTTLSec != DefaultIPRepeatBanTTLSec {
		t.Errorf("a file that only turns bans off keeps the default durations: %+v", loaded.System.IPHealth)
	}
}
