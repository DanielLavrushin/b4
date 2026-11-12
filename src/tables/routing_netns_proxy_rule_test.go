package tables

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

const (
	netnsDialLoopEnv     = "B4_NETNS_DIAL_LOOP"
	netnsDialLoopMsg     = "loop"
	netnsDialLoopReply   = "tproxy:" + netnsDialLoopMsg
	netnsDialLoopTimeout = 500 * time.Millisecond
	netnsRouterDialGap   = 10 * time.Millisecond
	netnsForwardSysctl   = "/proc/sys/net/ipv4/ip_forward"
)

type netnsDialTally struct {
	answered int
	lost     int
	held     int
	broken   int
	raw      string
}

func TestNetnsDeviceDialLoop(t *testing.T) {
	if os.Getenv(netnsDialLoopEnv) == "" {
		t.Skip("helper for the device side of the proxy rebuild tests")
	}
	addr := net.JoinHostPort(os.Getenv("B4_PROBE_HOST"), os.Getenv("B4_PROBE_PORT"))
	span, _ := time.ParseDuration(os.Getenv(netnsDialLoopEnv))
	until := time.Now().Add(span)

	held := make(chan [2]int, 1)
	go func() {
		exchanges, broken := netnsHoldConnection(addr, until)
		held <- [2]int{exchanges, broken}
	}()

	answered, lost := 0, 0
	for time.Now().Before(until) {
		if netnsDialLoopOnce(addr) {
			answered++
		} else {
			lost++
		}
	}
	h := <-held
	fmt.Printf("LOOP answered=%d lost=%d held=%d broken=%d\n", answered, lost, h[0], h[1])
}

func netnsDialLoopOnce(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, netnsDialLoopTimeout)
	if err != nil {
		return false
	}
	defer conn.Close()
	return netnsDialLoopExchange(conn)
}

func netnsDialLoopExchange(conn net.Conn) bool {
	_ = conn.SetDeadline(time.Now().Add(netnsDialLoopTimeout))
	if _, err := conn.Write([]byte(netnsDialLoopMsg)); err != nil {
		return false
	}
	buf := make([]byte, 64)
	n, err := conn.Read(buf)
	return err == nil && string(buf[:n]) == netnsDialLoopReply
}

func netnsHoldConnection(addr string, until time.Time) (exchanges, broken int) {
	conn, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err != nil {
		return 0, 1
	}
	defer conn.Close()
	for time.Now().Before(until) {
		if !netnsDialLoopExchange(conn) {
			return exchanges, 1
		}
		exchanges++
		time.Sleep(time.Millisecond)
	}
	return exchanges, 0
}

func (d *netnsDevice) dialLoop(host string, port int, span time.Duration) netnsDialTally {
	cmd := exec.Command("nsenter", "-t", strconv.Itoa(d.pid), "-n", os.Args[0], "-test.run", "^TestNetnsDeviceDialLoop$", "-test.v")
	cmd.Env = append(os.Environ(),
		netnsDialLoopEnv+"="+span.String(),
		"B4_PROBE_HOST="+host,
		"B4_PROBE_PORT="+strconv.Itoa(port),
	)
	out, _ := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		var tally netnsDialTally
		if _, err := fmt.Sscanf(line, "LOOP answered=%d lost=%d held=%d broken=%d", &tally.answered, &tally.lost, &tally.held, &tally.broken); err == nil {
			tally.raw = line
			return tally
		}
	}
	return netnsDialTally{raw: strings.TrimSpace(string(out))}
}

func netnsServeEcho(ln net.Listener) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 256)
			for {
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				n, err := c.Read(buf)
				if err != nil {
					return
				}
				if _, err := c.Write([]byte("tproxy:" + string(buf[:n]))); err != nil {
					return
				}
			}
		}(conn)
	}
}

func netnsTurnOnForwarding(t *testing.T) {
	t.Helper()
	prev, err := os.ReadFile(netnsForwardSysctl)
	if err != nil {
		t.Skipf("%s is not readable in this namespace: %v", netnsForwardSysctl, err)
	}
	if err := os.WriteFile(netnsForwardSysctl, []byte("1"), 0644); err != nil {
		t.Skipf("cannot turn on forwarding in this namespace: %v", err)
	}
	t.Cleanup(func() { _ = os.WriteFile(netnsForwardSysctl, prev, 0644) })
}

