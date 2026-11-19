package tables

import (
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	kpolLan        = "b4kbr0"
	kpolLanRouter  = "10.201.50.1"
	kpolPolicyHost = "10.201.50.10"
	kpolPlainHost  = "10.201.50.11"
	kpolTarget     = "198.51.100.152"
	kpolPort       = 18526
	kpolSetID      = "netns-keenetic-policy-set"
	kpolCounter    = "b4kcount"

	kpolMain   = "main"
	kpolPolicy = "policy"
	kpolSet    = "set"
)

type kpolUplink struct {
	name, link, peer, router, hop string
}

var kpolUplinks = []kpolUplink{
	{kpolMain, "b4kwm", "b4kwmn", "10.201.51.1", "10.201.51.2"},
	{kpolPolicy, "b4kwp", "b4kwpn", "10.201.52.1", "10.201.52.2"},
	{kpolSet, "b4kws", "b4kwsn", "10.201.53.1", "10.201.53.2"},
}

type kpolCase struct {
	name   string
	at     ndmsJumpPlace
	ctHeld bool
}

var kpolCases = []kpolCase{
	{"ndms below b4, connection mark held", ndmsJumpBelowB4, true},
	{"ndms above b4, connection mark held", ndmsJumpAboveB4, true},
	{"ndms below b4, connection mark not held as on the reporter's router", ndmsJumpBelowB4, false},
	{"ndms above b4, connection mark not held as on the reporter's router", ndmsJumpAboveB4, false},
}

type kpolRig struct {
	ndms        *ndmsStandIn
	policy      ndmsPolicy
	policyHost  *gwNode
	plainHost   *gwNode
	policyMAC   string
	setPre      string
	setMarkBase int
}

func kpolUplinkOf(name string) kpolUplink {
	for _, u := range kpolUplinks {
		if u.name == name {
			return u
		}
	}
	panic("no uplink " + name)
}

func kpolHost(t *testing.T, routerSide, nodeSide, addr string) (*gwNode, string) {
	t.Helper()
	n := gwStartNode(t)
	mac := n.attach(t, routerSide, nodeSide)
	netnsRun(t, "ip", "link", "set", routerSide, "master", kpolLan)
	netnsRun(t, "ip", "link", "set", routerSide, "up")
	n.run(t, "ip", "addr", "add", addr+"/24", "dev", nodeSide)
	n.run(t, "ip", "route", "add", "default", "via", kpolLanRouter)
	return n, mac
}

