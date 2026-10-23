package nfq

import (
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/quic"
	"github.com/daniellavrushin/b4/sock"
)

func sealQUICInitial(t *testing.T, hdrDCID, keyDCID []byte, pn byte, frames []byte) []byte {
	t.Helper()
	aead, hp, iv := quicInitialKeys(t, keyDCID)
	if pad := 1200 - len(frames); pad > 0 {
		frames = append(frames, make([]byte, pad)...)
	}

	header := []byte{0xC0, 0x00, 0x00, 0x00, 0x01, byte(len(hdrDCID))}
	header = append(header, hdrDCID...)
	header = append(header, 0x00, 0x00)
	header = append(header, encodeQUICVarint(uint64(1+len(frames)+aead.Overhead()))...)
	pnOff := len(header)
	aad := append(header, pn)

	nonce := append([]byte{}, iv...)
	nonce[len(nonce)-1] ^= pn
	packet := append(append([]byte{}, aad...), aead.Seal(nil, nonce, frames, aad)...)

	var mask [16]byte
	hp.Encrypt(mask[:], packet[pnOff+4:pnOff+4+16])
	packet[0] ^= mask[0] & 0x0f
	packet[pnOff] ^= mask[1]
	return packet
}

func quicCryptoFrame(off int, data []byte) []byte {
	frame := []byte{0x06}
	frame = append(frame, encodeQUICVarint(uint64(off))...)
	frame = append(frame, encodeQUICVarint(uint64(len(data)))...)
	return append(frame, data...)
}

type splitQUICHello struct {
	first      []byte
	completing []byte
	resend     []byte
	oneRTT     []byte
}

func newSplitQUICHello(t *testing.T, odcid, serverCID []byte, host string) splitQUICHello {
	t.Helper()
	hello := buildClientHelloHandshake(host)
	half := len(hello) / 2
	return splitQUICHello{
		first:      sealQUICInitial(t, odcid, odcid, 1, quicCryptoFrame(0, hello[:half])),
		completing: sealQUICInitial(t, odcid, odcid, 2, quicCryptoFrame(half, hello[half:])),
		resend:     sealQUICInitial(t, serverCID, odcid, 3, quicCryptoFrame(half, hello[half:])),
		oneRTT:     append(append([]byte{0x41}, serverCID...), make([]byte, 40)...),
	}
}

func newDomainOnlyQUICSet(mode string) config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "quic-flow"
	set.Name = "QUICFlow"
	set.Enabled = true
	set.Targets.SNIDomains = []string{"blocked.example"}
	set.Targets.DomainOnly = true
	set.UDP.Mode = mode
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	return set
}

func newQUICFlowWorker(t *testing.T, sets ...*config.SetConfig) *Worker {
	t.Helper()
	cfg := newUDPGateConfig(t, sets...)
	w := newTestWorker(t, &cfg)
	w.sock = &sock.Sender{}
	t.Cleanup(w.wg.Wait)
	return w
}

var (
	quicClient = net.ParseIP("10.0.0.1")
	quicServer = net.ParseIP("1.2.3.4")
)

func sendQUIC(w *Worker, payload []byte, sport uint16) engine.PacketVerdict {
	return w.ProcessPacket(makeV4UDPPacket(payload, quicClient, quicServer, sport, 443))
}

