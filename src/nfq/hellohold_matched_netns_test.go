package nfq

import (
	"bytes"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func TestNetnsASetThatLeavesTheHelloAloneDoesNotKeepSegmentOne(t *testing.T) {
	listed := func(set *config.SetConfig) { set.Targets.IpsToMatch = []string{"203.0.113.0/24"} }
	for _, tc := range []struct {
		name    string
		host    string
		mutate  func(set *config.SetConfig)
		split   bool
		settled string
	}{
		{"a listed address, the name of an acting set", "www.blocked.example", listed, true, "sent through set YT images"},
		{"a port filter, the name of an acting set", "www.blocked.example", func(set *config.SetConfig) {
			set.TCP.DPortFilter = strconv.Itoa(netnsHoldPort)
			set.TCPPortRanges = config.ParsePortRanges(strconv.Itoa(netnsHoldPort))
		}, true, "sent through set YT images"},
		{"a listed address, a name of no set", "allowed.example", listed, false, "released ahead of the next packet"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			netnsHoldRequire(t)
			srv := netnsHoldStartPeer(t)
			netnsHoldRun(t, "ip", "link", "set", "lo", "up")

			buf := &syncBuffer{}
			previous := log.Level(log.CurLevel.Load())
			log.Init(io.Discard, log.LevelTrace, true)
			log.StartCapture(buf)
			t.Cleanup(func() {
				log.StopCapture(buf)
				log.Init(os.Stderr, previous, true)
			})

			hello := buildClientHello(tc.host, 700, 0xAB)
			cut := netnsHoldMSS - 12
			if nameAt := bytes.Index(hello, []byte(tc.host)); nameAt < cut {
				t.Fatalf("the name sits at byte %d, inside the first %d bytes, so this test proves nothing", nameAt, cut)
			}
			received := netnsHoldServerIn(t, srv, len(hello))
			wire := netnsHoldCapture(t, srv)

			acting := newFragmentingYTImagesSet()
			acting.Targets.DomainsToMatch = []string{"blocked.example"}
			bystander := newBystanderSet(tc.mutate)
			cfg := config.NewConfig()
			cfg.Queue.StartNum, cfg.Queue.Threads = netnsHoldQueue, 1
			cfg.Sets = []*config.SetConfig{&acting, &bystander}
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

			conn, err := netnsHoldDial()
			if err != nil {
				t.Fatalf("dial %s:%d: %v", netnsHoldServer, netnsHoldPort, err)
			}
			defer conn.Close()
			port := uint16(conn.LocalAddr().(*net.TCPAddr).Port)
			_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
			if _, err := conn.Write(hello); err != nil {
				t.Fatalf("write the ClientHello: %v", err)
			}
			select {
			case got := <-received:
				if !bytes.Equal(got, hello) {
					t.Fatalf("the server read %d bytes that differ from the %d-byte ClientHello", len(got), len(hello))
				}
			case <-time.After(10 * time.Second):
				t.Fatal("the server never read the whole ClientHello")
			}
			time.Sleep(200 * time.Millisecond)

			var data []netnsWireSegment
			for _, s := range wire() {
				if s.sport == port && s.dport == netnsHoldPort && s.payload > 0 {
					data = append(data, s)
				}
			}
			if len(data) == 0 {
				t.Fatal("no data segment was captured at the server")
			}
			start := data[0].seq
			for _, s := range data {
				if int32(s.seq-start) < 0 {
					start = s.seq
				}
			}
			pieces := 0
			for _, s := range data {
				if s.seq-start < uint32(cut) {
					pieces++
				}
			}
			if split := pieces > 1; split != tc.split {
				t.Errorf("segment 1 reached the server in %d piece(s), split: %v, want %v: %v", pieces, split, tc.split, data)
			}

			key := fmt.Sprintf(connKeyFormat, netnsHoldRouter, port, netnsHoldServer, netnsHoldPort)
			log.Flush()
			line := regexp.MustCompile(regexp.QuoteMeta("Split ClientHello "+key+": segment 1 held ") + `(\S+), (.+)`).FindStringSubmatch(buf.String())
			if line == nil {
				t.Fatalf("segment 1 of %s was never held", key)
			}
			if line[2] != tc.settled {
				t.Errorf("segment 1 was settled by %q, want %q", line[2], tc.settled)
			}
			t.Logf("segment 1 held %s; on the wire: %v", line[1], data)
		})
	}
}
