package tun

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/selfdial"
	"github.com/daniellavrushin/b4/sock"
)

func netnsTxPackets(t *testing.T, iface string) int {
	t.Helper()
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		t.Fatalf("read /proc/net/dev: %v", err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		name, stats, ok := strings.Cut(line, ":")
		if !ok || strings.TrimSpace(name) != iface {
			continue
		}
		fields := strings.Fields(stats)
		if len(fields) < 10 {
			t.Fatalf("short /proc/net/dev line for %s: %q", iface, line)
		}
		n, err := strconv.Atoi(fields[9])
		if err != nil {
			t.Fatalf("tx packets of %s: %v", iface, err)
		}
		return n
	}
	t.Fatalf("%s is not in /proc/net/dev", iface)
	return 0
}

func netnsMarkedSYN(t *testing.T, mark uint32, dst string) {
	t.Helper()
	d := net.Dialer{
		Timeout: 300 * time.Millisecond,
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_MARK, int(mark))
			}); err != nil {
				return err
			}
			return serr
		},
	}
	conn, err := d.Dial("tcp4", dst)
	if err == nil {
		_ = conn.Close()
		t.Fatalf("a dial to %s connected, but nothing in this namespace answers there", dst)
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("the dial marked 0x%x to %s failed before its SYN left: %v", mark, dst, err)
	}
}

func captureChainHits(t *testing.T) (skipped, steered int) {
	t.Helper()
	out, err := run("iptables", "-t", "mangle", "-L", tunCaptureChain, "-v", "-x", "-n")
	if err != nil {
		t.Fatalf("list %s: %v", tunCaptureChain, err)
	}
	skip := "mark match " + config.SelfDialSkipMarkMatch()
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pkts, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		switch {
		case strings.Contains(line, skip) && fields[2] == "RETURN":
			skipped += pkts
		case fields[2] == "MARK" && (strings.Contains(line, "dpt:443") || strings.Contains(line, "dports 443")):
			steered += pkts
		}
	}
	return skipped, steered
}

func routingSetReturns(t *testing.T, set string) int {
	t.Helper()
	out, err := run("iptables", "-t", "mangle", "-L", tunCaptureChain, "-v", "-x", "-n")
	if err != nil {
		t.Fatalf("list %s: %v", tunCaptureChain, err)
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || fields[2] != "RETURN" || !strings.Contains(line, "match-set "+set+" dst") {
			continue
		}
		pkts, err := strconv.Atoi(fields[0])
		if err == nil {
			n += pkts
		}
	}
	return n
}

func markedOutputPackets(t *testing.T, mark uint32) int {
	t.Helper()
	out, err := run("iptables", "-t", "mangle", "-L", "OUTPUT", "-v", "-x", "-n")
	if err != nil {
		t.Fatalf("list mangle OUTPUT: %v", err)
	}
	match := fmt.Sprintf("mark match 0x%x", mark)
	n := 0
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.HasSuffix(strings.TrimSpace(line), match) {
			continue
		}
		pkts, err := strconv.Atoi(fields[0])
		if err == nil {
			n += pkts
		}
	}
	return n
}

