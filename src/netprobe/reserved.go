package netprobe

import (
	"net"
	"net/netip"
	"syscall"
)

type ReservedAddrError struct {
	Addr string
}

func (e *ReservedAddrError) Error() string {
	return e.Addr + " is a private or local address, it is not probed"
}

func RefuseAddrs(d *net.Dialer, refuse func(netip.Addr) bool) *net.Dialer {
	if refuse == nil {
		return d
	}
	control := d.Control
	d.Control = func(network, address string, c syscall.RawConn) error {
		if host, _, err := net.SplitHostPort(address); err == nil {
			if addr, err := netip.ParseAddr(host); err == nil && refuse(addr) {
				return &ReservedAddrError{Addr: addr.Unmap().String()}
			}
		}
		if control == nil {
			return nil
		}
		return control(network, address, c)
	}
	return d
}
