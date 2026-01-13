package nfq

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/utils"
)

func (w *Worker) sendComboFragments(cfg *config.SetConfig, packet []byte, dst net.IP) {
	pi, ok := ExtractPacketInfoV4(packet)
	if !ok || pi.PayloadLen < 20 {
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	combo := &cfg.Fragmentation.Combo

	if combo.DecoyEnabled {
		w.sendDecoyPacket(cfg, packet, pi, dst)
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

	for idx, splitPos := range splits {
		if splitPos <= prevEnd {
			continue
		}
		seg := BuildSegmentV4(packet, pi, pi.Payload[prevEnd:splitPos], uint32(prevEnd), uint16(idx))
		segments = append(segments, Segment{Data: seg, Seq: pi.Seq0 + uint32(prevEnd)})
		prevEnd = splitPos
	}

	if prevEnd < pi.PayloadLen {
		seg := BuildSegmentV4(packet, pi, pi.Payload[prevEnd:], uint32(prevEnd), uint16(len(segments)))
		segments = append(segments, Segment{Data: seg, Seq: pi.Seq0 + uint32(prevEnd)})
	}

	if len(segments) == 0 {
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	r := utils.NewRand()
	ShuffleSegments(segments, combo.ShuffleMode, r)
	SetMaxSeqPSH(segments, pi.IPHdrLen, sock.FixTCPChecksum)

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
				fakeSeg := BuildFakeOverlapSegmentV4(packet, pi, payloadLen, seqOffset, 0, seqovlPattern, cfg.Faking.TTL, true)
				if fakeSeg != nil {
					_ = w.sock.SendIPv4(fakeSeg, dst)
					time.Sleep(50 * time.Microsecond)
				}
			}
		}

		_ = w.sock.SendIPv4(seg.Data, dst)

		if i == 0 {
			jitter := r.Intn(firstDelayMs/3 + 1)
			time.Sleep(time.Duration(firstDelayMs+jitter) * time.Millisecond)
		} else if i < len(segments)-1 {
			time.Sleep(time.Duration(r.Intn(jitterMaxUs)) * time.Microsecond)
		}
	}
}

func (w *Worker) sendDecoyPacket(cfg *config.SetConfig, packet []byte, pi PacketInfo, dst net.IP) {

	log.Tracef("sendDecoyPacket: Sending decoy fragment packet to %s, set: %s", dst.String(), cfg.Name)
	fakeBlob := sock.GetPayload(&cfg.Faking)

	if len(fakeBlob) < 3 {
		log.Warnf("Not enough fake payload for fragmentation, need at least 3 bytes")
		return
	}

	if len(fakeBlob) > 680 {
		fakeBlob = fakeBlob[:680]
	}

	// Build fake packet with this blob as payload
	fakePacket := make([]byte, pi.PayloadStart+len(fakeBlob))
	copy(fakePacket[:pi.PayloadStart], packet[:pi.PayloadStart])
	copy(fakePacket[pi.PayloadStart:], fakeBlob)

	// Update IP length
	binary.BigEndian.PutUint16(fakePacket[2:4], uint16(len(fakePacket)))

	// Set low TTL so it won't reach server
	ttl := cfg.Faking.TTL
	if ttl == 0 {
		ttl = 3
	}
	fakePacket[8] = ttl

	sock.FixIPv4Checksum(fakePacket[:pi.IPHdrLen])
	sock.FixTCPChecksum(fakePacket)

	// Split at position 2 (like zapret2)
	splitPos := 2

	// Segment 1: first 2 bytes, WITH MD5
	seg1 := BuildSegmentV4(fakePacket, pi, fakeBlob[:splitPos], 0, 0)
	ClearPSH(seg1, pi.IPHdrLen)
	sock.FixTCPChecksum(seg1)

	// Segment 2: rest of fake blob, WITH MD5
	seg2 := BuildSegmentV4(fakePacket, pi, fakeBlob[splitPos:], uint32(splitPos), 1)

	_ = w.sock.SendIPv4(seg1, dst)
	time.Sleep(50 * time.Microsecond)
	_ = w.sock.SendIPv4(seg2, dst)

	if cfg.TCP.Seg2Delay > 0 {
		time.Sleep(time.Duration(cfg.TCP.Seg2Delay) * time.Millisecond)
	}
}
