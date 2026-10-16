package ctnl

import (
	"errors"
	"fmt"
	"net/netip"

	"golang.org/x/sys/unix"
)

type Direction uint8

const (
	Original Direction = attrTupleOrig
	Reply    Direction = attrTupleReply
)

func (d Direction) valid() bool {
	return d == Original || d == Reply
}

func (d Direction) String() string {
	switch d {
	case Original:
		return "original"
	case Reply:
		return "reply"
	}
	return fmt.Sprintf("direction %d", uint8(d))
}

type Tuple struct {
	proto uint8
	src   netip.AddrPort
	dst   netip.AddrPort
}

var (
	errTupleProto     = errors.New("conntrack tuple: only TCP and UDP tuples are supported")
	errTupleAddr      = errors.New("conntrack tuple: an address is missing")
	errTupleMapped    = errors.New("conntrack tuple: IPv4-mapped IPv6 address, use the IPv4 form")
	errTupleFamily    = errors.New("conntrack tuple: source and destination are of different families")
	errTuplePort      = errors.New("conntrack tuple: a port is missing")
	errTupleDirection = errors.New("conntrack tuple: the direction is neither original nor reply")
)

func NewTuple(proto uint8, src, dst netip.AddrPort) (Tuple, error) {
	t := Tuple{
		proto: proto,
		src:   netip.AddrPortFrom(src.Addr().WithZone(""), src.Port()),
		dst:   netip.AddrPortFrom(dst.Addr().WithZone(""), dst.Port()),
	}
	if err := t.check(); err != nil {
		return Tuple{}, err
	}
	return t, nil
}

func (t Tuple) check() error {
	switch {
	case t.proto != unix.IPPROTO_TCP && t.proto != unix.IPPROTO_UDP:
		return errTupleProto
	case !t.src.Addr().IsValid() || !t.dst.Addr().IsValid():
		return errTupleAddr
	case t.src.Addr().Is4In6() || t.dst.Addr().Is4In6():
		return errTupleMapped
	case t.src.Addr().Is4() != t.dst.Addr().Is4():
		return errTupleFamily
	case t.src.Port() == 0 || t.dst.Port() == 0:
		return errTuplePort
	}
	return nil
}

func (t Tuple) Reverse() Tuple {
	return Tuple{proto: t.proto, src: t.dst, dst: t.src}
}

func (t Tuple) family() uint8 {
	if t.src.Addr().Is4() {
		return unix.AF_INET
	}
	return unix.AF_INET6
}

func (t Tuple) String() string {
	name := fmt.Sprintf("proto %d", t.proto)
	switch t.proto {
	case unix.IPPROTO_TCP:
		name = "tcp"
	case unix.IPPROTO_UDP:
		name = "udp"
	}
	return fmt.Sprintf("%s %s -> %s", name, t.src, t.dst)
}
