package nfq

import (
	"encoding/binary"
	"math/rand"
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
	"github.com/florianl/go-nfqueue"
)

var corruptionStrategies = []string{"badsum", "badseq", "badack", "all"}

func (w *Worker) HandleIncoming(q *nfqueue.Nfqueue, id uint32, v byte, raw []byte, ihl int, src net.IP, dstStr string, dport uint16, srcStr string, sport uint16, payload []byte) int {
	incomingSet := connState.GetSetForIncoming(dstStr, dport, srcStr, sport)

	if incomingSet != nil && incomingSet.TCP.Incoming.Mode != config.ConfigOff {
		payloadLen := len(payload)

		if payloadLen > 0 {
			inc := &incomingSet.TCP.Incoming

			switch inc.Mode {
			case "fake":
				if v == IPv4 {
					w.InjectFakeIncoming(incomingSet, raw, ihl, src)
				} else {
					w.InjectFakeIncomingV6(incomingSet, raw, src)
				}

			case "reset":
				if connState.TrackIncomingBytes(dstStr, dport, srcStr, sport, uint64(payloadLen), inc) {
					if v == IPv4 {
						w.InjectResetIncoming(incomingSet, raw, ihl, src)
					} else {
						w.InjectResetIncomingV6(incomingSet, raw, src)
					}
				}

			case "fin":
				if connState.TrackIncomingBytes(dstStr, dport, srcStr, sport, uint64(payloadLen), inc) {
					if v == IPv4 {
						w.InjectFinIncoming(incomingSet, raw, ihl, src)
					} else {
						w.InjectFinIncomingV6(incomingSet, raw, src)
					}
				}

			case "desync":
				if connState.TrackIncomingBytes(dstStr, dport, srcStr, sport, uint64(payloadLen), inc) {
					if v == IPv4 {
						w.InjectDesyncIncoming(incomingSet, raw, ihl, src)
					} else {
						w.InjectDesyncIncomingV6(incomingSet, raw, src)
					}
				}
			}
		}
	}

	if err := q.SetVerdict(id, nfqueue.NfAccept); err != nil {
		log.Tracef("failed to accept incoming packet %d: %v", id, err)
	}
	return 0
}

func (w *Worker) applyCorruption(fake []byte, ihl int, strategy string) {
	// Bounds check: need at least IP header + 18 bytes of TCP (up to checksum)
	if len(fake) < ihl+18 {
		return
	}

	// Pick random strategy if "rand"
	if strategy == "rand" {
		strategy = corruptionStrategies[rand.Intn(len(corruptionStrategies))]
	}

	switch strategy {
	case "badseq":
		seq := binary.BigEndian.Uint32(fake[ihl+4 : ihl+8])
		binary.BigEndian.PutUint32(fake[ihl+4:ihl+8], seq+uint32(rand.Intn(100000)+10000))
		sock.FixIPv4Checksum(fake[:ihl])
		sock.FixTCPChecksum(fake)

	case "badack":
		ack := binary.BigEndian.Uint32(fake[ihl+8 : ihl+12])
		binary.BigEndian.PutUint32(fake[ihl+8:ihl+12], ack+uint32(rand.Intn(100000)+10000))
		sock.FixIPv4Checksum(fake[:ihl])
		sock.FixTCPChecksum(fake)

	case "all":
		seq := binary.BigEndian.Uint32(fake[ihl+4 : ihl+8])
		binary.BigEndian.PutUint32(fake[ihl+4:ihl+8], seq+uint32(rand.Intn(100000)+10000))
		ack := binary.BigEndian.Uint32(fake[ihl+8 : ihl+12])
		binary.BigEndian.PutUint32(fake[ihl+8:ihl+12], ack+uint32(rand.Intn(100000)+10000))
		sock.FixIPv4Checksum(fake[:ihl])
		// Corrupt checksum after fixing
		fake[ihl+16] ^= byte(rand.Intn(255) + 1)
		fake[ihl+17] ^= byte(rand.Intn(255) + 1)

	default: // "badsum"
		sock.FixIPv4Checksum(fake[:ihl])
		fake[ihl+16] ^= byte(rand.Intn(255) + 1)
		fake[ihl+17] ^= byte(rand.Intn(255) + 1)
	}
}

