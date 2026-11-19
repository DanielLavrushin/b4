package nfq

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
)

func sweepAcknowledgedCollectedHellos(w *Worker, now time.Time) {
	w.destState.watchMu.Lock()
	for _, h := range w.destState.watches {
		if h.collect && h.clientHelloLen > 0 {
			h.clientHelloAcked = true
		}
	}
	w.destState.watchMu.Unlock()
	w.sweepCollectedHellos(w.getConfig(), now)
}

func collectedReply(sport uint16, ack uint32, flags byte, payload []byte) []byte {
	raw := clientPacketAcking(sport, silentServerAck, ack, flags, payload)
	copy(raw[12:16], []byte{1, 2, 3, 4})
	copy(raw[16:20], []byte{10, 0, 0, 1})
	binary.BigEndian.PutUint16(raw[20:22], 443)
	binary.BigEndian.PutUint16(raw[22:24], sport)
	return raw
}

func TestCollectedACKGate(t *testing.T) {
	for _, name := range []string{"missing", "duplicate", "partial", "complete", "beyond-sent", "without-ACK-flag", "server-data", "client-close", "with-client-tail"} {
		t.Run(name, func(t *testing.T) {
			w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
			flow := silentFlow().flow
			flow.ID = 71
			lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return flow, true, nil }
			w.ProcessPacket(silentHelloFrom(42000))
			if !w.destState.Watching() {
				t.Fatal("ClientHello watch absent")
			}
			if name == "with-client-tail" {
				w.ProcessPacket(clientPacket(42000, afterHello(), 0x18, changeCipherSpecRecord))
			}
			if name != "missing" {
				ack := afterHello()
				flags := byte(0x10)
				var payload []byte
				switch name {
				case "with-client-tail":
					ack += uint32(len(changeCipherSpecRecord))
				case "duplicate":
					ack = silentHelloSeq
				case "partial":
					ack--
				case "beyond-sent":
					ack++
				case "without-ACK-flag":
					flags = 0
				case "server-data":
					payload = []byte{0x16, 3, 3, 0, 1, 2}
					flags = 0x18
				}
				raw := collectedReply(42000, ack, flags, payload)
				flow.ReplyPackets++
				flow.ReplyBytes += uint64(len(raw))
				w.ProcessPacket(raw)
			}
			if name == "client-close" {
				w.ProcessPacket(clientPacket(42000, afterHello(), 0x11, nil))
			}
			w.sweepCollectedHellos(w.getConfig(), time.Now().Add(4*time.Second))
			want := name == "complete" || name == "with-client-tail"
			if got := len(p.collected) == 1; got != want {
				t.Fatalf("candidate=%v want=%v", got, want)
			}
			if got := len(resetsTo(sent())) == 1; got != want {
				t.Fatalf("reset=%v want=%v", got, want)
			}
		})
	}
}

func TestCollectedACKSplitOriginalHello(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			w, p, _ := collectedRig(t, func(context.Context, *config.Config) error { return nil })
			flow := silentFlow().flow
			flow.ID = 71
			lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return flow, true, nil }
			hello := helloWithLateSNI("i.ytimg.com")
			cut := len(hello) / 2
			w.ProcessPacket(clientPacket(42000, silentHelloSeq, 0x18, hello[:cut]))
			w.ProcessPacket(clientPacket(42000, silentHelloSeq+uint32(cut), 0x18, hello[cut:]))
			ack := silentHelloSeq + uint32(cut)
			if complete {
				ack = silentHelloSeq + uint32(len(hello))
			}
			raw := collectedReply(42000, ack, 0x10, nil)
			flow.ReplyPackets++
			flow.ReplyBytes += uint64(len(raw))
			w.ProcessPacket(raw)
			w.sweepCollectedHellos(w.getConfig(), time.Now().Add(4*time.Second))
			if got := len(p.collected) == 1; got != complete {
				t.Fatalf("split candidate=%v want=%v", got, complete)
			}
		})
	}
}

func TestCollectedACKWraparound(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			w, p, _ := collectedRig(t, func(context.Context, *config.Config) error { return nil })
			flow := silentFlow().flow
			flow.ID = 71
			lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return flow, true, nil }
			start := uint32(0xfffffff0)
			hello := silentHello()
			w.ProcessPacket(clientPacket(42000, start, 0x18, hello))
			end := start + uint32(len(hello))
			ack := end
			if !complete {
				ack--
			}
			raw := collectedReply(42000, ack, 0x10, nil)
			flow.ReplyPackets++
			flow.ReplyBytes += uint64(len(raw))
			w.ProcessPacket(raw)
			w.sweepCollectedHellos(w.getConfig(), time.Now().Add(4*time.Second))
			if got := len(p.collected) == 1; got != complete {
				t.Fatalf("wrapped candidate=%v want=%v", got, complete)
			}
		})
	}
}

func TestCollectedACKTracksOriginalSentEnd(t *testing.T) {
	w, p, _ := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	flow := silentFlow().flow
	flow.ID = 71
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return flow, true, nil }
	hello := helloWithEarlySNI("i.ytimg.com")
	cut := len(hello) - 16
	w.ProcessPacket(clientPacket(42000, silentHelloSeq, 0x18, hello[:cut]))
	if !w.destState.Watching() {
		t.Fatal("early SNI watch absent")
	}
	w.ProcessPacket(clientPacket(42000, silentHelloSeq+uint32(cut), 0x18, hello[cut:]))
	raw := collectedReply(42000, silentHelloSeq+uint32(len(hello)), 0x10, nil)
	flow.ReplyPackets++
	flow.ReplyBytes += uint64(len(raw))
	w.ProcessPacket(raw)
	w.sweepCollectedHellos(w.getConfig(), time.Now().Add(4*time.Second))
	if len(p.collected) != 1 {
		t.Fatal("complete ACK after original tail was ignored")
	}
}

