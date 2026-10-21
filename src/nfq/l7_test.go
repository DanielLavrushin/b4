package nfq

import (
	"encoding/binary"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/sock"
)

func discordIPDiscovery() []byte {
	pkt := make([]byte, 74)
	binary.BigEndian.PutUint16(pkt[0:2], 1)
	binary.BigEndian.PutUint16(pkt[2:4], 70)
	binary.BigEndian.PutUint32(pkt[4:8], 0xDEADBEEF)
	return pkt
}

func stunBindingRequest() []byte {
	pkt := make([]byte, 20)
	binary.BigEndian.PutUint16(pkt[0:2], 0x0001)
	binary.BigEndian.PutUint32(pkt[4:8], 0x2112A442)
	copy(pkt[8:], "b4-l7-txid-1")
	return pkt
}

func rtpOpus() []byte {
	pkt := make([]byte, 60)
	pkt[0], pkt[1] = 0x80, 0x78
	return pkt
}

func newVoiceL7Set(classes ...string) config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "voice-l7"
	set.Name = "VoiceL7"
	set.Enabled = true
	set.UDP.Mode = config.UDPModeDrop
	set.UDP.DPortFilter = "50000-50100"
	set.UDP.FilterL7 = classes
	set.Fragmentation.Strategy = config.ConfigNone
	set.Fragmentation.StrategyPool = nil
	return set
}

func sendUDPTo(w *Worker, payload []byte, sport, dport uint16) engine.PacketVerdict {
	return w.ProcessPacket(makeV4UDPPacket(payload, quicClient, quicServer, sport, dport))
}

func TestAnL7SetActsOnlyOnThePayloadsItSelects(t *testing.T) {
	for _, c := range []struct {
		name    string
		classes []string
		payload []byte
		want    engine.PacketVerdict
	}{
		{"discord selects the IP discovery request", []string{config.UDPL7Discord}, discordIPDiscovery(), engine.VerdictDrop},
		{"discord leaves STUN alone", []string{config.UDPL7Discord}, stunBindingRequest(), engine.VerdictAccept},
		{"stun selects the binding request", []string{config.UDPL7STUN}, stunBindingRequest(), engine.VerdictDrop},
		{"stun leaves the IP discovery request alone", []string{config.UDPL7STUN}, discordIPDiscovery(), engine.VerdictAccept},
		{"both classes select both payloads", []string{config.UDPL7Discord, config.UDPL7STUN}, discordIPDiscovery(), engine.VerdictDrop},
		{"later media of the flow is not selected", []string{config.UDPL7Discord, config.UDPL7STUN}, rtpOpus(), engine.VerdictAccept},
		{"an unselected payload passes", []string{config.UDPL7Discord}, []byte("plain udp payload"), engine.VerdictAccept},
		{"an unknown class matches nothing", []string{"wireguard"}, discordIPDiscovery(), engine.VerdictAccept},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newVoiceL7Set(c.classes...)
			w := newQUICFlowWorker(t, &set)
			if v := sendUDPTo(w, c.payload, 51000, 50005); v != c.want {
				t.Fatalf("ProcessPacket returned %v, want %v", v, c.want)
			}
		})
	}
}

func TestAnL7SetIgnoresFilterSTUN(t *testing.T) {
	set := newVoiceL7Set(config.UDPL7Discord, config.UDPL7STUN)
	set.UDP.FilterSTUN = true
	w := newQUICFlowWorker(t, &set)
	if v := sendUDPTo(w, discordIPDiscovery(), 51001, 50005); v != engine.VerdictDrop {
		t.Fatalf("an L7 set must act on the packets it selects whatever filter_stun says, got %v", v)
	}
}

func TestAnL7SetOnlyActsOnItsOwnPorts(t *testing.T) {
	set := newVoiceL7Set(config.UDPL7Discord)
	w := newQUICFlowWorker(t, &set)
	if v := sendUDPTo(w, discordIPDiscovery(), 51002, 40000); v != engine.VerdictAccept {
		t.Fatalf("a port outside the filter must not be selected, got %v", v)
	}
}

func TestAnL7SetWinsOverAnAddressSetThatWouldExemptVoice(t *testing.T) {
	addressSet := config.NewSetConfig()
	addressSet.Id = "addr-set"
	addressSet.Name = "AddrSet"
	addressSet.Enabled = true
	addressSet.Targets.IPs = []string{"1.2.3.4/32"}
	addressSet.UDP.Mode = config.UDPModeFake
	addressSet.UDP.FilterSTUN = true
	addressSet.Fragmentation.Strategy = config.ConfigNone
	addressSet.Fragmentation.StrategyPool = nil

	voice := newVoiceL7Set(config.UDPL7Discord)

	w := newQUICFlowWorker(t, &addressSet, &voice)
	if v := sendUDPTo(w, discordIPDiscovery(), 51003, 50005); v != engine.VerdictDrop {
		t.Fatalf("the L7 set must claim the packet from the address set, got %v", v)
	}
}

