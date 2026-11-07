package nfq

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/iphealth"
)

const (
	tunDevice   = "192.168.1.50"
	tunWirePort = 44092
)

var tunSNATPorts = map[string]uint16{
	"port kept by SNAT":     tunWirePort,
	"port remapped by SNAT": 49874,
}

func tunTCPRow(devicePort uint16) string {
	return fmt.Sprintf("ipv4 2 tcp 6 431999 ESTABLISHED src=%s dst=1.2.3.4 sport=%d dport=443 src=1.2.3.4 dst=%s sport=443 dport=%d [ASSURED] use=1\n",
		tunDevice, devicePort, tunTestWAN, tunWirePort)
}

func onTheTunWire(raw []byte) []byte {
	copy(raw[12:16], net.ParseIP(tunTestWAN).To4())
	binary.BigEndian.PutUint16(raw[20:22], tunWirePort)
	return raw
}

func intoTun(t *testing.T, w *Worker, devicePort uint16) {
	t.Helper()
	w.getConfig().Queue.Mode = "tun"
	w.srcResolver = newResolverWith(t, tunTestWAN, tunTCPRow(devicePort))
}

func serverResets(sent [][]byte) []string {
	var to []string
	for _, p := range sent {
		if len(p) >= 40 && p[0]>>4 == IPv4 && p[9] == 6 && p[33]&0x04 != 0 && binary.BigEndian.Uint16(p[20:22]) == 443 {
			to = append(to, fmt.Sprintf("%s:%d", net.IP(p[16:20]), binary.BigEndian.Uint16(p[22:24])))
		}
	}
	slices.Sort(to)
	return to
}

func TestATunResetOnAReusedTupleGoesToTheNewConnection(t *testing.T) {
	thisHost(t)
	set := banningAddressSet()
	w := newInjectingWorker(t, &set)
	intoTun(t, w, 49874)
	w.bans = newTestIPBans(t, nil)
	banAddress(t, w.bans, "1.2.3.4", 443)
	sent := captureInjected(t, w)

	w.ProcessPacket(onTheTunWire(makeV4TCPPacketFlags(nil, 1000, 0x02)))
	if err := os.WriteFile(w.srcResolver.path, []byte(tunTCPRow(tunWirePort)), 0644); err != nil {
		t.Fatal(err)
	}
	w.ProcessPacket(onTheTunWire(makeV4TCPPacketFlags(nil, 7000, 0x02)))

	want := []string{fmt.Sprintf("%s:%d", tunDevice, tunWirePort), fmt.Sprintf("%s:%d", tunDevice, 49874)}
	if got := serverResets(sent()); !slices.Equal(got, want) {
		t.Fatalf("a new connection on a reused tuple must be reset at its own port, got resets to %v, want %v", got, want)
	}
}

func TestATunResetGoesToTheDevicesOwnPort(t *testing.T) {
	sites := map[string]func(t *testing.T, devicePort uint16) [][]byte{
		"a SYN to a banned address": func(t *testing.T, devicePort uint16) [][]byte {
			set := banningAddressSet()
			w := newInjectingWorker(t, &set)
			intoTun(t, w, devicePort)
			w.bans = newTestIPBans(t, nil)
			banAddress(t, w.bans, "1.2.3.4", 443)
			sent := captureInjected(t, w)
			w.ProcessPacket(onTheTunWire(bareSyn(IPv4)))
			return sent()
		},
		"a SYN to a dead address": func(t *testing.T, devicePort uint16) [][]byte {
			set := banningAddressSet()
			set.TCP.IPBlockDetect.CacheBlockedIPs = false
			set.TCP.IPBlockDetect.SynDetect = true
			w := newInjectingWorker(t, &set)
			intoTun(t, w, devicePort)
			sent := captureInjected(t, w)
			w.ipHealth = iphealth.NewTracker(func(string, uint16, int) bool { return false })
			t.Cleanup(w.ipHealth.Stop)
			markDead(t, w, "1.2.3.4")
			w.ProcessPacket(onTheTunWire(bareSyn(IPv4)))
			return sent()
		},
		"a ClientHello to a banned address": func(t *testing.T, devicePort uint16) [][]byte {
			set := silenceSet()
			w := newInjectingWorker(t, &set)
			intoTun(t, w, devicePort)
			w.bans = newTestIPBans(t, nil)
			banAddress(t, w.bans, "1.2.3.4", 443)
			sent := captureInjected(t, w)
			w.ProcessPacket(onTheTunWire(silentHelloFrom(tunWirePort)))
			return sent()
		},
		"a ClientHello a block set rejects": func(t *testing.T, devicePort uint16) [][]byte {
			w := newInjectingWorker(t, blockYTImages(config.BlockActionReject, false))
			intoTun(t, w, devicePort)
			sent := captureInjected(t, w)
			w.ProcessPacket(onTheTunWire(silentHelloFrom(tunWirePort)))
			return sent()
		},
		"resends of a ClientHello": func(t *testing.T, devicePort uint16) [][]byte {
			set := silenceSet()
			set.TCP.IPBlockDetect.RetransmitThreshold = 2
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)
			intoTun(t, r.w, devicePort)
			r.w.ProcessPacket(onTheTunWire(silentHelloFrom(tunWirePort)))
			r.w.ProcessPacket(onTheTunWire(silentHelloFrom(tunWirePort)))
			r.w.bans.wg.Wait()
			return r.sent()
		},
		"a ClientHello the server never answers": func(t *testing.T, devicePort uint16) [][]byte {
			set := silenceSet()
			r := newSilenceWorker(t, &set, iphealth.TLSSilent)
			intoTun(t, r.w, devicePort)
			r.w.ProcessPacket(onTheTunWire(silentHelloFrom(tunWirePort)))
			r.sweep(time.Now().Add(time.Minute))
			return r.sent()
		},
	}
	for site, send := range sites {
		for snat, devicePort := range tunSNATPorts {
			t.Run(site+"/"+snat, func(t *testing.T) {
				thisHost(t)
				want := []string{fmt.Sprintf("%s:%d", tunDevice, devicePort)}
				if got := serverResets(send(t, devicePort)); !slices.Equal(got, want) {
					t.Fatalf("the reset must reach the device's own socket, got resets to %v, want %v", got, want)
				}
			})
		}
	}
}
