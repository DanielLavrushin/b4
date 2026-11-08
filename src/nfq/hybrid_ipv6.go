package nfq

import (
	"net"

	"github.com/daniellavrushin/b4/config"
)

func (w *Worker) sendHybridFragmentsV6(cfg *config.SetConfig, packet []byte, dst net.IP, fakes bool) {
	const ipv6HdrLen = 40

	if len(packet) < ipv6HdrLen+20 {
		_ = w.sock.SendIPv6(packet, dst)
		return
	}

	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	payloadStart := ipv6HdrLen + tcpHdrLen
	payloadLen := len(packet) - payloadStart

	if payloadLen < 10 {
		_ = w.sock.SendIPv6(packet, dst)
		return
	}

	payload := packet[payloadStart:]

	extSplit := findPreSNIExtensionPoint(payload)
	sniStart, sniEnd, hasSNI := locateSNI(payload)

	if extSplit > 5 && hasSNI && sniEnd-sniStart > 6 {
		w.sendComboFragmentsV6(cfg, packet, dst, fakes)
	} else if hasSNI && sniEnd-sniStart > 6 {
		w.sendDisorderFragmentsV6(cfg, packet, dst)
	} else if extSplit > 5 {
		w.sendExtSplitFragmentsV6(cfg, packet, dst)
	} else {
		w.sendFirstByteDesyncV6(cfg, packet, dst)
	}
}
