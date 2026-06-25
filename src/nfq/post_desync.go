package nfq

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

func (w *Worker) sendPostDesyncRST(cfg *config.SetConfig, raw []byte, ipHdrLen int, dst net.IP) {
	if len(raw) < ipHdrLen+20 {
		return
	}

	tcpHdrLen := int((raw[ipHdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || ipHdrLen+tcpHdrLen > len(raw) {
		return
	}
	payloadLen := len(raw) - ipHdrLen - tcpHdrLen
	seq := binary.BigEndian.Uint32(raw[ipHdrLen+4 : ipHdrLen+8])
	ttl := dynamicTTL(raw, false, cfg.Faking.TTL)

	// Send burst of fake packets with different flags/sequences
	fakeTypes := []struct {
		flags  byte
		seqOff int
	}{
		{0x04, 0},              // RST
		{0x14, payloadLen},     // RST+ACK after payload
		{0x11, payloadLen + 1}, // FIN+ACK
		{0x04, -10000},         // RST with past seq
		{0x14, 100000},         // RST+ACK with future seq
	}

	for _, ft := range fakeTypes {
		rstLen := ipHdrLen + tcpHdrLen
		rst := make([]byte, rstLen)
		copy(rst, raw[:rstLen])

		rst[ipHdrLen+13] = ft.flags

		newSeq := int64(seq) + int64(ft.seqOff)
		if newSeq < 0 {
			newSeq = 0
		}
		binary.BigEndian.PutUint32(rst[ipHdrLen+4:ipHdrLen+8], uint32(newSeq))

		rst[8] = ttl
		binary.BigEndian.PutUint16(rst[2:4], uint16(rstLen))

		sock.FixIPv4Checksum(rst[:ipHdrLen])
		sock.FixTCPChecksum(rst)
		corruptTCPChecksum(rst, ipHdrLen)

		log.Tracef("Sending post-desync RST to %s with flags 0x%02x and seq offset %d", dst.String(), ft.flags, ft.seqOff)
		_ = w.sock.SendIPv4(rst, dst)
		time.Sleep(100 * time.Microsecond)
	}
}

func (w *Worker) sendPostDesyncRSTv6(cfg *config.SetConfig, raw []byte, dst net.IP) {
	const ipv6HdrLen = 40
	if len(raw) < ipv6HdrLen+20 {
		return
	}

	tcpHdrLen := int((raw[ipv6HdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || ipv6HdrLen+tcpHdrLen > len(raw) {
		return
	}
	payloadLen := len(raw) - ipv6HdrLen - tcpHdrLen

	rstLen := ipv6HdrLen + tcpHdrLen
	rst := make([]byte, rstLen)
	copy(rst, raw[:rstLen])

	rst[ipv6HdrLen+13] = 0x14

	seq := binary.BigEndian.Uint32(raw[ipv6HdrLen+4 : ipv6HdrLen+8])
	binary.BigEndian.PutUint32(rst[ipv6HdrLen+4:ipv6HdrLen+8], seq+uint32(payloadLen))

	rst[7] = dynamicTTL(raw, true, cfg.Faking.TTL)

	binary.BigEndian.PutUint16(rst[4:6], uint16(tcpHdrLen))

	sock.FixTCPChecksumV6(rst)
	corruptTCPChecksum(rst, ipv6HdrLen)

	_ = w.sock.SendIPv6(rst, dst)
}