func TestNetnsTheTUNEngineResendsB4sTelegramConnectionsWithTheTelegramSetsMark(t *testing.T) {
	tunNetnsRequire(t)
	netnsDummy(t, "b4wan0", "198.51.100.1/24")
	netnsIP(t, "route", "add", "default", "dev", "b4wan0")
	netnsIP(t, "link", "set", "b4wan0", "arp", "off")

	set := config.NewSetConfig()
	set.Id, set.Name, set.Enabled = "telegram", "Telegram", true
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	set.Faking.SNI = false
	set.Faking.SNIMutation.Mode = config.ConfigOff
	set.TCP.Desync.Mode = config.ConfigOff
	set.TCP.Win.Mode = config.ConfigOff
	set.TCP.DropSACK = false
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "b4.json")
	cfg.Queue.TCPConnBytesLimit = 2
	cfg.Sets = []*config.SetConfig{&set}
	cfg.System.MTProto.UpstreamSet = set.Id
	if _, _, err := cfg.GetTargetsForSet(&set); err != nil {
		t.Fatal(err)
	}
	cfg.BuildTCPPortMap()
	cfg.BuildSetPortRanges()
	if cfg.TelegramUpstreamSet() == nil {
		t.Fatal("the test's Telegram set is not usable")
	}
	pool := nfq.NewPool(&cfg)
	t.Cleanup(pool.Stop)

	reinject := uint32(cfg.Queue.Mark) | engine.ReinjectMarkBit
	dialMark := reinject | config.TelegramSetDialMark
	sender, err := sock.NewSenderWithMark(int(reinject))
	if err != nil {
		t.Fatalf("raw socket with mark 0x%x: %v", reinject, err)
	}
	t.Cleanup(sender.Close)
	e := NewEngine(&cfg, pool)
	e.sender = sender
	e.limiter = newFirstNLimiter(captureParamsFrom(&cfg))
	e.divertOwnTelegramFlows(&cfg)
	if e.telegramSender == nil {
		t.Fatal("the engine opened no socket for the Telegram set's re-sends")
	}
	t.Cleanup(e.telegramSender.Close)

	const relay = "203.0.113.10"
	for _, m := range []uint32{dialMark, reinject} {
		spec := []string{"-t", "mangle", "-A", "OUTPUT", "-d", relay, "-m", "mark", "--mark", fmt.Sprintf("0x%x/0xffffffff", m)}
		if _, err := run(append([]string{"iptables"}, spec...)...); err != nil {
			t.Fatalf("add the counter for mark 0x%x: %v", m, err)
		}
		t.Cleanup(func() { _, _ = run(append([]string{"iptables", "-t", "mangle", "-D"}, spec[3:]...)...) })
	}

	const own, other = uint16(47400), uint16(47401)
	selfdial.Note(own, netip.MustParseAddrPort(relay+":443"))
	send := func(sport uint16, n int) {
		for i := range n {
			flags := byte(tcpACK)
			if i == 0 {
				flags = tcpSYN
			}
			pkt := testPacket(6, "198.51.100.1", relay, sport, 443, flags)
			if pass, dir := e.processPacket(pool.Workers[0], pkt); pass {
				e.forwardPacket(pkt, dir)
			}
		}
	}
	send(own, 6)
	send(other, 4)

	if dial, plain := markedOutputPackets(t, dialMark), markedOutputPackets(t, reinject); dial != 6 || plain != 4 {
		t.Fatalf("%d packets left with 0x%x and %d with 0x%x, want 6 and 4: every packet the engine re-sends into a connection b4 opened for the Telegram set, past the first ones the worker sees as well, carries the Telegram set's dial mark so a routing set that lists the address leaves it alone, and every other connection keeps the ordinary re-send mark", dial, dialMark, plain, reinject)
	}
}

func TestNetnsATelegramSetDialToAnAddressARoutingSetHoldsIsStillCaptured(t *testing.T) {
	tunNetnsRequire(t)
	if _, err := run("ipset", "--version"); err != nil {
		t.Skipf("ipset is not available: %v", err)
	}
	netnsDummy(t, "b4wan0", "198.51.100.1/24")
	netnsIP(t, "route", "add", "default", "dev", "b4wan0")
	netnsDummy(t, "b4cap0", "")
	for _, link := range []string{"b4wan0", "b4cap0"} {
		netnsIP(t, "link", "set", link, "arp", "off")
		if err := os.WriteFile("/proc/sys/net/ipv6/conf/"+link+"/disable_ipv6", []byte("1"), 0o644); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("disable IPv6 on %s: %v", link, err)
		}
	}
	const routed = "b4r_tgtest_v4"
	if _, err := run("ipset", "create", routed, "hash:net", "-exist"); err != nil {
		t.Fatalf("create the routing set's ipset: %v", err)
	}
	t.Cleanup(func() { _, _ = run("ipset", "destroy", routed) })
	if _, err := run("ipset", "add", routed, "203.0.113.10", "-exist"); err != nil {
		t.Fatalf("add the relay to the routing set's ipset: %v", err)
	}

	r := &routeManager{
		tunName:      "b4cap0",
		captureTable: 96,
		quit:         make(chan struct{}),
		mark:         0x8000,
		tcpPorts:     []string{"443"},
		tcpLimit:     19,
		udpLimit:     8,
		skipTables:   true,
	}
	r.resolveCaptureMode()
	if err := r.setupPortCapture(""); err != nil {
		t.Fatalf("setupPortCapture: %v", err)
	}
	t.Cleanup(r.teardownPortCapture)
	if _, err := run("iptables", "-t", "mangle", "-F", tunCaptureChain); err != nil {
		t.Fatalf("flush %s: %v", tunCaptureChain, err)
	}
	for _, rule := range r.captureChainRules([]string{routed}, nil) {
		if _, err := run(append([]string{"iptables", "-t", "mangle", "-A", tunCaptureChain}, rule.spec...)...); err != nil {
			t.Fatalf("add %v to %s: %v", rule.spec, tunCaptureChain, err)
		}
	}

	const relay = "203.0.113.10:443"
	for _, c := range []struct {
		name  string
		mark  uint32
		steer bool
		why   string
	}{
		{"b4's connection for the Telegram set", config.TelegramSetDialMark, true, "a routing set's addresses stay out of the capture, but b4's own Telegram connections leave by the main route whatever a routing set holds, so the Telegram set's strategy still needs them captured"},
		{"b4's own connection that sets may process", config.SelfDialMark, false, "only the Telegram set's connections are exempt from a routing set's return"},
		{"a connection whose mark holds the Telegram set's bits among others", config.TelegramSetDialMark | 0x800000, false, "only b4's own Telegram mark, modulo the bits other rules add on the way, is exempt; a mark from another service that merely contains both bits keeps the return"},
		{"an unmarked connection", 0, false, "the control: a routing set's address is kept out of the capture"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := run("iptables", "-t", "mangle", "-Z", tunCaptureChain); err != nil {
				t.Fatalf("zero the counters of %s: %v", tunCaptureChain, err)
			}
			capBefore, wanBefore := netnsTxPackets(t, "b4cap0"), netnsTxPackets(t, "b4wan0")
			netnsMarkedSYN(t, c.mark, relay)
			_, steered := captureChainHits(t)
			returned := routingSetReturns(t, routed)
			captured := netnsTxPackets(t, "b4cap0") - capBefore
			uplink := netnsTxPackets(t, "b4wan0") - wanBefore

			if c.steer {
				if steered == 0 || returned != 0 || captured == 0 || uplink != 0 {
					t.Errorf("SYN marked 0x%x: steered %d, returned for the routing set %d, left by the capture device %d and the uplink %d, want the capture device: %s", c.mark, steered, returned, captured, uplink, c.why)
				}
				return
			}
			if returned == 0 || steered != 0 || captured != 0 || uplink == 0 {
				t.Errorf("SYN marked 0x%x: steered %d, returned for the routing set %d, left by the capture device %d and the uplink %d, want the return and the uplink: %s", c.mark, steered, returned, captured, uplink, c.why)
			}
		})
	}
}

