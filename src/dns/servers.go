package dns

import (
	"context"
	"errors"
	"fmt"
	"net"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

func SetServers(enabled bool, target, dohURL string, upstreams []string) ([]Server, bool) {
	if !enabled {
		return nil, false
	}
	cfg := config.DNSConfig{Enabled: true, TargetDNS: target, DoHURL: dohURL, Upstreams: upstreams}
	list := cfg.UpstreamList()
	servers := make([]Server, 0, len(list))
	for _, raw := range list {
		if config.IsDoHUpstream(raw) {
			servers = append(servers, Server{Source: raw, DoHURL: raw})
			continue
		}
		if ip := net.ParseIP(raw); ip != nil {
			servers = append(servers, Server{Source: ip.String(), UDP: ip})
		}
	}
	return servers, len(servers) > 0
}

type lookupResult struct {
	ips []net.IP
	err error
	srv Server
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
		if !strict {
			log.Tracef("DNS: every resolver of the set is cooling down, asking the router's resolver for %s", name)
			return lookupRouter(ctx, name, v4, v6)
		}
		candidates = servers
	}

	ch := make(chan lookupResult, len(candidates))
	for _, srv := range candidates {
		srv := srv
		go func() {
			sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), srv.timeout())
			defer cancel()
			ips, err := lookup(sctx, srv, name, v4, v6)
			ch <- lookupResult{ips: ips, err: err, srv: srv}
		}()
	}

	var transientErr error
	var firstErr error
	var firstErrSrv Server
	answered := 0
	for answered < len(candidates) {
		var r lookupResult
		select {
		case r = <-ch:
		case <-ctx.Done():
			recordLateAnswers(ch, len(candidates)-answered, false)
			return nil, ctx.Err()
		}
		answered++
		switch {
		case r.err == nil && len(r.ips) > 0:
			noteLookupOutcome(r, true)
			recordLateAnswers(ch, len(candidates)-answered, true)
			return r.ips, nil
		case errors.Is(r.err, ErrTransientAnswer):
			if transientErr == nil {
				transientErr = r.err
			}
			noteLookupOutcome(r, true)
		case errors.Is(r.err, ErrNoAddress):
			noteLookupOutcome(r, true)
			recordLateAnswers(ch, len(candidates)-answered, true)
			return nil, r.err
		default:
			if firstErr == nil {
				firstErr, firstErrSrv = r.err, r.srv
			}
			noteLookupOutcome(r, true)
		}
	}

	if transientErr != nil {
		return nil, transientErr
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

func noteLookupOutcome(r lookupResult, countTimeouts bool) {
	switch {
	case r.err == nil && len(r.ips) > 0:
		if NoteSourceSuccess(r.srv.Source) {
			log.Infof("DNS: %s is answering again", SourceLabel(r.srv.Source))
		}
	case r.err == nil, errors.Is(r.err, ErrNoAddress):
		if NoteSourceSuccess(r.srv.Source) {
			log.Infof("DNS: %s is answering again", SourceLabel(r.srv.Source))
		}
	case !countTimeouts && (errors.Is(r.err, context.Canceled) || errors.Is(r.err, context.DeadlineExceeded)):
	default:
		if NoteSourceFailure(r.srv.Source) {
			log.Warnf("DNS: %s failed %d times in a row, lookups of sets that allow a fallback skip it for %s", SourceLabel(r.srv.Source), SourceFailuresToTrip, sourceCooldown)
		}
	}
}

func recordLateAnswers(ch <-chan lookupResult, pending int, countTimeouts bool) {
	if pending <= 0 {
		return
	}
	go func() {
		for i := 0; i < pending; i++ {
			noteLookupOutcome(<-ch, countTimeouts)
		}
	}()
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
