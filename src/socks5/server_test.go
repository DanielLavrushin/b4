package socks5

import (
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
