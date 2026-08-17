package socks5

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestUpdateConfigSkipsMatcherWhenNotRunning(t *testing.T) {
	cfg := &config.Config{}
	s := NewServer(cfg)

	set := config.DefaultSetConfig
	set.Id = "s1"
	set.Enabled = true
	set.Targets.DomainsToMatch = []string{"example.com"}

	newCfg := &config.Config{Sets: []*config.SetConfig{&set}}
	s.UpdateConfig(newCfg)

	if s.getMatcher() != nil {
		t.Fatal("a stopped SOCKS5 server should not build or hold a matcher")
	}
}

func TestUpdateConfigBuildsMatcherWhenRunning(t *testing.T) {
	cfg := &config.Config{}
	s := NewServer(cfg)
	s.running.Store(true)

	set := config.DefaultSetConfig
	set.Id = "s1"
	set.Enabled = true
	set.Targets.DomainsToMatch = []string{"example.com"}

	newCfg := &config.Config{Sets: []*config.SetConfig{&set}}
	s.UpdateConfig(newCfg)

	m := s.getMatcher()
	if m == nil {
		t.Fatal("a running SOCKS5 server should build a matcher")
	}
	if matched, _ := m.MatchSNI("example.com"); !matched {
		t.Error("matcher should match the configured domain")
	}
}

func TestUpdateConfigStartsAndStopsListener(t *testing.T) {
	cfg := &config.Config{}
	cfg.System.Socks5 = config.Socks5Config{Enabled: false, BindAddress: "127.0.0.1", Port: 0}
	s := NewServer(cfg)

	if err := s.Start(); err != nil {
		t.Fatalf("start while disabled: %v", err)
	}
	if s.running.Load() {
		t.Fatal("a disabled server must not be listening")
	}

	enabled := &config.Config{}
	enabled.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: "127.0.0.1", Port: 0}
	s.UpdateConfig(enabled)

	if !s.running.Load() {
		t.Fatal("enabling SOCKS5 in the config must start the listener without a restart")
	}
	addr := s.listener.Addr().String()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatalf("listener not accepting on %s: %v", addr, err)
	}
	c.Close()

	disabled := &config.Config{}
	disabled.System.Socks5 = config.Socks5Config{Enabled: false, BindAddress: "127.0.0.1", Port: 0}
	s.UpdateConfig(disabled)

	if s.running.Load() {
		t.Fatal("disabling SOCKS5 in the config must stop the listener without a restart")
	}
	if _, err := net.Dial("tcp", addr); err == nil {
		t.Error("the old listener is still accepting after being disabled")
	}

	if err := s.Stop(); err != nil {
		t.Errorf("stopping an already stopped server: %v", err)
	}
}

func TestUpdateConfigRebindsOnPortChange(t *testing.T) {
	cfg := &config.Config{}
	cfg.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: "127.0.0.1", Port: freePort(t)}
	s := NewServer(cfg)
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	first := s.listener.Addr().String()

	moved := &config.Config{}
	moved.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: "127.0.0.1", Port: freePort(t)}
	s.UpdateConfig(moved)

	if !s.running.Load() {
		t.Fatal("server should still be running after a rebind")
	}
	if _, err := net.Dial("tcp", first); err == nil {
		t.Error("the previous listener was left open after the rebind")
	}
	c, err := net.Dial("tcp", s.listener.Addr().String())
	if err != nil {
		t.Fatalf("new listener not accepting: %v", err)
	}
	c.Close()
}
