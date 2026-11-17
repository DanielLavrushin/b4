package tables

import (
	"bytes"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/socks5"
	"github.com/daniellavrushin/b4/tproxy"
)

const (
	udpPortsNsSetID    = "netns-udp-ports"
	udpPortsNsFilter   = "50000-50100,19294-19344,53"
	udpPortsNsInRange  = 50050
	udpPortsNsInRange2 = 19300
	udpPortsNsOutRange = 9999
	udpPortsNsRouter   = 50070
	udpPortsNsClientSv = 50060
	udpPortsNsClientSp = 50080
	udpPortsNsPeerSp   = 50090
	udpPortsNsUpstream = 31904

	udpPortsNsScopeNone   = ""
	udpPortsNsScopeIface  = "interface"
	udpPortsNsScopeDevice = "device"

	udpPortsNsLAN6Router = "fd00:21::1"
	udpPortsNsClient6    = "fd00:21::100"
	udpPortsNsWAN6Router = "fd00:22::1"
	udpPortsNsWAN6GW     = "fd00:22::2"
	udpPortsNsInet6      = "2001:db8:47::7"
)

func udpPortsNsConfig(engine, scope, mac string) *config.Config {
	cfg := netnsConfig(engine)
	set := config.NewSetConfig()
	set.Id = udpPortsNsSetID
	set.Name = "netnsudpports"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeProxy
	set.Routing.Upstream.Host = "127.0.0.1"
	set.Routing.Upstream.Port = udpPortsNsUpstream
	set.Routing.Upstream.UDP = true
	set.UDP.DPortFilter = udpPortsNsFilter
	switch scope {
	case udpPortsNsScopeIface:
		set.Routing.SourceInterfaces = []string{quicNsLAN}
	case udpPortsNsScopeDevice:
		set.Targets.SourceDevices = []string{mac}
	}
	cfg.Sets = []*config.SetConfig{&set}
	return cfg
}

func udpPortsNsExchangeOn(network string, from *gwNode, srcPort int, host string, port int, msg string) string {
	var conn *net.UDPConn
	if err := quicNsDial(from, func() (err error) {
		conn, err = net.DialUDP(network, &net.UDPAddr{Port: srcPort}, &net.UDPAddr{IP: net.ParseIP(host), Port: port})
		return err
	}); err != nil {
		return "dial: " + err.Error()
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(1500 * time.Millisecond))
	if _, err := conn.Write([]byte(msg)); err != nil {
		return quicNsOutcome(err)
	}
	buf := make([]byte, 512)
	size, err := conn.Read(buf)
	if err != nil {
		return quicNsOutcome(err)
	}
	return "reply=" + string(buf[:size])
}

func udpPortsNsExchange(from *gwNode, srcPort int, host string, port int, msg string) string {
	return udpPortsNsExchangeOn("udp4", from, srcPort, host, port, msg)
}

func udpPortsNsServeTCPEcho(t *testing.T, n *gwNode, host string, port int) {
	t.Helper()
	var ln net.Listener
	if err := n.inside(func() (err error) {
		ln, err = net.Listen("tcp4", net.JoinHostPort(host, strconv.Itoa(port)))
		return err
	}); err != nil {
		t.Fatalf("tcp listen on %s:%d: %v", host, port, err)
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
				_ = c.SetDeadline(time.Now().Add(2 * time.Second))
				buf := make([]byte, 256)
				size, err := c.Read(buf)
				if err != nil {
					return
				}
				_, _ = c.Write(append([]byte("echo:"), buf[:size]...))
			}(conn)
		}
	}()
}

func udpPortsNsServeWhoami(t *testing.T, n *gwNode, network, host string, port int) {
	t.Helper()
	var uc *net.UDPConn
	if err := n.inside(func() (err error) {
		uc, err = net.ListenUDP(network, &net.UDPAddr{IP: net.ParseIP(host), Port: port})
		return err
	}); err != nil {
		t.Fatalf("udp listen on %s:%d: %v", host, port, err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 512)
		for {
			size, from, err := uc.ReadFromUDP(buf)
			if err != nil {
				return
			}
			_, _ = uc.WriteToUDP([]byte("from="+from.IP.String()+" "+string(buf[:size])), from)
		}
	}()
	t.Cleanup(func() {
		_ = uc.Close()
		<-done
	})
}

func udpPortsNsIptables(engine string) string {
	if engine == backendIPTablesLegacy {
		return backendIPTablesLegacy
	}
	return backendIPTables
}

func udpPortsNsChain(t *testing.T, engine, chain string) string {
	t.Helper()
	if engine == backendNFTables {
		return netnsRun(t, "nft", "list", "chain", "inet", routeNftTable, chain)
	}
	return netnsRun(t, udpPortsNsIptables(engine), "-w", "-t", "mangle", "-S", chain)
}

