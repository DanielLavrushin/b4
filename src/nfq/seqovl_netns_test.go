package nfq

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

const (
	netnsOverlapHost  = "overlap.long-enough-name.example.org"
	netnsOverlapSeg1  = netnsHoldMSS - 12
	netnsOverlapConns = 50
)

func netnsOverlapPad(extLen int) []byte {
	return buildExtension(21, bytes.Repeat([]byte{0xAB}, extLen-4))
}

func netnsOverlapRest() []byte {
	return buildExtension(13, bytes.Repeat([]byte{0xCD}, 300))
}

func netnsOverlapHelloCutInsideTheName(t *testing.T) []byte {
	t.Helper()
	const beforeName, intoName = 10, 14
	short := buildExtension(43, make([]byte, beforeName-4))
	name := buildSNIExtension(netnsOverlapHost)
	at := bytes.Index(setOrderClientHello(netnsOverlapPad(4), short, name, netnsOverlapRest()), name)
	hello := setOrderClientHello(netnsOverlapPad(netnsOverlapSeg1-intoName-at+4), short, name, netnsOverlapRest())
	if got := bytes.Index(hello, name) + intoName; got != netnsOverlapSeg1 {
		t.Fatalf("segment 1 would end at %d, want %d", got, netnsOverlapSeg1)
	}
	return hello
}

func netnsOverlapHelloNameBeforeTheCut(t *testing.T) []byte {
	t.Helper()
	const afterName = 8
	name := buildSNIExtension(netnsOverlapHost)
	end := bytes.Index(setOrderClientHello(netnsOverlapPad(4), name, netnsOverlapRest()), name) + len(name)
	hello := setOrderClientHello(netnsOverlapPad(netnsOverlapSeg1-afterName-end+4), name, netnsOverlapRest())
	if got := bytes.Index(hello, name) + len(name) + afterName; got != netnsOverlapSeg1 {
		t.Fatalf("segment 1 would end at %d, want %d", got, netnsOverlapSeg1)
	}
	return hello
}

type netnsOverlapRead struct {
	got []byte
	err error
}

func netnsOverlapServerIn(t *testing.T, pid, first, second int) <-chan netnsOverlapRead {
	t.Helper()
	var ln net.Listener
	netnsHoldInside(t, pid, func() error {
		var err error
		ln, err = net.Listen("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)))
		return err
	})
	reads := make(chan netnsOverlapRead, 1024)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(5 * time.Second))
				buf := make([]byte, first+second)
				_, err := io.ReadFull(c, buf[:first])
				if err == nil {
					_, err = c.Write([]byte{1})
				}
				if err == nil {
					_, err = io.ReadFull(c, buf[first:])
				}
				reads <- netnsOverlapRead{buf, err}
			}()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	return reads
}

func netnsOverlapSet(overlap int, domainOnly bool) config.SetConfig {
	set := netnsStallSet()
	set.Targets.DomainOnly = domainOnly
	set.Targets.DomainsToMatch = []string{netnsOverlapHost}
	if !domainOnly {
		set.Targets.IpsToMatch = []string{netnsHoldServer}
	}
	set.Fragmentation = config.DefaultSetConfig.Fragmentation
	set.Fragmentation.Strategy = "combo"
	set.Fragmentation.MiddleSNI = true
	set.Fragmentation.SeqOverlapLength = overlap
	set.Fragmentation.SeqOverlapBytes = []byte{0x16, 0x03, 0x03, 0x00, 0x00}
	return set
}

func netnsOverlapStart(t *testing.T, srv int, set *config.SetConfig) {
	t.Helper()
	holdLocalHellosFor(t, time.Second)
	netnsHoldRun(t, "ip", "link", "set", "lo", "up")
	netnsHoldRun(t, "nsenter", "-t", strconv.Itoa(srv), "-n", "iptables", "-w", "-t", "mangle", "-A", "PREROUTING", "-p", "tcp", "--dport", strconv.Itoa(netnsHoldPort), "-m", "ttl", "--ttl-lt", "30", "-j", "DROP")
	cfg := config.NewConfig()
	cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
	cfg.Sets = []*config.SetConfig{set}
	mark := fmt.Sprintf("0x%x/0x%x", cfg.Queue.Mark, cfg.Queue.Mark)
	rule := []string{"-t", "mangle", "OUTPUT", "-o", netnsHoldLink, "-p", "tcp", "--dport", strconv.Itoa(netnsHoldPort), "-m", "mark", "!", "--mark", mark, "-j", "NFQUEUE", "--queue-num", strconv.Itoa(netnsHoldQueue)}
	netnsHoldRun(t, append([]string{"iptables", "-w", rule[0], rule[1], "-A"}, rule[2:]...)...)
	t.Cleanup(func() {
		_ = exec.Command("iptables", append([]string{"-w", rule[0], rule[1], "-D"}, rule[2:]...)...).Run()
	})
	w := newTestWorker(t, &cfg)
	w.qnum = netnsHoldQueue
	if err := w.Start(); err != nil {
		t.Fatalf("bind queue %d: %v", netnsHoldQueue, err)
	}
	t.Cleanup(w.Stop)
}

