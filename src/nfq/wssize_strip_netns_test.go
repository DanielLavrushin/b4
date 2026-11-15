package nfq

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"golang.org/x/sys/unix"
)

const netnsWSOptWindowScale = 4

type netnsWSServed struct {
	scaled bool
	err    error
}

func netnsWSScalingAgreed(c net.Conn) (bool, error) {
	raw, err := c.(*net.TCPConn).SyscallConn()
	if err != nil {
		return false, err
	}
	var info *unix.TCPInfo
	var ierr error
	if err := raw.Control(func(fd uintptr) {
		info, ierr = unix.GetsockoptTCPInfo(int(fd), unix.IPPROTO_TCP, unix.TCP_INFO)
	}); err != nil {
		return false, err
	}
	if ierr != nil {
		return false, ierr
	}
	return info.Options&netnsWSOptWindowScale != 0, nil
}

func netnsWSAnsweringServerIn(t *testing.T, pid, helloLen, answer int) <-chan netnsWSServed {
	t.Helper()
	var ln net.Listener
	netnsHoldInside(t, pid, func() error {
		var err error
		ln, err = net.Listen("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)))
		return err
	})
	served := make(chan netnsWSServed, 1)
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		c, err := ln.Accept()
		if err != nil {
			served <- netnsWSServed{err: err}
			return
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(20 * time.Second))
		if _, err := io.ReadFull(c, make([]byte, helloLen)); err != nil {
			served <- netnsWSServed{err: err}
			return
		}
		scaled, err := netnsWSScalingAgreed(c)
		if err == nil {
			_, err = c.Write(make([]byte, answer))
		}
		served <- netnsWSServed{scaled: scaled, err: err}
	}()
	t.Cleanup(func() {
		ln.Close()
		wg.Wait()
	})
	return served
}

func netnsWSCaptureSent(t *testing.T, pid int) func() []netnsWireSegment {
	t.Helper()
	proto := binary.NativeEndian.Uint16(binary.BigEndian.AppendUint16(nil, unix.ETH_P_ALL))
	fd := -1
	netnsHoldInside(t, pid, func() error {
		iface, err := net.InterfaceByName(netnsHoldPeer)
		if err != nil {
			return err
		}
		if fd, err = unix.Socket(unix.AF_PACKET, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, int(proto)); err != nil {
			return err
		}
		if err := unix.Bind(fd, &unix.SockaddrLinklayer{Protocol: proto, Ifindex: iface.Index}); err != nil {
			return err
		}
		return unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_RCVTIMEO, &unix.Timeval{Usec: 50000})
	})
	var mu sync.Mutex
	var seen []netnsWireSegment
	stop, done := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(done)
		buf := make([]byte, 262144)
		for {
			select {
			case <-stop:
				return
			default:
			}
			n, from, err := unix.Recvfrom(fd, buf, 0)
			if err != nil {
				continue
			}
			if ll, ok := from.(*unix.SockaddrLinklayer); !ok || ll.Pkttype != unix.PACKET_OUTGOING {
				continue
			}
			if s, ok := netnsHoldParse(buf[:n]); ok {
				mu.Lock()
				seen = append(seen, s)
				mu.Unlock()
			}
		}
	}()
	var once sync.Once
	collect := func() []netnsWireSegment {
		once.Do(func() {
			close(stop)
			<-done
			_ = unix.Close(fd)
		})
		mu.Lock()
		defer mu.Unlock()
		return append([]netnsWireSegment(nil), seen...)
	}
	t.Cleanup(func() { collect() })
	return collect
}

