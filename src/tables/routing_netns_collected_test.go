package tables

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

const (
	netnsCollectedNext     = "198.51.100.9"
	netnsCollectedAdjacent = "198.51.100.10"
	netnsCollectedProxied  = "reply=tproxy:loop"
	netnsCollectedHoldEnv  = "B4_NETNS_COLLECTED_HOLD"
	netnsCollectedHoldN    = 12
)

func netnsCollectedProxySetUp(t *testing.T, engine string) (*netnsDevice, *config.Config, routeState) {
	t.Helper()
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	netnsTurnOnForwarding(t)
	dev := netnsStartDevice(t)

	routeEngine = nil
	base := netnsLocalGuardConfig(engine, "", []string{netnsDevOther})
	base.Sets[0].Targets.UseCollectedIPs = true
	if err := AddRules(base); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() {
		RoutingClearAll()
		_ = ClearRules(base)
		routeEngine = nil
	})
	RoutingSyncConfig(base.WithCollectedIPs(nil))
	routeMu.Lock()
	st, ok := routeRuleCache[base.Sets[0].Id]
	routeMu.Unlock()
	if !ok {
		t.Fatal("the collected proxy set built no rules")
	}
	port, _ := portFromState(st)
	go netnsServeEcho(netnsMarkedTransparentListener(t, port, SelfDialMark))
	return dev, base, st
}

func netnsCollectedSwitched(cfg *config.Config, collect bool) *config.Config {
	next := *cfg
	set := *cfg.Sets[0]
	set.Targets.UseCollectedIPs = collect
	next.Sets = []*config.SetConfig{&set}
	return &next
}

func netnsCountUplinkPacketsTo(t *testing.T, engine, addr string) func() int {
	t.Helper()
	ipt := netnsCounterBinary(engine)
	spec := func(action string) []string {
		return []string{ipt, "-w", "-t", "mangle", action, "POSTROUTING", "-o", netnsPrimary, "-d", addr, "-p", "tcp", "-j", "ACCEPT"}
	}
	for {
		if _, err := run(spec("-D")...); err != nil {
			break
		}
	}
	netnsRun(t, spec("-I")...)
	t.Cleanup(func() { _, _ = run(spec("-D")...) })
	return func() int {
		out := netnsRun(t, ipt, "-w", "-t", "mangle", "-L", "POSTROUTING", "-v", "-x", "-n")
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 9 || f[6] != netnsPrimary || f[8] != addr {
				continue
			}
			if n, err := strconv.Atoi(f[0]); err == nil {
				return n
			}
		}
		t.Fatalf("the uplink packet counter for %s is gone:\n%s", addr, out)
		return 0
	}
}

func netnsCollectedLeavesByTheUplink(t *testing.T, engine string, dev *netnsDevice, addr, why string) {
	t.Helper()
	uplink := netnsCountUplinkPacketsTo(t, engine, addr)
	got := dev.probe(t, "tcp", addr, 443, "loop")
	if n := uplink(); n == 0 || got == netnsCollectedProxied {
		netnsLogState(t, engine)
		t.Errorf("%s: a new connection to %s got %q and sent %d packet(s) by the uplink; an address outside the set must leave by the uplink", why, addr, got, n)
	}
}

func netnsCollectedIsProxied(t *testing.T, dev *netnsDevice, addr, when string) {
	t.Helper()
	if got := dev.probe(t, "tcp", addr, 443, "loop"); got != netnsCollectedProxied {
		t.Fatalf("%s: a connection to the collected address %s got %q, want it proxied", when, addr, got)
	}
}

func netnsCollectedGuardLacks(t *testing.T, engine, guard, addr string) {
	t.Helper()
	if members := netnsSetMembers(t, engine, guard); strings.Contains(members, addr) {
		t.Errorf("%s still lists %s after it left the pool:\n%s", guard, addr, members)
	}
}

func netnsACollectedAddressRemovedFromThePoolLeavesByTheUplink(t *testing.T, engine string) {
	dev, base, st := netnsCollectedProxySetUp(t, engine)
	guard := st.setV4 + "c"

	t.Run("removed", func(t *testing.T) {
		RoutingSyncConfig(base.WithCollectedIPs([]string{netnsDevInet}))
		netnsCollectedIsProxied(t, dev, netnsDevInet, "before the removal")
		RoutingSyncConfig(base.WithCollectedIPs(nil))
		netnsCollectedLeavesByTheUplink(t, engine, dev, netnsDevInet, "the pool was emptied")
		netnsCollectedGuardLacks(t, engine, guard, netnsDevInet)
	})

	t.Run("rolled back", func(t *testing.T) {
		RoutingSyncConfig(base.WithCollectedIPs([]string{netnsDevInet, netnsCollectedNext}))
		netnsCollectedIsProxied(t, dev, netnsCollectedNext, "before the rollback")
		RoutingSyncConfig(base.WithCollectedIPs([]string{netnsDevInet}))
		netnsCollectedLeavesByTheUplink(t, engine, dev, netnsCollectedNext, "a publish was rolled back to the previous pool")
		netnsCollectedGuardLacks(t, engine, guard, netnsCollectedNext)
		netnsCollectedIsProxied(t, dev, netnsDevInet, "after the rollback")
	})

	t.Run("adjacent", func(t *testing.T) {
		RoutingSyncConfig(base.WithCollectedIPs([]string{netnsCollectedNext, netnsCollectedAdjacent}))
		netnsCollectedIsProxied(t, dev, netnsCollectedNext, "before the removal")
		RoutingSyncConfig(base.WithCollectedIPs([]string{netnsCollectedAdjacent}))
		netnsCollectedLeavesByTheUplink(t, engine, dev, netnsCollectedNext, "its neighbour stayed in the pool")
		netnsCollectedIsProxied(t, dev, netnsCollectedAdjacent, "after its neighbour was removed")
	})
}

func TestNetnsACollectedAddressRemovedFromThePoolLeavesByTheUplink(t *testing.T) {
	netnsACollectedAddressRemovedFromThePoolLeavesByTheUplink(t, backendIPTables)
}

func TestNetnsACollectedAddressRemovedFromThePoolLeavesByTheUplinkNft(t *testing.T) {
	netnsACollectedAddressRemovedFromThePoolLeavesByTheUplink(t, backendNFTables)
}

