package tables

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
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
