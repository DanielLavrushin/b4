package tables

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestNetnsAFlushedBaseChainIsNoticed(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(backendIPTables)
	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	if !RoutingRulesPresent(cfg) {
		t.Fatalf("b4 has just installed its routing, so it must read as present")
	}

	if out, err := exec.Command("iptables", "-t", "mangle", "-F", "PREROUTING").CombinedOutput(); err != nil {
		t.Fatalf("flush PREROUTING: %v: %s", err, out)
	}

	if RoutingRulesPresent(cfg) {
		t.Fatalf("the firmware rebuilt its own firewall and took b4's jump out of mangle PREROUTING with it. " +
			"b4's own chains survive that, so checking only that they exist reads as healthy while no packet " +
			"reaches them: the set silently stops routing and nothing puts it back")
	}

	RoutingForceResync(cfg)
	if !RoutingRulesPresent(cfg) {
		t.Fatalf("a resync must put the jump back")
	}
}

func netnsProxyOnlyConfig() *config.Config {
	cfg := netnsConfig(backendIPTables)
	proxy := config.NewSetConfig()
	proxy.Id = "netns-jump-proxy"
	proxy.Name = "netnsjumpproxy"
	proxy.Enabled = true
	proxy.Routing.Enabled = true
	proxy.Routing.Mode = config.RoutingModeProxy
	proxy.Routing.Upstream.Host = "127.0.0.1"
	proxy.Routing.Upstream.Port = 1080
	proxy.Targets.IPs = []string{netnsProxyTarget}
	proxy.Targets.IpsToMatch = []string{netnsProxyTarget}
	cfg.Sets = append(cfg.Sets, &proxy)
	return cfg
}

func netnsPreroutingTargets(t *testing.T) []string {
	t.Helper()
	out := netnsRun(t, "iptables", "-w", "-t", "mangle", "-L", "PREROUTING", "--line-numbers", "-n")
	var targets []string
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 {
			continue
		}
		if _, err := strconv.Atoi(f[0]); err != nil {
			continue
		}
		targets = append(targets, f[1])
	}
	return targets
}

func netnsAssertPreJumpsAboveCapture(t *testing.T, why string) {
	t.Helper()
	targets := netnsPreroutingTargets(t)
	capture := -1
	for i, name := range targets {
		if name == captureChainPre {
			capture = i
		}
	}
	if capture < 0 {
		t.Fatalf("%s: the capture chain is not hooked into mangle PREROUTING at all: %v", why, targets)
	}
	for i, name := range targets {
		if routeIsPreChainName(name) && i > capture {
			t.Errorf("%s: %s sits below %s in mangle PREROUTING (%v). NFQUEUE is a terminating target, so the capture engine takes the reply packets of a diverted connection and the set's socket-match rule never runs - a proxy or mtproto-ws set then hands nothing to its listener",
				why, name, captureChainPre, targets)
		}
	}
}

func TestNetnsProxyJumpStaysAboveTheCaptureChain(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsProxyOnlyConfig()
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()
	netnsAssertPreJumpsAboveCapture(t, "after the first sync")

	RoutingForceResync(cfg)
	netnsAssertPreJumpsAboveCapture(t, "after a routing resync")

	if err := AddRules(cfg); err != nil {
		t.Fatalf("second AddRules: %v", err)
	}
	netnsAssertPreJumpsAboveCapture(t, "after the capture rules were rebuilt over the top")
}
