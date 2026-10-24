package nfq

import (
	"bytes"
	"fmt"
	"net"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/engine"
)

func TestATUNDecisionDropLeavesTheHeaderTheTUNReadIntact(t *testing.T) {
	thisHost(t)
	for name, set := range map[string]config.SetConfig{
		"udp.mode drop":   newDomainOnlyQUICSet(config.UDPModeDrop),
		"udp.mode reject": newDomainOnlyQUICSet(config.UDPModeReject),
		"block, drop":     blockingQUICSet(config.BlockActionDrop),
		"block, reject":   blockingQUICSet(config.BlockActionReject),
	} {
		t.Run(name, func(t *testing.T) {
			w := newQUICFlowWorker(t, &set)
			captureInjected(t, w)
			w.srcResolver = newResolverWith(t, tunTestWAN,
				"ipv4 2 udp 17 29 src=192.168.1.100 dst=1.2.3.4 sport=50601 dport=443 src=1.2.3.4 dst="+tunTestWAN+" sport=443 dport=50601 use=1\n")
			hello := buildQUICInitialWithSNI(t, []byte{0xd2, 1, 2, 3, 4, 5, 6, 7}, "www.blocked.example")
			raw := makeV4UDPPacket(hello, net.ParseIP(tunTestWAN), quicServer, 50601, 443)
			read := bytes.Clone(raw)

			if v := w.ProcessPacketDir(raw, engine.DirUnknown); v != engine.VerdictDropFlow {
				t.Fatalf("verdict %v, want VerdictDropFlow", v)
			}
			if !bytes.Equal(raw, read) {
				t.Fatal("ProcessPacketDir changed the packet the TUN read, whose post-SNAT header the TUN turns into the conntrack reply tuple")
			}
			lanKey := fmt.Sprintf(connKeyFormat, "192.168.1.100", 50601, quicServer.String(), 443)
			if host, _ := w.quicFlows.Lookup(lanKey, hello); host != "www.blocked.example" {
				t.Fatalf("b4 must have decided the flow as the LAN client 192.168.1.100, got %q for that key", host)
			}
		})
	}
}
