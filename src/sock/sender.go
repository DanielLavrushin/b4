package sock

import (
	"net"
	"syscall"

	"github.com/daniellavrushin/b4/log"
	"golang.org/x/sys/unix"
)

type Sender struct {
	fd4  int
	fd6  int
	mark int
}

func NewSenderWithMark(mark int) (*Sender, error) {
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
	}
	return s, nil
}

func NewSender(mark int) (*Sender, error) {
	return NewSenderWithMark(mark)
}

func (s *Sender) SendIPv4(packet []byte, destIP net.IP) error {
	log.Tracef("Sending IPv4 packet to %s, len=%d", destIP.String(), len(packet))
	addr := syscall.SockaddrInet4{}
	copy(addr.Addr[:], destIP.To4())
	return syscall.Sendto(s.fd4, packet, 0, &addr)
}

func (s *Sender) SendIPv6(packet []byte, destIP net.IP) error {
	if s.fd6 < 0 {
		return nil
	}
	log.Tracef("Sending IPv6 packet to %s, len=%d", destIP.String(), len(packet))
	addr := syscall.SockaddrInet6{}
	copy(addr.Addr[:], destIP.To16())
	return syscall.Sendto(s.fd6, packet, 0, &addr)
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
