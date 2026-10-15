package tables

import (
	"context"
	"encoding/binary"
	"net"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/nflog"
	"github.com/florianl/go-nfqueue"
	"golang.org/x/sys/unix"
)

const (
	netnsHookPrerouting = 0
	netnsHookInput      = 1
	netnsHookOutput     = 3

	netnsDNSCopyGroup = 537

	dnsqLink   = "b4dq0"
	dnsqPeer   = "b4dq0p"
	dnsqIP     = "10.203.0.1"
	dnsqPeerIP = "10.203.0.2"
)

var netnsDNSQueryPayload = []byte{
	0x12, 0x34, 0x01, 0x00, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	7, 'e', 'x', 'a', 'm', 'p', 'l', 'e', 3, 'c', 'o', 'm', 0, 0x00, 0x01, 0x00, 0x01,
}

type netnsDNSCopy struct {
	hook   uint8
	prefix string
	sport  uint16
	dport  uint16
}

type netnsDNSCopies struct {
	conn  *nflog.Conn
	alive bool
}

func netnsCopyDNSQueries(t *testing.T) *netnsDNSCopies {
	t.Helper()
	forgetRawFallbackWarnings(t)
	stubDNSKernel(t, keeneticDNSKernel)
	c := &netnsDNSCopies{alive: true}
	stubDNSCopyListener(t, func() (uint16, error) {
		if c.conn == nil {
			conn, err := nflog.Open(netnsDNSCopyGroup, 1<<20)
			if err != nil {
				return 0, err
			}
			c.conn = conn
		}
		return netnsDNSCopyGroup, nil
	})
	stubDNSCopyAlive(t, func() bool { return c.conn != nil && c.alive })
	t.Cleanup(func() {
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
	return c
}

func netnsRequireDNSCopy(t *testing.T, ipt string) {
	t.Helper()
	if got := dnsQueryPlacementOf(ipt); got != dnsCopied {
		reason, _ := dnsCopyErrors.Load(ipt)
		t.Skipf("DNS queries cannot be copied with NFLOG in this namespace (%v), so they were placed in %s", reason, got)
	}
}

func (c *netnsDNSCopies) collect(t *testing.T, wait time.Duration) []netnsDNSCopy {
	t.Helper()
	var out []netnsDNSCopy
	deadline := time.Now().Add(wait)
	for {
		err := c.conn.Drain(func(p nflog.Packet) {
			pkt := p.Payload
			if len(pkt) < 20 || pkt[0]>>4 != 4 || pkt[9] != 17 {
				return
			}
			ihl := int(pkt[0]&0x0f) * 4
			if len(pkt) < ihl+8 {
				return
			}
			out = append(out, netnsDNSCopy{
				hook:   p.Hook,
				prefix: p.Prefix,
				sport:  binary.BigEndian.Uint16(pkt[ihl : ihl+2]),
				dport:  binary.BigEndian.Uint16(pkt[ihl+2 : ihl+4]),
			})
		})
		if err != nil {
			t.Fatalf("read the NFLOG copies: %v", err)
		}
		if !time.Now().Before(deadline) {
			return out
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func netnsCopiesFrom(copies []netnsDNSCopy, sport int) []netnsDNSCopy {
	var out []netnsDNSCopy
	for _, c := range copies {
		if int(c.sport) == sport && c.dport == 53 {
			out = append(out, c)
		}
	}
	return out
}

func netnsSendDNSQuery(t *testing.T, dst string, mark int) int {
	t.Helper()
	var d net.Dialer
	if mark != 0 {
		d.Control = func(_, _ string, rc syscall.RawConn) error {
			var serr error
			if err := rc.Control(func(fd uintptr) {
				serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_MARK, mark)
			}); err != nil {
				return err
			}
			return serr
		}
	}
	conn, err := d.Dial("udp4", net.JoinHostPort(dst, "53"))
	if err != nil {
		t.Fatalf("dial %s port 53: %v", dst, err)
	}
	defer conn.Close()
	if _, err := conn.Write(netnsDNSQueryPayload); err != nil {
		t.Fatalf("send a DNS query to %s: %v", dst, err)
	}
	return conn.LocalAddr().(*net.UDPAddr).Port
}

func netnsAnswerRulePackets(t *testing.T, ipt string) int {
	t.Helper()
	for _, line := range strings.Split(netnsRun(t, ipt, "-w", "-t", "mangle", "-L", "B4_PREROUTING", "-n", "-v", "-x"), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 || !strings.Contains(line, "spt:53") || !strings.Contains(line, "NFQUEUE") {
			continue
		}
		if n, err := strconv.Atoi(fields[0]); err == nil {
			return n
		}
	}
	t.Fatalf("B4_PREROUTING has no DNS answer rule")
	return 0
}

func netnsDNSQueryLinks(t *testing.T) {
	t.Helper()
	t.Cleanup(func() { _, _ = run("ip", "link", "del", dnsqLink) })
	netnsRun(t, "ip", "link", "set", "lo", "up")
	netnsRun(t, "ip", "link", "add", dnsqLink, "type", "veth", "peer", "name", dnsqPeer)
	netnsRun(t, "ip", "link", "set", dnsqLink, "up")
	netnsRun(t, "ip", "link", "set", dnsqPeer, "up")
	netnsRun(t, "ip", "addr", "add", dnsqIP+"/24", "dev", dnsqLink)
	netnsRun(t, "ip", "neigh", "add", dnsqPeerIP, "lladdr", netnsLinkMAC(t, dnsqPeer), "dev", dnsqLink, "nud", "permanent")
}

type netnsQueuedUDP struct {
	hook  uint8
	sport uint16
	dport uint16
	ct    bool
}

func netnsConntrackQueueListener(t *testing.T, qnum uint16) (func() []netnsQueuedUDP, func()) {
	t.Helper()
	nf, err := nfqueue.Open(&nfqueue.Config{
		NfQueue:      qnum,
		MaxPacketLen: 0xffff,
		MaxQueueLen:  1024,
		Copymode:     nfqueue.NfQnlCopyPacket,
		Flags:        nfqueue.NfQaCfgFlagConntrack,
	})
	if err != nil {
		t.Fatalf("bind queue %d: %v", qnum, err)
	}
	var mu sync.Mutex
	var seen []netnsQueuedUDP
	ctx, cancel := context.WithCancel(context.Background())
	err = nf.RegisterWithErrorFunc(ctx,
		func(a nfqueue.Attribute) int {
			if a.Payload != nil && a.Hook != nil {
				pkt := *a.Payload
				if len(pkt) >= 28 && pkt[0]>>4 == 4 && pkt[9] == 17 {
					ihl := int(pkt[0]&0x0f) * 4
					if len(pkt) >= ihl+8 {
						mu.Lock()
						seen = append(seen, netnsQueuedUDP{
							hook:  *a.Hook,
							sport: binary.BigEndian.Uint16(pkt[ihl : ihl+2]),
							dport: binary.BigEndian.Uint16(pkt[ihl+2 : ihl+4]),
							ct:    a.Ct != nil,
						})
						mu.Unlock()
					}
				}
			}
			if a.PacketID != nil {
				_ = nf.SetVerdict(*a.PacketID, nfqueue.NfAccept)
			}
			return 0
		},
		func(error) int { return 0 },
	)
	if err != nil {
		cancel()
		nf.Close()
		t.Fatalf("register queue %d: %v", qnum, err)
	}
	snapshot := func() []netnsQueuedUDP {
		mu.Lock()
		defer mu.Unlock()
		return append([]netnsQueuedUDP(nil), seen...)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			nf.Close()
		})
	}
	t.Cleanup(stop)
	return snapshot, stop
}

func netnsSendUDP(t *testing.T, srcPort, dstPort int) {
	t.Helper()
	src := &net.UDPAddr{IP: net.ParseIP(dnsqIP), Port: srcPort}
	dst := &net.UDPAddr{IP: net.ParseIP(dnsqPeerIP), Port: dstPort}
	conn, err := net.DialUDP("udp4", src, dst)
	if err != nil {
		t.Fatalf("dial udp %v -> %v: %v", src, dst, err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("b4-dns-placement-probe")); err != nil {
		t.Fatalf("send udp %v -> %v: %v", src, dst, err)
	}
}

func netnsIptablesBin() string {
	if rulesAppliedBackend == backendIPTablesLegacy {
		return backendIPTablesLegacy
	}
	return backendIPTables
}

func netnsWipeDNSQueryRule(t *testing.T, engine string) {
	t.Helper()
	if engine == backendNFTables {
		netnsRun(t, "nft", "flush", "chain", "inet", nftTableName, nftRawOutChain)
		return
	}
	netnsRun(t, append([]string{netnsIptablesBin(), "-w", "-t", "raw", "-D", "OUTPUT"}, iptRawDNSJump()...)...)
}

func netnsDNSQueryRulesGone(t *testing.T, engine string) {
	t.Helper()
	if engine == backendNFTables {
		if out, _ := run("nft", "list", "tables"); strings.Contains(out, nftTableName) {
			t.Errorf("the %s table survived a clear", nftTableName)
		}
		return
	}
	out, _ := run(netnsIptablesBin(), "-w", "-t", "raw", "-S")
	if strings.Contains(out, iptRawChainName) {
		t.Errorf("the raw table still carries b4's DNS query rules after a clear:\n%s", out)
	}
}

func TestNetnsDNSQueriesReachTheQueueBeforeConntrack(t *testing.T) {
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequire(t)
			if engine == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			netnsDNSQueryLinks(t)

			routeEngine = nil
			defer func() { routeEngine = nil }()

			cfg := netnsConfig(engine)
			cfg.Sets = nil
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			cleared := false
			defer func() {
				if !cleared {
					_ = ClearRules(cfg)
				}
			}()
			if engine == backendIPTables && dnsQueryPlacementOf(netnsIptablesBin()) != dnsHeldRaw {
				t.Fatalf("the raw table can queue in this namespace, yet DNS queries were left in mangle")
			}
			if engine == backendNFTables {
				if out := netnsRun(t, "nft", "list", "chain", "inet", nftTableName, nftChainName); !strings.Contains(out, "udp dport 53 return") {
					t.Errorf("the capture chain must leave UDP to port 53 to the DNS query chains:\n%s", out)
				}
			} else if out := netnsRun(t, netnsIptablesBin(), "-w", "-t", "mangle", "-S", iptChainName); !strings.Contains(out, "--dport 53 -j RETURN") {
				t.Errorf("the capture chain must leave UDP to port 53 to the DNS query chain:\n%s", out)
			}

			snapshot, stop := netnsConntrackQueueListener(t, uint16(cfg.Queue.StartNum))
			netnsSendUDP(t, 41053, 53)
			netnsSendUDP(t, 53, 41054)
			time.Sleep(300 * time.Millisecond)
			stop()

			type key struct {
				hook  uint8
				query bool
			}
			got := map[key][]bool{}
			for _, p := range snapshot() {
				switch {
				case p.sport == 41053 && p.dport == 53:
					got[key{p.hook, true}] = append(got[key{p.hook, true}], p.ct)
				case p.sport == 53 && p.dport == 41054:
					got[key{p.hook, false}] = append(got[key{p.hook, false}], p.ct)
				}
			}
			for _, hook := range []uint8{netnsHookOutput, netnsHookPrerouting} {
				queries := got[key{hook, true}]
				if len(queries) == 0 {
					t.Errorf("hook %d: the DNS query never reached b4's queue", hook)
				}
				for _, ct := range queries {
					if ct {
						t.Errorf("hook %d: the DNS query reached the queue with a conntrack entry already attached; a second query from the same socket then clashes with it at confirmation and old kernels drop it", hook)
					}
				}
				answers := got[key{hook, false}]
				if len(answers) == 0 {
					t.Errorf("hook %d: the DNS answer never reached b4's queue", hook)
				}
				for _, ct := range answers {
					if !ct {
						t.Errorf("hook %d: the DNS answer reached the queue without a conntrack entry, so conntrack is not active here and the query check proves nothing", hook)
					}
				}
			}

			m := &Monitor{}
			if !m.checkRules(cfg) {
				t.Fatalf("the monitor finds rules missing right after they were installed")
			}
			netnsWipeDNSQueryRule(t, engine)
			if m.checkRules(cfg) {
				t.Errorf("the monitor did not notice the DNS query rule was wiped")
			}
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules after the wipe: %v", err)
			}
			if !m.checkRules(cfg) {
				t.Errorf("the monitor still finds rules missing after they were put back")
			}

			if err := ClearRules(cfg); err != nil {
				t.Fatalf("ClearRules: %v", err)
			}
			cleared = true
			netnsDNSQueryRulesGone(t, engine)
		})
	}
}

