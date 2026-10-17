package config

import "testing"

func TestAnySetTCPIncoming(t *testing.T) {
	set := func(enabled bool, mode string) *SetConfig {
		s := NewSetConfigWithDefaults()
		s.Enabled = enabled
		s.TCP.Incoming.Mode = mode
		return &s
	}
	cases := []struct {
		name string
		sets []*SetConfig
		want bool
	}{
		{"no sets", nil, false},
		{"a set left at the default", []*SetConfig{set(true, NewSetConfigWithDefaults().TCP.Incoming.Mode)}, false},
		{"a set with the mode off", []*SetConfig{set(true, ConfigOff)}, false},
		{"a set with no mode", []*SetConfig{set(true, "")}, false},
		{"a disabled set with a mode", []*SetConfig{set(false, "fake")}, false},
		{"a missing set", []*SetConfig{nil}, false},
		{"fake", []*SetConfig{set(true, ConfigOff), set(true, "fake")}, true},
		{"reset", []*SetConfig{set(true, "reset")}, true},
		{"fin", []*SetConfig{set(true, "fin")}, true},
		{"desync", []*SetConfig{set(true, "desync")}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := NewConfig()
			cfg.Sets = tc.sets
			if got := cfg.AnySetTCPIncoming(); got != tc.want {
				t.Errorf("AnySetTCPIncoming() = %v, want %v", got, tc.want)
			}
		})
	}
}
