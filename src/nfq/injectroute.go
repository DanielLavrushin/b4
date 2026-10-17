package nfq

import (
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	injectRouteTTL      = 5 * time.Second
	injectRouteRecheck  = time.Second
	injectRouteCacheMax = 4096
	injectRouteSlotsMax = 4
	injectRouteTimeout  = 250 * time.Millisecond
)

type injectRouteKey struct {
	dst  [16]byte
	mark uint32
}

type injectRouteEntry struct {
	oif     uint32
	ok      bool
	at      time.Time
	against uint32
}

type injectRouteFlight struct {
	done  chan struct{}
	until time.Time
	oif   uint32
	ok    bool
}

var (
	rerouteCheckAll atomic.Bool

	injectRouteMu       sync.Mutex
	injectRouteCache    = make(map[injectRouteKey]injectRouteEntry)
	injectRouteInflight = make(map[injectRouteKey]*injectRouteFlight)
	injectRouteSlots    = make(chan struct{}, injectRouteSlotsMax)
	injectRouteLookup   = netlinkRouteOIF
	injectRouteWait     = injectRouteTimeout

	injectRouteConnMu sync.Mutex
	injectRouteConn   *netlink.Conn

	RouteMarksFunc func(iface string) []uint32
)

func SetRerouteCheckAll(on bool) {
	rerouteCheckAll.Store(on)
}

func (e injectRouteEntry) servesFor(outDev uint32, now time.Time) bool {
	age := now.Sub(e.at)
	if age >= injectRouteTTL {
		return false
	}
	return !e.ok || e.oif == outDev || (e.against == outDev && age < injectRouteRecheck)
}

func injectRouteOIF(dst net.IP, mark, outDev uint32) (uint32, bool) {
	var key injectRouteKey
	copy(key.dst[:], dst.To16())
	key.mark = mark

	injectRouteMu.Lock()
	if e, had := injectRouteCache[key]; had && e.servesFor(outDev, time.Now()) {
		injectRouteMu.Unlock()
		return e.oif, e.ok
	}
	f, running := injectRouteInflight[key]
	if !running {
		select {
		case injectRouteSlots <- struct{}{}:
		default:
			injectRouteMu.Unlock()
			return 0, false
		}
		f = &injectRouteFlight{done: make(chan struct{}), until: time.Now().Add(injectRouteWait)}
		injectRouteInflight[key] = f
		go injectRouteResolve(key, f, append(net.IP(nil), dst...), mark, outDev)
	}
	injectRouteMu.Unlock()

	left := time.Until(f.until)
	if left <= 0 {
		select {
		case <-f.done:
			return f.oif, f.ok
		default:
			return 0, false
		}
	}
	t := time.NewTimer(left)
	defer t.Stop()
	select {
	case <-f.done:
		return f.oif, f.ok
	case <-t.C:
		return 0, false
	}
}

func injectRouteResolve(key injectRouteKey, f *injectRouteFlight, dst net.IP, mark, outDev uint32) {
	defer func() { <-injectRouteSlots }()
	oif, ok := injectRouteLookup(dst, mark)
	injectRouteMu.Lock()
	if len(injectRouteCache) >= injectRouteCacheMax {
		injectRouteCache = make(map[injectRouteKey]injectRouteEntry)
	}
	injectRouteCache[key] = injectRouteEntry{oif: oif, ok: ok, at: time.Now(), against: outDev}
	delete(injectRouteInflight, key)
	injectRouteMu.Unlock()
	f.oif, f.ok = oif, ok
	close(f.done)
}

func injectRouteRequest(dst net.IP, mark uint32) ([]byte, bool) {
	family, addr, bits := uint8(unix.AF_INET), dst.To4(), uint8(32)
	if addr == nil {
		family, addr, bits = uint8(unix.AF_INET6), dst.To16(), uint8(128)
	}
	if addr == nil {
		return nil, false
	}

	ae := netlink.NewAttributeEncoder()
	ae.Bytes(unix.RTA_DST, addr)
	ae.Uint32(unix.RTA_MARK, mark)
	attrs, err := ae.Encode()
	if err != nil {
		return nil, false
	}

	req := make([]byte, unix.SizeofRtMsg, unix.SizeofRtMsg+len(attrs))
	req[0] = family
	req[1] = bits
	return append(req, attrs...), true
}

func routeReplyOIF(msgs []netlink.Message) (uint32, bool) {
	for _, m := range msgs {
		if len(m.Data) < unix.SizeofRtMsg {
			continue
		}
		ad, err := netlink.NewAttributeDecoder(m.Data[unix.SizeofRtMsg:])
		if err != nil {
			continue
		}
		var oif uint32
		for ad.Next() {
			if ad.Type() == unix.RTA_OIF {
				oif = ad.Uint32()
			}
		}
		if ad.Err() == nil && oif != 0 {
			return oif, true
		}
	}
	return 0, false
}