func udpPortsNsDropPortRule(t *testing.T, engine, chain string) {
	t.Helper()
	if engine == backendNFTables {
		deleteNftRulesContaining(chain, "udp dport")
		return
	}
	ipt := udpPortsNsIptables(engine)
	for _, line := range strings.Split(netnsRun(t, ipt, "-w", "-t", "mangle", "-S", chain), "\n") {
		if !strings.Contains(line, "TPROXY") || !strings.Contains(line, "--ctdir") {
			continue
		}
		spec := strings.Fields(strings.Replace(line, "-A ", "-D ", 1))
		netnsRun(t, append([]string{ipt, "-w", "-t", "mangle"}, spec...)...)
	}
}

func udpPortsNsInstall(t *testing.T, cfg *config.Config) {
	t.Helper()
	routeEngine = nil
	t.Cleanup(func() { routeEngine = nil })
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	t.Cleanup(func() { _ = ClearRules(cfg) })
	RoutingSyncConfig(cfg)
	t.Cleanup(RoutingClearAll)
}

func netnsUDPPortOnlyProxySet(t *testing.T, engine, scope string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	client, inet, mac := quicNsRig(t)
	peerPort := udpPortsNsPeerSp
	clientPort := udpPortsNsClientSp
	if scope == udpPortsNsScopeDevice {
		peerPort++
		clientPort++
	}

	cfg := udpPortsNsConfig(engine, scope, mac)
	set := cfg.Sets[0]
	udpPortsNsInstall(t, cfg)

	st, ok := routeRuleCache[udpPortsNsSetID]
	if !ok {
		t.Fatal("a proxy set with UDP ports, a source scope and no destination target built no rules")
	}
	if st.udpPorts != "19294-19344,50000-50100" {
		t.Fatalf("routed ports = %q, want the listed ranges without the DNS port", st.udpPorts)
	}
	port, _ := portFromState(st)
	s := &netnsSockets{tcp: map[string][]string{}, udp: map[string][]string{}}
	netnsServeTCP(t, netnsMarkedTransparentListener(t, port, 0), "tproxy", "tproxy:", s)
	netnsServeUDP(t, netnsTransparentUDP(t, port), "tproxy", s, true)
	netnsServeUDP(t, netnsPlainUDP(t, udpPortsNsRouter), "router", s, false)

	direct := quicNsServeEcho(t, inet, quicNsMatched, udpPortsNsInRange)
	direct2 := quicNsServeEcho(t, inet, quicNsUnmatched, udpPortsNsInRange2)
	quicNsServeEcho(t, inet, quicNsMatched, udpPortsNsOutRange)
	quicNsServeEcho(t, inet, quicNsMatched, config.DNSPort)
	udpPortsNsServeTCPEcho(t, inet, quicNsMatched, udpPortsNsInRange)
	quicNsServeEcho(t, client, quicNsClient, udpPortsNsClientSv)
	quicNsServeEcho(t, client, quicNsClient, udpPortsNsOutRange)

	t.Logf("%s chain:\n%s", st.chainPre, udpPortsNsChain(t, engine, st.chainPre))

	in1 := net.JoinHostPort(quicNsMatched, strconv.Itoa(udpPortsNsInRange))
	in2 := net.JoinHostPort(quicNsUnmatched, strconv.Itoa(udpPortsNsInRange2))

	got := udpPortsNsExchange(client, 0, quicNsMatched, udpPortsNsInRange, "in1")
	if !s.got("udp", "tproxy", "in1@"+in1) {
		netnsLogState(t, engine)
		t.Errorf("client UDP to a listed port (%s) never reached the transparent socket with its original destination: %v", got, s.udp)
	}
	got = udpPortsNsExchange(client, 0, quicNsUnmatched, udpPortsNsInRange2, "in2")
	if !s.got("udp", "tproxy", "in2@"+in2) {
		t.Errorf("client UDP to a listed port on another address (%s) never reached the transparent socket: %v", got, s.udp)
	}
	if seen := append(direct.got(), direct2.got()...); len(seen) != 0 {
		t.Errorf("the destinations themselves received %v; UDP to a listed port must not leave directly", seen)
	}
	if got := udpPortsNsExchange(client, 0, quicNsMatched, udpPortsNsOutRange, "out"); got != "reply=echo:out" {
		t.Errorf("client UDP to a port the set does not list: %s; it must pass untouched", got)
	}
	if got := udpPortsNsExchange(client, 0, quicNsMatched, config.DNSPort, "dns"); got != "reply=echo:dns" {
		t.Errorf("client DNS to an outside resolver: %s; the DNS port stays out of a port-routed set even when it is listed", got)
	}
	if got := quicNsTCP(client, quicNsMatched, udpPortsNsInRange, "web"); got != "reply=echo:web" {
		t.Errorf("client TCP to a listed port: %s; the set takes UDP only", got)
	}
	got = udpPortsNsExchange(client, 0, quicNsLANRouter, udpPortsNsRouter, "torouter")
	if !s.got("udp", "router", "torouter") || quicNsDiverted(s, "tproxy", "torouter") {
		t.Errorf("client UDP to the router's own address on a listed port (%s) must reach the router's service, not the upstream: %v", got, s.udp)
	}
	if got := udpPortsNsExchange(nil, 0, quicNsMatched, udpPortsNsInRange, "own"); got != "reply=echo:own" {
		t.Errorf("the router's own UDP to a listed port: %s; only devices' UDP is taken", got)
	}
	if got := udpPortsNsExchange(client, clientPort, quicNsMatched, udpPortsNsOutRange, "reply"); got != "reply=echo:reply" {
		netnsLogState(t, engine)
		t.Errorf("a reply that returns to a client source port inside the listed range: %s; replies must pass (diverted: %v)", got, quicNsDiverted(s, "tproxy", "echo:reply"))
	}
	if got := udpPortsNsExchange(inet, 0, quicNsClient, udpPortsNsClientSv, "inbound"); got != "reply=echo:inbound" || quicNsDiverted(s, "tproxy", "inbound") {
		netnsLogState(t, engine)
		t.Errorf("a new flow from the uplink to a LAN host on a listed port: %s; it must pass (diverted: %v)", got, quicNsDiverted(s, "tproxy", "inbound"))
	}
	if got := udpPortsNsExchange(inet, peerPort, quicNsClient, udpPortsNsOutRange, "peer"); got != "reply=echo:peer" || quicNsDiverted(s, "tproxy", "echo:peer") {
		netnsLogState(t, engine)
		t.Errorf("a LAN host's reply to an outside peer whose source port is inside the listed range: %s; it must pass (diverted: %v)", got, quicNsDiverted(s, "tproxy", "echo:peer"))
	}
	if !RoutingRulesPresent(cfg) {
		t.Error("the monitor finds the rules of a port-routed proxy set incomplete, so it would rebuild them on every tick")
	}

	udpPortsNsDropPortRule(t, engine, st.chainPre)
	sources := routeNormalizedSources(set.Routing.SourceInterfaces)
	var err error
	if engine == backendNFTables {
		err = addProxyUDPPortRuleNft(st.chainPre, false, st.udpPorts, st.mark, port, sources, false)
	} else {
		err = addProxyUDPPortRuleIpt(false, st.chainPre, st.udpPorts, st.mark, port, sources, engine == backendIPTablesLegacy, false)
	}
	if err != nil {
		t.Fatalf("install the rule without the direction match: %v", err)
	}
	naive := udpPortsNsExchange(inet, peerPort+5, quicNsClient, udpPortsNsOutRange, "naive")
	if naive == "reply=echo:naive" || !quicNsDiverted(s, "tproxy", "echo:naive") {
		t.Errorf("without the connection-direction match the same reply of a LAN host passed (%s); if that holds, the match is no longer what protects replies and this test must say what does", naive)
	}

	set.UDP.DPortFilter = "40000"
	RoutingSyncConfig(cfg)
	got = udpPortsNsExchange(client, 0, quicNsMatched, 40000, "moved")
	if !s.got("udp", "tproxy", "moved@"+net.JoinHostPort(quicNsMatched, "40000")) {
		netnsLogState(t, engine)
		t.Errorf("after the port list changed, UDP to the new port (%s) never reached the transparent socket", got)
	}
	udpPortsNsExchange(client, 0, quicNsMatched, udpPortsNsInRange, "stale")
	if quicNsDiverted(s, "tproxy", "stale") {
		t.Error("after the port list changed, UDP to a port no longer listed still reached the upstream")
	}
}

