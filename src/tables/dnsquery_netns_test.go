package tables

import (
	"context"
	"encoding/binary"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/florianl/go-nfqueue"
)

const (
	netnsHookPrerouting = 0
	netnsHookOutput     = 3

	dnsqLink   = "b4dq0"
	dnsqPeer   = "b4dq0p"
	dnsqIP     = "10.203.0.1"
	dnsqPeerIP = "10.203.0.2"
)

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
			if engine == backendIPTables && !dnsQueriesQueuedFromRaw(netnsIptablesBin()) {
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
