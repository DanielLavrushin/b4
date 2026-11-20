package tables

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	egdSetID       = "netns-egress-down"
	egdEgress      = "b4ed0"
	egdEgressNode  = "b4ed0n"
	egdMain        = "b4em0"
	egdMainNode    = "b4em0n"
	egdLan         = "b4el0"
	egdLanNode     = "b4el0n"
	egdEgressIP    = "10.201.41.1"
	egdEgressPeer  = "10.201.41.2"
	egdMainIP      = "10.201.42.1"
	egdMainPeer    = "10.201.42.2"
	egdLanIP       = "10.201.43.1"
	egdClient      = "10.201.43.100"
	egdTarget      = "198.51.100.141"
	egdControl     = "198.51.100.142"
	egdPort        = 18141
	egdDialTimeout = 1500 * time.Millisecond
)

type egdWay int

const (
	egdBySet egdWay = iota
	egdByMain
	egdDropped
)

func (w egdWay) String() string {
	switch w {
	case egdBySet:
		return "leave by the set's interface " + egdEgress
	case egdByMain:
		return "carry the set's mark, fall through to the main table and leave by the ordinary uplink " + egdMain
	}
	return "be dropped and leave by neither uplink"
}

var egdCounterChains = map[string]string{egdEgress: "egd_egress", egdMain: "egd_main"}

type egdRig struct {
	engine string
	lan    *gwNode
}

type egdProbe struct {
	reply  string
	err    error
	leftBy map[string]int
}

func egdAnswer(tag string) func(c net.Conn, _ int32) {
	return func(c net.Conn, _ int32) { _, _ = c.Write([]byte(tag)) }
}

func egdStart(t *testing.T, engine string) *egdRig {
	t.Helper()
	gwKeepSysctl(t, "net/ipv4/ip_forward", "1")

	uplink, main, lan := gwStartNode(t), gwStartNode(t), gwStartNode(t)
	for _, l := range []struct {
		node                             *gwNode
		router, inside, routerIP, nodeIP string
	}{
		{uplink, egdEgress, egdEgressNode, egdEgressIP, egdEgressPeer},
		{main, egdMain, egdMainNode, egdMainIP, egdMainPeer},
		{lan, egdLan, egdLanNode, egdLanIP, egdClient},
	} {
		l.node.attach(t, l.router, l.inside)
		netnsRun(t, "ip", "addr", "add", l.routerIP+"/24", "dev", l.router)
		netnsRun(t, "ip", "link", "set", l.router, "up")
		l.node.run(t, "ip", "addr", "add", l.nodeIP+"/24", "dev", l.inside)
		l.node.run(t, "ip", "route", "add", "default", "via", l.routerIP)
	}
	uplink.run(t, "ip", "addr", "add", egdTarget+"/32", "dev", "lo")
	for _, dst := range []string{egdTarget, egdControl} {
		main.run(t, "ip", "addr", "add", dst+"/32", "dev", "lo")
		netnsRun(t, "ip", "route", "add", dst+"/32", "via", egdMainPeer, "dev", egdMain)
	}
	listen := net.JoinHostPort("0.0.0.0", strconv.Itoa(egdPort))
	gwServe(t, uplink, listen, egdAnswer(egdEgress))
	gwServe(t, main, listen, egdAnswer(egdMain))

	for _, chain := range egdCounterChains {
		netnsRun(t, "iptables", "-w", "-t", "mangle", "-N", chain)
		t.Cleanup(func() { _, _ = run("iptables", "-w", "-t", "mangle", "-X", chain) })
	}
	return &egdRig{engine: engine, lan: lan}
}

func egdCounts(t *testing.T) map[string]int {
	t.Helper()
	out := netnsRun(t, "iptables", "-w", "-t", "mangle", "-L", "POSTROUTING", "-v", "-x", "-n")
	counts := map[string]int{}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		for dev, chain := range egdCounterChains {
			if f[2] != chain {
				continue
			}
			if n, err := strconv.Atoi(f[0]); err == nil {
				counts[dev] += n
			}
		}
	}
	return counts
}

