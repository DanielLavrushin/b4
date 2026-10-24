package tables

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
	"golang.org/x/sys/unix"
)

const (
	netnsTCPFlagRST  = 0x04
	netnsHelloSize   = 300
	netnsReplyChunk  = 200
	netnsReplyChunks = 3
	netnsLateChunk   = 100
	netnsForgedRSTID = 0xb4b4
)

func netnsInjectorMark(queueMark uint) uint32 {
	return uint32(queueMark) | engine.InjectedMarkBit
}

type netnsB4Emulation struct {
	mu       sync.Mutex
	queued   []netnsQueued
	injected bool
	dropped  []netnsSegment
}

func (e *netnsB4Emulation) snapshot() ([]netnsQueued, []netnsSegment) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]netnsQueued(nil), e.queued...), append([]netnsSegment(nil), e.dropped...)
}

func netnsEmulateB4(t *testing.T, qnum uint16, injectMark uint32, copies int) (*netnsB4Emulation, func()) {
	t.Helper()
	sender, err := sock.NewSenderWithMark(int(injectMark))
	if err != nil {
		t.Fatalf("raw sender with mark %#x: %v", injectMark, err)
	}
	nf, err := nfqueue.Open(&nfqueue.Config{
		NfQueue:      qnum,
		MaxPacketLen: 0xffff,
		MaxQueueLen:  1024,
		Copymode:     nfqueue.NfQnlCopyPacket,
	})
	if err != nil {
		sender.Close()
		t.Fatalf("bind queue %d: %v", qnum, err)
	}
	e := &netnsB4Emulation{}
	ctx, cancel := context.WithCancel(context.Background())
	err = nf.RegisterWithErrorFunc(ctx,
		func(a nfqueue.Attribute) int {
			if a.PacketID == nil || a.Payload == nil {
				return 0
			}
			pkt := append([]byte(nil), *a.Payload...)
			var hook uint8
			if a.Hook != nil {
				hook = *a.Hook
			}
			verdict := nfqueue.NfAccept
			var inject []byte
			s, ok := netnsParseIPv4(pkt)
			e.mu.Lock()
			e.queued = append(e.queued, netnsQueued{hook: hook, pkt: pkt})
			switch {
			case !ok || s.proto != unix.IPPROTO_TCP:
			case s.dport == netnsSrvPort && len(s.payload) > 0 && !e.injected && copies > 0:
				e.injected = true
				verdict, inject = nfqueue.NfDrop, append([]byte(nil), pkt...)
			case s.sport == netnsSrvPort && s.flags&netnsTCPFlagRST != 0:
				verdict = nfqueue.NfDrop
				e.dropped = append(e.dropped, s)
			}
			e.mu.Unlock()
			_ = nf.SetVerdict(*a.PacketID, verdict)
			if inject != nil {
				sock.FixTCPChecksum(inject)
				for range copies {
					_ = sender.SendIPv4(inject, net.IP(inject[16:20]))
				}
			}
			return 0
		},
		func(error) int { return 0 },
	)
	if err != nil {
		cancel()
		nf.Close()
		sender.Close()
		t.Fatalf("register queue %d: %v", qnum, err)
	}
	var once sync.Once
	stop := func() {
		once.Do(func() {
			cancel()
			nf.Close()
			sender.Close()
		})
	}
	t.Cleanup(stop)
	return e, stop
}

func netnsForgedRST(src, dst net.IP, sport, dport uint16, seq uint32) []byte {
	pkt := make([]byte, 40)
	pkt[0] = 0x45
	binary.BigEndian.PutUint16(pkt[2:4], 40)
	binary.BigEndian.PutUint16(pkt[4:6], netnsForgedRSTID)
	pkt[8] = 64
	pkt[9] = unix.IPPROTO_TCP
	copy(pkt[12:16], src.To4())
	copy(pkt[16:20], dst.To4())
	binary.BigEndian.PutUint16(pkt[20:22], sport)
	binary.BigEndian.PutUint16(pkt[22:24], dport)
	binary.BigEndian.PutUint32(pkt[24:28], seq)
	pkt[32] = 5 << 4
	pkt[33] = netnsTCPFlagRST
	sock.FixIPv4Checksum(pkt[:20])
	sock.FixTCPChecksum(pkt)
	return pkt
}

