package nfq

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sni"
	"github.com/florianl/go-nfqueue"
)

var voiceServer = net.ParseIP("104.29.140.154")

const voicePort = 19305

func voiceRTP(b0, b1 byte, size int) []byte {
	p := make([]byte, size)
	p[0], p[1] = b0, b1
	binary.BigEndian.PutUint16(p[2:4], 0x1234)
	binary.BigEndian.PutUint32(p[4:8], 0x00010000)
	binary.BigEndian.PutUint32(p[8:12], 0x0000abcd)
	for i := 12; i < size; i++ {
		p[i] = byte(i*7 + 3)
	}
	return p
}

func voiceRTCP(rc, pt byte, words int) []byte {
	p := make([]byte, 4*(words+1))
	p[0] = 0x80 | rc
	p[1] = pt
	binary.BigEndian.PutUint16(p[2:4], uint16(words))
	binary.BigEndian.PutUint32(p[4:8], 0x0000abcd)
	for i := 8; i < len(p); i++ {
		p[i] = byte(i*5 + 1)
	}
	return p
}

func voiceSTUN(msgType uint16, body []byte) []byte {
	p := make([]byte, 20+len(body))
	binary.BigEndian.PutUint16(p[0:2], msgType)
	binary.BigEndian.PutUint16(p[2:4], uint16(len(body)))
	binary.BigEndian.PutUint32(p[4:8], 0x2112A442)
	copy(p[8:20], "txid-voice-1")
	copy(p[20:], body)
	return p
}

func voiceDTLS(ct byte, epoch uint16, hsType byte, size int) []byte {
	p := make([]byte, size)
	p[0] = ct
	p[1], p[2] = 0xFE, 0xFD
	binary.BigEndian.PutUint16(p[3:5], epoch)
	binary.BigEndian.PutUint16(p[11:13], uint16(size-13))
	if size > 13 {
		p[13] = hsType
	}
	for i := 14; i < size; i++ {
		p[i] = byte(i*11 + 5)
	}
	return p
}

type voicePacket struct {
	name     string
	payload  []byte
	selected bool
}

func browserCall() []voicePacket {
	return []voicePacket{
		{"STUN binding request", voiceSTUN(0x0001, make([]byte, 24)), true},
		{"DTLS ClientHello", voiceDTLS(22, 0, 1, 240), false},
		{"DTLS Finished", voiceDTLS(22, 1, 20, 61), false},
		{"SRTP opus PT111", voiceRTP(0x90, 0x6F, 120), false},
		{"SRTCP receiver report", voiceRTCP(1, 201, 9), false},
		{"STUN binding success response", voiceSTUN(0x0101, make([]byte, 12)), false},
		{"STUN consent request", voiceSTUN(0x0001, make([]byte, 24)), true},
	}
}

func desktopCall() []voicePacket {
	return []voicePacket{
		{"IP discovery", discordIPDiscovery(), true},
		{"RTP opus PT120", voiceRTP(0x80, 0x78, 80), false},
		{"RTCP sender report", voiceRTCP(0, 200, 6), false},
		{"RTP video PT101", voiceRTP(0x90, 0x65, 1100), false},
		{"RTP video PT103 marker", voiceRTP(0x90, 0xE7, 1100), false},
	}
}

func voiceAddressSet(ports string) config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "cloudflare-ip"
	set.Name = "CloudflareIP"
	set.Enabled = true
	set.Targets.IPs = []string{"104.29.128.0/18", "2001:db8::/64"}
	set.UDP.Mode = config.UDPModeReject
	set.UDP.FilterSTUN = true
	set.UDP.DPortFilter = ports
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	return set
}

func voiceFakeSet() config.SetConfig {
	set := newVoiceL7Set(config.UDPL7Discord, config.UDPL7STUN)
	set.UDP.Mode = config.UDPModeFake
	set.UDP.FakePayloadFile = config.FakePayloadAutoSTUN
	set.UDP.FakeLen = 64
	set.UDP.FakeSeqLength = 1
	set.UDP.DPortFilter = "19294-19344,50000-50100"
	set.Faking.TTL = 7
	return set
}

