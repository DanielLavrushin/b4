package nfq

import (
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
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
			if v := sendQUIC(w, flow.completing, 50001); v != engine.VerdictDrop {
				t.Fatalf("the datagram that completes the ClientHello must be stopped, got %v", v)
			}
			if v := sendQUIC(w, flow.resend, 50001); v != engine.VerdictDrop {
				t.Errorf("the resend under the server's connection ID belongs to the same flow and must be stopped, got %v", v)
			}
			if v := sendQUIC(w, flow.oneRTT, 50001); v != engine.VerdictDrop {
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
	if v := sendQUIC(w, flow.completing, 50201); v != engine.VerdictDrop {
		t.Fatalf("the blocked name's ClientHello must be stopped, got %v", v)
	}

	other := buildQUICInitialWithSNI(t, []byte{0x82, 1, 2, 3, 4, 5, 6, 7}, "www.allowed.example")
	if v := sendQUIC(w, other, 50202); v != engine.VerdictAccept {
		t.Fatalf("a connection to an allowed name on the same address must pass, got %v", v)
	}

	if v := sendQUIC(w, flow.resend, 50201); v != engine.VerdictDrop {
		t.Errorf("the stopped flow's resend must stay stopped after another set learned the address, got %v", v)
	}
	if v := sendQUIC(w, flow.oneRTT, 50201); v != engine.VerdictDrop {
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
	if v := sendQUIC(w, flow.completing, 50302); v != engine.VerdictDrop {
		t.Fatalf("the blocked name's ClientHello must be stopped, got %v", v)
	}
	if v := sendQUIC(w, flow.resend, 50302); v != engine.VerdictDrop {
		t.Errorf("a flow to a blocked name must stay stopped on an address the coalescing set learned, got %v", v)
	}
	if v := sendQUIC(w, flow.oneRTT, 50302); v != engine.VerdictDrop {
		t.Errorf("a short-header packet of the blocked flow must stay stopped, got %v", v)
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
	if v := sendQUIC(w, flow.completing, 50006); v != engine.VerdictDrop {
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
	if v := sendQUIC(w, flow.completing, 50007); v != engine.VerdictDrop {
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

	if host := c.Lookup("k", short); host != "www.blocked.example" {
		t.Fatalf("lookup of a fresh flow returned %q", host)
	}
	c.flows["k"].lastSeen = time.Now().Add(-quicFlowIdle - time.Second)
	if host := c.Lookup("k", short); host != "" {
		t.Errorf("an idle flow must not name later datagrams, got %q", host)
	}
	c.Cleanup()
	if len(c.flows) != 0 {
		t.Errorf("cleanup kept %d idle flows", len(c.flows))
	}
}
