package tables

import (
	"errors"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

const (
	quicNsLAN       = "b4qc"
	quicNsLANPeer   = "b4qcn"
	quicNsLANRouter = "10.201.21.1"
	quicNsClient    = "10.201.21.100"
	quicNsWAN       = "b4qw"
	quicNsWANPeer   = "b4qwn"
	quicNsWANRouter = "10.201.22.1"
	quicNsWANGW     = "10.201.22.2"
	quicNsInet      = "198.18.47.0/24"
	quicNsMatched   = "198.18.47.7"
	quicNsUnmatched = "198.18.47.8"
	quicNsSTUN      = 3478
	quicNsSetID     = "netns-quic-proxy"
)

type quicNsEcho struct {
	mu   sync.Mutex
	seen []string
}

func (e *quicNsEcho) got() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.seen...)
}

func quicNsServeEcho(t *testing.T, n *gwNode, host string, port int) *quicNsEcho {
	t.Helper()
	var uc *net.UDPConn
	if err := n.inside(func() (err error) {
		uc, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP(host), Port: port})
		return err
	}); err != nil {
		t.Fatalf("udp listen on %s:%d: %v", host, port, err)
	}
	e := &quicNsEcho{}
	done := make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 512)
		for {
			size, from, err := uc.ReadFromUDP(buf)
			if err != nil {
				return
			}
			e.mu.Lock()
			e.seen = append(e.seen, string(buf[:size]))
			e.mu.Unlock()
			_, _ = uc.WriteToUDP(append([]byte("echo:"), buf[:size]...), from)
		}
	}()
	t.Cleanup(func() {
		_ = uc.Close()
		<-done
	})
	return e
}

func quicNsDial(from *gwNode, dial func() error) error {
	if from == nil {
		return dial()
	}
	return from.inside(dial)
}

func quicNsOutcome(err error) string {
	switch {
	case errors.Is(err, syscall.ECONNREFUSED):
		return "refused"
	case errors.Is(err, syscall.EPERM):
		return "refused locally"
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "timeout"
	}
	return "error: " + err.Error()
}