func sendVoice(w *Worker, payload []byte, sport, dport uint16) engine.PacketVerdict {
	return w.ProcessPacket(makeV4UDPPacket(payload, quicClient, voiceServer, sport, dport))
}

func TestAnAddressSetWithoutAPortFilterLeavesPortsQueuedOnlyForProtocolSelection(t *testing.T) {
	addr := voiceAddressSet("")
	voice := voiceFakeSet()
	w := newQUICFlowWorker(t, &addr, &voice)
	sent := captureInjected(t, w)

	for i, p := range []voicePacket{
		{"DTLS ClientHello", voiceDTLS(22, 0, 1, 240), false},
		{"SRTP opus PT111", voiceRTP(0x90, 0x6F, 120), false},
		{"SRTCP receiver report", voiceRTCP(1, 201, 9), false},
		{"desktop RTCP sender report", voiceRTCP(0, 200, 6), false},
		{"RTP video PT101", voiceRTP(0x90, 0x65, 1100), false},
		{"STUN binding success response", voiceSTUN(0x0101, make([]byte, 12)), false},
	} {
		if v := sendVoice(w, p.payload, uint16(52000+i), voicePort); v != engine.VerdictAccept {
			t.Errorf("%s to %d: verdict %v, the address set has no UDP port filter and the port is queued only for the protocol-selection set", p.name, voicePort, v)
		}
		if got := sent(); len(got) != 0 {
			t.Errorf("%s to %d: %d packets sent, want none and no ICMP reject", p.name, voicePort, len(got))
		}
	}
}

func TestAnAddressSetStillActsOnPortsNotLeftToProtocolSelection(t *testing.T) {
	dtls := voiceDTLS(22, 0, 1, 240)
	for _, c := range []struct {
		name     string
		addrPort string
		voice    string
		other    string
		disabled bool
		dport    uint16
	}{
		{"443 even when the protocol-selection set lists it", "", "443,19294-19344", "", false, 443},
		{"a port another set lists explicitly", "", "19294-19344", "19300-19310", false, voicePort},
		{"a voice port its own list names", "19294-19344", "19294-19344", "", false, voicePort},
		{"a voice port while the protocol-selection set is disabled", "", "19294-19344", "", true, voicePort},
	} {
		t.Run(c.name, func(t *testing.T) {
			addr := voiceAddressSet(c.addrPort)
			voice := voiceFakeSet()
			voice.UDP.DPortFilter = c.voice
			voice.Enabled = !c.disabled
			sets := []*config.SetConfig{&addr, &voice}
			if c.other != "" {
				other := config.NewSetConfig()
				other.Id, other.Name, other.Enabled = "other", "Other", true
				other.Targets.IPs = []string{"198.51.100.0/24"}
				other.UDP.Mode = config.ConfigOff
				other.UDP.DPortFilter = c.other
				sets = append(sets, &other)
			}
			w := newQUICFlowWorker(t, sets...)
			sent := captureInjected(t, w)
			if v := sendVoice(w, dtls, 52100, c.dport); v != engine.VerdictDrop {
				t.Fatalf("verdict %v, the address set must still reject UDP to %d", v, c.dport)
			}
			if got := sent(); len(got) != 1 {
				t.Fatalf("%d packets sent, want the ICMP reject", len(got))
			}
		})
	}
}

