package tables

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

func discoveryDSCPSendV6(t *testing.T, mark uint32, addrs ...string) setDSCPSeen {
	t.Helper()
	netnsRun(t, "nft", "flush", "set", "inet", setDSCPV6Observer, "seen")
	for _, addr := range addrs {
		fd, err := unix.Socket(unix.AF_INET6, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatalf("IPv6 socket: %v", err)
		}
		if err := unix.SetsockoptInt(fd, unix.SOL_SOCKET, unix.SO_MARK, int(mark)); err != nil {
			unix.Close(fd)
			t.Fatalf("SO_MARK 0x%x: %v", mark, err)
		}
		sa := &unix.SockaddrInet6{Port: setDSCPDataPort}
		copy(sa.Addr[:], net.ParseIP(addr).To16())
		err = unix.Sendto(fd, []byte("dscp"), 0, sa)
		unix.Close(fd)
		if err != nil {
			t.Fatalf("send to %s: %v", addr, err)
		}
	}
	time.Sleep(150 * time.Millisecond)
	return setDSCPParse(t, netnsRun(t, "nft", "-n", "list", "set", "inet", setDSCPV6Observer, "seen"))
}

func discoveryDSCPAssertGone(t *testing.T, engine string) {
	t.Helper()
	if engine == backendNFTables {
		if _, err := run("nft", "list", "chain", "inet", nftTableName, discoveryStampChainNFT); err == nil {
			t.Errorf("the end of the run left the %s chain behind", discoveryStampChainNFT)
		}
		return
	}
	for _, bin := range setDSCPBinaries(engine) {
		if present, known := iptChainPresence(bin, "mangle", discoveryStampChainIPT); present || !known {
			t.Errorf("%s: the end of the run left the mangle chain %s (present=%t, known=%t)", bin, discoveryStampChainIPT, present, known)
		}
		if out := netnsRun(t, bin, "-w", "-t", "mangle", "-S", "POSTROUTING"); strings.Contains(out, "-j "+discoveryStampChainIPT) {
			t.Errorf("%s: mangle POSTROUTING still jumps to %s:\n%s", bin, discoveryStampChainIPT, out)
		}
	}
}

func TestNetnsDiscoveryProbesCarryTheRunsDSCP(t *testing.T) {
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		setDSCPObserve(t)
		setDSCPV6Link(t)
		setDSCPV6Observe(t)

		cfg := setDSCPConfig(engine, true, dscpPlanTestSet("dscp-a", 31, setDSCPAddrA))
		setDSCPStart(t, cfg)
		setDSCPAssertPerSet(t, engine)

		flow, injected := uint32(cfg.DiscoveryFlowMark()), uint32(cfg.DiscoveryInjectedMark())
		queue := cfg.Queue.StartNum + cfg.Queue.Threads
		t.Cleanup(func() { ClearDiscoverySteeringRules(cfg, uint(flow), uint(injected)) })
		probes := func(sport uint16) func() {
			return func() {
				setDSCPSendMarked(t, flow, sport, setDSCPAddrA, setDSCPAddrOther)
				setDSCPSendMarked(t, injected, sport+1, setDSCPAddrA, setDSCPAddrOther)
			}
		}
		untagged := map[string]int{setDSCPAddrA: 31, setDSCPAddrOther: dscpTestValue}

		if err := ApplyDiscoverySteeringRules(cfg, uint(flow), uint(injected), queue, 1, nil, nil); err != nil {
			t.Fatalf("ApplyDiscoverySteeringRules without a value: %v", err)
		}
		setDSCPCheck(t, "a run with no set value: the global value, and a set's value for that set's own addresses", untagged, probes(42101))
		setDSCPExpect(t, "IPv6 probe of a run with no set value", discoveryDSCPSendV6(t, flow, setDSCPAddrOther6), map[string]int{setDSCPAddrOther6: dscpTestValue})

		lost := make(chan error, 4)
		probe := &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true}
		if err := ApplyDiscoverySteeringRules(cfg, uint(flow), uint(injected), queue, 1, probe, func(err error) { lost <- err }); err != nil {
			t.Fatalf("ApplyDiscoverySteeringRules with DSCP 25: %v", err)
		}
		if got := ActiveDiscoveryDSCP(); got == nil || *got != 25 {
			t.Fatalf("the run reports the value it stamps, got %v", got)
		}
		tagged := map[string]int{setDSCPAddrA: 25, setDSCPAddrOther: 25}
		setDSCPCheck(t, "probes and injected packets of a run for a set with DSCP 25", tagged, probes(42111))
		setDSCPExpect(t, "IPv6 probe of a run for a set with DSCP 25", discoveryDSCPSendV6(t, flow, setDSCPAddrOther6), map[string]int{setDSCPAddrOther6: 25})
		setDSCPCheck(t, "this host's own traffic outside the run during it", untagged, func() {
			setDSCPSendUDP(t, 0, setDSCPDataPort, setDSCPAddrA, setDSCPAddrOther)
		})

		if err := RefreshRules(setDSCPConfig(engine, true, dscpPlanTestSet("dscp-a", 31, setDSCPAddrA))); err != nil {
			t.Fatalf("RefreshRules: %v", err)
		}
		setDSCPCheck(t, "a run's probes after a firewall rebuild, until the run stops", tagged, probes(42121))
		select {
		case err := <-lost:
			if !errors.Is(err, errDiscoveryDSCPRewritten) {
				t.Errorf("the run should learn that the DSCP rules were rewritten, got %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Error("a rebuild rewrote the DSCP rules during a run for a set, and the run was never told, so packets sent while they were rewritten could take another path")
		}

		ClearDiscoverySteeringRules(cfg, uint(flow), uint(injected))
		setDSCPCheck(t, "packets with Discovery's marks after the run", untagged, probes(42131))
		discoveryDSCPAssertGone(t, engine)
	})
}

