package nfq

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
)

const tunDNSWirePort = 45001

func tunDNSWorker(t *testing.T, set *config.SetConfig, devicePort uint16) *Worker {
	t.Helper()
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{set}
	cfg.System.DNS.QueryTimeoutSec = 1
	t.Cleanup(stopDNSRouteCleanup)
	w := newTestWorker(t, &cfg)
	w.srcResolver = newResolverWith(t, tunTestWAN, fmt.Sprintf(
		"ipv4 2 udp 17 29 src=%s dst=8.8.8.8 sport=%d dport=53 [UNREPLIED] src=8.8.8.8 dst=%s sport=53 dport=%d mark=0 use=1\n",
		tunDevice, devicePort, tunTestWAN, tunDNSWirePort))
	return w
}

func tunDNSQuery(txid uint16) []byte {
	return makeV4UDPPacket(dns.BuildQuery("i.ytimg.com", txid, 1), net.ParseIP(tunTestWAN), net.ParseIP("8.8.8.8"), tunDNSWirePort, 53)
}

func TestATunDNSAnswerGoesToTheDevicesOwnPort(t *testing.T) {
	sets := map[string]func() config.SetConfig{
		"a block set's answer": func() config.SetConfig {
			set := newTunDNSSet()
			set.Routing.Enabled = true
			set.Routing.Mode = config.RoutingModeBlock
			set.Routing.BlockAction = config.BlockActionReject
			return set
		},
		"a pinned answer": func() config.SetConfig {
			set := newTunDNSSet()
			set.DNS.Pins = map[string][]string{"i.ytimg.com": {"1.2.3.4"}}
			return set
		},
		"a redirect's answer": func() config.SetConfig {
			set := newTunDNSSet()
			set.DNS.Enabled = true
			set.DNS.TargetDNS = "127.0.0.1"
			set.DNS.Strict = true
			return set
		},
	}
	for name, mk := range sets {
		for snat, devicePort := range map[string]uint16{"port kept by SNAT": tunDNSWirePort, "port remapped by SNAT": 49874} {
			t.Run(name+"/"+snat, func(t *testing.T) {
				thisHost(t)
				set := mk()
				w := tunDNSWorker(t, &set, devicePort)
				sent := captureSends(t)

				w.ProcessPacket(tunDNSQuery(0x3131))
				w.wg.Wait()

				answer := sent.one(t)
				to := fmt.Sprintf("%s:%d", net.IP(answer[16:20]), binary.BigEndian.Uint16(answer[22:24]))
				if want := fmt.Sprintf("%s:%d", tunDevice, devicePort); to != want {
					t.Fatalf("the answer must reach the device's own socket, got it sent to %s, want %s", to, want)
				}
			})
		}
	}
}

func TestATunDNSExchangeLogsTheDevicesOwnPort(t *testing.T) {
	for snat, devicePort := range map[string]uint16{"port kept by SNAT": tunDNSWirePort, "port remapped by SNAT": 49874} {
		t.Run(snat, func(t *testing.T) {
			thisHost(t)
			lines := connectionLines(t)
			set := newTunDNSSet()
			set.DNS.Enabled = true
			set.DNS.TargetDNS = "127.0.0.1"
			set.DNS.Strict = true
			w := tunDNSWorker(t, &set, devicePort)
			captureSends(t)

			w.ProcessPacket(tunDNSQuery(0x4141))
			w.wg.Wait()

			logged := drainLines(lines)
			if len(logged) < 2 {
				t.Fatalf("want the interception and the SERVFAIL logged, got %v", logged)
			}
			want := fmt.Sprintf("%s:%d", tunDevice, devicePort)
			for _, line := range logged {
				if f := strings.Split(line, ","); len(f) < 5 || f[4] != want {
					t.Errorf("every line of the exchange must name the device's own socket %s, got %q", want, line)
				}
			}
		})
	}
}

func TestATunDNSQueryIsKeyedOnTheDevicesOwnPort(t *testing.T) {
	for snat, devicePort := range map[string]uint16{"port kept by SNAT": tunDNSWirePort, "port remapped by SNAT": 49874} {
		t.Run(snat, func(t *testing.T) {
			thisHost(t)
			withDNSNames(t, true)
			set := newTunDNSSet()
			set.Routing.Enabled = true
			w := tunDNSWorker(t, &set, devicePort)

			const txid = 0x3232
			w.ProcessPacket(tunDNSQuery(txid))

			reply := dnsRouteKeyResponse(IPv4, net.ParseIP(tunDevice), devicePort, net.ParseIP("8.8.8.8"), 53, txid, "i.ytimg.com")
			if _, ok := consumeDNSPendingRoute(reply); !ok {
				t.Error("the answer comes back to the device's own port, and its pending route must be found under it")
			}
			if !consumeDNSNameQuery(reply) {
				t.Error("the answer comes back to the device's own port, and its name query must be found under it")
			}
		})
	}
}
