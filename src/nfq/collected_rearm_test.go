package nfq

import (
	"context"
	"encoding/binary"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/engine"
)

func TestCollectedResidualRearmAfterAnsweredHello(t *testing.T) {
	w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	flow := silentFlow().flow
	flow.ID = 71
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return flow, true, nil }
	const sport = 42000
	if verdict := w.ProcessPacket(silentHelloFrom(sport)); verdict != engine.VerdictAccept {
		t.Fatalf("initial ClientHello verdict=%v", verdict)
	}
	if !w.destState.Watching() {
		t.Fatal("initial silent-flow watch missing")
	}
	payload := make([]byte, 203)
	copy(payload, []byte{0x16, 0x03, 0x03, 0x00, 0xc6, 0x02})
	reply := clientPacketAcking(sport, silentServerAck, afterHello(), 0x18, payload)
	copy(reply[12:16], []byte{1, 2, 3, 4})
	copy(reply[16:20], []byte{10, 0, 0, 1})
	binary.BigEndian.PutUint16(reply[20:22], 443)
	binary.BigEndian.PutUint16(reply[22:24], sport)
	flow.ReplyPackets++
	flow.ReplyBytes += uint64(len(reply))
	w.ProcessPacket(reply)
	if w.destState.Watching() {
		t.Fatal("server TLS payload did not cancel initial watch")
	}
	ack := uint32(silentServerAck + len(payload))
	w.ProcessPacket(clientPacketAcking(sport, afterHello(), ack, 0x10, nil))
	lateHello := clientPacketAcking(sport, silentHelloSeq, ack, 0x18, silentHello())
	w.ProcessPacket(lateHello)
	rearmed := w.destState.Watching()
	sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
	candidates := len(p.collected)
	published := len(p.collectedPublished)
	resets := len(resetsTo(sent()))
	t.Logf("server_payload=%d reply_bytes=%d reply_packets=%d advanced_client_ack=%d rearmed=%v candidates=%d published=%d resets=%d", len(payload), flow.ReplyBytes, flow.ReplyPackets, ack, rearmed, candidates, published, resets)
	if rearmed || candidates != 0 || published != 0 || resets != 0 {
		t.Fatalf("answered flow must stay out of Collect: rearmed=%v candidates=%d published=%d resets=%d", rearmed, candidates, published, resets)
	}
}

func TestCollectedResidualStillSilent(t *testing.T) {
	w, p, sent := collectedRig(t, func(context.Context, *config.Config) error { return nil })
	flow := silentFlow().flow
	flow.ID = 71
	lookupFlow = func(ctnl.Tuple, ctnl.Direction) (ctnl.Flow, bool, error) { return flow, true, nil }
	if verdict := w.ProcessPacket(silentHelloFrom(42000)); verdict != engine.VerdictAccept {
		t.Fatalf("ClientHello verdict=%v", verdict)
	}
	if !w.destState.Watching() {
		t.Fatal("silent-flow watch missing")
	}
	sweepAcknowledgedCollectedHellos(w, time.Now().Add(4*time.Second))
	candidates := len(p.collected)
	published := len(p.collectedPublished)
	resets := len(resetsTo(sent()))
	t.Logf("server_payload=0 reply_bytes=%d reply_packets=%d candidates=%d published=%d resets=%d", flow.ReplyBytes, flow.ReplyPackets, candidates, published, resets)
	if candidates != 1 || published != 1 || resets != 1 {
		t.Fatalf("silent flow must remain eligible: candidates=%d published=%d resets=%d", candidates, published, resets)
	}
}