func TestADomainSetWithoutAPortFilterLeavesPortsQueuedOnlyForProtocolSelection(t *testing.T) {
	const host = "c-fra10-1234.discord.media"
	domainSet := func() config.SetConfig {
		set := config.NewSetConfig()
		set.Id, set.Name, set.Enabled = "discord-media", "DiscordMedia", true
		set.Targets.SNIDomains = []string{"discord.media"}
		set.UDP.Mode = config.UDPModeReject
		set.UDP.FilterSTUN = true
		set.UDP.FilterQUIC = "all"
		set.Fragmentation.Strategy = config.ConfigNone
		set.Fragmentation.StrategyPool = nil
		return set
	}

	t.Run("learned address", func(t *testing.T) {
		media := domainSet()
		voice := voiceFakeSet()
		w := newQUICFlowWorker(t, &media, &voice)
		sent := captureInjected(t, w)
		w.matcher.Load().(*sni.SuffixSet).LearnIPToDomain(voiceServer, host, &media)
		if v := sendVoice(w, voiceDTLS(22, 0, 1, 240), 52200, voicePort); v != engine.VerdictAccept {
			t.Fatalf("verdict %v, a set chosen by a learned address must leave the voice port alone", v)
		}
		if got := sent(); len(got) != 0 {
			t.Fatalf("%d packets sent, want none", len(got))
		}
		if v := sendVoice(w, voiceDTLS(22, 0, 1, 240), 52201, 443); v != engine.VerdictDrop {
			t.Fatalf("verdict %v, the learned set still acts on 443", v)
		}
	})

	t.Run("QUIC server name", func(t *testing.T) {
		media := domainSet()
		voice := voiceFakeSet()
		w := newQUICFlowWorker(t, &media, &voice)
		sent := captureInjected(t, w)
		hello := buildQUICInitialWithSNI(t, []byte{0xd7, 1, 2, 3, 4, 5, 6, 7}, host)
		if v := sendVoice(w, hello, 52202, voicePort); v != engine.VerdictAccept {
			t.Fatalf("verdict %v, a set chosen by the QUIC server name must leave the voice port alone", v)
		}
		if got := sent(); len(got) != 0 {
			t.Fatalf("%d packets sent, want none", len(got))
		}
	})
}

func runVoiceCall(t *testing.T, w *Worker, sent func() [][]byte, lines chan string, call []voicePacket, send func([]byte) engine.PacketVerdict) {
	t.Helper()
	for i, p := range call {
		v := send(p.payload)
		got := sent()
		seen := drainLines(lines)
		if i == 0 {
			if v != engine.VerdictDrop || len(got) != 2 {
				t.Fatalf("%s opens the flow: verdict %v with %d packets sent, want the original dropped and sent again behind one decoy", p.name, v, len(got))
			}
			continue
		}
		if v != engine.VerdictAccept {
			t.Errorf("%s: verdict %v, a later packet of the voice flow must pass", p.name, v)
		}
		if len(got) != 0 {
			t.Errorf("%s: %d packets sent, want no decoy and no ICMP reject", p.name, len(got))
		}
		if len(seen) != 1 || !strings.Contains(seen[0], ",VoiceL7,") {
			t.Errorf("%s: want one connection line under the voice set, got %v", p.name, seen)
			continue
		}
		if labelled := strings.HasSuffix(seen[0], ","+voiceExemptReason); labelled == p.selected {
			t.Errorf("%s: selected=%v, but the line is %q", p.name, p.selected, seen[0])
		}
	}
}

func TestAVoiceFlowBelongsToTheProtocolSelectionSetThatSelectedIt(t *testing.T) {
	for _, ports := range []string{"", "443,19294-19344"} {
		for name, call := range map[string][]voicePacket{"browser": browserCall(), "desktop": desktopCall()} {
			t.Run(fmt.Sprintf("%s, address set ports %q", name, ports), func(t *testing.T) {
				addr := voiceAddressSet(ports)
				voice := voiceFakeSet()
				w := newQUICFlowWorker(t, &addr, &voice)
				sent := captureInjected(t, w)
				lines := connectionLines(t)
				runVoiceCall(t, w, sent, lines, call, func(payload []byte) engine.PacketVerdict {
					return sendVoice(w, payload, 52300, voicePort)
				})
			})
		}
	}
}