func TestNetnsUDPPortOnlyProxySet(t *testing.T) {
	netnsUDPPortOnlyProxySet(t, backendIPTables, udpPortsNsScopeIface)
}

func TestNetnsUDPPortOnlyProxySetNft(t *testing.T) {
	netnsUDPPortOnlyProxySet(t, backendNFTables, udpPortsNsScopeIface)
}

func TestNetnsUDPPortOnlyProxySetLegacy(t *testing.T) {
	netnsUDPPortOnlyProxySet(t, backendIPTablesLegacy, udpPortsNsScopeIface)
}

func TestNetnsUDPPortOnlyProxySetByDevice(t *testing.T) {
	netnsUDPPortOnlyProxySet(t, backendIPTables, udpPortsNsScopeDevice)
}

func TestNetnsUDPPortOnlyProxySetByDeviceNft(t *testing.T) {
	netnsUDPPortOnlyProxySet(t, backendNFTables, udpPortsNsScopeDevice)
}

func TestNetnsUDPPortOnlyProxySetByDeviceLegacy(t *testing.T) {
	netnsUDPPortOnlyProxySet(t, backendIPTablesLegacy, udpPortsNsScopeDevice)
}

func netnsUDPPortOnlySetWithoutScopeInstallsNothing(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	client, inet, _ := quicNsRig(t)
	cfg := udpPortsNsConfig(engine, udpPortsNsScopeNone, "")
	udpPortsNsInstall(t, cfg)

	if _, ok := routeRuleCache[udpPortsNsSetID]; ok {
		t.Fatal("a port-only proxy set with no source interface and no source device built rules; unscoped, they take replies and inbound connections from the uplink")
	}
	quicNsServeEcho(t, inet, quicNsMatched, udpPortsNsInRange)
	if got := udpPortsNsExchange(client, 0, quicNsMatched, udpPortsNsInRange, "direct"); got != "reply=echo:direct" {
		t.Errorf("client UDP to a listed port of an unscoped set: %s; nothing is installed, so it passes untouched", got)
	}
}