func discoveryDSCPMarkedProbes(t *testing.T, cfg *config.Config, sport uint16) func() {
	return func() {
		setDSCPSendMarked(t, uint32(cfg.DiscoveryFlowMark()), sport, setDSCPAddrA, setDSCPAddrOther)
		setDSCPSendMarked(t, uint32(cfg.DiscoveryInjectedMark()), sport+1, setDSCPAddrA, setDSCPAddrOther)
	}
}

func discoveryDSCPStartRun(t *testing.T, cfg *config.Config, lost func(error)) {
	t.Helper()
	flow, injected := uint(cfg.DiscoveryFlowMark()), uint(cfg.DiscoveryInjectedMark())
	t.Cleanup(func() { ClearDiscoverySteeringRules(cfg, flow, injected) })
	probe := &config.DSCPProbe{Value: 25, IPv4: true, IPv6: true}
	if err := ApplyDiscoverySteeringRules(cfg, flow, injected, cfg.Queue.StartNum+cfg.Queue.Threads, 1, probe, lost); err != nil {
		t.Fatalf("ApplyDiscoverySteeringRules: %v", err)
	}
}

func netnsDiscoveryFollowsAFallback(t *testing.T, engine string, global bool) {
	setDSCPObserve(t)
	stubBinaryPresence(t, map[string]bool{"ipset": false})

	cfg := setDSCPConfig(engine, global, dscpPlanTestSet("dscp-a", 31, setDSCPAddrA))
	setDSCPStart(t, cfg)
	want := 0
	if st := dscpApplied.Load(); global {
		want = dscpTestValue
		if st == nil || st.ipt == nil || st.ipt.incapable[setDSCPBinaries(engine)[0]] == "" {
			t.Fatalf("without the ipset command the IPv4 firewall must fall back to the global value only, state %+v", st)
		}
	} else if st != nil {
		t.Fatalf("precondition: with the global stamp off and no ipset, b4 keeps no DSCP state, got %+v", st)
	}
	live := map[string]int{setDSCPAddrA: want, setDSCPAddrOther: want}
	setDSCPCheck(t, "this host's own traffic with the set's value out of reach", live, func() {
		setDSCPSendUDP(t, 0, setDSCPDataPort, setDSCPAddrA, setDSCPAddrOther)
	})

	discoveryDSCPStartRun(t, cfg, nil)
	if got := ActiveDiscoveryDSCP(); got != nil {
		t.Errorf("the set's traffic never carries its own value here, so the run must not claim DSCP %d", *got)
	}
	setDSCPCheck(t, "a run for the set probes the path its traffic takes", live, discoveryDSCPMarkedProbes(t, cfg, 42201))
}

