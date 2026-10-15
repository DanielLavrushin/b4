package nfq

import (
	"encoding/binary"
	"net"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

func (w *Worker) sendFakeSyn(set *config.SetConfig, raw []byte, ipHdrLen, tcpHdrLen int) {
	fakePayload := sock.GetPayload(&set.Faking)

	fakePayloadLen := 0
	if set.TCP.SynFakeLen > 0 {
		fakePayloadLen = set.TCP.SynFakeLen
		if fakePayloadLen > len(fakePayload) {
			fakePayloadLen = len(fakePayload)
		}
	}
	totalLen := ipHdrLen + tcpHdrLen + fakePayloadLen
	fakePkt := make([]byte, totalLen)

	copy(fakePkt[:ipHdrLen+tcpHdrLen], raw[:ipHdrLen+tcpHdrLen])
	copy(fakePkt[ipHdrLen+tcpHdrLen:], fakePayload[:fakePayloadLen])

	binary.BigEndian.PutUint16(fakePkt[2:4], uint16(totalLen))

	configTTL := set.TCP.SynTTL
	if configTTL == 0 {
		configTTL = set.Faking.TTL
	}
	fakePkt[8] = dynamicTTL(raw, false, configTTL)

	// Apply sequence modification based on strategy
	switch set.Faking.Strategy {
	case "randseq":
		seq := binary.BigEndian.Uint32(fakePkt[ipHdrLen+4 : ipHdrLen+8])
		seq += uint32(set.Faking.SeqOffset)
		if set.Faking.SeqOffset == 0 {
			seq += 100000
		}
		binary.BigEndian.PutUint32(fakePkt[ipHdrLen+4:ipHdrLen+8], seq)

	case "pastseq":
		seq := binary.BigEndian.Uint32(fakePkt[ipHdrLen+4 : ipHdrLen+8])
		offset := uint32(set.Faking.SeqOffset)
		if offset == 0 {
			offset = 10000
		}
		if seq > offset {
			seq -= offset
		}
		binary.BigEndian.PutUint32(fakePkt[ipHdrLen+4:ipHdrLen+8], seq)

	case "timestamp":
		decrease := set.Faking.TimestampDecrease
		if decrease == 0 {
			decrease = 600000 // Default value matching youtubeUnblock
		}
		sock.DecreaseTCPTimestamp(fakePkt, decrease, false)
	}

	sock.FixIPv4Checksum(fakePkt[:ipHdrLen])
	sock.FixTCPChecksum(fakePkt)
	corruptTCPChecksum(fakePkt, ipHdrLen)

	dst := net.IP(fakePkt[16:20])
	_ = w.sock.SendIPv4(fakePkt, dst)
}

func (w *Worker) sendFakeSynV6(set *config.SetConfig, raw []byte, ipHdrLen, tcpHdrLen int) {
	if !hasPlainIPv6Header(raw, ipProtoTCP) {
		log.Tracef("Fake SYN v6: skipping set %s, the TCP header is not at the fixed IPv6 offset", set.Name)
		return
	}

	fakePayload := sock.GetPayload(&set.Faking)

	fakePayloadLen := 0
	if set.TCP.SynFakeLen > 0 {
		fakePayloadLen = set.TCP.SynFakeLen
		if fakePayloadLen > len(fakePayload) {
			fakePayloadLen = len(fakePayload)
		}
	}

	totalLen := ipHdrLen + tcpHdrLen + fakePayloadLen
	fakePkt := make([]byte, totalLen)

	copy(fakePkt[:ipHdrLen+tcpHdrLen], raw[:ipHdrLen+tcpHdrLen])
	copy(fakePkt[ipHdrLen+tcpHdrLen:], fakePayload[:fakePayloadLen])

	payloadLen := tcpHdrLen + fakePayloadLen
	binary.BigEndian.PutUint16(fakePkt[4:6], uint16(payloadLen))

	configTTL := set.TCP.SynTTL
	if configTTL == 0 {
		configTTL = set.Faking.TTL
	}
	fakePkt[7] = dynamicTTL(raw, true, configTTL)

	switch set.Faking.Strategy {
	case "randseq":
		seq := binary.BigEndian.Uint32(fakePkt[ipHdrLen+4 : ipHdrLen+8])
		seq += uint32(set.Faking.SeqOffset)
		if set.Faking.SeqOffset == 0 {
			seq += 100000
		}
		binary.BigEndian.PutUint32(fakePkt[ipHdrLen+4:ipHdrLen+8], seq)

	case "pastseq":
		seq := binary.BigEndian.Uint32(fakePkt[ipHdrLen+4 : ipHdrLen+8])
		offset := uint32(set.Faking.SeqOffset)
		if offset == 0 {
			offset = 10000
		}
		if seq > offset {
			seq -= offset
		}
		binary.BigEndian.PutUint32(fakePkt[ipHdrLen+4:ipHdrLen+8], seq)

	case "timestamp":
		decrease := set.Faking.TimestampDecrease
		if decrease == 0 {
			decrease = 600000 // Default value matching youtubeUnblock
		}
		sock.DecreaseTCPTimestamp(fakePkt, decrease, true)
	}

	sock.FixTCPChecksumV6(fakePkt)
	corruptTCPChecksum(fakePkt, ipHdrLen)

	dst := net.IP(fakePkt[24:40])
	_ = w.sock.SendIPv6(fakePkt, dst)
}

func (w *Worker) sendFakeSynWithMD5(set *config.SetConfig, raw []byte, ihl int, dst net.IP) {
	fakeSyn := make([]byte, len(raw))
	copy(fakeSyn, raw)

	fakeSyn[8] = dynamicTTL(raw, false, set.Faking.TTL)

	// Modify seq so server ignores if it arrives
	seq := binary.BigEndian.Uint32(fakeSyn[ihl+4 : ihl+8])
	binary.BigEndian.PutUint32(fakeSyn[ihl+4:ihl+8], seq-10000)

	sock.FixIPv4Checksum(fakeSyn[:ihl])
	sock.FixTCPChecksum(fakeSyn)

	signed, ok := sock.AddTCPMD5Option(fakeSyn, false)
	if !ok {
		signed, ok = sock.AddTCPMD5Option(withoutTCPOptions(fakeSyn, ihl), false)
	}
	if !ok {
		return
	}

	_ = w.sock.SendIPv4(signed, dst)
}

func (w *Worker) sendFakeSynWithMD5V6(set *config.SetConfig, raw []byte, dst net.IP) {
	const ipv6HdrLen = 40

	if !hasPlainIPv6Header(raw, ipProtoTCP) {
		log.Tracef("Fake MD5 SYN v6: skipping %s, the TCP header is not at the fixed IPv6 offset", dst.String())
		return
	}

	fakeSyn := make([]byte, len(raw))
	copy(fakeSyn, raw)

	fakeSyn[7] = dynamicTTL(raw, true, set.Faking.TTL)

	// Modify seq so server ignores if it arrives
	seq := binary.BigEndian.Uint32(fakeSyn[ipv6HdrLen+4 : ipv6HdrLen+8])
	binary.BigEndian.PutUint32(fakeSyn[ipv6HdrLen+4:ipv6HdrLen+8], seq-10000)

	signed, ok := sock.AddTCPMD5Option(fakeSyn, true)
	if !ok {
		signed, ok = sock.AddTCPMD5Option(withoutTCPOptions(fakeSyn, ipv6HdrLen), true)
	}
	if !ok {
		return
	}

	_ = w.sock.SendIPv6(signed, dst)
}

func withoutTCPOptions(pkt []byte, l3Len int) []byte {
	if len(pkt) < l3Len+20 {
		return pkt
	}
	tcpHL := int(pkt[l3Len+12]>>4) * 4
	if tcpHL < 20 || len(pkt) < l3Len+tcpHL {
		return pkt
	}
	out := make([]byte, len(pkt)-tcpHL+20)
	copy(out, pkt[:l3Len+20])
	copy(out[l3Len+20:], pkt[l3Len+tcpHL:])
	out[l3Len+12] = 5<<4 | out[l3Len+12]&0x0f
	if out[0]>>4 == IPv6 {
		binary.BigEndian.PutUint16(out[4:6], uint16(len(out)-IPv6HeaderLen))
		sock.FixTCPChecksumV6(out)
	} else {
		binary.BigEndian.PutUint16(out[2:4], uint16(len(out)))
		sock.FixIPv4Checksum(out[:l3Len])
		sock.FixTCPChecksum(out)
	}
	return out
}

func synWithoutPayload(raw []byte, ver byte, l3Len, tcpHdrLen int) ([]byte, bool) {
	end := l3Len + tcpHdrLen
	if len(raw) <= end || (ver == IPv6 && !hasPlainIPv6Header(raw, ipProtoTCP)) {
		return nil, false
	}
	syn := make([]byte, end)
	copy(syn, raw[:end])
	if ver == IPv4 {
		binary.BigEndian.PutUint16(syn[2:4], uint16(end))
		sock.FixIPv4Checksum(syn[:l3Len])
		sock.FixTCPChecksum(syn)
	} else {
		binary.BigEndian.PutUint16(syn[4:6], uint16(end-IPv6HeaderLen))
		sock.FixTCPChecksumV6(syn)
	}
	return syn, true
}
