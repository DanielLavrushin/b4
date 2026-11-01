package nfq

import (
	"net"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/sni"
)

const loopbackIface = "lo"

var (
	scopeIfaceTTL     = 5 * time.Second
	scopeIfaceByIndex = net.InterfaceByIndex
	scopeIfaceNames   sync.Map
)

type scopeIfaceEntry struct {
	name string
	at   time.Time
}

type ingressScope struct {
	matcher      *sni.SuffixSet
	client       net.IP
	mac          string
	dev          uint32
	router       bool
	unattributed bool
	uplink       bool
	resolved     bool
	name         string
}

func newIngressScope(matcher *sni.SuffixSet, client net.IP, mac string, dev uint32, router bool) *ingressScope {
	return &ingressScope{matcher: matcher, client: client, mac: mac, dev: dev, router: router, resolved: router}
}

func (sc *ingressScope) admits(set *config.SetConfig) bool {
	if set == nil || !set.BlockRoutingScoped() {
		return true
	}
	sc.resolve()
	return !sc.router && (sc.name == "" || set.ListsSourceInterface(sc.name))
}

func (sc *ingressScope) filter() sni.SetFilter {
	if sc == nil || !sc.matcher.ScopesBlockSets() {
		return nil
	}
	return sc.admits
}

func (sc *ingressScope) learns(host string) bool {
	return sc.learnsTLS(host, 0, 0)
}

func (sc *ingressScope) learnsTLS(host string, tlsVersion uint16, ipVersion uint8) bool {
	return sc == nil || !sc.matcher.ScopedBlockSetWins(host, sc.mac, tlsVersion, ipVersion)
}

func (sc *ingressScope) orSnapshot(matcher *sni.SuffixSet) *ingressScope {
	if sc != nil {
		return sc
	}
	return &ingressScope{matcher: matcher}
}

func (sc *ingressScope) settled() *ingressScope {
	if sc == nil {
		return nil
	}
	sc.resolve()
	return &ingressScope{resolved: true, router: sc.router, name: sc.name}
}

func scopeFrom(from []*ingressScope) *ingressScope {
	if len(from) == 0 {
		return nil
	}
	return from[0]
}

func (w *Worker) matcherIn(sc *ingressScope) *sni.SuffixSet {
	if sc != nil && sc.matcher != nil {
		return sc.matcher
	}
	return w.getMatcher()
}

func (sc *ingressScope) resolve() {
	if sc.resolved {
		return
	}
	sc.resolved = true
	if sc.uplink {
		return
	}
	if sc.client != nil && hostAddress(sc.client) {
		sc.router = !sc.unattributed
		return
	}
	name := scopeIfaceName(sc.dev)
	if name == "" && sc.client != nil {
		if oif, ok := sourceRouteOIF(sc.client); ok {
			name = scopeIfaceName(oif)
		}
	}
	if name == loopbackIface {
		sc.router = !sc.unattributed
		return
	}
	sc.name = name
}

func scopeIfaceName(idx uint32) string {
	if idx == 0 {
		return ""
	}
	now := time.Now()
	if v, ok := scopeIfaceNames.Load(idx); ok {
		if e := v.(scopeIfaceEntry); now.Sub(e.at) < scopeIfaceTTL {
			return e.name
		}
	}
	var name string
	if iface, err := scopeIfaceByIndex(int(idx)); err == nil {
		name = iface.Name
		if cached, ok := ifaceCache.Load(idx); !ok || cached != name {
			ifaceCache.Store(idx, name)
		}
	} else {
		name = getIfaceName(idx)
	}
	scopeIfaceNames.Store(idx, scopeIfaceEntry{name: name, at: now})
	return name
}

func (vc *verdictCtx) routerOriginated() bool {
	return vc.localSocket || (vc.hooked && vc.hook == nfHookLocalOut)
}

func (w *Worker) packetScope(vc *verdictCtx, pkt *pktInfo, matcher *sni.SuffixSet) *ingressScope {
	if !vc.scoped {
		vc.scoped = true
		if matcher.ScopesBlockSets() {
			vc.scope = newIngressScope(matcher, pkt.src, pkt.srcMac, vc.inDev, vc.routerOriginated())
			vc.scope.unattributed = w.sourceUnattributed(pkt)
			vc.scope.uplink = pkt.srcUnattributed
			vc.keep = vc.scope.admits
		}
	}
	return vc.scope
}

func (w *Worker) blockScope(vc *verdictCtx, pkt *pktInfo, matcher *sni.SuffixSet) sni.SetFilter {
	w.packetScope(vc, pkt, matcher)
	return vc.keep
}

func (w *Worker) sourceUnattributed(pkt *pktInfo) bool {
	return w.srcResolver == nil && !w.tunLocalOnly.Load() && pkt.ver == IPv4 && (pkt.proto == 6 || pkt.proto == 17) && w.getConfig().Queue.Mode == "tun"
}

func (w *Worker) clientScope(client net.IP) *ingressScope {
	if client == nil {
		return nil
	}
	return clientScopeIn(w.getMatcher(), client, w.getMacByIp(client.String()))
}

func clientScopeIn(matcher *sni.SuffixSet, client net.IP, mac string) *ingressScope {
	if client == nil || !matcher.ScopesBlockSets() {
		return nil
	}
	return newIngressScope(matcher, client, mac, 0, false)
}
