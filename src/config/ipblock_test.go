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
	if got := zero.ResolvedBlockedTTL(); got != DefaultIPBlockBlockedTTLSec*time.Second {
		t.Errorf("ResolvedBlockedTTL = %s, want %ds", got, DefaultIPBlockBlockedTTLSec)
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
	set.TCP.IPBlockDetect.BlockedTTLSec = 0
	set.TCP.IPBlockDetect.HealTTLSec = 0
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	ibd := cfg.Sets[0].TCP.IPBlockDetect
	if ibd.SynThreshold != DefaultIPBlockSynThreshold {
		t.Errorf("syn_threshold = %d, want %d", ibd.SynThreshold, DefaultIPBlockSynThreshold)
	}
	if ibd.BlockedTTLSec != DefaultIPBlockBlockedTTLSec {
		t.Errorf("blocked_ttl_sec = %d, want %d", ibd.BlockedTTLSec, DefaultIPBlockBlockedTTLSec)
	}
	if ibd.HealTTLSec != DefaultIPBlockHealTTLSec {
		t.Errorf("heal_ttl_sec = %d, want %d", ibd.HealTTLSec, DefaultIPBlockHealTTLSec)
	}
}