func TestAVoiceFlowOverIPv6BelongsToTheProtocolSelectionSet(t *testing.T) {
	addr := voiceAddressSet("19294-19344")
	voice := voiceFakeSet()
	cfg := newUDPGateConfig(t, &addr, &voice)
	cfg.Queue.IPv6Enabled = true
	w := newTestWorker(t, &cfg)
	t.Cleanup(w.wg.Wait)
	sent := captureInjected(t, w)
	lines := connectionLines(t)
	runVoiceCall(t, w, sent, lines, browserCall(), func(payload []byte) engine.PacketVerdict {
		return w.ProcessPacket(makeV6UDPPacketWithExt(payload, 52400, voicePort, 0, nil))
	})
}

func TestAVoiceFlowInTUNModeBelongsToTheProtocolSelectionSet(t *testing.T) {
	addr := voiceAddressSet("19294-19344")
	voice := voiceFakeSet()
	cfg := newUDPGateConfig(t, &addr, &voice)
	cfg.Queue.Mode = "tun"
	w := newTestWorker(t, &cfg)
	t.Cleanup(w.wg.Wait)
	sent := captureInjected(t, w)
	lines := connectionLines(t)
	runVoiceCall(t, w, sent, lines, desktopCall(), func(payload []byte) engine.PacketVerdict {
		return w.ProcessPacketDir(makeV4UDPPacket(payload, quicClient, voiceServer, 52500, voicePort), engine.DirUnknown)
	})
}

func TestAnIdleVoiceFlowFallsBackToTheAddressSet(t *testing.T) {
	addr := voiceAddressSet("19294-19344")
	voice := voiceFakeSet()
	w := newQUICFlowWorker(t, &addr, &voice)
	sent := captureInjected(t, w)

	if v := sendVoice(w, discordIPDiscovery(), 52600, voicePort); v != engine.VerdictDrop || len(sent()) != 2 {
		t.Fatalf("verdict %v, the IP discovery request opens the flow behind a decoy", v)
	}
	key := fmt.Sprintf(connKeyFormat, quicClient.String(), 52600, voiceServer.String(), voicePort)
	w.voiceFlows.mu.Lock()
	flow := w.voiceFlows.flows[key]
	if flow != nil {
		flow.lastSeen = time.Now().Add(-voiceFlowIdle - time.Second)
	}
	w.voiceFlows.mu.Unlock()
	if flow == nil {
		t.Fatal("the selected packet must open a voice flow")
	}

	if v := sendVoice(w, voiceRTP(0x90, 0x65, 1100), 52600, voicePort); v != engine.VerdictDrop {
		t.Fatalf("verdict %v, once the flow went idle the address set takes its packets again", v)
	}
	if got := sent(); len(got) != 1 {
		t.Fatalf("%d packets sent, want the address set's ICMP reject", len(got))
	}
	if v := sendVoice(w, discordIPDiscovery(), 52600, voicePort); v != engine.VerdictDrop || len(sent()) != 2 {
		t.Fatalf("verdict %v, a selected packet after the idle timeout opens the flow again behind a decoy", v)
	}
}

func TestAVoiceFlowInUseStaysOwnedPastTheIdleTimeout(t *testing.T) {
	if voiceFlowIdle < 30*time.Second || voiceFlowIdle > 120*time.Second {
		t.Fatalf("voiceFlowIdle %v: a flow must outlive the pauses between STUN consent checks and still expire within about a minute", voiceFlowIdle)
	}
	addr := voiceAddressSet("19294-19344")
	voice := voiceFakeSet()
	w := newQUICFlowWorker(t, &addr, &voice)
	sent := captureInjected(t, w)

	if v := sendVoice(w, discordIPDiscovery(), 52650, voicePort); v != engine.VerdictDrop || len(sent()) != 2 {
		t.Fatalf("verdict %v, the IP discovery request opens the flow behind a decoy", v)
	}
	key := fmt.Sprintf(connKeyFormat, quicClient.String(), 52650, voiceServer.String(), voicePort)
	age := func(by time.Duration) {
		w.voiceFlows.mu.Lock()
		if flow := w.voiceFlows.flows[key]; flow != nil {
			flow.lastSeen = time.Now().Add(-by)
		}
		w.voiceFlows.mu.Unlock()
	}
	for i := 0; i < 3; i++ {
		age(voiceFlowIdle - 5*time.Second)
		if v := sendVoice(w, voiceRTP(0x90, 0x65, 1100), 52650, voicePort); v != engine.VerdictAccept {
			t.Fatalf("round %d: verdict %v, traffic within the idle timeout keeps the flow owned", i, v)
		}
		if got := sent(); len(got) != 0 {
			t.Fatalf("round %d: %d packets sent, an owned packet passes untouched", i, len(got))
		}
		w.voiceFlows.mu.Lock()
		flow := w.voiceFlows.flows[key]
		var idle time.Duration
		if flow != nil {
			idle = time.Since(flow.lastSeen)
		}
		w.voiceFlows.mu.Unlock()
		if flow == nil || idle > 5*time.Second {
			t.Fatalf("round %d: idle %v, a packet of the flow must refresh its idle timer", i, idle)
		}
	}
}