func netnsACollectedAddressDroppedWhileTheSwitchWasOffLeavesByTheUplink(t *testing.T, engine string) {
	dev, base, _ := netnsCollectedProxySetUp(t, engine)

	RoutingSyncConfig(base.WithCollectedIPs([]string{netnsDevInet}))
	netnsCollectedIsProxied(t, dev, netnsDevInet, "before the switch went off")
	RoutingSyncConfig(netnsCollectedSwitched(base, false))
	RoutingSyncConfig(base.WithCollectedIPs([]string{netnsCollectedNext}))
	netnsCollectedLeavesByTheUplink(t, engine, dev, netnsDevInet, "the pool lost it while collected IPs were off")
	netnsCollectedIsProxied(t, dev, netnsCollectedNext, "after the switch came back on")
}

func TestNetnsACollectedAddressDroppedWhileTheSwitchWasOffLeavesByTheUplink(t *testing.T) {
	netnsACollectedAddressDroppedWhileTheSwitchWasOffLeavesByTheUplink(t, backendIPTables)
}

func TestNetnsACollectedAddressDroppedWhileTheSwitchWasOffLeavesByTheUplinkNft(t *testing.T) {
	netnsACollectedAddressDroppedWhileTheSwitchWasOffLeavesByTheUplink(t, backendNFTables)
}

func TestNetnsDeviceHoldsAConnectionAcrossAChange(t *testing.T) {
	if os.Getenv(netnsCollectedHoldEnv) == "" {
		t.Skip("helper for the device side of the collected removal tests")
	}
	paused, resume := os.Getenv("B4_HOLD_PAUSED"), os.Getenv("B4_HOLD_RESUME")
	conn, err := net.DialTimeout("tcp", net.JoinHostPort(os.Getenv("B4_PROBE_HOST"), "443"), 3*time.Second)
	if err != nil {
		fmt.Printf("HOLD dial failed: %v\n", err)
		return
	}
	defer conn.Close()
	ok := 0
	buf := make([]byte, 256)
	for i := 0; i < netnsCollectedHoldN; i++ {
		if i == netnsCollectedHoldN/3 {
			_ = os.WriteFile(paused, []byte("1"), 0o644)
			for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(20 * time.Millisecond) {
				if _, err := os.Stat(resume); err == nil {
					break
				}
			}
		}
		_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
		msg := fmt.Sprintf("m%d", i)
		if _, err := conn.Write([]byte(msg)); err != nil {
			fmt.Printf("HOLD ok=%d of %d, write %d failed: %v\n", ok, netnsCollectedHoldN, i, err)
			return
		}
		n, err := conn.Read(buf)
		if err != nil || !strings.HasSuffix(string(buf[:n]), msg) {
			fmt.Printf("HOLD ok=%d of %d, read %d failed: %v\n", ok, netnsCollectedHoldN, i, err)
			return
		}
		ok++
		time.Sleep(30 * time.Millisecond)
	}
	fmt.Printf("HOLD ok=%d of %d\n", ok, netnsCollectedHoldN)
}

func (d *netnsDevice) holdAcross(t *testing.T, host string, change func()) string {
	t.Helper()
	dir := t.TempDir()
	paused, resume := filepath.Join(dir, "paused"), filepath.Join(dir, "resume")
	cmd := exec.Command("nsenter", "-t", strconv.Itoa(d.pid), "-n", os.Args[0], "-test.run", "^TestNetnsDeviceHoldsAConnectionAcrossAChange$", "-test.v")
	cmd.Env = append(os.Environ(), netnsCollectedHoldEnv+"=1", "B4_PROBE_HOST="+host, "B4_HOLD_PAUSED="+paused, "B4_HOLD_RESUME="+resume)
	var out strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("start the device side: %v", err)
	}
	for deadline := time.Now().Add(8 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(paused); err == nil {
			break
		}
	}
	change()
	_ = os.WriteFile(resume, []byte("1"), 0o644)
	_ = cmd.Wait()
	for _, line := range strings.Split(out.String(), "\n") {
		if strings.HasPrefix(line, "HOLD ") {
			return strings.TrimPrefix(line, "HOLD ")
		}
	}
	return "no device output: " + strings.TrimSpace(out.String())
}

func netnsTurnOffEarlyDemux(t *testing.T) {
	t.Helper()
	if _, err := os.Stat("/proc/sys/net/ipv4/ip_early_demux"); err != nil {
		t.Skipf("this kernel has no early demux switch inside a network namespace (%v), so a run that needs early demux off cannot be set up here", err)
	}
	gwKeepSysctl(t, "net/ipv4/ip_early_demux", "0")
	if _, err := os.Stat("/proc/sys/net/ipv4/tcp_early_demux"); err == nil {
		gwKeepSysctl(t, "net/ipv4/tcp_early_demux", "0")
	}
}

func netnsAHeldCollectedConnectionSurvivesItsRemoval(t *testing.T, engine string) {
	dev, base, _ := netnsCollectedProxySetUp(t, engine)
	netnsTurnOffEarlyDemux(t)

	RoutingSyncConfig(base.WithCollectedIPs([]string{netnsDevInet}))
	got := dev.holdAcross(t, netnsDevInet, func() { RoutingSyncConfig(base.WithCollectedIPs(nil)) })
	if want := fmt.Sprintf("ok=%d of %d", netnsCollectedHoldN, netnsCollectedHoldN); got != want {
		netnsLogState(t, engine)
		t.Errorf("a connection proxied to %s broke when the address left the pool: %q, want %q; a connection the proxy already holds keeps its connection mark and must stay with the proxy", netnsDevInet, got, want)
	}
}

func TestNetnsAHeldCollectedConnectionSurvivesItsRemoval(t *testing.T) {
	netnsAHeldCollectedConnectionSurvivesItsRemoval(t, backendIPTables)
}

func TestNetnsAHeldCollectedConnectionSurvivesItsRemovalNft(t *testing.T) {
	netnsAHeldCollectedConnectionSurvivesItsRemoval(t, backendNFTables)
}

func netnsCollectedSwitchedMode(cfg *config.Config, mode string) *config.Config {
	next := *cfg
	set := *cfg.Sets[0]
	set.Routing.Mode = mode
	if mode == config.RoutingModeInterface {
		set.Routing.EgressInterface = netnsSecondary
	}
	next.Sets = []*config.SetConfig{&set}
	return &next
}

func netnsACollectedConsumerSurvivesARoutingModeChange(t *testing.T, engine string) {
	for _, mode := range []string{config.RoutingModeBlock, config.RoutingModeInterface} {
		t.Run(mode, func(t *testing.T) {
			_, base, _ := netnsCollectedProxySetUp(t, engine)
			RoutingSyncConfig(base.WithCollectedIPs([]string{netnsDevInet}))
			if errs := RoutingStatus().SetErrors; len(errs) > 0 {
				t.Fatalf("the collected proxy consumer did not install: %v", errs)
			}
			moved := netnsCollectedSwitchedMode(base, mode).WithCollectedIPs([]string{netnsDevInet})
			for _, pass := range []string{"the change", "the retry"} {
				RoutingSyncConfig(moved)
				if errs := RoutingStatus().SetErrors; len(errs) > 0 {
					t.Errorf("%s from proxy to %s mode left the collected consumer uninstalled: %v", pass, mode, errs)
				}
				if !RoutingRulesPresent(moved) {
					t.Errorf("after %s from proxy to %s mode the monitor finds the consumer's rules missing", pass, mode)
				}
			}
		})
	}
}

