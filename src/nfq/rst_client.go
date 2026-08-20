package nfq

import (
	"encoding/binary"
	"net"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

func (w *Worker) sendRSTToClientV4(raw []byte, ihl int, srcIP, dstIP net.IP) {
	tcp := raw[ihl:]
	if len(tcp) < 20 {
		return
	}

	clientPort := binary.BigEndian.Uint16(tcp[0:2])
	serverPort := binary.BigEndian.Uint16(tcp[2:4])
	clientAck := binary.BigEndian.Uint32(tcp[8:12])

	rst := make([]byte, 40)

	rst[0] = 0x45
	binary.BigEndian.PutUint16(rst[2:4], 40)
	binary.BigEndian.PutUint16(rst[4:6], uint16(time.Now().UnixNano()&0xFFFF))
	rst[8] = 64
	rst[9] = 6
	copy(rst[12:16], dstIP.To4())
	copy(rst[16:20], srcIP.To4())

	binary.BigEndian.PutUint16(rst[20:22], serverPort)
	binary.BigEndian.PutUint16(rst[22:24], clientPort)
	binary.BigEndian.PutUint32(rst[24:28], clientAck)
	rst[32] = 0x50
	rst[33] = 0x04

	sock.FixIPv4Checksum(rst[:20])
	sock.FixTCPChecksum(rst)

	if err := w.clientSender().SendIPv4(rst, srcIP); err != nil {
		log.Tracef("ip-block: failed to send RST to client %s:%d: %v", srcIP, clientPort, err)
	}
}

func (w *Worker) sendSynRSTToClientV4(raw []byte, ihl int, srcIP, dstIP net.IP) {
	rst := buildSynRSTV4(raw, ihl, srcIP, dstIP)
	if rst == nil {
		return
	}
	if err := w.clientSender().SendIPv4(rst, srcIP); err != nil {
		log.Tracef("ip-block: failed to send SYN reset to client %s: %v", srcIP, err)
	}
}

func buildSynRSTV4(raw []byte, ihl int, srcIP, dstIP net.IP) []byte {
	if len(raw) < ihl+20 {
		return nil
	}
	tcp := raw[ihl:]

	clientPort := binary.BigEndian.Uint16(tcp[0:2])
	serverPort := binary.BigEndian.Uint16(tcp[2:4])
	clientSeq := binary.BigEndian.Uint32(tcp[4:8])

	rst := make([]byte, 40)

	rst[0] = 0x45
	binary.BigEndian.PutUint16(rst[2:4], 40)
	binary.BigEndian.PutUint16(rst[4:6], uint16(time.Now().UnixNano()&0xFFFF))
	rst[8] = 64
	rst[9] = 6
	copy(rst[12:16], dstIP.To4())
	copy(rst[16:20], srcIP.To4())

	binary.BigEndian.PutUint16(rst[20:22], serverPort)
	binary.BigEndian.PutUint16(rst[22:24], clientPort)
	binary.BigEndian.PutUint32(rst[28:32], clientSeq+1)
	rst[32] = 0x50
	rst[33] = 0x14

	sock.FixIPv4Checksum(rst[:20])
	sock.FixTCPChecksum(rst)

	return rst
}

func (w *Worker) sendSynRSTToClientV6(raw []byte, srcIP, dstIP net.IP) {
	rst := buildSynRSTV6(raw, srcIP, dstIP)
	if rst == nil {
		return
	}
	if err := w.clientSender().SendIPv6(rst, srcIP); err != nil {
		log.Tracef("ip-block: failed to send SYN reset to client %s: %v", srcIP, err)
	}
}

func buildSynRSTV6(raw []byte, srcIP, dstIP net.IP) []byte {
	const ipv6HdrLen = 40
	if len(raw) < ipv6HdrLen+20 {
		return nil
	}
	if !hasPlainIPv6Header(raw, ipProtoTCP) {
		return nil
	}
	tcp := raw[ipv6HdrLen:]

	clientPort := binary.BigEndian.Uint16(tcp[0:2])
	serverPort := binary.BigEndian.Uint16(tcp[2:4])
	clientSeq := binary.BigEndian.Uint32(tcp[4:8])

	rst := make([]byte, 60)

	rst[0] = 0x60
	binary.BigEndian.PutUint16(rst[4:6], 20)
	rst[6] = 6
	rst[7] = 64
	copy(rst[8:24], dstIP.To16())
	copy(rst[24:40], srcIP.To16())

	binary.BigEndian.PutUint16(rst[40:42], serverPort)
	binary.BigEndian.PutUint16(rst[42:44], clientPort)
	binary.BigEndian.PutUint32(rst[48:52], clientSeq+1)
	rst[52] = 0x50
	rst[53] = 0x14

	sock.FixTCPChecksumV6(rst)

	return rst
}

func (w *Worker) sendRSTToClientV6(raw []byte, srcIP, dstIP net.IP) {
	ipv6HdrLen := 40
	if !hasPlainIPv6Header(raw, ipProtoTCP) {
		log.Tracef("ip-block: no RST to client %s, the TCP header is not at the fixed IPv6 offset", srcIP)
		return
	}

	tcp := raw[ipv6HdrLen:]
	if len(tcp) < 20 {
		return
	}

	clientPort := binary.BigEndian.Uint16(tcp[0:2])
	serverPort := binary.BigEndian.Uint16(tcp[2:4])
	clientAck := binary.BigEndian.Uint32(tcp[8:12])

	rst := make([]byte, 60)

	rst[0] = 0x60
	binary.BigEndian.PutUint16(rst[4:6], 20)
	rst[6] = 6
	rst[7] = 64
	copy(rst[8:24], dstIP.To16())
	copy(rst[24:40], srcIP.To16())

	binary.BigEndian.PutUint16(rst[40:42], serverPort)
	binary.BigEndian.PutUint16(rst[42:44], clientPort)
	binary.BigEndian.PutUint32(rst[44:48], clientAck)
	rst[52] = 0x50
	rst[53] = 0x04

	sock.FixTCPChecksumV6(rst)

	if err := w.clientSender().SendIPv6(rst, srcIP); err != nil {
		log.Tracef("ip-block: failed to send RST to client %s:%d: %v", srcIP, clientPort, err)
	}
}