func TestNetnsUDPPortOnlySetWithoutScopeInstallsNothing(t *testing.T) {
	netnsUDPPortOnlySetWithoutScopeInstallsNothing(t, backendIPTables)
}

func TestNetnsUDPPortOnlySetWithoutScopeInstallsNothingNft(t *testing.T) {
	netnsUDPPortOnlySetWithoutScopeInstallsNothing(t, backendNFTables)
}

func TestNetnsUDPPortOnlySetWithoutScopeInstallsNothingLegacy(t *testing.T) {
	netnsUDPPortOnlySetWithoutScopeInstallsNothing(t, backendIPTablesLegacy)
}

func udpPortsNsUpstreamServer(t *testing.T) {
	t.Helper()
	upCfg := config.NewConfig()
	upCfg.System.Socks5.Enabled = true
	upCfg.System.Socks5.BindAddress = "127.0.0.1"
	upCfg.System.Socks5.Port = udpPortsNsUpstream
	upstream := socks5.NewServer(&upCfg)
	if err := upstream.Start(); err != nil {
		t.Fatalf("SOCKS5 upstream: %v", err)
	}
	t.Cleanup(func() { _ = upstream.Stop() })
}

func udpPortsNsListener(t *testing.T, cfg *config.Config) {
	t.Helper()
	st, ok := routeRuleCache[udpPortsNsSetID]
	if !ok {
		t.Fatal("the port-routed proxy set built no rules")
	}
	port, _ := portFromState(st)
	mgr := tproxy.NewManager(nil)
	mgr.SyncConfig(cfg)
	t.Cleanup(mgr.Stop)
	if ls := mgr.ListenerStatus(udpPortsNsSetID, port); !ls.Running {
		t.Fatalf("no transparent listener started for a port-routed proxy set: %+v", ls)
	}
}

func udpPortsNsEnableIPv6(t *testing.T, client, inet *gwNode) {
	t.Helper()
	for _, key := range []string{"net/ipv6/conf/all/disable_ipv6", "net/ipv6/conf/default/disable_ipv6",
		"net/ipv6/conf/" + quicNsLAN + "/disable_ipv6", "net/ipv6/conf/" + quicNsWAN + "/disable_ipv6", "net/ipv6/conf/lo/disable_ipv6"} {
		gwKeepSysctl(t, key, "0")
	}
	gwKeepSysctl(t, "net/ipv6/conf/all/forwarding", "1")
	netnsRun(t, "ip", "-6", "addr", "add", udpPortsNsLAN6Router+"/64", "dev", quicNsLAN, "nodad")
	netnsRun(t, "ip", "-6", "addr", "add", udpPortsNsWAN6Router+"/64", "dev", quicNsWAN, "nodad")
	netnsRun(t, "ip", "-6", "route", "add", udpPortsNsInet6+"/128", "via", udpPortsNsWAN6GW, "dev", quicNsWAN)
	client.run(t, "ip", "-6", "addr", "add", udpPortsNsClient6+"/64", "dev", quicNsLANPeer, "nodad")
	client.run(t, "ip", "-6", "route", "add", "default", "via", udpPortsNsLAN6Router)
	inet.run(t, "ip", "-6", "addr", "add", udpPortsNsWAN6GW+"/64", "dev", quicNsWANPeer, "nodad")
	inet.run(t, "ip", "-6", "addr", "add", udpPortsNsInet6+"/128", "dev", "lo", "nodad")
	inet.run(t, "ip", "-6", "route", "add", "default", "via", udpPortsNsWAN6Router)
}