func TestNetnsACollectedConsumerSurvivesARoutingModeChange(t *testing.T) {
	netnsACollectedConsumerSurvivesARoutingModeChange(t, backendIPTables)
}

func TestNetnsACollectedConsumerSurvivesARoutingModeChangeNft(t *testing.T) {
	netnsACollectedConsumerSurvivesARoutingModeChange(t, backendNFTables)
}

func netnsADNSAnswerInstallsACollectedConsumer(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	netnsTurnOnForwarding(t)
	forgetInstallFailures(t)
	dev := netnsStartDevice(t)

	routeEngine = nil
	base := netnsLocalGuardConfig(engine, "", []string{netnsDevOther})
	base.Sets[0].Targets.UseCollectedIPs = true
	if err := AddRules(base); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() {
		RoutingClearAll()
		_ = ClearRules(base)
		routeEngine = nil
	})
	view := base.WithCollectedIPs([]string{netnsDevInet})
	RoutingHandleDNS(view, view.Sets[0], []net.IP{net.ParseIP(netnsCollectedNext)})
	routeMu.Lock()
	st, ok := routeRuleCache[view.Sets[0].Id]
	routeMu.Unlock()
	if !ok {
		netnsLogState(t, engine)
		t.Fatal("a DNS answer for a collected consumer that no sync had installed yet did not install it, so the set routes nothing until the next sync")
	}
	if !RoutingRulesPresent(view) {
		t.Error("after the DNS install the monitor finds the consumer's rules missing")
	}
	port, _ := portFromState(st)
	go netnsServeEcho(netnsMarkedTransparentListener(t, port, SelfDialMark))
	netnsCollectedIsProxied(t, dev, netnsDevInet, "after a DNS answer installed the consumer")
}

func TestNetnsADNSAnswerInstallsACollectedConsumer(t *testing.T) {
	netnsADNSAnswerInstallsACollectedConsumer(t, backendIPTables)
}

func TestNetnsADNSAnswerInstallsACollectedConsumerNft(t *testing.T) {
	netnsADNSAnswerInstallsACollectedConsumer(t, backendNFTables)
}

func netnsASourceScopedCollectedConsumerIsNotRestoredEveryTick(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	netnsTurnOnForwarding(t)
	dev := netnsStartDevice(t)

	routeEngine = nil
	base := netnsLocalGuardConfig(engine, dev.mac, []string{netnsDevOther})
	base.Sets[0].Targets.UseCollectedIPs = true
	if err := AddRules(base); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() {
		RoutingClearAll()
		_ = ClearRules(base)
		routeEngine = nil
	})
	cfg := base.WithCollectedIPs([]string{netnsDevInet})
	RoutingSyncConfig(cfg)
	routeMu.Lock()
	st, ok := routeRuleCache[base.Sets[0].Id]
	routeMu.Unlock()
	if !ok || !st.srcScoped {
		t.Fatalf("the device-scoped collected proxy set did not install as a source-scoped set: installed=%v", ok)
	}
	port, _ := portFromState(st)
	go netnsServeEcho(netnsMarkedTransparentListener(t, port, SelfDialMark))

	if !RoutingRulesPresent(cfg) {
		t.Errorf("right after the sync the monitor finds the rules of a source-scoped collected consumer missing")
	}
	RoutingSyncConfig(cfg)
	if !RoutingRulesPresent(cfg) {
		t.Errorf("after a second sync the monitor finds the rules of a source-scoped collected consumer missing")
	}
	m, ptr := newLockTestMonitor(t)
	ptr.Store(cfg)
	m.snapshotRoutingIfaces(cfg)
	var resyncs []bool
	for i := 0; i < 3; i++ {
		m.lost = false
		m.reconcileRouting(false)
		resyncs = append(resyncs, m.lost)
	}
	for _, lost := range resyncs {
		if lost {
			netnsLogState(t, engine)
			t.Errorf("the monitor rebuilt the routing rules on these ticks %v, want none: it demands a guard in %s that the emitter never installs for a source-scoped set", resyncs, st.chainOut)
			break
		}
	}
	netnsCollectedIsProxied(t, dev, netnsDevInet, "after the monitor ticks")
}

func TestNetnsASourceScopedCollectedConsumerIsNotRestoredEveryTick(t *testing.T) {
	netnsASourceScopedCollectedConsumerIsNotRestoredEveryTick(t, backendIPTables)
}

func TestNetnsASourceScopedCollectedConsumerIsNotRestoredEveryTickNft(t *testing.T) {
	netnsASourceScopedCollectedConsumerIsNotRestoredEveryTick(t, backendNFTables)
}