func netnsOverlapExchange(t *testing.T, reads <-chan netnsOverlapRead, hello []byte, then ...[]byte) uint16 {
	t.Helper()
	want := bytes.Join(append([][]byte{hello}, then...), nil)
	conn, err := netnsHoldDial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(hello); err != nil {
		t.Fatalf("write the ClientHello: %v", err)
	}
	if _, err := io.ReadFull(conn, make([]byte, 1)); err != nil {
		t.Fatalf("the server never answered the ClientHello: %v", err)
	}
	for _, record := range then {
		if _, err := conn.Write(record); err != nil {
			t.Fatalf("write after the ClientHello: %v", err)
		}
	}
	select {
	case r := <-reads:
		if r.err != nil {
			t.Fatalf("the server never read all %d bytes: %v", len(want), r.err)
		}
		if !bytes.Equal(r.got, want) {
			first := 0
			for r.got[first] == want[first] {
				first++
			}
			t.Fatalf("the server read other bytes than the client wrote, from byte %d of %d, segment 1 ends at %d", first, len(want), netnsOverlapSeg1)
		}
	case <-time.After(7 * time.Second):
		t.Fatal("the server reported nothing")
	}
	return uint16(conn.LocalAddr().(*net.TCPAddr).Port)
}

func netnsOverlapConnWire(all []netnsWireSegment, port uint16) (start uint32, seqs map[uint32]bool, ok bool) {
	seqs = map[uint32]bool{}
	for _, s := range all {
		if s.sport != port || s.dport != netnsHoldPort {
			continue
		}
		if !ok {
			start, ok = s.seq+1, true
		}
		if s.payload > 0 {
			seqs[s.seq] = true
		}
	}
	return start, seqs, ok
}

func TestNetnsAnOverlapOnASplitHelloLeavesEverySegmentsBytesAlone(t *testing.T) {
	for _, layout := range []struct {
		name  string
		hello func(*testing.T) []byte
	}{
		{"the cut inside the name's extension", netnsOverlapHelloCutInsideTheName},
		{"the whole name just before the cut", netnsOverlapHelloNameBeforeTheCut},
	} {
		for _, domainOnly := range []bool{false, true} {
			for _, overlap := range []int{64, 681} {
				t.Run(fmt.Sprintf("%s, domain only %v, overlap %d", layout.name, domainOnly, overlap), func(t *testing.T) {
					netnsHoldRequire(t)
					srv := netnsHoldStartPeer(t)
					hello := layout.hello(t)
					reads := netnsOverlapServerIn(t, srv, len(hello), 0)
					wire := netnsHoldCapture(t, srv)
					set := netnsOverlapSet(overlap, domainOnly)
					netnsOverlapStart(t, srv, &set)

					var ports []uint16
					for range netnsOverlapConns {
						ports = append(ports, netnsOverlapExchange(t, reads, hello))
					}
					time.Sleep(50 * time.Millisecond)

					seen := wire()
					for _, port := range ports {
						start, seqs, ok := netnsOverlapConnWire(seen, port)
						if !ok {
							t.Fatalf("nothing of the connection from port %d on the wire", port)
						}
						segment2 := start + netnsOverlapSeg1
						if !seqs[segment2] {
							t.Fatalf("no piece starts at segment 2 in %v, so the ClientHello did not leave in two segments and this test proves nothing", seqs)
						}
						if !seqs[start-uint32(overlap)] {
							t.Fatal("segment 1 lost its overlap, the fix must only take it off segment 2")
						}
						if seqs[segment2-uint32(overlap)] {
							t.Fatal("segment 2 still carries an overlap that reaches back over segment 1")
						}
					}
				})
			}
		}
	}
}