func (w *Worker) applyCorruptionV6(fake []byte, strategy string) {
	ipv6HdrLen := 40

	// Bounds check: need at least IPv6 header + 18 bytes of TCP (up to checksum)
	if len(fake) < ipv6HdrLen+18 {
		return
	}

	if strategy == "rand" {
		strategy = corruptionStrategies[rand.Intn(len(corruptionStrategies))]
	}

	switch strategy {
	case "badseq":
		seq := binary.BigEndian.Uint32(fake[ipv6HdrLen+4 : ipv6HdrLen+8])
		binary.BigEndian.PutUint32(fake[ipv6HdrLen+4:ipv6HdrLen+8], seq+uint32(rand.Intn(100000)+10000))
		sock.FixTCPChecksumV6(fake)

	case "badack":
		ack := binary.BigEndian.Uint32(fake[ipv6HdrLen+8 : ipv6HdrLen+12])
		binary.BigEndian.PutUint32(fake[ipv6HdrLen+8:ipv6HdrLen+12], ack+uint32(rand.Intn(100000)+10000))
		sock.FixTCPChecksumV6(fake)

	case "all":
		seq := binary.BigEndian.Uint32(fake[ipv6HdrLen+4 : ipv6HdrLen+8])
		binary.BigEndian.PutUint32(fake[ipv6HdrLen+4:ipv6HdrLen+8], seq+uint32(rand.Intn(100000)+10000))
		ack := binary.BigEndian.Uint32(fake[ipv6HdrLen+8 : ipv6HdrLen+12])
		binary.BigEndian.PutUint32(fake[ipv6HdrLen+8:ipv6HdrLen+12], ack+uint32(rand.Intn(100000)+10000))
		fake[ipv6HdrLen+16] ^= byte(rand.Intn(255) + 1)
		fake[ipv6HdrLen+17] ^= byte(rand.Intn(255) + 1)

	default: // "badsum"
		fake[ipv6HdrLen+16] ^= byte(rand.Intn(255) + 1)
		fake[ipv6HdrLen+17] ^= byte(rand.Intn(255) + 1)
	}
}

func (w *Worker) InjectFakeIncoming(cfg *config.SetConfig, raw []byte, ihl int, serverIP net.IP) {
	if len(raw) < ihl+20 {
		return
	}
	inc := &cfg.TCP.Incoming
	tcp := raw[ihl:]

	sport := binary.BigEndian.Uint16(tcp[0:2]) // server port (443)
	dport := binary.BigEndian.Uint16(tcp[2:4]) // client port
	serverSeq := binary.BigEndian.Uint32(tcp[4:8])
	serverAck := binary.BigEndian.Uint32(tcp[8:12]) // where server expects client's seq
	tcpHdrLen := int((tcp[12] >> 4) * 4)
	payloadLen := len(tcp) - tcpHdrLen

	for i := 0; i < inc.FakeCount; i++ {
		fake := make([]byte, 40)

		fake[0] = 0x45
		binary.BigEndian.PutUint16(fake[2:4], 40)
		binary.BigEndian.PutUint16(fake[4:6], uint16(time.Now().UnixNano()&0xFFFF)+uint16(i))
		fake[8] = inc.FakeTTL
		fake[9] = 6
		copy(fake[12:16], raw[16:20]) // src = client (was dst in incoming)
		copy(fake[16:20], raw[12:16]) // dst = server (was src in incoming)

		binary.BigEndian.PutUint16(fake[20:22], dport)                             // src port = client port
		binary.BigEndian.PutUint16(fake[22:24], sport)                             // dst port = server port (443)
		binary.BigEndian.PutUint32(fake[24:28], serverAck+uint32(rand.Intn(1000))) // client seq position + jitter
		binary.BigEndian.PutUint32(fake[28:32], serverSeq+uint32(payloadLen))      // ack server's payload
		fake[32] = 0x50
		fake[33] = 0x10                                // ACK flag
		binary.BigEndian.PutUint16(fake[34:36], 65535) // window

		w.applyCorruption(fake, 20, inc.Strategy)

		_ = w.sock.SendIPv4(fake, serverIP)
	}

	log.Tracef("Incoming: injected %d fake ACKs (strategy: %s)", inc.FakeCount, inc.Strategy)
}

