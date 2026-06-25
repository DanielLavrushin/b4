package nfq

import (
	"encoding/binary"
	"math/rand"
	"net"
	"sort"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

func ExtractPacketInfoV4(packet []byte) (PacketInfo, bool) {
	if len(packet) < 40 {
		return PacketInfo{}, false
	}
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	if len(packet) < ipHdrLen+20 {
		return PacketInfo{}, false
	}
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	payloadStart := ipHdrLen + tcpHdrLen
	if payloadStart > len(packet) {
		return PacketInfo{}, false
	}
	payloadLen := len(packet) - payloadStart

	return PacketInfo{
		IPHdrLen:     ipHdrLen,
		TCPHdrLen:    tcpHdrLen,
		PayloadStart: payloadStart,
		PayloadLen:   payloadLen,
		Payload:      packet[payloadStart:],
		Seq0:         binary.BigEndian.Uint32(packet[ipHdrLen+4 : ipHdrLen+8]),
		ID0:          binary.BigEndian.Uint16(packet[4:6]),
		IsIPv6:       false,
	}, true
}

func BuildSegmentV4(packet []byte, pi PacketInfo, payloadSlice []byte, seqOffset uint32, idOffset uint16) []byte {
	segLen := pi.PayloadStart + len(payloadSlice)
	seg := make([]byte, segLen)
	copy(seg[:pi.PayloadStart], packet[:pi.PayloadStart])
	copy(seg[pi.PayloadStart:], payloadSlice)

	binary.BigEndian.PutUint32(seg[pi.IPHdrLen+4:pi.IPHdrLen+8], pi.Seq0+seqOffset)
	binary.BigEndian.PutUint16(seg[4:6], pi.ID0+idOffset)
	binary.BigEndian.PutUint16(seg[2:4], uint16(segLen))

	sock.FixIPv4Checksum(seg[:pi.IPHdrLen])
	sock.FixTCPChecksum(seg)
	return seg
}

func ShuffleSegments(segments []Segment, mode string, r *rand.Rand) {
	switch mode {
	case "full":
		for i := len(segments) - 1; i > 0; i-- {
			j := r.Intn(i + 1)
			segments[i], segments[j] = segments[j], segments[i]
		}
	case "reverse":
		for i, j := 0, len(segments)-1; i < j; i, j = i+1, j-1 {
			segments[i], segments[j] = segments[j], segments[i]
		}
	case "middle":
		if len(segments) > 3 {
			middle := segments[1 : len(segments)-1]
			for i := len(middle) - 1; i > 0; i-- {
				j := r.Intn(i + 1)
				middle[i], middle[j] = middle[j], middle[i]
			}
		} else if len(segments) > 1 {
			for i, j := 0, len(segments)-1; i < j; i, j = i+1, j-1 {
				segments[i], segments[j] = segments[j], segments[i]
			}
		}
	}
}

func (w *Worker) SendSegmentsV4(segs [][]byte, dst net.IP, cfg *config.SetConfig) {
	delay := config.ResolveSeg2Delay(cfg.TCP.Seg2Delay, cfg.TCP.Seg2DelayMax)
	if cfg.Fragmentation.ReverseOrder {
		for i := len(segs) - 1; i >= 0; i-- {
			_ = w.sock.SendIPv4(segs[i], dst)
			if i > 0 && delay > 0 {
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
		}
	} else {
		for i, seg := range segs {
			_ = w.sock.SendIPv4(seg, dst)
			if i < len(segs)-1 && delay > 0 {
				time.Sleep(time.Duration(delay) * time.Millisecond)
			}
		}
	}
}

func SetPSH(seg []byte, ipHdrLen int) {
	seg[ipHdrLen+13] |= 0x08
}

func ClearPSH(seg []byte, ipHdrLen int) {
	seg[ipHdrLen+13] &^= 0x08
}

func GetSNISplitPoints(payload []byte, payloadLen int, middleSNI bool, sniPosition int) []int {
	var splits []int

	if middleSNI {
		if sniStart, sniEnd, ok := locateSNI(payload); ok && sniEnd > sniStart {
			sniLen := sniEnd - sniStart
			splits = append(splits, sniStart)
			if sniLen > 6 {
				splits = append(splits, sniStart+sniLen/2)
			}
			splits = append(splits, sniEnd)
		}
	}

	if len(splits) == 0 && sniPosition > 0 && sniPosition < payloadLen {
		splits = append(splits, sniPosition)
	}

	return splits
}

func (w *Worker) SendTwoSegmentsV4(seg1, seg2 []byte, dst net.IP, delay int, reverse bool) {
	if reverse {
		log.Tracef("Sending two segments in reverse order to %s", dst.String())
		_ = w.sock.SendIPv4(seg2, dst)
		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv4(seg1, dst)
	} else {
		log.Tracef("Sending two segments to %s", dst.String())
		_ = w.sock.SendIPv4(seg1, dst)
		if delay > 0 {
			time.Sleep(time.Duration(delay) * time.Millisecond)
		}
		_ = w.sock.SendIPv4(seg2, dst)
	}
}

func uniqueSorted(splits []int, maxVal int) []int {
	seen := make(map[int]bool)
	result := make([]int, 0, len(splits))
	for _, s := range splits {
		if s > 0 && s < maxVal && !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	sort.Ints(result)
	return result
}

func locateSNI(payload []byte) (start, end int, ok bool) {
	if len(payload) < 5 || payload[0] != TLSHandshakeType {
		return 0, 0, false
	}

	p := 5

	if p+4 > len(payload) || payload[p] != TLSClientHello {
		return 0, 0, false
	}

	p += 4

	if p+2+32 > len(payload) {
		return 0, 0, false
	}
	p += 2 + 32

	if p >= len(payload) {
		return 0, 0, false
	}
	sidLen := int(payload[p])
	p++
	if p+sidLen > len(payload) {
		return 0, 0, false
	}
	p += sidLen

	if p+2 > len(payload) {
		return 0, 0, false
	}
	csLen := int(binary.BigEndian.Uint16(payload[p : p+2]))
	p += 2
	if p+csLen > len(payload) {
		return 0, 0, false
	}
	p += csLen

	if p >= len(payload) {
		return 0, 0, false
	}
	cmLen := int(payload[p])
	p++
	if p+cmLen > len(payload) {
		return 0, 0, false
	}
	p += cmLen

	if p+2 > len(payload) {
		return 0, 0, false
	}
	extLen := int(binary.BigEndian.Uint16(payload[p : p+2]))
	p += 2
	if p+extLen > len(payload) {
		extLen = len(payload) - p
	}
	e := p
	ee := p + extLen

	for e+4 <= ee {
		extType := binary.BigEndian.Uint16(payload[e : e+2])
		extDataLen := int(binary.BigEndian.Uint16(payload[e+2 : e+4]))
		e += 4
		if e+extDataLen > ee {
			break
		}

		if extType == 0 && extDataLen >= 5 {
			q := e
			if q+2 > e+extDataLen {
				break
			}
			listLen := int(binary.BigEndian.Uint16(payload[q : q+2]))
			q += 2
			if q+listLen > e+extDataLen {
				break
			}
			if q+3 > e+extDataLen {
				break
			}
			nameType := payload[q]
			q++
			if nameType != 0 {
				break
			}
			nameLen := int(binary.BigEndian.Uint16(payload[q : q+2]))
			q += 2
			if nameLen == 0 || q+nameLen > e+extDataLen {
				break
			}
			return q, q + nameLen, true
		}

		e += extDataLen
	}
	return 0, 0, false
}

func SetMaxSeqPSH(segments []Segment, ipHdrLen int, fixChecksum func([]byte)) {
	maxSeqIdx := 0
	for i := range segments {
		ClearPSH(segments[i].Data, ipHdrLen)
		fixChecksum(segments[i].Data)
		if segments[i].Seq > segments[maxSeqIdx].Seq {
			maxSeqIdx = i
		}
	}
	SetPSH(segments[maxSeqIdx].Data, ipHdrLen)
	fixChecksum(segments[maxSeqIdx].Data)
}

func GetComboSplitPoints(payload []byte, payloadLen int, combo *config.ComboFragConfig, middleSNI bool) []int {
	splits := []int{}

	if combo.FirstByteSplit {
		splits = append(splits, 1)
	}

	if combo.ExtensionSplit {
		if extSplit := findPreSNIExtensionPoint(payload); extSplit > 1 && extSplit < payloadLen-5 {
			splits = append(splits, extSplit)
		}
	}

	if middleSNI {
		if sniStart, sniEnd, ok := locateSNI(payload); ok && sniEnd > sniStart {
			sniLen := sniEnd - sniStart
			if sniStart > 2 {
				splits = append(splits, sniStart-1)
			}
			splits = append(splits, sniStart+sniLen/2)
			if sniLen > 15 {
				splits = append(splits, sniStart+sniLen*3/4)
			}
		}
	}

	return splits
}

func GetDisorderJitter(disorder *config.DisorderFragConfig) (minJitter, maxJitter int) {
	minJitter = disorder.MinJitterUs
	maxJitter = disorder.MaxJitterUs
	if minJitter <= 0 {
		minJitter = 1000
	}
	if maxJitter <= minJitter {
		maxJitter = minJitter + 2000
	}
	return
}

func BuildValidSplits(splits []int, payloadLen int) []int {
	validSplits := []int{0}
	for _, s := range splits {
		if s > 0 && s < payloadLen {
			validSplits = append(validSplits, s)
		}
	}
	validSplits = append(validSplits, payloadLen)
	return validSplits
}

func BuildSeqOverlapSegmentV4(packet []byte, pi PacketInfo, origPayload []byte, origStartOffset int, seqovlLen int, pattern []byte, idOffset uint16) []byte {
	if seqovlLen <= 0 || len(pattern) == 0 {
		return BuildSegmentV4(packet, pi, origPayload, uint32(origStartOffset), idOffset)
	}
	ext := make([]byte, seqovlLen+len(origPayload))
	for i := 0; i < seqovlLen; i++ {
		ext[i] = pattern[i%len(pattern)]
	}
	copy(ext[seqovlLen:], origPayload)
	seqOffset := uint32(origStartOffset) - uint32(seqovlLen)
	return BuildSegmentV4(packet, pi, ext, seqOffset, idOffset)
}

func BuildFakeOverlapSegmentV4(packet []byte, pi PacketInfo, payloadLen int, seqOffset uint32, idOffset uint16, fakePattern []byte, fakeTTL uint8) []byte {
	if payloadLen <= 0 {
		return nil
	}

	segLen := pi.PayloadStart + payloadLen
	seg := make([]byte, segLen)
	copy(seg[:pi.PayloadStart], packet[:pi.PayloadStart])

	patLen := len(fakePattern)
	if patLen == 0 {
		for i := 0; i < payloadLen; i++ {
			seg[pi.PayloadStart+i] = byte((i * 7) & 0xFF)
		}
	} else {
		for i := 0; i < payloadLen; i++ {
			seg[pi.PayloadStart+i] = fakePattern[i%patLen]
		}
	}

	binary.BigEndian.PutUint32(seg[pi.IPHdrLen+4:pi.IPHdrLen+8], pi.Seq0+seqOffset)
	binary.BigEndian.PutUint16(seg[4:6], pi.ID0+idOffset)
	binary.BigEndian.PutUint16(seg[2:4], uint16(segLen))

	seg[8] = dynamicTTL(packet, false, fakeTTL)

	seg[pi.IPHdrLen+13] &^= 0x08

	sock.FixIPv4Checksum(seg[:pi.IPHdrLen])
	sock.FixTCPChecksum(seg)
	corruptTCPChecksum(seg, pi.IPHdrLen)

	return seg
}
