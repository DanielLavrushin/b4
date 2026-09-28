package dns

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
)

const (
	defaultLookupTimeout = 5 * time.Second
	FallbackBudget       = 5 * time.Second
)

var ErrNoAddress = errors.New("the resolver has no address for the name")

type Server struct {
	Source  string
	DoHURL  string
	UDP     net.IP
	Port    int
	Mark    int
	Timeout time.Duration
}

type LookupFunc func(ctx context.Context, srv Server, name string, v4, v6 bool) ([]net.IP, error)

func SetServer(enabled bool, target, dohURL string) (Server, bool) {
	if !enabled {
		return Server{}, false
	}
	if dohURL != "" {
		return Server{Source: dohURL, DoHURL: dohURL}, true
	}
	if ip := net.ParseIP(target); ip != nil {
		return Server{Source: target, UDP: ip}, true
	}
	return Server{}, false
}

func (s Server) String() string {
	if s.DoHURL != "" {
		return s.DoHURL
	}
	return s.UDP.String()
}

func (s Server) timeout() time.Duration {
	if s.Timeout > 0 {
		return s.Timeout
	}
	return defaultLookupTimeout
}

func (s Server) Budget() time.Duration {
	return 2*s.timeout() + FallbackBudget
}

func LookupWithFallback(ctx context.Context, lookup LookupFunc, srv Server, useSet, strict bool, name string, v4, v6 bool) ([]net.IP, error) {
	if useSet {
		if !strict && SourceUnreachable(srv.Source) {
			log.Tracef("DNS: %s is cooling down after repeated failures, resolving %s through the router's resolver", SourceLabel(srv.Source), name)
		} else {
			sctx, cancel := context.WithTimeout(ctx, srv.timeout())
			ips, err := lookup(sctx, srv, name, v4, v6)
			cancel()
			if err == nil || errors.Is(err, ErrNoAddress) {
				if NoteSourceSuccess(srv.Source) {
					log.Infof("DNS: %s is answering again", SourceLabel(srv.Source))
				}
				return ips, err
			}
			if ctx.Err() != nil {
				return nil, err
			}
			if NoteSourceFailure(srv.Source) {
				log.Warnf("DNS: %s failed %d times in a row, lookups of sets that allow a fallback skip it for %s", SourceLabel(srv.Source), SourceFailuresToTrip, sourceCooldown)
			}
			if strict {
				return nil, err
			}
			log.Tracef("DNS: %s could not resolve %s (%v), asking the router's resolver", SourceLabel(srv.Source), name, err)
		}
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(ctx, name)
	if err != nil {
		return nil, err
	}
	ips := make([]net.IP, 0, len(addrs))
	for _, a := range addrs {
		if (a.IP.To4() != nil && v4) || (a.IP.To4() == nil && v6) {
			ips = append(ips, a.IP)
		}
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("%s: %w", name, ErrNoAddress)
	}
	return ips, nil
}

func LookupIPs(ctx context.Context, srv Server, name string, v4, v6 bool) ([]net.IP, error) {
	var qtypes []uint16
	if v4 {
		qtypes = append(qtypes, rrTypeA)
	}
	if v6 {
		qtypes = append(qtypes, rrTypeAAAA)
	}
	type result struct {
		ips []net.IP
		err error
	}
	results := make([]result, len(qtypes))
	var wg sync.WaitGroup
	for i, qtype := range qtypes {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ips, err := srv.exchange(ctx, name, qtype)
			results[i] = result{ips: ips, err: err}
		}()
	}
	wg.Wait()

	var out []net.IP
	var failure error
	answered := false
	for i, r := range results {
		switch {
		case r.err == nil:
			answered = true
			for _, ip := range r.ips {
				if (ip.To4() != nil) == (qtypes[i] == rrTypeA) {
					out = append(out, ip)
				}
			}
		case errors.Is(r.err, ErrNoAddress):
			answered = true
		case failure == nil:
			failure = r.err
		}
	}
	switch {
	case len(out) > 0:
		return out, nil
	case failure != nil:
		return nil, failure
	case answered:
		return nil, fmt.Errorf("%s at %s: %w", name, srv, ErrNoAddress)
	}
	return nil, fmt.Errorf("no address family was asked for %s", name)
}

func (s Server) exchange(ctx context.Context, name string, qtype uint16) ([]net.IP, error) {
	timeout := s.timeout()
	var txid uint16
	var resp []byte
	var err error
	if s.DoHURL != "" {
		qctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()
		resp, err = ResolveDoH(qctx, lookupDoHClient(s.Mark, timeout), s.DoHURL, BuildQuery(name, txid, qtype))
	} else {
		if deadline, ok := ctx.Deadline(); ok {
			left := time.Until(deadline)
			if left <= 0 {
				return nil, ctx.Err()
			}
			timeout = min(timeout, left)
		}
		txid = uint16(rand.Uint32())
		resp, err = ResolveUpstreamContext(ctx, BuildQuery(name, txid, qtype), s.UDP, ForwardOptions{Mark: s.Mark, Timeout: timeout, Port: s.Port})
	}
	if err != nil {
		return nil, err
	}
	if len(resp) < 12 || binary.BigEndian.Uint16(resp[0:2]) != txid {
		return nil, fmt.Errorf("%s sent an answer that does not match the query for %s", s, name)
	}
	if rcode, _ := ResponseRcode(resp); rcode != 0 && rcode != 3 {
		return nil, fmt.Errorf("%s answered %s with rcode %d: %w", s, name, rcode, ErrNoAddress)
	}
	ips := ParseResponseIPs(resp)
	if len(ips) == 0 {
		return nil, ErrNoAddress
	}
	return ips, nil
}

var (
	lookupClientMu sync.Mutex
	lookupClients  = map[[2]int64]*http.Client{}
)

func lookupDoHClient(mark int, timeout time.Duration) *http.Client {
	key := [2]int64{int64(mark), int64(timeout)}
	lookupClientMu.Lock()
	defer lookupClientMu.Unlock()
	if c, ok := lookupClients[key]; ok {
		return c
	}
	c := MarkedDoHClient(mark, timeout)
	lookupClients[key] = c
	return c
}