func netnsSendRawFrom(t *testing.T, pid int, pkt []byte) {
	t.Helper()
	netnsInside(t, pid, func() error {
		fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_RAW)
		if err != nil {
			return err
		}
		defer unix.Close(fd)
		var to unix.SockaddrInet4
		copy(to.Addr[:], pkt[16:20])
		return unix.Sendto(fd, pkt, 0, &to)
	})
}

type netnsResponder struct {
	release chan struct{}
	once    sync.Once
}

func (r *netnsResponder) sendLate() {
	r.once.Do(func() { close(r.release) })
}

func netnsResponderIn(t *testing.T, pid int) *netnsResponder {
	t.Helper()
	var ln net.Listener
	netnsInside(t, pid, func() error {
		var err error
		ln, err = net.Listen("tcp4", net.JoinHostPort(netnsSrvIP, strconv.Itoa(netnsSrvPort)))
		return err
	})
	r := &netnsResponder{release: make(chan struct{})}
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(15 * time.Second))
		if _, err := io.ReadFull(c, make([]byte, netnsHelloSize)); err != nil {
			return
		}
		for range netnsReplyChunks {
			if _, err := c.Write(bytes.Repeat([]byte{'A'}, netnsReplyChunk)); err != nil {
				return
			}
			time.Sleep(50 * time.Millisecond)
		}
		select {
		case <-r.release:
		case <-time.After(10 * time.Second):
			return
		}
		if _, err := c.Write(bytes.Repeat([]byte{'B'}, netnsLateChunk)); err != nil {
			return
		}
		_, _ = io.Copy(io.Discard, c)
	}()
	t.Cleanup(func() {
		r.sendLate()
		ln.Close()
		wg.Wait()
	})
	return r
}

