package tables

import (
	"encoding/binary"
	"net/netip"
	"time"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const nftNetlinkDeadline = 5 * time.Second

var nftRefreshElements = nftRefreshElementsNetlink

func nftGenMsg(family uint8, resID uint16) []byte {
	b := []byte{family, unix.NFNETLINK_V0, 0, 0}
	binary.BigEndian.PutUint16(b[2:], resID)
	return b
}

func nftBatchMessage(kind uint16) netlink.Message {
	return netlink.Message{
		Header: netlink.Header{Type: netlink.HeaderType(kind), Flags: netlink.Request},
		Data:   nftGenMsg(unix.AF_UNSPEC, unix.NFNL_SUBSYS_NFTABLES),
	}
}

func nftSetElemMessage(kind uint16, table, set string, addrs []netip.Addr, ttl time.Duration) (netlink.Message, error) {
	ae := netlink.NewAttributeEncoder()
	ae.String(unix.NFTA_SET_ELEM_LIST_TABLE, table)
	ae.String(unix.NFTA_SET_ELEM_LIST_SET, set)
	ae.Nested(unix.NFTA_SET_ELEM_LIST_ELEMENTS, func(list *netlink.AttributeEncoder) error {
		for _, addr := range addrs {
			list.Nested(unix.NFTA_LIST_ELEM, func(elem *netlink.AttributeEncoder) error {
				elem.Nested(unix.NFTA_SET_ELEM_KEY, func(key *netlink.AttributeEncoder) error {
					key.Bytes(unix.NFTA_DATA_VALUE, addr.AsSlice())
					return nil
				})
				if ttl > 0 {
					elem.Bytes(unix.NFTA_SET_ELEM_TIMEOUT, binary.BigEndian.AppendUint64(nil, uint64(ttl.Milliseconds())))
				}
				return nil
			})
		}
		return nil
	})
	attrs, err := ae.Encode()
	if err != nil {
		return netlink.Message{}, err
	}
	flags := netlink.Request | netlink.Acknowledge
	if kind == unix.NFT_MSG_NEWSETELEM {
		flags |= netlink.Create
	}
	return netlink.Message{
		Header: netlink.Header{Type: netlink.HeaderType(unix.NFNL_SUBSYS_NFTABLES<<8 | kind), Flags: flags},
		Data:   append(nftGenMsg(unix.NFPROTO_INET, 0), attrs...),
	}, nil
}

func nftRefreshMessages(table, set string, addrs []netip.Addr, ttl time.Duration) ([]netlink.Message, error) {
	msgs := []netlink.Message{nftBatchMessage(unix.NFNL_MSG_BATCH_BEGIN)}
	for _, step := range []struct {
		kind uint16
		ttl  time.Duration
	}{{unix.NFT_MSG_NEWSETELEM, ttl}, {unix.NFT_MSG_DELSETELEM, 0}, {unix.NFT_MSG_NEWSETELEM, ttl}} {
		m, err := nftSetElemMessage(step.kind, table, set, addrs, step.ttl)
		if err != nil {
			return nil, err
		}
		msgs = append(msgs, m)
	}
	return append(msgs, nftBatchMessage(unix.NFNL_MSG_BATCH_END)), nil
}

func nftRefreshElementsNetlink(table, set string, addrs []netip.Addr, ttl time.Duration) error {
	msgs, err := nftRefreshMessages(table, set, addrs, ttl)
	if err != nil {
		return err
	}
	conn, err := netlink.Dial(unix.NETLINK_NETFILTER, nil)
	if err != nil {
		return err
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(nftNetlinkDeadline)); err != nil {
		return err
	}
	sent, err := conn.SendMessages(msgs)
	if err != nil {
		return err
	}
	waiting := map[uint32]bool{}
	for _, m := range sent {
		if m.Header.Flags&netlink.Acknowledge != 0 {
			waiting[m.Header.Sequence] = true
		}
	}
	for len(waiting) > 0 {
		replies, err := conn.Receive()
		if err != nil {
			return err
		}
		for _, r := range replies {
			if r.Header.Type == netlink.Error {
				delete(waiting, r.Header.Sequence)
			}
		}
	}
	return nil
}
