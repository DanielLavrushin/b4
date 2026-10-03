package netprobe

import (
	"context"
	"encoding/json"
	"errors"
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

type NXDomainError struct {
	Domain string
	Server string
}

func (e *NXDomainError) Error() string {
	return fmt.Sprintf("%s answers that %s does not exist", e.Server, e.Domain)
}

var errNoAddress = errors.New("no server returned an address")

type dohJSONResponse struct {
	Status int `json:"Status"`
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
		if rcode, ok := dns.ResponseRcode(body); ok && rcode == dns.RcodeNXDomain {
			return nil, &NXDomainError{Domain: domain, Server: srv.URL}
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

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("doh %s: unexpected status %d", srv.URL, resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, 65536))
	if err != nil {
		return nil, err
	}

	var doh dohJSONResponse
	if err := json.Unmarshal(body, &doh); err != nil {
		return nil, err
	}
	if doh.Status == int(dns.RcodeNXDomain) {
		return nil, &NXDomainError{Domain: domain, Server: srv.URL}
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
	dohAttempts := make([]func(context.Context) (ResolveOutcome, error), 0, len(r.dohServers()))
	for _, srv := range r.dohServers() {
		srv := srv
		dohAttempts = append(dohAttempts, func(c context.Context) (ResolveOutcome, error) {
			ips, err := r.ResolveDoHOnce(c, srv, domain, recordType)
			return ResolveOutcome{IPs: ips, DoHURL: srv.URL}, err
		})
	}
	out, dohErr := r.race(ctx, dohAttempts)
	if dohErr == nil {
		return out, nil
	}

	udpAttempts := make([]func(context.Context) (ResolveOutcome, error), 0, len(r.udpServers()))
	for _, server := range r.udpServers() {
		server := server
		udpAttempts = append(udpAttempts, func(c context.Context) (ResolveOutcome, error) {
			ans, err := r.ResolveUDPOnce(c, server, domain, recordType)
			return ResolveOutcome{IPs: ans.IPs, UDPSrv: server}, err
		})
	}
	if out, err := r.race(ctx, udpAttempts); err == nil {
		return out, nil
	}

	var nx *NXDomainError
	if errors.As(dohErr, &nx) {
		return ResolveOutcome{}, nx
	}
	return ResolveOutcome{}, fmt.Errorf("no DoH or UDP server resolved %s", domain)
}

func (r *Resolver) phaseTimeout() time.Duration {
	if t := r.timeout(); t < 5*time.Second {
		return t
	}
	return 5 * time.Second
}

type raceAnswer struct {
	out ResolveOutcome
	err error
}

func (r *Resolver) race(ctx context.Context, attempts []func(context.Context) (ResolveOutcome, error)) (ResolveOutcome, error) {
	if len(attempts) == 0 {
		return ResolveOutcome{}, errNoAddress
	}
	rctx, cancel := context.WithTimeout(ctx, r.phaseTimeout())
	defer cancel()

	ch := make(chan raceAnswer, len(attempts))
	for _, a := range attempts {
		a := a
		go func() {
			out, err := a(rctx)
			ch <- raceAnswer{out: out, err: err}
		}()
	}

	var nxdomain *NXDomainError
	for range attempts {
		select {
		case ans := <-ch:
			if ans.err == nil && len(ans.out.IPs) > 0 {
				return ans.out, nil
			}
			if nxdomain == nil {
				errors.As(ans.err, &nxdomain)
			}
		case <-rctx.Done():
			return ResolveOutcome{}, raceFailure(nxdomain)
		}
	}
	return ResolveOutcome{}, raceFailure(nxdomain)
}

func raceFailure(nxdomain *NXDomainError) error {
	if nxdomain != nil {
		return nxdomain
	}
	return errNoAddress
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
