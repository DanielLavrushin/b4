package detector

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/netprobe"
)

func round1(v float64) float64 {
	return float64(int64(v*10+0.5)) / 10
}

const maxNameservers = 3

var defaultNameservers = []string{"127.0.0.1", "::1"}

func systemNameservers() []string {
	f, err := os.Open("/etc/resolv.conf")
	if err != nil {
		return defaultNameservers
	}
	defer f.Close()
	if servers := parseNameservers(f); len(servers) > 0 {
		return servers
	}
	return defaultNameservers
}

func parseNameservers(r io.Reader) []string {
	var out []string
	seen := make(map[string]bool)
	used := 0
	sc := bufio.NewScanner(r)
	for used < maxNameservers && sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 || fields[0] != "nameserver" {
			continue
		}
		addr, err := netip.ParseAddr(fields[1])
		if err != nil {
			continue
		}
		used++
		ip := addr.String()
		if seen[ip] {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	return out
}

func fetchText(ctx context.Context, mark uint, url string, timeout time.Duration) string {
	client := netprobe.HTTPClient(int(mark), timeout)
	defer client.CloseIdleConnections()
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("User-Agent", "curl/8.0")
	resp, err := client.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return ""
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 256))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(body))
}

func (s *Suite) runNetwork() {
	lists := Lists()
	info := &NetworkInfo{}
	for _, u := range lists.IPLookupURLs {
		if s.canceled() {
			return
		}
		if ip := fetchText(s.ctx, s.directMark, u, 4*time.Second); net.ParseIP(ip) != nil {
			info.WANIP = ip
			break
		}
	}
	for _, u := range lists.IP6LookupURLs {
		if s.canceled() {
			return
		}
		ip := fetchText(s.ctx, s.directMark, u, 3*time.Second)
		if p := net.ParseIP(ip); p != nil && p.To4() == nil {
			info.IPv6 = true
			break
		}
	}
	if info.WANIP != "" {
		asn := lookupASN(s.ctx, s.directMark, info.WANIP)
		info.ASN, info.Org, info.Country = asn.ASN, asn.Org, asn.Country
	}
	s.mu.Lock()
	s.Network = info
	s.mu.Unlock()
}

func fetchBytes(ctx context.Context, mark uint, url string, timeout time.Duration, limit int64) ([]byte, error) {
	client := netprobe.HTTPClient(int(mark), timeout)
	defer client.CloseIdleConnections()
	rctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "curl/8.0")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}
