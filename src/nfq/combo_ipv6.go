package nfq

import (
	"bytes"
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/utils"
)

func (w *Worker) sendComboFragmentsV6(cfg *config.SetConfig, packet []byte, dst net.IP) {
	pi, ok := ExtractPacketInfoV6(packet)
	if !ok || pi.PayloadLen < 20 {
		_ = w.sock.SendIPv6(packet, dst)
		return
	}

	combo := &cfg.Fragmentation.Combo

	if combo.DecoyEnabled && len(combo.DecoySNIs) > 0 {
		w.sendDecoyPacketV6(cfg, packet, pi, dst)
	}

	splits := GetComboSplitPoints(pi.Payload, pi.PayloadLen, combo, cfg.Fragmentation.MiddleSNI)
	splits = uniqueSorted(splits, pi.PayloadLen)
	if len(splits) < 1 {
		splits = []int{pi.PayloadLen / 2}
	}

	seqovlPattern := cfg.Fragmentation.SeqOverlapBytes
	seqovlLen := len(seqovlPattern)

	segments := make([]Segment, 0, len(splits)+1)
	prevEnd := 0

	for _, splitPos := range splits {
		if splitPos <= prevEnd {
			continue
		}
		seg := BuildSegmentV6(packet, pi, pi.Payload[prevEnd:splitPos], uint32(prevEnd))
		segments = append(segments, Segment{Data: seg, Seq: pi.Seq0 + uint32(prevEnd)})
		prevEnd = splitPos
	}

	if prevEnd < pi.PayloadLen {
		seg := BuildSegmentV6(packet, pi, pi.Payload[prevEnd:], uint32(prevEnd))
		segments = append(segments, Segment{Data: seg, Seq: pi.Seq0 + uint32(prevEnd)})
	}

	if len(segments) == 0 {
		_ = w.sock.SendIPv6(packet, dst)
		return
	}

	r := utils.NewRand()
	ShuffleSegments(segments, combo.ShuffleMode, r)
	SetMaxSeqPSH(segments, pi.IPHdrLen, sock.FixTCPChecksumV6)

	firstDelayMs := combo.FirstDelayMs
	if firstDelayMs <= 0 {
		firstDelayMs = 100
	}
	jitterMaxUs := combo.JitterMaxUs
	if jitterMaxUs <= 0 {
		jitterMaxUs = 2000
	}

	for i, seg := range segments {
		if i == 0 && seqovlLen > 0 {
			payloadLen := len(seg.Data) - pi.PayloadStart
			if seqovlLen <= payloadLen {
				seqOffset := seg.Seq - pi.Seq0
				fakeSeg := BuildFakeOverlapSegmentV6(packet, pi, payloadLen, seqOffset, seqovlPattern, cfg.Faking.TTL, true)
				if fakeSeg != nil {
					_ = w.sock.SendIPv6(fakeSeg, dst)
					time.Sleep(50 * time.Microsecond)
				}
			}
		}

		_ = w.sock.SendIPv6(seg.Data, dst)

		if i == 0 {
			jitter := r.Intn(firstDelayMs/3 + 1)
			time.Sleep(time.Duration(firstDelayMs+jitter) * time.Millisecond)
		} else if i < len(segments)-1 {
			time.Sleep(time.Duration(r.Intn(jitterMaxUs)) * time.Microsecond)
		}
	}
}

func (w *Worker) sendDecoyPacketV6(cfg *config.SetConfig, packet []byte, pi PacketInfo, dst net.IP) {
	sniStart, sniEnd, ok := locateSNI(pi.Payload)
	if !ok || sniEnd <= sniStart {
		return
	}

	decoySNIs := cfg.Fragmentation.Combo.DecoySNIs

	sniLen := sniEnd - sniStart
	fakeSNI := []byte(decoySNIs[pi.Seq0%uint32(len(decoySNIs))])

	if len(fakeSNI) < sniLen {
		fakeSNI = append(fakeSNI, bytes.Repeat([]byte{'.'}, sniLen-len(fakeSNI))...)
	} else if len(fakeSNI) > sniLen {
		fakeSNI = fakeSNI[:sniLen]
	}

	decoyPkt := make([]byte, len(packet))
	copy(decoyPkt, packet)

	copy(decoyPkt[pi.PayloadStart+sniStart:pi.PayloadStart+sniEnd], fakeSNI)

	hopLimit := cfg.Faking.TTL
	if hopLimit == 0 {
		hopLimit = 3
	}
	decoyPkt[7] = hopLimit

	sock.FixTCPChecksumV6(decoyPkt)

	_ = w.sock.SendIPv6(decoyPkt, dst)

	if cfg.TCP.Seg2Delay > 0 {
		time.Sleep(time.Duration(cfg.TCP.Seg2Delay) * time.Millisecond)
	}
}
