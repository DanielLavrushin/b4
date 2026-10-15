package nflog

import (
	"bytes"
	"encoding/binary"
	"errors"

	"github.com/mdlayher/netlink"
	"github.com/mdlayher/netlink/nlenc"
)

const (
	nlmsgHeaderLen = 16
	nfgenmsgLen    = 4

	nfulaPacketHdr     = 1
	nfulaMark          = 2
	nfulaIfindexIndev  = 4
	nfulaIfindexOutdev = 5
	nfulaPayload       = 9
	nfulaPrefix        = 10
	nfulaSeq           = 12
)

var errShortPacketHeader = errors.New("short NFLOG packet header")

func (c *Conn) process(b []byte, fn func(Packet)) {
	for len(b) >= nlmsgHeaderLen {
		l := int(nlenc.Uint32(b[0:4]))
		if l < nlmsgHeaderLen || l > len(b) {
			return
		}
		if nlenc.Uint16(b[4:6]) == nfulnlMsgPacket {
			if p, ok := parsePacket(b[nlmsgHeaderLen:l]); ok {
				if p.HasSeq {
					c.noteSeq(p.Seq)
				}
				fn(p)
			}
		}
		b = b[min(align4(l), len(b)):]
	}
}

func parsePacket(b []byte) (Packet, bool) {
	if len(b) < nfgenmsgLen {
		return Packet{}, false
	}
	ad, err := netlink.NewAttributeDecoder(b[nfgenmsgLen:])
	if err != nil {
		return Packet{}, false
	}
	ad.ByteOrder = binary.BigEndian
	var p Packet
	for ad.Next() {
		switch ad.Type() {
		case nfulaPacketHdr:
			ad.Do(func(v []byte) error {
				if len(v) < 3 {
					return errShortPacketHeader
				}
				p.Hook = v[2]
				return nil
			})
		case nfulaMark:
			p.Mark = ad.Uint32()
		case nfulaIfindexIndev:
			p.InDev = ad.Uint32()
		case nfulaIfindexOutdev:
			p.OutDev = ad.Uint32()
		case nfulaPayload:
			ad.Do(func(v []byte) error {
				p.Payload = v
				return nil
			})
		case nfulaPrefix:
			ad.Do(func(v []byte) error {
				if i := bytes.IndexByte(v, 0); i >= 0 {
					v = v[:i]
				}
				p.Prefix = string(v)
				return nil
			})
		case nfulaSeq:
			p.Seq, p.HasSeq = ad.Uint32(), true
		}
	}
	if ad.Err() != nil {
		return Packet{}, false
	}
	return p, true
}

func (c *Conn) noteSeq(seq uint32) {
	if c.seqKnown {
		if gap := seq - c.seqNext; gap != 0 && gap < 1<<31 {
			c.gaps.Add(uint64(gap))
		}
	}
	c.seqNext = seq + 1
	c.seqKnown = true
}

func align4(n int) int {
	return (n + 3) &^ 3
}