func kpolBuild(t *testing.T, c kpolCase) *kpolRig {
	t.Helper()
	netnsRequire(t)
	netnsRequireEngine(t, backendIPTables)
	if _, err := os.Lstat("/proc/thread-self"); err != nil {
		t.Skip("the uplink servers and the hosts enter their namespaces through /proc/thread-self, which this kernel lacks")
	}
	gwKeepSysctl(t, "net/ipv4/ip_forward", "1")

	_, _ = run("ip", "link", "del", kpolLan)
	netnsRun(t, "ip", "link", "add", kpolLan, "type", "bridge")
	t.Cleanup(func() { _, _ = run("ip", "link", "del", kpolLan) })
	netnsRun(t, "ip", "addr", "add", kpolLanRouter+"/24", "dev", kpolLan)
	netnsRun(t, "ip", "link", "set", kpolLan, "up")

	r := &kpolRig{}
	r.policyHost, r.policyMAC = kpolHost(t, "b4kpd", "b4kpdn", kpolPolicyHost)
	r.plainHost, _ = kpolHost(t, "b4kdd", "b4kddn", kpolPlainHost)

	for _, u := range kpolUplinks {
		n := gwStartNode(t)
		mac := n.attach(t, u.link, u.peer)
		netnsRun(t, "ip", "addr", "add", u.router+"/24", "dev", u.link)
		netnsRun(t, "ip", "link", "set", u.link, "up")
		n.run(t, "ip", "addr", "add", u.hop+"/24", "dev", u.peer)
		n.run(t, "ip", "addr", "add", kpolTarget+"/32", "dev", "lo")
		n.run(t, "ip", "route", "add", "default", "via", u.router)
		if u.name == kpolSet {
			netnsRun(t, "ip", "neigh", "replace", kpolTarget, "lladdr", mac, "dev", u.link, "nud", "permanent")
		} else {
			netnsRun(t, "ip", "neigh", "replace", u.hop, "lladdr", mac, "dev", u.link, "nud", "permanent")
			gwKeepSysctl(t, "net/ipv4/conf/"+u.link+"/rp_filter", "2")
		}
		name := u.name
		gwServe(t, n, net.JoinHostPort(kpolTarget, strconv.Itoa(kpolPort)), func(c net.Conn, _ int32) {
			_, _ = c.Write([]byte(name))
		})
	}
	main, policy, set := kpolUplinkOf(kpolMain), kpolUplinkOf(kpolPolicy), kpolUplinkOf(kpolSet)
	netnsRun(t, "ip", "route", "replace", kpolTarget+"/32", "via", main.hop, "dev", main.link)
	for _, h := range []*gwNode{r.policyHost, r.plainHost} {
		reply, err := r.dial(h, 3*time.Second)
		if err != nil {
			t.Skipf("with nothing but a plain route to %s a host could not reach it (%v), so this namespace cannot carry the test's connections", kpolTarget, err)
		}
		if reply != kpolMain {
			t.Fatalf("with nothing but the main table's route to %s a host must leave by the main uplink, yet it was answered by %q", kpolTarget, reply)
		}
	}

	r.ndms = ndmsStart(t)
	r.policy = ndmsPolicyAt(0)
	r.ndms.addPolicy(t, r.policy, policy.link, policy.hop)
	r.ndms.addMember(t, r.policy, r.policyMAC)
	r.ndms.addSourceRule(t, policy.router, 16386, 1154, policy.link, policy.hop)
	r.ndms.addSourceRule(t, main.router, 16387, 1156, main.link, main.hop)
	r.ndms.addSourceRule(t, set.router, 16395, 1172, set.link, "")

	held := routeCTMarkIsHeld()
	routeCTMarkHeld.Store(c.ctHeld)
	t.Cleanup(func() { routeCTMarkHeld.Store(held) })
	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })
	s := netnsGatewaySet(kpolSetID, set.link, "")
	s.Targets.IPs = []string{kpolTarget}
	s.Targets.IpsToMatch = []string{kpolTarget}
	netnsGatewayInstall(t, netnsGatewayConfig(backendIPTables, s))
	st := netnsGatewayState(t, kpolSetID)
	r.setPre = st.chainPre
	fallback := false
	for _, line := range strings.Split(netnsRun(t, backendIPTables, "-w", "-t", "mangle", "-S", st.chainPre), "\n") {
		if strings.Contains(line, "-m mark --mark 0x0/") && strings.Contains(line, "-j MARK") {
			fallback = true
		}
	}
	if fallback == c.ctHeld {
		t.Fatalf("with the connection mark held=%v the set's chain %s must carry the per-packet fallback mark rule only when the mark is not held, yet it has it=%v", c.ctHeld, st.chainPre, fallback)
	}
	netnsRun(t, "ipset", "add", st.setV4, kpolTarget, "-exist")
	want, table := routeSetMarkRule(st.mark), strconv.Itoa(st.table)
	found := false
	for _, line := range strings.Split(netnsRuleLines(t), "\n") {
		if routeRuleField(line, "fwmark") == want && routeRuleField(line, "lookup") == table {
			found = true
		}
	}
	if !found {
		t.Fatalf("the set's rule fwmark %s lookup %s is missing, so the set routes nothing:\n%s", want, table, netnsRuleLines(t))
	}

	for _, h := range []*gwNode{r.policyHost, r.plainHost} {
		reply, err := r.dial(h, 3*time.Second)
		if err != nil || reply != kpolSet {
			netnsLogState(t, backendIPTables)
			t.Fatalf("before the policy chain is hooked in, every host must leave by the set's uplink, yet one was answered by %q (%v), although it reached %s by a plain route via the main uplink a moment ago", reply, err, kpolTarget)
		}
	}
	time.Sleep(200 * time.Millisecond)
	r.setMarkBase = r.setMarked(t)

	r.ndms.placeJump(t, c.at)
	return r
}

