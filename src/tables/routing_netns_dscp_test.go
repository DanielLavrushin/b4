package tables

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	b4engine "github.com/daniellavrushin/b4/engine"
	"golang.org/x/sys/unix"
)

const (
	dscpTestValue    = 7
	dscpTestCounters = "b4dscpcnt"
	dscpClientIf     = "b4dc"
	dscpClientPeer   = "b4dc-r"
	dscpClientIP     = "192.168.79.100"
	dscpClientGW     = "192.168.79.1"
)

func dscpNetnsConfig(engine string, stamp bool) *config.Config {
	cfg := config.NewConfig()
	cfg.Queue.IPv4Enabled = true
	cfg.Queue.IPv6Enabled = false
	cfg.Queue.Threads = 1
	cfg.Queue.Mark = 0x8000
	cfg.System.Tables.Engine = engine
	cfg.System.Tables.SkipSetup = false
	cfg.System.Tables.DSCP = config.DSCPConfig{Enabled: stamp, Value: dscpTestValue}

	set := config.NewSetConfig()
	set.Id = "dscp-netns"
	set.Name = "dscp"
	set.Enabled = true
	set.TCP.DPortFilter = "443"
	set.UDP.DPortFilter = "443"
	set.Targets.SNIDomains = []string{"dscp.example"}
	cfg.Sets = []*config.SetConfig{&set}
	return &cfg
}

func dscpNftIn(t *testing.T, pid int, script string) {
	t.Helper()
	cmd := exec.Command("nft", "-f", "-")
	if pid != 0 {
		cmd = exec.Command("nsenter", "-t", strconv.Itoa(pid), "-n", "nft", "-f", "-")
	}
	cmd.Stdin = strings.NewReader(script)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("nft script in netns %d: %v: %s\n%s", pid, err, strings.TrimSpace(string(out)), script)
	}
}

func dscpCounter(t *testing.T, pid int, name string) int {
	t.Helper()
	args := []string{"nft", "list", "counter", "inet", dscpTestCounters, name}
	if pid != 0 {
		args = append([]string{"nsenter", "-t", strconv.Itoa(pid), "-n"}, args...)
	}
	out, err := exec.Command(args[0], args[1:]...).CombinedOutput()
	if err != nil {
		t.Fatalf("read counter %s: %v: %s", name, err, out)
	}
	fields := strings.Fields(string(out))
	for i, f := range fields {
		if f == "packets" && i+1 < len(fields) {
			n, err := strconv.Atoi(fields[i+1])
			if err != nil {
				t.Fatalf("counter %s: %q is not a number", name, fields[i+1])
			}
			return n
		}
	}
	t.Fatalf("counter %s not found in:\n%s", name, out)
	return 0
}

func dscpWireCounters(t *testing.T) {
	t.Helper()
	names := []string{"udp_ok", "udp_all", "raw_ok", "raw_all", "cm_ok", "cm_all", "fwd_tcp_ok", "fwd_tcp_all", "fwd_icmp_ok", "fwd_icmp_all"}
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\ndelete table inet %s\nadd table inet %s\n", dscpTestCounters, dscpTestCounters, dscpTestCounters)
	for _, n := range names {
		fmt.Fprintf(&b, "add counter inet %s %s\n", dscpTestCounters, n)
	}
	fmt.Fprintf(&b, "add chain inet %s in { type filter hook prerouting priority -300 ; policy accept ; }\n", dscpTestCounters)
	for _, r := range []string{
		fmt.Sprintf(`iifname "%sp" udp dport 443 ip dscp %d ip ecn ect0 counter name "udp_ok"`, netnsPrimary, dscpTestValue),
		fmt.Sprintf(`iifname "%sp" udp dport 443 counter name "udp_all"`, netnsPrimary),
		fmt.Sprintf(`iifname "%sp" ip saddr %s tcp dport 443 ip dscp %d counter name "raw_ok"`, netnsPrimary, netnsPrimaryIP, dscpTestValue),
		fmt.Sprintf(`iifname "%sp" ip saddr %s tcp dport 443 counter name "raw_all"`, netnsPrimary, netnsPrimaryIP),
		fmt.Sprintf(`iifname "%sp" ip saddr %s tcp dport 9443 ip dscp %d counter name "cm_ok"`, netnsPrimary, netnsPrimaryIP, dscpTestValue),
		fmt.Sprintf(`iifname "%sp" ip saddr %s tcp dport 9443 counter name "cm_all"`, netnsPrimary, netnsPrimaryIP),
		fmt.Sprintf(`iifname "%sp" ip saddr %s tcp dport 443 ip dscp %d counter name "fwd_tcp_ok"`, netnsPrimary, dscpClientIP, dscpTestValue),
		fmt.Sprintf(`iifname "%sp" ip saddr %s tcp dport 443 counter name "fwd_tcp_all"`, netnsPrimary, dscpClientIP),
		fmt.Sprintf(`iifname "%sp" icmp type echo-request ip dscp %d counter name "fwd_icmp_ok"`, netnsPrimary, dscpTestValue),
		fmt.Sprintf(`iifname "%sp" icmp type echo-request counter name "fwd_icmp_all"`, netnsPrimary),
		fmt.Sprintf(`iifname "%sp" drop`, netnsPrimary),
	} {
		fmt.Fprintf(&b, "add rule inet %s in %s\n", dscpTestCounters, r)
	}
	dscpNftIn(t, 0, b.String())
	t.Cleanup(func() { _, _ = run("nft", "delete", "table", "inet", dscpTestCounters) })
}

