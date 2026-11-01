package ctnl

import (
	"encoding/binary"
	"errors"
	"sync"
	"time"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	attrProtoInfo     = 4
	attrCountersOrig  = 9
	attrCountersReply = 10

	attrProtoInfoTCP      = 1
	attrProtoInfoTCPState = 1

	attrCountersPackets = 1
	attrCountersBytes   = 2

	TCPEstablished = 3
)

var errUnavailable = errors.New("conntrack netlink is unavailable")

type Flow struct {
	TCPState     uint8
	HasState     bool
	OrigPackets  uint64
	OrigBytes    uint64
	ReplyPackets uint64
	ReplyBytes   uint64
	HasCounters  bool
}

var (
	lookupMu   sync.Mutex
	lookupConn *netlink.Conn
)

func Lookup(t Tuple, dir Direction) (Flow, bool, error) {
	if err := t.check(); err != nil {
		return Flow{}, false, err
	}
	if !dir.valid() {
		return Flow{}, false, errTupleDirection
	}
	if !Available() {
		return Flow{}, false, errUnavailable
	}
	m, err := lookupRequest(t, dir)
	if err != nil {
		return Flow{}, false, err
	}
	lookupMu.Lock()
	defer lookupMu.Unlock()
	if lookupConn == nil {
		c, err := dial()
		if err != nil {
			return Flow{}, false, err
		}
		lookupConn = c
	}
	_ = lookupConn.SetDeadline(time.Now().Add(requestTimeout))
	msgs, err := lookupConn.Execute(m)
	switch {
	case errors.Is(err, unix.ENOENT):
		return Flow{}, false, nil
	case err != nil:
		_ = lookupConn.Close()
		lookupConn = nil
		return Flow{}, false, err
	case len(msgs) == 0 || len(msgs[0].Data) < 4:
		return Flow{}, false, errors.New("conntrack lookup returned no entry")
	}
	f, err := parseFlow(msgs[0].Data[4:])
	if err != nil {
		return Flow{}, false, err
	}
	return f, true, nil
}

func lookupRequest(t Tuple, dir Direction) (netlink.Message, error) {
	ae := netlink.NewAttributeEncoder()
	encodeTuple(ae, uint16(dir), t)
	attrs, err := ae.Encode()
	if err != nil {
		return netlink.Message{}, err
	}
	return netlink.Message{
		Header: netlink.Header{Type: netlink.HeaderType(msgGet), Flags: netlink.Request | netlink.Acknowledge},
		Data:   append([]byte{t.family(), unix.NFNETLINK_V0, 0, 0}, attrs...),
	}, nil
}

func parseFlow(b []byte) (Flow, error) {
	ad, err := netlink.NewAttributeDecoder(b)
	if err != nil {
		return Flow{}, err
	}
	ad.ByteOrder = binary.BigEndian
	var f Flow
	for ad.Next() {
		switch ad.Type() {
		case attrProtoInfo:
			ad.Nested(func(info *netlink.AttributeDecoder) error {
				for info.Next() {
					if info.Type() != attrProtoInfoTCP {
						continue
					}
					info.Nested(func(tcp *netlink.AttributeDecoder) error {
						for tcp.Next() {
							if tcp.Type() == attrProtoInfoTCPState {
								f.TCPState, f.HasState = tcp.Uint8(), true
							}
						}
						return nil
					})
				}
				return nil
			})
		case attrCountersOrig, attrCountersReply:
			reply := ad.Type() == attrCountersReply
			ad.Nested(func(c *netlink.AttributeDecoder) error {
				c.ByteOrder = binary.BigEndian
				var packets, bytes uint64
				for c.Next() {
					switch c.Type() {
					case attrCountersPackets:
						packets = c.Uint64()
					case attrCountersBytes:
						bytes = c.Uint64()
					}
				}
				if reply {
					f.ReplyPackets, f.ReplyBytes = packets, bytes
				} else {
					f.OrigPackets, f.OrigBytes = packets, bytes
				}
				f.HasCounters = true
				return nil
			})
		}
	}
	return f, ad.Err()
}