func netnsUDPPortOnlySetRelaysThroughSOCKS5(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	client, inet, _ := quicNsRig(t)
	udpPortsNsEnableIPv6(t, client, inet)

	cfg := udpPortsNsConfig(engine, udpPortsNsScopeIface, "")
	cfg.Queue.IPv6Enabled = true
	udpPortsNsUpstreamServer(t)
	udpPortsNsInstall(t, cfg)
	udpPortsNsListener(t, cfg)
	t.Logf("%s chain:\n%s", routeRuleCache[udpPortsNsSetID].chainPre, udpPortsNsChain(t, engine, routeRuleCache[udpPortsNsSetID].chainPre))

	udpPortsNsServeWhoami(t, inet, "udp4", quicNsMatched, udpPortsNsInRange)
	udpPortsNsServeWhoami(t, inet, "udp4", quicNsUnmatched, udpPortsNsInRange2)
	udpPortsNsServeWhoami(t, inet, "udp4", quicNsMatched, udpPortsNsOutRange)
	udpPortsNsServeWhoami(t, inet, "udp6", udpPortsNsInet6, udpPortsNsInRange)
	udpPortsNsServeWhoami(t, inet, "udp6", udpPortsNsInet6, udpPortsNsOutRange)

	for _, c := range []struct {
		network string
		host    string
		port    int
		msg     string
		seenAs  string
	}{
		{"udp4", quicNsMatched, udpPortsNsInRange, "one", quicNsWANRouter},
		{"udp4", quicNsUnmatched, udpPortsNsInRange2, "two", quicNsWANRouter},
		{"udp4", quicNsMatched, udpPortsNsInRange, "three", quicNsWANRouter},
		{"udp4", quicNsMatched, udpPortsNsOutRange, "four", quicNsClient},
		{"udp6", udpPortsNsInet6, udpPortsNsInRange, "five", udpPortsNsWAN6Router},
		{"udp6", udpPortsNsInet6, udpPortsNsOutRange, "six", udpPortsNsClient6},
	} {
		got := udpPortsNsExchangeOn(c.network, client, 0, c.host, c.port, c.msg)
		want := "reply=from=" + c.seenAs + " " + c.msg
		if got != want {
			netnsLogState(t, engine)
			t.Errorf("client UDP to %s port %d: %s, want %q", c.host, c.port, got, want)
		}
	}
}

func TestNetnsUDPPortOnlySetRelaysThroughSOCKS5(t *testing.T) {
	netnsUDPPortOnlySetRelaysThroughSOCKS5(t, backendIPTables)
}

func TestNetnsUDPPortOnlySetRelaysThroughSOCKS5Nft(t *testing.T) {
	netnsUDPPortOnlySetRelaysThroughSOCKS5(t, backendNFTables)
}

func TestNetnsUDPPortOnlySetRelaysThroughSOCKS5Legacy(t *testing.T) {
	netnsUDPPortOnlySetRelaysThroughSOCKS5(t, backendIPTablesLegacy)
}

func netnsAStalledUpstreamKeepsEstablishedUDPFlowing(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	client, inet, _ := quicNsRig(t)

	cfg := udpPortsNsConfig(engine, udpPortsNsScopeIface, "")
	udpPortsNsUpstreamServer(t)
	udpPortsNsInstall(t, cfg)
	udpPortsNsListener(t, cfg)

	udpPortsNsServeWhoami(t, inet, "udp4", quicNsMatched, udpPortsNsInRange)
	udpPortsNsServeWhoami(t, inet, "udp4", quicNsUnmatched, udpPortsNsInRange2)

	var live *net.UDPConn
	if err := client.inside(func() (err error) {
		live, err = net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(quicNsMatched), Port: udpPortsNsInRange})
		return err
	}); err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer live.Close()
	ping := func(msg string) (string, time.Duration) {
		start := time.Now()
		_ = live.SetDeadline(start.Add(1500 * time.Millisecond))
		if _, err := live.Write([]byte(msg)); err != nil {
			return quicNsOutcome(err), time.Since(start)
		}
		buf := make([]byte, 512)
		size, err := live.Read(buf)
		if err != nil {
			return quicNsOutcome(err), time.Since(start)
		}
		return string(buf[:size]), time.Since(start)
	}
	want := "from=" + quicNsWANRouter + " "
	if got, _ := ping("before"); got != want+"before" {
		t.Fatalf("the flow never went through the upstream before it stalled: %s", got)
	}

	stall := []string{"INPUT", "-i", "lo", "-p", "tcp", "--dport", strconv.Itoa(udpPortsNsUpstream), "--syn", "-j", "DROP"}
	ipt := udpPortsNsIptables(engine)
	netnsRun(t, append([]string{ipt, "-w", "-t", "filter", "-I"}, stall...)...)
	t.Cleanup(func() { _, _ = run(append([]string{ipt, "-w", "-t", "filter", "-D"}, stall...)...) })

	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 4; i++ {
			udpPortsNsExchange(client, 0, quicNsUnmatched, udpPortsNsInRange2, "new"+strconv.Itoa(i))
		}
	}()
	time.Sleep(300 * time.Millisecond)
	worst := time.Duration(0)
	for i := 0; i < 5; i++ {
		got, took := ping("during" + strconv.Itoa(i))
		if took > worst {
			worst = took
		}
		if got != want+"during"+strconv.Itoa(i) {
			t.Errorf("an established flow while new flows wait for an upstream that does not answer: %s after %v; the relay must keep carrying it", got, took)
			break
		}
	}
	t.Logf("MEASURED [%s] slowest round trip of the established flow while the upstream ignored new sessions: %v", engine, worst)
	<-done
}