func TestCollectedACKCaptureRows(t *testing.T) {
	cases := []struct {
		name        string
		length, ack uint32
		want        bool
	}{
		{"provider195", 1978, 0, false}, {"provider89", 2254, 0, false},
		{"google173", 2036, 2036, true}, {"google74", 2006, 2006, true},
		{"partial1978", 1978, 1977, false}, {"past1978", 1978, 1979, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tracker := &destStateTracker{}
			h := &helloWatch{collect: true, key: "lan", replyKey: "wire", clientHelloSeq: 1000, clientHelloLen: tc.length, clientSentEnd: 1000 + tc.length}
			tracker.WatchHello(h)
			tcp := make([]byte, 20)
			tcp[12] = 0x50
			tcp[13] = 0x10
			binary.BigEndian.PutUint32(tcp[8:12], 1000+tc.ack)
			tracker.NoteCollectedServerPacket("wire", tcp, 0, 52)
			if h.clientHelloAcked != tc.want {
				t.Fatalf("acknowledged=%v want=%v", h.clientHelloAcked, tc.want)
			}
		})
	}
}

func TestCollectedACKConcurrentObservation(t *testing.T) {
	tracker := &destStateTracker{}
	h := &helloWatch{collect: true, key: "lan", replyKey: "wire", clientHelloSeq: 1000, clientHelloLen: 2000, clientSentEnd: 3000}
	tracker.WatchHello(h)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				tcp := make([]byte, 20)
				tcp[12] = 0x50
				tcp[13] = 0x10
				binary.BigEndian.PutUint32(tcp[8:12], 3000)
				tracker.NoteCollectedServerPacket("wire", tcp, 0, 52)
				tracker.NextCollectedDeadline()
			}
		}()
	}
	wg.Wait()
	tracker.watchMu.RLock()
	defer tracker.watchMu.RUnlock()
	if !h.clientHelloAcked || h.observedReplyPackets != 800 {
		t.Fatalf("ack=%v packets=%d", h.clientHelloAcked, h.observedReplyPackets)
	}
}

func TestCollectedACKIPv6(t *testing.T) {
	for _, complete := range []bool{false, true} {
		t.Run(fmt.Sprint(complete), func(t *testing.T) {
			w, p, _ := collectedRig(t, func(context.Context, *config.Config) error { return nil })
			w.getConfig().Queue.IPv6Enabled = true
			flow := silentFlow().flow
			flow.ID = 71
			lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return flow, true, nil }
			hello := makeV6TCPPacket(silentHello(), silentHelloSeq)
			binary.BigEndian.PutUint32(hello[IPv6HeaderLen+8:IPv6HeaderLen+12], silentServerAck)
			seedEarlierSilentConnection(w, net.IP(hello[24:40]), 443)
			w.ProcessPacket(hello)
			reply := makeV6TCPPacket(nil, silentServerAck)
			copy(reply[8:24], hello[24:40])
			copy(reply[24:40], hello[8:24])
			binary.BigEndian.PutUint16(reply[40:42], 443)
			binary.BigEndian.PutUint16(reply[42:44], 12345)
			ack := uint32(silentHelloSeq)
			if complete {
				ack = afterHello()
			}
			binary.BigEndian.PutUint32(reply[48:52], ack)
			reply[53] = 0x10
			flow.ReplyPackets++
			flow.ReplyBytes += uint64(len(reply))
			w.ProcessPacket(reply)
			w.sweepCollectedHellos(w.getConfig(), time.Now().Add(4*time.Second))
			if got := len(p.collected) == 1; got != complete {
				t.Fatalf("IPv6 candidate=%v want=%v", got, complete)
			}
		})
	}
}

func TestCollectedACKLateReplyDoesNotRearm(t *testing.T) {
	w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	flow := silentFlow().flow
	flow.ID = 71
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return flow, true, nil }
	w.ProcessPacket(silentHelloFrom(42000))
	w.sweepCollectedHellos(w.getConfig(), time.Now().Add(4*time.Second))
	reply := collectedReply(42000, afterHello(), 0x10, nil)
	flow.ReplyPackets++
	flow.ReplyBytes += uint64(len(reply))
	w.ProcessPacket(reply)
	w.ProcessPacket(silentHelloFrom(42000))
	w.sweepCollectedHellos(w.getConfig(), time.Now().Add(5*time.Second))
	if len(p.collected) != 0 || len(resetsTo(sent())) != 0 || w.destState.Watching() {
		t.Fatal("late ACK revived a rejected observation")
	}
}

func TestCollectedACKUnknownMultiRecordExtent(t *testing.T) {
	w, p, _ := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	hello := helloWithEarlySNI("i.ytimg.com")
	records, _ := twoRecordHello(hello, len(hello)-5-16)
	w.ProcessPacket(clientPacket(42000, silentHelloSeq, 0x18, records))
	w.sweepCollectedHellos(w.getConfig(), time.Now().Add(4*time.Second))
	if len(p.collected) != 0 || w.destState.Watching() {
		t.Fatal("unknown multi-record extent became a Collect watch")
	}
}
