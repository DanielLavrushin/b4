package tables

import (
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

const (
	ctdirMark  = 0x5179
	ctdirTable = 2291
	ctdirChain = "b4r_ctdir_pre"
	ctdirCount = "B4_CTDIR_CNT"
	ctdirSet   = "b4r_ctdir_v4"
	ctdirEgres = "b4cd1"
	ctdirDest  = "198.51.100.9"
)

func ctdirRun(t *testing.T, args ...string) {
	t.Helper()
	if out, err := exec.Command(args[0], args[1:]...).CombinedOutput(); err != nil {
		t.Fatalf("%s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
}

func ctdirTry(args ...string) {
	_ = exec.Command(args[0], args[1:]...).Run()
}

func ctdirReplyMarkedCount(t *testing.T) int {
	t.Helper()
	out, err := exec.Command("iptables", "-t", "mangle", "-L", ctdirCount, "-n", "-v", "-x").CombinedOutput()
	if err != nil {
		t.Fatalf("read counters: %v: %s", err, out)
	}
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(lines) < 3 {
		t.Fatalf("counter chain has no rules:\n%s", out)
	}
	fields := strings.Fields(lines[len(lines)-1])
	n, err := strconv.Atoi(fields[0])
	if err != nil {
		t.Fatalf("unparsable counter %q", lines[len(lines)-1])
	}
	return n
}

func ctdirSetup(t *testing.T, directionScoped bool) {
	t.Helper()

	ctdirRun(t, "ip", "link", "add", ctdirEgres, "type", "dummy")
	ctdirRun(t, "ip", "link", "set", ctdirEgres, "up")
	ctdirRun(t, "ip", "addr", "add", "10.9.9.1/24", "dev", ctdirEgres)
	ctdirRun(t, "ipset", "create", ctdirSet, "hash:net", "family", "inet", "-exist")
	ctdirRun(t, "ipset", "add", ctdirSet, ctdirDest, "-exist")
	ctdirRun(t, "ip", "route", "add", "default", "dev", ctdirEgres, "table", strconv.Itoa(ctdirTable))
	ctdirRun(t, "ip", "rule", "add", "fwmark", fmt.Sprintf("0x%x/0x%x", ctdirMark, routeSetMarkMask),
		"lookup", strconv.Itoa(ctdirTable), "priority", "10229")

	mask := fmt.Sprintf("0x%x", routeSetMarkMask)
	ctdirRun(t, "iptables", "-t", "mangle", "-N", ctdirChain)
	ctdirRun(t, "iptables", "-t", "mangle", "-A", "PREROUTING", "-j", ctdirChain)

	restore := []string{"iptables", "-t", "mangle", "-A", ctdirChain}
	if directionScoped {
		restore = append(restore, "-m", "conntrack", "--ctdir", "ORIGINAL")
	}
	restore = append(restore, "-m", "conntrack", "!", "--ctstate", "NEW",
		"-j", "CONNMARK", "--restore-mark", "--nfmask", mask, "--ctmask", mask)
	ctdirRun(t, restore...)

	ctdirRun(t, "iptables", "-t", "mangle", "-A", ctdirChain,
		"-m", "conntrack", "--ctstate", "NEW", "-m", "set", "--match-set", ctdirSet, "dst",
		"-j", "MARK", "--set-xmark", fmt.Sprintf("0x%x/%s", ctdirMark, mask))
	ctdirRun(t, "iptables", "-t", "mangle", "-A", ctdirChain,
		"-m", "conntrack", "--ctstate", "NEW", "-m", "set", "--match-set", ctdirSet, "dst",
		"-j", "CONNMARK", "--save-mark", "--nfmask", mask, "--ctmask", mask)

	ctdirRun(t, "iptables", "-t", "mangle", "-N", ctdirCount)
	ctdirRun(t, "iptables", "-t", "mangle", "-A", "PREROUTING", "-j", ctdirCount)
	ctdirRun(t, "iptables", "-t", "mangle", "-A", ctdirCount,
		"-m", "conntrack", "--ctdir", "REPLY", "-m", "mark", "--mark",
		fmt.Sprintf("0x%x/%s", ctdirMark, mask))

	t.Cleanup(func() {
		ctdirTry("iptables", "-t", "mangle", "-D", "PREROUTING", "-j", ctdirCount)
		ctdirTry("iptables", "-t", "mangle", "-D", "PREROUTING", "-j", ctdirChain)
		for _, c := range []string{ctdirCount, ctdirChain} {
			ctdirTry("iptables", "-t", "mangle", "-F", c)
			ctdirTry("iptables", "-t", "mangle", "-X", c)
		}
		ctdirTry("ip", "rule", "del", "fwmark", fmt.Sprintf("0x%x/0x%x", ctdirMark, routeSetMarkMask),
			"lookup", strconv.Itoa(ctdirTable))
		ctdirTry("ip", "route", "flush", "table", strconv.Itoa(ctdirTable))
		ctdirTry("ipset", "destroy", ctdirSet)
		ctdirTry("ip", "link", "del", ctdirEgres)
	})
}

func ctdirDriveFlow(t *testing.T) {
	t.Helper()
	for i := 0; i < 4; i++ {
		ctdirTry("ping", "-c", "1", "-W", "1", "-I", "10.9.9.1", ctdirDest)
	}
}

func TestNetnsRestoreMarkNeverClaimsAReply(t *testing.T) {
	netnsRequire(t)
	ctdirSetup(t, true)
	ctdirDriveFlow(t)

	if n := ctdirReplyMarkedCount(t); n != 0 {
		t.Fatalf("%d reply packet(s) carried the routing mark. A marked reply is sent back out the set's "+
			"egress interface instead of to the client, which loops the router until it stops forwarding", n)
	}
}

func TestNetnsRestoreMarkWithoutDirectionScopeClaimsReplies(t *testing.T) {
	netnsRequire(t)
	if testing.Short() {
		t.Skip("this test deliberately builds the broken rule set")
	}
	ctdirSetup(t, false)
	ctdirDriveFlow(t)

	t.Logf("without --ctdir ORIGINAL the reply counter reached %d; the guard in the sibling test is what keeps it at 0",
		ctdirReplyMarkedCount(t))
}