func (w *Worker) InjectFakeIncomingV6(cfg *config.SetConfig, raw []byte, serverIP net.IP) {
	inc := &cfg.TCP.Incoming
	ipv6HdrLen := 40

	if len(raw) < ipv6HdrLen+20 {
		return
	}

	tcp := raw[ipv6HdrLen:]
	sport := binary.BigEndian.Uint16(tcp[0:2]) // server port (443)
	dport := binary.BigEndian.Uint16(tcp[2:4]) // client port
	serverSeq := binary.BigEndian.Uint32(tcp[4:8])
	serverAck := binary.BigEndian.Uint32(tcp[8:12]) // where server expects client's seq
	tcpHdrLen := int((tcp[12] >> 4) * 4)
	payloadLen := len(tcp) - tcpHdrLen

	for i := 0; i < inc.FakeCount; i++ {
		fake := make([]byte, 60) // 40 IPv6 + 20 TCP

		fake[0] = 0x60                            // IPv6 version
		binary.BigEndian.PutUint16(fake[4:6], 20) // payload length = TCP header only
		fake[6] = 6                               // next header = TCP
		fake[7] = inc.FakeTTL                     // hop limit
		copy(fake[8:24], raw[24:40])              // src = client (was dst in incoming)
		copy(fake[24:40], raw[8:24])              // dst = server (was src in incoming)

		binary.BigEndian.PutUint16(fake[40:42], dport)                             // src port = client port
		binary.BigEndian.PutUint16(fake[42:44], sport)                             // dst port = server port (443)
		binary.BigEndian.PutUint32(fake[44:48], serverAck+uint32(rand.Intn(1000))) // client seq position + jitter
		binary.BigEndian.PutUint32(fake[48:52], serverSeq+uint32(payloadLen))      // ack server's payload
		fake[52] = 0x50
		fake[53] = 0x10                                // ACK flag
		binary.BigEndian.PutUint16(fake[54:56], 65535) // window

		w.applyCorruptionV6(fake, inc.Strategy)

		_ = w.sock.SendIPv6(fake, serverIP)
	}

	log.Tracef("Incoming V6: injected %d fake ACKs (strategy: %s)", inc.FakeCount, inc.Strategy)
}

func (w *Worker) InjectResetIncoming(cfg *config.SetConfig, raw []byte, ihl int, serverIP net.IP) {
	inc := &cfg.TCP.Incoming
	tcp := raw[ihl:]

	sport := binary.BigEndian.Uint16(tcp[0:2])
	dport := binary.BigEndian.Uint16(tcp[2:4])
	ack := binary.BigEndian.Uint32(tcp[8:12])

	for i := 0; i < inc.FakeCount; i++ {
		rst := make([]byte, 40)

		rst[0] = 0x45
		binary.BigEndian.PutUint16(rst[2:4], 40)
		binary.BigEndian.PutUint16(rst[4:6], uint16(time.Now().UnixNano()&0xFFFF)+uint16(i))
		rst[8] = inc.FakeTTL
		rst[9] = 6
		copy(rst[12:16], raw[16:20])
		copy(rst[16:20], raw[12:16])

		binary.BigEndian.PutUint16(rst[20:22], dport)
		binary.BigEndian.PutUint16(rst[22:24], sport)
		binary.BigEndian.PutUint32(rst[24:28], ack)
		rst[32] = 0x50
		rst[33] = 0x04

		sock.FixIPv4Checksum(rst[:20])
		sock.FixTCPChecksum(rst)

		_ = w.sock.SendIPv4(rst, serverIP)
	}

	log.Tracef("Incoming: injected %d RST packets to %s:%d", inc.FakeCount, serverIP, sport)
}