func dscpClientCounters(t *testing.T, pid int) {
	t.Helper()
	var b strings.Builder
	fmt.Fprintf(&b, "add table inet %s\n", dscpTestCounters)
	for _, n := range []string{"reply_ok", "reply_all", "req_ok", "req_all"} {
		fmt.Fprintf(&b, "add counter inet %s %s\n", dscpTestCounters, n)
	}
	fmt.Fprintf(&b, "add chain inet %s in { type filter hook prerouting priority -300 ; policy accept ; }\n", dscpTestCounters)
	for _, r := range []string{
		fmt.Sprintf(`iifname "%s" icmp type echo-reply ip dscp %d counter name "reply_ok"`, dscpClientIf, dscpTestValue),
		fmt.Sprintf(`iifname "%s" icmp type echo-reply counter name "reply_all"`, dscpClientIf),
		fmt.Sprintf(`iifname "%s" icmp type echo-request ip dscp %d counter name "req_ok"`, dscpClientIf, dscpTestValue),
		fmt.Sprintf(`iifname "%s" icmp type echo-request counter name "req_all"`, dscpClientIf),
	} {
		fmt.Fprintf(&b, "add rule inet %s in %s\n", dscpTestCounters, r)
	}
	dscpNftIn(t, pid, b.String())
}

func dscpSetupClient(t *testing.T) int {
	t.Helper()
	pid := loopSpawnNetns(t)
	loopTry("ip", "link", "del", dscpClientPeer)
	loopRun(t, "ip", "link", "add", dscpClientIf, "type", "veth", "peer", "name", dscpClientPeer)
	loopRun(t, "ip", "link", "set", dscpClientIf, "netns", strconv.Itoa(pid))
	loopRun(t, "ip", "addr", "add", dscpClientGW+"/24", "dev", dscpClientPeer)
	loopRun(t, "ip", "link", "set", dscpClientPeer, "up")
	loopNsExec(t, pid, fmt.Sprintf("ip link set lo up; ip addr add %s/24 dev %s; ip link set %s up; ip route add default via %s",
		dscpClientIP, dscpClientIf, dscpClientIf, dscpClientGW))
	loopTry("sh", "-c", "echo 1 > /proc/sys/net/ipv4/ip_forward")
	t.Cleanup(func() { loopTry("ip", "link", "del", dscpClientPeer) })
	time.Sleep(300 * time.Millisecond)
	return pid
}