func (r *kpolRig) counters(t *testing.T) func() map[string]int {
	t.Helper()
	_, _ = run(backendIPTables, "-w", "-t", "mangle", "-X", kpolCounter)
	netnsRun(t, backendIPTables, "-w", "-t", "mangle", "-N", kpolCounter)
	t.Cleanup(func() { _, _ = run(backendIPTables, "-w", "-t", "mangle", "-X", kpolCounter) })
	links := map[string]string{}
	for _, u := range kpolUplinks {
		spec := []string{backendIPTables, "-w", "-t", "mangle", "-I", "POSTROUTING",
			"-o", u.link, "-d", kpolTarget, "-p", "tcp", "--dport", strconv.Itoa(kpolPort), "-j", kpolCounter}
		netnsRun(t, spec...)
		spec[4] = "-D"
		t.Cleanup(func() { _, _ = run(spec...) })
		links[u.link] = u.name
	}
	read := func() map[string]int {
		out := netnsRun(t, backendIPTables, "-w", "-t", "mangle", "-L", "POSTROUTING", "-v", "-x", "-n")
		counts := map[string]int{}
		for _, line := range strings.Split(out, "\n") {
			f := strings.Fields(line)
			if len(f) < 7 || f[2] != kpolCounter {
				continue
			}
			n, err := strconv.Atoi(f[0])
			if err != nil {
				t.Fatalf("unparsable counter in %q", line)
			}
			counts[links[f[6]]] = n
		}
		if len(counts) != len(kpolUplinks) {
			t.Fatalf("an uplink counter is gone:\n%s", out)
		}
		return counts
	}
	time.Sleep(300 * time.Millisecond)
	if quiet := read(); quiet[kpolMain]+quiet[kpolPolicy]+quiet[kpolSet] != 0 {
		t.Skipf("the uplinks carried %v toward %s:%d before any host sent anything, so the counts would prove nothing", quiet, kpolTarget, kpolPort)
	}
	return read
}

func (r *kpolRig) dial(host *gwNode, timeout time.Duration) (string, error) {
	var reply string
	err := host.inside(func() error {
		c, err := net.DialTimeout("tcp4", net.JoinHostPort(kpolTarget, strconv.Itoa(kpolPort)), timeout)
		if err != nil {
			return err
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 64)
		n, err := c.Read(buf)
		reply = string(buf[:n])
		return err
	})
	return reply, err
}

func (r *kpolRig) setMarked(t *testing.T) int {
	t.Helper()
	out := netnsRun(t, backendIPTables, "-w", "-t", "mangle", "-L", r.setPre, "-v", "-x", "-n")
	total := 0
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[2] != "MARK" {
			continue
		}
		n, err := strconv.Atoi(f[0])
		if err != nil {
			t.Fatalf("unparsable counter in %q", line)
		}
		total += n
	}
	return total - r.setMarkBase
}

func kpolOnly(counts map[string]int, uplink string) bool {
	for name, n := range counts {
		if (name == uplink) != (n > 0) {
			return false
		}
	}
	return true
}