func TestNetnsWSSizeStripScaleTurnsScalingOffAtBothEnds(t *testing.T) {
	for _, window := range []int{1, 2} {
		t.Run(fmt.Sprintf("window %d", window), func(t *testing.T) {
			netnsHoldRequire(t)
			srv := netnsHoldStartPeer(t)
			hello := buildClientHello(netnsStallHost, 1400, 0xab)
			const answer = 512 << 10
			served := netnsWSAnsweringServerIn(t, srv, len(hello), answer)
			wire := netnsHoldCapture(t, srv)
			sent := netnsWSCaptureSent(t, srv)
			_, set := wsFixture()
			set.Targets.IpsToMatch = []string{netnsHoldServer}
			set.TCP.RSTProtection.Enabled = false
			set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: window, StripScale: true}
			netnsOverlapStart(t, srv, set)

			conn, err := netnsHoldDial()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
			if _, err := conn.Write(hello); err != nil {
				t.Fatal(err)
			}
			began := time.Now()
			if _, err := io.ReadFull(conn, make([]byte, answer)); err != nil {
				t.Fatalf("the answer of %d bytes did not arrive: %v", answer, err)
			}
			took := time.Since(began)
			clientScaled, err := netnsWSScalingAgreed(conn)
			if err != nil {
				t.Fatal(err)
			}
			var server netnsWSServed
			select {
			case server = <-served:
			case <-time.After(5 * time.Second):
				t.Fatal("the server did not finish")
			}
			if server.err != nil {
				t.Fatal(server.err)
			}
			if clientScaled || server.scaled {
				t.Fatalf("window scaling is still agreed (client %v, server %v), so later windows are read with a scale the device does not use", clientScaled, server.scaled)
			}

			port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
			time.Sleep(50 * time.Millisecond)
			var start uint32
			syn, helloParts, later := false, 0, 0
			firstLater := -1
			for _, p := range wire() {
				if p.sport != port || p.dport != netnsHoldPort {
					continue
				}
				if p.flags&0x02 != 0 {
					syn, start = true, p.seq
					if int(p.window) != window || p.scale != -1 {
						t.Fatalf("the SYN reached the server with window %d and Window Scale %d, want window %d and no option", p.window, p.scale, window)
					}
					continue
				}
				if !syn {
					continue
				}
				offset := int(int32(p.seq - (start + 1)))
				switch {
				case p.payload > 0 && offset < len(hello):
					helloParts++
					if int(p.window) != window {
						t.Fatalf("a ClientHello segment at offset %d reached the server with window %d, want %d", offset, p.window, window)
					}
				case p.payload == 0 && p.flags&0x05 == 0 && offset == len(hello):
					later++
					if firstLater < 0 {
						firstLater = int(p.window)
					}
				}
			}
			if !syn || helloParts < 2 || later == 0 {
				t.Fatalf("the capture missed the opening: SYN %v, %d ClientHello segments, %d later packets", syn, helloParts, later)
			}
			if firstLater < 16384 {
				t.Fatalf("the first packet after the ClientHello carries window %d, want the device's own unscaled window", firstLater)
			}

			var synAck, firstData *netnsWireSegment
			answered := sent()
			for i := range answered {
				p := &answered[i]
				if p.sport != netnsHoldPort || p.dport != port {
					continue
				}
				switch {
				case p.flags&0x12 == 0x12 && synAck == nil:
					synAck = p
				case p.payload > 0 && firstData == nil:
					firstData = p
				}
			}
			if synAck == nil || firstData == nil {
				t.Fatalf("the capture missed the server's side: SYN-ACK %v, first data segment %v", synAck != nil, firstData != nil)
			}
			if synAck.scale != -1 {
				t.Fatalf("the server answered with Window Scale %d although the SYN reached it without the option", synAck.scale)
			}
			if firstData.payload != window {
				t.Fatalf("the server's first data segment is %d bytes, want the %d the window allows", firstData.payload, window)
			}
			t.Logf("%d bytes in %v, %d ClientHello segments with window %d, first server segment %d bytes, first later window %d over %d later packets",
				answer, took.Round(time.Millisecond), helloParts, window, firstData.payload, firstLater, later)
		})
	}
}

func TestNetnsWSSizeStripScaleForwardedClientOutlivesTheCaptureWindow(t *testing.T) {
	netnsHoldRequire(t)
	srv := netnsHoldStartPeer(t)
	lan := netnsWSStartLANClient(t)
	hello := buildClientHello(netnsStallHost, 1400, 0xab)
	const answer = 512 << 10
	served := netnsWSAnsweringServerIn(t, srv, len(hello), answer)
	wire := netnsHoldCapture(t, srv)
	_, set := wsFixture()
	set.Targets.IpsToMatch = []string{netnsHoldServer}
	set.TCP.RSTProtection.Enabled = false
	set.TCP.WSSize = config.WSSizeConfig{Enabled: true, Window: 1, StripScale: true}
	w := netnsWSStartWorkerBehindProductionRules(t, set)
	queued := atomic.LoadUint64(&w.packetsProcessed)

	var conn net.Conn
	netnsHoldInside(t, lan, func() error {
		var err error
		conn, err = net.DialTimeout("tcp4", net.JoinHostPort(netnsHoldServer, strconv.Itoa(netnsHoldPort)), 3*time.Second)
		return err
	})
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	if _, err := conn.Write(hello); err != nil {
		t.Fatal(err)
	}
	if _, err := io.ReadFull(conn, make([]byte, answer)); err != nil {
		t.Fatalf("the answer of %d bytes did not reach the forwarded client: %v", answer, err)
	}
	clientScaled, err := netnsWSScalingAgreed(conn)
	if err != nil {
		t.Fatal(err)
	}
	var server netnsWSServed
	select {
	case server = <-served:
	case <-time.After(5 * time.Second):
		t.Fatal("the server did not finish")
	}
	if server.err != nil {
		t.Fatal(server.err)
	}
	queued = atomic.LoadUint64(&w.packetsProcessed) - queued
	if clientScaled || server.scaled {
		t.Fatalf("window scaling is still agreed (client %v, server %v)", clientScaled, server.scaled)
	}
	if queued >= 100 {
		t.Fatalf("%d packets went through the queue, the capture window was not in effect", queued)
	}

	port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
	time.Sleep(50 * time.Millisecond)
	var start uint32
	syn, afterHello, largest := false, 0, 0
	for _, p := range wire() {
		if p.sport != port || p.dport != netnsHoldPort {
			continue
		}
		if p.flags&0x02 != 0 {
			syn, start = true, p.seq
			if p.window != 1 || p.scale != -1 {
				t.Fatalf("the forwarded SYN reached the server with window %d and Window Scale %d, want window 1 and no option", p.window, p.scale)
			}
			continue
		}
		if !syn || p.payload > 0 || p.flags&0x05 != 0 || int(int32(p.seq-(start+1))) != len(hello) {
			continue
		}
		afterHello++
		if p.window == 1 {
			t.Fatalf("packet %d after the ClientHello still carries the written window", afterHello)
		}
		largest = max(largest, int(p.window))
	}
	if !syn || afterHello < 20 {
		t.Fatalf("the capture missed the opening or the transfer: SYN %v, %d packets after the ClientHello", syn, afterHello)
	}
	if largest < 16384 {
		t.Fatalf("the largest window after the ClientHello is %d, want the device's own unscaled window", largest)
	}
	t.Logf("%d packets through the queue, %d packets after the ClientHello, largest window %d", queued, afterHello, largest)
}
