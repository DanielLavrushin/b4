package tables

import (
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

const (
	tgsTarget     = "10.201.17.7"
	tgsPort       = 18443
	tgsUplink     = "b4tsu0"
	tgsUplinkIP   = "10.201.14.1"
	tgsUplinkPeer = "10.201.14.2"
	tgsVPN        = "b4tsv0"
	tgsVPNIP      = "10.201.15.1"
	tgsVPNPeer    = "10.201.15.2"
	tgsLAN        = "b4tsl0"
	tgsLANIP      = "10.201.16.1"
	tgsLANClient  = "10.201.16.2"
)

func TestNetnsATargetlessTelegramSetCarriesOnlyTheConnectionsMarkedForIt(t *testing.T) {
	netnsRequire(t)
	for _, c := range []struct {
		name, engine, routerTraffic string
	}{
		{backendNFTables, backendNFTables, config.RouterTrafficAuto},
		{backendIPTables, backendIPTables, config.RouterTrafficAuto},
		{"router traffic excluded", backendNFTables, config.RouterTrafficExclude},
	} {
		t.Run(c.name, func(t *testing.T) { tgsCarries(t, c.engine, c.routerTraffic) })
	}
}

func tgsDial(mark uint32) error {
	d := net.Dialer{Timeout: 2 * time.Second}
	if mark != 0 {
		d.Control = func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, int(mark))
			}); err != nil {
				return err
			}
			return serr
		}
	}
	c, err := d.Dial("tcp4", net.JoinHostPort(tgsTarget, strconv.Itoa(tgsPort)))
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = c.Read(make([]byte, 1))
	return err
}

type tgsRig struct {
	lan               *gwNode
	viaUplink, viaVPN *gwServer
	route             SetRoute
	engine            string
}

func tgsSetUp(t *testing.T, engine string, tune func(*config.SetConfig)) *tgsRig {
	t.Helper()
	if engine == backendNFTables && !nftWorking() {
		t.Skip("nftables is not usable here")
	}
	gwKeepSysctl(t, "net/ipv4/ip_forward", "1")
	gwKeepSysctl(t, "net/ipv4/conf/all/rp_filter", "1")

	uplink, vpn, lan := gwStartNode(t), gwStartNode(t), gwStartNode(t)
	for _, side := range []struct {
		node                     *gwNode
		router, inside, rip, nip string
	}{
		{uplink, tgsUplink, "b4tsun", tgsUplinkIP, tgsUplinkPeer},
		{vpn, tgsVPN, "b4tsvn", tgsVPNIP, tgsVPNPeer},
	} {
		side.node.attach(t, side.router, side.inside)
		netnsRun(t, "ip", "addr", "add", side.rip+"/24", "dev", side.router)
		netnsRun(t, "ip", "link", "set", side.router, "up")
		side.node.run(t, "ip", "addr", "add", side.nip+"/24", "dev", side.inside)
		side.node.run(t, "ip", "addr", "add", tgsTarget+"/32", "dev", "lo")
		side.node.run(t, "ip", "route", "add", tgsLANClient+"/32", "via", side.rip)
	}
	lan.attach(t, tgsLAN, "b4tsln")
	netnsRun(t, "ip", "addr", "add", tgsLANIP+"/24", "dev", tgsLAN)
	netnsRun(t, "ip", "link", "set", tgsLAN, "up")
	lan.run(t, "ip", "addr", "add", tgsLANClient+"/24", "dev", "b4tsln")
	lan.run(t, "ip", "route", "add", "default", "via", tgsLANIP)

	_, _ = run("ip", "route", "del", tgsTarget+"/32")
	netnsRun(t, "ip", "route", "add", tgsTarget+"/32", "via", tgsUplinkPeer, "dev", tgsUplink)
	t.Cleanup(func() { _, _ = run("ip", "route", "del", tgsTarget+"/32") })

	addr := net.JoinHostPort(tgsTarget, strconv.Itoa(tgsPort))
	reply := func(c net.Conn, _ int32) { _, _ = c.Write([]byte("k")) }
	viaUplink, viaVPN := gwServe(t, uplink, addr, reply), gwServe(t, vpn, addr, reply)

	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })
	set := netnsGatewaySet("netns-telegram-set", tgsVPN, tgsVPNPeer)
	set.Targets.IPs, set.Targets.IpsToMatch = nil, nil
	tune(set)
	cfg := netnsGatewayConfig(engine, set)
	cfg.System.MTProto.UpstreamSet = set.Id
	netnsGatewayInstall(t, cfg)

	route, ok := RoutingSetRoute(set.Id)
	if !ok || route.Mark == 0 || route.Mode != config.RoutingModeInterface {
		netnsLogState(t, engine)
		t.Fatalf("route %+v (%v): the Telegram set must get its route installed though it has no targets", route, ok)
	}
	return &tgsRig{lan: lan, viaUplink: viaUplink, viaVPN: viaVPN, route: route, engine: engine}
}