func TestNetnsADeviceInAKeeneticPolicyLeavesByThePolicyUplinkNotByTheSets(t *testing.T) {
	for _, c := range kpolCases {
		t.Run(c.name, func(t *testing.T) {
			r := kpolBuild(t, c)
			counts := r.counters(t)

			reply, err := r.dial(r.policyHost, 3*time.Second)
			left, marked, setMarked := counts(), r.ndms.marked(t, r.policyMAC), r.setMarked(t)
			if err != nil || reply != kpolPolicy || !kpolOnly(left, kpolPolicy) {
				netnsLogState(t, backendIPTables)
				t.Errorf("a device in KeeneticOS policy 0x%x must leave by the policy's table %d even though an enabled interface set routes %s through %s: the connection was answered by %q (%v), packets per uplink %v",
					r.policy.mark, r.policy.table, kpolTarget, kpolUplinkOf(kpolSet).link, reply, err, left)
			}
			if marked == 0 {
				t.Errorf("the stand-in marked none of the device's packets, so this case did not put the device into the policy")
			}
			switch {
			case c.at == ndmsJumpBelowB4 && setMarked == 0:
				t.Errorf("with the policy chain below b4's jumps the set must mark the device's packets before the policy's whole mark replaces it, yet its chain marked none")
			case c.at == ndmsJumpBelowB4 && !c.ctHeld && setMarked < 2:
				t.Errorf("with the policy chain below b4's jumps and the connection mark not held, the policy's mark saved into the connection mark replaces b4's, so the set's restore finds nothing and its fallback rule must mark every packet after the first, which its rule for new connections marks; the chain must therefore mark at least two, yet it marked %d", setMarked)
			case c.at == ndmsJumpAboveB4 && setMarked != 0:
				t.Errorf("with the policy chain above b4's jumps the set must step aside for packets that already carry the policy's mark, yet its chain marked %d", setMarked)
			}
			t.Logf("policy device: answered by %q, packets per uplink %v, stand-in marked %d, the set's chain marked %d", reply, left, marked, setMarked)

			reply, err = r.dial(r.plainHost, 3*time.Second)
			if err != nil || reply != kpolSet {
				t.Errorf("control: a device outside any policy must leave by the set's uplink, or the case above proves nothing about the set; it was answered by %q (%v)", reply, err)
			}
		})
	}
}

func TestNetnsADeviceInTheDefaultKeeneticPolicyStillLeavesByTheSetsUplink(t *testing.T) {
	for _, c := range kpolCases {
		t.Run(c.name, func(t *testing.T) {
			r := kpolBuild(t, c)
			counts := r.counters(t)

			reply, err := r.dial(r.plainHost, 3*time.Second)
			left := counts()
			if err != nil || reply != kpolSet || !kpolOnly(left, kpolSet) {
				netnsLogState(t, backendIPTables)
				t.Errorf("a device KeeneticOS puts in no policy must keep leaving by the set's uplink %s while the policy chain is in mangle PREROUTING: the connection was answered by %q (%v), packets per uplink %v",
					kpolUplinkOf(kpolSet).link, reply, err, left)
			}
			t.Logf("default device: answered by %q, packets per uplink %v", reply, left)
		})
	}
}

func TestNetnsAKeeneticPolicyWithAnEmptyTableDropsItsDevicesTrafficAtTheBlackhole(t *testing.T) {
	for _, c := range kpolCases {
		t.Run(c.name, func(t *testing.T) {
			r := kpolBuild(t, c)
			counts := r.counters(t)

			reply, err := r.dial(r.policyHost, 3*time.Second)
			if err != nil || reply != kpolPolicy {
				netnsLogState(t, backendIPTables)
				t.Fatalf("control: with its table populated the policy device must leave by the policy uplink, it was answered by %q (%v), packets per uplink %v", reply, err, counts())
			}

			r.ndms.emptyTable(t, r.policy)
			before, markedBefore := counts(), r.ndms.marked(t, r.policyMAC)
			reply, err = r.dial(r.policyHost, 1500*time.Millisecond)
			after, marked := counts(), r.ndms.marked(t, r.policyMAC)
			if err == nil {
				t.Errorf("with policy table %d empty the device still connected, answered by %q: the blackhole rule at priority %d must end its lookup", r.policy.table, reply, r.policy.priority+1)
			}
			for _, u := range kpolUplinks {
				if n := after[u.name] - before[u.name]; n != 0 {
					t.Errorf("with policy table %d empty %d packet(s) of the policy device leaked by the %s uplink %s instead of stopping at the blackhole", r.policy.table, n, u.name, u.link)
				}
			}
			if marked == markedBefore {
				t.Errorf("no packet of the second connection reached the stand-in, so the missing traffic proves nothing about the blackhole")
			}
			t.Logf("second connection: %v; stand-in marked %d packet(s)", err, marked-markedBefore)
		})
	}
}
