package nfq

import (
	"bytes"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestNetnsWSSizeRewritesTheRealOpening(t *testing.T) {
	netnsHoldRequire(t)
	srv := netnsHoldStartPeer(t)
	payload := []byte("first outgoing data")
	reads := netnsHoldServerIn(t, srv, len(payload))
	wire := netnsHoldCapture(t, srv)
	_, set := wsFixture()
	set.Targets.IpsToMatch = []string{netnsHoldServer}
	set.TCP.RSTProtection.Enabled = false
	netnsOverlapStart(t, srv, set)
	conn, err := netnsHoldDial()
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	if _, err = conn.Write(payload); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-reads:
		if !bytes.Equal(got, payload) {
			t.Fatal("server did not receive original data")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server timed out")
	}
	time.Sleep(50 * time.Millisecond)
	port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
	syn, data := false, false
	for _, p := range wire() {
		if p.sport != port || p.dport != netnsHoldPort {
			continue
		}
		if p.flags&2 != 0 {
			syn = true
			if p.window != 1 || p.scale != 1 {
				t.Fatalf("real SYN window=%d scale=%d", p.window, p.scale)
			}
		}
		if p.payload > 0 {
			data = true
			if p.window != 1 {
				t.Fatalf("first data window=%d", p.window)
			}
		}
	}
	if !syn || !data {
		t.Fatal("opening not captured")
	}
}

func TestNetnsWSSizeOriginalHelloAndRealFragments(t *testing.T) {
	for _, strategy := range []string{"none", "combo"} {
		t.Run(strategy, func(t *testing.T) {
			netnsHoldRequire(t)
			srv := netnsHoldStartPeer(t)
			hello := wsHelloRecords(buildClientHello(netnsStallHost, 1400, 0xab), 100)
			later := []byte("later application data")
			reads := netnsOverlapServerIn(t, srv, len(hello), len(later))
			wire := netnsHoldCapture(t, srv)
			_, set := wsFixture()
			set.Targets.IpsToMatch = []string{netnsHoldServer}
			set.TCP.RSTProtection.Enabled = false
			set.TCP.WSSize.Cutoff = "client_hello"
			set.Fragmentation.Strategy = strategy
			if strategy == "combo" {
				set.Fragmentation = config.DefaultSetConfig.Fragmentation
				set.Fragmentation.Strategy = strategy
				set.Fragmentation.Combo.FirstDelayMs = 1
				set.Fragmentation.Combo.JitterMaxUs = 1
				set.Fragmentation.Combo.FirstByteSplit = true
				set.Fragmentation.Combo.ExtensionSplit = false
			}
			netnsOverlapStart(t, srv, set)
			conn, err := netnsHoldDial()
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conn.SetDeadline(time.Now().Add(5 * time.Second))
			for _, part := range [][]byte{hello[:32], hello[32:]} {
				if _, err := conn.Write(part); err != nil {
					t.Fatal(err)
				}
				time.Sleep(20 * time.Millisecond)
			}
			if _, err := conn.Read(make([]byte, 1)); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Write(later); err != nil {
				t.Fatal(err)
			}
			select {
			case got := <-reads:
				if got.err != nil || !bytes.Equal(got.got, append(hello, later...)) {
					t.Fatalf("original application bytes changed: %v", got.err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("receiver timed out")
			}
			time.Sleep(50 * time.Millisecond)
			port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
			var start uint32
			parts, after := 0, 0
			tinyFirst := false
			for _, p := range wire() {
				if p.sport != port || p.dport != netnsHoldPort {
					continue
				}
				if p.flags&2 != 0 {
					start = p.seq + 1
					if p.window != 1 || p.scale != 1 {
						t.Fatal("opening window/scale missing")
					}
				}
				if p.payload == 0 {
					continue
				}
				if int32(p.seq-start) < int32(len(hello)) {
					parts++
					if p.seq == start && p.payload == 1 {
						tinyFirst = true
					}
					if p.window != 1 {
						t.Fatalf("original ClientHello part lost window: %+v", p)
					}
				} else {
					after++
					if p.window == 1 {
						t.Fatalf("later data still rewritten: %+v", p)
					}
				}
			}
			if parts < 3 || after == 0 {
				t.Fatalf("capture did not exercise multiple original packets and cutoff: %d/%d", parts, after)
			}
			if strategy == "combo" && !tinyFirst {
				t.Fatal("Combo did not produce its real one-byte first fragment")
			}
		})
	}
}