func TestAVoiceFlowBelongsToTheSetThatSelectedIt(t *testing.T) {
	stun := newVoiceL7Set(config.UDPL7STUN)
	stun.Id, stun.Name = "stun-block", "StunBlock"
	stun.UDP.DPortFilter = "19294-19344,50000-50100"
	stun.UDP.Mode = config.ConfigOff
	stun.Routing.Enabled = true
	stun.Routing.Mode = config.RoutingModeBlock
	stun.Routing.BlockAction = config.BlockActionDrop
	discord := voiceFakeSet()
	discord.Id, discord.Name = "discord-fake", "DiscordFake"
	discord.UDP.FilterL7 = []string{config.UDPL7Discord}
	w := newQUICFlowWorker(t, &stun, &discord)
	sent := captureInjected(t, w)
	lines := connectionLines(t)

	if v := sendVoice(w, discordIPDiscovery(), 52680, voicePort); v != engine.VerdictDrop || len(sent()) != 2 {
		t.Fatalf("verdict %v, the discord set opens the flow behind a decoy", v)
	}
	drainLines(lines)
	for _, p := range [][]byte{voiceRTP(0x80, 0x78, 160), voiceRTP(0x90, 0x65, 1100), voiceRTCP(1, 200, 6)} {
		if v := sendVoice(w, p, 52680, voicePort); v != engine.VerdictAccept {
			t.Fatalf("verdict %v, the flow belongs to the discord set, not to the other protocol-selection set on the same ports", v)
		}
		for _, line := range drainLines(lines) {
			if strings.Contains(line, "StunBlock") || strings.HasSuffix(line, ",block") {
				t.Fatalf("the flow was credited to the other set: %s", line)
			}
		}
	}
}

func TestAProtocolSelectionBlockSetBlocksTheWholeFlow(t *testing.T) {
	for _, action := range []string{config.BlockActionDrop, config.BlockActionReject} {
		t.Run(action, func(t *testing.T) {
			voice := voiceFakeSet()
			voice.UDP.Mode = config.ConfigOff
			voice.Routing.Enabled = true
			voice.Routing.Mode = config.RoutingModeBlock
			voice.Routing.BlockAction = action
			w := newQUICFlowWorker(t, &voice)
			sent := captureInjected(t, w)
			lines := connectionLines(t)

			wantSent := 0
			if action == config.BlockActionReject {
				wantSent = 1
			}
			for _, p := range browserCall() {
				if v := sendVoice(w, p.payload, 52700, voicePort); v != engine.VerdictDrop {
					t.Errorf("%s: verdict %v, the block set owns the whole flow", p.name, v)
				}
				if got := sent(); len(got) != wantSent {
					t.Errorf("%s: %d packets sent, want %d", p.name, len(got), wantSent)
				}
				seen := drainLines(lines)
				if len(seen) != 2 || !strings.HasSuffix(seen[1], ",block") {
					t.Errorf("%s: want a match line and a block line, got %v", p.name, seen)
				}
				if len(seen) > 0 && strings.HasSuffix(seen[0], ","+voiceExemptReason) {
					t.Errorf("%s: a blocked packet must not carry the voice-exempt label: %s", p.name, seen[0])
				}
			}
			if v := sendVoice(w, voiceDTLS(22, 0, 1, 240), 52701, voicePort); v != engine.VerdictAccept {
				t.Fatalf("verdict %v, a flow the set never selected is not blocked", v)
			}
		})
	}
}

