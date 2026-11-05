package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/daniellavrushin/b4/log"
)

func isDoHURL(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(raw, "https://") || strings.HasPrefix(raw, "http://")
}

func SetServers(enabled bool, target, dohURL string, upstreams []string) ([]Server, bool) {
	if !enabled {
		return nil, false
	}
	servers := make([]Server, 0, 2+len(upstreams))
	seen := make(map[string]struct{}, 2+len(upstreams))
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		var srv Server
		switch {
		case isDoHURL(raw):
			if strings.TrimSpace(raw[len("https://"):]) == "" {
				return
			}
			srv = Server{Source: raw, DoHURL: raw}
		default:
			ip := net.ParseIP(raw)
			if ip == nil {
				return
			}
			srv = Server{Source: ip.String(), UDP: ip}
		}
		key := srv.String()
		if _, dup := seen[key]; dup {
			return
		}
		seen[key] = struct{}{}
		servers = append(servers, srv)
	}
	add(dohURL)
	add(target)
	for _, raw := range upstreams {
		add(raw)
	}
	return servers, len(servers) > 0
}

func LookupAny(ctx context.Context, lookup LookupFunc, servers []Server, useSet, strict bool, name string, v4, v6 bool) ([]net.IP, error) {
	if !useSet || len(servers) == 0 {
		return lookupRouter(ctx, name, v4, v6)
	}

	candidates := make([]Server, 0, len(servers))
	for _, srv := range servers {
		if SourceUnreachable(srv.Source) {
			continue
		}
		candidates = append(candidates, srv)
	}
	if len(candidates) == 0 {
		candidates = servers
	}

	type result struct {
		ips []net.IP
		err error
		srv Server
	}
	ch := make(chan result, len(candidates))
	for _, srv := range candidates {
		srv := srv
		go func() {
			sctx, cancel := context.WithTimeout(ctx, srv.Budget())
			defer cancel()
			ips, err := lookup(sctx, srv, name, v4, v6)
			ch <- result{ips: ips, err: err, srv: srv}
		}()
	}

	var firstErr error
	var firstErrSrv Server
	for range candidates {
		r := <-ch
		switch {
		case r.err == nil && len(r.ips) > 0:
			if NoteSourceSuccess(r.srv.Source) {
				log.Infof("DNS: %s is answering again", SourceLabel(r.srv.Source))
			}
			return r.ips, nil
		case errors.Is(r.err, ErrNoAddress):
			return nil, r.err
		default:
			if firstErr == nil {
				firstErr, firstErrSrv = r.err, r.srv
			}
			if NoteSourceFailure(r.srv.Source) {
				log.Warnf("DNS: %s failed %d times in a row, lookups of sets that allow a fallback skip it for %s", SourceLabel(r.srv.Source), SourceFailuresToTrip, sourceCooldown)
			}
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	if firstErr == nil {
		firstErr = fmt.Errorf("%s: %w", name, ErrNoAddress)
	}
	if strict {
		return nil, firstErr
	}
	log.Tracef("DNS: %s could not resolve %s (%v), asking the router's resolver", SourceLabel(firstErrSrv.Source), name, firstErr)
	return lookupRouter(ctx, name, v4, v6)
}

func lookupRouter(ctx context.Context, name string, v4, v6 bool) ([]net.IP, error) {
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