func routeRefused(errno unix.Errno) bool {
	return errno == unix.ENETUNREACH || errno == unix.EHOSTUNREACH || errno == unix.EACCES || errno == unix.EINVAL
}

func netlinkRouteOIF(dst net.IP, mark uint32) (uint32, bool) {
	req, ok := injectRouteRequest(dst, mark)
	if !ok {
		return 0, false
	}

	injectRouteConnMu.Lock()
	defer injectRouteConnMu.Unlock()
	if injectRouteConn == nil {
		c, err := netlink.Dial(unix.NETLINK_ROUTE, nil)
		if err != nil {
			return 0, false
		}
		injectRouteConn = c
	}
	c := injectRouteConn
	if err := c.SetDeadline(time.Now().Add(injectRouteTimeout)); err != nil {
		_ = c.Close()
		injectRouteConn = nil
		return 0, false
	}
	msgs, err := c.Execute(netlink.Message{
		Header: netlink.Header{Type: unix.RTM_GETROUTE, Flags: netlink.Request},
		Data:   req,
	})
	if err != nil {
		var errno unix.Errno
		if !errors.As(err, &errno) {
			_ = c.Close()
			injectRouteConn = nil
			return 0, false
		}
		return 0, dst.To4() != nil && routeRefused(errno)
	}
	return routeReplyOIF(msgs)
}

func setRoutesWith(iface string, bits uint32) bool {
	if RouteMarksFunc == nil || bits == 0 {
		return false
	}
	for _, m := range RouteMarksFunc(iface) {
		if m&config.PerSetRouteMarkBits == bits {
			return true
		}
	}
	return false
}

func (w *Worker) reroutedEgress(vc *verdictCtx, pkt *pktInfo, cfg *config.Config) string {
	if vc.rerouteChecked {
		return vc.rerouted
	}
	vc.rerouteChecked = true
	if vc.foreignMark == 0 || vc.outDev == 0 || cfg.Queue.IsDiscovery {
		return ""
	}
	mark := uint32(cfg.MainInjectedMark())
	if (!rerouteCheckAll.Load() || cfg.System.Tables.SkipSetup) && vc.foreignMark&mark != mark {
		return ""
	}
	if vc.foreignMark == uint32(cfg.DiscoveryFlowMark()) || vc.foreignMark == uint32(cfg.DiscoveryInjectedMark()) {
		return ""
	}
	name := getIfaceName(vc.outDev)
	if name == "" {
		return ""
	}
	lookup := mark
	if bits := vc.foreignMark & config.PerSetRouteMarkBits; setRoutesWith(name, bits) {
		lookup = mark&^config.PerSetRouteMarkBits | bits
	}
	oif, ok := injectRouteOIF(pkt.dst, lookup, vc.outDev)
	if !ok || oif == vc.outDev {
		return ""
	}
	if oif == 0 {
		log.Tracef("NFQ: %s with mark 0x%x leaves by %s, b4's own packets have no route there: handing the connection off", pkt.dstStr, vc.foreignMark, name)
	} else {
		log.Tracef("NFQ: %s with mark 0x%x leaves by %s, b4's own packets would leave by %s: handing the connection off", pkt.dstStr, vc.foreignMark, name, getIfaceName(oif))
	}
	vc.rerouted = name
	return name
}

func (w *Worker) routedSetOverridden(vc *verdictCtx, pkt *pktInfo, cfg *config.Config, set *config.SetConfig) string {
	if !set.RoutesToInterface() || vc.foreignMark == 0 || vc.outDev == 0 || cfg.Queue.IsDiscovery {
		return ""
	}
	mark := uint32(cfg.MainInjectedMark())
	if vc.foreignMark&mark != mark || vc.foreignMark == uint32(cfg.DiscoveryFlowMark()) || vc.foreignMark == uint32(cfg.DiscoveryInjectedMark()) {
		return ""
	}
	if setRoutesWith(set.Routing.EgressInterface, vc.foreignMark&config.PerSetRouteMarkBits) {
		return ""
	}
	name := getIfaceName(vc.outDev)
	if name == "" {
		return ""
	}
	log.Tracef("NFQ: %s with mark 0x%x leaves by %s without the route mark of set %s, whose own packets b4 routes through %s: handing the connection off", pkt.dstStr, vc.foreignMark, name, set.Name, set.Routing.EgressInterface)
	vc.rerouted = name
	return name
}

func (w *Worker) handsOffEgress(vc *verdictCtx, pkt *pktInfo, cfg *config.Config, set *config.SetConfig) bool {
	if set.Routing.Enabled {
		return w.routedSetOverridden(vc, pkt, cfg, set) != ""
	}
	return vc.tunnel != "" || w.reroutedEgress(vc, pkt, cfg) != ""
}

func (vc *verdictCtx) handOffReason() string {
	if vc.tunnel != "" {
		return "tunnel->" + vc.tunnel
	}
	if vc.rerouted != "" {
		return "reroute->" + vc.rerouted
	}
	return ""
}