func TestNetnsDNSQueriesAreNotQueuedAgainAfterConntrack(t *testing.T) {
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequire(t)
			if engine == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			netnsDNSQueryLinks(t)

			routeEngine = nil
			defer func() { routeEngine = nil }()

			cfg := netnsConfig(engine)
			cfg.Queue.Interfaces = []string{dnsqLink}
			set := config.NewSetConfig()
			set.Id = "netns-udp53-set"
			set.Name = "udp53"
			set.Enabled = true
			set.UDP.DPortFilter = "53"
			cfg.Sets = []*config.SetConfig{&set}
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			defer func() { _ = ClearRules(cfg) }()
			placement := netnsIptablesBin()
			if engine == backendNFTables {
				placement = backendNFTables
			}
			if dnsQueryPlacementOf(placement) != dnsHeldRaw {
				t.Fatalf("the raw table can queue in this namespace, yet DNS queries were left after conntrack")
			}

			snapshot, stop := netnsConntrackQueueListener(t, uint16(cfg.Queue.StartNum))
			netnsSendUDP(t, 41055, 53)
			time.Sleep(300 * time.Millisecond)
			stop()

			queued := 0
			for _, p := range snapshot() {
				if p.sport != 41055 || p.dport != 53 {
					continue
				}
				queued++
				if p.ct {
					t.Errorf("hook %d: with Capture Interfaces set and a set on UDP port 53, the DNS query was queued again after conntrack; a second query from the same socket then clashes with it at confirmation and old kernels drop it", p.hook)
				}
			}
			if queued == 0 {
				t.Errorf("the DNS query never reached b4's queue")
			}
		})
	}
}