func (w *Worker) InjectResetIncomingV6(cfg *config.SetConfig, raw []byte, serverIP net.IP) {
	inc := &cfg.TCP.Incoming
	ipv6HdrLen := 40
	tcp := raw[ipv6HdrLen:]

	sport := binary.BigEndian.Uint16(tcp[0:2])
	dport := binary.BigEndian.Uint16(tcp[2:4])
	ack := binary.BigEndian.Uint32(tcp[8:12])

	for i := 0; i < inc.FakeCount; i++ {
		rst := make([]byte, 60)

		rst[0] = 0x60
		binary.BigEndian.PutUint16(rst[4:6], 20)
		rst[6] = 6
		rst[7] = inc.FakeTTL
		copy(rst[8:24], raw[24:40])
		copy(rst[24:40], raw[8:24])

		binary.BigEndian.PutUint16(rst[40:42], dport)
		binary.BigEndian.PutUint16(rst[42:44], sport)
		binary.BigEndian.PutUint32(rst[44:48], ack)
		rst[52] = 0x50
		rst[53] = 0x04

		sock.FixTCPChecksumV6(rst)

		_ = w.sock.SendIPv6(rst, serverIP)
	}

	log.Tracef("Incoming V6: injected %d RST packets to %s:%d", inc.FakeCount, serverIP, sport)
}

func (w *Worker) InjectFinIncoming(cfg *config.SetConfig, raw []byte, ihl int, serverIP net.IP) {
	inc := &cfg.TCP.Incoming
	tcp := raw[ihl:]

	sport := binary.BigEndian.Uint16(tcp[0:2])
	dport := binary.BigEndian.Uint16(tcp[2:4])
	ack := binary.BigEndian.Uint32(tcp[8:12])

	for i := 0; i < inc.FakeCount; i++ {
		fin := make([]byte, 40)

		fin[0] = 0x45
		binary.BigEndian.PutUint16(fin[2:4], 40)
		binary.BigEndian.PutUint16(fin[4:6], uint16(time.Now().UnixNano()&0xFFFF)+uint16(i))
		fin[8] = inc.FakeTTL
		fin[9] = 6
		copy(fin[12:16], raw[16:20])
		copy(fin[16:20], raw[12:16])

		binary.BigEndian.PutUint16(fin[20:22], dport)
		binary.BigEndian.PutUint16(fin[22:24], sport)
		binary.BigEndian.PutUint32(fin[24:28], ack)
		fin[32] = 0x50
		fin[33] = 0x01

		sock.FixIPv4Checksum(fin[:20])
		sock.FixTCPChecksum(fin)

		_ = w.sock.SendIPv4(fin, serverIP)
	}

	log.Tracef("Incoming: injected %d FIN packets to %s:%d", inc.FakeCount, serverIP, sport)
}

