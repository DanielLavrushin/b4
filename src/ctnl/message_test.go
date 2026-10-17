package ctnl

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

func littleEndianHost() bool {
	return binary.NativeEndian.Uint16([]byte{1, 0}) == 1
}

func unhex(t *testing.T, parts ...string) []byte {
	t.Helper()
	b, err := hex.DecodeString(strings.Join(parts, ""))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestDeleteMessageGoldenBytes(t *testing.T) {
	if !littleEndianHost() {
		t.Skip("the golden bytes carry little-endian netlink attribute headers; TestDeleteMessageLayout covers this host")
	}
	v4Tuple := func(outer string) []string {
		return []string{
			"02000000",
			"3400", outer,
			"14000180", "080001000a000102", "08000200cb00710a",
			"1c000280", "0500010011000000", "060002009c400000", "0600030001bb0000",
			"08000800b4b4b4b4", "08001500ffffffff",
		}
	}
	v6Tuple := func(outer string) []string {
		return []string{
			"0a000000",
			"4c00", outer,
			"2c000180",
			"14000300", "20010db8000000000000000000000001",
			"14000400", "20010db8ffff00000000000000000010",
			"1c000280", "0500010011000000", "060002009c400000", "0600030001bb0000",
			"08000800b4b4b4b4", "08001500ffffffff",
		}
	}
	cases := []struct {
		name     string
		src, dst netip.AddrPort
		dir      Direction
		want     []string
	}{
		{"IPv4 original", clientV4, serverV4, Original, v4Tuple("0180")},
		{"IPv4 reply", clientV4, serverV4, Reply, v4Tuple("0280")},
		{"IPv6 original", clientV6, serverV6, Original, v6Tuple("0180")},
		{"IPv6 reply", clientV6, serverV6, Reply, v6Tuple("0280")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m, err := request(msgDelete, mustTuple(t, unix.IPPROTO_UDP, c.src, c.dst), c.dir)
			if err != nil {
				t.Fatal(err)
			}
			if m.Header.Type != 0x0102 {
				t.Errorf("message type %#04x, want IPCTNL_MSG_CT_DELETE of NFNL_SUBSYS_CTNETLINK (0x0102)", uint16(m.Header.Type))
			}
			if m.Header.Flags != netlink.Request|netlink.Acknowledge {
				t.Errorf("flags %v, want NLM_F_REQUEST|NLM_F_ACK", m.Header.Flags)
			}
			if want := unhex(t, c.want...); !bytes.Equal(m.Data, want) {
				t.Errorf("payload\n got %x\nwant %x", m.Data, want)
			}
		})
	}
}

func TestGetMessageSharesTheDeleteEncoding(t *testing.T) {
	tuple := mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4)
	get, err := request(msgGet, tuple, Original)
	if err != nil {
		t.Fatal(err)
	}
	del, err := request(msgDelete, tuple, Original)
	if err != nil {
		t.Fatal(err)
	}
	if get.Header.Type != 0x0101 {
		t.Errorf("message type %#04x, want IPCTNL_MSG_CT_GET (0x0101)", uint16(get.Header.Type))
	}
	if get.Header.Flags&netlink.Dump != 0 {
		t.Error("the self-test GET must ask for one entry, not a dump")
	}
	if !bytes.Equal(get.Data, del.Data) {
		t.Errorf("the self-test must exercise the delete encoder: GET payload %x, DELETE payload %x", get.Data, del.Data)
	}
}

type decodedTuple struct {
	which    uint16
	family   uint8
	src, dst []byte
	srcAttr  uint16
	dstAttr  uint16
	proto    uint8
	sport    uint16
	dport    uint16
	mark     uint32
	markMask uint32
	order    []uint16
}

func decodeRequest(t *testing.T, m netlink.Message) decodedTuple {
	t.Helper()
	if len(m.Data) < 4 {
		t.Fatalf("payload of %d bytes has no nfgenmsg", len(m.Data))
	}
	if m.Data[1] != unix.NFNETLINK_V0 || m.Data[2] != 0 || m.Data[3] != 0 {
		t.Fatalf("nfgenmsg %x, want version 0 and resource id 0", m.Data[:4])
	}
	d := decodedTuple{family: m.Data[0]}
	ad, err := netlink.NewAttributeDecoder(m.Data[4:])
	if err != nil {
		t.Fatal(err)
	}
	for ad.Next() {
		d.order = append(d.order, ad.Type())
		switch ad.Type() {
		case attrTupleOrig, attrTupleReply:
			if ad.TypeFlags()&netlink.Nested == 0 {
				t.Errorf("tuple attribute %d lacks NLA_F_NESTED", ad.Type())
			}
			d.which = ad.Type()
			ad.Nested(func(tuple *netlink.AttributeDecoder) error {
				for tuple.Next() {
					switch tuple.Type() {
					case attrTupleIP:
						tuple.Nested(func(ip *netlink.AttributeDecoder) error {
							for ip.Next() {
								switch ip.Type() {
								case attrIPv4Src, attrIPv6Src:
									d.srcAttr, d.src = ip.Type(), ip.Bytes()
								case attrIPv4Dst, attrIPv6Dst:
									d.dstAttr, d.dst = ip.Type(), ip.Bytes()
								default:
									t.Errorf("unexpected IP attribute %d", ip.Type())
								}
							}
							return nil
						})
					case attrTupleProto:
						tuple.Nested(func(p *netlink.AttributeDecoder) error {
							for p.Next() {
								switch p.Type() {
								case attrProtoNum:
									d.proto = p.Uint8()
								case attrProtoSrcPort:
									d.sport = binary.BigEndian.Uint16(p.Bytes())
								case attrProtoDstPort:
									d.dport = binary.BigEndian.Uint16(p.Bytes())
								default:
									t.Errorf("unexpected proto attribute %d", p.Type())
								}
							}
							return nil
						})
					default:
						t.Errorf("unexpected tuple attribute %d", tuple.Type())
					}
				}
				return nil
			})
		case attrMark:
			d.mark = binary.BigEndian.Uint32(ad.Bytes())
		case attrMarkMask:
			d.markMask = binary.BigEndian.Uint32(ad.Bytes())
		default:
			t.Errorf("unexpected top-level attribute %d", ad.Type())
		}
	}
	if err := ad.Err(); err != nil {
		t.Fatal(err)
	}
	return d
}