func TestNetnsARelayDialForSetsIsSteeredIntoTheCaptureAndANoDPIOneIsNot(t *testing.T) {
	tunNetnsRequire(t)
	netnsDummy(t, "b4wan0", "198.51.100.1/24")
	netnsIP(t, "route", "add", "default", "dev", "b4wan0")
	netnsDummy(t, "b4cap0", "")
	for _, link := range []string{"b4wan0", "b4cap0"} {
		netnsIP(t, "link", "set", link, "arp", "off")
		if err := os.WriteFile("/proc/sys/net/ipv6/conf/"+link+"/disable_ipv6", []byte("1"), 0o644); err != nil && !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("disable IPv6 on %s, whose router solicitations would count as frames the link sent: %v", link, err)
		}
	}

	r := &routeManager{
		tunName:      "b4cap0",
		captureTable: 96,
		quit:         make(chan struct{}),
		mark:         0x8000,
		tcpPorts:     []string{"443"},
		tcpLimit:     19,
		udpLimit:     8,
		skipTables:   true,
	}
	r.resolveCaptureMode()
	if err := r.setupPortCapture(""); err != nil {
		t.Fatalf("setupPortCapture: %v", err)
	}
	t.Cleanup(r.teardownPortCapture)

	const relay = "203.0.113.10:443"
	for _, c := range []struct {
		name  string
		mark  uint32
		steer bool
		why   string
	}{
		{"a relay of the Telegram set", config.TelegramSetDialMark, true, "with a Telegram set without routing b4 dials the relays with 0x4040000, and in TUN mode the set's strategy needs the capture"},
		{"b4's own connection that sets may process", config.SelfDialMark, true, "0x40000 lets a set covering the destination process b4's own connection, and in TUN mode that needs the capture"},
		{"a relay without DPI", config.SelfDialRelayMark, false, "0x240000 keeps b4's Cloudflare relays away from every set while no Telegram set applies, so the capture must return it before the steer rules"},
		{"an unmarked connection", 0, true, "the control: ordinary traffic to a captured port is steered"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := run("iptables", "-t", "mangle", "-Z", tunCaptureChain); err != nil {
				t.Fatalf("zero the counters of %s: %v", tunCaptureChain, err)
			}
			capBefore, wanBefore := netnsTxPackets(t, "b4cap0"), netnsTxPackets(t, "b4wan0")
			netnsMarkedSYN(t, c.mark, relay)
			skipped, steered := captureChainHits(t)
			captured := netnsTxPackets(t, "b4cap0") - capBefore
			uplink := netnsTxPackets(t, "b4wan0") - wanBefore

			if c.steer {
				if steered == 0 || skipped != 0 {
					t.Errorf("SYN marked 0x%x: the steer rule counted %d packet(s) and the relay return %d, want a steer and no return: %s", c.mark, steered, skipped, c.why)
				}
				if captured == 0 || uplink != 0 {
					t.Errorf("SYN marked 0x%x left by the capture device %d time(s) and by the uplink %d time(s), want the capture device: %s", c.mark, captured, uplink, c.why)
				}
				return
			}
			if skipped == 0 || steered != 0 {
				t.Errorf("SYN marked 0x%x: the relay return counted %d packet(s) and the steer rule %d, want the return only: %s", c.mark, skipped, steered, c.why)
			}
			if captured != 0 || uplink == 0 {
				t.Errorf("SYN marked 0x%x left by the capture device %d time(s) and by the uplink %d time(s), want the uplink: %s", c.mark, captured, uplink, c.why)
			}
		})
	}
}
