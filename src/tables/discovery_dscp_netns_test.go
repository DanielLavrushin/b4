package tables

import (
	"net"
	"strings"
	"testing"
	"time"

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

		if err := ApplyDiscoverySteeringRules(cfg, uint(flow), uint(injected), queue, 1, nil); err != nil {
			t.Fatalf("ApplyDiscoverySteeringRules without a value: %v", err)
		}
		setDSCPCheck(t, "a run with no set value: the global value, and a set's value for that set's own addresses", untagged, probes(42101))
		setDSCPExpect(t, "IPv6 probe of a run with no set value", discoveryDSCPSendV6(t, flow, setDSCPAddrOther6), map[string]int{setDSCPAddrOther6: dscpTestValue})

		value := 25
		if err := ApplyDiscoverySteeringRules(cfg, uint(flow), uint(injected), queue, 1, &value); err != nil {
			t.Fatalf("ApplyDiscoverySteeringRules with DSCP 25: %v", err)
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
		setDSCPCheck(t, "a run's probes after a firewall rebuild during the run", tagged, probes(42121))

		ClearDiscoverySteeringRules(cfg, uint(flow), uint(injected))
		setDSCPCheck(t, "packets with Discovery's marks after the run", untagged, probes(42131))
		discoveryDSCPAssertGone(t, engine)
	})
}