func netnsCounterBinary(engine string) string {
	if engine == backendNFTables {
		return backendIPTables
	}
	return engine
}

func netnsUplinkSpec(ipt, action string) []string {
	return []string{ipt, "-w", "-t", "mangle", action, "POSTROUTING",
		"-o", netnsPrimary, "-d", netnsDevInet, "-p", "tcp", "-j", "ACCEPT"}
}

func netnsCountUplinkPackets(t *testing.T, engine string) func() int {
	t.Helper()
	ipt := netnsCounterBinary(engine)
	for {
		if _, err := run(netnsUplinkSpec(ipt, "-D")...); err != nil {
			break
		}
	}
	netnsRun(t, netnsUplinkSpec(ipt, "-I")...)
	t.Cleanup(func() { _, _ = run(netnsUplinkSpec(ipt, "-D")...) })
	return func() int {
		out := netnsRun(t, ipt, "-w", "-t", "mangle", "-L", "POSTROUTING", "-v", "-x", "-n")
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 9 || f[6] != netnsPrimary || f[8] != netnsDevInet {
				continue
			}
			if n, err := strconv.Atoi(f[0]); err == nil {
				return n
			}
		}
		t.Fatalf("the uplink packet counter is gone:\n%s", out)
		return 0
	}
}

func netnsProxyRuleLines(t *testing.T, mark uint32, table int) []string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(netnsRun(t, "ip", "rule", "show"), "\n") {
		if routeRuleField(line, "fwmark") != routeSetMarkRule(mark) {
			continue
		}
		if routeRuleField(line, "lookup") != strconv.Itoa(table) {
			continue
		}
		found = append(found, strings.TrimSpace(line))
	}
	return found
}

func netnsProxySetUp(t *testing.T, engine string) (*netnsDevice, *config.Config, routeState) {
	t.Helper()
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	netnsTurnOnForwarding(t)
	dev := netnsStartDevice(t)

	routeEngine = nil
	cfg := netnsLocalGuardConfig(engine, "", []string{netnsDevInet})
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	RoutingSyncConfig(cfg)
	t.Cleanup(func() {
		RoutingClearAll()
		_ = ClearRules(cfg)
		routeEngine = nil
	})

	routeMu.Lock()
	st, ok := routeRuleCache["netns-localdst-proxy"]
	routeMu.Unlock()
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	port, _ := portFromState(st)
	go netnsServeEcho(netnsMarkedTransparentListener(t, port, SelfDialMark))
	return dev, cfg, st
}

func netnsARebuiltProxySetKeepsTakingItsConnections(t *testing.T, engine string) {
	dev, cfg, st := netnsProxySetUp(t, engine)
	uplink := netnsCountUplinkPackets(t, engine)
	addr := net.JoinHostPort(netnsDevInet, "443")

	if !netnsDialLoopOnce(addr) {
		netnsLogState(t, engine)
		t.Fatal("the set does not take the router's own connection before any rebuild")
	}
	if n := uplink(); n != 0 {
		t.Fatalf("%d packet(s) left by the uplink before any rebuild", n)
	}

	calm := dev.dialLoop(netnsDevInet, 443, 2*time.Second)
	t.Logf("%s: with no rebuild running: device %q, %d packet(s) left by the uplink", engine, calm.raw, uplink())
	if calm.answered == 0 || calm.lost != 0 || calm.broken != 0 || uplink() != 0 {
		t.Skipf("this namespace loses connections through the set while nothing is being rebuilt (%q, %d packet(s) on the uplink), so it cannot show what a rebuild does to them", calm.raw, uplink())
	}

	const span = 5 * time.Second
	deviceDone := make(chan netnsDialTally, 1)
	go func() { deviceDone <- dev.dialLoop(netnsDevInet, 443, span) }()

	stop := make(chan struct{})
	var wg sync.WaitGroup
	routerAnswered, routerLost := 0, 0
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			if netnsDialLoopOnce(addr) {
				routerAnswered++
			} else {
				routerLost++
			}
			time.Sleep(netnsRouterDialGap)
		}
	}()

	time.Sleep(700 * time.Millisecond)
	rebuilds := 0
	for deadline := time.Now().Add(span - 1500*time.Millisecond); time.Now().Before(deadline); {
		RoutingForceResync(cfg)
		cfg.Sets[0].Routing.Upstream.UDP = !cfg.Sets[0].Routing.Upstream.UDP
		RoutingSyncConfig(cfg)
		rebuilds += 2
	}
	device := <-deviceDone
	close(stop)
	wg.Wait()
	leaked := uplink()

	t.Logf("%s: %d rebuilds; device %q; router answered=%d lost=%d; %d packet(s) left by the uplink",
		engine, rebuilds, device.raw, routerAnswered, routerLost, leaked)
	if device.answered == 0 || device.held == 0 || routerAnswered == 0 {
		t.Fatalf("one of the three kinds of traffic never reached the listener, so the test measured nothing: device %q, router answered=%d", device.raw, routerAnswered)
	}
	if leaked != 0 || device.lost != 0 || device.broken != 0 || routerLost != 0 {
		netnsLogState(t, engine)
		t.Fatalf("while the set was rebuilt %d time(s): %d packet(s) for the address it proxies left by the ordinary uplink, %d of %d new connections from a device and %d of %d from the router never reached its listener, and the connection a device kept open broke %d time(s) after %d exchanges. A rebuild must never take out of the kernel what the set stands on before its replacement is in: without the rule that delivers the set's mark to b4 a device's connection is dropped and the router's own goes out unproxied, and without the jump that marks the router's own traffic it goes out unproxied too (rules for the mark now: %v)",
			rebuilds, leaked, device.lost, device.answered+device.lost, routerLost, routerAnswered+routerLost,
			device.broken, device.held, netnsProxyRuleLines(t, st.mark, st.table))
	}
}

