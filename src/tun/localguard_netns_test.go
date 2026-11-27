package tun

import (
	"net"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func tunNetnsRequire(t *testing.T) {
	t.Helper()
	if os.Getenv("B4_NETNS_TEST") != "1" {
		t.Skip("set B4_NETNS_TEST=1 and run inside a network namespace (make test-netns)")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root inside the namespace")
	}
	self, err1 := os.Readlink("/proc/self/ns/net")
	init, err2 := os.Readlink("/proc/1/ns/net")
	if err1 == nil && err2 == nil {
		if self == init {
			t.Fatal("refusing to run: this is the host network namespace, and the test moves the local routing rule")
		}
		return
	}
	ifaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("refusing to run: the network interfaces could not be listed to check the namespace: %v", err)
	}
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagLoopback == 0 && ifc.Flags&net.FlagUp != 0 {
			t.Fatalf("refusing to run: the network namespace could not be compared with the host's (%v; %v) and has %s up, so it may be the host's, and the test moves the local routing rule; run it in a fresh namespace (make test-netns)", err1, err2, ifc.Name)
		}
	}
}

func netnsIP(t *testing.T, args ...string) string {
	t.Helper()
	out, err := exec.Command("ip", args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ip %s: %v (%s)", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return string(out)
}

func netnsRouteGet(args ...string) string {
	out, err := exec.Command("ip", append([]string{"route", "get"}, args...)...).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil {
		return "error: " + text
	}
	return text
}

func netnsDummy(t *testing.T, name, addr string) {
	t.Helper()
	if out, err := exec.Command("ip", "link", "add", name, "type", "dummy").CombinedOutput(); err != nil {
		if vout, verr := exec.Command("ip", "link", "add", name, "type", "veth", "peer", "name", name+"p").CombinedOutput(); verr != nil {
			t.Fatalf("ip link add %s: type dummy: %v (%s); type veth: %v (%s)", name, err, strings.TrimSpace(string(out)), verr, strings.TrimSpace(string(vout)))
		}
		netnsIP(t, "link", "set", name+"p", "up")
	}
	t.Cleanup(func() { exec.Command("ip", "link", "del", name).Run() })
	if addr != "" {
		netnsIP(t, "addr", "add", addr, "dev", name)
	}
	netnsIP(t, "link", "set", name, "up")
}

func netnsLocalRulePrios(t *testing.T) []string {
	t.Helper()
	var prios []string
	for _, line := range strings.Split(netnsIP(t, "rule", "show"), "\n") {
		f := strings.Fields(line)
		if len(f) == 5 && f[1] == "from" && f[2] == "all" && f[3] == "lookup" && f[4] == "local" {
			prios = append(prios, strings.TrimSuffix(f[0], ":"))
		}
	}
	return prios
}

func netnsMoveLocalRule(t *testing.T, prio int) {
	t.Helper()
	p := strconv.Itoa(prio)
	was := netnsLocalRulePrios(t)
	if len(was) == 1 && was[0] == p {
		return
	}
	if !slices.Contains(was, p) {
		netnsIP(t, "rule", "add", "priority", p, "lookup", "local")
	}
	for _, old := range was {
		if old != p {
			netnsIP(t, "rule", "del", "priority", old, "lookup", "local")
		}
	}
	t.Cleanup(func() {
		for _, old := range was {
			exec.Command("ip", "rule", "add", "priority", old, "lookup", "local").Run()
		}
		if !slices.Contains(was, p) {
			exec.Command("ip", "rule", "del", "priority", p, "lookup", "local").Run()
		}
	})
}

func steerRules(show string) []string {
	var out []string
	for _, line := range strings.Split(show, "\n") {
		if strings.Contains(line, "fwmark 0x40000000/0x40000000") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

func TestNetnsSteeredPacketsForThisDeviceStayLocal(t *testing.T) {
	tunNetnsRequire(t)
	for _, tc := range []struct {
		name      string
		localPrio int
		guard     bool
	}{
		{"local table looked up first", 0, false},
		{"local table looked up after the capture rule, as in RouterOS containers", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			netnsDummy(t, "b4lan0", "192.0.2.1/24")
			netnsDummy(t, "b4cap0", "")
			netnsMoveLocalRule(t, tc.localPrio)

			r := &routeManager{tunName: "b4cap0", captureTable: 96, quit: make(chan struct{})}
			if err := r.setupCaptureTable(); err != nil {
				t.Fatalf("setupCaptureTable: %v", err)
			}
			torn := false
			t.Cleanup(func() {
				if !torn {
					r.teardownPortCapture()
				}
			})

			answer := netnsRouteGet("192.0.2.1", "from", "192.0.2.2", "iif", "b4lan0", "mark", "0x40000000")
			if !strings.HasPrefix(answer, "local 192.0.2.1 ") {
				t.Errorf("a DNS answer for this device's own address carries the steer mark and is routed as %q; it must be delivered locally, or every lookup this device makes itself times out", answer)
			}
			own := netnsRouteGet("192.0.2.1", "mark", "0x40000000")
			if !strings.HasPrefix(own, "local 192.0.2.1 ") {
				t.Errorf("a steered packet this device sends to its own address is routed as %q, want local delivery", own)
			}
			away := netnsRouteGet("198.51.100.7", "mark", "0x40000000")
			if !strings.Contains(away, "dev b4cap0") {
				t.Errorf("a steered packet to another host is routed as %q, want it in b4cap0", away)
			}
			onLink := netnsRouteGet("192.0.2.2", "mark", "0x40000000")
			if !strings.Contains(onLink, "dev b4cap0") {
				t.Errorf("a steered packet to another host on a connected network is routed as %q, want it in b4cap0: DNS answers to LAN devices and queries to a resolver on the LAN must still be captured", onLink)
			}

			show := netnsIP(t, "rule", "show")
			if tc.guard && !localGuardPresent(show, r.steerMarkStr(), r.capturePrio-1) {
				t.Errorf("b4 does not recognise the guard it added in this ip rule output, so every reconcile would try to add it again:\n%s", show)
			}
			rules := steerRules(show)
			var guards []string
			for _, line := range rules {
				if strings.HasSuffix(line, "lookup local") {
					guards = append(guards, line)
				}
			}
			if tc.guard && len(guards) != 1 {
				t.Errorf("rules with the steer mark %q: want exactly one that looks up the local table above the capture rule", rules)
			}
			if !tc.guard && len(guards) != 0 {
				t.Errorf("rules with the steer mark %q: the local table already comes first here, so b4 must add no rule for it", rules)
			}

			r.teardownPortCapture()
			torn = true
			show = netnsIP(t, "rule", "show")
			if left := steerRules(show); len(left) != 0 {
				t.Errorf("after teardown these rules with the steer mark are left: %q", left)
			}
			wantLocal := strconv.Itoa(tc.localPrio) + ":\tfrom all lookup local"
			if !strings.Contains(show, wantLocal) {
				t.Errorf("after teardown the system's local rule %q is gone:\n%s", wantLocal, show)
			}
		})
	}
}

func bypassRules(show string) []string {
	var out []string
	for _, line := range strings.Split(show, "\n") {
		if strings.Contains(line, "fwmark 0x20000000/0x20000000") || strings.Contains(line, "fwmark 0x10000000/0x10000000") {
			out = append(out, strings.TrimSpace(line))
		}
	}
	return out
}

func TestNetnsWholeDefaultPacketsForThisDeviceStayLocal(t *testing.T) {
	tunNetnsRequire(t)
	netnsIP(t, "link", "set", "lo", "up")
	for _, tc := range []struct {
		name      string
		localPrio int
		guard     bool
	}{
		{"local table looked up first", 0, false},
		{"local table looked up after b4's bypass rules, as in RouterOS containers", 200, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			netnsDummy(t, "b4up0", "192.0.2.1/24")
			netnsDummy(t, "b4cap0", "")
			netnsMoveLocalRule(t, tc.localPrio)
			netnsIP(t, "route", "replace", "default", "via", "192.0.2.254", "dev", "b4up0")
			t.Cleanup(func() { exec.Command("ip", "route", "del", "default").Run() })

			r := &routeManager{tunName: "b4cap0", outIface: "b4up0", outGateway: "192.0.2.254", routeTable: 97, quit: make(chan struct{})}
			torn := false
			tearDown := func() {
				r.removeClientBypass()
				r.removeReinjectLocalRule()
				r.removeBypassGuards()
				r.delFwmarkRule(reinjectMarkMatch(), "97")
				exec.Command("ip", "route", "flush", "table", "97").Run()
				torn = true
			}
			t.Cleanup(func() {
				if !torn {
					tearDown()
				}
			})
			if err := r.setupBypassTable(); err != nil {
				t.Fatalf("setupBypassTable: %v", err)
			}
			r.ensureClientBypass()
			if !r.clientBypassOK.Load() {
				t.Skip("this ip does not accept suppress_prefixlength")
			}
			if err := r.replaceDefaultIntoTun(); err != nil {
				t.Fatalf("replaceDefaultIntoTun: %v", err)
			}

			for _, mark := range []string{"0x20000000", "0x10000000"} {
				for _, dst := range []string{"192.0.2.1", "127.0.0.1"} {
					if got := netnsRouteGet(dst, "mark", mark); !strings.HasPrefix(got, "local "+dst+" ") {
						t.Errorf("a packet b4 sends with mark %s to this device's own address %s is routed as %q, want local delivery", mark, dst, got)
					}
				}
				if got := netnsRouteGet("198.51.100.7", "mark", mark); !strings.Contains(got, "dev b4up0") {
					t.Errorf("a packet b4 sends with mark %s to another host is routed as %q, want it on b4up0", mark, got)
				}
			}

			show := netnsIP(t, "rule", "show")
			var guards []string
			for _, line := range bypassRules(show) {
				if strings.HasSuffix(line, "lookup local") {
					guards = append(guards, line)
				}
			}
			if tc.guard && len(guards) != 2 {
				t.Errorf("rules that look up the local table for b4's bypass marks: %q, want one for each mark", guards)
			}
			if !tc.guard && len(guards) != 0 {
				t.Errorf("rules that look up the local table for b4's bypass marks: %q, the local table already comes first here, so b4 must add none", guards)
			}
			for _, g := range bypassGuards() {
				if tc.guard && !localGuardPresent(show, g.mark, g.before-1) {
					t.Errorf("b4 does not recognise the guard it added for mark %s in this ip rule output, so every reconcile would try to add it again:\n%s", g.mark, show)
				}
			}

			tearDown()
			show = netnsIP(t, "rule", "show")
			if left := bypassRules(show); len(left) != 0 {
				t.Errorf("after teardown these rules with b4's bypass marks are left: %q", left)
			}
			wantLocal := strconv.Itoa(tc.localPrio) + ":\tfrom all lookup local"
			if !strings.Contains(show, wantLocal) {
				t.Errorf("after teardown the system's local rule %q is gone:\n%s", wantLocal, show)
			}
		})
	}
}
