package dns

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

const HeadStart = 300 * time.Millisecond

var ErrAllSourcesCooling = errors.New("every resolver of the set is cooling down")

func SetServers(enabled bool, target, dohURL string, upstreams []string) ([]Server, bool) {
	if !enabled {
		return nil, false
	}
	list := (&config.DNSConfig{Enabled: true, TargetDNS: target, DoHURL: dohURL, Upstreams: upstreams}).UpstreamList()
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

func ServersBudget(servers []Server, defTimeout time.Duration) time.Duration {
	if defTimeout <= 0 {
		defTimeout = defaultLookupTimeout
	}
	timeout := defTimeout
	for _, srv := range servers {
		if t := srv.timeout(); t > timeout {
			timeout = t
		}
	}
	budget := 2*timeout + FallbackBudget
	if extra := time.Duration(len(servers)-1) * HeadStart; extra > 0 {
		budget += extra
	}
	return budget
}

func AllSourcesCooling(servers []Server) bool {
	if len(servers) == 0 {
		return true
	}
	for _, srv := range servers {
		if !SourceUnreachable(srv.Source) {
			return false
		}
	}
	return true
}

type answerClass int

const (
	answerFailed answerClass = iota
	answerRetryable
	answerNoData
	answerGood
)

type judgement struct {
	class answerClass
}

type asked[T any] struct {
	val       T
	srv       Server
	err       error
	j         judgement
	cancelled bool
}

func judgeResponse(resp []byte, err error) judgement {
	if err != nil {
		return judgement{class: answerFailed}
	}
	rcode, ok := ResponseRcode(resp)
	if !ok {
		return judgement{class: answerFailed}
	}
	switch rcode {
	case 0:
		if AnswerCount(resp) > 0 {
			return judgement{class: answerGood}
		}
		return judgement{class: answerNoData}
	case 3:
		return judgement{class: answerNoData}
	default:
		return judgement{class: answerRetryable}
	}
}

func judgeLookup(ips []net.IP, err error) judgement {
	switch {
	case err == nil && len(ips) > 0:
		return judgement{class: answerGood}
	case errors.Is(err, ErrTransientAnswer):
		return judgement{class: answerRetryable}
	case err == nil || errors.Is(err, ErrNoAddress):
		return judgement{class: answerNoData}
	default:
		return judgement{class: answerFailed}
	}
}

func askInOrder[T any](ctx context.Context, servers []Server, strict bool, headStart time.Duration,
	ask func(context.Context, Server) (T, error), judge func(T, error) judgement) (T, Server, error) {

	var zero T
	candidates := servers
	if !strict {
		live := make([]Server, 0, len(servers))
		for _, srv := range servers {
			if !SourceUnreachable(srv.Source) {
				live = append(live, srv)
			}
		}
		if len(live) == 0 {
			if len(servers) > 0 {
				return zero, servers[0], ErrAllSourcesCooling
			}
			return zero, Server{}, ErrAllSourcesCooling
		}
		candidates = live
	}

	if len(candidates) == 1 {
		srv := candidates[0]
		sctx, cancel := context.WithTimeout(ctx, srv.timeout())
		defer cancel()
		val, err := ask(sctx, srv)
		if ctx.Err() != nil {
			return zero, srv, err
		}
		noteAnswer(srv, judge(val, err))
		if err != nil {
			return zero, srv, err
		}
		return val, srv, nil
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	ch := make(chan asked[T], len(candidates))

	started := 0
	startNext := func() bool {
		if started >= len(candidates) {
			return false
		}
		srv := candidates[started]
		started++
		go func(srv Server) {
			sctx, cancel := context.WithTimeout(runCtx, srv.timeout())
			defer cancel()
			val, err := ask(sctx, srv)
			out := asked[T]{val: val, srv: srv, err: err, j: judge(val, err)}
			if runCtx.Err() != nil && ctx.Err() == nil {
				out.cancelled = true
			}
			ch <- out
		}(srv)
		return true
	}

	startNext()
	timer := time.NewTimer(headStart)
	defer timer.Stop()
	timerC := timer.C

	var fallback *asked[T]
	var failure *asked[T]

	for answered := 0; answered < len(candidates); {
		var got asked[T]
		select {
		case got = <-ch:
			answered++
			if got.cancelled {
				continue
			}
			noteAnswer(got.srv, got.j)
			switch got.j.class {
			case answerGood, answerNoData:
				cancelRun()
				return got.val, got.srv, got.err
			case answerRetryable:
				if fallback == nil {
					fallback = &got
				}
			default:
				if failure == nil {
					failure = &got
				}
			}
			if startNext() {
				armTimer(timer, &timerC, headStart)
			} else {
				timerC = nil
			}
		case <-timerC:
			if startNext() {
				armTimer(timer, &timerC, headStart)
			} else {
				timerC = nil
			}
		case <-ctx.Done():
			return zero, Server{}, ctx.Err()
		}
	}

	if fallback != nil {
		return fallback.val, fallback.srv, fallback.err
	}
	if failure != nil {
		return zero, failure.srv, failure.err
	}
	return zero, Server{}, ErrNoAddress
}

func armTimer(timer *time.Timer, ch *<-chan time.Time, d time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(d)
	*ch = timer.C
}

func noteAnswer(srv Server, j judgement) {
	if j.class == answerFailed {
		if NoteSourceFailure(srv.Source) {
			log.Warnf("DNS: %s failed %d times in a row, lookups of sets that allow a fallback skip it for %s",
				SourceLabel(srv.Source), SourceFailuresToTrip, sourceCooldown)
		}
		return
	}
	if NoteSourceSuccess(srv.Source) {
		log.Infof("DNS: %s is answering again", SourceLabel(srv.Source))
	}
}

func AskServers(ctx context.Context, servers []Server, strict bool, ask func(context.Context, Server) ([]byte, error)) ([]byte, string, error) {
	resp, srv, err := askInOrder(ctx, servers, strict, HeadStart, ask, judgeResponse)
	if srv.Source == "" && len(servers) > 0 {
		srv = servers[0]
	}
	if err != nil {
		return nil, srv.Source, err
	}
	return resp, srv.Source, nil
}

func LookupWithFallback(ctx context.Context, lookup LookupFunc, servers []Server, useSet, strict bool, name string, v4, v6 bool) ([]net.IP, error) {
	if !useSet || len(servers) == 0 {
		return LookupRouter(ctx, name, v4, v6)
	}
	ips, srv, err := askInOrder(ctx, servers, strict, HeadStart, func(sctx context.Context, srv Server) ([]net.IP, error) {
		return lookup(sctx, srv, name, v4, v6)
	}, judgeLookup)
	if err == nil {
		return ips, nil
	}
	if errors.Is(err, ErrAllSourcesCooling) {
		log.Tracef("DNS: every resolver of the set is cooling down, asking the router's resolver for %s", name)
		return LookupRouter(ctx, name, v4, v6)
	}
	if errors.Is(err, ErrNoAddress) || strict {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, err
	}
	log.Tracef("DNS: %s could not resolve %s (%v), asking the router's resolver", SourceLabel(srv.Source), name, err)
	return LookupRouter(ctx, name, v4, v6)
}

func LookupRouter(ctx context.Context, name string, v4, v6 bool) ([]net.IP, error) {
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