func TestNetnsAStalledUpstreamKeepsEstablishedUDPFlowing(t *testing.T) {
	netnsAStalledUpstreamKeepsEstablishedUDPFlowing(t, backendIPTables)
}

func TestNetnsAStalledUpstreamKeepsEstablishedUDPFlowingNft(t *testing.T) {
	netnsAStalledUpstreamKeepsEstablishedUDPFlowing(t, backendNFTables)
}

func TestNetnsAStalledUpstreamKeepsEstablishedUDPFlowingLegacy(t *testing.T) {
	netnsAStalledUpstreamKeepsEstablishedUDPFlowing(t, backendIPTablesLegacy)
}

func netnsAPortRoutedSetBesideAnAddressSet(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	client, _, _ := quicNsRig(t)

	for _, tc := range []struct {
		name         string
		addressFirst bool
		addressScope bool
		shared       string
	}{
		{"an unscoped address set listed first", true, false, "ports"},
		{"an unscoped address set listed second", false, false, "ports"},
		{"a scoped address set listed first", true, true, "address"},
		{"a scoped address set listed second", false, true, "ports"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := udpPortsNsConfig(engine, udpPortsNsScopeIface, "")
			ports := cfg.Sets[0]
			address := config.NewSetConfig()
			address.Id = "netns-udp-address"
			address.Name = "netnsudpaddress"
			address.Enabled = true
			address.Routing.Enabled = true
			address.Routing.Mode = config.RoutingModeProxy
			address.Routing.Upstream.Host = "127.0.0.1"
			address.Routing.Upstream.Port = udpPortsNsUpstream + 1
			address.Routing.Upstream.UDP = true
			address.Targets.IPs = []string{quicNsMatched}
			address.Targets.IpsToMatch = []string{quicNsMatched}
			if tc.addressScope {
				address.Routing.SourceInterfaces = []string{quicNsLAN}
			}
			cfg.Sets = []*config.SetConfig{ports, &address}
			if tc.addressFirst {
				cfg.Sets = []*config.SetConfig{&address, ports}
			}
			udpPortsNsInstall(t, cfg)

			portState, ok := routeRuleCache[udpPortsNsSetID]
			addressState, ok2 := routeRuleCache[address.Id]
			if !ok || !ok2 {
				t.Fatalf("both sets must install, got %v", routeRuleCache)
			}
			portsPort, _ := portFromState(portState)
			addressPort, _ := portFromState(addressState)
			s := &netnsSockets{tcp: map[string][]string{}, udp: map[string][]string{}}
			netnsServeUDP(t, netnsTransparentUDP(t, portsPort), "ports", s, true)
			netnsServeUDP(t, netnsTransparentUDP(t, addressPort), "address", s, true)

			for _, c := range []struct {
				host string
				port int
				msg  string
				want string
				why  string
			}{
				{quicNsMatched, udpPortsNsInRange, "shared", tc.shared, "the address set's address on a routed port"},
				{quicNsMatched, udpPortsNsOutRange, "byaddress", "address", "the address set's address on a port the other set does not list"},
				{quicNsUnmatched, udpPortsNsInRange2, "byport", "ports", "another address on a routed port"},
			} {
				got := udpPortsNsExchange(client, 0, c.host, c.port, c.msg)
				if !s.got("udp", c.want, c.msg+"@"+net.JoinHostPort(c.host, strconv.Itoa(c.port))) {
					netnsLogState(t, engine)
					t.Errorf("UDP to %s (%s) must go to the %q set, got %v", c.why, got, c.want, s.udp)
				}
			}
			if !RoutingRulesPresent(cfg) {
				t.Error("the monitor finds the rules of a port-routed set beside an address set incomplete")
			}
		})
	}
}

func TestNetnsAPortRoutedSetBesideAnAddressSet(t *testing.T) {
	netnsAPortRoutedSetBesideAnAddressSet(t, backendIPTables)
}

func TestNetnsAPortRoutedSetBesideAnAddressSetNft(t *testing.T) {
	netnsAPortRoutedSetBesideAnAddressSet(t, backendNFTables)
}

