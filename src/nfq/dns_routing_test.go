package nfq

import (
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestRegisterLearnedRoutePrefersAsyncHooks(t *testing.T) {
	prevIP, prevHost := RoutingLearnIPFunc, RoutingLearnHostFunc
	prevIPAsync, prevHostAsync := RoutingLearnIPAsyncFunc, RoutingLearnHostAsyncFunc
	defer func() {
		RoutingLearnIPFunc, RoutingLearnHostFunc = prevIP, prevHost
		RoutingLearnIPAsyncFunc, RoutingLearnHostAsyncFunc = prevIPAsync, prevHostAsync
	}()

	var syncIP, syncHost, asyncIP, asyncHost int
	RoutingLearnIPFunc = func(*config.Config, *config.SetConfig, net.IP) { syncIP++ }
	RoutingLearnHostFunc = func(*config.Config, *config.SetConfig, string) { syncHost++ }
	RoutingLearnIPAsyncFunc = func(*config.Config, *config.SetConfig, net.IP) { asyncIP++ }
	RoutingLearnHostAsyncFunc = func(*config.Config, *config.SetConfig, string) { asyncHost++ }

	set := config.NewSetConfig()
	set.Enabled = true
	set.Routing.Enabled = true

	registerLearnedRoute(&config.Config{}, &set, net.ParseIP("1.2.3.4"), "example.com")

	if asyncIP != 1 || asyncHost != 1 {
		t.Fatalf("async hooks not used: ip=%d host=%d", asyncIP, asyncHost)
	}
	if syncIP != 0 || syncHost != 0 {
		t.Fatalf("blocking hooks must not run on the reader goroutine: ip=%d host=%d", syncIP, syncHost)
	}
}

func TestRegisterEscalatedRoutePrefersAsyncHook(t *testing.T) {
	prevSync, prevAsync := RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc
	defer func() {
		RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc = prevSync, prevAsync
	}()

	var sync, async int
	RoutingHandleDNSFunc = func(*config.Config, *config.SetConfig, []net.IP) { sync++ }
	RoutingHandleDNSAsyncFunc = func(*config.Config, *config.SetConfig, []net.IP) { async++ }

	set := config.NewSetConfig()
	set.Enabled = true
	set.Routing.Enabled = true

	registerEscalatedRoute(&config.Config{}, &set, net.ParseIP("1.2.3.4"))

	if async != 1 || sync != 0 {
		t.Fatalf("escalated route must use the async hook: async=%d sync=%d", async, sync)
	}
}

func TestRoutingHandleDNSAsyncFallsBackToSyncHook(t *testing.T) {
	prevSync, prevAsync := RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc
	defer func() {
		RoutingHandleDNSFunc, RoutingHandleDNSAsyncFunc = prevSync, prevAsync
	}()

	var sync int
	RoutingHandleDNSFunc = func(*config.Config, *config.SetConfig, []net.IP) { sync++ }
	RoutingHandleDNSAsyncFunc = nil

	if !routingHandleDNSAvailable() {
		t.Fatal("sync hook alone must count as available")
	}
	routingHandleDNSAsync(&config.Config{}, nil, nil)
	if sync != 1 {
		t.Fatalf("expected fallback to the sync hook, got %d calls", sync)
	}

	RoutingHandleDNSFunc = nil
	if routingHandleDNSAvailable() {
		t.Fatal("no hooks means unavailable")
	}
}
