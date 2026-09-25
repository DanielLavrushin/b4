package tproxy

import (
	"net"
	"time"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/sni"
)

const (
	sniffFirstWait  = 250 * time.Millisecond
	sniffTotalWait  = time.Second
	sniffMaxBytes   = 16 * 1024
	tlsRecordHeader = 5
)

type sniffResult struct {
	prefix     []byte
	host       string
	tlsVersion uint16
}

func sniffClient(c net.Conn, firstWait, total time.Duration, limit int) sniffResult {
	defer func() { _ = c.SetReadDeadline(time.Time{}) }()
	start := time.Now()
	buf := make([]byte, limit)
	_ = c.SetReadDeadline(start.Add(firstWait))
	n, err := c.Read(buf)
	if n <= 0 {
		return sniffResult{}
	}
	_ = c.SetReadDeadline(start.Add(total))
	for err == nil && n < limit && needsMoreBytes(buf[:n]) {
		var m int
		m, err = c.Read(buf[n:])
		n += m
	}
	res := sniffResult{prefix: append([]byte(nil), buf[:n]...)}
	res.host, res.tlsVersion = sniffedHost(res.prefix)
	return res
}

func isClientHelloRecord(b []byte) bool {
	return len(b) > tlsRecordHeader && b[0] == 0x16 && b[tlsRecordHeader] == 0x01
}

func needsMoreBytes(b []byte) bool {
	if len(b) > 0 && b[0] == 0x16 {
		if len(b) < tlsRecordHeader+1 {
			return true
		}
		if b[tlsRecordHeader] != 0x01 {
			return false
		}
		return len(b) < tlsRecordHeader+(int(b[3])<<8|int(b[4]))
	}
	if sni.LooksLikeHTTPRequest(b) {
		return !sni.HTTPHeadersComplete(b)
	}
	return false
}

func sniffedHost(b []byte) (string, uint16) {
	if isClientHelloRecord(b) {
		host, ver, _ := sni.ParseTLSClientHelloSNI(b)
		return dns.CleanHostName(host), ver
	}
	if host, ok := sni.ParseHTTPHost(b); ok {
		return dns.CleanHostName(host), 0
	}
	return "", 0
}

type targetInputs struct {
	sniffed string
	names   dns.NameMatches
	learned string
	inSet   func(string) bool
	pinned  func(string) bool
}

func needsSniff(names dns.NameMatches, setHasDomains bool) bool {
	if len(names.Own) == 1 {
		return false
	}
	return !names.Empty() || setHasDomains
}

func chooseTarget(in targetInputs) (string, string) {
	name, source := pickName(in)
	if name == "" || (in.pinned != nil && in.pinned(name)) {
		return "", ""
	}
	return name, source
}

func pickName(in targetInputs) (string, string) {
	if in.sniffed != "" {
		if in.names.Has(in.sniffed) || (in.inSet != nil && in.inSet(in.sniffed)) {
			return in.sniffed, "sni"
		}
		return "", ""
	}
	if len(in.names.Own) > 0 {
		return in.names.Own[0], "dns"
	}
	if in.learned != "" {
		return in.learned, "learned"
	}
	for _, name := range in.names.Local {
		if in.inSet != nil && in.inSet(name) {
			return name, "dns"
		}
	}
	return "", ""
}
