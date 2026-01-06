package nfq

import (
	"encoding/binary"
	"math/rand"
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sock"
)

// WindowManipulator handles TCP window size manipulation
type WindowManipulator struct {
	mode   string
	values []int
	index  int
}

// NewWindowManipulator creates a window manipulator
func NewWindowManipulator(cfg *config.TCPConfig) *WindowManipulator {
	values := cfg.Win.Values
	if len(values) == 0 {
		values = []int{0, 1460, 8192, 65535}
	}

	return &WindowManipulator{
		mode:   cfg.Win.Mode,
		values: values,
		index:  0,
	}
}

// ManipulateWindowIPv4 sends packets with manipulated TCP window
func (w *Worker) ManipulateWindowIPv4(cfg *config.SetConfig, packet []byte, dst net.IP) {
	if cfg.TCP.Win.Mode == config.ConfigOff {
		return
	}

	ipHdrLen := int((packet[0] & 0x0F) * 4)
	if len(packet) < ipHdrLen+20 {
		return
	}

	tcpHdrLen := int((packet[ipHdrLen+12] >> 4) * 4)
	if len(packet) < ipHdrLen+tcpHdrLen {
		return
	}

	wm := NewWindowManipulator(&cfg.TCP)

	switch wm.mode {
	case "oscillate":
		w.sendOscillatingWindows(packet, dst, ipHdrLen, wm)
	case "zero":
		w.sendZeroWindow(packet, dst, ipHdrLen)
	case "random":
		w.sendRandomWindows(packet, dst, ipHdrLen, wm)
	case "escalate":
		w.sendEscalatingWindows(packet, dst, ipHdrLen)
	default:
		w.sendOscillatingWindows(packet, dst, ipHdrLen, wm)
	}
}

// sendOscillatingWindows sends fake packets with oscillating window sizes
func (w *Worker) sendOscillatingWindows(packet []byte, dst net.IP, ipHdrLen int, wm *WindowManipulator) {
	log.Tracef("Window manipulation: oscillating mode")

	// Send fake packets with different windows BEFORE real packet
	for i, winSize := range wm.values {
		fake := make([]byte, ipHdrLen+20) // Just headers, no payload for fakes
		copy(fake, packet[:ipHdrLen+20])

		// Set window size
		binary.BigEndian.PutUint16(fake[ipHdrLen+14:ipHdrLen+16], uint16(winSize))

		// Modify TTL for fake packets (decreasing TTL)
		fake[8] = uint8(10 - i)
		if fake[8] < 1 {
			fake[8] = 1
		}

		// Update IP length
		binary.BigEndian.PutUint16(fake[2:4], uint16(len(fake)))

		// Set ACK flag only (no PSH)
		fake[ipHdrLen+13] = 0x10

		// Fix checksums
		sock.FixIPv4Checksum(fake[:ipHdrLen])
		sock.FixTCPChecksum(fake)

		// Send fake
		_ = w.sock.SendIPv4(fake, dst)

		// Small delay between fakes
		time.Sleep(100 * time.Microsecond)
	}
}

// sendZeroWindow sends zero window probe attack
func (w *Worker) sendZeroWindow(packet []byte, dst net.IP, ipHdrLen int) {
	log.Tracef("Window manipulation: zero window attack")

	// First, send fake packet with zero window
	fake := make([]byte, len(packet))
	copy(fake, packet)

	// Set window to 0
	binary.BigEndian.PutUint16(fake[ipHdrLen+14:ipHdrLen+16], 0)

	// Set low TTL
	fake[8] = 3

	// Fix checksums
	sock.FixIPv4Checksum(fake[:ipHdrLen])
	sock.FixTCPChecksum(fake)

	_ = w.sock.SendIPv4(fake, dst)

	// Small delay
	time.Sleep(500 * time.Microsecond)

	// Send another fake with max window
	fake2 := make([]byte, len(packet))
	copy(fake2, packet)
	binary.BigEndian.PutUint16(fake2[ipHdrLen+14:ipHdrLen+16], 65535)
	fake2[8] = 2
	sock.FixIPv4Checksum(fake2[:ipHdrLen])
	sock.FixTCPChecksum(fake2)
	_ = w.sock.SendIPv4(fake2, dst)
}

// sendRandomWindows sends packets with random window sizes
func (w *Worker) sendRandomWindows(packet []byte, dst net.IP, ipHdrLen int, wm *WindowManipulator) {
	log.Tracef("Window manipulation: random windows")

	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	// Send 3-5 fake packets with random windows
	numFakes := 3 + r.Intn(3)

	for i := 0; i < numFakes; i++ {
		fake := make([]byte, ipHdrLen+20)
		copy(fake, packet[:ipHdrLen+20])

		// Random window from configured values or fully random
		var winSize uint16
		if len(wm.values) > 0 {
			winSize = uint16(wm.values[r.Intn(len(wm.values))])
		} else {
			winSize = uint16(r.Intn(65536))
		}

		binary.BigEndian.PutUint16(fake[ipHdrLen+14:ipHdrLen+16], winSize)

		// Decreasing TTL
		fake[8] = uint8(8 - i)
		if fake[8] < 1 {
			fake[8] = 1
		}

		// Update length
		binary.BigEndian.PutUint16(fake[2:4], uint16(len(fake)))

		sock.FixIPv4Checksum(fake[:ipHdrLen])
		sock.FixTCPChecksum(fake)

		_ = w.sock.SendIPv4(fake, dst)
		time.Sleep(time.Duration(r.Intn(500)) * time.Microsecond)
	}
}

