package tables

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
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