func TestNetnsARebuiltProxySetKeepsTakingItsConnections(t *testing.T) {
	netnsARebuiltProxySetKeepsTakingItsConnections(t, backendIPTables)
}

func TestNetnsARebuiltProxySetKeepsTakingItsConnectionsNft(t *testing.T) {
	netnsARebuiltProxySetKeepsTakingItsConnections(t, backendNFTables)
}

func TestNetnsARebuiltProxySetKeepsTakingItsConnectionsLegacy(t *testing.T) {
	netnsARebuiltProxySetKeepsTakingItsConnections(t, backendIPTablesLegacy)
}

func netnsAProxyRuleTakenAwayIsPutBack(t *testing.T, engine string) {
	dev, cfg, st := netnsProxySetUp(t, engine)
	uplink := netnsCountUplinkPackets(t, engine)
	addr := net.JoinHostPort(netnsDevInet, "443")

	if lines := netnsProxyRuleLines(t, st.mark, st.table); len(lines) != 1 {
		t.Fatalf("the set holds %d local-delivery rule(s) before the test starts, want one: %v", len(lines), lines)
	}
	netnsRun(t, "ip", "rule", "del", "fwmark", routeSetMarkRule(st.mark), "lookup", strconv.Itoa(st.table))
	routeDelSourceCheckRules(st.mark)

	if got := dev.probe(t, "tcp", netnsDevInet, 443, netnsDialLoopMsg); got == "reply="+netnsDialLoopReply {
		t.Fatal("a device's connection reached the listener without the rule, so the test proves nothing about it")
	}
	if netnsDialLoopOnce(addr) {
		t.Fatal("the router's own connection reached the listener without the rule, so the test proves nothing about it")
	}
	if uplink() == 0 {
		t.Fatal("nothing left by the uplink without the rule, so the test does not see the leak it is about")
	}
	if !RoutingRulesPresent(cfg) {
		t.Fatal("the firewall check reads the set as broken, so a full resync would hide what the rule check does")
	}

	RoutingReconcilePolicyRules(cfg)

	if lines := netnsProxyRuleLines(t, st.mark, st.table); len(lines) != 1 {
		netnsLogState(t, engine)
		t.Fatalf("the rule that delivers the set's mark to b4 was taken out of the kernel and a monitor pass left %d in place. The set's chains are all there, so b4 reads it as healthy while nothing it marks reaches its listener, until the set is edited or b4 restarts: %v", len(lines), lines)
	}
	if current, earlier := netnsSourceCheckRuleCounts(t, st.mark); current != 1 || earlier != 0 {
		t.Errorf("whatever took the set's rule took its source-check rule too, and the monitor pass left %d current and %d earlier in place, want 1 and 0; with net.ipv4.conf.all.src_valid_mark=1 the set then drops every device's connection as a martian source", current, earlier)
	}
	if got := dev.probe(t, "tcp", netnsDevInet, 443, netnsDialLoopMsg); got != "reply="+netnsDialLoopReply {
		netnsLogState(t, engine)
		t.Errorf("the rule is back but the set still does not take a device's connection: %s", got)
	}
	if !netnsDialLoopOnce(addr) {
		netnsLogState(t, engine)
		t.Errorf("the rule is back but the set still does not take the router's own connection")
	}

	netnsRun(t, "ip", "rule", "del", "fwmark", routeSetMarkRule(st.mark), "lookup", strconv.Itoa(st.table))
	routeDelSourceCheckRules(st.mark)
	RoutingReconcilePolicyRules(cfg)
	lines := netnsProxyRuleLines(t, st.mark, st.table)
	if current, _ := netnsSourceCheckRuleCounts(t, st.mark); len(lines) != 1 || current != 1 {
		t.Errorf("both rules were taken out a second time before a monitor pass had seen them back, and the next pass left %d of the set's rule and %d of its source-check rule, want one each: %v", len(lines), current, lines)
	}

	routeDelSourceCheckRules(st.mark)
	RoutingReconcilePolicyRules(cfg)
	if current, _ := netnsSourceCheckRuleCounts(t, st.mark); current != 1 {
		t.Errorf("only the source-check rule was taken out and a monitor pass left %d in place, want one", current)
	}

	RoutingReconcilePolicyRules(cfg)
	if lines := netnsProxyRuleLines(t, st.mark, st.table); len(lines) != 1 {
		t.Errorf("a further monitor pass left %d rules for the mark, want one; a rule that is already there must not be added again: %v", len(lines), lines)
	}
	if current, earlier := netnsSourceCheckRuleCounts(t, st.mark); current != 1 || earlier != 0 {
		t.Errorf("a further monitor pass left %d current and %d earlier source-check rules, want 1 and 0", current, earlier)
	}
}

