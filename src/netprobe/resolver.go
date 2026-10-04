package netprobe

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"net/http"
	"time"

	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/dns/endpoint"
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

type NoDataError struct {
	Domain string
	Server string
}

func (e *NoDataError) Error() string {
	return fmt.Sprintf("%s answers that %s exists but has no address of the asked family", e.Server, e.Domain)
}

type RcodeError struct {
	Server string
	Rcode  uint8
}

func (e *RcodeError) Error() string {
	switch e.Rcode {
	case dns.RcodeServFail:
		return fmt.Sprintf("%s answers SERVFAIL", e.Server)
	case dns.RcodeRefused:
		return fmt.Sprintf("%s refuses the query", e.Server)
	}
	return fmt.Sprintf("%s answers with error code %d", e.Server, e.Rcode)
}

var (
	errNoAddress = errors.New("no server returned an address")
	errTruncated = errors.New("the answer was truncated")
)

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
		return answerIPs(body, 0, srv.URL, domain, recordType)
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
	if len(ips) == 0 && doh.Status == 0 {
		return nil, &NoDataError{Domain: domain, Server: srv.URL}
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

	var nodata *NoDataError
	var nx *NXDomainError
	switch {
	case errors.As(dohErr, &nodata):
		return ResolveOutcome{}, nodata
	case errors.As(dohErr, &nx):
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
	var nodata *NoDataError
	for range attempts {
		select {
		case ans := <-ch:
			if ans.err == nil && len(ans.out.IPs) > 0 {
				return ans.out, nil
			}
			if nxdomain == nil {
				errors.As(ans.err, &nxdomain)
			}
			if nodata == nil {
				errors.As(ans.err, &nodata)
			}
		case <-rctx.Done():
			return ResolveOutcome{}, raceFailure(nxdomain, nodata)
		}
	}
	return ResolveOutcome{}, raceFailure(nxdomain, nodata)
}

func raceFailure(nxdomain *NXDomainError, nodata *NoDataError) error {
	switch {
	case nodata != nil:
		return nodata
	case nxdomain != nil:
		return nxdomain
	}
	return errNoAddress
}

type EndpointAnswer struct {
	IPs     []string
	OverUDP bool
}

var endpointDoHClient = func(mark int, timeout time.Duration) *http.Client {
	return dns.MarkedDoHClient(mark, timeout)
}

func (r *Resolver) ResolveEndpoint(ctx context.Context, ep endpoint.Endpoint, domain, recordType string) (EndpointAnswer, error) {
	switch ep.Transport {
	case endpoint.HTTPS:
		client := endpointDoHClient(r.Mark, r.timeout())
		defer client.CloseIdleConnections()
		query := dns.BuildQuery(domain, 0, qtypeForRecord(recordType))
		body, err := dns.ResolveDoH(ctx, client, ep.URL, query)
		if err != nil {
			return EndpointAnswer{}, err
		}
		ips, err := answerIPs(body, 0, ep.String(), domain, recordType)
		return EndpointAnswer{IPs: ips}, err
	case endpoint.TCP:
		ips, err := r.exchange(ctx, "tcp", ep, domain, recordType)
		return EndpointAnswer{IPs: ips}, err
	}
	ips, err := r.exchange(ctx, "udp", ep, domain, recordType)
	var rcode *RcodeError
	switch {
	case err == nil, NoAddressAnswer(err), errors.As(err, &rcode):
		return EndpointAnswer{IPs: ips, OverUDP: true}, err
	case errors.Is(err, errTruncated), ep.Transport == endpoint.TCPUDP && ctx.Err() == nil:
		ips, err = r.exchange(ctx, "tcp", ep, domain, recordType)
		return EndpointAnswer{IPs: ips}, err
	}
	return EndpointAnswer{}, err
}

func NoAddressAnswer(err error) bool {
	var nx *NXDomainError
	var nodata *NoDataError
	return errors.As(err, &nx) || errors.As(err, &nodata)
}

func (r *Resolver) exchange(ctx context.Context, network string, ep endpoint.Endpoint, domain, recordType string) ([]string, error) {
	ctx, cancel := context.WithTimeout(ctx, r.timeout())
	defer cancel()

	conn, err := Dialer(r.Mark, r.timeout(), 0).DialContext(ctx, network, ep.Addr.String())
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Unix(1, 0)) })
	defer stop()

	id := uint16(rand.N(65535)) + 1
	query := dns.BuildQuery(domain, id, qtypeForRecord(recordType))
	var resp []byte
	if network == "tcp" {
		resp, err = tcpRoundTrip(conn, query)
	} else {
		resp, err = udpRoundTrip(conn, query)
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, ctxErr
		}
		return nil, err
	}
	return answerIPs(resp, id, ep.String(), domain, recordType)
}

func udpRoundTrip(conn net.Conn, query []byte) ([]byte, error) {
	if _, err := conn.Write(query); err != nil {
		return nil, err
	}
	buf := make([]byte, 65535)
	for {
		n, err := conn.Read(buf)
		if err != nil {
			return nil, err
		}
		if n >= 12 && buf[0] == query[0] && buf[1] == query[1] {
			return buf[:n], nil
		}
	}
}

func tcpRoundTrip(conn net.Conn, query []byte) ([]byte, error) {
	framed := make([]byte, 2+len(query))
	binary.BigEndian.PutUint16(framed, uint16(len(query)))
	copy(framed[2:], query)
	if _, err := conn.Write(framed); err != nil {
		return nil, err
	}
	var size [2]byte
	if _, err := io.ReadFull(conn, size[:]); err != nil {
		return nil, err
	}
	resp := make([]byte, binary.BigEndian.Uint16(size[:]))
	if _, err := io.ReadFull(conn, resp); err != nil {
		return nil, err
	}
	return resp, nil
}

func answerIPs(resp []byte, id uint16, server, domain, recordType string) ([]string, error) {
	if len(resp) < 12 || resp[2]&0x80 == 0 {
		return nil, fmt.Errorf("%s sent something that is not a DNS answer", server)
	}
	if binary.BigEndian.Uint16(resp) != id {
		return nil, fmt.Errorf("%s answered another query", server)
	}
	if resp[2]&0x02 != 0 {
		return nil, errTruncated
	}
	switch rcode := resp[3] & 0x0F; rcode {
	case dns.RcodeNoError:
	case dns.RcodeNXDomain:
		return nil, &NXDomainError{Domain: domain, Server: server}
	default:
		return nil, &RcodeError{Server: server, Rcode: rcode}
	}
	ips := filterIPStrings(dns.ParseResponseIPs(resp), recordType)
	if len(ips) == 0 {
		return nil, &NoDataError{Domain: domain, Server: server}
	}
	return ips, nil
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
