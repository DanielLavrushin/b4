package tun

import (
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/engine"
)

type carrierNetnsPacket struct {
	proto    byte
	src, dst string
	dir      engine.Direction
}

type carrierNetnsLane struct {
	mu   sync.Mutex
	seen []carrierNetnsPacket
}

func (l *carrierNetnsLane) ProcessPacketDir(raw []byte, dir engine.Direction) engine.PacketVerdict {
	if len(raw) >= 20 {
		l.mu.Lock()
		l.seen = append(l.seen, carrierNetnsPacket{proto: raw[9], src: net.IP(raw[12:16]).String(), dst: net.IP(raw[16:20]).String(), dir: dir})
		l.mu.Unlock()
	}
	return engine.VerdictAccept
}

func (l *carrierNetnsLane) packets() []carrierNetnsPacket {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]carrierNetnsPacket(nil), l.seen...)
}

func carrierNetnsSend(t *testing.T, proto int, dst [4]byte, port, mark int) {
	t.Helper()
	typ := syscall.SOCK_DGRAM
	if proto == syscall.IPPROTO_TCP {
		typ = syscall.SOCK_STREAM
	}
	fd, err := syscall.Socket(syscall.AF_INET, typ|syscall.SOCK_NONBLOCK|syscall.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.Close(fd)
	if err := syscall.SetsockoptInt(fd, syscall.SOL_SOCKET, syscall.SO_MARK, mark); err != nil {
		t.Fatal(err)
	}
	sa := &syscall.SockaddrInet4{Port: port, Addr: dst}
	if proto == syscall.IPPROTO_TCP {
		_ = syscall.Connect(fd, sa)
		time.Sleep(50 * time.Millisecond)
		return
	}
	_ = syscall.Sendto(fd, []byte("probe"), 0, sa)
}

func carrierNetnsLinkExists(name string) bool {
	return exec.Command("ip", "link", "show", "dev", name).Run() == nil
}

func carrierNetnsTxPackets(t *testing.T, dev string) uint64 {
	t.Helper()
	lines := strings.Split(netnsIP(t, "-s", "link", "show", "dev", dev), "\n")
	for i, line := range lines {
		if !strings.HasPrefix(strings.TrimSpace(line), "TX:") || i+1 >= len(lines) {
			continue
		}
		if fields := strings.Fields(lines[i+1]); len(fields) > 1 {
			if n, err := strconv.ParseUint(fields[1], 10, 64); err == nil {
				return n
			}
		}
	}
	t.Fatalf("no TX counters for %s", dev)
	return 0
}

func TestNetnsCarrierProbesWithMarksOutsideTheQueueMarkStayOutOfPortCapture(t *testing.T) {
	tunNetnsRequire(t)
	netnsIP(t, "link", "set", "lo", "up")
	netnsDummy(t, "b4dclan0", "192.168.1.1/24")
	netnsDummy(t, "b4dcwan0", "203.0.113.2/24")
	netnsDummy(t, "b4dccap0", "")
	if err := os.WriteFile("/proc/sys/net/ipv6/conf/b4dccap0/disable_ipv6", []byte("1\n"), 0o644); err != nil {
		t.Logf("IPv6 stays on b4dccap0 (%v); its own IPv6 packets may count as captured", err)
	}
	netnsMoveLocalRule(t, 0)
	netnsIP(t, "route", "add", "default", "via", "203.0.113.1", "dev", "b4dcwan0")
	netnsIP(t, "route", "add", "default", "dev", "b4dccap0", "table", "96")
	netnsIP(t, "rule", "add", "fwmark", "0x40000000/0x40000000", "lookup", "96", "priority", "10")
	t.Cleanup(func() {
		exec.Command("ip", "rule", "del", "priority", "10").Run()
		exec.Command("ip", "route", "flush", "table", "96").Run()
		exec.Command("ip", "route", "del", "default", "via", "203.0.113.1", "dev", "b4dcwan0").Run()
	})

	r := &routeManager{
		tunName:           "b4dccap0",
		mark:              0x8000,
		outIface:          "b4dcwan0",
		tcpPorts:          []string{"443"},
		udpPorts:          []string{"443"},
		tcpLimit:          19,
		udpLimit:          8,
		noConnbytes:       true,
		captureTable:      96,
		capturePrio:       10,
		discoveryFlow:     0x100001,
		discoveryInjected: 0x100002,
	}
	if r.ensureCaptureChain() < 0 {
		t.Skip("iptables cannot create a mangle chain in this namespace")
	}
	t.Cleanup(func() {
		exec.Command("iptables", "-t", "mangle", "-D", "OUTPUT", "-j", tunCaptureChain).Run()
		exec.Command("iptables", "-t", "mangle", "-F", tunCaptureChain).Run()
		exec.Command("iptables", "-t", "mangle", "-X", tunCaptureChain).Run()
	})
	r.rebuildCaptureChain()
	if r.captureMissing > 0 {
		t.Skipf("%d capture rule(s) could not be added in this namespace", r.captureMissing)
	}
	r.ensureJump("OUTPUT", "-j", tunCaptureChain)

	e := carrierEngine("ports", "203.0.113.2")
	e.routes.outIface = "b4dcwan0"
	lane := &carrierNetnsLane{}
	c, err := e.startDiscoveryCarrier(DiscoveryCarrierRequest{FlowMark: 0x100001, InjectedMark: 0x100002, Lost: func(error) {}}, lane)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Stop)

	captured := carrierNetnsTxPackets(t, "b4dccap0")
	carrierNetnsSend(t, syscall.IPPROTO_UDP, [4]byte{8, 8, 8, 8}, 53, 0x100001)
	carrierNetnsSend(t, syscall.IPPROTO_TCP, [4]byte{8, 8, 8, 8}, 443, 0x100001)
	deadline := time.Now().Add(2 * time.Second)
	for len(lane.packets()) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)

	if got := lane.packets(); len(got) != 2 {
		t.Errorf("probes whose flow mark lacks a bit of the queue mark must reach the carrier, not the capture device, got %+v", got)
	}
	if n := c.sendErrs.Load(); n != 0 {
		t.Errorf("the lane failed to re-send %d packet(s)", n)
	}
	if n := carrierNetnsTxPackets(t, "b4dccap0") - captured; n != 0 {
		t.Errorf("neither a probe nor its re-send may enter the capture device, %d packet(s) did", n)
	}
}