func (r *egdRig) probe(t *testing.T, dst string) egdProbe {
	t.Helper()
	st := netnsGatewayState(t, egdSetID)
	mark := fmt.Sprintf("0x%x/0x%x", st.mark, routeSetMarkMask)
	for dev, chain := range egdCounterChains {
		spec := []string{"POSTROUTING", "-o", dev, "-d", dst, "-p", "tcp", "--dport", strconv.Itoa(egdPort),
			"-m", "mark", "--mark", mark, "-j", chain}
		netnsRun(t, append([]string{"iptables", "-w", "-t", "mangle", "-I"}, spec...)...)
		defer func() { _, _ = run(append([]string{"iptables", "-w", "-t", "mangle", "-D"}, spec...)...) }()
	}

	var p egdProbe
	p.err = r.lan.inside(func() error {
		c, err := net.DialTimeout("tcp4", net.JoinHostPort(dst, strconv.Itoa(egdPort)), egdDialTimeout)
		if err != nil {
			return err
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(2 * time.Second))
		buf := make([]byte, 32)
		n, err := c.Read(buf)
		p.reply = string(buf[:n])
		return err
	})
	p.leftBy = egdCounts(t)
	return p
}

func (r *egdRig) expect(t *testing.T, step, why string, want egdWay) {
	t.Helper()
	p := r.probe(t, egdTarget)
	report := fmt.Sprintf("client error %v, reply %q, packets carrying the set's mark out %s %d, out %s %d",
		p.err, p.reply, egdEgress, p.leftBy[egdEgress], egdMain, p.leftBy[egdMain])
	var ok bool
	switch want {
	case egdBySet:
		ok = p.err == nil && p.reply == egdEgress && p.leftBy[egdEgress] > 0 && p.leftBy[egdMain] == 0
	case egdByMain:
		ok = p.err == nil && p.reply == egdMain && p.leftBy[egdMain] > 0 && p.leftBy[egdEgress] == 0
	case egdDropped:
		ok = p.err != nil && p.reply == "" && p.leftBy[egdEgress] == 0 && p.leftBy[egdMain] == 0
	}
	if !ok {
		netnsLogState(t, r.engine)
		t.Errorf("%s: a LAN device's connection to the set's destination %s must %s, because %s; got %s",
			step, egdTarget, want, why, report)
		return
	}
	if want != egdDropped {
		return
	}
	c := r.probe(t, egdControl)
	if c.err != nil || c.reply != egdMain {
		t.Errorf("%s: a destination outside the set must still leave by %s, or the drop above says nothing about "+
			"the set: client error %v, reply %q", step, egdMain, c.err, c.reply)
	}
}

func egdScenario(t *testing.T, engine string, killSwitch bool) {
	r := egdStart(t, engine)

	set := netnsGatewaySet(egdSetID, egdEgress, "")
	set.Targets.IPs = []string{egdTarget}
	set.Targets.IpsToMatch = []string{egdTarget}
	set.Routing.KillSwitch = killSwitch
	cfg := netnsGatewayConfig(engine, set)
	netnsGatewayInstall(t, cfg)
	t.Cleanup(netnsStartQueueListener(t, uint16(cfg.Queue.StartNum)))

	down, downWhy := egdByMain, "without the kill switch a set whose interface is gone gives way to the ordinary route"
	if killSwitch {
		down, downWhy = egdDropped, "the kill switch exists so that the set's traffic never leaves by the ordinary uplink"
	}

	r.expect(t, "with the output interface up", "that is what the set is for", egdBySet)

	netnsRun(t, "ip", "link", "set", egdEgress, "down")
	r.expect(t, "with the output interface set down", "the kernel flushes the set's route with the interface and "+downWhy, down)

	netnsRun(t, "ip", "link", "set", egdEgress, "up")
	r.expect(t, "with the output interface up again before b4 reinstalls the set",
		"the kernel does not put a route back into the set's table when the link returns, so only b4's reinstall "+
			"brings the set back, and "+downWhy, down)

	RoutingReinstallForInterface(cfg, egdEgress)
	r.expect(t, "after the reinstall the link watcher runs when the interface comes back up",
		"the reinstall puts the set's route back into its table", egdBySet)

	netnsRun(t, "ip", "link", "del", egdEgress)
	r.expect(t, "with the output interface deleted", "the set's route went with the interface and "+downWhy, down)

	RoutingForceResync(cfg)
	r.expect(t, "after the resync the monitor runs when the interface's address vanishes",
		"b4 has seen the interface, so a rebuild keeps the set's rule and "+downWhy, down)
}

func TestNetnsASetWhoseInterfaceGoesDownLeavesByTheMainUplinkWithoutTheKillSwitch(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) { egdScenario(t, engine, false) })
}

func TestNetnsASetWhoseInterfaceGoesDownIsDroppedWithTheKillSwitch(t *testing.T) {
	netnsGatewayEngines(t, func(t *testing.T, engine string) { egdScenario(t, engine, true) })
}
