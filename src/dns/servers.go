package dns

import (
	"context"
	"errors"
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

func ServersBudget(servers []Server) time.Duration {
	if len(servers) == 0 {
		return FallbackBudget
	}
	budget := servers[0].Budget()
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

type result[T any] struct {
	val T
	srv Server
	err error
	j   judgement
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
		noteAnswer(srv, judge(val, err), err)
		if err != nil {
			return zero, srv, err
		}
		return val, srv, nil
	}

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()
	ch := make(chan result[T], len(candidates))
	runStart := time.Now()
	pendingDoH, lastDoH := 0, -1
	for i, srv := range candidates {
		if srv.DoHURL != "" {
			pendingDoH++
			lastDoH = i
		}
		go func(i int, srv Server) {
			if i > 0 {
				timer := time.NewTimer(time.Duration(i) * headStart)
				select {
				case <-timer.C:
				case <-runCtx.Done():
					timer.Stop()
					return
				}
			}
			sctx, cancel := context.WithTimeout(runCtx, srv.timeout())
			defer cancel()
			val, err := ask(sctx, srv)
			if runCtx.Err() != nil {
				return
			}
			ch <- result[T]{val: val, srv: srv, err: err, j: judge(val, err)}
		}(i, srv)
	}

	var held *result[T]
	var fallback *result[T]
	var failure error
	var graceTimer *time.Timer
	var graceCh <-chan time.Time
	drainLeft := len(candidates)

	for answered := 0; answered < len(candidates); answered++ {
		var got result[T]
		select {
		case got = <-ch:
			drainLeft--
			if got.srv.DoHURL != "" {
				pendingDoH--
			}
			noteAnswer(got.srv, got.j, got.err)
			switch got.j.class {
			case answerGood, answerNoData:
				if pendingDoH == 0 {
					return stopInOrder(runCtx, ch, drainLeft, got.val, got.srv, cancelRun)
				}
				held = better(held, &got)
				if graceCh == nil {
					wait := time.Until(runStart.Add(time.Duration(lastDoH)*headStart + headStart))
					if wait <= 0 {
						return stopInOrder(runCtx, ch, drainLeft, held.val, held.srv, cancelRun)
					}
					graceTimer = time.NewTimer(wait)
					graceCh = graceTimer.C
				}
			case answerRetryable:
				if fallback == nil {
					fallback = &got
				}
			default:
				if failure == nil {
					failure = got.err
				}
			}
		case <-graceCh:
			if held != nil {
				return stopInOrder(runCtx, ch, drainLeft, held.val, held.srv, cancelRun)
			}
			graceCh = nil
		case <-ctx.Done():
			if graceTimer != nil {
				graceTimer.Stop()
			}
			return zero, Server{}, ctx.Err()
		}
	}
	if graceTimer != nil {
		graceTimer.Stop()
	}
	if held != nil {
		return held.val, held.srv, nil
	}
	if fallback != nil {
		return fallback.val, fallback.srv, fallback.err
	}
	if failure == nil {
		failure = ErrNoAddress
	}
	return zero, Server{}, failure
}

func better[T any](held, got *result[T]) *result[T] {
	if held == nil || got.j.class > held.j.class {
		return got
	}
	return held
}

func stopInOrder[T any](runCtx context.Context, ch <-chan result[T], left int, val T, srv Server, cancel context.CancelFunc) (T, Server, error) {
	cancel()
	go func() {
		for i := 0; i < left; i++ {
			got, ok := <-ch
			if !ok {
				return
			}
			noteAnswer(got.srv, got.j, got.err)
		}
	}()
	return val, srv, nil
}

func noteAnswer(srv Server, j judgement, err error) {
	switch {
	case j.class == answerFailed:
		if NoteSourceFailure(srv.Source) {
			log.Warnf("DNS: %s failed %d times in a row, lookups of sets that allow a fallback skip it for %s",
				SourceLabel(srv.Source), SourceFailuresToTrip, sourceCooldown)
		}
	case err == nil || j.class != answerFailed:
		if NoteSourceSuccess(srv.Source) {
			log.Infof("DNS: %s is answering again", SourceLabel(srv.Source))
		}
	}
}

func AskServers(ctx context.Context, servers []Server, strict bool, ask func(context.Context, Server) ([]byte, error)) ([]byte, string, error) {
	resp, srv, err := askInOrder(ctx, servers, strict, HeadStart, ask, judgeResponse)
	if err != nil {
		return nil, srv.Source, err
	}
	return resp, srv.Source, nil
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
	if errors.Is(err, ErrNoAddress) || errors.Is(err, ErrTransientAnswer) || strict {
		return nil, err
	}
	if ctx.Err() != nil {
		return nil, err
	}
	log.Tracef("DNS: %s could not resolve %s (%v), asking the router's resolver", SourceLabel(srv.Source), name, err)
	return LookupRouter(ctx, name, v4, v6)
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
		return nil, ErrNoAddress
	}
	return ips, nil
}