func (w *Worker) InjectFinIncomingV6(cfg *config.SetConfig, raw []byte, serverIP net.IP) {
	inc := &cfg.TCP.Incoming
	ipv6HdrLen := 40
	tcp := raw[ipv6HdrLen:]

	sport := binary.BigEndian.Uint16(tcp[0:2])
	dport := binary.BigEndian.Uint16(tcp[2:4])
	ack := binary.BigEndian.Uint32(tcp[8:12])

	for i := 0; i < inc.FakeCount; i++ {
		fin := make([]byte, 60)

		fin[0] = 0x60
		binary.BigEndian.PutUint16(fin[4:6], 20)
		fin[6] = 6
		fin[7] = inc.FakeTTL
		copy(fin[8:24], raw[24:40])
		copy(fin[24:40], raw[8:24])

		binary.BigEndian.PutUint16(fin[40:42], dport)
		binary.BigEndian.PutUint16(fin[42:44], sport)
		binary.BigEndian.PutUint32(fin[44:48], ack)
		fin[52] = 0x50
		fin[53] = 0x01

		sock.FixTCPChecksumV6(fin)

		_ = w.sock.SendIPv6(fin, serverIP)
	}

	log.Tracef("Incoming V6: injected %d FIN packets to %s:%d", inc.FakeCount, serverIP, sport)
}

func (w *Worker) InjectDesyncIncoming(cfg *config.SetConfig, raw []byte, ihl int, serverIP net.IP) {
	inc := &cfg.TCP.Incoming
	tcp := raw[ihl:]

	sport := binary.BigEndian.Uint16(tcp[0:2])
	dport := binary.BigEndian.Uint16(tcp[2:4])
	ack := binary.BigEndian.Uint32(tcp[8:12])

	flags := []byte{0x04, 0x01, 0x10} // RST, FIN, ACK

	for i := 0; i < inc.FakeCount; i++ {
		for _, flag := range flags {
			pkt := make([]byte, 40)

			pkt[0] = 0x45
			binary.BigEndian.PutUint16(pkt[2:4], 40)
			binary.BigEndian.PutUint16(pkt[4:6], uint16(time.Now().UnixNano()&0xFFFF)+uint16(i))
			pkt[8] = inc.FakeTTL
			pkt[9] = 6
			copy(pkt[12:16], raw[16:20])
			copy(pkt[16:20], raw[12:16])

			binary.BigEndian.PutUint16(pkt[20:22], dport)
			binary.BigEndian.PutUint16(pkt[22:24], sport)
			binary.BigEndian.PutUint32(pkt[24:28], ack)
			pkt[32] = 0x50
			pkt[33] = flag

			sock.FixIPv4Checksum(pkt[:20])
			sock.FixTCPChecksum(pkt)

			_ = w.sock.SendIPv4(pkt, serverIP)
		}
	}

	log.Tracef("Incoming: injected %d desync sequences to %s:%d", inc.FakeCount, serverIP, sport)
}

func (w *Worker) InjectDesyncIncomingV6(cfg *config.SetConfig, raw []byte, serverIP net.IP) {
	inc := &cfg.TCP.Incoming
	ipv6HdrLen := 40
	tcp := raw[ipv6HdrLen:]

	sport := binary.BigEndian.Uint16(tcp[0:2])
	dport := binary.BigEndian.Uint16(tcp[2:4])
	ack := binary.BigEndian.Uint32(tcp[8:12])

	flags := []byte{0x04, 0x01, 0x10}

	for i := 0; i < inc.FakeCount; i++ {
		for _, flag := range flags {
			pkt := make([]byte, 60)

			pkt[0] = 0x60
			binary.BigEndian.PutUint16(pkt[4:6], 20)
			pkt[6] = 6
			pkt[7] = inc.FakeTTL
			copy(pkt[8:24], raw[24:40])
			copy(pkt[24:40], raw[8:24])

			binary.BigEndian.PutUint16(pkt[40:42], dport)
			binary.BigEndian.PutUint16(pkt[42:44], sport)
			binary.BigEndian.PutUint32(pkt[44:48], ack)
			pkt[52] = 0x50
			pkt[53] = flag

			sock.FixTCPChecksumV6(pkt)

			_ = w.sock.SendIPv6(pkt, serverIP)
		}
	}

	log.Tracef("Incoming V6: injected %d desync sequences to %s:%d", inc.FakeCount, serverIP, sport)
}