func dscpSendLocalUDP(t *testing.T) {
	t.Helper()
	conn, err := net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(netnsTarget), Port: 443})
	if err != nil {
		t.Fatalf("dial udp: %v", err)
	}
	defer conn.Close()
	raw, err := conn.SyscallConn()
	if err != nil {
		t.Fatalf("raw conn: %v", err)
	}
	var serr error
	if err := raw.Control(func(fd uintptr) {
		serr = unix.SetsockoptInt(int(fd), unix.IPPROTO_IP, unix.IP_TOS, 0x02)
	}); err != nil || serr != nil {
		t.Fatalf("set IP_TOS: %v %v", err, serr)
	}
	if _, err := conn.Write([]byte("dscp")); err != nil {
		t.Fatalf("udp write: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
}

func dscpDialFromNetns(t *testing.T, pid int, addr string) {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		f, err := os.Open(fmt.Sprintf("/proc/%d/ns/net", pid))
		if err != nil {
			done <- err
			return
		}
		defer f.Close()
		if err := unix.Setns(int(f.Fd()), unix.CLONE_NEWNET); err != nil {
			done <- err
			return
		}
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_STREAM|unix.SOCK_NONBLOCK, 0)
		if err != nil {
			done <- err
			return
		}
		defer unix.Close(fd)
		host, port, _ := net.SplitHostPort(addr)
		p, _ := strconv.Atoi(port)
		sa := &unix.SockaddrInet4{Port: p}
		copy(sa.Addr[:], net.ParseIP(host).To4())
		if err := unix.Connect(fd, sa); err != nil && err != syscall.EINPROGRESS {
			done <- err
			return
		}
		time.Sleep(300 * time.Millisecond)
		done <- nil
	}()
	if err := <-done; err != nil {
		t.Fatalf("connect from netns %d: %v", pid, err)
	}
}

type dscpWire struct{ ok, all int }

func dscpRead(t *testing.T, pid int, name string) dscpWire {
	return dscpWire{ok: dscpCounter(t, pid, name+"_ok"), all: dscpCounter(t, pid, name+"_all")}
}

func dscpExpectStamped(t *testing.T, what string, before, after dscpWire) {
	t.Helper()
	sent := after.all - before.all
	stamped := after.ok - before.ok
	if sent == 0 {
		t.Errorf("%s: no packet reached the far end of the link, so the test proves nothing", what)
		return
	}
	if stamped != sent {
		t.Errorf("%s: %d of %d packets carried DSCP %d", what, stamped, sent, dscpTestValue)
	}
}

func dscpExpectPlain(t *testing.T, what string, before, after dscpWire) {
	t.Helper()
	sent := after.all - before.all
	if sent == 0 {
		t.Errorf("%s: no packet reached the far end of the link, so the test proves nothing", what)
		return
	}
	if stamped := after.ok - before.ok; stamped != 0 {
		t.Errorf("%s: %d of %d packets carried DSCP %d although they must not", what, stamped, sent, dscpTestValue)
	}
}

func dscpIptOrder(t *testing.T) (jump, capture int) {
	t.Helper()
	out := netnsRun(t, "iptables", "-w", "-t", "mangle", "-L", "POSTROUTING", "-n", "--line-numbers")
	jump, capture, _ = dscpJumpPlacement(out)
	return jump, capture
}

func dscpAssertSeated(t *testing.T, why string) {
	t.Helper()
	jump, capture := dscpIptOrder(t)
	if jump == 0 || capture == 0 || jump > capture {
		out := netnsRun(t, "iptables", "-w", "-t", "mangle", "-S", "POSTROUTING")
		t.Fatalf("%s: the jump to %s must sit above the capture jump (dscp=%d, capture=%d):\n%s", why, dscpChainName, jump, capture, out)
	}
}

func dscpChainStampCount(t *testing.T) int {
	t.Helper()
	out := netnsRun(t, "iptables", "-w", "-t", "mangle", "-L", dscpChainName, "-v", "-x", "-n")
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) > 2 && f[2] == "DSCP" {
			n, err := strconv.Atoi(f[0])
			if err != nil {
				t.Fatalf("unparsable counter in %q", line)
			}
			return n
		}
	}
	t.Fatalf("no DSCP rule in %s:\n%s", dscpChainName, out)
	return 0
}

func dscpJumpCount(t *testing.T) int {
	t.Helper()
	out := netnsRun(t, "iptables", "-w", "-t", "mangle", "-L", "POSTROUTING", "-v", "-x", "-n")
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) > 2 && f[2] == dscpChainName {
			n, err := strconv.Atoi(f[0])
			if err != nil {
				t.Fatalf("unparsable counter in %q", line)
			}
			return n
		}
	}
	t.Fatalf("no jump to %s in mangle POSTROUTING:\n%s", dscpChainName, out)
	return 0
}

func dscpAssertGone(t *testing.T, why string) {
	t.Helper()
	if _, err := run("iptables", "-w", "-t", "mangle", "-S", dscpChainName); err == nil {
		t.Errorf("%s: the %s chain is still there", why, dscpChainName)
	}
	if out, _ := run("iptables", "-w", "-t", "mangle", "-S", "POSTROUTING"); strings.Contains(out, dscpChainName) {
		t.Errorf("%s: mangle POSTROUTING still jumps to %s:\n%s", why, dscpChainName, out)
	}
	if hasBinary("nft") && dscpNftTablePresent() {
		t.Errorf("%s: the %s table is still there", why, dscpNftTable)
	}
}