func TestNetnsDiscoveryCarrierCarriesOnlyRemoteProbesAndLeavesNothingBehind(t *testing.T) {
	tunNetnsRequire(t)
	netnsIP(t, "link", "set", "lo", "up")
	netnsDummy(t, "b4dclan0", "192.168.1.1/24")
	netnsDummy(t, "b4dcwan0", "203.0.113.2/24")
	netnsIP(t, "route", "add", "default", "via", "203.0.113.1", "dev", "b4dcwan0")
	netnsIP(t, "route", "add", "default", "via", "203.0.113.1", "dev", "b4dcwan0", "table", "97")
	t.Cleanup(func() {
		exec.Command("ip", "route", "flush", "table", "97").Run()
		exec.Command("ip", "route", "del", "default", "via", "203.0.113.1", "dev", "b4dcwan0").Run()
	})

	for _, tc := range []struct {
		name     string
		capture  string
		localAt  int
		wantRule []int
	}{
		{"ports, local at 0", "ports", 0, []int{carrierFlowPrio}},
		{"default, local at 0", "default", 0, []int{carrierFlowPrio, carrierInjectedMainPrio, carrierInjectedUplinkPrio}},
		{"ports, local at 200", "ports", 200, []int{carrierGuardPrio, carrierFlowPrio}},
		{"default, local at 200", "default", 200, []int{carrierGuardPrio, carrierFlowPrio, carrierInjectedMainPrio, carrierInjectedUplinkPrio}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			netnsMoveLocalRule(t, tc.localAt)
			e := carrierEngine(tc.capture, "203.0.113.2")
			e.routes.outIface = "b4dcwan0"
			lane := &carrierNetnsLane{}
			var lost error
			c, err := e.startDiscoveryCarrier(DiscoveryCarrierRequest{FlowMark: 0x8001, InjectedMark: 0x8002, Lost: func(err error) { lost = err }}, lane)
			if err != nil {
				t.Fatal(err)
			}
			stopped := false
			t.Cleanup(func() {
				if !stopped {
					c.Stop()
				}
			})

			rules := netnsIP(t, "rule", "show")
			for _, prio := range tc.wantRule {
				if !strings.Contains(rules, strconv.Itoa(prio)+":") {
					t.Errorf("no carrier rule at priority %d:\n%s", prio, rules)
				}
			}
			if addrs := strings.TrimSpace(netnsIP(t, "-o", "addr", "show", "dev", c.name)); addrs != "" {
				t.Errorf("the carrier device must have no address, got %s", addrs)
			}
			if err := c.check(); err != nil {
				t.Errorf("the reconcile check fails on the real rules: %v", err)
			}

			carrierNetnsSend(t, syscall.IPPROTO_UDP, [4]byte{8, 8, 8, 8}, 53, 0x8001)
			carrierNetnsSend(t, syscall.IPPROTO_TCP, [4]byte{8, 8, 8, 8}, 443, 0x8001)
			carrierNetnsSend(t, syscall.IPPROTO_UDP, [4]byte{203, 0, 113, 2}, 53, 0x8001)
			carrierNetnsSend(t, syscall.IPPROTO_UDP, [4]byte{192, 168, 1, 1}, 53, 0x8001)
			carrierNetnsSend(t, syscall.IPPROTO_UDP, [4]byte{127, 0, 0, 1}, 53, 0x8001)
			carrierNetnsSend(t, syscall.IPPROTO_UDP, [4]byte{8, 8, 4, 4}, 53, 0)
			deadline := time.Now().Add(2 * time.Second)
			for len(lane.packets()) < 2 && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			time.Sleep(100 * time.Millisecond)

			got := lane.packets()
			if len(got) != 2 {
				t.Errorf("exactly the two remote probes with the flow mark must reach the lane, got %+v", got)
			}
			for _, p := range got {
				if p.src != "203.0.113.2" || p.dst != "8.8.8.8" || p.dir != engine.DirOriginal {
					t.Errorf("a carried probe keeps the uplink source and goes out in the original direction, got %+v", p)
				}
			}
			if n := c.sendErrs.Load(); n != 0 {
				t.Errorf("the lane failed to re-send %d packet(s)", n)
			}

			c.Stop()
			stopped = true
			if lost != nil {
				t.Errorf("an intact carrier reported a loss: %v", lost)
			}
			for _, line := range strings.Split(netnsIP(t, "rule", "show"), "\n") {
				if prio, ok := rulePriority(line); ok && isCarrierPriority(prio) {
					t.Errorf("rule left after Stop: %s", line)
				}
			}
			if routes := strings.TrimSpace(netnsIP(t, "route", "show", "table", strconv.Itoa(c.table))); routes != "" {
				t.Errorf("table %d is not empty after Stop: %s", c.table, routes)
			}
			if carrierNetnsLinkExists(c.name) {
				t.Errorf("%s is left after Stop", c.name)
			}
		})
	}
}
