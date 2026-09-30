package handler

import (
	"slices"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/tables"
)

func TestBridgeNetfilterDiag(t *testing.T) {
	prev := routingTProxyFamilies
	t.Cleanup(func() { routingTProxyFamilies = prev })
	installed := func(ipv4, ipv6 bool) {
		routingTProxyFamilies = func() (bool, bool) { return ipv4, ipv6 }
	}
	newCfg := func(ipv6 bool) *config.Config {
		cfg := config.DefaultConfig
		cfg.Queue.IPv4Enabled = true
		cfg.Queue.IPv6Enabled = ipv6
		return &cfg
	}
	on := tables.BridgeNetfilter{GlobalV4: true, BridgesV4: []string{"br-lan", "docker0"}}
	both := tables.BridgeNetfilter{GlobalV4: true, GlobalV6: true, BridgesV4: []string{"br-lan"}, BridgesV6: []string{"br-lan", "br-v6"}}

	cases := []struct {
		name         string
		bn           tables.BridgeNetfilter
		cfg          *config.Config
		v4, v6       bool
		bridges      []string
		safe         bool
		sysctls, att []string
	}{
		{name: "off", bn: tables.BridgeNetfilter{}, cfg: newCfg(false), v4: true, safe: true},
		{name: "on without an installed transparent-proxy rule", bn: on, cfg: newCfg(false), bridges: []string{"br-lan", "docker0"}, safe: true},
		{name: "on with an installed IPv4 rule", bn: on, cfg: newCfg(false), v4: true, bridges: []string{"br-lan", "docker0"}, sysctls: []string{"net.bridge.bridge-nf-call-iptables"}},
		{name: "IPv6 bridges with IPv6 off", bn: tables.BridgeNetfilter{GlobalV6: true, BridgesV6: []string{"br-lan"}}, cfg: newCfg(false), v4: true, safe: true},
		{name: "only the installed families conflict", bn: both, cfg: newCfg(true), v4: true, bridges: []string{"br-lan"}, sysctls: []string{"net.bridge.bridge-nf-call-iptables"}},
		{name: "installed IPv6 rule", bn: both, cfg: newCfg(true), v4: true, v6: true, bridges: []string{"br-lan", "br-v6"}, sysctls: []string{"net.bridge.bridge-nf-call-iptables", "net.bridge.bridge-nf-call-ip6tables"}},
		{name: "per-bridge flag", bn: tables.BridgeNetfilter{BridgesV4: []string{"br-lan"}}, cfg: newCfg(false), v4: true, bridges: []string{"br-lan"}, att: []string{"nf_call_iptables"}},
		{name: "no config", bn: on, cfg: nil, bridges: []string{"br-lan", "docker0"}, safe: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			installed(tc.v4, tc.v6)
			bridges, safe, sysctls, attrs := bridgeNetfilterDiag(tc.bn, tc.cfg)
			if !slices.Equal(bridges, tc.bridges) || safe != tc.safe || !slices.Equal(sysctls, tc.sysctls) || !slices.Equal(attrs, tc.att) {
				t.Fatalf("got bridges %v safe=%v sysctls %v attrs %v, want %v %v %v %v", bridges, safe, sysctls, attrs, tc.bridges, tc.safe, tc.sysctls, tc.att)
			}
		})
	}
}
