package sock

import (
	"errors"
	"fmt"
	"net"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

const sendTimeoutSec = 1

var (
	sendDropped     atomic.Uint64
	sendDropLastLog atomic.Int64
)

func SendDropped() uint64 {
	return sendDropped.Load()
}

func ResetSendDropped() {
	sendDropped.Store(0)
}

func isSendBackpressure(err error) bool {
	return errors.Is(err, syscall.EAGAIN) || errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.ENOBUFS)
}

func noteSendDrop(family string, err error) {
	n := sendDropped.Add(1)
	now := time.Now().Unix()
	last := sendDropLastLog.Load()
	if now-last >= 10 && sendDropLastLog.CompareAndSwap(last, now) {
		log.Warnf("Raw %s socket cannot take more packets (%v), %d injected packets dropped so far - the outgoing interface is not draining", family, err, n)
	}
}

func setSendTimeout(fd int) {
	tv := unix.Timeval{Sec: sendTimeoutSec}
	if err := unix.SetsockoptTimeval(fd, unix.SOL_SOCKET, unix.SO_SNDTIMEO, &tv); err != nil {
		log.Tracef("raw socket send timeout not applied: %v", err)
	}
}

type Sender struct {
	fd4  int
	fd6  int
	mark int
}

func NewSenderWithMark(mark int) (*Sender, error) {
	return NewSenderWithMarkDevice(mark, "")
}

func NewSenderWithMarkDevice(mark int, device string) (*Sender, error) {
	s := &Sender{
		fd4:  -1,
		fd6:  -1,
		mark: mark,
	}

	// Create IPv4 raw socket
	fd4, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
	if err != nil {
		return nil, err
	}
	s.fd4 = fd4

	if err := syscall.SetsockoptInt(s.fd4, syscall.IPPROTO_IP, syscall.IP_HDRINCL, 1); err != nil {
		s.Close()
		return nil, err
	}
	if err := syscall.SetsockoptInt(s.fd4, syscall.SOL_SOCKET, unix.SO_MARK, mark); err != nil {
		s.Close()
		return nil, err
	}
	setSendTimeout(s.fd4)
	if device != "" {
		if err := syscall.SetsockoptString(s.fd4, syscall.SOL_SOCKET, unix.SO_BINDTODEVICE, device); err != nil {
			s.Close()
			return nil, fmt.Errorf("bind IPv4 raw socket to %s: %w", device, err)
		}
	}

	// Create IPv6 raw socket
	fd6, err := syscall.Socket(syscall.AF_INET6, syscall.SOCK_RAW, syscall.IPPROTO_RAW)
	if err != nil {
		log.Warnf("Failed to create IPv6 raw socket: %v - IPv6 bypass disabled", err)
		s.fd6 = -1
	} else {
		s.fd6 = fd6
		if err := syscall.SetsockoptInt(s.fd6, syscall.SOL_SOCKET, unix.SO_MARK, mark); err != nil {
			log.Warnf("Failed to set SO_MARK on IPv6 socket: %v", err)
		}
		setSendTimeout(s.fd6)
		if device != "" {
			if err := syscall.SetsockoptString(s.fd6, syscall.SOL_SOCKET, unix.SO_BINDTODEVICE, device); err != nil {
				log.Warnf("Failed to bind IPv6 socket to %s: %v - IPv6 bypass disabled", device, err)
				_ = syscall.Close(s.fd6)
				s.fd6 = -1
			}
		}
	}
	return s, nil
}

func NewSender(mark int) (*Sender, error) {
	return NewSenderWithMark(mark)
}

func (s *Sender) SendIPv4(packet []byte, destIP net.IP) error {
	if log.Level(log.CurLevel.Load()) >= log.LevelTrace {
		log.Tracef("Sending IPv4 packet to %s, len=%d", destIP.String(), len(packet))
	}
	addr := syscall.SockaddrInet4{}
	copy(addr.Addr[:], destIP.To4())
	if err := syscall.Sendto(s.fd4, packet, unix.MSG_DONTWAIT, &addr); err != nil {
		if isSendBackpressure(err) {
			noteSendDrop("IPv4", err)
			return nil
		}
		return err
	}
	return nil
}

func (s *Sender) SendIPv6(packet []byte, destIP net.IP) error {
	if s.fd6 < 0 {
		return nil
	}
	if log.Level(log.CurLevel.Load()) >= log.LevelTrace {
		log.Tracef("Sending IPv6 packet to %s, len=%d", destIP.String(), len(packet))
	}
	addr := syscall.SockaddrInet6{}
	copy(addr.Addr[:], destIP.To16())
	if err := syscall.Sendto(s.fd6, packet, unix.MSG_DONTWAIT, &addr); err != nil {
		if isSendBackpressure(err) {
			noteSendDrop("IPv6", err)
			return nil
		}
		return err
	}
	return nil
}

func (s *Sender) IPv6Ready() bool {
	return s.fd6 >= 0
}

func (s *Sender) Close() {
	if s.fd4 >= 0 {
		_ = syscall.Close(s.fd4)
		s.fd4 = -1
	}
	if s.fd6 >= 0 {
		_ = syscall.Close(s.fd6)
		s.fd6 = -1
	}
}
