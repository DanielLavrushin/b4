package tables

import (
	"sync"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type routeIfaceState struct {
	ifaces   map[string]ifaceSnapshot
	egressIP map[string]bool
}

func egressIPKey(iface, ip string) string { return iface + "|" + ip }

func routeSnapshotIfaceState(cfg *config.Config) routeIfaceState {
	st := routeIfaceState{
		ifaces:   make(map[string]ifaceSnapshot),
		egressIP: make(map[string]bool),
	}
	if cfg == nil {
		return st
	}
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled || !set.Routing.Enabled || set.Routing.EgressInterface == "" {
			continue
		}
		iface := set.Routing.EgressInterface
		if set.Routing.EgressIP != "" {
			st.egressIP[egressIPKey(iface, set.Routing.EgressIP)] = routeEgressIPOnIface(iface, set.Routing.EgressIP)
		}
		if _, ok := st.ifaces[iface]; ok {
			continue
		}
		st.ifaces[iface] = ifaceSnapshot{
			v4: routeGetIfaceAddr(iface, false),
			v6: routeGetIfaceAddr(iface, true),
		}
	}
	return st
}

func routeIfaceStateChanged(cfg *config.Config, prev routeIfaceState) bool {
	for iface, old := range prev.ifaces {
		curV4 := routeGetIfaceAddr(iface, false)
		curV6 := routeGetIfaceAddr(iface, true)
		if curV4 != old.v4 || curV6 != old.v6 {
			log.Tracef("Routing keeper: interface %s changed (v4: %q->%q, v6: %q->%q)",
				iface, old.v4, curV4, old.v6, curV6)
			return true
		}
	}
	if cfg == nil {
		return false
	}
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled || !set.Routing.Enabled || set.Routing.EgressIP == "" {
			continue
		}
		iface := set.Routing.EgressInterface
		was, tracked := prev.egressIP[egressIPKey(iface, set.Routing.EgressIP)]
		now := routeEgressIPOnIface(iface, set.Routing.EgressIP)
		if !tracked || was == now {
			continue
		}
		if now {
			log.Infof("Routing keeper: egress IP %s is back on %s; restoring the source rewrite for set '%s'", set.Routing.EgressIP, iface, set.Name)
		} else {
			log.Warnf("Routing keeper: egress IP %s left %s, taking set '%s' with it; putting the address back and rebuilding its rules", set.Routing.EgressIP, iface, set.Name)
		}
		return true
	}
	return false
}

type RoutingKeeper struct {
	mu     sync.Mutex
	state  routeIfaceState
	primed bool
}

func NewRoutingKeeper() *RoutingKeeper {
	return &RoutingKeeper{state: routeIfaceState{
		ifaces:   make(map[string]ifaceSnapshot),
		egressIP: make(map[string]bool),
	}}
}

func (k *RoutingKeeper) Resnapshot(cfg *config.Config) {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.state = routeSnapshotIfaceState(cfg)
	k.primed = true
}

func (k *RoutingKeeper) Reconcile(cfg *config.Config) {
	if cfg == nil || cfg.System.Tables.SkipSetup {
		return
	}

	k.mu.Lock()
	if !k.primed {
		k.state = routeSnapshotIfaceState(cfg)
		k.primed = true
		k.mu.Unlock()
		return
	}
	prev := k.state
	k.mu.Unlock()

	switch {
	case routeIfaceStateChanged(cfg, prev):
		log.Warnf("Routing interface change detected, resyncing routing rules...")
		RoutingForceResync(cfg)
		k.Resnapshot(cfg)
		log.Tracef("Routing rules resynced after interface change")
	case !RoutingRulesPresent(cfg):
		log.Warnf("Routing rules missing, restoring...")
		RoutingForceResync(cfg)
		k.Resnapshot(cfg)
		log.Infof("Routing rules restored successfully")
	default:
		RoutingPeriodicReResolve(cfg)
	}
}