func TestNetnsAnOverlapLeavesTheDataAfterTheHelloAlone(t *testing.T) {
	netnsHoldRequire(t)
	const overlap = 64
	srv := netnsHoldStartPeer(t)
	hello := buildClientHello(netnsOverlapHost, 100, 0xAB)
	record := func(fill byte, length int) []byte {
		return append([]byte{0x17, 0x03, 0x03, byte(length >> 8), byte(length)}, bytes.Repeat([]byte{fill}, length)...)
	}
	one, two := record(0x41, 25), record(0x42, 200)
	reads := netnsOverlapServerIn(t, srv, len(hello), len(one)+len(two))
	wire := netnsHoldCapture(t, srv)
	set := netnsOverlapSet(overlap, false)
	netnsOverlapStart(t, srv, &set)

	var ports []uint16
	for range netnsOverlapConns {
		ports = append(ports, netnsOverlapExchange(t, reads, hello, one, two))
	}
	time.Sleep(50 * time.Millisecond)

	seen := wire()
	for _, port := range ports {
		start, seqs, ok := netnsOverlapConnWire(seen, port)
		if !ok {
			t.Fatalf("nothing of the connection from port %d on the wire", port)
		}
		if !seqs[start-overlap] {
			t.Fatal("a ClientHello in one segment lost its overlap")
		}
	}
}

func netnsOverlapRequest(path string) []byte {
	return []byte("GET " + path + " HTTP/1.1\r\nHost: " + netnsOverlapHost + "\r\nUser-Agent: b4-netns-overlap\r\nAccept: */*\r\n\r\n")
}

func netnsOverlapConnect() []byte {
	return []byte("CONNECT " + netnsOverlapHost + ":443 HTTP/1.1\r\nHost: " + netnsOverlapHost + ":443\r\n\r\n")
}

func netnsOverlapConverse(t *testing.T, reads <-chan netnsOverlapRead, first, second []byte, pipelined bool) (uint16, bool) {
	t.Helper()
	conn, err := netnsHoldDial()
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err := conn.Write(first); err != nil {
		t.Fatalf("write the first message: %v", err)
	}
	if !pipelined {
		if _, err := io.ReadFull(conn, make([]byte, 1)); err != nil {
			t.Fatalf("the server never answered the first message: %v", err)
		}
	}
	if _, err := conn.Write(second); err != nil {
		t.Fatalf("write the second message: %v", err)
	}
	port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
	select {
	case r := <-reads:
		return port, r.err == nil && bytes.Equal(r.got, bytes.Join([][]byte{first, second}, nil))
	case <-time.After(7 * time.Second):
		t.Fatal("the server reported nothing")
	}
	return port, false
}

func TestNetnsAnOverlapStaysOffALaterMessageOfTheConnection(t *testing.T) {
	hello := buildClientHello(netnsOverlapHost, 100, 0xAB)
	for _, c := range []struct {
		name          string
		first, second []byte
		pipelined     bool
	}{
		{"an HTTP request pipelined behind the first", netnsOverlapRequest("/one"), netnsOverlapRequest("/two"), true},
		{"an HTTP request after the answer to the first", netnsOverlapRequest("/one"), netnsOverlapRequest("/two"), false},
		{"a ClientHello right after CONNECT", netnsOverlapConnect(), hello, true},
		{"a ClientHello after the CONNECT answer", netnsOverlapConnect(), hello, false},
	} {
		for _, overlap := range []int{64, 256} {
			t.Run(fmt.Sprintf("%s, overlap %d", c.name, overlap), func(t *testing.T) {
				netnsHoldRequire(t)
				srv := netnsHoldStartPeer(t)
				reads := netnsOverlapServerIn(t, srv, len(c.first), len(c.second))
				wire := netnsHoldCapture(t, srv)
				set := netnsOverlapSet(overlap, false)
				netnsOverlapStart(t, srv, &set)

				var ports []uint16
				wrong := 0
				for range netnsOverlapConns {
					port, intact := netnsOverlapConverse(t, reads, c.first, c.second, c.pipelined)
					ports = append(ports, port)
					if !intact {
						wrong++
					}
				}
				time.Sleep(50 * time.Millisecond)
				if wrong > 0 {
					t.Errorf("the server read other bytes than the client wrote on %d of %d connections", wrong, netnsOverlapConns)
				}

				seen := wire()
				carried := 0
				for _, port := range ports {
					start, seqs, ok := netnsOverlapConnWire(seen, port)
					if !ok {
						t.Fatalf("nothing of the connection from port %d on the wire", port)
					}
					if !seqs[start-uint32(overlap)] {
						t.Fatal("the connection's first message lost its overlap")
					}
					if seqs[start+uint32(len(c.first))-uint32(overlap)] {
						carried++
					}
				}
				if carried > 0 {
					t.Errorf("the second message carried an overlap that reaches back over the first on %d of %d connections", carried, netnsOverlapConns)
				}
			})
		}
	}
}