func TestNetnsDSCPStampReachesTheWire(t *testing.T) {
	netnsRequireNft(t)
	netnsSetupLinks(t)
	client := dscpSetupClient(t)
	dscpWireCounters(t)
	dscpClientCounters(t, client)

	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			routeEngine = nil
			defer func() { routeEngine = nil }()

			cfg := dscpNetnsConfig(engine, true)
			stop := netnsStartQueueListener(t, uint16(cfg.Queue.StartNum))
			defer stop()
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			defer func() { _ = ClearRules(cfg) }()
			if dscpApplied.Load() == nil {
				t.Fatalf("the stamp was not installed")
			}

			before := dscpRead(t, 0, "udp")
			dscpSendLocalUDP(t)
			dscpExpectStamped(t, "local UDP to a queued port, sent with ECT(0)", before, dscpRead(t, 0, "udp"))

			before = dscpRead(t, 0, "raw")
			netnsSendMarked(t, uint32(cfg.Queue.Mark), 40443, 443)
			dscpExpectStamped(t, "a raw packet injected with the queue mark", before, dscpRead(t, 0, "raw"))

			before = dscpRead(t, 0, "cm")
			netnsSendMarked(t, uint32(cfg.Queue.Mark)|b4engine.ClientMark, 40444, 9443)
			dscpExpectPlain(t, "a raw packet b4 sends toward a client (client mark)", before, dscpRead(t, 0, "cm"))

			before = dscpRead(t, 0, "fwd_tcp")
			dscpDialFromNetns(t, client, net.JoinHostPort(netnsTarget, "443"))
			dscpExpectStamped(t, "a forwarded TCP SYN to a queued port", before, dscpRead(t, 0, "fwd_tcp"))

			before = dscpRead(t, 0, "fwd_icmp")
			loopNsExec(t, client, "ping -c 1 -W 1 "+netnsTarget+" >/dev/null 2>&1 || true")
			dscpExpectStamped(t, "a forwarded ICMP echo request", before, dscpRead(t, 0, "fwd_icmp"))

			reqBefore, replyBefore := dscpRead(t, client, "req"), dscpRead(t, client, "reply")
			loopRun(t, "ping", "-c", "1", "-W", "1", dscpClientIP)
			loopNsExec(t, client, "ping -c 1 -W 1 "+dscpClientGW+" >/dev/null")
			dscpExpectStamped(t, "an echo request this host starts towards the client", reqBefore, dscpRead(t, client, "req"))
			dscpExpectPlain(t, "this host's echo reply to a client that pinged it", replyBefore, dscpRead(t, client, "reply"))

			if engine == backendIPTables {
				dscpAssertSeated(t, "after the first AddRules")

				stampedBefore := dscpChainStampCount(t)
				if stampedBefore == 0 {
					t.Fatalf("the stamp rule counted no packets, so the refresh check proves nothing")
				}
				jumpedBefore := dscpJumpCount(t)
				if err := RefreshRules(cfg); err != nil {
					t.Fatalf("RefreshRules: %v", err)
				}
				if got := dscpChainStampCount(t); got < stampedBefore {
					t.Errorf("a refresh with unchanged DSCP settings rebuilt the stamp chain (counter %d -> %d)", stampedBefore, got)
				}
				if got := dscpJumpCount(t); got < jumpedBefore {
					t.Errorf("a refresh with unchanged DSCP settings replaced the jump to %s (counter %d -> %d), so the capture jump sat above the stamp until the re-seat", dscpChainName, jumpedBefore, got)
				}
				dscpAssertSeated(t, "after RefreshRules")

				netnsRun(t, "iptables", "-w", "-t", "mangle", "-D", "POSTROUTING", "-j", dscpCaptureChain)
				if err := AddRules(cfg); err != nil {
					t.Fatalf("AddRules after the capture jump went missing: %v", err)
				}
				dscpAssertSeated(t, "after the capture jump was put back on top")

				netnsRun(t, "iptables", "-w", "-t", "mangle", "-D", "POSTROUTING", "-j", dscpChainName)
				netnsRun(t, "iptables", "-w", "-t", "mangle", "-A", "POSTROUTING", "-j", dscpChainName)
				before = dscpRead(t, 0, "udp")
				dscpSendLocalUDP(t)
				dscpExpectPlain(t, "control: local UDP while the stamp sits below the capture jump", before, dscpRead(t, 0, "udp"))

				if !ensureDSCPLocked(cfg, false) {
					t.Fatalf("the monitor check did not notice the stamp below the capture jump")
				}
				dscpAssertSeated(t, "after the monitor check")
				if ensureDSCPLocked(cfg, false) {
					t.Errorf("a second monitor check restored the stamp again although it was in place")
				}
				before = dscpRead(t, 0, "udp")
				dscpSendLocalUDP(t)
				dscpExpectStamped(t, "local UDP after the monitor put the stamp back", before, dscpRead(t, 0, "udp"))
			} else {
				netnsRun(t, "nft", "delete", "table", "inet", dscpNftTable)
				if !ensureDSCPLocked(cfg, false) {
					t.Fatalf("the monitor check did not notice the missing %s table", dscpNftTable)
				}
				if ensureDSCPLocked(cfg, false) {
					t.Errorf("a second monitor check restored the table again although it was in place")
				}
				before = dscpRead(t, 0, "udp")
				dscpSendLocalUDP(t)
				dscpExpectStamped(t, "local UDP after the monitor put the table back", before, dscpRead(t, 0, "udp"))
			}

			if err := ClearRules(cfg); err != nil {
				t.Fatalf("ClearRules: %v", err)
			}
			dscpAssertGone(t, "after ClearRules")
			if dscpApplied.Load() != nil {
				t.Errorf("ClearRules left the stamp recorded as applied, so the monitor would put it back")
			}
		})
	}
}

