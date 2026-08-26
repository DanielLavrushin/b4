package tables

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/netif"
	"golang.org/x/sys/unix"
)

const (
	netnsTunDevice   = "b4tun0"
	netnsTunIP       = "10.201.3.1"
	netnsTunQueueNum = 561
)

func netnsOpenTun(t *testing.T, name string) int {
	t.Helper()

	fd, err := unix.Open("/dev/net/tun", unix.O_RDWR, 0)
	if err != nil {
		t.Skipf("/dev/net/tun is not usable here: %v", err)
	}

	var ifr struct {
		name  [unix.IFNAMSIZ]byte
		flags uint16
		_     [22]byte
	}
	copy(ifr.name[:], name)
	ifr.flags = unix.IFF_TUN | unix.IFF_NO_PI

	if _, _, errno := unix.Syscall(unix.SYS_IOCTL, uintptr(fd),
		uintptr(unix.TUNSETIFF), uintptr(unsafe.Pointer(&ifr))); errno != 0 {
		_ = unix.Close(fd)
		t.Skipf("TUNSETIFF %s: %v", name, errno)
	}

	t.Cleanup(func() { _ = unix.Close(fd) })

	netnsRun(t, "ip", "link", "set", name, "up")
	netnsRun(t, "ip", "addr", "add", netnsTunIP+"/24", "dev", name)
	netif.Forget()

	if _, err := os.Stat(netif.Root + netnsPrimary); err != nil {
		t.Skip("/sys is the host's, not this namespace's: run through 'make test-netns', which remounts sysfs")
	}
	if !netif.IsUserspaceTunnel(name) {
		t.Fatalf("%s was created through /dev/net/tun but netif does not recognise it; the whole loop guard hangs off that check", name)
	}
	return fd
}

func netnsTunConfig(engine, routerTraffic string) *config.Config {
	cfg := netnsConfig(engine)
	cfg.Queue.StartNum = netnsTunQueueNum
	cfg.Sets[0].Routing.EgressInterface = netnsTunDevice
	cfg.Sets[0].Routing.RouterTraffic = routerTraffic
	return cfg
}

func netnsCountersFor(t *testing.T, devs ...string) {
	t.Helper()
	for _, dev := range devs {
		spec := []string{"iptables", "-w", "-t", "mangle", "-C", "POSTROUTING",
			"-o", dev, "-d", netnsTarget, "-p", "tcp", "-j", "ACCEPT"}
		if _, err := run(spec...); err == nil {
			continue
		}
		spec[4] = "-I"
		netnsRun(t, spec...)

		del := append([]string{}, spec...)
		del[4] = "-D"
		t.Cleanup(func() { _, _ = run(del...) })
	}
}

func netnsSeedSet(t *testing.T, engine string) {
	t.Helper()
	setName := netnsRoutingSetName(t, engine)
	if engine == backendNFTables {
		netnsRun(t, "nft", "add", "element", "inet", routeNftTable, setName, "{", netnsTarget, "}")
		return
	}
	netnsRun(t, "ipset", "add", setName, netnsTarget, "-exist")
}

func TestNetnsUserspaceTunnelDoesNotSwallowRouterTraffic(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsOpenTun(t, netnsTunDevice)

	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			t.Run("auto keeps it out", func(t *testing.T) {
				netnsTunEgressCase(t, engine, config.RouterTrafficAuto, netnsPrimary,
					"a userspace proxy answers a routed connection by opening its own to the same address; marking that dial routes it back into the same TUN and the loop never ends")
			})
			t.Run("include puts it in", func(t *testing.T) {
				netnsTunEgressCase(t, engine, config.RouterTrafficInclude, netnsTunDevice,
					"an explicit include is the operator saying the egress cannot re-dial, so the router's own traffic follows the set again")
			})
		})
	}
}

