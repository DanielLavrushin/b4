package tables

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/florianl/go-nfqueue"
)

const (
	netnsHookPostrouting = 4

	netnsDirectionServerPort = 443
)

type netnsQueuedTCP struct {
	hook  uint8
	sport uint16
	dport uint16
}

func netnsTCPQueueListener(t *testing.T, qnum uint16) (func() []netnsQueuedTCP, func()) {
	t.Helper()
	nf, err := nfqueue.Open(&nfqueue.Config{
		NfQueue:      qnum,
		MaxPacketLen: 0xffff,
		MaxQueueLen:  1024,
		Copymode:     nfqueue.NfQnlCopyPacket,
	})
	if err != nil {
		t.Fatalf("bind queue %d: %v", qnum, err)
	}
	var mu sync.Mutex
	var seen []netnsQueuedTCP
	ctx, cancel := context.WithCancel(context.Background())
	err = nf.RegisterWithErrorFunc(ctx,
		func(a nfqueue.Attribute) int {
			if a.Payload != nil && a.Hook != nil {
				pkt := *a.Payload
				if len(pkt) >= 40 && pkt[0]>>4 == 4 && pkt[9] == 6 {
					ihl := int(pkt[0]&0x0f) * 4
					if len(pkt) >= ihl+4 {
						mu.Lock()
						seen = append(seen, netnsQueuedTCP{
							hook:  *a.Hook,
							sport: binary.BigEndian.Uint16(pkt[ihl : ihl+2]),
							dport: binary.BigEndian.Uint16(pkt[ihl+2 : ihl+4]),
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
	snapshot := func() []netnsQueuedTCP {
		mu.Lock()
		defer mu.Unlock()
		return append([]netnsQueuedTCP(nil), seen...)
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

func netnsCtdirRuleCount(t *testing.T) map[string]int {
	t.Helper()
	counts := map[string]int{}
	for _, chain := range []string{iptChainName, "B4_PREROUTING"} {
		for _, line := range strings.Split(netnsRun(t, netnsIptablesBin(), "-w", "-t", "mangle", "-S", chain), "\n") {
			if strings.Contains(line, "--ctdir") && strings.Contains(line, "NFQUEUE") {
				counts[chain]++
			}
		}
	}
	return counts
}

func netnsTCPEcho(t *testing.T) uint16 {
	t.Helper()
	ln, err := net.Listen("tcp4", net.JoinHostPort(dnsqIP, strconv.Itoa(netnsDirectionServerPort)))
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(5 * time.Second))
		buf := make([]byte, 64)
		n, err := c.Read(buf)
		if err != nil {
			return
		}
		_, _ = c.Write(buf[:n])
	}()

	d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(dnsqIP)}, Timeout: 5 * time.Second}
	conn, err := d.Dial("tcp4", net.JoinHostPort(dnsqIP, strconv.Itoa(netnsDirectionServerPort)))
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	clientPort := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write([]byte("b4-direction-probe")); err != nil {
		t.Fatalf("write: %v", err)
	}
	buf := make([]byte, 64)
	if _, err := io.ReadAtLeast(conn, buf, 1); err != nil {
		t.Fatalf("read the echo: %v", err)
	}
	conn.Close()
	<-done
	return clientPort
}

func TestNetnsQueueHookFollowsConntrackDirection(t *testing.T) {
	for _, engine := range []string{backendIPTables, backendNFTables} {
		t.Run(engine, func(t *testing.T) {
			netnsRequire(t)
			if engine == backendNFTables && !hasBinary("nft") {
				t.Skip("nft is not installed")
			}
			netnsDNSQueryLinks(t)

			routeEngine = nil
			defer func() { routeEngine = nil }()

			var v4, v6 bool
			var published int
			queueDirectionMu.Lock()
			origHook := QueueDirectionExactFunc
			QueueDirectionExactFunc = func(a, b bool) { v4, v6, published = a, b, published+1 }
			queueDirectionMu.Unlock()
			defer func() {
				queueDirectionMu.Lock()
				QueueDirectionExactFunc = origHook
				queueDirectionMu.Unlock()
			}()

			cfg := netnsConfig(engine)
			set := config.NewSetConfig()
			set.Id, set.Name, set.Enabled = "netns-wide-ports", "wide", true
			set.TCP.DPortFilter = "1-65535"
			cfg.Sets = []*config.SetConfig{&set}
			if err := AddRules(cfg); err != nil {
				t.Fatalf("AddRules: %v", err)
			}
			defer func() { _ = ClearRules(cfg) }()
			wantV6 := engine == backendNFTables
			if published == 0 || !v4 || v6 != wantV6 {
				t.Fatalf("after the apply IPv4 published exact=%v, IPv6 exact=%v (%d publications), want IPv4 exact and IPv6 %v: this kernel takes the conntrack direction match", v4, v6, published, wantV6)
			}
			if engine == backendIPTables {
				before := netnsCtdirRuleCount(t)
				if before[iptChainName] == 0 || before["B4_PREROUTING"] == 0 {
					t.Fatalf("no queue rule carries the conntrack direction match: %v", before)
				}
				if err := AddRules(cfg); err != nil {
					t.Fatalf("restore: %v", err)
				}
				if after := netnsCtdirRuleCount(t); after[iptChainName] != before[iptChainName] || after["B4_PREROUTING"] != before["B4_PREROUTING"] {
					t.Fatalf("a restore of the same rules changed the direction-matching rules from %v to %v: iptables -C does not recognise the spec b4 emits, so every restore appends them again", before, after)
				}
			}

			snapshot, stop := netnsTCPQueueListener(t, uint16(cfg.Queue.StartNum))
			clientPort := netnsTCPEcho(t)
			time.Sleep(300 * time.Millisecond)
			stop()

			var client, server int
			hooks := map[string]int{}
			for _, p := range snapshot() {
				switch {
				case p.sport == clientPort && p.dport == netnsDirectionServerPort:
					client++
					hooks["client@"+strconv.Itoa(int(p.hook))]++
					if p.hook != netnsHookOutput && p.hook != netnsHookPostrouting {
						t.Errorf("a client packet was queued at hook %d; with a port filter covering the client's port only the outgoing hooks may queue it", p.hook)
					}
				case p.sport == netnsDirectionServerPort && p.dport == clientPort:
					server++
					hooks["server@"+strconv.Itoa(int(p.hook))]++
					if p.hook != netnsHookPrerouting {
						t.Errorf("a server reply was queued at hook %d; only prerouting may queue a reply", p.hook)
					}
				}
			}
			t.Logf("queued packets by side and hook: %v", hooks)
			if client == 0 || server == 0 {
				t.Fatalf("queued %d client and %d server packets; both directions must reach the queue or this test proves nothing", client, server)
			}
		})
	}
}
