package socks5

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestRelaxingTheSOCKS5GateRebindsTheListener(t *testing.T) {
	base := config.Socks5Config{Enabled: true, Port: 1080, Username: "u", Password: "p", AllowedSources: []string{"10.0.0.0/8"}}
	cases := []struct {
		name   string
		mutate func(*config.Socks5Config)
		want   bool
	}{
		{"credentials cleared", func(c *config.Socks5Config) { c.Username, c.Password = "", "" }, true},
		{"password changed", func(c *config.Socks5Config) { c.Password = "q" }, false},
		{"sources changed behind credentials", func(c *config.Socks5Config) { c.AllowedSources = nil }, false},
		{"unchanged", func(c *config.Socks5Config) {}, false},
	}
	for _, tc := range cases {
		next := base
		next.AllowedSources = append([]string{}, base.AllowedSources...)
		tc.mutate(&next)
		if got := socks5NeedsRestart(&base, &next); got != tc.want {
			t.Errorf("%s: restart=%v, want %v", tc.name, got, tc.want)
		}
	}
}
