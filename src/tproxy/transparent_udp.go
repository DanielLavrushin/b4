package tproxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

func udpSockControl(v6 bool, recvOrigDst bool) func(string, string, syscall.RawConn) error {
	return func(network, address string, c syscall.RawConn) error {
		var ctlErr error
		err := c.Control(func(fd uintptr) {
			f := int(fd)
			if e := unix.SetsockoptInt(f, unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); e != nil {
				ctlErr = fmt.Errorf("SO_REUSEADDR: %w", e)
				return
			}
			if e := unix.SetsockoptInt(f, unix.SOL_SOCKET, unix.SO_REUSEPORT, 1); e != nil {
				ctlErr = fmt.Errorf("SO_REUSEPORT: %w", e)
				return
			}
			if v6 {
				if e := unix.SetsockoptInt(f, unix.IPPROTO_IPV6, unix.IPV6_V6ONLY, 1); e != nil {
					ctlErr = fmt.Errorf("IPV6_V6ONLY: %w", e)
					return
				}
				if e := unix.SetsockoptInt(f, unix.SOL_IPV6, unix.IPV6_TRANSPARENT, 1); e != nil {
					ctlErr = fmt.Errorf("IPV6_TRANSPARENT: %w", e)
					return
				}
				if recvOrigDst {
					if e := unix.SetsockoptInt(f, unix.SOL_IPV6, unix.IPV6_RECVORIGDSTADDR, 1); e != nil {
						ctlErr = fmt.Errorf("IPV6_RECVORIGDSTADDR: %w", e)
						return
					}
				}
			} else {
				if e := unix.SetsockoptInt(f, unix.SOL_IP, unix.IP_TRANSPARENT, 1); e != nil {
					ctlErr = fmt.Errorf("IP_TRANSPARENT: %w", e)
					return
				}
				if recvOrigDst {
					if e := unix.SetsockoptInt(f, unix.SOL_IP, unix.IP_RECVORIGDSTADDR, 1); e != nil {
						ctlErr = fmt.Errorf("IP_RECVORIGDSTADDR: %w", e)
						return
					}
				}
			}
		})
		if err != nil {
			return err
		}
		return ctlErr
	}
}

func listenTransparentUDP(ctx context.Context, network, addr string, v6 bool) (*net.UDPConn, error) {
	lc := net.ListenConfig{Control: udpSockControl(v6, true)}
	pc, err := lc.ListenPacket(ctx, network, addr)
	if err != nil {
		return nil, err
	}
	uc, ok := pc.(*net.UDPConn)
	if !ok {
		pc.Close()
		return nil, fmt.Errorf("listener is not *net.UDPConn")
	}
	return uc, nil
}

func openReplySocket(ctx context.Context, dst *net.UDPAddr, v6 bool) (*net.UDPConn, error) {
	network := "udp4"
	if v6 {
		network = "udp6"
	}
	lc := net.ListenConfig{Control: udpSockControl(v6, false)}
	pc, err := lc.ListenPacket(ctx, network, dst.String())
	if err != nil {
		return nil, err
	}
	uc, ok := pc.(*net.UDPConn)
	if !ok {
		pc.Close()
		return nil, fmt.Errorf("reply socket is not *net.UDPConn")
	}
	return uc, nil
}

func parseOrigDst(oob []byte, v6 bool) (*net.UDPAddr, error) {
	msgs, err := unix.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, err
	}
	for _, m := range msgs {
		if v6 {
			if m.Header.Level == unix.IPPROTO_IPV6 && m.Header.Type == unix.IPV6_ORIGDSTADDR {
				if len(m.Data) < unix.SizeofSockaddrInet6 {
					continue
				}
				sa := (*unix.RawSockaddrInet6)(unsafe.Pointer(&m.Data[0]))
				p := (*[2]byte)(unsafe.Pointer(&sa.Port))
				port := int(p[0])<<8 | int(p[1])
				ip := make(net.IP, net.IPv6len)
				copy(ip, sa.Addr[:])
				return &net.UDPAddr{IP: ip, Port: port}, nil
			}
			continue
		}
		if m.Header.Level == unix.IPPROTO_IP && m.Header.Type == unix.IP_ORIGDSTADDR {
			if len(m.Data) < unix.SizeofSockaddrInet4 {
				continue
			}
			sa := (*unix.RawSockaddrInet4)(unsafe.Pointer(&m.Data[0]))
			p := (*[2]byte)(unsafe.Pointer(&sa.Port))
			port := int(p[0])<<8 | int(p[1])
			ip := net.IPv4(sa.Addr[0], sa.Addr[1], sa.Addr[2], sa.Addr[3])
			return &net.UDPAddr{IP: ip, Port: port}, nil
		}
	}
	return nil, fmt.Errorf("no original destination in control message")
}

func isClosedErr(err error) bool {
	return errors.Is(err, net.ErrClosed)
}