func TestNetnsDiscoveryProbesFollowAGlobalOnlyFallback(t *testing.T) {
	setDSCPEngines(t, []string{backendIPTables, backendIPTablesLegacy}, func(t *testing.T, engine string) {
		netnsDiscoveryFollowsAFallback(t, engine, true)
	})
}

func TestNetnsDiscoveryProbesFollowAnUnstampedFallback(t *testing.T) {
	setDSCPEngines(t, []string{backendIPTables, backendIPTablesLegacy}, func(t *testing.T, engine string) {
		netnsDiscoveryFollowsAFallback(t, engine, false)
	})
}

func TestNetnsDiscoveryRunIsStoppedWhenARebuildMovesItsProbes(t *testing.T) {
	setDSCPEngines(t, []string{backendIPTables, backendIPTablesLegacy}, func(t *testing.T, engine string) {
		setDSCPObserve(t)
		build := func() *config.Config {
			return setDSCPConfig(engine, true, dscpPlanTestSet("dscp-a", 31, setDSCPAddrA))
		}
		cfg := build()
		setDSCPStart(t, cfg)
		setDSCPAssertPerSet(t, engine)

		lost := make(chan error, 4)
		discoveryDSCPStartRun(t, cfg, func(err error) { lost <- err })
		setDSCPCheck(t, "the run's probes before the rebuild", map[string]int{setDSCPAddrA: 25, setDSCPAddrOther: 25}, discoveryDSCPMarkedProbes(t, cfg, 42301))

		stubBinaryPresence(t, map[string]bool{"ipset": false})
		if err := RefreshRules(build()); err != nil {
			t.Fatalf("RefreshRules: %v", err)
		}
		select {
		case err := <-lost:
			if !errors.Is(err, errDiscoveryDSCPRewritten) {
				t.Errorf("the run should learn that the DSCP rules were rewritten, got %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("the rebuild took the set's value off the set's traffic and the run's probes, and the run was never told, so it would mix results from two paths")
		}
		setDSCPCheck(t, "the run's probes after the rebuild follow the set's traffic to the global value", map[string]int{setDSCPAddrA: dscpTestValue, setDSCPAddrOther: dscpTestValue}, discoveryDSCPMarkedProbes(t, cfg, 42311))
	})
}

func TestNetnsDiscoveryRunIsStoppedByADSCPOnlyChange(t *testing.T) {
	setDSCPEngines(t, setDSCPAllEngines, func(t *testing.T, engine string) {
		setDSCPObserve(t)
		cfg := setDSCPConfig(engine, true, dscpPlanTestSet("dscp-a", 31, setDSCPAddrA))
		setDSCPStart(t, cfg)
		setDSCPAssertPerSet(t, engine)

		lost := make(chan error, 4)
		discoveryDSCPStartRun(t, cfg, func(err error) { lost <- err })

		next := setDSCPConfig(engine, true, dscpPlanTestSet("dscp-a", 6, setDSCPAddrA))
		setDSCPSync(t, next)
		select {
		case err := <-lost:
			if !errors.Is(err, errDiscoveryDSCPRewritten) {
				t.Errorf("the run should learn that the DSCP rules were rewritten, got %v", err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("a save that changed only the DSCP rules gave the set's traffic another value during the run, and the run was never told")
		}
		setDSCPCheck(t, "this host's own traffic after the change", map[string]int{setDSCPAddrA: 6, setDSCPAddrOther: dscpTestValue}, func() {
			setDSCPSendUDP(t, 0, setDSCPDataPort, setDSCPAddrA, setDSCPAddrOther)
		})
	})
}
