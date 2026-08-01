package nfq

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestRegisterLearnedRoute_SkipsDomainOnly(t *testing.T) {
	prevIP := RoutingLearnIPFunc
	prevHost := RoutingLearnHostFunc
	defer func() {
		RoutingLearnIPFunc = prevIP
		RoutingLearnHostFunc = prevHost
	}()

	var calls int
	RoutingLearnIPFunc = func(cfg *config.Config, set *config.SetConfig, ip net.IP) {
		calls++
	}
	var hostCalls int
	var lastHost string
	RoutingLearnHostFunc = func(cfg *config.Config, set *config.SetConfig, host string) {
		hostCalls++
		lastHost = host
	}

	cfg := &config.Config{}
	dst := net.ParseIP("1.2.3.4")

	set := config.NewSetConfig()
	set.Name = "cdn"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Targets.DomainOnly = true
	registerLearnedRoute(cfg, &set, dst, "cdn.example.com")
	if calls != 0 {
		t.Errorf("domain-only set must not register a learned route, got %d calls", calls)
	}
	if hostCalls != 0 {
		t.Errorf("domain-only set must not register a learned host, got %d calls", hostCalls)
	}

	set.Targets.DomainOnly = false
	registerLearnedRoute(cfg, &set, dst, "cdn.example.com")
	if calls != 1 {
		t.Errorf("non-domain-only set must register a learned route, got %d calls", calls)
	}
	if hostCalls != 1 || lastHost != "cdn.example.com" {
		t.Errorf("non-domain-only set must register the matched host, got %d calls (host %q)", hostCalls, lastHost)
	}

	registerLearnedRoute(cfg, &set, dst, "")
	if hostCalls != 1 {
		t.Errorf("empty host must not be registered, got %d calls", hostCalls)
	}
}
