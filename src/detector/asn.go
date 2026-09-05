package detector

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/netprobe"
)

type asnInfo struct {
	ASN     string
	Org     string
	Country string
}

var (
	asnCache   = make(map[string]asnInfo)
	asnCacheMu sync.Mutex
)

func cymruName(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.%d.origin.asn.cymru.com", v4[3], v4[2], v4[1], v4[0])
	}
	hex := make([]byte, 0, 32)
	for _, b := range ip.To16() {
		hex = append(hex, "0123456789abcdef"[b>>4], "0123456789abcdef"[b&0x0f])
	}
	parts := make([]string, 0, 32)
	for i := len(hex) - 1; i >= 0; i-- {
		parts = append(parts, string(hex[i]))
	}
	return strings.Join(parts, ".") + ".origin6.asn.cymru.com"
}

func cymruTXT(ctx context.Context, mark uint, name string) (string, error) {
	client := netprobe.HTTPClient(int(mark), 5*time.Second)
	defer client.CloseIdleConnections()
	query := dns.BuildQuery(name, 0, 16)
	var lastErr error
	for _, srv := range Lists().CymruDoHServers {
		qctx, cancel := context.WithTimeout(ctx, 5*time.Second)
		body, err := dns.ResolveDoH(qctx, client, srv, query)
		cancel()
		if err != nil {
			lastErr = err
			continue
		}
		txts := dns.ParseResponseTXT(body)
		if len(txts) == 0 {
			lastErr = fmt.Errorf("no TXT answer")
			continue
		}
		return txts[0], nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no Cymru DoH server configured")
	}
	return "", lastErr
}

func lookupASN(ctx context.Context, mark uint, ipStr string) asnInfo {
	ip := net.ParseIP(ipStr)
	if ip == nil || isFakeRange(ipStr) {
		return asnInfo{}
	}
	asnCacheMu.Lock()
	if info, ok := asnCache[ipStr]; ok {
		asnCacheMu.Unlock()
		return info
	}
	asnCacheMu.Unlock()

	info := asnInfo{}
	origin, err := cymruTXT(ctx, mark, cymruName(ip))
	if err == nil {
		fields := strings.Split(origin, "|")
		if len(fields) >= 3 {
			if asnFields := strings.Fields(fields[0]); len(asnFields) > 0 {
				info.ASN = asnFields[0]
				info.Country = strings.TrimSpace(fields[2])
			}
		}
	}
	if info.ASN != "" {
		if desc, err := cymruTXT(ctx, mark, "AS"+info.ASN+".asn.cymru.com"); err == nil {
			fields := strings.Split(desc, "|")
			if len(fields) >= 5 {
				info.Org = strings.TrimSpace(fields[4])
			}
		}
	}
	if info.ASN != "" {
		asnCacheMu.Lock()
		asnCache[ipStr] = info
		asnCacheMu.Unlock()
	}
	return info
}

func knownResolverOrg(org string) bool {
	low := strings.ToLower(org)
	for _, token := range Lists().KnownResolverNames {
		if token != "" && strings.Contains(low, strings.ToLower(token)) {
			return true
		}
	}
	return false
}