func TestNetnsAPortRoutedSetBesideAnAddressSetLegacy(t *testing.T) {
	netnsAPortRoutedSetBesideAnAddressSet(t, backendIPTablesLegacy)
}

func netnsASetThatTurnsPortOnlyDropsItsLearnedAddresses(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	client, inet, _ := quicNsRig(t)

	cfg := udpPortsNsConfig(engine, udpPortsNsScopeIface, "")
	set := cfg.Sets[0]
	set.Targets.SNIDomains = []string{"learned.test"}
	set.Targets.DomainsToMatch = []string{"learned.test"}
	udpPortsNsInstall(t, cfg)

	st, ok := routeRuleCache[udpPortsNsSetID]
	if !ok {
		t.Fatal("the domain set built no rules")
	}
	port, _ := portFromState(st)
	s := &netnsSockets{tcp: map[string][]string{}, udp: map[string][]string{}}
	netnsServeTCP(t, netnsMarkedTransparentListener(t, port, 0), "tproxy", "tproxy:", s)
	netnsServeUDP(t, netnsTransparentUDP(t, port), "tproxy", s, true)
	udpPortsNsServeTCPEcho(t, inet, quicNsMatched, 8080)
	quicNsServeEcho(t, inet, quicNsMatched, udpPortsNsOutRange)

	RoutingHandleDNS(cfg, set, []net.IP{net.ParseIP(quicNsMatched)})
	learned := ""
	for attempt := 0; attempt < 20 && learned != "reply=tproxy:t1"; attempt++ {
		if attempt > 0 {
			time.Sleep(100 * time.Millisecond)
		}
		learned = quicNsTCP(client, quicNsMatched, 8080, "t1")
	}
	if learned != "reply=tproxy:t1" {
		netnsLogState(t, engine)
		t.Fatalf("with the domain listed, TCP to its learned address: %s; it must go to the upstream before the test can show the address being dropped", learned)
	}

	set.Targets.SNIDomains = nil
	set.Targets.DomainsToMatch = nil
	RoutingSyncConfig(cfg)
	if got := routeRuleCache[udpPortsNsSetID].udpPorts; got == "" {
		t.Fatal("without its last domain the set must route by UDP port")
	}

	if got := quicNsTCP(client, quicNsMatched, 8080, "t2"); got != "reply=echo:t2" {
		netnsLogState(t, engine)
		t.Errorf("TCP to an address the set learned for a domain it no longer lists: %s; a port-routed set takes no TCP", got)
	}
	if got := udpPortsNsExchange(client, 0, quicNsMatched, udpPortsNsOutRange, "u2"); got != "reply=echo:u2" || quicNsDiverted(s, "tproxy", "u2") {
		netnsLogState(t, engine)
		t.Errorf("UDP to that address on a port the set does not list: %s (diverted: %v); only the listed ports are routed", got, quicNsDiverted(s, "tproxy", "u2"))
	}
	got := udpPortsNsExchange(client, 0, quicNsMatched, udpPortsNsInRange, "u3")
	if !s.got("udp", "tproxy", "u3@"+net.JoinHostPort(quicNsMatched, strconv.Itoa(udpPortsNsInRange))) {
		t.Errorf("UDP to a listed port (%s) must still reach the upstream", got)
	}
	if !RoutingRulesPresent(cfg) {
		t.Error("the monitor finds the rules of a set that turned port-only incomplete, so it would rebuild them on every tick")
	}
}

func TestNetnsASetThatTurnsPortOnlyDropsItsLearnedAddresses(t *testing.T) {
	netnsASetThatTurnsPortOnlyDropsItsLearnedAddresses(t, backendIPTables)
}

func TestNetnsASetThatTurnsPortOnlyDropsItsLearnedAddressesNft(t *testing.T) {
	netnsASetThatTurnsPortOnlyDropsItsLearnedAddresses(t, backendNFTables)
}

func TestNetnsASetThatTurnsPortOnlyDropsItsLearnedAddressesLegacy(t *testing.T) {
	netnsASetThatTurnsPortOnlyDropsItsLearnedAddresses(t, backendIPTablesLegacy)
}

func TestNetnsUDPPortsUpstreamHelper(t *testing.T) {
	bind := os.Getenv("B4_UDPPORTS_UPSTREAM")
	if bind == "" {
		t.Skip("helper: a SOCKS5 upstream that runs inside a LAN host's namespace")
	}
	host, portText, err := net.SplitHostPort(bind)
	if err != nil {
		t.Fatalf("B4_UDPPORTS_UPSTREAM: %v", err)
	}
	port, _ := strconv.Atoi(portText)
	log.Init(os.Stderr, log.LevelInfo, true)
	upCfg := config.NewConfig()
	upCfg.System.Socks5.Enabled = true
	upCfg.System.Socks5.BindAddress = host
	upCfg.System.Socks5.Port = port
	upstream := socks5.NewServer(&upCfg)
	if err := upstream.Start(); err != nil {
		t.Fatalf("SOCKS5 upstream: %v", err)
	}
	defer func() { _ = upstream.Stop() }()
	_, _ = os.Stdin.Read(make([]byte, 1))
}

