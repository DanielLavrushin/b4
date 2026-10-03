package main

import (
	"fmt"
	"strconv"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/metrics"
	"github.com/daniellavrushin/b4/nfq"
	"github.com/daniellavrushin/b4/sock"
	"github.com/daniellavrushin/b4/tables"
	"github.com/daniellavrushin/b4/tproxy"
)

type dashboardSources struct {
	config    func() *config.Config
	packets   func() uint64
	upstreams func() []tproxy.UpstreamHealth
	open      func() map[string]int64
	rules     func() tables.RulesStatus
	replaced  func() bool
	overload  func() (uint64, uint64, uint64)
}

func wireDashboard(mc *metrics.MetricsCollector, src dashboardSources) {
	var sets func() ([]metrics.SetMeta, int)
	if load := src.config; load != nil {
		sets = func() ([]metrics.SetMeta, int) { return setMetas(load()) }
	}
	var upstreams func() []metrics.UpstreamAttention
	if health := src.upstreams; health != nil {
		upstreams = func() []metrics.UpstreamAttention { return upstreamAttention(mc, health()) }
	}
	var rules func() metrics.RulesInfo
	if status := src.rules; status != nil {
		rules = func() metrics.RulesInfo { return rulesInfo(mc, status()) }
	}
	metrics.SetSetsProvider(sets)
	metrics.SetUpstreamProvider(upstreams)
	metrics.SetRulesProvider(rules)
	metrics.SetPacketCounterProvider(src.packets)
	metrics.SetProxyOpenProvider(src.open)
	metrics.SetBinaryReplacedProvider(src.replaced)
	metrics.SetOverloadProvider(src.overload)
}

func engineOverload() (injectSkipped, rawSendDropped, queueOverflow uint64) {
	return nfq.InjectOverloaded(), sock.SendDropped(), nfq.QueueOverflows()
}

func setKind(set *config.SetConfig) string {
	switch {
	case !set.Routing.Enabled:
		return metrics.SetKindBypass
	case config.RoutingIsBlock(set.Routing.Mode):
		return metrics.SetKindBlock
	case config.RoutingUsesTProxy(set.Routing.Mode):
		return metrics.SetKindProxy
	}
	return metrics.SetKindRoute
}

func setMetas(cfg *config.Config) ([]metrics.SetMeta, int) {
	if cfg == nil {
		return nil, 0
	}
	metas := make([]metrics.SetMeta, 0, len(cfg.Sets))
	disabled := 0
	for _, set := range cfg.Sets {
		if set == nil {
			continue
		}
		if !set.Enabled {
			disabled++
			continue
		}
		metas = append(metas, metrics.SetMeta{
			ID:      set.Id,
			Name:    set.Name,
			Kind:    setKind(set),
			Watched: set.Discovery.Watchdog,
		})
	}
	return metas, disabled
}

func upstreamAttention(mc *metrics.MetricsCollector, health []tproxy.UpstreamHealth) []metrics.UpstreamAttention {
	var out []metrics.UpstreamAttention
	for _, h := range health {
		if h.ConsecutiveFailures <= 0 {
			continue
		}
		out = append(out, metrics.UpstreamAttention{
			SetID:       h.SetID,
			SetName:     h.SetName,
			Upstream:    h.Upstream,
			FailOpen:    h.FailOpen,
			Failures:    h.ConsecutiveFailures,
			LastError:   h.LastError,
			LastFailure: mc.WallMillis(h.LastFailure),
		})
	}
	return out
}

func rulesInfo(mc *metrics.MetricsCollector, st tables.RulesStatus) metrics.RulesInfo {
	info := metrics.RulesInfo{
		Monitored:   st.Monitored,
		LastCheck:   mc.WallMillis(st.LastCheck),
		Restores:    st.Restores,
		LastRestore: mc.WallMillis(st.LastRestore),
	}
	if st.Monitored {
		info.IntervalS = int(st.Interval / time.Second)
	}
	return info
}

func noteStarted(mc *metrics.MetricsCollector, version string, info metrics.EngineInfo) {
	mc.Event(metrics.LevelInfo, metrics.EventStarted,
		map[string]string{"version": version, "engine": info.Mode, "threads": strconv.Itoa(info.Threads)},
		fmt.Sprintf("B4 %s is fully operational (%s, %d threads)", version, engineLabel(info.Mode), info.Threads))
}
