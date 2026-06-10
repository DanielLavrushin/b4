package dns

import (
	"encoding/binary"
	"strings"
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