type udpPortsNsLog struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (l *udpPortsNsLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.buf.Write(p)
}

func (l *udpPortsNsLog) count(needle string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return strings.Count(l.buf.String(), needle)
}

func udpPortsNsUpstreamOnNode(t *testing.T, n *gwNode, host string) func() int {
	t.Helper()
	bind := net.JoinHostPort(host, strconv.Itoa(udpPortsNsUpstream))
	cmd := exec.Command("nsenter", "-t", strconv.Itoa(n.pid), "-n", os.Args[0], "-test.run", "^TestNetnsUDPPortsUpstreamHelper$")
	cmd.Env = append(os.Environ(), "B4_UDPPORTS_UPSTREAM="+bind)
	out := &udpPortsNsLog{}
	cmd.Stderr = out
	sessions := func() int { return out.count("SOCKS5 UDP ASSOCIATE from") }
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("upstream helper: %v", err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatalf("upstream helper: %v", err)
	}
	t.Cleanup(func() {
		_ = stdin.Close()
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
	})
	deadline := time.Now().Add(5 * time.Second)
	for {
		conn, err := net.DialTimeout("tcp4", bind, 300*time.Millisecond)
		if err == nil {
			conn.Close()
			return sessions
		}
		if time.Now().After(deadline) {
			t.Fatalf("the SOCKS5 upstream on the LAN host never listened on %s: %v", bind, err)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func netnsAPortRoutedSetSendsALANUpstreamsOwnUDPDirect(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	netnsRequireEngine(t, engine)
	client, inet, _ := quicNsRig(t)
	upstreamHost, _ := quicNsAttachClient(t, quicNsLAN2, quicNsLAN2Peer, quicNsLAN2Rtr, quicNsClient2)
	relayed := udpPortsNsUpstreamOnNode(t, upstreamHost, quicNsClient2)

	cfg := udpPortsNsConfig(engine, udpPortsNsScopeIface, "")
	set := cfg.Sets[0]
	set.Routing.Upstream.Host = quicNsClient2
	set.Routing.SourceInterfaces = []string{quicNsLAN, quicNsLAN2}
	udpPortsNsInstall(t, cfg)
	udpPortsNsListener(t, cfg)

	udpPortsNsServeWhoami(t, inet, "udp4", quicNsMatched, udpPortsNsInRange)
	udpPortsNsServeWhoami(t, inet, "udp4", quicNsUnmatched, udpPortsNsInRange2)
	udpPortsNsServeWhoami(t, inet, "udp4", quicNsMatched, udpPortsNsOutRange)

	viaRouter := "reply=from=" + quicNsWANRouter + " "
	if got := udpPortsNsExchange(client, 0, quicNsUnmatched, udpPortsNsInRange2, "call"); got != viaRouter+"call" {
		netnsLogState(t, engine)
		t.Errorf("a client's UDP through an upstream on a LAN host: %s; the upstream's own datagram for it comes back through the router and must leave from there", got)
	}
	if got := udpPortsNsExchange(upstreamHost, 0, quicNsMatched, udpPortsNsInRange, "transport"); got != viaRouter+"transport" {
		netnsLogState(t, engine)
		t.Errorf("UDP the upstream's own host sends to a listed port at an address no client uses: %s; it must leave from the router", got)
	}
	if n := relayed(); n != 1 {
		t.Errorf("the upstream opened %d UDP sessions, want 1, the client's: UDP from the upstream's own host must leave from the router at once, not travel through the upstream first", n)
	}
	if got := udpPortsNsExchange(upstreamHost, 0, quicNsMatched, udpPortsNsOutRange, "other"); got != "reply=from="+quicNsClient2+" other" {
		t.Errorf("UDP the upstream's own host sends to a port the set does not list: %s; it must pass untouched", got)
	}
}

func TestNetnsAPortRoutedSetSendsALANUpstreamsOwnUDPDirect(t *testing.T) {
	netnsAPortRoutedSetSendsALANUpstreamsOwnUDPDirect(t, backendIPTables)
}

func TestNetnsAPortRoutedSetSendsALANUpstreamsOwnUDPDirectNft(t *testing.T) {
	netnsAPortRoutedSetSendsALANUpstreamsOwnUDPDirect(t, backendNFTables)
}

func TestNetnsAPortRoutedSetSendsALANUpstreamsOwnUDPDirectLegacy(t *testing.T) {
	netnsAPortRoutedSetSendsALANUpstreamsOwnUDPDirect(t, backendIPTablesLegacy)
}