func netnsMarkedTransparentListener6(t *testing.T, port int, mark uint32) net.Listener {
	t.Helper()
	lc := net.ListenConfig{
		Control: func(network, address string, c syscall.RawConn) error {
			var ctlErr error
			if err := c.Control(func(fd uintptr) {
				for _, opt := range []struct{ level, name, value int }{
					{unix.SOL_IPV6, unix.IPV6_TRANSPARENT, 1},
					{unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 1},
					{unix.SOL_SOCKET, unix.SO_MARK, int(mark)},
				} {
					if ctlErr = unix.SetsockoptInt(int(fd), opt.level, opt.name, opt.value); ctlErr != nil {
						return
					}
				}
			}); err != nil {
				return err
			}
			return ctlErr
		},
	}
	ln, err := lc.Listen(context.Background(), "tcp6", fmt.Sprintf("[::]:%d", port))
	if err != nil {
		t.Fatalf("transparent listen on [::]:%d: %v", port, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	return ln
}

func netnsEchoOnceAndHold(ln net.Listener, held chan<- net.Conn) {
	for {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			buf := make([]byte, 256)
			_ = c.SetDeadline(time.Now().Add(5 * time.Second))
			n, err := c.Read(buf)
			if err == nil {
				_, err = c.Write([]byte("tproxy:" + string(buf[:n])))
			}
			if err != nil {
				_ = c.Close()
				return
			}
			held <- c
		}(conn)
	}
}

func netnsCloseWithoutAPacket(t *testing.T, conn net.Conn) {
	t.Helper()
	raw, err := conn.(*net.TCPConn).SyscallConn()
	if err != nil {
		t.Fatalf("the proxy socket: %v", err)
	}
	var optErr error
	if err := raw.Control(func(fd uintptr) { optErr = unix.SetsockoptInt(int(fd), unix.SOL_TCP, unix.TCP_REPAIR, 1) }); err != nil || optErr != nil {
		t.Fatalf("cannot put the proxy socket in repair mode, which closes it without sending a packet: %v %v", err, optErr)
	}
	_ = conn.Close()
}

func netnsAHeldCollectedConnectionStillGetsTheKernelsReset(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	client, inet, _ := quicNsRig(t)
	udpPortsNsEnableIPv6(t, client, inet)

	routeEngine = nil
	base := netnsLocalGuardConfig(engine, "", []string{netnsDevOther})
	base.Queue.IPv6Enabled = true
	base.Sets[0].Targets.UseCollectedIPs = true
	if err := AddRules(base); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() {
		RoutingClearAll()
		_ = ClearRules(base)
		routeEngine = nil
	})
	RoutingSyncConfig(base.WithCollectedIPs([]string{udpPortsNsInet6}))
	routeMu.Lock()
	st, ok := routeRuleCache[base.Sets[0].Id]
	routeMu.Unlock()
	if !ok {
		t.Fatal("the collected proxy set built no rules")
	}
	port, _ := portFromState(st)
	held := make(chan net.Conn, 1)
	go netnsEchoOnceAndHold(netnsMarkedTransparentListener6(t, port, SelfDialMark), held)

	var conn net.Conn
	if err := client.inside(func() (err error) {
		conn, err = net.DialTimeout("tcp6", net.JoinHostPort(udpPortsNsInet6, "443"), 3*time.Second)
		return err
	}); err != nil {
		t.Fatalf("dial the collected address %s: %v", udpPortsNsInet6, err)
	}
	defer conn.Close()
	buf := make([]byte, 256)
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	_, err := conn.Write([]byte("m0"))
	n := 0
	if err == nil {
		n, err = conn.Read(buf)
	}
	if err != nil || string(buf[:n]) != "tproxy:m0" {
		t.Fatalf("a connection to the collected address %s got %q (%v), want it proxied", udpPortsNsInet6, buf[:n], err)
	}
	select {
	case proxied := <-held:
		netnsCloseWithoutAPacket(t, proxied)
	case <-time.After(3 * time.Second):
		t.Fatal("the proxy never took the connection")
	}
	RoutingSyncConfig(base.WithCollectedIPs(nil))

	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err = conn.Write([]byte("m1")); err == nil {
		_, err = conn.Read(buf)
	}
	if !errors.Is(err, syscall.ECONNRESET) {
		netnsLogState(t, engine)
		if engine == backendNFTables {
			out, _ := run("nft", "list", "chain", "inet", routeNftTable, st.chainOut)
			t.Logf("%s:\n%s", st.chainOut, out)
		} else {
			out, _ := run(backendIP6Tables, "-w", "-t", "mangle", "-L", st.chainOut, "-v", "-x", "-n")
			t.Logf("%s:\n%s", st.chainOut, out)
		}
		t.Errorf("a device that writes into a connection whose proxy socket is gone and whose address left the pool got %v, want the kernel's reset; the reset travels in the reply direction and must reach the device, not be marked for the proxy and looped back into the router", err)
	}
}

func TestNetnsAHeldCollectedConnectionStillGetsTheKernelsReset(t *testing.T) {
	netnsAHeldCollectedConnectionStillGetsTheKernelsReset(t, backendIPTables)
}

func TestNetnsAHeldCollectedConnectionStillGetsTheKernelsResetNft(t *testing.T) {
	netnsAHeldCollectedConnectionStillGetsTheKernelsReset(t, backendNFTables)
}

const (
	netnsForeignCTMark    = "0x10c3d"
	netnsCTRewriteNftable = "b4t_ctrewrite"
)

func netnsRewriteConnmark(t *testing.T, engine, hook, state string) func() int {
	t.Helper()
	if engine == backendNFTables {
		prio := "0"
		if hook == "prerouting" {
			prio = "-190"
		}
		_, _ = run("nft", "delete", "table", "inet", netnsCTRewriteNftable)
		netnsRun(t, "nft", "add", "table", "inet", netnsCTRewriteNftable)
		t.Cleanup(func() { _, _ = run("nft", "delete", "table", "inet", netnsCTRewriteNftable) })
		netnsRun(t, "nft", "add", "chain", "inet", netnsCTRewriteNftable, "rewrite", "{", "type", "filter", "hook", hook, "priority", prio, ";", "policy", "accept", ";", "}")
		netnsRun(t, "nft", "add", "rule", "inet", netnsCTRewriteNftable, "rewrite", "ip", "daddr", netnsDevInet, "tcp", "dport", "443", "ct", "state", state, "counter", "ct", "mark", "set", netnsForeignCTMark)
		return func() int {
			out, _ := run("nft", "list", "table", "inet", netnsCTRewriteNftable)
			return gwNftPackets(out)
		}
	}
	chain := strings.ToUpper(hook)
	spec := func(action string, at ...string) []string {
		args := append([]string{engine, "-w", "-t", "mangle", action, chain}, at...)
		return append(args, "-d", netnsDevInet, "-p", "tcp", "--dport", "443", "-m", "conntrack", "--ctstate", strings.ToUpper(state), "-j", "CONNMARK", "--set-xmark", netnsForeignCTMark+"/0xffffffff")
	}
	netnsRun(t, spec("-I", "1")...)
	t.Cleanup(func() { _, _ = run(spec("-D")...) })
	return func() int {
		out, _ := run(engine, "-w", "-t", "mangle", "-L", chain, "-v", "-x", "-n")
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 9 || f[2] != "CONNMARK" || f[8] != netnsDevInet {
				continue
			}
			if n, err := strconv.Atoi(f[0]); err == nil {
				return n
			}
		}
		return 0
	}
}

