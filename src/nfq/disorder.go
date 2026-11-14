package nfq

import (
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/utils"
)

func (w *Worker) sendDisorderFragments(cfg *config.SetConfig, packet []byte, dst net.IP) {
	disorder := &cfg.Fragmentation.Disorder
	pi, ok := ExtractPacketInfoV4(packet)
	if !ok || pi.PayloadLen < 10 {
		_ = w.sock.SendIPv4(packet, dst)
		return
	}

	validSplits := BuildValidSplits(disorderSplitPoints(&cfg.Fragmentation, pi.Payload, pi.PayloadLen), pi.PayloadLen)

	seqovlPattern := cfg.Fragmentation.SeqOverlapBytes
	seqovlLen := len(seqovlPattern)

	segments := make([]Segment, 0, len(validSplits)-1)
	for i := 0; i < len(validSplits)-1; i++ {
		start, end := validSplits[i], validSplits[i+1]
		realPayload := pi.Payload[start:end]

		seg := BuildSegmentV4(packet, pi, realPayload, uint32(start), uint16(i))
		if i < len(validSplits)-2 {
			ClearPSH(seg, pi.IPHdrLen)
			sock.FixTCPChecksum(seg)
		}
		segments = append(segments, Segment{Data: seg, Seq: pi.Seq0 + uint32(start)})
	}

	if seqovlLength := cfg.Fragmentation.SeqOverlapLength; seqovlLength > 0 && seqovlLen > 0 && len(segments) >= 2 {
		second := segments[1]
		startOff := int(second.Seq - pi.Seq0)
		if seqovlLength < startOff {
			payloadLen := len(second.Data) - pi.PayloadStart
			if startOff+payloadLen <= pi.PayloadLen {
				origPayload := pi.Payload[startOff : startOff+payloadLen]
				newData := BuildSeqOverlapSegmentV4(packet, pi, origPayload, startOff, seqovlLength, seqovlPattern, 1)
				if len(segments) > 2 {
					ClearPSH(newData, pi.IPHdrLen)
					sock.FixTCPChecksum(newData)
				}
				segments[1] = Segment{
					Data: newData,
					Seq:  pi.Seq0 + uint32(startOff) - uint32(seqovlLength),
				}
			}
		}
	}

	r := utils.NewRand()
	ShuffleSegments(segments, cfg.Fragmentation.Disorder.ShuffleMode, r)
	SetMaxSeqPSH(segments, pi.IPHdrLen, sock.FixTCPChecksum)

	minJitter, maxJitter := GetDisorderJitter(disorder)

	fakePerSeg := disorder.FakePerSegment
	fakePerSegCount := config.ResolveRange(disorder.FakePerSegCount, disorder.FakePerSegCountMax)
	if fakePerSegCount <= 0 {
		fakePerSegCount = 1
	} else if fakePerSegCount > 11 {
		fakePerSegCount = 11
	}

	seg2d := config.ResolveSeg2Delay(cfg.TCP.Seg2Delay, cfg.TCP.Seg2DelayMax)
	for i, seg := range segments {
		sendFake := (fakePerSeg || i == 0) && seqovlLen > 0
		if sendFake {
			payloadLen := len(seg.Data) - pi.PayloadStart
			if seqovlLen <= payloadLen {
				seqOffset := seg.Seq - pi.Seq0
				for f := 0; f < fakePerSegCount; f++ {
					fakeSeg := BuildFakeOverlapSegmentV4(packet, pi, payloadLen, seqOffset, 0, seqovlPattern, cfg.Faking.TTL)
					if fakeSeg != nil {
						_ = w.sock.SendIPv4(fakeSeg, dst)
						time.Sleep(50 * time.Microsecond)
					}
				}
			}
		}

		_ = w.sock.SendIPv4(seg.Data, dst)
		if i < len(segments)-1 {
			if seg2d > 0 {
				jitter := r.Intn(seg2d/2 + 1)
				time.Sleep(time.Duration(seg2d+jitter) * time.Millisecond)
			} else {
				jitter := minJitter + r.Intn(maxJitter-minJitter+1)
				time.Sleep(time.Duration(jitter) * time.Microsecond)
			}
		}
	}
}