func TestAProtocolSelectionBlockSetOwnsFlowsOnlyFromItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	thisHost(t, scopeRouter)
	voice := newVoiceL7Set(config.UDPL7Discord)
	voice.UDP.Mode = config.ConfigOff
	scopeToGuest(&voice, config.BlockActionDrop)
	w := newQUICFlowWorker(t, &voice)

	packet := func(payload []byte, sport uint16) []byte {
		return makeV4UDPPacket(payload, quicClient, quicServer, sport, 50005)
	}
	media := voiceRTP(0x90, 0x65, 1100)

	if got := queuedVerdict(t, w, packet(discordIPDiscovery(), 52800), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: the voice set blocks the IP discovery request from br-guest", got)
	}
	if got := queuedVerdict(t, w, packet(media, 52800), nil, 0, idx["br-guest"]); got != nfqueue.NfDrop {
		t.Fatalf("verdict %d: the rest of the flow from br-guest is blocked too", got)
	}
	if got := queuedVerdict(t, w, packet(media, 52800), nil, 0, idx["br0"]); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: the set does not own the flow for a packet from br0", got)
	}
	if got := queuedVerdict(t, w, packet(discordIPDiscovery(), 52801), nil, 0, idx["br0"]); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: the IP discovery request from br0 is outside the set's source interfaces", got)
	}
	if got := queuedVerdict(t, w, packet(media, 52801), nil, 0, idx["br0"]); got != nfqueue.NfAccept {
		t.Fatalf("verdict %d: a flow from br0 does not belong to the set", got)
	}
}

func TestUDPDecoysUseTheDefaultTTLForZero(t *testing.T) {
	for _, c := range []struct {
		configured uint8
		want       byte
	}{{0, 5}, {64, 64}, {7, 7}} {
		t.Run(fmt.Sprint(c.configured), func(t *testing.T) {
			voice := voiceFakeSet()
			voice.Faking.TTL = c.configured
			cfg := newUDPGateConfig(t, &voice)
			cfg.Queue.IPv6Enabled = true
			w := newTestWorker(t, &cfg)
			t.Cleanup(w.wg.Wait)
			sent := captureInjected(t, w)

			if v := sendVoice(w, discordIPDiscovery(), 52900, voicePort); v != engine.VerdictDrop {
				t.Fatalf("verdict %v, want the IPv4 request dropped and sent again", v)
			}
			if got := sent(); len(got) != 2 || got[0][8] != c.want {
				t.Fatalf("IPv4 decoy: %d packets sent, want 2 with the decoy at TTL %d", len(got), c.want)
			}
			if v := w.ProcessPacket(makeV6UDPPacketWithExt(discordIPDiscovery(), 52901, voicePort, 0, nil)); v != engine.VerdictDrop {
				t.Fatalf("verdict %v, want the IPv6 request dropped and sent again", v)
			}
			if got := sent(); len(got) != 2 || got[0][7] != c.want {
				t.Fatalf("IPv6 decoy: %d packets sent, want 2 with the decoy at hop limit %d", len(got), c.want)
			}
		})
	}
}

func TestTheVoiceFlowCacheStaysBounded(t *testing.T) {
	c := newVoiceFlowCache()
	for i := 0; i <= maxVoiceFlowEntries; i++ {
		c.Store(fmt.Sprintf("flow-%d", i), "voice-l7")
	}
	if n := len(c.flows); n > maxVoiceFlowEntries {
		t.Fatalf("%d entries, want at most %d", n, maxVoiceFlowEntries)
	}
	if got := c.Lookup(fmt.Sprintf("flow-%d", maxVoiceFlowEntries)); got != "voice-l7" {
		t.Fatalf("the newest flow must be kept, got %q", got)
	}
}
