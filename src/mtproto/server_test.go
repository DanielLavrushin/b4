package mtproto

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func mtprotoCfg(mut func(*config.MTProtoConfig)) *config.Config {
	cfg := &config.Config{}
	cfg.System.MTProto = config.MTProtoConfig{
		Enabled: true,
		Port:    3128,
		Secrets: []config.MTProtoSecret{
			{ID: "a", Name: "Max", Secret: "sec-a", Enabled: true},
		},
	}
	if mut != nil {
		mut(&cfg.System.MTProto)
	}
	return cfg
}

func TestMTProtoNeedsRestart_SecretsAreLive(t *testing.T) {
	base := mtprotoCfg(nil)

	cases := []struct {
		name string
		mut  func(*config.MTProtoConfig)
		want bool
	}{
		{"identical config", nil, false},
		{"rename secret", func(m *config.MTProtoConfig) { m.Secrets[0].Name = "Ivan" }, false},
		{"disable secret", func(m *config.MTProtoConfig) { m.Secrets[0].Enabled = false }, false},
		{"add secret", func(m *config.MTProtoConfig) {
			m.Secrets = append(m.Secrets, config.MTProtoSecret{ID: "b", Name: "Ivan", Secret: "sec-b", Enabled: true})
		}, false},
		{"rotate secret value", func(m *config.MTProtoConfig) { m.Secrets[0].Secret = "sec-a2" }, false},
		{"change port", func(m *config.MTProtoConfig) { m.Port = 4000 }, true},
		{"toggle proxy", func(m *config.MTProtoConfig) { m.Enabled = false }, true},
		{"change bind address", func(m *config.MTProtoConfig) { m.BindAddress = "127.0.0.1" }, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			newCfg := mtprotoCfg(tc.mut)
			if got := mtprotoNeedsRestart(base, newCfg); got != tc.want {
				t.Fatalf("mtprotoNeedsRestart = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestMTProtoMaxConnections(t *testing.T) {
	cases := []struct {
		name string
		set  int
		want int
	}{
		{"legacy config omits the field (zero) -> default 2048", 0, defaultMaxConnections},
		{"explicit value is honored", 5000, 5000},
		{"explicit low value is honored", 64, 64},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := &config.Config{}
			cfg.System.MTProto.MaxConnections = tc.set
			if got := mtprotoMaxConnections(cfg); got != tc.want {
				t.Fatalf("mtprotoMaxConnections(%d) = %d, want %d", tc.set, got, tc.want)
			}
		})
	}
}