func netnsAProxiedCollectedConnectionSurvivesAConnectionMarkRewrite(t *testing.T, engine string) {
	for _, c := range []struct {
		name, hook, state string
		late              bool
	}{
		{"at the SYN", "input", "new", false},
		{"late without early demux", "prerouting", "established", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			dev, base, st := netnsCollectedProxySetUp(t, engine)
			RoutingSyncConfig(base.WithCollectedIPs([]string{netnsDevInet}))
			var rewritten func() int
			change := func() {}
			if c.late {
				netnsTurnOffEarlyDemux(t)
				change = func() { rewritten = netnsRewriteConnmark(t, engine, c.hook, c.state) }
			} else {
				rewritten = netnsRewriteConnmark(t, engine, c.hook, c.state)
			}
			got := dev.holdAcross(t, netnsDevInet, change)
			if rewritten == nil || rewritten() == 0 {
				t.Fatalf("the connection-mark rewrite never matched a packet, so this run proves nothing; the device saw %q", got)
			}
			if want := fmt.Sprintf("ok=%d of %d", netnsCollectedHoldN, netnsCollectedHoldN); got != want {
				netnsLogState(t, engine)
				if engine == backendNFTables {
					out, _ := run("nft", "list", "chain", "inet", routeNftTable, st.chainPre)
					t.Logf("%s:\n%s", st.chainPre, out)
				} else {
					out, _ := run(engine, "-w", "-t", "mangle", "-L", st.chainPre, "-v", "-x", "-n")
					t.Logf("%s:\n%s", st.chainPre, out)
				}
				t.Errorf("a connection the transparent listener holds for the collected address %s broke once something else rewrote its connection mark (%s): %q, want %q; the divert must take the listener's own connections before the collected guard returns a connection whose mark is not the set's", netnsDevInet, c.name, got, want)
			}
		})
	}
}

func TestNetnsAProxiedCollectedConnectionSurvivesAConnectionMarkRewrite(t *testing.T) {
	netnsAProxiedCollectedConnectionSurvivesAConnectionMarkRewrite(t, backendIPTables)
}

func TestNetnsAProxiedCollectedConnectionSurvivesAConnectionMarkRewriteNft(t *testing.T) {
	netnsAProxiedCollectedConnectionSurvivesAConnectionMarkRewrite(t, backendNFTables)
}

const (
	netnsCollectedIfaceLink   = "b4ci0"
	netnsCollectedIfacePeer   = "b4ci0p"
	netnsCollectedIfaceRouter = "10.201.47.1"
	netnsCollectedIfaceServer = "10.201.47.2"
)

func netnsKeepSysctlIfPresent(t *testing.T, key, value string) {
	t.Helper()
	if _, err := os.Stat("/proc/sys/" + key); err != nil {
		return
	}
	gwKeepSysctl(t, key, value)
}

func netnsListenInside(t *testing.T, pid int, addr string) net.Listener {
	t.Helper()
	type result struct {
		ln  net.Listener
		err error
	}
	done := make(chan result, 1)
	go func() {
		runtime.LockOSThread()
		home, err := os.Open(netnsThreadNetNS())
		if err != nil {
			done <- result{err: err}
			return
		}
		defer home.Close()
		target, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
		if err != nil {
			done <- result{err: err}
			return
		}
		defer target.Close()
		if err := unix.Setns(int(target.Fd()), unix.CLONE_NEWNET); err != nil {
			done <- result{err: err}
			return
		}
		ln, err := net.Listen("tcp4", addr)
		if unix.Setns(int(home.Fd()), unix.CLONE_NEWNET) == nil {
			runtime.UnlockOSThread()
		}
		done <- result{ln: ln, err: err}
	}()
	r := <-done
	if r.err != nil {
		t.Fatalf("listen on %s inside the server namespace: %v", addr, r.err)
	}
	t.Cleanup(func() { _ = r.ln.Close() })
	return r.ln
}

func netnsStartCollectedInterfaceServer(t *testing.T) {
	t.Helper()
	_, _ = run("ip", "link", "del", netnsCollectedIfaceLink)
	netnsRun(t, "ip", "link", "add", netnsCollectedIfaceLink, "type", "veth", "peer", "name", netnsCollectedIfacePeer)
	t.Cleanup(func() { _, _ = run("ip", "link", "del", netnsCollectedIfaceLink) })
	netnsRun(t, "ip", "link", "set", netnsCollectedIfaceLink, "up")
	netnsRun(t, "ip", "addr", "add", netnsCollectedIfaceRouter+"/24", "dev", netnsCollectedIfaceLink)

	srv := exec.Command("unshare", "-n", "sleep", "600")
	if err := srv.Start(); err != nil {
		t.Skipf("cannot open a server network namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = srv.Process.Kill()
		_, _ = srv.Process.Wait()
	})
	pid := srv.Process.Pid
	self, _ := os.Readlink(netnsThreadNetNS())
	for deadline := time.Now().Add(3 * time.Second); ; time.Sleep(20 * time.Millisecond) {
		other, err := os.Readlink(fmt.Sprintf("/proc/%d/ns/net", pid))
		if err == nil && other != self {
			break
		}
		if time.Now().After(deadline) {
			t.Skip("the server never entered its own network namespace")
		}
	}
	netnsRun(t, "ip", "link", "set", netnsCollectedIfacePeer, "netns", strconv.Itoa(pid))
	in := func(args ...string) {
		netnsRun(t, append([]string{"nsenter", "-t", strconv.Itoa(pid), "-n"}, args...)...)
	}
	in("ip", "link", "set", "lo", "up")
	in("ip", "link", "set", netnsCollectedIfacePeer, "up")
	in("ip", "addr", "add", netnsCollectedIfaceServer+"/24", "dev", netnsCollectedIfacePeer)
	in("ip", "addr", "add", netnsDevInet+"/32", "dev", "lo")
	in("ip", "route", "add", "default", "via", netnsCollectedIfaceRouter)
	go netnsServeEcho(netnsListenInside(t, pid, net.JoinHostPort(netnsDevInet, "443")))
}

func netnsDropAtTheUplinksFarEnd(t *testing.T, engine string) func() int {
	t.Helper()
	ipt := netnsCounterBinary(engine)
	spec := func(action string) []string {
		return []string{ipt, "-w", "-t", "raw", action, "PREROUTING", "-i", netnsPrimary + "p", "-d", netnsDevInet, "-j", "DROP"}
	}
	netnsRun(t, spec("-I")...)
	t.Cleanup(func() { _, _ = run(spec("-D")...) })
	return func() int {
		out := netnsRun(t, ipt, "-w", "-t", "raw", "-L", "PREROUTING", "-v", "-x", "-n")
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 9 || f[2] != "DROP" || f[5] != netnsPrimary+"p" || f[8] != netnsDevInet {
				continue
			}
			if n, err := strconv.Atoi(f[0]); err == nil {
				return n
			}
		}
		t.Fatalf("the drop rule at the uplink's far end is gone:\n%s", out)
		return 0
	}
}