func TestNetnsDSCPOffEmitsNothing(t *testing.T) {
	netnsRequireNft(t)
	netnsSetupLinks(t)
	dscpWireCounters(t)

	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			routeEngine = nil
			defer func() { routeEngine = nil }()

			cfg := dscpNetnsConfig(engine, false)
			stop := netnsStartQueueListener(t, uint16(cfg.Queue.StartNum))
			defer stop()
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			defer func() { _ = ClearRules(cfg) }()

			dscpAssertGone(t, "with the stamp off")
			before := dscpRead(t, 0, "udp")
			dscpSendLocalUDP(t)
			dscpExpectPlain(t, "local UDP with the stamp off", before, dscpRead(t, 0, "udp"))
		})
	}
}

func TestNetnsDSCPOnlyFollowsTheSwitch(t *testing.T) {
	netnsRequireNft(t)
	netnsSetupLinks(t)

	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			on := dscpNetnsConfig(engine, true)
			on.System.Tables.DSCP.Interfaces = []string{netnsPrimary}
			if err := ApplyDSCPOnly(on); err != nil {
				t.Fatalf("ApplyDSCPOnly: %v", err)
			}
			defer ClearDSCPOnly(on)
			if dscpApplied.Load() == nil || !dscpIntact(dscpApplied.Load()) {
				t.Fatalf("the stamp is not in place after ApplyDSCPOnly")
			}

			changed := dscpNetnsConfig(engine, true)
			changed.System.Tables.DSCP.Value = 31
			if err := ApplyDSCPOnly(changed); err != nil {
				t.Fatalf("ApplyDSCPOnly with a new value: %v", err)
			}
			if engine == backendNFTables {
				out := netnsRun(t, "nft", "list", "chain", "inet", dscpNftTable, dscpNftChain)
				if strings.Contains(out, netnsPrimary) || strings.Count(out, "dscp set 0x1f") != 2 {
					t.Errorf("the new value and scope did not replace the old ones:\n%s", out)
				}
			} else {
				out := netnsRun(t, "iptables", "-w", "-t", "mangle", "-S", dscpChainName)
				if strings.Contains(out, netnsPrimary) || !strings.Contains(out, "--set-dscp 0x1f") || strings.Count(out, "-j DSCP") != 1 {
					t.Errorf("the new value and scope did not replace the old ones:\n%s", out)
				}
			}

			off := dscpNetnsConfig(engine, false)
			if err := ApplyDSCPOnly(off); err != nil {
				t.Fatalf("ApplyDSCPOnly with the stamp off: %v", err)
			}
			dscpAssertGone(t, "after the switch went off")
			if dscpApplied.Load() != nil {
				t.Errorf("the stamp is still recorded as applied after the switch went off")
			}
		})
	}
}
