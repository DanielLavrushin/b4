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

const (
	HeadStart     = 300 * time.Millisecond
	askGrace      = time.Second
	maxSourceAsks = 64
)

var ErrSourceCooling = errors.New("the resolver is still unreachable")

var ErrShortAnswer = errors.New("the resolver sent a reply too short to be a DNS answer")

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
		if !SourceCooling(srv.Source) {
			return false
		}
	}
	return true
}

type answerClass int

const (
	answerFailed answerClass = iota
	answerRetryable
	answerUsable
)

type asked[T any] struct {
	val       T
	srv       Server
	err       error
	class     answerClass
	cancelled bool
}

func judgeResponse(resp []byte, err error) answerClass {
	if err != nil {
		return answerFailed
	}
	rcode, ok := ResponseRcode(resp)
	switch {
	case !ok:
		return answerFailed
	case rcode == RcodeNoError || rcode == RcodeNXDomain:
		return answerUsable
	}
	return answerRetryable
}

func judgeLookup(_ []net.IP, err error) answerClass {
	switch {
	case errors.Is(err, ErrTransientAnswer):
		return answerRetryable
	case err == nil || errors.Is(err, ErrNoAddress):
		return answerUsable
	}
	return answerFailed
}

type Options struct {
	Life       context.Context
	Strict     bool
	Quiet      bool
	Background bool
	Track      func() (done func())
}

func askInOrder[T any](callCtx context.Context, servers []Server, opt Options,
	ask func(context.Context, Server) (T, error), judge func(T, error) answerClass) (T, Server, error) {

	var zero T
	if len(servers) == 0 {
		return zero, Server{}, ErrSourceCooling
	}
	lifeCtx := opt.Life
	if lifeCtx == nil || len(servers) == 1 {
		lifeCtx = callCtx
	}

	ch := make(chan asked[T], len(servers))
	launch := func(srv Server, release func()) {
		done := func() {}
		if opt.Track != nil {
			done = opt.Track()
		}
		go func() {
			defer done()
			defer release()
			sctx, cancel := context.WithTimeout(lifeCtx, srv.timeout()+askGrace)
			defer cancel()
			val, err := ask(sctx, srv)
			out := asked[T]{val: val, srv: srv, err: err, class: judge(val, err)}
			if err != nil && lifeCtx.Err() != nil {
				out.cancelled = true
			}
			ch <- out
		}()
	}

	queue, next := servers, 0
	capped := len(servers) > 1
	lastResort := opt.Strict && !opt.Background
	consult := !opt.Quiet && (capped || !lastResort)
	var held []Server
	leftOut, anyLeftOut := servers[0], false
	startNext := func(nobodyFailed bool) bool {
		for {
			for next < len(queue) {
				srv := queue[next]
				next++
				release, free := func() {}, true
				if !opt.Quiet {
					release, free = beginSourceAsk(srv.Source, consult, capped)
				}
				if !free {
					switch {
					case lastResort && consult:
						held = append(held, srv)
					case !anyLeftOut:
						leftOut, anyLeftOut = srv, true
					}
					continue
				}
				launch(srv, release)
				return true
			}
			if len(held) == 0 || nobodyFailed {
				return false
			}
			queue, next, held, consult = held, 0, nil, false
		}
	}

	if !startNext(false) {
		return zero, leftOut, ErrSourceCooling
	}
	launched, answered := 1, 0
	timer := time.NewTimer(HeadStart)
	defer timer.Stop()
	timerC := timer.C
	more := true
	advance := func(nobodyFailed bool) {
		if more && startNext(nobodyFailed) {
			launched++
			armTimer(timer, &timerC, HeadStart)
			return
		}
		more = more && len(held) > 0
		timerC = nil
	}

	var fallback *asked[T]
	var failure *asked[T]

	for answered < launched || more {
		select {
		case got := <-ch:
			answered++
			if got.cancelled {
				more = false
				timerC = nil
				continue
			}
			if !opt.Quiet {
				noteAnswer(got.srv, got.class)
			}
			switch got.class {
			case answerUsable:
				drainAnswers(ch, launched-answered, opt.Quiet)
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
			advance(false)
		case <-timerC:
			advance(true)
		case <-callCtx.Done():
			drainAnswers(ch, launched-answered, opt.Quiet)
			return zero, Server{}, callCtx.Err()
		}
	}

	switch {
	case callCtx.Err() != nil:
		return zero, Server{}, callCtx.Err()
	case failure != nil:
		return zero, failure.srv, failure.err
	case anyLeftOut:
		return zero, leftOut, ErrSourceCooling
	case fallback != nil:
		return fallback.val, fallback.srv, fallback.err
	case lifeCtx.Err() != nil:
		return zero, servers[0], lifeCtx.Err()
	}
	return zero, servers[0], ErrNoAddress
}

func drainAnswers[T any](ch <-chan asked[T], left int, quiet bool) {
	if left <= 0 {
		return
	}
	go func() {
		for i := 0; i < left; i++ {
			got := <-ch
			if got.cancelled || quiet {
				continue
			}
			noteAnswer(got.srv, got.class)
		}
	}()
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

func noteAnswer(srv Server, class answerClass) {
	if class == answerFailed {
		if NoteSourceFailure(srv.Source) {
			log.Warnf("DNS: %s failed %d times in a row, lookups leave it out for %s where another resolver of the set or a fallback can answer",
				SourceLabel(srv.Source), SourceFailuresToTrip, sourceCooldown)
		}
		return
	}
	if NoteSourceSuccess(srv.Source) {
		log.Infof("DNS: %s is answering again", SourceLabel(srv.Source))
	}
}

func AskServers(ctx context.Context, servers []Server, opt Options, ask func(context.Context, Server) ([]byte, error)) ([]byte, string, error) {
	resp, srv, err := askInOrder(ctx, servers, opt, ask, judgeResponse)
	if srv.Source == "" && len(servers) > 0 {
		srv = servers[0]
	}
	if err == nil && len(resp) < 12 {
		err = ErrShortAnswer
	}
	if err != nil {
		return nil, srv.Source, err
	}
	return resp, srv.Source, nil
}

func LookupWithFallback(ctx context.Context, lookup LookupFunc, servers []Server, opt Options, useSet bool, name string, v4, v6 bool) ([]net.IP, error) {
	if !useSet || len(servers) == 0 {
		return LookupRouter(ctx, name, v4, v6)
	}
	ips, srv, err := askInOrder(ctx, servers, opt, func(sctx context.Context, srv Server) ([]net.IP, error) {
		return lookup(sctx, srv, name, v4, v6)
	}, judgeLookup)
	if err == nil {
		return ips, nil
	}
	if errors.Is(err, ErrNoAddress) || opt.Strict || ctx.Err() != nil {
		return nil, err
	}
	if errors.Is(err, ErrSourceCooling) {
		log.Tracef("DNS: %s is cooling down after repeated failures, resolving %s through the router's resolver", SourceLabel(srv.Source), name)
	} else {
		log.Tracef("DNS: %s could not resolve %s (%v), asking the router's resolver", SourceLabel(srv.Source), name, err)
	}
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