func TestDeleteMessageLayout(t *testing.T) {
	for _, c := range []struct {
		name               string
		src, dst           netip.AddrPort
		dir                Direction
		family             uint8
		srcAttr, dstAttr   uint16
		wantSrc, wantDst   []byte
		wantSport, wantDpt uint16
	}{
		{"IPv4 original", clientV4, serverV4, Original, unix.AF_INET, attrIPv4Src, attrIPv4Dst, []byte{10, 0, 1, 2}, []byte{203, 0, 113, 10}, 40000, 443},
		{"IPv4 reply", serverV4, netip.MustParseAddrPort("198.51.100.1:50042"), Reply, unix.AF_INET, attrIPv4Src, attrIPv4Dst, []byte{203, 0, 113, 10}, []byte{198, 51, 100, 1}, 443, 50042},
		{"IPv6 original", clientV6, serverV6, Original, unix.AF_INET6, attrIPv6Src, attrIPv6Dst, clientV6.Addr().AsSlice(), serverV6.Addr().AsSlice(), 40000, 443},
		{"IPv6 reply", serverV6, clientV6, Reply, unix.AF_INET6, attrIPv6Src, attrIPv6Dst, serverV6.Addr().AsSlice(), clientV6.Addr().AsSlice(), 443, 40000},
	} {
		t.Run(c.name, func(t *testing.T) {
			m, err := request(msgDelete, mustTuple(t, unix.IPPROTO_UDP, c.src, c.dst), c.dir)
			if err != nil {
				t.Fatal(err)
			}
			d := decodeRequest(t, m)
			want := []uint16{uint16(c.dir), attrMark, attrMarkMask}
			if len(d.order) != len(want) || d.order[0] != want[0] || d.order[1] != want[1] || d.order[2] != want[2] {
				t.Fatalf("top-level attributes %v, want %v (the tuple, CTA_MARK, CTA_MARK_MASK)", d.order, want)
			}
			if d.family != c.family {
				t.Errorf("nfgenmsg family %d, want %d", d.family, c.family)
			}
			if d.srcAttr != c.srcAttr || d.dstAttr != c.dstAttr {
				t.Errorf("address attributes %d/%d, want %d/%d", d.srcAttr, d.dstAttr, c.srcAttr, c.dstAttr)
			}
			if !bytes.Equal(d.src, c.wantSrc) || !bytes.Equal(d.dst, c.wantDst) {
				t.Errorf("addresses %x -> %x, want %x -> %x", d.src, d.dst, c.wantSrc, c.wantDst)
			}
			if d.proto != unix.IPPROTO_UDP || d.sport != c.wantSport || d.dport != c.wantDpt {
				t.Errorf("proto %d ports %d -> %d, want 17 %d -> %d", d.proto, d.sport, d.dport, c.wantSport, c.wantDpt)
			}
			if d.mark != flushGuardMark || d.markMask != flushGuardMask {
				t.Errorf("CTA_MARK %#x / CTA_MARK_MASK %#x, want %#x / %#x: without them a delete that lost its tuple flushes the whole table", d.mark, d.markMask, uint32(flushGuardMark), uint32(flushGuardMask))
			}
		})
	}
}

func TestNoRequestLeavesTheEncoderWithoutATuple(t *testing.T) {
	tuple := mustTuple(t, unix.IPPROTO_UDP, clientV4, serverV4)
	for _, c := range []struct {
		name  string
		tuple Tuple
		dir   Direction
		want  error
	}{
		{"the zero tuple", Tuple{}, Original, errTupleProto},
		{"the zero tuple as a reply", Tuple{}, Reply, errTupleProto},
		{"no direction", tuple, 0, errTupleDirection},
		{"CTA_STATUS in place of a tuple", tuple, 3, errTupleDirection},
		{"CTA_TUPLE_MASTER in place of a tuple", tuple, 14, errTupleDirection},
	} {
		for _, kind := range []uint16{msgDelete, msgGet} {
			m, err := request(kind, c.tuple, c.dir)
			if !errors.Is(err, c.want) {
				t.Errorf("%s (type %#04x): error %v, want %v", c.name, kind, err, c.want)
			}
			if len(m.Data) != 0 || m.Header.Type != 0 {
				t.Errorf("%s (type %#04x): a message came back next to the error: %+v", c.name, kind, m)
			}
		}
	}
}