func netnsTunEgressCase(t *testing.T, engine, routerTraffic, want, why string) {
	t.Helper()
	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsTunConfig(engine, routerTraffic)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	netnsSeedSet(t, engine)
	netnsCountersFor(t, netnsPrimary, netnsSecondary, netnsTunDevice)

	stopQueue := netnsStartQueueListener(t, uint16(cfg.Queue.StartNum))
	defer stopQueue()

	netnsZeroCounters(t)
	netnsSendMarked(t, 0, 41010, 8443)

	counts := netnsEgressCounts(t)
	if counts[want] != 1 {
		netnsLogState(t, engine)
		t.Errorf("router traffic with router_traffic=%q: expected the packet on %s, counters were %v - %s",
			routerTraffic, want, counts, why)
	}
	for dev, n := range counts {
		if dev != want && n != 0 {
			t.Errorf("router traffic with router_traffic=%q: %d packet(s) left over %s instead", routerTraffic, n, dev)
		}
	}
}

func TestNetnsUserspaceTunnelChainCarriesNoBareMarkRule(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsOpenTun(t, netnsTunDevice)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsTunConfig(backendNFTables, config.RouterTrafficAuto)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	out := netnsRun(t, "nft", "list", "table", "inet", routeNftTable)
	chain := netnsChainBody(out, "_out")
	if chain == "" {
		t.Fatalf("no out chain in:\n%s", out)
	}

	for _, line := range strings.Split(chain, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "meta mark set") {
			continue
		}
		if !strings.Contains(line, "meta mark & 0x00008000") {
			t.Errorf("the out chain of a TUN-egress set may only re-mark what b4 injected itself; %q marks by destination alone and pulls the proxy's own dials back in", line)
		}
	}

	if !strings.Contains(chain, "meta mark & 0x00008000") {
		t.Errorf("the rule that re-marks b4's injected packets went missing, so its fakes would leave by the wrong uplink:\n%s", chain)
	}
}

func netnsChainBody(listing, suffix string) string {
	var b strings.Builder
	inChain := false
	for _, line := range strings.Split(listing, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "chain ") {
			name := strings.TrimSpace(strings.TrimSuffix(trimmed[len("chain "):], "{"))
			inChain = strings.HasSuffix(name, suffix)
			continue
		}
		if trimmed == "}" {
			inChain = false
			continue
		}
		if inChain {
			b.WriteString(line)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func netnsReadTunSYNs(t *testing.T, fd int, redial bool, until time.Duration) int {
	t.Helper()
	if err := unix.SetNonblock(fd, true); err != nil {
		t.Fatalf("set tun non-blocking: %v", err)
	}

	buf := make([]byte, 2048)
	seen := 0
	deadline := time.Now().Add(until)

	for time.Now().Before(deadline) {
		n, err := unix.Read(fd, buf)
		if err != nil || n < 40 {
			time.Sleep(10 * time.Millisecond)
			continue
		}
		pkt := buf[:n]
		if pkt[0]>>4 != 4 || pkt[9] != unix.IPPROTO_TCP {
			continue
		}
		ihl := int(pkt[0]&0x0F) * 4
		if len(pkt) < ihl+20 {
			continue
		}
		flags := pkt[ihl+13]
		if flags&0x02 == 0 || flags&0x10 != 0 {
			continue
		}
		seen++
		if !redial || seen > 50 {
			continue
		}
		dst := net.IP(pkt[16:20]).String()
		port := int(pkt[ihl+2])<<8 | int(pkt[ihl+3])
		go func() {
			c, err := net.DialTimeout("tcp", net.JoinHostPort(dst, strconv.Itoa(port)), 900*time.Millisecond)
			if err == nil {
				_ = c.Close()
			}
		}()
	}
	return seen
}

func TestNetnsUserspaceTunnelSurvivesARedialingProxy(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	fd := netnsOpenTun(t, netnsTunDevice)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsTunConfig(backendNFTables, config.RouterTrafficAuto)
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()
	netnsSeedSet(t, backendNFTables)

	done := make(chan int, 1)
	go func() { done <- netnsReadTunSYNs(t, fd, true, 2*time.Second) }()

	if c, err := net.DialTimeout("tcp", net.JoinHostPort(netnsTarget, "443"), 300*time.Millisecond); err == nil {
		_ = c.Close()
	}

	if seen := <-done; seen != 0 {
		t.Errorf("%d SYN(s) reached %s from the router's own dial; each one makes the proxy dial again and the loop is what takes the box's memory", seen, netnsTunDevice)
	}
}
