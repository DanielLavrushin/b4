package nfq

import (
	"net"

	"github.com/daniellavrushin/b4/config"
)

var DSCPLearnFunc func(cfg *config.Config, set *config.SetConfig, ips []net.IP, fromTLS bool) []<-chan struct{}

var DSCPLearnAsyncFunc func(cfg *config.Config, set *config.SetConfig, ips []net.IP, fromTLS bool)

func dscpLearns(cfg *config.Config, set *config.SetConfig) bool {
	return cfg != nil && set != nil && set.DSCP.Enabled && !set.Targets.DomainOnly && !cfg.Queue.IsDiscovery
}

func dscpLearnAwait(cfg *config.Config, set *config.SetConfig, ips []net.IP) []<-chan struct{} {
	if len(ips) == 0 || !dscpLearns(cfg, set) {
		return nil
	}
	if DSCPLearnFunc != nil {
		return DSCPLearnFunc(cfg, set, ips, false)
	}
	if DSCPLearnAsyncFunc != nil {
		DSCPLearnAsyncFunc(cfg, set, ips, false)
	}
	return nil
}

func dscpLearnAsync(cfg *config.Config, set *config.SetConfig, ips []net.IP, fromTLS bool) {
	if len(ips) == 0 || !dscpLearns(cfg, set) {
		return
	}
	if DSCPLearnAsyncFunc != nil {
		DSCPLearnAsyncFunc(cfg, set, ips, fromTLS)
		return
	}
	if DSCPLearnFunc != nil {
		DSCPLearnFunc(cfg, set, ips, fromTLS)
	}
}

func joinWaits(waits, more []<-chan struct{}) []<-chan struct{} {
	switch {
	case len(more) == 0:
		return waits
	case len(waits) == 0:
		return more
	}
	return append(waits[:len(waits):len(waits)], more...)
}

func learnAnswerAwait(cfg *config.Config, set *config.SetConfig, ips []net.IP) ([]<-chan struct{}, bool) {
	if cfg == nil || set == nil || len(ips) == 0 || set.Targets.DomainOnly || cfg.Queue.IsDiscovery {
		return nil, false
	}
	var waits []<-chan struct{}
	routed := false
	if set.Routing.Enabled && routingHandleDNSAvailable() {
		waits, routed = routingHandleDNSAwait(cfg, set, ips), true
	}
	return joinWaits(waits, dscpLearnAwait(cfg, set, ips)), routed
}

func (w *Worker) learnAnswerInline(cfg *config.Config, set *config.SetConfig, ips []net.IP, cancel <-chan struct{}) {
	waits := dscpLearnAwait(cfg, set, ips)
	if set.Routing.Enabled && !set.Targets.DomainOnly && !cfg.Queue.IsDiscovery && RoutingHandleDNSFunc != nil {
		RoutingHandleDNSFunc(cfg, set, ips)
	}
	w.waitRoutesInline(waits, cancel)
}