func TestNetnsDNSQueriesAreCopiedNotQueued(t *testing.T) {
	netnsRequire(t)
	netnsDNSQueryLinks(t)
	copies := netnsCopyDNSQueries(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(backendIPTables)
	cfg.Sets = nil
	queue := []string{"-p", "udp", "--dport", "53", "-j", "NFQUEUE", "--queue-num", strconv.Itoa(cfg.Queue.StartNum), "--queue-bypass"}
	netnsRun(t, backendIPTables, "-w", "-t", "mangle", "-N", "B4_PREROUTING")
	netnsRun(t, backendIPTables, "-w", "-t", "mangle", "-I", "PREROUTING", "-j", "B4_PREROUTING")
	netnsRun(t, append([]string{backendIPTables, "-w", "-t", "mangle", "-I", "B4_PREROUTING"}, queue...)...)
	netnsRun(t, append([]string{backendIPTables, "-w", "-t", "mangle", "-I", "OUTPUT"}, queue...)...)
	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules over the rules of a b4 1.86.0 killed with -9: %v", err)
	}
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	cleared := false
	defer func() {
		if !cleared {
			_ = ClearRules(cfg)
		}
	}()
	ipt := netnsIptablesBin()
	netnsRequireDNSCopy(t, ipt)

	for _, line := range strings.Split(netnsRun(t, ipt, "-w", "-t", "mangle", "-S"), "\n") {
		if queuesDNSQueries(line) {
			t.Errorf("b4 copies DNS queries, yet a rule still holds them in its queue (a 1.86.0 leftover or a new one): %s", line)
		}
	}

	copies.collect(t, 0)
	loPort := netnsSendDNSQuery(t, "127.0.0.1", 0)
	wirePort := netnsSendDNSQuery(t, dnsqPeerIP, 0)
	markedLoPort := netnsSendDNSQuery(t, "127.0.0.1", 0x8000)
	markedWirePort := netnsSendDNSQuery(t, dnsqPeerIP, 0x8000)
	got := copies.collect(t, 500*time.Millisecond)

	if lo := netnsCopiesFrom(got, loPort); len(lo) != 1 || lo[0].hook != netnsHookInput || lo[0].prefix != config.DNSQueryCopyPrefix {
		t.Errorf("a query to the router's own resolver must be copied once, at INPUT, with the prefix %s; got %+v", config.DNSQueryCopyPrefix, lo)
	}
	if wire := netnsCopiesFrom(got, wirePort); len(wire) != 1 || wire[0].hook != netnsHookOutput || wire[0].prefix != config.DNSQueryCopyPrefix {
		t.Errorf("a query the router sends out must be copied once, at OUTPUT, with the prefix %s; got %+v", config.DNSQueryCopyPrefix, wire)
	}
	if marked := append(netnsCopiesFrom(got, markedLoPort), netnsCopiesFrom(got, markedWirePort)...); len(marked) != 0 {
		t.Errorf("a query carrying b4's queue mark is b4's own and must not be copied, got %+v", marked)
	}

	before := netnsAnswerRulePackets(t, ipt)
	answer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 53})
	if err != nil {
		t.Fatalf("bind port 53: %v", err)
	}
	_, err = answer.WriteToUDP(netnsDNSQueryPayload, &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 41057})
	answer.Close()
	if err != nil {
		t.Fatalf("send from port 53: %v", err)
	}
	time.Sleep(100 * time.Millisecond)
	if after := netnsAnswerRulePackets(t, ipt); after <= before {
		t.Errorf("a packet from port 53 to a local port must still reach the answer rule in B4_PREROUTING (%d packets before, %d after)", before, after)
	}

	m := &Monitor{}
	if !m.checkRules(cfg) {
		t.Fatalf("the monitor finds rules missing right after they were installed")
	}
	inputJump := []string{"INPUT", "-p", "udp", "--dport", "53", "-j", iptDNSCopyChainName}
	netnsRun(t, append([]string{ipt, "-w", "-t", "mangle", "-D"}, inputJump...)...)
	if m.checkRules(cfg) {
		t.Errorf("the monitor did not notice the INPUT copy jump was deleted")
	}
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules after the wipe: %v", err)
	}
	if _, err := run(append([]string{ipt, "-w", "-t", "mangle", "-C"}, inputJump...)...); err != nil {
		t.Errorf("AddRules did not put the INPUT copy jump back")
	}
	if !m.checkRules(cfg) {
		t.Errorf("the monitor still finds rules missing after they were put back")
	}
	copies.alive = false
	if m.checkRules(cfg) {
		t.Errorf("the monitor did not notice the NFLOG listener stopped, so nothing reads the copies")
	}
	copies.alive = true

	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	cleared = true
	if out := netnsRun(t, ipt, "-w", "-t", "mangle", "-S"); strings.Contains(out, iptDNSCopyChainName) {
		t.Errorf("a clear left the copy rules behind:\n%s", out)
	}
}

