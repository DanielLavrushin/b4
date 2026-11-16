package tables

import (
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type netnsProxyCaptureCase struct {
	name     string
	late     bool
	restored bool
	devices  bool
}

func TestNetnsARouterDialToAProxySetStaysOutOfTheQueue(t *testing.T) {
	netnsRequire(t)
	netnsSetupLinks(t)

	for _, engine := range []string{backendIPTables, backendNFTables} {
		cases := []netnsProxyCaptureCase{
			{name: "set enabled at start"},
			{name: "set enabled after start", late: true},
		}
		if engine == backendIPTables {
			cases = append(cases, netnsProxyCaptureCase{name: "set enabled after start, return deleted and restored", late: true, restored: true})
		}
		cases = append(cases,
			netnsProxyCaptureCase{name: "device filter, set enabled at start", devices: true},
			netnsProxyCaptureCase{name: "device filter, set enabled after start", devices: true, late: true},
		)
		for _, c := range cases {
			t.Run(engine+", "+c.name, func(t *testing.T) {
				if engine == backendNFTables && !hasBinary("nft") {
					t.Skip("nft is not installed")
				}
				netnsRouterDialStaysOutOfTheQueue(t, engine, c)
			})
		}
	}
}

func netnsRouterDialStaysOutOfTheQueue(t *testing.T, engine string, c netnsProxyCaptureCase) {
	t.Helper()
	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsProxyMixConfig()
	cfg.System.Tables.Engine = engine
	capture := cfg
	if c.late {
		capture = netnsConfig(engine)
	}
	if c.devices {
		for _, each := range []*config.Config{cfg, capture} {
			each.Queue.Devices.Enabled = true
			each.Queue.Devices.Devices = []config.Device{{MAC: "02:B4:00:00:00:99", Selected: true}}
		}
	}
	if err := AddRules(capture); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(capture) }()

	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	if c.restored {
		netnsRun(t, append([]string{backendIPTables, "-w", "-t", "mangle", "-D", iptChainName}, strings.Fields(iptLocalDeliveryReturnRule)...)...)
		if err := AddRules(capture); err != nil {
			t.Fatalf("AddRules after the return was deleted: %v", err)
		}
	}

	if engine == backendIPTables {
		if err := AddRules(capture); err != nil {
			t.Fatalf("AddRules a second time: %v", err)
		}
		listing := netnsRun(t, backendIPTables, "-w", "-t", "mangle", "-S", iptChainName)
		routerOwn := strings.Join(iptRouterOwnProxyReturn(), " ")
		if n := strings.Count(listing, routerOwn); n != 1 {
			t.Errorf("%s holds %d copies of %q after AddRules ran again, want 1: iptables -C does not find the rule, so every restore adds another copy:\n%s", iptChainName, n, routerOwn, listing)
		}
	}

	st, ok := routeRuleCache["netns-proxy-set"]
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	if engine == backendNFTables {
		netnsRun(t, "nft", "add", "element", "inet", routeNftTable, st.setV4, "{", netnsProxyTarget, "}")
	} else {
		netnsRun(t, "ipset", "add", st.setV4, netnsProxyTarget, "-exist")
	}

	port, _ := portFromState(st)
	ln := netnsTransparentListener(t, port)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		buf := make([]byte, 4)
		if _, err := io.ReadFull(conn, buf); err == nil {
			_, _ = conn.Write([]byte("pong"))
		}
	}()

	queued, stopQueue := netnsMarkedQueueListener(t, uint16(cfg.Queue.StartNum))
	defer stopQueue()

	conn, err := net.DialTimeout("tcp", netnsProxyTarget+":443", 3*time.Second)
	if err != nil {
		t.Fatalf("the router's own dial to the proxy set's address never completed: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if _, err := conn.Write([]byte("ping")); err != nil {
		t.Fatalf("write to the listener: %v", err)
	}
	reply := make([]byte, 4)
	if _, err := io.ReadFull(conn, reply); err != nil || string(reply) != "pong" {
		t.Fatalf("the set's listener did not answer the router's own connection (%q, %v)", reply, err)
	}
	time.Sleep(200 * time.Millisecond)

	n := 0
	for mark, count := range queued() {
		if mark&routeSetMarkMask == st.mark&routeSetMarkMask {
			n += count
		}
	}
	if n > 0 {
		t.Errorf("%d packets of the router's own connection, marked 0x%x for the proxy set and on their way to its listener over the loopback interface, reached b4's queue; there a server name that another set lists gets that set's strategy, and the ClientHello leaves by the uplink instead of reaching the listener", n, st.mark)
	}
}
