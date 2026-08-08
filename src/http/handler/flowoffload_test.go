package handler

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestNftFlowOffloadGuard(t *testing.T) {
	cases := []struct {
		name string
		rule string
		want int
	}{
		{"unguarded", "meta l4proto { tcp, udp } flow add @ft", 0},
		{"ge", "meta l4proto { tcp, udp } ct original packets ge 30 flow offload @ft", 30},
		{"symbolic ge", "meta l4proto { tcp, udp } ct original packets >= 40 flow add @ft", 40},
		{"gt", "meta l4proto { tcp, udp } ct original packets gt 29 flow add @ft", 30},
		{"symbolic gt", "meta l4proto { tcp, udp } ct original packets > 29 flow add @ft", 30},
		{"reply counter is not a guard", "ct reply packets ge 30 flow add @ft", 0},
		{"total counter is not a guard", "ct packets ge 30 flow add @ft", 0},
		{"zero threshold", "ct original packets ge 0 flow add @ft", 0},
		{"garbage threshold", "ct original packets ge abc flow add @ft", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := nftFlowOffloadGuard(tc.rule); got != tc.want {
				t.Errorf("nftFlowOffloadGuard(%q) = %d, want %d", tc.rule, got, tc.want)
			}
		})
	}
}

func TestIptablesFlowOffloadGuard(t *testing.T) {
	cases := []struct {
		name string
		rule string
		want int
	}{
		{"unguarded", "-A FORWARD -j FLOWOFFLOAD", 0},
		{"guarded", "-A FORWARD -m connbytes --connbytes 30:0 --connbytes-mode packets --connbytes-dir original -j FLOWOFFLOAD", 30},
		{"open upper bound", "-A FORWARD -m connbytes --connbytes 40: --connbytes-mode packets --connbytes-dir original -j FLOWOFFLOAD", 40},
		{"bytes mode is not a guard", "-A FORWARD -m connbytes --connbytes 30:0 --connbytes-mode bytes --connbytes-dir original -j FLOWOFFLOAD", 0},
		{"reply direction is not a guard", "-A FORWARD -m connbytes --connbytes 30:0 --connbytes-mode packets --connbytes-dir reply -j FLOWOFFLOAD", 0},
		{"inverted match is not a guard", "-A FORWARD -m connbytes ! --connbytes 0:29 --connbytes-mode packets --connbytes-dir original -j FLOWOFFLOAD", 0},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := iptablesFlowOffloadGuard(tc.rule); got != tc.want {
				t.Errorf("iptablesFlowOffloadGuard(%q) = %d, want %d", tc.rule, got, tc.want)
			}
		})
	}
}

func TestFlowOffloadSafe(t *testing.T) {
	newCfg := func() *config.Config {
		cfg := config.DefaultConfig
		return &cfg
	}

	t.Run("off is always safe", func(t *testing.T) {
		if !flowOffloadSafe("off", 0, newCfg()) {
			t.Error("expected off to be safe")
		}
	})

	t.Run("unguarded is unsafe", func(t *testing.T) {
		if flowOffloadSafe("software", 0, newCfg()) {
			t.Error("expected unguarded offload to be unsafe")
		}
	})

	t.Run("guard above the queue window is safe", func(t *testing.T) {
		cfg := newCfg()
		if !flowOffloadSafe("hardware", cfg.Queue.TCPConnBytesLimit+1, cfg) {
			t.Error("expected guard above the window to be safe")
		}
	})

	t.Run("guard equal to the queue window is unsafe", func(t *testing.T) {
		cfg := newCfg()
		if flowOffloadSafe("software", cfg.Queue.TCPConnBytesLimit, cfg) {
			t.Error("expected guard equal to the window to be unsafe")
		}
	})

	t.Run("udp window is taken into account", func(t *testing.T) {
		cfg := newCfg()
		cfg.Queue.TCPConnBytesLimit = 10
		cfg.Queue.UDPConnBytesLimit = 25
		if flowOffloadSafe("software", 20, cfg) {
			t.Error("expected guard below the udp window to be unsafe")
		}
		if !flowOffloadSafe("software", 26, cfg) {
			t.Error("expected guard above both windows to be safe")
		}
	})

	t.Run("duplication is never safe", func(t *testing.T) {
		cfg := newCfg()
		set := &config.SetConfig{Enabled: true}
		set.TCP.Duplicate.Enabled = true
		set.Targets.IpsToMatch = []string{"1.2.3.4"}
		cfg.Sets = []*config.SetConfig{set}
		if flowOffloadSafe("software", 1000, cfg) {
			t.Error("expected duplication sets to make any guard unsafe")
		}
	})
}