func netnsAnInterfaceConsumerKeepsACollectedConnectionOnItsInterface(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	netnsTurnOnForwarding(t)
	dev := netnsStartDevice(t)
	netnsStartCollectedInterfaceServer(t)
	for _, iface := range []string{"all", "default", netnsCollectedIfaceLink} {
		gwKeepSysctl(t, "net/ipv4/conf/"+iface+"/rp_filter", "0")
	}
	netnsKeepSysctlIfPresent(t, "net/netfilter/nf_conntrack_tcp_loose", "0")
	uplink := netnsDropAtTheUplinksFarEnd(t, engine)

	routeCTMarkHeld.Store(false)
	t.Cleanup(func() { routeCTMarkHeld.Store(true) })
	routeEngine = nil
	cfg := netnsConfig(engine)
	set := cfg.Sets[0]
	set.Routing.EgressInterface = netnsCollectedIfaceLink
	set.Targets.IPs = []string{netnsDevOther}
	set.Targets.IpsToMatch = []string{netnsDevOther}
	set.Targets.UseCollectedIPs = true
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() {
		RoutingClearAll()
		_ = ClearRules(cfg)
		routeEngine = nil
	})
	RoutingSyncConfig(cfg.WithCollectedIPs([]string{netnsDevInet}))
	if errs := RoutingStatus().SetErrors; len(errs) > 0 {
		t.Fatalf("the collected interface consumer did not install: %v", errs)
	}
	routeMu.Lock()
	st, ok := routeRuleCache[set.Id]
	routeMu.Unlock()
	if !ok {
		t.Fatal("the collected interface consumer built no rules")
	}
	if routeCTMarkIsHeld() {
		t.Fatal("the sync took the connection mark back as held, so this run would not test the per-packet fallback")
	}

	var rewritten func() int
	got := dev.holdAcross(t, netnsDevInet, func() { rewritten = netnsRewriteConnmark(t, engine, "prerouting", "established") })
	if rewritten == nil || rewritten() == 0 {
		t.Fatalf("the connection-mark rewrite never matched a packet, so this run proves nothing; the device saw %q", got)
	}
	leaked := uplink()
	if want := fmt.Sprintf("ok=%d of %d", netnsCollectedHoldN, netnsCollectedHoldN); got != want || leaked != 0 {
		netnsLogState(t, engine)
		if engine == backendNFTables {
			out, _ := run("nft", "list", "chain", "inet", routeNftTable, st.chainPre)
			t.Logf("%s:\n%s", st.chainPre, out)
		} else {
			out, _ := run(engine, "-w", "-t", "mangle", "-L", st.chainPre, "-v", "-x", "-n")
			t.Logf("%s:\n%s", st.chainPre, out)
		}
		t.Errorf("on a router that overwrites the connection mark, a connection of an interface set to its collected address %s got %q (want %q) and sent %d packet(s) by the ordinary uplink (want 0); the set marks every packet there, and a guard that returns established connections above that fallback sends them out the wrong interface", netnsDevInet, got, want, leaked)
	}
}

func TestNetnsAnInterfaceConsumerKeepsACollectedConnectionOnItsInterface(t *testing.T) {
	netnsAnInterfaceConsumerKeepsACollectedConnectionOnItsInterface(t, backendIPTables)
}

func TestNetnsAnInterfaceConsumerKeepsACollectedConnectionOnItsInterfaceNft(t *testing.T) {
	netnsAnInterfaceConsumerKeepsACollectedConnectionOnItsInterface(t, backendNFTables)
}

const (
	netnsCollectedDatagramEnv = "B4_NETNS_COLLECTED_DGRAM"
	netnsCollectedUDPPort     = 5353
	netnsQUICCountNftable     = "b4t_quiccount"
)

func TestNetnsDeviceSendsDatagrams(t *testing.T) {
	if os.Getenv(netnsCollectedDatagramEnv) == "" {
		t.Skip("helper for the device side of the collected UDP tests")
	}
	port, _ := strconv.Atoi(os.Getenv("B4_PROBE_PORT"))
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(os.Getenv("B4_PROBE_HOST")), Port: port})
	if err != nil {
		fmt.Printf("DGRAM dial failed: %v\n", err)
		return
	}
	defer conn.Close()
	var res []string
	buf := make([]byte, 256)
	for _, msg := range []string{"one", "two", "three"} {
		if _, err := conn.Write([]byte(msg)); err != nil {
			res = append(res, msg+":senderr")
			continue
		}
		_ = conn.SetReadDeadline(time.Now().Add(time.Second))
		n, err := conn.Read(buf)
		switch {
		case errors.Is(err, syscall.ECONNREFUSED):
			res = append(res, msg+":refused")
		case err != nil:
			res = append(res, msg+":noreply")
		default:
			res = append(res, msg+":"+string(buf[:n]))
		}
	}
	fmt.Printf("DGRAM %s\n", strings.Join(res, " "))
}

func (d *netnsDevice) datagrams(t *testing.T, host string, port int) string {
	t.Helper()
	cmd := exec.Command("nsenter", "-t", strconv.Itoa(d.pid), "-n", os.Args[0], "-test.run", "^TestNetnsDeviceSendsDatagrams$", "-test.v")
	cmd.Env = append(os.Environ(), netnsCollectedDatagramEnv+"=1", "B4_PROBE_HOST="+host, "B4_PROBE_PORT="+strconv.Itoa(port))
	out, _ := cmd.CombinedOutput()
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "DGRAM ") {
			return strings.TrimPrefix(line, "DGRAM ")
		}
	}
	return "no device output: " + strings.TrimSpace(string(out))
}

func netnsServeUDPRelay(t *testing.T, port int) func() []string {
	t.Helper()
	uc := netnsTransparentUDP(t, port)
	var mu sync.Mutex
	var got []string
	note := func(s string) {
		mu.Lock()
		got = append(got, s)
		mu.Unlock()
	}
	go func() {
		buf := make([]byte, 2048)
		oob := make([]byte, 512)
		for {
			n, oobn, _, src, err := uc.ReadMsgUDP(buf, oob)
			if err != nil {
				return
			}
			dst, err := parseNetnsOrigDst(oob[:oobn])
			if err != nil {
				note(string(buf[:n]) + "@?")
				continue
			}
			note(string(buf[:n]) + "@" + dst)
			lc := net.ListenConfig{Control: func(network, address string, c syscall.RawConn) error {
				var ctlErr error
				if err := c.Control(func(fd uintptr) {
					for _, opt := range []struct{ level, name, value int }{
						{unix.SOL_SOCKET, unix.SO_REUSEADDR, 1},
						{unix.SOL_SOCKET, unix.SO_MARK, int(SelfDialMark)},
						{unix.SOL_IP, unix.IP_TRANSPARENT, 1},
					} {
						if ctlErr = unix.SetsockoptInt(int(fd), opt.level, opt.name, opt.value); ctlErr != nil {
							return
						}
					}
				}); err != nil {
					return err
				}
				return ctlErr
			}}
			reply, err := lc.ListenPacket(context.Background(), "udp4", dst)
			if err != nil {
				note("reply socket: " + err.Error())
				continue
			}
			_, _ = reply.WriteTo([]byte("relay:"+string(buf[:n])), src)
			_ = reply.Close()
		}
	}()
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), got...)
	}
}