func TestNetnsCopiedDNSQueriesAreNeverQueued(t *testing.T) {
	netnsRequire(t)
	netnsDNSQueryLinks(t)
	copies := netnsCopyDNSQueries(t)

	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(backendIPTables)
	cfg.Queue.Interfaces = []string{dnsqLink}
	set := config.NewSetConfig()
	set.Id = "netns-udp53-set"
	set.Name = "udp53"
	set.Enabled = true
	set.UDP.DPortFilter = "53"
	cfg.Sets = []*config.SetConfig{&set}
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()
	netnsRequireDNSCopy(t, netnsIptablesBin())

	copies.collect(t, 0)
	snapshot, stop := netnsConntrackQueueListener(t, uint16(cfg.Queue.StartNum))
	netnsSendUDP(t, 41058, 53)
	time.Sleep(300 * time.Millisecond)
	stop()

	for _, p := range snapshot() {
		if p.sport == 41058 && p.dport == 53 {
			t.Errorf("hook %d: with Capture Interfaces set and a set on UDP port 53, a DNS query b4 copies was also queued, where it waits after conntrack and old kernels drop its twin", p.hook)
		}
	}
	if got := netnsCopiesFrom(copies.collect(t, 200*time.Millisecond), 41058); len(got) != 1 || got[0].hook != netnsHookOutput {
		t.Errorf("the query must be copied once, at OUTPUT, got %+v", got)
	}
}