func TestADomainOnlySetStopsTheRestOfAQUICFlowAfterItsClientHello(t *testing.T) {
	block := newDomainOnlyQUICSet(config.ConfigOff)
	block.Routing.Enabled = true
	block.Routing.Mode = config.RoutingModeBlock
	block.Routing.BlockAction = config.BlockActionDrop

	cases := map[string]config.SetConfig{
		"drop":   newDomainOnlyQUICSet(config.UDPModeDrop),
		"reject": newDomainOnlyQUICSet(config.UDPModeReject),
		"block":  block,
	}
	for name, set := range cases {
		t.Run(name, func(t *testing.T) {
			w := newQUICFlowWorker(t, &set)
			flow := newSplitQUICHello(t, []byte{0x51, 1, 2, 3, 4, 5, 6, 7}, []byte{0x52, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")

			if v := sendQUIC(w, flow.first, 50001); v != engine.VerdictAccept {
				t.Fatalf("the first datagram carries no complete ClientHello yet, want accept, got %v", v)
			}
			if v := sendQUIC(w, flow.completing, 50001); v != engine.VerdictDropFlow {
				t.Fatalf("the datagram that completes the ClientHello must be stopped, got %v", v)
			}
			if v := sendQUIC(w, flow.resend, 50001); v != engine.VerdictDropFlow {
				t.Errorf("the resend under the server's connection ID belongs to the same flow and must be stopped, got %v", v)
			}
			if v := sendQUIC(w, flow.oneRTT, 50001); v != engine.VerdictDropFlow {
				t.Errorf("a short-header packet of the same flow must be stopped, got %v", v)
			}
			if v := sendQUIC(w, flow.oneRTT, 50002); v != engine.VerdictAccept {
				t.Errorf("a domain-only set must not stop another flow to the same address, got %v", v)
			}
		})
	}
}

func TestPacketShapingModesLeaveTheLaterDatagramsOfAQUICFlowAlone(t *testing.T) {
	for _, mode := range []string{config.UDPModeCoalesce, config.UDPModeFake} {
		t.Run(mode, func(t *testing.T) {
			set := newDomainOnlyQUICSet(mode)
			w := newQUICFlowWorker(t, &set)
			flow := newSplitQUICHello(t, []byte{0x53, 1, 2, 3, 4, 5, 6, 7}, []byte{0x54, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")

			sendQUIC(w, flow.first, 50003)
			if v := sendQUIC(w, flow.completing, 50003); v != engine.VerdictDrop {
				t.Fatalf("udp.mode %s must rewrite the datagram that completes the ClientHello, got %v", mode, v)
			}
			if v := sendQUIC(w, flow.resend, 50003); v != engine.VerdictAccept {
				t.Errorf("udp.mode %s must leave the resend under the server's connection ID untouched, got %v", mode, v)
			}
			if v := sendQUIC(w, flow.oneRTT, 50003); v != engine.VerdictAccept {
				t.Errorf("udp.mode %s must leave a short-header packet untouched, got %v", mode, v)
			}
		})
	}
}

func TestCoalesceActsOnlyOnTheDatagramThatCompletesTheClientHello(t *testing.T) {
	set := newDomainOnlyQUICSet(config.UDPModeCoalesce)
	set.Id = "quic-learn"
	set.Name = "QUICLearn"
	set.Targets.DomainOnly = false
	w := newQUICFlowWorker(t, &set)

	first := newSplitQUICHello(t, []byte{0x70, 1, 2, 3, 4, 5, 6, 7}, []byte{0x71, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, first.first, 50101); v != engine.VerdictAccept {
		t.Fatalf("the first datagram carries no complete ClientHello, want accept, got %v", v)
	}
	if v := sendQUIC(w, first.completing, 50101); v != engine.VerdictDrop {
		t.Fatalf("the datagram that completes the ClientHello must be coalesced, got %v", v)
	}
	if v := sendQUIC(w, first.resend, 50101); v != engine.VerdictAccept {
		t.Errorf("the resend under the server's connection ID must go out untouched, got %v", v)
	}

	second := newSplitQUICHello(t, []byte{0x72, 1, 2, 3, 4, 5, 6, 7}, []byte{0x73, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, second.first, 50102); v != engine.VerdictAccept {
		t.Errorf("once the set has learned the address, a datagram that does not complete the ClientHello must still go out untouched, got %v", v)
	}
	if v := sendQUIC(w, second.completing, 50102); v != engine.VerdictDrop {
		t.Errorf("a later connection's ClientHello must still be coalesced, got %v", v)
	}
	if v := sendQUIC(w, second.resend, 50102); v != engine.VerdictAccept {
		t.Errorf("a later connection's resend must go out untouched, got %v", v)
	}
	if v := sendQUIC(w, second.oneRTT, 50102); v != engine.VerdictAccept {
		t.Errorf("a short-header packet must go out untouched, got %v", v)
	}
}

func TestANewQUICConnectionOnTheSamePortDropsThePreviousName(t *testing.T) {
	set := newDomainOnlyQUICSet(config.UDPModeDrop)

	t.Run("split hello", func(t *testing.T) {
		w := newQUICFlowWorker(t, &set)
		old := newSplitQUICHello(t, []byte{0x55, 1, 2, 3, 4, 5, 6, 7}, []byte{0x56, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
		sendQUIC(w, old.first, 50004)
		sendQUIC(w, old.completing, 50004)

		next := newSplitQUICHello(t, []byte{0x57, 1, 2, 3, 4, 5, 6, 7}, []byte{0x58, 1, 2, 3, 4, 5, 6, 7}, "www.allowed.example")
		for i, payload := range [][]byte{next.first, next.completing, next.resend, next.oneRTT} {
			if v := sendQUIC(w, payload, 50004); v != engine.VerdictAccept {
				t.Errorf("datagram %d of a new connection to an allowed name was stopped by the port's previous name: %v", i, v)
			}
		}
	})

	t.Run("hello without a readable name", func(t *testing.T) {
		w := newQUICFlowWorker(t, &set)
		old := newSplitQUICHello(t, []byte{0x61, 1, 2, 3, 4, 5, 6, 7}, []byte{0x62, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
		sendQUIC(w, old.first, 50008)
		sendQUIC(w, old.completing, 50008)

		next := newSplitQUICHello(t, []byte{0x63, 1, 2, 3, 4, 5, 6, 7}, []byte{0x64, 1, 2, 3, 4, 5, 6, 7}, "")
		for i, payload := range [][]byte{next.first, next.completing, next.resend, next.oneRTT} {
			if v := sendQUIC(w, payload, 50008); v != engine.VerdictAccept {
				t.Errorf("datagram %d of a new connection without a readable name was stopped by the port's previous name: %v", i, v)
			}
		}
	})

	t.Run("whole hello", func(t *testing.T) {
		w := newQUICFlowWorker(t, &set)
		old := newSplitQUICHello(t, []byte{0x59, 1, 2, 3, 4, 5, 6, 7}, []byte{0x5a, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
		sendQUIC(w, old.first, 50005)
		sendQUIC(w, old.completing, 50005)

		if v := sendQUIC(w, buildQUICInitialWithSNI(t, []byte{0x5b, 1, 2, 3, 4, 5, 6, 7}, "www.allowed.example"), 50005); v != engine.VerdictAccept {
			t.Fatalf("a new connection's ClientHello for an allowed name was stopped: %v", v)
		}
		if v := sendQUIC(w, old.oneRTT, 50005); v != engine.VerdictAccept {
			t.Errorf("a short-header packet after the new connection's ClientHello must not use the previous name, got %v", v)
		}
	})
}

func TestAnAddressLearnedForAnotherNameDoesNotRestartAStoppedQUICFlow(t *testing.T) {
	blocked := newDomainOnlyQUICSet(config.UDPModeDrop)

	allowed := config.NewSetConfig()
	allowed.Id = "allowed"
	allowed.Name = "Allowed"
	allowed.Enabled = true
	allowed.Targets.SNIDomains = []string{"allowed.example"}
	allowed.UDP.Mode = config.ConfigOff
	allowed.Fragmentation.Strategy = config.ConfigNone
	allowed.Fragmentation.StrategyPool = nil

	w := newQUICFlowWorker(t, &blocked, &allowed)
	flow := newSplitQUICHello(t, []byte{0x80, 1, 2, 3, 4, 5, 6, 7}, []byte{0x81, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")

	sendQUIC(w, flow.first, 50201)
	if v := sendQUIC(w, flow.completing, 50201); v != engine.VerdictDropFlow {
		t.Fatalf("the blocked name's ClientHello must be stopped, got %v", v)
	}

	other := buildQUICInitialWithSNI(t, []byte{0x82, 1, 2, 3, 4, 5, 6, 7}, "www.allowed.example")
	if v := sendQUIC(w, other, 50202); v != engine.VerdictAccept {
		t.Fatalf("a connection to an allowed name on the same address must pass, got %v", v)
	}

	if v := sendQUIC(w, flow.resend, 50201); v != engine.VerdictDropFlow {
		t.Errorf("the stopped flow's resend must stay stopped after another set learned the address, got %v", v)
	}
	if v := sendQUIC(w, flow.oneRTT, 50201); v != engine.VerdictDropFlow {
		t.Errorf("a short-header packet of the stopped flow must stay stopped, got %v", v)
	}
}

func TestAnAddressLearnedByACoalescingSetDoesNotHideABlockedName(t *testing.T) {
	coalescing := newDomainOnlyQUICSet(config.UDPModeCoalesce)
	coalescing.Id = "coalescing"
	coalescing.Name = "Coalescing"
	coalescing.Targets.SNIDomains = []string{"allowed.example"}
	coalescing.Targets.DomainOnly = false

	blocked := newDomainOnlyQUICSet(config.UDPModeDrop)
	w := newQUICFlowWorker(t, &coalescing, &blocked)

	teach := buildQUICInitialWithSNI(t, []byte{0x90, 1, 2, 3, 4, 5, 6, 7}, "www.allowed.example")
	if v := sendQUIC(w, teach, 50301); v != engine.VerdictDrop {
		t.Fatalf("the coalescing set must act on its own name's ClientHello, got %v", v)
	}

	flow := newSplitQUICHello(t, []byte{0x91, 1, 2, 3, 4, 5, 6, 7}, []byte{0x92, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	sendQUIC(w, flow.first, 50302)
	if v := sendQUIC(w, flow.completing, 50302); v != engine.VerdictDropFlow {
		t.Fatalf("the blocked name's ClientHello must be stopped, got %v", v)
	}
	if v := sendQUIC(w, flow.resend, 50302); v != engine.VerdictDropFlow {
		t.Errorf("a flow to a blocked name must stay stopped on an address the coalescing set learned, got %v", v)
	}
	if v := sendQUIC(w, flow.oneRTT, 50302); v != engine.VerdictDropFlow {
		t.Errorf("a short-header packet of the blocked flow must stay stopped, got %v", v)
	}
}

func TestAnEscalationToACoalescingSetCoalescesOnALearnedAddress(t *testing.T) {
	fallback := newDomainOnlyQUICSet(config.UDPModeCoalesce)
	fallback.Id = "coalesce-fallback"
	fallback.Name = "CoalesceFallback"
	fallback.Targets.SNIDomains = []string{"fallback.example"}

	learning := newDomainOnlyQUICSet(config.ConfigOff)
	learning.Id = "learning"
	learning.Name = "Learning"
	learning.Targets.SNIDomains = []string{"escalate.example"}
	learning.Targets.DomainOnly = false
	learning.Escalate.To = fallback.Id

	for _, escalated := range []bool{false, true} {
		w := newQUICFlowWorker(t, &learning, &fallback)
		sent := captureInjected(t, w)
		if v := sendQUIC(w, buildQUICInitialWithSNI(t, []byte{0xa0, 1, 2, 3, 4, 5, 6, 7}, "www.escalate.example"), 50401); v != engine.VerdictAccept {
			t.Fatalf("the learning set leaves QUIC alone, got %v", v)
		}
		if escalated {
			w.destState.SetEscalation("www.escalate.example", fallback.Id, escalateReasonStall, time.Minute)
		}

		hello := buildQUICInitialWithSNI(t, []byte{0xa1, 1, 2, 3, 4, 5, 6, 7}, "www.escalate.example")
		v := sendQUIC(w, hello, 50402)
		got := sent()
		if !escalated {
			if v != engine.VerdictAccept || len(got) != 0 {
				t.Errorf("without escalation the learning set leaves the ClientHello datagram alone, got %v and %d packets", v, len(got))
			}
			continue
		}
		if v != engine.VerdictDrop || len(got) != 1 || len(got[0]) <= 28+len(hello) {
			t.Errorf("after escalation to a coalescing set, a ClientHello datagram to the learned address must go out coalesced, got %v and %d packets", v, len(got))
		}
	}
}

func TestAStaticAddressSetKeepsTheLaterDatagramsOfAQUICFlow(t *testing.T) {
	named := newDomainOnlyQUICSet(config.UDPModeDrop)
	static := config.NewSetConfig()
	static.Id = "static"
	static.Name = "Static"
	static.Enabled = true
	static.Targets.IPs = []string{"1.2.3.4/32"}
	static.UDP.Mode = config.ConfigOff
	static.Fragmentation.Strategy = config.ConfigNone

	w := newQUICFlowWorker(t, &static, &named)
	flow := newSplitQUICHello(t, []byte{0x5c, 1, 2, 3, 4, 5, 6, 7}, []byte{0x5d, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")

	sendQUIC(w, flow.first, 50006)
	if v := sendQUIC(w, flow.completing, 50006); v != engine.VerdictDropFlow {
		t.Fatalf("the ClientHello's name picks the drop set, got %v", v)
	}
	if v := sendQUIC(w, flow.resend, 50006); v != engine.VerdictAccept {
		t.Errorf("a static address match keeps the flow's later datagrams, got %v", v)
	}
}

func TestAQUICFlowFollowsTheCurrentConfig(t *testing.T) {
	set := newDomainOnlyQUICSet(config.UDPModeDrop)
	w := newQUICFlowWorker(t, &set)
	flow := newSplitQUICHello(t, []byte{0x5e, 1, 2, 3, 4, 5, 6, 7}, []byte{0x5f, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")

	sendQUIC(w, flow.first, 50007)
	if v := sendQUIC(w, flow.completing, 50007); v != engine.VerdictDropFlow {
		t.Fatalf("the ClientHello must be dropped, got %v", v)
	}

	changed := newDomainOnlyQUICSet(config.ConfigOff)
	cfg := newUDPGateConfig(t, &changed)
	cfg.ConfigPath = t.TempDir() + "/b4.json"
	w.cfg.Store(&cfg)
	w.matcher.Store(buildMatcher(&cfg))

	if v := sendQUIC(w, flow.resend, 50007); v != engine.VerdictAccept {
		t.Errorf("after the set stops dropping, the flow's later datagrams must pass, got %v", v)
	}
}

func TestAnIdleQUICFlowIsForgotten(t *testing.T) {
	c := newQUICFlowCache()
	odcid := []byte{0x60, 1, 2, 3, 4, 5, 6, 7}
	c.Store("k", "www.blocked.example", odcid)
	short := append([]byte{0x41}, odcid...)

	if host, _ := c.Lookup("k", short); host != "www.blocked.example" {
		t.Fatalf("lookup of a fresh flow returned %q", host)
	}
	c.flows["k"].lastSeen = time.Now().Add(-quicFlowIdle - time.Second)
	if host, _ := c.Lookup("k", short); host != "" {
		t.Errorf("an idle flow must not name later datagrams, got %q", host)
	}
	c.Cleanup()
	if len(c.flows) != 0 {
		t.Errorf("cleanup kept %d idle flows", len(c.flows))
	}
}

func TestALearnedAddressDoesNotHideABlockedQUICName(t *testing.T) {
	learning := newDomainOnlyQUICSet(config.ConfigOff)
	learning.Id = "learning"
	learning.Name = "Learning"
	learning.Targets.SNIDomains = []string{"allowed.example"}
	learning.Targets.DomainOnly = false

	blocked := newDomainOnlyQUICSet(config.UDPModeDrop)
	w := newQUICFlowWorker(t, &learning, &blocked)

	teach := buildQUICInitialWithSNI(t, []byte{0xA0, 1, 2, 3, 4, 5, 6, 7}, "www.allowed.example")
	if v := sendQUIC(w, teach, 50401); v != engine.VerdictAccept {
		t.Fatalf("teaching connection: got %v", v)
	}

	flow := newSplitQUICHello(t, []byte{0xA1, 1, 2, 3, 4, 5, 6, 7}, []byte{0xA2, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	sendQUIC(w, flow.first, 50402)
	if v := sendQUIC(w, flow.completing, 50402); v != engine.VerdictDropFlow {
		t.Errorf("ClientHello of the blocked name: got %v, want the flow stopped", v)
	}
	if v := sendQUIC(w, flow.resend, 50402); v != engine.VerdictDropFlow {
		t.Errorf("resend of the blocked flow: got %v, want the flow stopped", v)
	}
}

func TestALearningDropSetLeavesUnrelatedQUICNamesAlone(t *testing.T) {
	dropper := newDomainOnlyQUICSet(config.UDPModeDrop)
	dropper.Targets.DomainOnly = false
	w := newQUICFlowWorker(t, &dropper)

	own := buildQUICInitialWithSNI(t, []byte{0xD2, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, own, 50701); v != engine.VerdictDropFlow {
		t.Fatalf("own name: got %v, want the flow stopped", v)
	}

	unrelated := buildQUICInitialWithSNI(t, []byte{0xD3, 1, 2, 3, 4, 5, 6, 7}, "www.unrelated.test")
	if v := sendQUIC(w, unrelated, 50702); v != engine.VerdictAccept {
		t.Errorf("a name no set targets, on the learned address: got %v, want accept", v)
	}
}

func TestALearningFakeSetLeavesUnrelatedQUICNamesUntouched(t *testing.T) {
	faking := newDomainOnlyQUICSet(config.UDPModeFake)
	faking.Targets.DomainOnly = false
	w := newQUICFlowWorker(t, &faking)
	sent := captureInjected(t, w)

	own := buildQUICInitialWithSNI(t, []byte{0xE1, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, own, 50801); v != engine.VerdictDrop {
		t.Fatalf("the set's own name must be reinjected behind its fakes, got %v", v)
	}
	if got := sent(); len(got) == 0 {
		t.Fatal("the set's own ClientHello datagram must be sent by b4")
	}

	unrelated := buildQUICInitialWithSNI(t, []byte{0xE2, 1, 2, 3, 4, 5, 6, 7}, "www.unrelated.test")
	if v := sendQUIC(w, unrelated, 50802); v != engine.VerdictAccept {
		t.Errorf("a name no set targets, on the learned address: got %v, want accept", v)
	}
	if got := sent(); len(got) != 0 {
		t.Errorf("%d packets sent for a name no set targets on the learned address", len(got))
	}
}

func TestAPortSetTakesTheQUICALearnedAddressNoLongerClaims(t *testing.T) {
	learning := newDomainOnlyQUICSet(config.ConfigOff)
	learning.Targets.DomainOnly = false

	ports := config.NewSetConfig()
	ports.Id = "ports"
	ports.Name = "Ports"
	ports.Enabled = true
	ports.UDP.DPortFilter = "443"
	ports.UDP.Mode = config.UDPModeDrop
	ports.Fragmentation.Strategy = config.ConfigNone
	ports.Fragmentation.StrategyPool = nil

	w := newQUICFlowWorker(t, &learning, &ports)

	teach := buildQUICInitialWithSNI(t, []byte{0xF1, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, teach, 50811); v != engine.VerdictAccept {
		t.Fatalf("the learning set leaves its own name alone, got %v", v)
	}

	unrelated := buildQUICInitialWithSNI(t, []byte{0xF2, 1, 2, 3, 4, 5, 6, 7}, "www.unrelated.test")
	if v := sendQUIC(w, unrelated, 50812); v != engine.VerdictDropFlow {
		t.Errorf("a name no set targets on the learned address belongs to the port set, got %v, want the flow stopped", v)
	}
}

func TestCoalesceGivesUpOnAHelloTheServerKeepsIgnoring(t *testing.T) {
	set := newDomainOnlyQUICSet(config.UDPModeCoalesce)
	w := newQUICFlowWorker(t, &set)
	sent := captureInjected(t, w)

	hello := buildQUICInitialWithSNI(t, []byte{0x61, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	for attempt := 1; attempt <= maxCoalescedHellos; attempt++ {
		if v := sendQUIC(w, hello, 50901); v != engine.VerdictDrop {
			t.Fatalf("attempt %d must be coalesced, got %v", attempt, v)
		}
		if got := sent(); len(got) != 1 {
			t.Fatalf("attempt %d sent %d packets, want the coalesced datagram", attempt, len(got))
		}
	}
	for _, attempt := range []int{maxCoalescedHellos + 1, maxCoalescedHellos + 2} {
		if v := sendQUIC(w, hello, 50901); v != engine.VerdictAccept {
			t.Errorf("attempt %d must go out unchanged so the server sees a plain ClientHello, got %v", attempt, v)
		}
		if got := sent(); len(got) != 0 {
			t.Errorf("attempt %d sent %d packets of its own", attempt, len(got))
		}
	}

	later := buildQUICInitialWithSNI(t, []byte{0x62, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, later, 50901); v != engine.VerdictAccept {
		t.Errorf("a connection opened while the ports are still busy shares their limit, got %v", v)
	}
	ageQUICFlow(t, w, 50901, quicFlowShared+time.Second)
	newer := buildQUICInitialWithSNI(t, []byte{0x63, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, newer, 50901); v != engine.VerdictDrop {
		t.Errorf("once the ports have gone quiet a new connection gets its own limit, got %v", v)
	}
	if v := sendQUIC(w, hello, 50902); v != engine.VerdictDrop {
		t.Errorf("another flow to the same address keeps its own count, got %v", v)
	}
}

func ageQUICFlow(t *testing.T, w *Worker, sport uint16, by time.Duration) {
	t.Helper()
	key := fmt.Sprintf(connKeyFormat, quicClient.String(), sport, quicServer.String(), 443)
	w.quicFlows.mu.Lock()
	defer w.quicFlows.mu.Unlock()
	entry := w.quicFlows.flows[key]
	if entry == nil {
		t.Fatalf("no flow entry for port %d to age", sport)
	}
	entry.lastSeen = entry.lastSeen.Add(-by)
}

func TestASplitHelloCoalescesOncePerAttempt(t *testing.T) {
	set := newDomainOnlyQUICSet(config.UDPModeCoalesce)
	w := newQUICFlowWorker(t, &set)
	flow := newSplitQUICHello(t, []byte{0x63, 1, 2, 3, 4, 5, 6, 7}, []byte{0x64, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")

	for attempt := 1; attempt <= maxCoalescedHellos; attempt++ {
		if v := sendQUIC(w, flow.first, 50911); v != engine.VerdictAccept {
			t.Fatalf("attempt %d: the datagram that carries no complete ClientHello goes out plain, got %v", attempt, v)
		}
		if v := sendQUIC(w, flow.completing, 50911); v != engine.VerdictDrop {
			t.Fatalf("attempt %d: the datagram that completes the ClientHello must be coalesced, got %v", attempt, v)
		}
	}
	if v := sendQUIC(w, flow.completing, 50911); v != engine.VerdictAccept {
		t.Errorf("past the limit the ClientHello must reach the server unchanged, got %v", v)
	}
}

func TestALearnedAddressDoesNotClaimTheRestOfAnUnrelatedQUICFlow(t *testing.T) {
	dropper := newDomainOnlyQUICSet(config.UDPModeDrop)
	dropper.Targets.DomainOnly = false
	w := newQUICFlowWorker(t, &dropper)

	own := buildQUICInitialWithSNI(t, []byte{0xC1, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, own, 51001); v != engine.VerdictDropFlow {
		t.Fatalf("the set's own name teaches it the address, got %v", v)
	}

	unrelated := newSplitQUICHello(t, []byte{0xC2, 1, 2, 3, 4, 5, 6, 7}, []byte{0xC3, 1, 2, 3, 4, 5, 6, 7}, "www.unrelated.test")
	if v := sendQUIC(w, unrelated.first, 51002); v != engine.VerdictAccept {
		t.Fatalf("the first datagram of the unrelated flow carries no readable name, got %v", v)
	}
	if v := sendQUIC(w, unrelated.completing, 51002); v != engine.VerdictAccept {
		t.Fatalf("the unrelated name matches no set, got %v", v)
	}
	if v := sendQUIC(w, unrelated.resend, 51002); v != engine.VerdictAccept {
		t.Errorf("the resend of the unrelated flow: got %v, want accept", v)
	}
	if v := sendQUIC(w, unrelated.oneRTT, 51002); v != engine.VerdictAccept {
		t.Errorf("a short-header packet of the unrelated flow carries no name b4 can read, so the learned address must not claim it either: got %v, want accept", v)
	}
}

func TestCoalesceGivesUpForASetMatchedByItsDestination(t *testing.T) {
	set := config.NewSetConfig()
	set.Id = "ip-coalesce"
	set.Name = "IPCoalesce"
	set.Enabled = true
	set.Targets.IPs = []string{"1.2.3.4/32"}
	set.UDP.Mode = config.UDPModeCoalesce
	set.UDP.FilterQUIC = config.QUICFilterAll
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil

	w := newQUICFlowWorker(t, &set)
	hello := buildQUICInitialWithSNI(t, []byte{0xC4, 1, 2, 3, 4, 5, 6, 7}, "www.unrelated.test")

	for attempt := 1; attempt <= maxCoalescedHellos; attempt++ {
		if v := sendQUIC(w, hello, 51101); v != engine.VerdictDrop {
			t.Fatalf("attempt %d of a set matched by address must be coalesced, got %v", attempt, v)
		}
	}
	if v := sendQUIC(w, hello, 51101); v != engine.VerdictAccept {
		t.Errorf("a set matched by address must stop coalescing a repeated ClientHello too, got %v", v)
	}
}

func TestALearnedAddressDoesNotClaimAQUICFlowItCouldNotName(t *testing.T) {
	dropper := newDomainOnlyQUICSet(config.UDPModeDrop)
	dropper.Targets.DomainOnly = false
	w := newQUICFlowWorker(t, &dropper)

	own := buildQUICInitialWithSNI(t, []byte{0xB1, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, own, 52001); v != engine.VerdictDropFlow {
		t.Fatalf("the set's own name teaches it the address, got %v", v)
	}

	nameless := append([]byte{0xc3, 0, 0, 0, 1, 8, 0xB2, 2, 3, 4, 5, 6, 7, 8, 0}, make([]byte, 60)...)
	if v := sendQUIC(w, nameless, 52002); v != engine.VerdictAccept {
		t.Fatalf("an Initial whose ClientHello b4 cannot read, got %v", v)
	}
	oneRTT := append(append([]byte{0x41}, []byte{0xB3, 2, 3, 4, 5, 6, 7, 8}...), make([]byte, 40)...)
	if v := sendQUIC(w, oneRTT, 52002); v != engine.VerdictAccept {
		t.Errorf("a short-header packet carries no name either, so the flow whose Initial b4 saw must keep passing: got %v, want accept", v)
	}
}

func TestMatchingQUICByDestinationKeepsTheRestOfAKnownFlow(t *testing.T) {
	faking := newDomainOnlyQUICSet(config.UDPModeFake)
	faking.Targets.DomainOnly = false
	faking.UDP.FilterQUIC = config.QUICFilterAll
	w := newQUICFlowWorker(t, &faking)
	sent := captureInjected(t, w)

	flow := newSplitQUICHello(t, []byte{0xB4, 1, 2, 3, 4, 5, 6, 7}, []byte{0xB5, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	sendQUIC(w, flow.first, 52101)
	sent()
	if v := sendQUIC(w, flow.completing, 52101); v != engine.VerdictDrop {
		t.Fatalf("the ClientHello of the set's own name must be handled, got %v", v)
	}
	sent()

	if v := sendQUIC(w, flow.oneRTT, 52101); v != engine.VerdictDrop {
		t.Errorf("By SNI or matched destination asks for every QUIC packet to a matched address, short headers included, got %v", v)
	}
	if got := sent(); len(got) == 0 {
		t.Error("no packet was sent for the short-header packet of the known flow")
	}
}

func TestAnExpiredFlowEntryDoesNotBlockANewOneOnTheSameTuple(t *testing.T) {
	dropper := newDomainOnlyQUICSet(config.UDPModeDrop)
	dropper.Targets.DomainOnly = false
	w := newQUICFlowWorker(t, &dropper)

	own := buildQUICInitialWithSNI(t, []byte{0xA3, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, own, 53101); v != engine.VerdictDropFlow {
		t.Fatalf("the set's own name teaches it the address, got %v", v)
	}

	nameless := append([]byte{0xc3, 0, 0, 0, 1, 8, 0xA4, 2, 3, 4, 5, 6, 7, 8, 0}, make([]byte, 60)...)
	if v := sendQUIC(w, nameless, 53102); v != engine.VerdictAccept {
		t.Fatalf("the first nameless Initial must pass, got %v", v)
	}

	key := fmt.Sprintf(connKeyFormat, quicClient.String(), 53102, quicServer.String(), 443)
	w.quicFlows.mu.Lock()
	entry := w.quicFlows.flows[key]
	if entry == nil {
		w.quicFlows.mu.Unlock()
		t.Fatal("the nameless Initial left no entry to expire")
	}
	entry.lastSeen = time.Now().Add(-quicFlowIdle - time.Second)
	w.quicFlows.mu.Unlock()

	if v := sendQUIC(w, nameless, 53102); v != engine.VerdictAccept {
		t.Fatalf("a nameless Initial on the reused tuple must pass, got %v", v)
	}
	oneRTT := append(append([]byte{0x41}, []byte{0xA4, 2, 3, 4, 5, 6, 7, 8}...), make([]byte, 40)...)
	if v := sendQUIC(w, oneRTT, 53102); v != engine.VerdictAccept {
		t.Errorf("the short-header packets of the flow that replaced the expired entry must pass: got %v, want accept", v)
	}
}

func TestAGreasedShortHeaderOfAnUnrelatedQUICFlowPasses(t *testing.T) {
	dropper := newDomainOnlyQUICSet(config.UDPModeDrop)
	dropper.Targets.DomainOnly = false
	w := newQUICFlowWorker(t, &dropper)

	own := buildQUICInitialWithSNI(t, []byte{0xF5, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	if v := sendQUIC(w, own, 53301); v != engine.VerdictDropFlow {
		t.Fatalf("the set's own name teaches it the address, got %v", v)
	}

	unrelated := newSplitQUICHello(t, []byte{0xF6, 1, 2, 3, 4, 5, 6, 7}, []byte{0xF7, 1, 2, 3, 4, 5, 6, 7}, "www.unrelated.test")
	sendQUIC(w, unrelated.first, 53302)
	if v := sendQUIC(w, unrelated.completing, 53302); v != engine.VerdictAccept {
		t.Fatalf("the unrelated name matches no set, got %v", v)
	}

	greased := append(append([]byte{0x01}, []byte{0xF7, 1, 2, 3, 4, 5, 6, 7}...), make([]byte, 40)...)
	greased[1] = 0x5a
	if v := sendQUIC(w, greased, 53302); v != engine.VerdictAccept {
		t.Errorf("a short-header packet with the QUIC bit greased away (RFC 9287) belongs to the unrelated flow: got %v, want accept", v)
	}
}

func TestOtherProtocolsOnATupleThatCarriedQUICStillFollowTheLearnedAddress(t *testing.T) {
	wireguard := make([]byte, 148)
	wireguard[0] = 0x01
	dtls := append([]byte{0x16, 0xfe, 0xfd, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x30}, make([]byte, 48)...)
	srtp := make([]byte, 60)
	srtp[0], srtp[1] = 0x80, 0x60

	for _, c := range []struct {
		name    string
		payload []byte
	}{
		{"WireGuard", wireguard},
		{"DTLS", dtls},
		{"SRTP", srtp},
	} {
		t.Run(c.name, func(t *testing.T) {
			if quic.LooksLikeQUIC(c.payload) {
				t.Fatalf("%s must not look like a long-header QUIC packet, or this case tests nothing", c.name)
			}
			dropper := newDomainOnlyQUICSet(config.UDPModeDrop)
			dropper.Targets.DomainOnly = false
			dropper.UDP.FilterSTUN = false
			w := newQUICFlowWorker(t, &dropper)

			own := buildQUICInitialWithSNI(t, []byte{0xF8, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
			if v := sendQUIC(w, own, 53401); v != engine.VerdictDropFlow {
				t.Fatalf("the set's own name teaches it the address, got %v", v)
			}
			nameless := append([]byte{0xc3, 0, 0, 0, 1, 8, 0xF9, 2, 3, 4, 5, 6, 7, 8, 0}, make([]byte, 60)...)
			if v := sendQUIC(w, nameless, 53402); v != engine.VerdictAccept {
				t.Fatalf("an Initial b4 cannot name must pass and mark the flow, got %v", v)
			}
			if v := sendQUIC(w, c.payload, 53402); v != engine.VerdictDrop {
				t.Errorf("%s on a tuple that once carried QUIC still follows the learned address: got %v, want drop", c.name, v)
			}
		})
	}
}

func TestInterleavedQUICConnectionsShareTheCoalesceBudgetOfTheirTuple(t *testing.T) {
	set := newDomainOnlyQUICSet(config.UDPModeCoalesce)
	w := newQUICFlowWorker(t, &set)

	first := buildQUICInitialWithSNI(t, []byte{0xE5, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
	second := buildQUICInitialWithSNI(t, []byte{0xE6, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")

	if v := sendQUIC(w, first, 53501); v != engine.VerdictDrop {
		t.Fatalf("the first connection's ClientHello must be coalesced, got %v", v)
	}
	if v := sendQUIC(w, second, 53501); v != engine.VerdictDrop {
		t.Fatalf("a second connection on the same ports must be coalesced too, got %v", v)
	}
	if v := sendQUIC(w, first, 53501); v != engine.VerdictAccept {
		t.Errorf("connections sharing one tuple must not reset each other's limit, so this repeat goes out unchanged: got %v, want accept", v)
	}
}
