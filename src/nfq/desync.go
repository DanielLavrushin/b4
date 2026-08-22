package nfq

import (
	"crypto/rand"
	"encoding/binary"
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

type DesyncAttacker struct {
	mode  string
	ttl   uint8
	count int
}

// dynamicTTL computes a fake TTL that looks plausible to DPI.
// Uses config TTL (user-tuned hop count to DPI) but clamps it to never
// exceed the original packet's TTL, preventing impossible values that DPI detects.
func dynamicTTL(packet []byte, isIPv6 bool, configTTL uint8) uint8 {
	var realTTL uint8
	if isIPv6 {
		realTTL = packet[7]
	} else {
		realTTL = packet[8]
	}
	ttl := configTTL
	if ttl == 0 {
		ttl = 5
	}
	if ttl >= realTTL && realTTL > 1 {
		ttl = realTTL - 1
	}
	if ttl < 1 {
		ttl = 1
	}
	return ttl
}

func corruptTCPChecksum(pkt []byte, ipHdrLen int) {
	pkt[ipHdrLen+16] ^= 0xFF
	pkt[ipHdrLen+17] ^= 0xFF
}

func NewDesyncAttacker(cfg *config.TCPConfig) *DesyncAttacker {
	return &DesyncAttacker{
		mode:  cfg.Desync.Mode,
		ttl:   cfg.Desync.TTL,
		count: cfg.Desync.Count,
	}
}

func (w *Worker) ExecuteDesyncIPv4(cfg *config.SetConfig, packet []byte, dst net.IP) {
	if cfg.TCP.Desync.Mode == config.ConfigOff {
		return
	}

	da := NewDesyncAttacker(&cfg.TCP)

	switch da.mode {
	case "rst":
		w.sendDesyncRST(packet, dst, da)
	case "fin":
		w.sendDesyncFIN(packet, dst, da)
	case "ack":
		w.sendDesyncACK(packet, dst, da)
	case "combo":
		w.sendDesyncCombo(packet, dst, da)
	case "full":
		w.sendDesyncFull(packet, dst, da)
	default:
		w.sendDesyncCombo(packet, dst, da)
	}
}

func (w *Worker) sendDesyncRST(packet []byte, dst net.IP, da *DesyncAttacker) {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	if len(packet) < ipHdrLen+20 {
		return
	}

	log.Tracef("Desync: Sending %d fake RST packets", da.count)

	origSeq := binary.BigEndian.Uint32(packet[ipHdrLen+4 : ipHdrLen+8])
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || ipHdrLen+tcpHdrLen > len(packet) {
		return
	}

	for i := 0; i < da.count; i++ {
		fake := make([]byte, ipHdrLen+tcpHdrLen)
		copy(fake, packet[:ipHdrLen+tcpHdrLen])

		if i%2 == 0 {
			fake[ipHdrLen+13] = 0x04
		} else {
			fake[ipHdrLen+13] = 0x14
		}

		var seqOffset int32
		switch i {
		case 0:
			seqOffset = -10000
		case 1:
			seqOffset = 0
		case 2:
			seqOffset = 10000
		default:
			seqOffset = int32(i * 5000)
		}

		newSeq := uint32(int32(origSeq) + seqOffset)
		binary.BigEndian.PutUint32(fake[ipHdrLen+4:ipHdrLen+8], newSeq)

		if fake[ipHdrLen+13] == 0x04 {
			binary.BigEndian.PutUint32(fake[ipHdrLen+8:ipHdrLen+12], 0)
		}

		fake[8] = dynamicTTL(packet, false, da.ttl)
		binary.BigEndian.PutUint16(fake[2:4], uint16(ipHdrLen+tcpHdrLen))

		sock.FixIPv4Checksum(fake[:ipHdrLen])
		sock.FixTCPChecksum(fake)
		corruptTCPChecksum(fake, ipHdrLen)

		_ = w.sock.SendIPv4(fake, dst)
		time.Sleep(100 * time.Microsecond)
	}
}

func (w *Worker) sendDesyncFIN(packet []byte, dst net.IP, da *DesyncAttacker) {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	if len(packet) < ipHdrLen+20 {
		return
	}

	log.Tracef("Desync: Sending %d fake FIN packets", da.count)

	origSeq := binary.BigEndian.Uint32(packet[ipHdrLen+4 : ipHdrLen+8])
	origAck := binary.BigEndian.Uint32(packet[ipHdrLen+8 : ipHdrLen+12])
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || ipHdrLen+tcpHdrLen > len(packet) {
		return
	}

	for i := 0; i < da.count; i++ {
		fake := make([]byte, ipHdrLen+tcpHdrLen)
		copy(fake, packet[:ipHdrLen+tcpHdrLen])

		fake[ipHdrLen+13] = 0x11

		seqOffset := uint32(50000 + i*10000)
		if origSeq > seqOffset {
			binary.BigEndian.PutUint32(fake[ipHdrLen+4:ipHdrLen+8], origSeq-seqOffset)
		} else {
			binary.BigEndian.PutUint32(fake[ipHdrLen+4:ipHdrLen+8], 1)
		}

		binary.BigEndian.PutUint32(fake[ipHdrLen+8:ipHdrLen+12], origAck)

		fake[8] = dynamicTTL(packet, false, da.ttl)
		binary.BigEndian.PutUint16(fake[2:4], uint16(ipHdrLen+tcpHdrLen))

		sock.FixIPv4Checksum(fake[:ipHdrLen])
		sock.FixTCPChecksum(fake)
		corruptTCPChecksum(fake, ipHdrLen)

		_ = w.sock.SendIPv4(fake, dst)
		time.Sleep(200 * time.Microsecond)
	}
}

func (w *Worker) sendDesyncACK(packet []byte, dst net.IP, da *DesyncAttacker) {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	if len(packet) < ipHdrLen+20 {
		return
	}

	log.Tracef("Desync: Sending %d fake ACK packets", da.count)

	origSeq := binary.BigEndian.Uint32(packet[ipHdrLen+4 : ipHdrLen+8])
	origAck := binary.BigEndian.Uint32(packet[ipHdrLen+8 : ipHdrLen+12])
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || ipHdrLen+tcpHdrLen > len(packet) {
		return
	}
	baseTTL := dynamicTTL(packet, false, da.ttl)

	for i := 0; i < da.count; i++ {
		fake := make([]byte, ipHdrLen+tcpHdrLen)
		copy(fake, packet[:ipHdrLen+tcpHdrLen])

		fake[ipHdrLen+13] = 0x10

		var rb [4]byte
		rand.Read(rb[:])
		futureSeq := origSeq + binary.BigEndian.Uint32(rb[:])
		binary.BigEndian.PutUint32(fake[ipHdrLen+4:ipHdrLen+8], futureSeq)

		futureAck := origAck + uint32(100000*(i+1))
		binary.BigEndian.PutUint32(fake[ipHdrLen+8:ipHdrLen+12], futureAck)

		ttl := baseTTL
		if uint8(i) >= ttl {
			ttl = 1
		} else {
			ttl = ttl - uint8(i)
		}
		if ttl < 1 {
			ttl = 1
		}
		fake[8] = ttl

		binary.BigEndian.PutUint16(fake[2:4], uint16(ipHdrLen+tcpHdrLen))

		sock.FixIPv4Checksum(fake[:ipHdrLen])
		sock.FixTCPChecksum(fake)
		corruptTCPChecksum(fake, ipHdrLen)

		_ = w.sock.SendIPv4(fake, dst)
		time.Sleep(50 * time.Microsecond)
	}
}

func (w *Worker) sendDesyncCombo(packet []byte, dst net.IP, da *DesyncAttacker) {
	log.Tracef("Desync: Combo attack (RST+FIN+ACK)")

	w.sendDesyncRST(packet, dst, &DesyncAttacker{ttl: da.ttl, count: 1})
	time.Sleep(500 * time.Microsecond)

	w.sendDesyncFIN(packet, dst, &DesyncAttacker{ttl: da.ttl, count: 1})
	time.Sleep(500 * time.Microsecond)

	w.sendDesyncACK(packet, dst, &DesyncAttacker{ttl: da.ttl, count: 2})
}

func (w *Worker) sendDesyncFull(packet []byte, dst net.IP, da *DesyncAttacker) {
	ipHdrLen := int((packet[0] & 0x0F) * 4)
	if len(packet) < ipHdrLen+20 {
		return
	}

	log.Tracef("Desync: Full attack sequence")

	origSeq := binary.BigEndian.Uint32(packet[ipHdrLen+4 : ipHdrLen+8])
	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || ipHdrLen+tcpHdrLen > len(packet) {
		return
	}
	ttl := dynamicTTL(packet, false, da.ttl)

	synFake := make([]byte, ipHdrLen+tcpHdrLen)
	copy(synFake, packet[:ipHdrLen+tcpHdrLen])
	synFake[ipHdrLen+13] = 0x02
	binary.BigEndian.PutUint32(synFake[ipHdrLen+4:ipHdrLen+8], origSeq-100000)
	synFake[8] = ttl
	binary.BigEndian.PutUint16(synFake[2:4], uint16(ipHdrLen+tcpHdrLen))
	sock.FixIPv4Checksum(synFake[:ipHdrLen])
	sock.FixTCPChecksum(synFake)
	corruptTCPChecksum(synFake, ipHdrLen)
	_ = w.sock.SendIPv4(synFake, dst)

	time.Sleep(100 * time.Microsecond)

	for i := 0; i < 3; i++ {
		rstFake := make([]byte, ipHdrLen+tcpHdrLen)
		copy(rstFake, packet[:ipHdrLen+tcpHdrLen])
		rstFake[ipHdrLen+13] = 0x04

		seq := origSeq + uint32(i*100)
		binary.BigEndian.PutUint32(rstFake[ipHdrLen+4:ipHdrLen+8], seq)

		rstFake[8] = ttl
		binary.BigEndian.PutUint16(rstFake[2:4], uint16(ipHdrLen+tcpHdrLen))
		sock.FixIPv4Checksum(rstFake[:ipHdrLen])
		sock.FixTCPChecksum(rstFake)
		corruptTCPChecksum(rstFake, ipHdrLen)

		_ = w.sock.SendIPv4(rstFake, dst)
		time.Sleep(50 * time.Microsecond)
	}

	pushFake := make([]byte, ipHdrLen+tcpHdrLen)
	copy(pushFake, packet[:ipHdrLen+tcpHdrLen])
	pushFake[ipHdrLen+13] = 0x18
	pushFake[8] = ttl
	binary.BigEndian.PutUint16(pushFake[2:4], uint16(ipHdrLen+tcpHdrLen))
	sock.FixIPv4Checksum(pushFake[:ipHdrLen])
	sock.FixTCPChecksum(pushFake)
	corruptTCPChecksum(pushFake, ipHdrLen)
	_ = w.sock.SendIPv4(pushFake, dst)

	time.Sleep(100 * time.Microsecond)

	urgFake := make([]byte, ipHdrLen+tcpHdrLen)
	copy(urgFake, packet[:ipHdrLen+tcpHdrLen])
	urgFake[ipHdrLen+13] = 0x39
	binary.BigEndian.PutUint16(urgFake[ipHdrLen+18:ipHdrLen+20], 0xFFFF)
	urgFake[8] = ttl
	binary.BigEndian.PutUint16(urgFake[2:4], uint16(ipHdrLen+tcpHdrLen))
	sock.FixIPv4Checksum(urgFake[:ipHdrLen])
	sock.FixTCPChecksum(urgFake)
	corruptTCPChecksum(urgFake, ipHdrLen)
	_ = w.sock.SendIPv4(urgFake, dst)
}

func (w *Worker) ExecuteDesyncIPv6(cfg *config.SetConfig, packet []byte, dst net.IP) {
	if cfg.TCP.Desync.Mode == config.ConfigOff {
		return
	}

	da := NewDesyncAttacker(&cfg.TCP)

	switch da.mode {
	case "rst":
		w.sendDesyncRSTv6(packet, dst, da)
	case "fin":
		w.sendDesyncFINv6(packet, dst, da)
	case "ack":
		w.sendDesyncACKv6(packet, dst, da)
	case "combo":
		w.sendDesyncCombov6(packet, dst, da)
	case "full":
		w.sendDesyncFullv6(packet, dst, da)
	default:
		w.sendDesyncCombov6(packet, dst, da)
	}
}

func (w *Worker) sendDesyncRSTv6(packet []byte, dst net.IP, da *DesyncAttacker) {
	ipv6HdrLen := 40
	if len(packet) < ipv6HdrLen+20 {
		return
	}

	origSeq := binary.BigEndian.Uint32(packet[ipv6HdrLen+4 : ipv6HdrLen+8])
	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || ipv6HdrLen+tcpHdrLen > len(packet) {
		return
	}

	for i := 0; i < da.count; i++ {
		fake := make([]byte, ipv6HdrLen+tcpHdrLen)
		copy(fake, packet[:ipv6HdrLen+tcpHdrLen])

		if i%2 == 0 {
			fake[ipv6HdrLen+13] = 0x04
		} else {
			fake[ipv6HdrLen+13] = 0x14
		}

		var seqOffset int32
		switch i {
		case 0:
			seqOffset = -10000
		case 1:
			seqOffset = 0
		case 2:
			seqOffset = 10000
		default:
			seqOffset = int32(i * 5000)
		}

		newSeq := uint32(int32(origSeq) + seqOffset)
		binary.BigEndian.PutUint32(fake[ipv6HdrLen+4:ipv6HdrLen+8], newSeq)

		if fake[ipv6HdrLen+13] == 0x04 {
			binary.BigEndian.PutUint32(fake[ipv6HdrLen+8:ipv6HdrLen+12], 0)
		}

		fake[7] = dynamicTTL(packet, true, da.ttl)
		binary.BigEndian.PutUint16(fake[4:6], uint16(tcpHdrLen))

		sock.FixTCPChecksumV6(fake)
		corruptTCPChecksum(fake, ipv6HdrLen)

		_ = w.sock.SendIPv6(fake, dst)
		time.Sleep(100 * time.Microsecond)
	}
}

func (w *Worker) sendDesyncFINv6(packet []byte, dst net.IP, da *DesyncAttacker) {
	ipv6HdrLen := 40
	if len(packet) < ipv6HdrLen+20 {
		return
	}

	origSeq := binary.BigEndian.Uint32(packet[ipv6HdrLen+4 : ipv6HdrLen+8])
	origAck := binary.BigEndian.Uint32(packet[ipv6HdrLen+8 : ipv6HdrLen+12])
	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || ipv6HdrLen+tcpHdrLen > len(packet) {
		return
	}

	for i := 0; i < da.count; i++ {
		fake := make([]byte, ipv6HdrLen+tcpHdrLen)
		copy(fake, packet[:ipv6HdrLen+tcpHdrLen])

		fake[ipv6HdrLen+13] = 0x11

		seqOffset := uint32(50000 + i*10000)
		if origSeq > seqOffset {
			binary.BigEndian.PutUint32(fake[ipv6HdrLen+4:ipv6HdrLen+8], origSeq-seqOffset)
		} else {
			binary.BigEndian.PutUint32(fake[ipv6HdrLen+4:ipv6HdrLen+8], 1)
		}

		binary.BigEndian.PutUint32(fake[ipv6HdrLen+8:ipv6HdrLen+12], origAck)

		fake[7] = dynamicTTL(packet, true, da.ttl)
		binary.BigEndian.PutUint16(fake[4:6], uint16(tcpHdrLen))

		sock.FixTCPChecksumV6(fake)
		corruptTCPChecksum(fake, ipv6HdrLen)

		_ = w.sock.SendIPv6(fake, dst)
		time.Sleep(200 * time.Microsecond)
	}
}

func (w *Worker) sendDesyncACKv6(packet []byte, dst net.IP, da *DesyncAttacker) {
	ipv6HdrLen := 40
	if len(packet) < ipv6HdrLen+20 {
		return
	}

	origSeq := binary.BigEndian.Uint32(packet[ipv6HdrLen+4 : ipv6HdrLen+8])
	origAck := binary.BigEndian.Uint32(packet[ipv6HdrLen+8 : ipv6HdrLen+12])
	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || ipv6HdrLen+tcpHdrLen > len(packet) {
		return
	}
	baseTTL := dynamicTTL(packet, true, da.ttl)

	for i := 0; i < da.count; i++ {
		fake := make([]byte, ipv6HdrLen+tcpHdrLen)
		copy(fake, packet[:ipv6HdrLen+tcpHdrLen])

		fake[ipv6HdrLen+13] = 0x10

		var rb [4]byte
		rand.Read(rb[:])
		futureSeq := origSeq + binary.BigEndian.Uint32(rb[:])
		binary.BigEndian.PutUint32(fake[ipv6HdrLen+4:ipv6HdrLen+8], futureSeq)

		futureAck := origAck + uint32(100000*(i+1))
		binary.BigEndian.PutUint32(fake[ipv6HdrLen+8:ipv6HdrLen+12], futureAck)

		ttl := baseTTL
		if uint8(i) >= ttl {
			ttl = 1
		} else {
			ttl = ttl - uint8(i)
		}
		if ttl < 1 {
			ttl = 1
		}
		fake[7] = ttl

		binary.BigEndian.PutUint16(fake[4:6], uint16(tcpHdrLen))

		sock.FixTCPChecksumV6(fake)
		corruptTCPChecksum(fake, ipv6HdrLen)

		_ = w.sock.SendIPv6(fake, dst)
		time.Sleep(50 * time.Microsecond)
	}
}

func (w *Worker) sendDesyncCombov6(packet []byte, dst net.IP, da *DesyncAttacker) {
	w.sendDesyncRSTv6(packet, dst, &DesyncAttacker{ttl: da.ttl, count: 1})
	time.Sleep(500 * time.Microsecond)

	w.sendDesyncFINv6(packet, dst, &DesyncAttacker{ttl: da.ttl, count: 1})
	time.Sleep(500 * time.Microsecond)

	w.sendDesyncACKv6(packet, dst, &DesyncAttacker{ttl: da.ttl, count: 2})
}

func (w *Worker) sendDesyncFullv6(packet []byte, dst net.IP, da *DesyncAttacker) {
	ipv6HdrLen := 40
	if len(packet) < ipv6HdrLen+20 {
		return
	}

	origSeq := binary.BigEndian.Uint32(packet[ipv6HdrLen+4 : ipv6HdrLen+8])
	tcpHdrLen := int((packet[ipv6HdrLen+12] >> 4) * 4)
	if tcpHdrLen < 20 || ipv6HdrLen+tcpHdrLen > len(packet) {
		return
	}
	ttl := dynamicTTL(packet, true, da.ttl)

	synFake := make([]byte, ipv6HdrLen+tcpHdrLen)
	copy(synFake, packet[:ipv6HdrLen+tcpHdrLen])
	synFake[ipv6HdrLen+13] = 0x02
	binary.BigEndian.PutUint32(synFake[ipv6HdrLen+4:ipv6HdrLen+8], origSeq-100000)
	synFake[7] = ttl
	binary.BigEndian.PutUint16(synFake[4:6], uint16(tcpHdrLen))
	sock.FixTCPChecksumV6(synFake)
	corruptTCPChecksum(synFake, ipv6HdrLen)
	_ = w.sock.SendIPv6(synFake, dst)

	time.Sleep(100 * time.Microsecond)

	for i := 0; i < 3; i++ {
		rstFake := make([]byte, ipv6HdrLen+tcpHdrLen)
		copy(rstFake, packet[:ipv6HdrLen+tcpHdrLen])
		rstFake[ipv6HdrLen+13] = 0x04

		seq := origSeq + uint32(i*100)
		binary.BigEndian.PutUint32(rstFake[ipv6HdrLen+4:ipv6HdrLen+8], seq)

		rstFake[7] = ttl
		binary.BigEndian.PutUint16(rstFake[4:6], uint16(tcpHdrLen))
		sock.FixTCPChecksumV6(rstFake)
		corruptTCPChecksum(rstFake, ipv6HdrLen)

		_ = w.sock.SendIPv6(rstFake, dst)
		time.Sleep(50 * time.Microsecond)
	}
}
