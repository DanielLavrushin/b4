package tables

import (
	"net"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

const netnsExposePort = 18443

func netnsExposeListener(t *testing.T) {
	t.Helper()
	ln, err := net.Listen("tcp", net.JoinHostPort("0.0.0.0", "18443"))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				buf := make([]byte, 256)
				n, _ := c.Read(buf)
				_, _ = c.Write(append([]byte("pong:"), buf[:n]...))
			}(conn)
		}
	}()
}

func netnsExposeReachable(t *testing.T, dev *netnsDevice) bool {
	t.Helper()
	got := dev.probe(t, "tcp", netnsDevRouter, netnsExposePort, "ping")
	switch {
	case got == "reply=pong:ping":
		return true
	case strings.HasPrefix(got, "failed"):
		return false
	}
	t.Fatalf("unexpected probe result: %s", got)
	return false
}

var netnsExposedMTProto = []config.ExposedPort{{Service: config.ExposeMTProto, Port: netnsExposePort, V4: true, V6: true}}

func TestNetnsExposeOpensAPortThroughAnIptablesDropPolicy(t *testing.T) {
	netnsRequire(t)
	resetExposeState(t)
	dev := netnsStartDevice(t)
	netnsExposeListener(t)

	netnsRun(t, "iptables", "-w", "-P", "INPUT", "DROP")
	t.Cleanup(func() {
		ClearExposure()
		_, _ = run("iptables", "-w", "-P", "INPUT", "ACCEPT")
		_, _ = run("iptables", "-w", "-F", "INPUT")
	})

	if netnsExposeReachable(t, dev) {
		t.Fatal("the DROP policy must block the port before b4 exposes it")
	}

	SyncExposure(netnsExposedMTProto, nil, false)
	if !netnsExposeReachable(t, dev) {
		t.Fatalf("exposed port unreachable; INPUT:\n%s", netnsRun(t, "iptables", "-w", "-S", "INPUT"))
	}

	netnsRun(t, "iptables", "-w", "-F", "INPUT")
	if netnsExposeReachable(t, dev) {
		t.Fatal("flushing INPUT must drop the jump, as a firewall reload does")
	}
	exposeCheck()
	if !netnsExposeReachable(t, dev) {
		t.Fatalf("the check must restore the jump after the reload; INPUT:\n%s", netnsRun(t, "iptables", "-w", "-S", "INPUT"))
	}

	ClearExposure()
	if netnsExposeReachable(t, dev) {
		t.Fatal("clearing must close the port again")
	}
	if _, err := run("iptables", "-w", "-t", "filter", "-nL", exposeChain); err == nil {
		t.Fatalf("clearing must delete the %s chain", exposeChain)
	}
}

func TestNetnsExposeLeavesABanListInForce(t *testing.T) {
	netnsRequire(t)
	if !hasBinary("nft") {
		t.Skip("nft is not installed")
	}
	resetExposeState(t)
	dev := netnsStartDevice(t)
	netnsExposeListener(t)

	script := "table inet fw4 {\n\tchain input {\n\t\ttype filter hook input priority filter; policy drop;\n\t}\n}\n" +
		"table inet f2b-table {\n\tset banned {\n\t\ttype ipv4_addr\n\t\telements = { " + netnsDevIP + " }\n\t}\n" +
		"\tchain f2b-chain {\n\t\ttype filter hook input priority -1; policy accept;\n\t\tip saddr @banned reject\n\t}\n}\n"
	if _, err := runNftStdin(script); err != nil {
		t.Fatalf("create the firewall and the ban list: %v", err)
	}
	t.Cleanup(func() {
		ClearExposure()
		_, _ = run("nft", "delete", "table", "inet", "fw4")
		_, _ = run("nft", "delete", "table", "inet", "f2b-table")
	})

	SyncExposure(netnsExposedMTProto, nil, false)
	if netnsExposeReachable(t, dev) {
		t.Fatalf("a banned address must stay banned on an exposed port; f2b-chain:\n%s", netnsRun(t, "nft", "list", "chain", "inet", "f2b-table", "f2b-chain"))
	}

	netnsRun(t, "nft", "delete", "element", "inet", "f2b-table", "banned", "{", netnsDevIP, "}")
	if !netnsExposeReachable(t, dev) {
		t.Fatalf("once unbanned the device must get through; fw4 input:\n%s", netnsRun(t, "nft", "-a", "list", "chain", "inet", "fw4", "input"))
	}
}

func TestNetnsExposeOpensAPortThroughAnFw4StyleNftDrop(t *testing.T) {
	netnsRequire(t)
	if !hasBinary("nft") {
		t.Skip("nft is not installed")
	}
	resetExposeState(t)
	dev := netnsStartDevice(t)
	netnsExposeListener(t)

	if _, err := runNftStdin("table inet fw4 {\n\tchain input {\n\t\ttype filter hook input priority filter; policy drop;\n\t}\n}\n"); err != nil {
		t.Fatalf("create the fw4-style table: %v", err)
	}
	t.Cleanup(func() {
		ClearExposure()
		_, _ = run("nft", "delete", "table", "inet", "fw4")
	})

	if netnsExposeReachable(t, dev) {
		t.Fatal("the fw4-style drop policy must block the port before b4 exposes it")
	}

	SyncExposure(netnsExposedMTProto, nil, false)
	if !netnsExposeReachable(t, dev) {
		t.Fatalf("exposed port unreachable; fw4 input:\n%s", netnsRun(t, "nft", "-a", "list", "chain", "inet", "fw4", "input"))
	}

	netnsRun(t, "nft", "flush", "chain", "inet", "fw4", "input")
	if netnsExposeReachable(t, dev) {
		t.Fatal("flushing fw4's chain must drop b4's rule, as fw4 reload does")
	}
	exposeCheck()
	if !netnsExposeReachable(t, dev) {
		t.Fatalf("the check must restore the rule after the reload; fw4 input:\n%s", netnsRun(t, "nft", "-a", "list", "chain", "inet", "fw4", "input"))
	}

	ClearExposure()
	if netnsExposeReachable(t, dev) {
		t.Fatal("clearing must close the port again")
	}
	if out := netnsRun(t, "nft", "list", "chain", "inet", "fw4", "input"); strings.Contains(out, exposeCommentTag) {
		t.Fatalf("clearing must remove b4's rule from fw4's chain:\n%s", out)
	}
}