func netnsCountUplinkUDPTo(t *testing.T, engine, addr string) func() int {
	t.Helper()
	ipt := netnsCounterBinary(engine)
	spec := func(action string) []string {
		return []string{ipt, "-w", "-t", "mangle", action, "POSTROUTING", "-o", netnsPrimary, "-d", addr, "-p", "udp", "-j", "ACCEPT"}
	}
	netnsRun(t, spec("-I")...)
	t.Cleanup(func() { _, _ = run(spec("-D")...) })
	return func() int {
		out := netnsRun(t, ipt, "-w", "-t", "mangle", "-L", "POSTROUTING", "-v", "-x", "-n")
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 9 || (f[3] != "udp" && f[3] != "17") || f[6] != netnsPrimary || f[8] != addr {
				continue
			}
			if n, err := strconv.Atoi(f[0]); err == nil {
				return n
			}
		}
		t.Fatalf("the uplink UDP counter for %s is gone:\n%s", addr, out)
		return 0
	}
}

func netnsCountQUICTo(t *testing.T, engine, hook string) func() int {
	t.Helper()
	if engine == backendNFTables {
		_, _ = run("nft", "delete", "chain", "inet", netnsQUICCountNftable, hook)
		_, _ = run("nft", "add", "table", "inet", netnsQUICCountNftable)
		t.Cleanup(func() { _, _ = run("nft", "delete", "table", "inet", netnsQUICCountNftable) })
		netnsRun(t, "nft", "add", "chain", "inet", netnsQUICCountNftable, hook, "{", "type", "filter", "hook", hook, "priority", "-200", ";", "policy", "accept", ";", "}")
		netnsRun(t, "nft", "add", "rule", "inet", netnsQUICCountNftable, hook, "ip", "daddr", netnsDevInet, "udp", "dport", strconv.Itoa(quicRejectPort), "counter")
		return func() int {
			out, _ := run("nft", "list", "chain", "inet", netnsQUICCountNftable, hook)
			return gwNftPackets(out)
		}
	}
	chain := strings.ToUpper(hook)
	spec := func(action string, at ...string) []string {
		args := append([]string{engine, "-w", "-t", "mangle", action, chain}, at...)
		return append(args, "-d", netnsDevInet, "-p", "udp", "--dport", strconv.Itoa(quicRejectPort))
	}
	netnsRun(t, spec("-I", "1")...)
	t.Cleanup(func() { _, _ = run(spec("-D")...) })
	return func() int {
		out, _ := run(engine, "-w", "-t", "mangle", "-L", chain, "-v", "-x", "-n")
		for _, line := range strings.Split(out, "\n") {
			if !strings.Contains(line, netnsDevInet) || !strings.Contains(line, fmt.Sprintf("dpt:%d", quicRejectPort)) {
				continue
			}
			if n, err := strconv.Atoi(strings.Fields(line)[0]); err == nil {
				return n
			}
		}
		return 0
	}
}

func netnsCollectedRefusesQUIC(cfg *config.Config) *config.Config {
	next := *cfg
	set := *cfg.Sets[0]
	set.UDP.Mode = config.UDPModeReject
	set.UDP.FilterQUIC = config.QUICFilterAll
	next.Sets = []*config.SetConfig{&set}
	return &next
}

func netnsCollectedPreChain(t *testing.T, engine, chain string) string {
	t.Helper()
	if engine == backendNFTables {
		out, _ := run("nft", "list", "chain", "inet", routeNftTable, chain)
		return out
	}
	out, _ := run(engine, "-w", "-t", "mangle", "-L", chain, "-v", "-x", "-n")
	return out
}

func netnsUDPToACollectedAddressStaysOnTheUpstream(t *testing.T, engine string) {
	t.Run("routed datagrams", func(t *testing.T) {
		dev, base, st := netnsCollectedProxySetUp(t, engine)
		port, _ := portFromState(st)
		relay := netnsServeUDPRelay(t, port)
		uplink := netnsCountUplinkUDPTo(t, engine, netnsDevInet)
		RoutingSyncConfig(base.WithCollectedIPs([]string{netnsDevInet}))

		got := dev.datagrams(t, netnsDevInet, netnsCollectedUDPPort)
		time.Sleep(200 * time.Millisecond)
		dst := net.JoinHostPort(netnsDevInet, strconv.Itoa(netnsCollectedUDPPort))
		want := []string{"one@" + dst, "two@" + dst, "three@" + dst}
		seen, leaked := relay(), uplink()
		if got != "one:relay:one two:relay:two three:relay:three" || !slices.Equal(seen, want) || leaked != 0 {
			t.Logf("%s:\n%s", st.chainPre, netnsCollectedPreChain(t, engine, st.chainPre))
			t.Errorf("three datagrams from one socket to the collected address %s: the device got %q, the upstream relay saw %v (want %v) and %d left by the uplink (want 0); the first datagram of a UDP flow to a collected address must be tagged for the set, or every later one meets the guard's return and leaves by the uplink", dst, got, seen, want, leaked)
		}
		if engine != backendNFTables {
			listing := netnsCollectedPreChain(t, engine, st.chainPre)
			writes := 0
			for _, line := range strings.Split(listing, "\n") {
				if !strings.Contains(line, "CONNMARK") || !strings.Contains(line, "ctstate NEW") {
					continue
				}
				writes++
				if !routeCollectedGuardLine(line) {
					t.Errorf("a connection-mark write of the collected guard is not recognised as one, so the connection-mark check counts it as an interface claim: %s", line)
				}
			}
			if writes < 2 {
				t.Errorf("expected the TCP and the UDP tag of the collected guard in %s, found %d connection-mark write(s):\n%s", st.chainPre, writes, listing)
			}
		}
	})

	t.Run("QUIC refused", func(t *testing.T) {
		dev, base, st := netnsCollectedProxySetUp(t, engine)
		port, _ := portFromState(st)
		relay := netnsServeUDPRelay(t, port)
		cfg := netnsCollectedRefusesQUIC(base).WithCollectedIPs([]string{netnsDevInet})
		RoutingSyncConfig(cfg)
		routeMu.Lock()
		cur := routeRuleCache[base.Sets[0].Id]
		routeMu.Unlock()
		if !cur.quicReject {
			t.Fatal("the set does not refuse QUIC, so this run proves nothing")
		}
		forwarded, input := netnsCountQUICTo(t, engine, "forward"), netnsCountQUICTo(t, engine, "input")

		got := dev.datagrams(t, netnsDevInet, quicRejectPort)
		time.Sleep(200 * time.Millisecond)
		if fw, in, seen := forwarded(), input(), relay(); !strings.HasPrefix(got, "one:refused") || fw == 0 || in != 0 || len(seen) != 0 {
			t.Logf("%s:\n%s", cur.chainPre, netnsCollectedPreChain(t, engine, cur.chainPre))
			t.Errorf("QUIC to the collected address %s of a set that refuses QUIC: the device got %q, %d datagram(s) were forwarded (want some) and %d reached the router's own input (want 0), the relay saw %v (want none); a UDP/443 datagram must reach the forward QUIC refusal, not be marked for the proxy and delivered to the router", netnsDevInet, got, fw, in, seen)
		}
		_ = st
	})
}

