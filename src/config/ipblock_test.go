package config

import (
	"encoding/json"
	"testing"
	"time"
)

func TestNormalizeIPBlockAction(t *testing.T) {
	cases := map[string]string{
		"":        IPBlockActionRST,
		"rst":     IPBlockActionRST,
		"heal":    IPBlockActionHeal,
		"proxy":   IPBlockActionProxy,
		"garbage": IPBlockActionRST,
	}
	for in, want := range cases {
		if got := NormalizeIPBlockAction(in); got != want {
			t.Errorf("NormalizeIPBlockAction(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIPBlockDetectResolvedDefaults(t *testing.T) {
	var zero IPBlockDetectConfig

	if got := zero.ResolvedAction(); got != IPBlockActionRST {
		t.Errorf("ResolvedAction = %q, want the reset action", got)
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
	if got := nilCfg.ResolvedAction(); got != IPBlockActionRST {
		t.Errorf("nil ResolvedAction = %q, want the reset action", got)
	}
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
	set.TCP.IPBlockDetect.Action = "nonsense"
	set.TCP.IPBlockDetect.SynThreshold = 0
	set.TCP.IPBlockDetect.BlockedTTLSec = 0
	set.TCP.IPBlockDetect.HealTTLSec = 0
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	ibd := cfg.Sets[0].TCP.IPBlockDetect
	if ibd.Action != IPBlockActionRST {
		t.Errorf("action = %q, want an unknown action to fall back to reset", ibd.Action)
	}
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

func TestValidateIPBlockProxyActionNeedsProxyRouting(t *testing.T) {
	t.Run("without proxy routing", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "s1"
		set.TCP.IPBlockDetect.Enabled = true
		set.TCP.IPBlockDetect.Action = IPBlockActionProxy
		cfg.Sets = []*SetConfig{&set}

		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if got := cfg.Sets[0].TCP.IPBlockDetect.Action; got != IPBlockActionRST {
			t.Errorf("action = %q, want a fallback to reset when there is no upstream proxy", got)
		}
	})

	t.Run("with proxy routing", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "s1"
		set.TCP.IPBlockDetect.Enabled = true
		set.TCP.IPBlockDetect.Action = IPBlockActionProxy
		set.Routing.Enabled = true
		set.Routing.Mode = RoutingModeProxy
		set.Routing.Upstream.Host = "10.0.0.1"
		set.Routing.Upstream.Port = 1080
		cfg.Sets = []*SetConfig{&set}

		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if got := cfg.Sets[0].TCP.IPBlockDetect.Action; got != IPBlockActionProxy {
			t.Errorf("action = %q, want the proxy action kept", got)
		}
	})

	t.Run("detection off leaves the action alone", func(t *testing.T) {
		cfg := NewConfig()
		set := NewSetConfig()
		set.Id = "s1"
		set.TCP.IPBlockDetect.Enabled = false
		set.TCP.IPBlockDetect.Action = IPBlockActionProxy
		cfg.Sets = []*SetConfig{&set}

		if err := cfg.Validate(); err != nil {
			t.Fatalf("Validate: %v", err)
		}
		if got := cfg.Sets[0].TCP.IPBlockDetect.Action; got != IPBlockActionProxy {
			t.Errorf("action = %q, want it untouched while detection is off", got)
		}
	})
}