func quicNsUDP(from *gwNode, host string, port int, msg string) string {
	var conn *net.UDPConn
	if err := quicNsDial(from, func() (err error) {
		conn, err = net.DialUDP("udp4", nil, &net.UDPAddr{IP: net.ParseIP(host), Port: port})
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

func quicNsTCP(from *gwNode, host string, port int, msg string) string {
	var conn net.Conn
	if err := quicNsDial(from, func() (err error) {
		conn, err = net.DialTimeout("tcp4", net.JoinHostPort(host, strconv.Itoa(port)), 2*time.Second)
		return err
	}); err != nil {
		return "dial: " + err.Error()
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(2 * time.Second))
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

func quicNsDiverted(s *netnsSockets, msg string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, item := range s.udp["tproxy"] {
		if strings.HasPrefix(item, msg+"@") {
			return true
		}
	}
	return false
}

func quicNsRig(t *testing.T) (client, inet *gwNode) {
	t.Helper()
	gwKeepSysctl(t, "net/ipv4/ip_forward", "1")
	client, inet = gwStartNode(t), gwStartNode(t)

	client.attach(t, quicNsLAN, quicNsLANPeer)
	netnsRun(t, "ip", "addr", "add", quicNsLANRouter+"/24", "dev", quicNsLAN)
	netnsRun(t, "ip", "link", "set", quicNsLAN, "up")
	client.run(t, "ip", "addr", "add", quicNsClient+"/24", "dev", quicNsLANPeer)
	client.run(t, "ip", "route", "add", "default", "via", quicNsLANRouter)

	inet.attach(t, quicNsWAN, quicNsWANPeer)
	netnsRun(t, "ip", "addr", "add", quicNsWANRouter+"/24", "dev", quicNsWAN)
	netnsRun(t, "ip", "link", "set", quicNsWAN, "up")
	netnsRun(t, "ip", "route", "add", quicNsInet, "via", quicNsWANGW, "dev", quicNsWAN)
	inet.run(t, "ip", "addr", "add", quicNsWANGW+"/24", "dev", quicNsWANPeer)
	for _, ip := range []string{quicNsMatched, quicNsUnmatched} {
		inet.run(t, "ip", "addr", "add", ip+"/32", "dev", "lo")
	}
	inet.run(t, "ip", "route", "add", "default", "via", quicNsWANRouter)
	return client, inet
}

func quicNsBlock(set *config.SetConfig, on bool) {
	if on {
		set.UDP.Mode, set.UDP.FilterQUIC = config.UDPModeReject, config.QUICFilterAll
		return
	}
	set.UDP.Mode, set.UDP.FilterQUIC = config.UDPModeFake, config.QUICFilterSNI
}

func quicNsConfig(engine string) *config.Config {
	cfg := netnsConfig(engine)
	set := config.NewSetConfig()
	set.Id = quicNsSetID
	set.Name = "netnsquic"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = config.RoutingModeProxy
	set.Routing.Upstream.Host = "127.0.0.1"
	set.Routing.Upstream.Port = 31902
	set.Routing.Upstream.UDP = true
	set.Targets.IPs = []string{quicNsMatched}
	set.Targets.IpsToMatch = []string{quicNsMatched}
	quicNsBlock(&set, true)
	cfg.Sets = []*config.SetConfig{&set}
	return cfg
}

func quicNsLogState(t *testing.T, engine string) {
	t.Helper()
	netnsLogState(t, engine)
	if engine == backendIPTables {
		out, _ := run("iptables", "-w", "-t", "filter", "-S")
		t.Logf("filter:\n%s", out)
	}
}

func quicNsChainExists(engine, chain string) bool {
	if engine == backendNFTables {
		_, err := run("nft", "list", "chain", "inet", routeNftTable, chain)
		return err == nil
	}
	_, err := run("iptables", "-w", "-t", "filter", "-S", chain)
	return err == nil
}

func netnsBlockQUICOnAUDPProxySet(t *testing.T, engine string) {
	netnsRequire(t)
	netnsSetupLinks(t)
	if engine == backendNFTables && !hasBinary("nft") {
		t.Skip("nft is not installed")
	}
	client, inet := quicNsRig(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := quicNsConfig(engine)
	set := cfg.Sets[0]
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()
	RoutingSyncConfig(cfg)
	defer RoutingClearAll()

	st, ok := routeRuleCache[quicNsSetID]
	if !ok {
		t.Fatal("the proxy set built no rules")
	}
	port, _ := portFromState(st)
	s := &netnsSockets{tcp: map[string][]string{}, udp: map[string][]string{}}
	netnsServeTCP(t, netnsMarkedTransparentListener(t, port, 0), "tproxy", "tproxy:", s)
	netnsServeUDP(t, netnsTransparentUDP(t, port), "tproxy", s, true)

	matchedQUIC := quicNsServeEcho(t, inet, quicNsMatched, 443)
	matchedSTUN := quicNsServeEcho(t, inet, quicNsMatched, quicNsSTUN)
	quicNsServeEcho(t, inet, quicNsUnmatched, 443)

	except := "udp dport != 443"
	if engine == backendIPTables {
		except = "! --dport 443"
	}
	if pre := netnsPreChain(t, engine, st.chainPre); !strings.Contains(pre, except) {
		t.Errorf("the set blocks QUIC, yet its UDP TPROXY rule carries no exception for UDP 443:\n%s", pre)
	}

	if got := quicNsTCP(client, quicNsMatched, 443, "web"); got != "reply=tproxy:web" {
		t.Errorf("client TCP to a matched address: %s; it must still go to the upstream", got)
	}
	if got := quicNsUDP(client, quicNsMatched, 443, "quic"); got != "refused" {
		quicNsLogState(t, engine)
		t.Errorf("client QUIC to a matched address: %s, want ICMP port unreachable so the browser falls back to TCP at once (diverted to the upstream: %v)", got, quicNsDiverted(s, "quic"))
	}
	got := quicNsUDP(client, quicNsMatched, quicNsSTUN, "stun")
	if !s.got("udp", "tproxy", "stun@"+net.JoinHostPort(quicNsMatched, strconv.Itoa(quicNsSTUN))) {
		t.Errorf("client STUN to a matched address (%s) never reached the transparent socket with its original destination, so it leaves around the upstream: %v", got, s.udp)
	}
	if got := quicNsUDP(client, quicNsUnmatched, 443, "other"); got != "reply=echo:other" {
		t.Errorf("client QUIC to an address the set does not match: %s; it must pass untouched", got)
	}
	if seen := append(matchedQUIC.got(), matchedSTUN.got()...); len(seen) != 0 {
		t.Errorf("the matched address itself received %v; the set's UDP must never leave directly", seen)
	}

	if got := quicNsTCP(nil, quicNsMatched, 443, "own"); got != "reply=tproxy:own" {
		t.Errorf("the router's own TCP to a matched address: %s; it must still go to the upstream", got)
	}
	if got := quicNsUDP(nil, quicNsMatched, 443, "ownquic"); !strings.HasPrefix(got, "refused") {
		quicNsLogState(t, engine)
		t.Errorf("the router's own QUIC to a matched address: %s, want it refused like a client's", got)
	}
	if got := quicNsUDP(nil, quicNsMatched, quicNsSTUN, "ownstun"); strings.HasPrefix(got, "refused") {
		t.Errorf("the router's own UDP to another port of a matched address: %s; only UDP 443 is refused", got)
	}
	if got := quicNsUDP(nil, quicNsUnmatched, 443, "ownother"); got != "reply=echo:ownother" {
		t.Errorf("the router's own QUIC to an address the set does not match: %s; it must pass untouched", got)
	}
	if !RoutingRulesPresent(cfg) {
		t.Error("the monitor finds the rules of a set that blocks QUIC incomplete, so it would rebuild them on every tick")
	}

	quicNsBlock(set, false)
	RoutingSyncConfig(cfg)
	st = routeRuleCache[quicNsSetID]
	if st.quicReject {
		t.Fatal("Block QUIC was turned off, yet the set still refuses QUIC")
	}
	got = quicNsUDP(client, quicNsMatched, 443, "quic2")
	if !s.got("udp", "tproxy", "quic2@"+net.JoinHostPort(quicNsMatched, "443")) {
		quicNsLogState(t, engine)
		t.Errorf("with Block QUIC off, client QUIC to a matched address (%s) never reached the transparent socket", got)
	}
	if quicNsChainExists(engine, st.chainQUIC) {
		t.Errorf("%s outlived Block QUIC", st.chainQUIC)
	}
	if !RoutingRulesPresent(cfg) {
		t.Error("the monitor finds the rules incomplete after Block QUIC was turned off")
	}

	set.Routing.Upstream.UDP = false
	quicNsBlock(set, true)
	RoutingSyncConfig(cfg)
	if got := quicNsUDP(client, quicNsMatched, 443, "quic3"); got != "refused" {
		quicNsLogState(t, engine)
		t.Errorf("with UDP off the upstream, client QUIC to a matched address: %s; it was refused before this change and must still be", got)
	}
	if got := quicNsUDP(client, quicNsMatched, quicNsSTUN, "stun3"); got != "reply=echo:stun3" {
		t.Errorf("with UDP off the upstream, client STUN to a matched address: %s; it leaves directly as before", got)
	}
	if !RoutingRulesPresent(cfg) {
		t.Error("the monitor finds the rules incomplete with UDP off the upstream")
	}
}

func TestNetnsBlockQUICOnAProxySetThatRoutesUDPUpstream(t *testing.T) {
	netnsBlockQUICOnAUDPProxySet(t, backendIPTables)
}

func TestNetnsBlockQUICOnAProxySetThatRoutesUDPUpstreamNft(t *testing.T) {
	netnsBlockQUICOnAUDPProxySet(t, backendNFTables)
}
