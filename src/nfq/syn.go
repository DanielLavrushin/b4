package nfq

import (
	"encoding/binary"
	"net"

	"github.com/daniellavrushin/b4/config"
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

	// Add MD5 option
	fakeSyn = sock.AddTCPMD5Option(fakeSyn, false)

	_ = w.sock.SendIPv4(fakeSyn, dst)
}

func (w *Worker) sendFakeSynWithMD5V6(set *config.SetConfig, raw []byte, dst net.IP) {
	const ipv6HdrLen = 40

	fakeSyn := make([]byte, len(raw))
	copy(fakeSyn, raw)

	fakeSyn[7] = dynamicTTL(raw, true, set.Faking.TTL)

	// Modify seq so server ignores if it arrives
	seq := binary.BigEndian.Uint32(fakeSyn[ipv6HdrLen+4 : ipv6HdrLen+8])
	binary.BigEndian.PutUint32(fakeSyn[ipv6HdrLen+4:ipv6HdrLen+8], seq-10000)

	// Add MD5 option (also fixes checksums)
	fakeSyn = sock.AddTCPMD5Option(fakeSyn, true)

	_ = w.sock.SendIPv6(fakeSyn, dst)
}