func (r *tgsRig) reached(t *testing.T, dial func() error) (uplink, vpn int32, err error) {
	t.Helper()
	uplinkBefore, vpnBefore := r.viaUplink.accepted.Load(), r.viaVPN.accepted.Load()
	err = dial()
	time.Sleep(50 * time.Millisecond)
	return r.viaUplink.accepted.Load() - uplinkBefore, r.viaVPN.accepted.Load() - vpnBefore, err
}

func tgsCarries(t *testing.T, engine, routerTraffic string) {
	rig := tgsSetUp(t, engine, func(set *config.SetConfig) { set.Routing.RouterTraffic = routerTraffic })
	route, lan, viaUplink, viaVPN := rig.route, rig.lan, rig.viaUplink, rig.viaVPN

	for _, c := range []struct {
		name string
		dial func() error
		want *gwServer
		why  string
	}{
		{"b4's own dial for the Telegram set", func() error { return tgsDial(config.SelfDialRelayMark | route.Mark) }, viaVPN,
			"the set's route mark must pick its table, and the replies must get past strict rp_filter"},
		{"b4's own dial without the set", func() error { return tgsDial(config.SelfDialMark) }, viaUplink,
			"a self-dial that does not carry the set's mark stays on the main route"},
		{"the router's own dial", func() error { return tgsDial(0) }, viaUplink,
			"a set without targets must not take the router's own traffic"},
		{"a device's connection", func() error { return lan.inside(func() error { return tgsDial(0) }) }, viaUplink,
			"a set without targets must not take a device's traffic"},
	} {
		uplinkBefore, vpnBefore := viaUplink.accepted.Load(), viaVPN.accepted.Load()
		if err := c.dial(); err != nil {
			netnsLogState(t, engine)
			t.Fatalf("%s: %v: %s", c.name, err, c.why)
		}
		gotUplink, gotVPN := viaUplink.accepted.Load()-uplinkBefore, viaVPN.accepted.Load()-vpnBefore
		wantUplink, wantVPN := int32(0), int32(0)
		if c.want == viaUplink {
			wantUplink = 1
		} else {
			wantVPN = 1
		}
		if gotUplink != wantUplink || gotVPN != wantVPN {
			netnsLogState(t, engine)
			t.Fatalf("%s reached the uplink %d and the set's interface %d times, want %d and %d: %s", c.name, gotUplink, gotVPN, wantUplink, wantVPN, c.why)
		}
	}
}

func TestNetnsTheTelegramSetsKillSwitchHoldsItsConnectionsWhenTheInterfaceGoesAway(t *testing.T) {
	netnsRequire(t)
	for _, c := range []struct {
		name       string
		killSwitch bool
	}{
		{"kill switch on", true},
		{"kill switch off", false},
	} {
		t.Run(c.name, func(t *testing.T) {
			rig := tgsSetUp(t, backendIPTables, func(set *config.SetConfig) { set.Routing.KillSwitch = c.killSwitch })
			marked := func() error { return tgsDial(config.SelfDialRelayMark | rig.route.Mark) }
			if _, vpn, err := rig.reached(t, marked); err != nil || vpn != 1 {
				netnsLogState(t, rig.engine)
				t.Fatalf("with the interface up the marked dial reached the set's interface %d times (%v), want once", vpn, err)
			}

			netnsRun(t, "ip", "link", "set", tgsVPN, "down")
			t.Cleanup(func() { _, _ = run("ip", "link", "set", tgsVPN, "up") })

			uplink, vpn, err := rig.reached(t, marked)
			switch {
			case c.killSwitch && (err == nil || uplink != 0 || vpn != 0):
				netnsLogState(t, rig.engine)
				t.Fatalf("with the interface down the marked dial reached the uplink %d and the set's interface %d times (%v): the kill switch must hold b4's own Telegram connections like the set's other traffic", uplink, vpn, err)
			case !c.killSwitch && (err != nil || uplink != 1):
				netnsLogState(t, rig.engine)
				t.Fatalf("with the interface down and no kill switch the marked dial reached the uplink %d times (%v), want once: the set's traffic falls back to the normal uplink", uplink, err)
			}
		})
	}
}

