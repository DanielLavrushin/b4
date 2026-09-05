package dns

import (
	"encoding/binary"
	"net"
	"strings"
)

const (
	optRecordType   = 41
	optUDPSize      = 1232
	ecsOptionCode   = 8
	ecsFamilyIPv4   = 1
	ecsFamilyIPv6   = 2
	ecsScopeUnknown = 0
)

func BuildAQuery(domain string, txid uint16) []byte {
	return BuildQuery(domain, txid, 1)
}

func BuildQuery(domain string, txid uint16, qtype uint16) []byte {
	domain = strings.TrimSuffix(strings.TrimSpace(domain), ".")

	buf := make([]byte, 12, 12+len(domain)+2+5)
	binary.BigEndian.PutUint16(buf[0:2], txid)
	binary.BigEndian.PutUint16(buf[2:4], 0x0100)
	binary.BigEndian.PutUint16(buf[4:6], 1)
	binary.BigEndian.PutUint16(buf[6:8], 0)
	binary.BigEndian.PutUint16(buf[8:10], 0)
	binary.BigEndian.PutUint16(buf[10:12], 0)

	if domain != "" {
		for _, label := range strings.Split(domain, ".") {
			if label == "" {
				continue
			}
			if len(label) > 63 {
				label = label[:63]
			}
			buf = append(buf, byte(len(label)))
			buf = append(buf, label...)
		}
	}
	buf = append(buf, 0)

	qsuffix := make([]byte, 4)
	binary.BigEndian.PutUint16(qsuffix[0:2], qtype)
	binary.BigEndian.PutUint16(qsuffix[2:4], 1)
	buf = append(buf, qsuffix...)

	return buf
}

// BuildQueryWithECS is BuildQuery plus an EDNS0 client-subnet option, so a
// resolver that honours ECS answers as it would for a client inside subnet.
func BuildQueryWithECS(domain string, txid uint16, qtype uint16, subnet net.IPNet) []byte {
	query := BuildQuery(domain, txid, qtype)

	family := uint16(ecsFamilyIPv4)
	addr := subnet.IP.To4()
	if addr == nil {
		addr = subnet.IP.To16()
		family = ecsFamilyIPv6
	}
	if addr == nil {
		return query
	}
	prefix, bits := subnet.Mask.Size()
	if bits == 0 || prefix > len(addr)*8 {
		prefix = len(addr) * 8
	}
	addrLen := (prefix + 7) / 8

	rdata := make([]byte, 0, 8+addrLen)
	rdata = binary.BigEndian.AppendUint16(rdata, ecsOptionCode)
	rdata = binary.BigEndian.AppendUint16(rdata, uint16(4+addrLen))
	rdata = binary.BigEndian.AppendUint16(rdata, family)
	rdata = append(rdata, byte(prefix), ecsScopeUnknown)
	rdata = append(rdata, addr[:addrLen]...)

	opt := make([]byte, 0, 11+len(rdata))
	opt = append(opt, 0)
	opt = binary.BigEndian.AppendUint16(opt, optRecordType)
	opt = binary.BigEndian.AppendUint16(opt, optUDPSize)
	opt = append(opt, 0, 0, 0, 0)
	opt = binary.BigEndian.AppendUint16(opt, uint16(len(rdata)))
	opt = append(opt, rdata...)

	query = append(query, opt...)
	binary.BigEndian.PutUint16(query[10:12], 1)
	return query
}
