package tables

import (
	"strings"
	"sync"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var routeWSReuseDiverted atomic.Pointer[map[string]struct{}]

var routeWSReuseUndivertedWarned sync.Map

func WSReuseDiverts(setID string) bool {
	if p := routeWSReuseDiverted.Load(); p != nil {
		_, ok := (*p)[setID]
		return ok
	}
	return false
}

func (st routeState) reuseDiverted() bool {
	return st.mode == config.RoutingModeWSReuse && st.diverts && st.tproxied
}

func (st routeState) withoutReuseDiversion() routeState {
	st.diverts, st.tproxied = false, false
	return st
}

func routeWarnRefusedWSReuseTProxy(set *config.SetConfig, st routeState) {
	if st.mode != config.RoutingModeWSReuse || !st.diverts || st.tproxied {
		return
	}
	log.Warnf("Routing: set '%s' reuses WSSIZE, but the firewall refused its TPROXY rule for TCP/443, so b4 does not count on its TCP/443 connections being diverted to the reuse relay: those that reach b4's packet queue get the set's own TCP strategy. b4 tries the rule again when it next rebuilds the set's rules", set.Name)
}

func routePublishWSReuseDivertsLocked() {
	diverted := make(map[string]struct{})
	for id, st := range routeRuleCache {
		if st.reuseDiverted() {
			diverted[id] = struct{}{}
		}
	}
	routeWSReuseDiverted.Store(&diverted)
}

func routeProbeWSReuseDiversion(be routeBackend, desired map[string]*config.SetConfig) bool {
	var reuse []*config.SetConfig
	for _, set := range desired {
		if set.Routing.Mode == config.RoutingModeWSReuse {
			reuse = append(reuse, set)
		}
	}
	routeWSReuseUndivertedWarned.Range(func(id, _ any) bool {
		if set, ok := desired[id.(string)]; !ok || set.Routing.Mode != config.RoutingModeWSReuse {
			routeWSReuseUndivertedWarned.Delete(id)
		}
		return true
	})
	if len(reuse) == 0 {
		return false
	}
	c := tproxyCapabilityFor(routeBackendCapKey(be), false)
	for _, set := range reuse {
		if c.usable {
			routeWSReuseUndivertedWarned.Delete(set.Id)
			continue
		}
		if _, warned := routeWSReuseUndivertedWarned.LoadOrStore(set.Id, struct{}{}); !warned {
			log.Warnf("Routing: set '%s' reuses WSSIZE, but the firewall does not support %s, so b4 does not count on its TCP/443 connections being diverted to the reuse relay: those that reach b4's packet queue get the set's own TCP strategy. %s",
				set.Name, strings.Join(c.missing, ", "), kmodMissingHint(c.missing))
		}
	}
	return c.usable
}
