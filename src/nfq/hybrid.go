package nfq

import (
	"net"

	"github.com/daniellavrushin/b4/config"
)

func (w *Worker) sendHybridFragments(cfg *config.SetConfig, packet []byte, dst net.IP, fakes bool) {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen
	payloadLen := len(packet) - payloadStart

	if payloadLen < 10 {
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	payload := packet[payloadStart:]

	extSplit := findPreSNIExtensionPoint(payload)
	sniStart, sniEnd, hasSNI := locateSNI(payload)

	if extSplit > 5 && hasSNI && sniEnd-sniStart > 6 {
		w.sendComboFragments(cfg, packet, dst, fakes)
	} else if hasSNI && sniEnd-sniStart > 6 {
		w.sendDisorderFragments(cfg, packet, dst)
	} else if extSplit > 5 {
		w.sendExtSplitFragments(cfg, packet, dst)
	} else {
		w.sendFirstByteDesync(cfg, packet, dst)
	}
}