func TestAnEscalatedHostDoesNotTakeAPacketAnL7SetSelected(t *testing.T) {
	const host = "c-fra10-1234.discord.media"
	for _, c := range []struct {
		name          string
		voiceMode     string
		escalatedMode string
		want          engine.PacketVerdict
	}{
		{"the L7 set's own mode applies", config.UDPModeDrop, config.ConfigOff, engine.VerdictDrop},
		{"the escalated set's mode and filter_stun do not apply", config.ConfigOff, config.UDPModeDrop, engine.VerdictAccept},
	} {
		t.Run(c.name, func(t *testing.T) {
			escalated := config.NewSetConfig()
			escalated.Id = "media-escalated"
			escalated.Name = "MediaEscalated"
			escalated.Enabled = true
			escalated.Targets.SNIDomains = []string{"escalated.example"}
			escalated.UDP.Mode = c.escalatedMode
			escalated.UDP.FilterSTUN = true
			escalated.Fragmentation.Strategy = config.ConfigNone
			escalated.Fragmentation.StrategyPool = nil

			media := config.NewSetConfig()
			media.Id = "discord-media"
			media.Name = "DiscordMedia"
			media.Enabled = true
			media.Targets.SNIDomains = []string{"discord.media"}
			media.UDP.Mode = config.ConfigOff
			media.Escalate.To = escalated.Id
			media.Fragmentation.Strategy = config.ConfigNone
			media.Fragmentation.StrategyPool = nil

			voice := newVoiceL7Set(config.UDPL7Discord)
			voice.UDP.Mode = c.voiceMode

			w := newQUICFlowWorker(t, &voice, &media, &escalated)
			w.matcher.Load().(*sni.SuffixSet).LearnIPToDomain(quicServer, host, &media)
			w.destState.SetEscalation(host, escalated.Id, escalateReasonStall, time.Minute)

			if v := sendUDPTo(w, discordIPDiscovery(), 51006, 50005); v != c.want {
				t.Fatalf("a packet the L7 set selected belongs to that set even when its address was learned for an escalated host, got %v, want %v", v, c.want)
			}
		})
	}
}

func TestAnL7SetSelectsOnlyItsIPVersion(t *testing.T) {
	for _, c := range []struct {
		name    string
		version string
		v4, v6  engine.PacketVerdict
	}{
		{"no restriction selects both", "", engine.VerdictDrop, engine.VerdictDrop},
		{"IPv4 only leaves IPv6 alone", "4", engine.VerdictDrop, engine.VerdictAccept},
		{"IPv6 only leaves IPv4 alone", "6", engine.VerdictAccept, engine.VerdictDrop},
	} {
		t.Run(c.name, func(t *testing.T) {
			set := newVoiceL7Set(config.UDPL7Discord)
			set.Targets.IPVersion = c.version
			cfg := newUDPGateConfig(t, &set)
			cfg.Queue.IPv6Enabled = true
			w := newTestWorker(t, &cfg)
			w.sock = &sock.Sender{}
			t.Cleanup(w.wg.Wait)
			if v := sendUDPTo(w, discordIPDiscovery(), 51010, 50005); v != c.v4 {
				t.Fatalf("IPv4 packet: got %v, want %v", v, c.v4)
			}
			if v := w.ProcessPacket(makeV6UDPPacketWithExt(discordIPDiscovery(), 51011, 50005, 0, nil)); v != c.v6 {
				t.Fatalf("IPv6 packet: got %v, want %v", v, c.v6)
			}
		})
	}
}

func TestAPortOnlySetDoesNotClaimTheL7SetsPorts(t *testing.T) {
	portOnly := config.NewSetConfig()
	portOnly.Id = "port-only"
	portOnly.Name = "PortOnly"
	portOnly.Enabled = true
	portOnly.UDP.Mode = config.UDPModeDrop
	portOnly.UDP.DPortFilter = "50000-50100"
	portOnly.Fragmentation.Strategy = config.ConfigNone
	portOnly.Fragmentation.StrategyPool = nil

	voice := newVoiceL7Set(config.UDPL7Discord)
	voice.UDP.Mode = config.ConfigOff

	w := newQUICFlowWorker(t, &voice, &portOnly)
	if v := sendUDPTo(w, discordIPDiscovery(), 51004, 50005); v != engine.VerdictAccept {
		t.Fatalf("the L7 set claims its own ports, so the port-only set must not drop the packet, got %v", v)
	}
	if v := sendUDPTo(w, []byte("plain udp payload"), 51005, 50005); v != engine.VerdictDrop {
		t.Fatalf("a payload no L7 set selects still belongs to the port-only set, got %v", v)
	}
}