func TestNetnsB4SeesTheResetsOfConnectionsItSendsInto(t *testing.T) {
	for _, engineName := range []string{backendIPTables, backendNFTables} {
		for _, tc := range []struct {
			name           string
			injectMark     func(q uint) uint32
			incoming       bool
			rstQueued      bool
			serverDataSeen bool
			lateDataSeen   bool
		}{
			{"b4's injector", netnsInjectorMark, false, true, false, false},
			{"b4's injector with an incoming mode set", netnsInjectorMark, true, true, true, true},
			{"the injector mark of earlier versions", func(q uint) uint32 { return uint32(q) }, false, false, false, true},
		} {
			t.Run(engineName+"/"+tc.name, func(t *testing.T) {
				netnsRequire(t)
				if engineName == backendNFTables && !hasBinary("nft") {
					t.Skip("nft is not installed")
				}
				srv := netnsStartPeer(t, netnsSrvLink, netnsSrvPeer, netnsSrvRouter, netnsSrvIP)
				responder := netnsResponderIn(t, srv)

				routeEngine = nil
				defer func() { routeEngine = nil }()

				cfg := netnsConfig(engineName)
				cfg.Sets = nil
				if tc.incoming {
					set := config.NewSetConfig()
					set.Id, set.Name, set.Enabled = "incoming", "incoming", true
					set.TCP.Incoming.Mode = "fake"
					cfg.Sets = []*config.SetConfig{&set}
				}
				if err := AddRules(cfg); err != nil {
					t.Fatalf("AddRules: %v", err)
				}
				defer func() { _ = ClearRules(cfg) }()

				b4, stop := netnsEmulateB4(t, uint16(cfg.Queue.StartNum), tc.injectMark(cfg.Queue.Mark), 2)
				conn, err := netnsDialServer()
				if err != nil {
					t.Fatalf("dial: %v", err)
				}
				defer conn.Close()
				port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
				_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
				if _, err := conn.Write(bytes.Repeat([]byte{0x16}, netnsHelloSize)); err != nil {
					t.Fatalf("write: %v", err)
				}
				if _, err := io.ReadFull(conn, make([]byte, netnsReplyChunks*netnsReplyChunk)); err != nil {
					t.Fatalf("read the server's answer: %v", err)
				}

				queued, _ := b4.snapshot()
				var isn uint32
				found := false
				for _, q := range queued {
					s, ok := netnsParseIPv4(q.pkt)
					if ok && s.proto == unix.IPPROTO_TCP && s.sport == netnsSrvPort && s.dport == port && s.flags&(netnsTCPFlagSYN|netnsTCPFlagACK) == netnsTCPFlagSYN|netnsTCPFlagACK {
						isn, found = s.seq, true
					}
				}
				if !found {
					t.Fatalf("the SYN-ACK never reached the queue, so the forged reset cannot be placed in the window")
				}
				seq := isn + 1 + netnsReplyChunks*netnsReplyChunk
				netnsSendRawFrom(t, srv, netnsForgedRST(net.ParseIP(netnsSrvIP), net.ParseIP(netnsSrvRouter), netnsSrvPort, port, seq))
				time.Sleep(300 * time.Millisecond)
				responder.sendLate()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				_, lateErr := io.ReadFull(conn, make([]byte, netnsLateChunk))
				time.Sleep(100 * time.Millisecond)
				stop()

				queued, dropped := b4.snapshot()
				rst, serverData, lateData := 0, 0, 0
				for _, q := range queued {
					s, ok := netnsParseIPv4(q.pkt)
					if !ok || s.proto != unix.IPPROTO_TCP || s.sport != netnsSrvPort || s.dport != port {
						continue
					}
					if s.flags&netnsTCPFlagRST != 0 && s.id == netnsForgedRSTID && s.seq == seq {
						rst++
						if q.hook != netnsHookPrerouting {
							t.Errorf("the forged reset was queued at hook %d, want %d", q.hook, netnsHookPrerouting)
						}
					}
					switch {
					case len(s.payload) > 0 && s.payload[0] == 'B':
						lateData++
					case len(s.payload) > 0:
						serverData++
					}
				}
				if !b4.injected {
					t.Fatalf("the client's first data packet never reached the queue, so nothing was sent into the connection and this test proves nothing")
				}
				switch {
				case tc.rstQueued && (rst != 1 || len(dropped) != 1):
					t.Errorf("the forged reset reached the queue %d time(s) and %d reset(s) were dropped; b4 must be handed the resets of a connection it sent packets into, or RST protection never acts on it", rst, len(dropped))
				case !tc.rstQueued && rst != 0:
					t.Errorf("the forged reset reached the queue %d time(s); with this injector mark the connection carries the queue mark, which returns every server packet that reaches the router's socket first", rst)
				}
				switch {
				case tc.rstQueued && lateErr != nil:
					t.Errorf("the connection did not survive the dropped reset: %v", lateErr)
				case !tc.rstQueued && lateErr == nil:
					t.Errorf("the forged reset never reached b4, yet the connection read the late answer; the reset was not in the window and this control proves nothing")
				}
				switch {
				case tc.serverDataSeen && serverData == 0:
					t.Errorf("an incoming mode set needs the server's data, yet none of it reached the queue")
				case !tc.serverDataSeen && serverData != 0:
					t.Errorf("%d data packets of the server reached the queue; without an incoming mode set b4 has no use for them, and queueing them reorders them at the window edge", serverData)
				}
				switch {
				case tc.lateDataSeen && lateData == 0 && tc.incoming:
					t.Errorf("an incoming mode set needs the server's data, yet its late answer never reached the queue")
				case tc.lateDataSeen && lateData == 0:
					t.Errorf("the reset closed the router's socket, so the server's late answer reaches no socket there and must go to the queue rules like a device's; it never reached the queue")
				case !tc.lateDataSeen && lateData != 0:
					t.Errorf("%d late data packets of the server reached the queue on a connection that is still open; without an incoming mode set b4 has no use for them", lateData)
				}
				t.Logf("forged reset queued %d time(s), dropped %d, server data packets queued %d, late ones %d, late read error %v", rst, len(dropped), serverData, lateData, lateErr)
			})
		}
	}
}

