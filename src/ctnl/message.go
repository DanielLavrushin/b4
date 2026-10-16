package ctnl

import (
	"encoding/binary"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	msgGet    = unix.NFNL_SUBSYS_CTNETLINK<<8 | 1
	msgDelete = unix.NFNL_SUBSYS_CTNETLINK<<8 | 2

	attrTupleOrig  = 1
	attrTupleReply = 2
	attrMark       = 8
	attrMarkMask   = 21

	attrTupleIP    = 1
	attrTupleProto = 2

	attrIPv4Src = 1
	attrIPv4Dst = 2
	attrIPv6Src = 3
	attrIPv6Dst = 4

	attrProtoNum     = 1
	attrProtoSrcPort = 2
	attrProtoDstPort = 3

	flushGuardMark = 0xb4b4b4b4
	flushGuardMask = 0xffffffff
)

func request(kind uint16, t Tuple, dir Direction) (netlink.Message, error) {
	if err := t.check(); err != nil {
		return netlink.Message{}, err
	}
	if !dir.valid() {
		return netlink.Message{}, errTupleDirection
	}
	ae := netlink.NewAttributeEncoder()
	encodeTuple(ae, uint16(dir), t)
	ae.Bytes(attrMark, binary.BigEndian.AppendUint32(nil, flushGuardMark))
	ae.Bytes(attrMarkMask, binary.BigEndian.AppendUint32(nil, flushGuardMask))
	attrs, err := ae.Encode()
	if err != nil {
		return netlink.Message{}, err
	}
	return netlink.Message{
		Header: netlink.Header{Type: netlink.HeaderType(kind), Flags: netlink.Request | netlink.Acknowledge},
		Data:   append([]byte{t.family(), unix.NFNETLINK_V0, 0, 0}, attrs...),
	}, nil
}

func encodeTuple(ae *netlink.AttributeEncoder, typ uint16, t Tuple) {
	srcAttr, dstAttr := uint16(attrIPv4Src), uint16(attrIPv4Dst)
	if t.family() == unix.AF_INET6 {
		srcAttr, dstAttr = attrIPv6Src, attrIPv6Dst
	}
	ae.Nested(typ, func(tuple *netlink.AttributeEncoder) error {
		tuple.Nested(attrTupleIP, func(ip *netlink.AttributeEncoder) error {
			ip.Bytes(srcAttr, t.src.Addr().AsSlice())
			ip.Bytes(dstAttr, t.dst.Addr().AsSlice())
			return nil
		})
		tuple.Nested(attrTupleProto, func(proto *netlink.AttributeEncoder) error {
			proto.Uint8(attrProtoNum, t.proto)
			proto.Bytes(attrProtoSrcPort, binary.BigEndian.AppendUint16(nil, t.src.Port()))
			proto.Bytes(attrProtoDstPort, binary.BigEndian.AppendUint16(nil, t.dst.Port()))
			return nil
		})
		return nil
	})
}