func TestNetnsUDPToACollectedAddressStaysOnTheUpstream(t *testing.T) {
	netnsUDPToACollectedAddressStaysOnTheUpstream(t, backendIPTables)
}

func TestNetnsUDPToACollectedAddressStaysOnTheUpstreamNft(t *testing.T) {
	netnsUDPToACollectedAddressStaysOnTheUpstream(t, backendNFTables)
}

const netnsConntrackAcct = "net/netfilter/nf_conntrack_acct"

func netnsReadSysctl(t *testing.T, key string) string {
	t.Helper()
	b, err := os.ReadFile("/proc/sys/" + key)
	if err != nil {
		t.Fatalf("read %s: %v", key, err)
	}
	return strings.TrimSpace(string(b))
}

func netnsClearingRulesLeavesKernelForcedAccountingOn(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	if _, err := os.Stat("/proc/sys/" + netnsConntrackAcct); err != nil {
		t.Skipf("no %s in this namespace: %v", netnsConntrackAcct, err)
	}
	prevSnap := sysctlSnapPath
	sysctlSnapPath = filepath.Join(t.TempDir(), "sysctl.json")
	t.Cleanup(func() { sysctlSnapPath = prevSnap })
	gwKeepSysctl(t, netnsConntrackAcct, "0")

	routeEngine = nil
	cfg := netnsConfig(engine)
	cfg.Sets[0].TCP.IPBlockDetect.Enabled = true
	cfg.Sets[0].TCP.IPBlockDetect.Action = "collect"
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() {
		RoutingClearAll()
		_ = ClearRules(cfg)
		routeEngine = nil
	})
	if got := netnsReadSysctl(t, netnsConntrackAcct); got != "1" {
		t.Fatalf("after b4 loaded its rules for a Collect producer, nf_conntrack_acct reads %s, want 1", got)
	}
	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if got := netnsReadSysctl(t, netnsConntrackAcct); got != "1" {
		t.Errorf("clearing the rules set nf_conntrack_acct to %s; the kernel turned it on itself when b4's counter rules loaded, so b4 never owned the 0 it saved before them and must not write it back", got)
	}
}

func TestNetnsClearingRulesLeavesKernelForcedAccountingOn(t *testing.T) {
	netnsClearingRulesLeavesKernelForcedAccountingOn(t, backendIPTables)
}

func TestNetnsClearingRulesLeavesKernelForcedAccountingOnNft(t *testing.T) {
	netnsClearingRulesLeavesKernelForcedAccountingOn(t, backendNFTables)
}

func netnsADiscoveryProbeToACollectedAddressSkipsTheConsumer(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)

	routeEngine = nil
	base := netnsLocalGuardConfig(engine, "", []string{netnsDevOther})
	base.Queue.Mark = 0x8001
	base.Sets[0].Targets.UseCollectedIPs = true
	probeMark := base.DiscoveryFlowMark()
	if probeMark&base.Queue.Mark == base.Queue.Mark || uint32(probeMark)&routeSetMarkMask == 0 {
		t.Fatalf("the flow mark 0x%x under the queue mark 0x%x does not take the path this test covers", probeMark, base.Queue.Mark)
	}
	if err := AddRules(base); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() {
		RoutingClearAll()
		_ = ClearRules(base)
		routeEngine = nil
	})
	RoutingSyncConfig(base.WithCollectedIPs([]string{netnsDevInet}))
	routeMu.Lock()
	st, ok := routeRuleCache[base.Sets[0].Id]
	routeMu.Unlock()
	if !ok {
		t.Fatal("the collected proxy set built no rules")
	}
	port, _ := portFromState(st)
	ln := netnsMarkedTransparentListener(t, port, SelfDialMark)
	accepted := make(chan struct{}, 8)
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			accepted <- struct{}{}
			_ = conn.Close()
		}
	}()
	taken := func() bool {
		select {
		case <-accepted:
			return true
		case <-time.After(500 * time.Millisecond):
			return false
		}
	}
	target := net.JoinHostPort(netnsDevInet, "443")

	uplink := netnsCountUplinkPacketsTo(t, engine, netnsDevInet)
	if conn, err := netnsMarkedDialer(int(probeMark), nil).Dial("tcp4", target); err == nil {
		_ = conn.Close()
	}
	if taken() {
		t.Errorf("a Discovery probe with flow mark 0x%x to the collected address %s reached the proxy set's listener; its mark has a bit under 0x%x, so the set must leave it alone and Discovery must measure the strategy, not the upstream", probeMark, netnsDevInet, routeSetMarkMask)
	}
	if n := uplink(); n == 0 {
		netnsLogState(t, engine)
		t.Errorf("the Discovery probe to %s sent no packet by the uplink", netnsDevInet)
	}

	conn, err := netnsMarkedDialer(0, nil).Dial("tcp4", target)
	if err != nil {
		t.Fatalf("the router's own unmarked dial to the collected address %s failed: %v", netnsDevInet, err)
	}
	_ = conn.Close()
	if !taken() {
		t.Errorf("the router's own unmarked dial to the collected address %s did not reach the proxy set's listener", netnsDevInet)
	}
}

func TestNetnsADiscoveryProbeToACollectedAddressSkipsTheConsumer(t *testing.T) {
	netnsADiscoveryProbeToACollectedAddressSkipsTheConsumer(t, backendIPTables)
}

func TestNetnsADiscoveryProbeToACollectedAddressSkipsTheConsumerNft(t *testing.T) {
	netnsADiscoveryProbeToACollectedAddressSkipsTheConsumer(t, backendNFTables)
}