func TestNetnsAProxyRuleTakenAwayIsPutBack(t *testing.T) {
	netnsAProxyRuleTakenAwayIsPutBack(t, backendIPTables)
}

func TestNetnsAProxyRuleTakenAwayIsPutBackNft(t *testing.T) {
	netnsAProxyRuleTakenAwayIsPutBack(t, backendNFTables)
}

func TestNetnsAProxyRuleTakenAwayIsPutBackLegacy(t *testing.T) {
	netnsAProxyRuleTakenAwayIsPutBack(t, backendIPTablesLegacy)
}

func netnsARebuiltProxySetDoesNotStackItsRules(t *testing.T, engine string) {
	_, cfg, st := netnsProxySetUp(t, engine)

	for i := 0; i < 3; i++ {
		RoutingForceResync(cfg)
		cfg.Sets[0].Routing.Upstream.UDP = !cfg.Sets[0].Routing.Upstream.UDP
		RoutingSyncConfig(cfg)
		RoutingReconcilePolicyRules(cfg)
	}

	if lines := netnsProxyRuleLines(t, st.mark, st.table); len(lines) != 1 {
		t.Errorf("six rebuilds and three monitor passes left %d rules sending the set's mark to its table, want one. An old kernel takes an identical rule a second time instead of refusing it (3.16 does, 4.9 refuses it), so a rule added without looking first is stacked once more on every rebuild: %v", len(lines), lines)
	}
	if current, earlier := netnsSourceCheckRuleCounts(t, st.mark); current != 1 || earlier != 0 {
		t.Errorf("six rebuilds and three monitor passes left %d current and %d earlier source-check rules, want 1 and 0", current, earlier)
	}
}

func TestNetnsARebuiltProxySetDoesNotStackItsRules(t *testing.T) {
	netnsARebuiltProxySetDoesNotStackItsRules(t, backendIPTables)
}

func TestNetnsARebuiltProxySetDoesNotStackItsRulesNft(t *testing.T) {
	netnsARebuiltProxySetDoesNotStackItsRules(t, backendNFTables)
}

func TestNetnsARebuiltProxySetDoesNotStackItsRulesLegacy(t *testing.T) {
	netnsARebuiltProxySetDoesNotStackItsRules(t, backendIPTablesLegacy)
}