func netnsTargetOnlyLearned(t *testing.T, engine, setName string) {
	t.Helper()
	if engine == backendNFTables {
		static := netnsRun(t, "nft", "list", "set", "inet", routeNftTable, setName)
		learned := netnsRun(t, "nft", "list", "set", "inet", routeNftTable, routeNftDynSet(setName))
		if strings.Contains(static, netnsTarget) || !strings.Contains(learned, netnsTarget) {
			t.Fatalf("%s must sit in the learned set only, so the test proves what an address learned from DNS or a server name does:\n%s\n%s", netnsTarget, static, learned)
		}
		return
	}
	out := netnsRun(t, "ipset", "list", setName)
	for _, line := range strings.Split(out, "\n") {
		if f := strings.Fields(line); len(f) >= 3 && f[0] == netnsTarget && f[1] == "timeout" && f[2] != "0" {
			return
		}
	}
	t.Fatalf("%s is not a learned entry of %s, so the test proves nothing about an address learned from DNS or a server name:\n%s", netnsTarget, setName, out)
}

func TestNetnsAPacketB4SendsIntoItsTelegramConnectionLeavesWithTheConnection(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			if engine == backendNFTables && !nftWorking() {
				t.Skip("nftables is not usable here")
			}
			routeEngine = nil
			defer func() { routeEngine = nil }()

			cfg := netnsConfig(engine)
			set := cfg.Sets[0]
			set.Targets.IPs = []string{"198.51.100.99"}
			set.Targets.IpsToMatch = set.Targets.IPs
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			defer func() { _ = ClearRules(cfg) }()
			RoutingSyncConfig(cfg)
			defer RoutingClearAll()

			RoutingLearnIP(cfg, set, net.ParseIP(netnsTarget))
			netnsTargetOnlyLearned(t, engine, netnsRoutingSetName(t, engine))
			netnsAddCounters(t)
			stopQueue := netnsStartQueueListener(t, uint16(cfg.Queue.StartNum))
			defer stopQueue()

			injector := netnsInjectorMark(cfg.Queue.Mark)
			for _, tc := range []struct {
				name  string
				mark  uint32
				sport uint16
				want  string
				why   string
			}{
				{"b4's own Telegram connection", config.TelegramSetDialMark, 41021, netnsPrimary,
					"b4's own dials stay on the main table"},
				{"a packet b4 sends into its Telegram connection", injector | config.TelegramSetDialMark, 41022, netnsPrimary,
					"it carries the connection's dial mark, so it leaves the way the connection does"},
				{"a packet b4 sends into another connection", injector, 41023, netnsSecondary,
					"the learned address takes the injector mark to the set's interface, which split the Telegram connection when its packets carried that mark alone"},
			} {
				netnsZeroCounters(t)
				netnsSendMarked(t, tc.mark, tc.sport, 443)
				counts := netnsEgressCounts(t)
				if counts[tc.want] != 1 {
					netnsLogState(t, engine)
					t.Errorf("%s (0x%x): expected the packet on %s, counters were %v: %s", tc.name, tc.mark, tc.want, counts, tc.why)
				}
				for dev, n := range counts {
					if dev != tc.want && n != 0 {
						t.Errorf("%s (0x%x): %d packet(s) left over %s instead", tc.name, tc.mark, n, dev)
					}
				}
			}
		})
	}
}
