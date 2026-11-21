package tun

import (
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
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
		{"a relay sets may process", config.SelfDialMark, true, "with cfproxy_dpi or cfworker_dpi on, b4 dials the relay with 0x40000 so that a set covering it can process it, and in TUN mode that needs the capture"},
		{"a relay without DPI", config.SelfDialRelayMark, false, "0x240000 keeps b4's Cloudflare relays away from every set, so the capture must return it before the steer rules"},
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
