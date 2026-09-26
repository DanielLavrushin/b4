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
	dcKey  string
	cfURL  string
	cfNext time.Time
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
	dcKey := strconv.FormatBool(mt.DCFallbackEnabled) + "|" + mt.DCFallbackURL
	if dcKey != st.dcKey {
		st.dcKey = dcKey
		if err := upstreamRefreshDCs(mt.DCFallbackEnabled, mt.DCFallbackURL); err != nil {
			log.Infof("MTProto DC list not refreshed, keeping the built-in addresses: %v", err)
		}
	}
	if !mt.CFProxyEnabled {
		st.cfURL, st.cfNext = "", time.Time{}
		return 0
	}
	if mt.CFProxyURL != st.cfURL || !now.Before(st.cfNext) {
		first := mt.CFProxyURL != st.cfURL
		st.cfURL, st.cfNext = mt.CFProxyURL, now.Add(cfProxyRefreshInt)
		n, err := upstreamRefreshCF(mt.CFProxyURL)
		switch {
		case err != nil && first:
			log.Warnf("CF proxy list not refreshed, keeping the current pool: %v", err)
		case err != nil:
			log.Debugf("CF proxy refresh failed: %v", err)
		case first:
			log.Infof("CF proxy pool refreshed (%d domains)", n)
		default:
			log.Debugf("CF proxy pool refreshed (%d domains)", n)
		}
	}
	return st.cfNext.Sub(now)
}
