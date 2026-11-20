package tun

import (
	"net"
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
