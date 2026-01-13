package nfq

import (
	"encoding/binary"
	"net"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sock"
)

// sendFakeSyn sends a fake SYN packet with payload to confuse DPI systems
func (w *Worker) sendFakeSyn(set *config.SetConfig, raw []byte, ipHdrLen, tcpHdrLen int) {
	var fakePayload []byte
	switch set.Faking.SNIType {
	case config.FakePayloadDefault2:
		fakePayload = sock.FakeSNI2
	default:
		fakePayload = sock.FakeSNI1
	}

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

	ttl := set.TCP.SynTTL
	if ttl == 0 {
		ttl = set.Faking.TTL
	}
	if ttl == 0 {
		ttl = 3
	}
	fakePkt[8] = ttl

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
	}

	sock.FixIPv4Checksum(fakePkt[:ipHdrLen])
	sock.FixTCPChecksum(fakePkt)

	// ALWAYS corrupt TCP checksum so server drops it even if TTL reaches
	fakePkt[ipHdrLen+16] ^= 0xFF
	fakePkt[ipHdrLen+17] ^= 0xFF

	dst := net.IP(fakePkt[16:20])
	_ = w.sock.SendIPv4(fakePkt, dst)
}

// sendFakeSynV6 sends a fake SYN packet for IPv6
func (w *Worker) sendFakeSynV6(set *config.SetConfig, raw []byte, ipHdrLen, tcpHdrLen int) {
	var fakePayload []byte
	switch set.Faking.SNIType {
	case config.FakePayloadDefault2:
		fakePayload = sock.FakeSNI2
	default:
		fakePayload = sock.FakeSNI1
	}

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

	// ALWAYS set low hop limit
	ttl := set.TCP.SynTTL
	if ttl == 0 {
		ttl = set.Faking.TTL
	}
	if ttl == 0 {
		ttl = 3
	}
	fakePkt[7] = ttl

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
	}

	sock.FixTCPChecksumV6(fakePkt)

	// ALWAYS corrupt TCP checksum
	fakePkt[ipHdrLen+16] ^= 0xFF
	fakePkt[ipHdrLen+17] ^= 0xFF

	dst := net.IP(fakePkt[24:40])
	_ = w.sock.SendIPv6(fakePkt, dst)
}

func (w *Worker) sendFakeSynWithMD5(set *config.SetConfig, raw []byte, ihl int, dst net.IP) {
	fakeSyn := make([]byte, len(raw))
	copy(fakeSyn, raw)

	// Low TTL - reaches DPI but dies before server
	ttl := set.Faking.TTL
	if ttl == 0 {
		ttl = 3
	}
	fakeSyn[8] = ttl

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

	// Low hop limit - reaches DPI but dies before server
	ttl := set.Faking.TTL
	if ttl == 0 {
		ttl = 3
	}
	fakeSyn[7] = ttl

	// Modify seq so server ignores if it arrives
	seq := binary.BigEndian.Uint32(fakeSyn[ipv6HdrLen+4 : ipv6HdrLen+8])
	binary.BigEndian.PutUint32(fakeSyn[ipv6HdrLen+4:ipv6HdrLen+8], seq-10000)

	// Add MD5 option (also fixes checksums)
	fakeSyn = sock.AddTCPMD5Option(fakeSyn, true)

	_ = w.sock.SendIPv6(fakeSyn, dst)
}