func netnsListedRules(t *testing.T, chain string) []string {
	t.Helper()
	var rules []string
	for _, line := range strings.Split(netnsRun(t, "iptables", "-w", "-t", "mangle", "-S", chain), "\n") {
		if strings.HasPrefix(line, "-A "+chain+" ") {
			rules = append(rules, line)
		}
	}
	return rules
}

func netnsCountListed(rules []string, parts ...string) int {
	n := 0
	for _, r := range rules {
		all := true
		for _, p := range parts {
			all = all && strings.Contains(r, p)
		}
		if all {
			n++
		}
	}
	return n
}

func TestNetnsTheRulesForConnectionsB4SendsIntoGoInOnceAndLeave(t *testing.T) {
	netnsRequire(t)
	routeEngine = nil
	defer func() { routeEngine = nil }()

	cfg := netnsConfig(backendIPTables)
	cfg.Sets = nil
	earlier := strings.Fields("-m mark --mark 0x8000/0x20008000 -j CONNMARK --save-mark --nfmask 0x8000 --ctmask 0x8000")
	netnsRun(t, append([]string{"iptables", "-w", "-t", "mangle", "-I", "OUTPUT"}, earlier...)...)
	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if err := AddRules(cfg); err != nil {
		t.Fatalf("AddRules: %v", err)
	}
	defer func() { _ = ClearRules(cfg) }()
	if err := AddRules(cfg); err != nil {
		t.Fatalf("a second AddRules, as a restore runs it: %v", err)
	}

	output := netnsListedRules(t, "OUTPUT")
	pre := netnsListedRules(t, "B4_PREROUTING")
	b4 := netnsListedRules(t, iptChainName)
	for _, c := range []struct {
		what  string
		rules []string
		parts []string
	}{
		{"the save for b4's own connections", output, []string{"--mark 0x8000/0x22008000", "CONNMARK", "--nfmask 0x8000"}},
		{"the save for the connections b4 sends into", output, []string{"--mark 0x2008000/0x22008000", "CONNMARK", "--nfmask 0x2000000"}},
		{"the return for the connections b4 sends into", pre, []string{"--mark 0x2000000/0x2008000", "--tcp-flags SYN,RST NONE", "-j RETURN"}},
	} {
		if got := netnsCountListed(c.rules, c.parts...); got != 1 {
			t.Errorf("%s is listed %d times after two applies, want once; iptables -C does not find it by the spec b4 emits: %q", c.what, got, c.rules)
		}
	}
	if got := netnsCountListed(b4, "connmark"); got != 0 {
		t.Errorf("B4 selects %d rules by the connection mark; the connections b4 sends into share the queue window of every other connection: %q", got, b4)
	}
	if got := netnsCountListed(output, "0x8000/0x20008000"); got != 0 {
		t.Errorf("the CONNMARK save of earlier versions survived the clear and apply of a start: %q", output)
	}
	inj := slices.IndexFunc(pre, func(r string) bool { return strings.Contains(r, "--mark 0x2000000/0x2008000") })
	own := slices.IndexFunc(pre, func(r string) bool {
		return strings.Contains(r, "--mark 0x8000/0x8000") && strings.Contains(r, "-j RETURN")
	})
	queue := slices.IndexFunc(pre, func(r string) bool { return strings.Contains(r, "NFQUEUE") })
	if own < 0 || inj != own+1 || queue < inj {
		t.Errorf("B4_PREROUTING order: return for b4's own connections %d, for the ones it sends into %d, first queue rule %d: %q", own, inj, queue, pre)
	}

	if err := ClearRules(cfg); err != nil {
		t.Fatalf("ClearRules: %v", err)
	}
	if left := netnsCountListed(netnsListedRules(t, "OUTPUT"), "CONNMARK"); left != 0 {
		t.Errorf("%d CONNMARK rules stay in mangle OUTPUT after the clear", left)
	}
}
