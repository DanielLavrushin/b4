package netprobe

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/daniellavrushin/b4/dns"
)

type Resolver struct {
	Mark    int
	Timeout time.Duration
	DoH     []DoHServer
	UDP     []string
}

type UDPAnswer struct {
	IPs      []string
	NXDomain bool
	Empty    bool
}

type ResolveOutcome struct {
	IPs    []string
	DoHURL string
	UDPSrv string
}

type dohJSONResponse struct {
	Answer []struct {
		Type int    `json:"type"`
		Data string `json:"data"`
	} `json:"Answer"`
}

func (r *Resolver) dohServers() []DoHServer {
	if len(r.DoH) > 0 {
		return r.DoH
	}
	return DefaultDoHServers
}

func (r *Resolver) udpServers() []string {
	if len(r.UDP) > 0 {
		return r.UDP
	}
	return DefaultUDPServers
}

func (r *Resolver) timeout() time.Duration {
	if r.Timeout > 0 {
		return r.Timeout
	}
	return 5 * time.Second
}

func wantsV6(recordType string) bool {
	return recordType == "AAAA" || recordType == "ip6"
}

func matchesFamily(ip net.IP, recordType string) bool {
	if wantsV6(recordType) {
		return ip.To4() == nil && ip.To16() != nil
	}
	return ip.To4() != nil
}

func qtypeForRecord(recordType string) uint16 {
	if wantsV6(recordType) {
		return 28
	}
	return 1
}

func (r *Resolver) ResolveDoHOnce(ctx context.Context, srv DoHServer, domain, recordType string) ([]string, error) {
	client := HTTPClient(r.Mark, r.timeout())
	defer client.CloseIdleConnections()

	if srv.Format == DoHWire {
		query := dns.BuildQuery(domain, 0, qtypeForRecord(recordType))
		body, err := dns.ResolveDoH(ctx, client, srv.URL, query)
		if err != nil {
			return nil, err
		}
		return filterIPStrings(dns.ParseResponseIPs(body), recordType), nil
	}

	if recordType == "" {
		recordType = "A"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL, nil)
	if err != nil {
		return nil, err
	}
	q := req.URL.Query()
	q.Set("name", domain)
	q.Set("type", recordType)
	req.URL.RawQuery = q.Encode()
	req.Header.Set("Accept", "application/dns-json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		return nil, err
	}

	var doh dohJSONResponse
	if err := json.Unmarshal(body, &doh); err != nil {
		return nil, err
	}

	wantType := 1
	if wantsV6(recordType) {
		wantType = 28
	}

	seen := make(map[string]bool)
	var ips []string
	for _, ans := range doh.Answer {
		if ans.Type != wantType {
			continue
		}
		if ans.Data == "" || seen[ans.Data] {
			continue
		}
		seen[ans.Data] = true
		ips = append(ips, ans.Data)
	}
	return ips, nil
}

func (r *Resolver) ResolveUDPOnce(ctx context.Context, server, domain, recordType string) (UDPAnswer, error) {
	udpCtx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	addr := server
	if _, _, err := net.SplitHostPort(server); err != nil {
		addr = net.JoinHostPort(server, "53")
	}
	conn, err := Dialer(r.Mark, r.timeout(), 0).DialContext(udpCtx, "udp", addr)
	if err != nil {
		return UDPAnswer{}, err
	}
	defer conn.Close()

	if deadline, ok := udpCtx.Deadline(); ok {
		conn.SetDeadline(deadline)
	}

	if _, err := conn.Write(dns.BuildQuery(domain, 0x4242, qtypeForRecord(recordType))); err != nil {
		return UDPAnswer{}, err
	}

	buf := make([]byte, 1500)
	n, err := conn.Read(buf)
	if err != nil {
		return UDPAnswer{}, err
	}
	resp := buf[:n]
	if len(resp) < 12 {
		return UDPAnswer{}, fmt.Errorf("short DNS response")
	}

	if rcode := resp[3] & 0x0F; rcode == 3 {
		return UDPAnswer{NXDomain: true}, nil
	}

	ips := filterIPStrings(dns.ParseResponseIPs(resp), recordType)
	if len(ips) == 0 {
		return UDPAnswer{Empty: true}, nil
	}
	return UDPAnswer{IPs: ips}, nil
}

func (r *Resolver) ResolveResilient(ctx context.Context, domain, recordType string) (ResolveOutcome, error) {
	for _, srv := range r.dohServers() {
		ips, err := r.ResolveDoHOnce(ctx, srv, domain, recordType)
		if err == nil && len(ips) > 0 {
			return ResolveOutcome{IPs: ips, DoHURL: srv.URL}, nil
		}
	}

	for _, server := range r.udpServers() {
		ans, err := r.ResolveUDPOnce(ctx, server, domain, recordType)
		if err == nil && len(ans.IPs) > 0 {
			return ResolveOutcome{IPs: ans.IPs, UDPSrv: server}, nil
		}
	}

	return ResolveOutcome{}, fmt.Errorf("no DoH or UDP server resolved %s", domain)
}

func filterIPStrings(ips []net.IP, recordType string) []string {
	seen := make(map[string]bool)
	var out []string
	for _, ip := range ips {
		if !matchesFamily(ip, recordType) {
			continue
		}
		s := ip.String()
		if seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}
