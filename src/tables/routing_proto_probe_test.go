package tables

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"
)

func probeRouteProtocols(t *testing.T, ip func(args []string) (string, error)) (bool, []string) {
	t.Helper()
	stubBinaries(t, "ip")
	prevRun := run
	t.Cleanup(func() {
		run = prevRun
		routeProtoOnce = sync.Once{}
		routeProtoOK = false
	})
	var asked []string
	run = func(args ...string) (string, error) {
		asked = append(asked, strings.Join(args, " "))
		return ip(args)
	}
	routeProtoOnce = sync.Once{}
	routeProtoOK = false
	return routeIPSupportsProtoExec(), asked
}

func routeProbeShowArgs(args []string) (table, proto string, ok bool) {
	if len(args) < 3 || args[0] != "ip" || args[1] != "route" || (args[2] != "show" && args[2] != "list") {
		return "", "", false
	}
	return routeRuleField(strings.Join(args, " "), "table"), routeRuleField(strings.Join(args, " "), "proto"), true
}

func routeProbeKnownProto(proto string) bool {
	if proto == "" {
		return true
	}
	if _, err := fmt.Sscanf(proto, "%d", new(int)); err == nil {
		return true
	}
	return slices.Contains([]string{"kernel", "boot", "static", "dhcp"}, proto)
}

func strictDumpIproute2(args []string) (string, error) {
	if strings.Join(args, " ") == "ip -V" {
		return "ip utility, iproute2-6.19.0, libbpf 1.6.3\n", nil
	}
	table, proto, ok := routeProbeShowArgs(args)
	if !ok {
		return "", errors.New("exit status 255")
	}
	if !routeProbeKnownProto(proto) {
		return fmt.Sprintf("Error: argument %q is wrong: invalid \"protocol\"\n", proto), errors.New("exit status 255")
	}
	switch table {
	case "", "main", "local":
		return "", nil
	}
	return "Error: ipv4: FIB table does not exist.\nDump terminated\n", errors.New("exit status 2")
}

func busyBoxIP(args []string) (string, error) {
	if len(args) > 1 && args[1] == "-V" {
		return "BusyBox v1.37.0 multi-call binary.\n\nUsage: ip [OPTIONS] address|route|link|tunnel|neigh|rule [ARGS]\n", errors.New("exit status 1")
	}
	table, proto, ok := routeProbeShowArgs(args)
	if !ok {
		return "", errors.New("exit status 1")
	}
	if !routeProbeKnownProto(proto) {
		return fmt.Sprintf("ip: invalid argument '%s' to 'protocol'\n", proto), errors.New("exit status 1")
	}
	var id int
	if _, err := fmt.Sscanf(table, "%d", &id); err == nil && id >= 10000 {
		return fmt.Sprintf("ip: invalid argument '%s' to 'table'\n", table), errors.New("exit status 1")
	}
	return "", nil
}

func TestTheRouteProtocolProbeSeesIproute2OnAKernelThatRefusesToListAMissingTable(t *testing.T) {
	ok, asked := probeRouteProtocols(t, strictDumpIproute2)
	if !ok {
		t.Fatalf("iproute2 on a kernel with strict dump checking understands route protocols, but the probe said it does not, so b4 leaves its routes untagged and has to recognise them by their shape. "+
			"A numbered table that does not exist fails to list there with \"FIB table does not exist\", which says nothing about protocols; the probe asked:\n%s", strings.Join(asked, "\n"))
	}
}

func TestTheRouteProtocolProbeStillSeesIproute2WhereAMissingTableListsAsEmpty(t *testing.T) {
	ok, asked := probeRouteProtocols(t, func(args []string) (string, error) {
		if strings.Join(args, " ") == "ip -V" {
			return "ip utility, iproute2-ss140804\n", nil
		}
		if _, _, ok := routeProbeShowArgs(args); !ok {
			return "", errors.New("exit status 255")
		}
		return "", nil
	})
	if !ok {
		t.Fatalf("an older kernel lists a missing table as empty and iproute2 there understands route protocols, the probe must keep saying so; it asked:\n%s", strings.Join(asked, "\n"))
	}
}

func TestTheRouteProtocolProbeLeavesBusyBoxIPOnTheInterfaceCheck(t *testing.T) {
	ok, asked := probeRouteProtocols(t, busyBoxIP)
	if ok {
		t.Fatalf("BusyBox ip accepts \"proto 155\" but never prints a route's protocol and ignores it as a filter, so b4 could not tell its own tagged routes from anyone else's and would call its own table foreign; the probe asked:\n%s", strings.Join(asked, "\n"))
	}
}

