package mtproto

import (
	"net"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

type closeRecordConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *closeRecordConn) Close() error {
	c.closed.Store(true)
	return nil
}

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

func revocationTestServer(t *testing.T) (*Server, func(mut func(*config.MTProtoConfig)) *config.Config, *Secret, *Secret) {
	t.Helper()

	genA, err := GenerateSecret("a.example.com")
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	genB, err := GenerateSecret("b.example.com")
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}
	hexA, hexB := genA.Hex(), genB.Hex()

	mkCfg := func(mut func(*config.MTProtoConfig)) *config.Config {
		cfg := &config.Config{}
		cfg.System.MTProto = config.MTProtoConfig{
			Enabled: true,
			Port:    3128,
			Secrets: []config.MTProtoSecret{
				{ID: "a", Name: "Max", Secret: hexA, Enabled: true},
				{ID: "b", Name: "Ivan", Secret: hexB, Enabled: true},
			},
		}
		if mut != nil {
			mut(&cfg.System.MTProto)
		}
		return cfg
	}

	cfg := mkCfg(nil)
	srv := NewServer(cfg)
	secrets, err := buildSecrets(cfg)
	if err != nil {
		t.Fatalf("buildSecrets: %v", err)
	}
	srv.secrets.Store(&secrets)
	srv.running = true

	var secA, secB *Secret
	for _, s := range secrets {
		switch s.ID {
		case "a":
			secA = s
		case "b":
			secB = s
		}
	}
	if secA == nil || secB == nil {
		t.Fatalf("built secrets missing: a=%v b=%v", secA, secB)
	}
	return srv, mkCfg, secA, secB
}

func TestDisableSecretClosesActiveConns(t *testing.T) {
	srv, mkCfg, secA, secB := revocationTestServer(t)

	connA := &closeRecordConn{}
	connB := &closeRecordConn{}
	srv.trackConn(secA, connA)
	srv.trackConn(secB, connB)

	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Secrets[0].Enabled = false
	}))

	if !connA.closed.Load() {
		t.Fatalf("connection for disabled secret was not closed")
	}
	if connB.closed.Load() {
		t.Fatalf("connection for still-enabled secret was closed")
	}
	if srv.secretActive(secA) {
		t.Fatalf("disabled secret still reported active")
	}
	if !srv.secretActive(secB) {
		t.Fatalf("enabled secret no longer reported active")
	}
}

func TestRotateSecretClosesOldConns(t *testing.T) {
	srv, mkCfg, secA, secB := revocationTestServer(t)

	genNew, err := GenerateSecret("a.example.com")
	if err != nil {
		t.Fatalf("GenerateSecret: %v", err)
	}

	connA := &closeRecordConn{}
	connB := &closeRecordConn{}
	srv.trackConn(secA, connA)
	srv.trackConn(secB, connB)

	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Secrets[0].Secret = genNew.Hex()
	}))

	if !connA.closed.Load() {
		t.Fatalf("connection for rotated secret was not closed")
	}
	if connB.closed.Load() {
		t.Fatalf("connection for untouched secret was closed")
	}
}

func TestDisableProxyClosesAllConns(t *testing.T) {
	srv, mkCfg, secA, secB := revocationTestServer(t)

	connA := &closeRecordConn{}
	connB := &closeRecordConn{}
	srv.trackConn(secA, connA)
	srv.trackConn(secB, connB)

	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Enabled = false
	}))

	if !connA.closed.Load() || !connB.closed.Load() {
		t.Fatalf("disabling the proxy left connections open: a=%v b=%v", connA.closed.Load(), connB.closed.Load())
	}
}

func TestRenameSecretKeepsConns(t *testing.T) {
	srv, mkCfg, secA, _ := revocationTestServer(t)

	connA := &closeRecordConn{}
	srv.trackConn(secA, connA)

	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Secrets[0].Name = "Maxim"
	}))

	if connA.closed.Load() {
		t.Fatalf("renaming a secret closed its connections")
	}
	if !srv.secretActive(secA) {
		t.Fatalf("renamed secret no longer reported active")
	}
}

func TestUntrackRemovesConn(t *testing.T) {
	srv, _, secA, _ := revocationTestServer(t)

	connA := &closeRecordConn{}
	untrack := srv.trackConn(secA, connA)
	untrack()

	srv.closeRevokedConns(nil)
	if connA.closed.Load() {
		t.Fatalf("untracked connection was closed by sweep")
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
