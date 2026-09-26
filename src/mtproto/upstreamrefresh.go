package mtproto

import (
	"context"
	"strconv"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var (
	upstreamRefreshKick = make(chan struct{}, 1)
	upstreamRefreshDCs  = RefreshDCs
	upstreamRefreshCF   = func(url string) (int, error) {
		if err := cfBalancerInst.refreshFromURL(url); err != nil {
			return 0, err
		}
		return cfBalancerInst.size(), nil
	}
)

type upstreamRefreshState struct {
	dcKey      string
	dcRetry    time.Time
	dcFailures int
	cfURL      string
	cfNext     time.Time
	cfFailures int
	cfOK       bool
}

func KickUpstreamRefresh() {
	select {
	case upstreamRefreshKick <- struct{}{}:
	default:
	}
}

func StartUpstreamRefresh(ctx context.Context, getCfg func() *config.Config) {
	go runUpstreamRefresh(ctx, getCfg)
	KickUpstreamRefresh()
}

func runUpstreamRefresh(ctx context.Context, getCfg func() *config.Config) {
	timer := time.NewTimer(cfProxyRefreshInt)
	timer.Stop()
	defer timer.Stop()
	var st upstreamRefreshState
	for {
		select {
		case <-ctx.Done():
			return
		case <-upstreamRefreshKick:
		case <-timer.C:
		}
		next := st.step(getCfg(), time.Now())
		timer.Stop()
		if next > 0 {
			timer.Reset(next)
		}
	}
}

func (st *upstreamRefreshState) step(cfg *config.Config, now time.Time) time.Duration {
	if !cfg.TelegramInUse() {
		*st = upstreamRefreshState{}
		return 0
	}
	mt := cfg.System.MTProto
	st.stepDCs(mt, now)
	st.stepCF(mt, now)
	return st.nextWake(now)
}

func (st *upstreamRefreshState) stepDCs(mt config.MTProtoConfig, now time.Time) {
	dcKey := strconv.FormatBool(mt.DCFallbackEnabled) + "|" + mt.DCFallbackURL
	changed := dcKey != st.dcKey
	if !changed && (st.dcRetry.IsZero() || now.Before(st.dcRetry)) {
		return
	}
	if changed {
		st.dcKey, st.dcFailures = dcKey, 0
	}
	st.dcRetry = time.Time{}
	err := upstreamRefreshDCs(mt.DCFallbackEnabled, mt.DCFallbackURL)
	if err == nil {
		st.dcFailures = 0
		return
	}
	st.dcFailures++
	wait := telegramCIDRRetryDelay(st.dcFailures)
	st.dcRetry = now.Add(wait)
	if st.dcFailures == 1 {
		log.Infof("MTProto DC list not refreshed, keeping the built-in addresses, next attempt in %v: %v", wait, err)
	} else {
		log.Debugf("MTProto DC list refresh failed again (%d), next attempt in %v: %v", st.dcFailures, wait, err)
	}
}

func (st *upstreamRefreshState) stepCF(mt config.MTProtoConfig, now time.Time) {
	if !mt.CFProxyEnabled {
		st.cfURL, st.cfNext, st.cfFailures, st.cfOK = "", time.Time{}, 0, false
		return
	}
	changed := mt.CFProxyURL != st.cfURL
	if !changed && now.Before(st.cfNext) {
		return
	}
	if changed {
		st.cfURL, st.cfFailures, st.cfOK = mt.CFProxyURL, 0, false
	}
	n, err := upstreamRefreshCF(mt.CFProxyURL)
	if err == nil {
		if st.cfOK {
			log.Debugf("CF proxy pool refreshed (%d domains)", n)
		} else {
			log.Infof("CF proxy pool refreshed (%d domains)", n)
		}
		st.cfFailures, st.cfOK = 0, true
		st.cfNext = now.Add(cfProxyRefreshInt)
		return
	}
	st.cfFailures++
	wait := telegramCIDRRetryDelay(st.cfFailures)
	st.cfNext = now.Add(wait)
	if st.cfFailures == 1 && !st.cfOK {
		log.Warnf("CF proxy list not refreshed, keeping the current pool, next attempt in %v: %v", wait, err)
	} else {
		log.Debugf("CF proxy list refresh failed again (%d), next attempt in %v: %v", st.cfFailures, wait, err)
	}
}

func (st *upstreamRefreshState) nextWake(now time.Time) time.Duration {
	var next time.Time
	for _, t := range []time.Time{st.dcRetry, st.cfNext} {
		if !t.IsZero() && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	if next.IsZero() {
		return 0
	}
	if d := next.Sub(now); d > 0 {
		return d
	}
	return time.Millisecond
}
