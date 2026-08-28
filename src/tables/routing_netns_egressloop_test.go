package tables

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	loopMark   = 0x5179
	loopTable  = 2292
	loopChain  = "b4r_loop_pre"
	loopCount  = "B4_LOOP_CNT"
	loopSet    = "b4r_loop_v4"
	loopEgress = "b4lp0"
	loopDest   = "203.0.113.5"
)

func loopRun(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

func loopTry(args ...string) { _ = exec.Command(args[0], args[1:]...).Run() }

func loopSpawnNetns(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("unshare", "-n", "sleep", "60")
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot spawn a peer namespace: %v", err)
	}
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	})
	time.Sleep(400 * time.Millisecond)
	return cmd.Process.Pid
}

func loopNsExec(t *testing.T, pid int, script string) {
	t.Helper()
	out, err := exec.Command("nsenter", "-t", strconv.Itoa(pid), "-n", "sh", "-c", script).CombinedOutput()
	if err != nil {
		t.Fatalf("nsenter %d: %v: %s", pid, err, strings.TrimSpace(string(out)))
	}
}

func loopReentries(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("iptables", "-t", "mangle", "-L", loopCount, "-n", "-v", "-x").CombinedOutput()
	if err != nil {
		t.Fatalf("read counters: %v: %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		t.Fatalf("counter chain empty:\n%s", out)
	}
	n, err := strconv.Atoi(strings.Fields(lines[2])[0])
	if err != nil {
		t.Fatalf("unparsable counter %q", lines[2])
	}
	return n
}

func loopSetup(t *testing.T, guarded bool) {
	t.Helper()

	clientPid := loopSpawnNetns(t)
	peerPid := loopSpawnNetns(t)

	loopRun(t, "ip", "link", "add", "b4lpc", "type", "veth", "peer", "name", "b4lpc-r")
	loopRun(t, "ip", "link", "set", "b4lpc", "netns", strconv.Itoa(clientPid))
	loopRun(t, "ip", "link", "add", "b4lp-p", "type", "veth", "peer", "name", loopEgress)
	loopRun(t, "ip", "link", "set", "b4lp-p", "netns", strconv.Itoa(peerPid))

	loopRun(t, "ip", "addr", "add", "192.168.77.1/24", "dev", "b4lpc-r")
	loopRun(t, "ip", "link", "set", "b4lpc-r", "up")
	loopRun(t, "ip", "addr", "add", "192.168.78.1/24", "dev", loopEgress)
	loopRun(t, "ip", "link", "set", loopEgress, "up")

	loopNsExec(t, clientPid, "ip link set lo up; ip addr add 192.168.77.100/24 dev b4lpc; ip link set b4lpc up; ip route add default via 192.168.77.1")
	loopNsExec(t, peerPid, "ip link set lo up; ip addr add 192.168.78.2/24 dev b4lp-p; ip link set b4lp-p up; ip route add default via 192.168.78.1; echo 1 > /proc/sys/net/ipv4/ip_forward")

	loopTry("sh", "-c", "echo 1 > /proc/sys/net/ipv4/ip_forward")
	for _, i := range []string{"all", "b4lpc-r", loopEgress} {
		loopTry("sh", "-c", fmt.Sprintf("echo 2 > /proc/sys/net/ipv4/conf/%s/rp_filter", i))
	}

	loopRun(t, "ipset", "create", loopSet, "hash:net", "family", "inet", "-exist")
	loopRun(t, "ipset", "add", loopSet, loopDest, "-exist")
	loopRun(t, "ip", "route", "add", "default", "via", "192.168.78.2", "dev", loopEgress, "table", strconv.Itoa(loopTable))
	loopRun(t, "ip", "rule", "add", "fwmark", fmt.Sprintf("0x%x/0x%x", loopMark, routeSetMarkMask),
		"lookup", strconv.Itoa(loopTable), "priority", "10230")

	loopRun(t, "iptables", "-t", "mangle", "-N", loopChain)
	loopRun(t, "iptables", "-t", "mangle", "-A", "PREROUTING", "-j", loopChain)
	if guarded {
		loopRun(t, "iptables", "-t", "mangle", "-A", loopChain, "-i", loopEgress, "-j", "RETURN")
	}
	loopRun(t, "iptables", "-t", "mangle", "-A", loopChain,
		"-m", "set", "--match-set", loopSet, "dst",
		"-j", "MARK", "--set-xmark", fmt.Sprintf("0x%x/0x%x", loopMark, routeSetMarkMask))

	loopRun(t, "iptables", "-t", "mangle", "-N", loopCount)
	loopRun(t, "iptables", "-t", "mangle", "-A", "PREROUTING", "-j", loopCount)
	loopRun(t, "iptables", "-t", "mangle", "-A", loopCount, "-i", loopEgress, "-d", loopDest)

	t.Cleanup(func() {
		loopTry("iptables", "-t", "mangle", "-D", "PREROUTING", "-j", loopCount)
		loopTry("iptables", "-t", "mangle", "-D", "PREROUTING", "-j", loopChain)
		for _, c := range []string{loopCount, loopChain} {
			loopTry("iptables", "-t", "mangle", "-F", c)
			loopTry("iptables", "-t", "mangle", "-X", c)
		}
		loopTry("ip", "rule", "del", "fwmark", fmt.Sprintf("0x%x/0x%x", loopMark, routeSetMarkMask),
			"lookup", strconv.Itoa(loopTable))
		loopTry("ip", "route", "flush", "table", strconv.Itoa(loopTable))
		loopTry("ipset", "destroy", loopSet)
		loopTry("ip", "link", "del", loopEgress)
		loopTry("ip", "link", "del", "b4lpc-r")
	})

	_ = exec.Command("nsenter", "-t", strconv.Itoa(clientPid), "-n",
		"ping", "-c", "1", "-W", "1", loopDest).Run()
	time.Sleep(2 * time.Second)
}

func TestNetnsEgressLoopGuardStopsThePacketComingBack(t *testing.T) {
	netnsRequire(t)
	loopSetup(t, true)

	if n := loopReentries(t); n > 1 {
		t.Fatalf("one client packet re-entered the set's egress %d times. Without a guard on traffic arriving "+
			"from the egress interface, every packet a proxy re-emits is marked again and sent straight back to "+
			"the same proxy, which saturates the router", n)
	}
}

func TestNetnsWithoutTheGuardTheSamePacketCyclesManyTimes(t *testing.T) {
	netnsRequire(t)
	loopSetup(t, false)

	n := loopReentries(t)
	t.Logf("without the egress guard one packet cycled %d times; the sibling test is what keeps that at 1", n)
	if n <= 1 {
		t.Skip("this kernel did not reproduce the loop, so the guard test is the only meaningful one here")
	}
}