// sendEscalatingWindows gradually increases window size
func (w *Worker) sendEscalatingWindows(packet []byte, dst net.IP, ipHdrLen int) {
	log.Tracef("Window manipulation: escalating windows")

	// Start with tiny window, escalate to full
	windows := []uint16{0, 100, 500, 1460, 8192, 32768, 65535}

	for i, win := range windows {
		fake := make([]byte, ipHdrLen+20)
		copy(fake, packet[:ipHdrLen+20])

		binary.BigEndian.PutUint16(fake[ipHdrLen+14:ipHdrLen+16], win)

		// TTL decreases
		fake[8] = uint8(10 - i)
		if fake[8] < 1 {
			fake[8] = 1
		}

		binary.BigEndian.PutUint16(fake[2:4], uint16(len(fake)))

		sock.FixIPv4Checksum(fake[:ipHdrLen])
		sock.FixTCPChecksum(fake)

		_ = w.sock.SendIPv4(fake, dst)

		// Exponential backoff in delays
		time.Sleep(time.Duration(1<<uint(i)) * 10 * time.Microsecond)
	}
}

// ManipulateWindowIPv6 for IPv6 packets
func (w *Worker) ManipulateWindowIPv6(cfg *config.SetConfig, packet []byte, dst net.IP) {
	if cfg.TCP.Win.Mode == config.ConfigOff {
		return
	}

	ipv6HdrLen := 40
	if len(packet) < ipv6HdrLen+20 {
		return
	}

	wm := NewWindowManipulator(&cfg.TCP)

	switch wm.mode {
	case "oscillate":
		w.sendOscillatingWindowsV6(packet, dst, wm)
	case "zero":
		w.sendZeroWindowV6(packet, dst)
	case "random":
		w.sendRandomWindowsV6(packet, dst, wm)
	case "escalate":
		w.sendEscalatingWindowsV6(packet, dst)
	default:
		w.sendOscillatingWindowsV6(packet, dst, wm)
	}
}

func (w *Worker) sendOscillatingWindowsV6(packet []byte, dst net.IP, wm *WindowManipulator) {
	ipv6HdrLen := 40

	for i, winSize := range wm.values {
		fake := make([]byte, ipv6HdrLen+20)
		copy(fake, packet[:ipv6HdrLen+20])

		// Set window
		binary.BigEndian.PutUint16(fake[ipv6HdrLen+14:ipv6HdrLen+16], uint16(winSize))

		// Modify hop limit
		fake[7] = uint8(10 - i)
		if fake[7] < 1 {
			fake[7] = 1
		}

		// Update payload length
		binary.BigEndian.PutUint16(fake[4:6], 20)

		// ACK only
		fake[ipv6HdrLen+13] = 0x10

		sock.FixTCPChecksumV6(fake)

		_ = w.sock.SendIPv6(fake, dst)
		time.Sleep(100 * time.Microsecond)
	}
}

func (w *Worker) sendZeroWindowV6(packet []byte, dst net.IP) {
	ipv6HdrLen := 40

	fake := make([]byte, len(packet))
	copy(fake, packet)

	binary.BigEndian.PutUint16(fake[ipv6HdrLen+14:ipv6HdrLen+16], 0)
	fake[7] = 3

	sock.FixTCPChecksumV6(fake)
	_ = w.sock.SendIPv6(fake, dst)

	time.Sleep(500 * time.Microsecond)

	fake2 := make([]byte, len(packet))
	copy(fake2, packet)
	binary.BigEndian.PutUint16(fake2[ipv6HdrLen+14:ipv6HdrLen+16], 65535)
	fake2[7] = 2

	sock.FixTCPChecksumV6(fake2)
	_ = w.sock.SendIPv6(fake2, dst)
}

func (w *Worker) sendRandomWindowsV6(packet []byte, dst net.IP, wm *WindowManipulator) {
	ipv6HdrLen := 40
	r := rand.New(rand.NewSource(time.Now().UnixNano()))

	numFakes := 3 + r.Intn(3)

	for i := 0; i < numFakes; i++ {
		fake := make([]byte, ipv6HdrLen+20)
		copy(fake, packet[:ipv6HdrLen+20])

		var winSize uint16
		if len(wm.values) > 0 {
			winSize = uint16(wm.values[r.Intn(len(wm.values))])
		} else {
			winSize = uint16(r.Intn(65536))
		}

		binary.BigEndian.PutUint16(fake[ipv6HdrLen+14:ipv6HdrLen+16], winSize)
		fake[7] = uint8(8 - i)
		if fake[7] < 1 {
			fake[7] = 1
		}

		binary.BigEndian.PutUint16(fake[4:6], 20)

		sock.FixTCPChecksumV6(fake)
		_ = w.sock.SendIPv6(fake, dst)

		time.Sleep(time.Duration(r.Intn(500)) * time.Microsecond)
	}
}

func (w *Worker) sendEscalatingWindowsV6(packet []byte, dst net.IP) {
	ipv6HdrLen := 40
	windows := []uint16{0, 100, 500, 1460, 8192, 32768, 65535}

	for i, win := range windows {
		fake := make([]byte, ipv6HdrLen+20)
		copy(fake, packet[:ipv6HdrLen+20])

		binary.BigEndian.PutUint16(fake[ipv6HdrLen+14:ipv6HdrLen+16], win)
		fake[7] = uint8(10 - i)
		if fake[7] < 1 {
			fake[7] = 1
		}

		binary.BigEndian.PutUint16(fake[4:6], 20)

		sock.FixTCPChecksumV6(fake)
		_ = w.sock.SendIPv6(fake, dst)

		time.Sleep(time.Duration(1<<uint(i)) * 10 * time.Microsecond)
	}
}