func TestTheRouteProtocolProbeSaysNoWhenIproute2RefusesTheProtocol(t *testing.T) {
	ok, _ := probeRouteProtocols(t, func(args []string) (string, error) {
		if strings.Join(args, " ") == "ip -V" {
			return "ip utility, iproute2-ss100224\n", nil
		}
		return "Error: argument \"155\" is wrong: invalid \"protocol\"\n", errors.New("exit status 255")
	})
	if ok {
		t.Fatal("an ip that rejects \"proto 155\" cannot tag routes, the probe must say so")
	}
}

func TestTheSweepTakesOutTheUntaggedRoutesAnOlderBuildLeftInItsOwnTable(t *testing.T) {
	stubBinaries(t, "ip")
	table, other := 137, 138
	mark := fmt.Sprintf("0x%x/0x%x", 0x1234, routeSetMarkMask)
	otherMark := fmt.Sprintf("0x%x/0x%x", 0x1235, routeSetMarkMask)
	rule := fmt.Sprintf("%d:\tfrom all fwmark %s lookup %d\n%d:\tfrom all fwmark %s lookup %d", routePolicyRuleBase+table, mark, table, routePolicyRuleBase+other, otherMark, other)
	routes := map[string]string{
		fmt.Sprintf("ip route show table %d", other): "default via 192.168.9.1 dev ovpn9",
		fmt.Sprintf("ip route show table %d", table): strings.Join([]string{
			"default via 10.7.0.1 dev wg0 src 10.7.0.2",
			"blackhole default metric " + routeKillSwitchMetric,
			"default via 192.168.2.1 dev eth1 metric 50",
			"default via 10.9.0.1 dev eth2 proto static",
			"10.0.0.0/8 via 10.0.0.1 dev tun0",
			"blackhole 10.99.0.0/16 metric 5",
		}, "\n"),
		fmt.Sprintf("ip -6 route show table %d", table): strings.Join([]string{
			"default via fe80::1 dev wg0 proto static metric 100 pref medium",
			"default dev wg0 metric 1024 pref medium",
			"blackhole default dev lo metric " + routeKillSwitchMetric + " pref medium",
			"default via fe80::1 dev eth1 metric 100 pref medium",
		}, "\n"),
	}

	prevRun, prevDel, prevProto := run, routeDelRuleLoop, routeIPSupportsProto
	t.Cleanup(func() { run, routeDelRuleLoop, routeIPSupportsProto = prevRun, prevDel, prevProto })
	routeIPSupportsProto = func() bool { return true }
	routeDelRuleLoop = func(bool, string, string) {}
	var executed []string
	run = func(args ...string) (string, error) {
		joined := strings.Join(args, " ")
		executed = append(executed, joined)
		switch {
		case joined == "ip rule show", joined == "ip -6 rule show":
			return rule, nil
		case strings.Contains(joined, " rule del "), strings.Contains(joined, " -D "):
			return "", errors.New("stub: no such rule")
		}
		return routes[joined], nil
	}

	routeSweepOwnRules()

	want := []string{
		fmt.Sprintf("ip route del default dev wg0 table %d", table),
		fmt.Sprintf("ip route del blackhole default metric %s table %d", routeKillSwitchMetric, table),
		fmt.Sprintf("ip -6 route del default dev wg0 metric 1024 table %d", table),
		fmt.Sprintf("ip -6 route del blackhole default metric %s table %d", routeKillSwitchMetric, table),
		fmt.Sprintf("ip route del default dev ovpn9 table %d", other),
	}
	var deleted []string
	for _, cmd := range executed {
		if strings.Contains(cmd, " route del ") {
			deleted = append(deleted, cmd)
		}
	}
	for _, cmd := range want {
		if n := slices.Index(deleted, cmd); n < 0 {
			t.Errorf("an untagged route an older b4 left in a table its own leftover rule points at must be swept, or the first start of this build calls the table foreign and leaves the route there until a reboot; %q never ran. Deletes run:\n%s", cmd, strings.Join(deleted, "\n"))
		}
	}
	for _, cmd := range deleted {
		if !slices.Contains(want, cmd) {
			t.Errorf("the sweep deleted a route that is not one b4 writes, or deleted with \"proto %s\", which never matches an untagged route: %s", routeProtoID, cmd)
		}
	}
}
