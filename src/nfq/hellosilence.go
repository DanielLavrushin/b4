package nfq

import (
	"encoding/binary"
	"net"
	"net/netip"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/ctnl"
	"github.com/daniellavrushin/b4/iphealth"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"golang.org/x/sys/unix"
)

const (
	maxHelloWatches   = 4096
	helloWatchTTL     = 2 * time.Minute
	maxSilenceLookups = 16
)

var lookupFlow = ctnl.Lookup

type serverSilence uint8

const (
	silenceUnknown serverSilence = iota
	silenceGone
	silenceAnswered
	silenceOpen
)

type helloWatch struct {
	key      string
	gen      uint64
	dst      net.IP
	port     uint16
	client   net.IP
	cport    uint16
	ownPort  uint16
	wire     net.IP
	tun      bool
	ver      uint8
	ihl      int
	hdr      []byte
	helloAck uint32
	host     string
	setID    string
	set      string
	sniSet   string
	ipSet    string
	mac      string
	scope    *ingressScope
	tls      uint16
	deadline time.Time
	probing  bool
	parked   bool
	closed   bool
	counted  bool

	fakesCloseEntry bool
}

func (h *helloWatch) isTo(ip net.IP, port uint16) bool {
	return h.port == port && h.dst.Equal(ip)
}

func (h *helloWatch) waitsOn(ip net.IP, port uint16) bool {
	return (h.probing || h.parked) && h.isTo(ip, port)
}

func (t *destStateTracker) Watching() bool {
	return t != nil && t.watching.Load() > 0
}

func (t *destStateTracker) WatchHello(h *helloWatch) {
	if t == nil || h == nil {
		return
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	if prev, ok := t.watches[h.key]; ok {
		if prev.setID == h.setID {
			return
		}
		h.counted = prev.counted
	} else if len(t.watches) >= maxHelloWatches {
		return
	}
	if t.watches == nil {
		t.watches = make(map[string]*helloWatch)
	}
	t.watchGen++
	h.gen = t.watchGen
	t.watches[h.key] = h
	t.watching.Store(int32(len(t.watches)))
}

func (t *destStateTracker) NoteClientPacket(key string, ack uint32, acked, closing bool, now time.Time, answeredBy func(dst net.IP, port uint16)) {
	if !t.Watching() {
		return
	}
	t.watchMu.RLock()
	h, ok := t.watches[key]
	t.watchMu.RUnlock()
	answered := ok && acked && int32(ack-h.helloAck) > 0
	if !answered && !(ok && closing) {
		return
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	if t.watches[key] != h {
		return
	}
	if answered {
		if answeredBy != nil {
			answeredBy(h.dst, h.port)
		}
		t.unwatchLocked(key)
		return
	}
	if now.Before(h.deadline) {
		t.unwatchLocked(key)
		return
	}
	h.closed = true
}

func (t *destStateTracker) Unwatch(key string) bool {
	if !t.Watching() {
		return false
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	if _, ok := t.watches[key]; !ok {
		return false
	}
	t.unwatchLocked(key)
	return true
}

func (t *destStateTracker) EndWatch(h helloWatch) bool {
	if !t.Watching() {
		return false
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	if cur, ok := t.watches[h.key]; !ok || cur.gen != h.gen {
		return false
	}
	t.unwatchLocked(h.key)
	return true
}

func (t *destStateTracker) updateWatch(h helloWatch, change func(*helloWatch)) {
	if !t.Watching() {
		return
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	if cur, ok := t.watches[h.key]; ok && cur.gen == h.gen {
		change(cur)
	}
}

func (t *destStateTracker) Unprobe(h helloWatch) {
	t.updateWatch(h, func(w *helloWatch) { w.probing = false })
}

func (t *destStateTracker) Park(h helloWatch) {
	t.updateWatch(h, func(w *helloWatch) { w.probing, w.parked = false, true })
}

func (t *destStateTracker) CountOnce(h helloWatch, count func() int) (int, bool) {
	if !t.Watching() {
		return 0, false
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	cur, ok := t.watches[h.key]
	if !ok || cur.gen != h.gen {
		return 0, false
	}
	cur.counted = true
	return count(), true
}

func (t *destStateTracker) DueSilent(now time.Time) []helloWatch {
	if !t.Watching() {
		return nil
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	var due []helloWatch
	for key, h := range t.watches {
		if now.Before(h.deadline) {
			continue
		}
		if now.Sub(h.deadline) > helloWatchTTL {
			t.unwatchLocked(key)
			continue
		}
		if h.probing || h.parked && !h.closed {
			continue
		}
		h.probing = true
		due = append(due, *h)
	}
	return due
}

func (t *destStateTracker) TakeSilent(ip net.IP, port uint16) []helloWatch {
	if !t.Watching() {
		return nil
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	var taken []helloWatch
	for key, h := range t.watches {
		if h.waitsOn(ip, port) {
			taken = append(taken, *h)
			t.unwatchLocked(key)
		}
	}
	return taken
}

func (t *destStateTracker) WatchesTo(ip net.IP, port uint16) []helloWatch {
	if !t.Watching() {
		return nil
	}
	t.watchMu.RLock()
	defer t.watchMu.RUnlock()
	var watched []helloWatch
	for _, h := range t.watches {
		if h.isTo(ip, port) {
			watched = append(watched, *h)
		}
	}
	return watched
}

func (t *destStateTracker) escalatedAway(cfg *config.Config, h helloWatch) bool {
	to, _, ok := t.GetEscalation(h.host)
	if !ok || to == h.setID {
		return false
	}
	target := cfg.GetSetById(to)
	return target != nil && target.Enabled && sni.SetMatchesSource(target, h.mac) && (h.scope == nil || h.scope.admits(target))
}

func (t *destStateTracker) unwatchLocked(key string) {
	delete(t.watches, key)
	t.watching.Store(int32(len(t.watches)))
}

func (w *Worker) watchHelloSilence(cfg *config.Config, set *config.SetConfig, pkt *pktInfo, scope *ingressScope, connKey, host, sniSet, ipSet string, sport, dport, tlsVersion uint16, deadline time.Time) {
	if w == nil || w.destState == nil || cfg == nil || cfg.Queue.IsDiscovery || !banEnforced(set) || !w.bans.on() {
		return
	}
	hdrLen := pkt.ihl + TCPHeaderMinLen
	if len(pkt.raw) < hdrLen {
		return
	}
	if flags := pkt.raw[pkt.ihl+13]; flags&0x02 != 0 || flags&0x10 == 0 {
		return
	}
	wire := pkt.raw[12:16]
	if pkt.ver == IPv6 {
		wire = pkt.raw[8:24]
	}
	w.destState.WatchHello(&helloWatch{
		key:      connKey,
		dst:      append(net.IP(nil), pkt.dst...),
		port:     dport,
		client:   append(net.IP(nil), pkt.src...),
		cport:    sport,
		ownPort:  pkt.clientPort(),
		wire:     append(net.IP(nil), wire...),
		tun:      cfg.Queue.Mode == "tun",
		ver:      pkt.ver,
		ihl:      pkt.ihl,
		hdr:      append([]byte(nil), pkt.raw[:hdrLen]...),
		helloAck: binary.BigEndian.Uint32(pkt.raw[pkt.ihl+8 : pkt.ihl+12]),
		host:     host,
		setID:    set.Id,
		set:      set.Name,
		sniSet:   sniSet,
		ipSet:    ipSet,
		mac:      pkt.srcMac,
		scope:    scope.settled(),
		tls:      tlsVersion,
		deadline: deadline,

		fakesCloseEntry: w.closingFakesMayHaveHit(cfg, set, host),
	})
}

func sendsClosingFakes(set *config.SetConfig) bool {
	desync := &set.TCP.Desync
	return desync.PostDesync || (desync.Mode != config.ConfigOff && desync.Mode != "ack")
}

func (w *Worker) closingFakesMayHaveHit(cfg *config.Config, set *config.SetConfig, host string) bool {
	if sendsClosingFakes(set) {
		return true
	}
	if to, _, escalated := w.destState.GetEscalation(host); !escalated || to != set.Id {
		return false
	}
	for _, from := range cfg.Sets {
		if from != nil && sendsClosingFakes(from) && escalationReaches(cfg, from, set.Id) {
			return true
		}
	}
	return false
}

func (w *Worker) noteClientProgress(connKey string, tcp []byte) {
	if w == nil || !w.destState.Watching() {
		return
	}
	closing, acked := tcp[13]&0x05 != 0, tcp[13]&0x10 != 0
	if !closing && !acked {
		return
	}
	w.destState.NoteClientPacket(connKey, binary.BigEndian.Uint32(tcp[8:12]), acked, closing, time.Now(), w.bans.forgetSilentHellos)
}

func silenceWatchedSet(cfg *config.Config, setID string) bool {
	for _, set := range cfg.Sets {
		if set != nil && set.Id == setID {
			return set.Enabled && banEnforced(set) && !set.RoutingHandsOffPackets()
		}
	}
	return false
}

func checkServerSilence(h helloWatch) (serverSilence, error) {
	source := h.client
	if h.tun {
		source = h.wire
	}
	client, ok1 := netip.AddrFromSlice(source)
	server, ok2 := netip.AddrFromSlice(h.dst)
	if !ok1 || !ok2 {
		return silenceUnknown, nil
	}
	from, to := netip.AddrPortFrom(client.Unmap(), h.cport), netip.AddrPortFrom(server.Unmap(), h.port)
	dir := ctnl.Original
	if h.tun {
		from, to, dir = to, from, ctnl.Reply
	}
	tuple, err := ctnl.NewTuple(unix.IPPROTO_TCP, from, to)
	if err != nil {
		return silenceUnknown, nil
	}
	flow, found, err := lookupFlow(tuple, dir)
	switch {
	case err != nil:
		return silenceUnknown, err
	case !found && h.fakesCloseEntry:
		return silenceOpen, nil
	case !found:
		return silenceGone, nil
	case !flow.HasCounters:
		return silenceUnknown, nil
	}
	headers := uint64(60 + 20)
	if h.ver == IPv6 {
		headers = 60 + IPv6HeaderLen
	}
	if flow.ReplyBytes > flow.ReplyPackets*headers {
		return silenceAnswered, nil
	}
	if flow.HasState && flow.TCPState != ctnl.TCPEstablished && !h.fakesCloseEntry {
		return silenceGone, nil
	}
	return silenceOpen, nil
}

func (w *Worker) sweepSilentHellos(cfg *config.Config, now time.Time) {
	if w == nil || w.destState == nil {
		return
	}
	due := w.destState.DueSilent(now)
	if len(due) == 0 {
		return
	}
	active := cfg != nil && !cfg.Queue.IsDiscovery && w.bans.on()
	lookups, lookupsOff := 0, false
	var answered map[string]bool
	var silent []helloWatch
	for _, h := range due {
		switch {
		case !active || !silenceWatchedSet(cfg, h.setID):
			w.destState.EndWatch(h)
			continue
		case w.bans.banned(h.dst, h.port):
			if w.destState.EndWatch(h) && !h.closed {
				w.resetSilentHello(h)
			}
			continue
		case w.bans.answeredRecently(h.dst, h.port, now):
			w.destState.EndWatch(h)
			continue
		}
		if !h.closed {
			if lookupsOff {
				w.destState.Park(h)
				continue
			}
			if lookups >= maxSilenceLookups {
				w.destState.Unprobe(h)
				continue
			}
			lookups++
			state, err := checkServerSilence(h)
			if err != nil {
				lookupsOff = true
			}
			switch state {
			case silenceAnswered:
				if answered == nil {
					answered = make(map[string]bool)
				}
				answered[iphealth.BanKey(h.dst, int(h.port))] = true
				w.bans.forgetSilentHellos(h.dst, h.port)
				w.destState.EndWatch(h)
				continue
			case silenceGone:
				w.destState.EndWatch(h)
				continue
			case silenceUnknown:
				w.destState.Park(h)
				continue
			}
		}
		if w.destState.escalatedAway(cfg, h) {
			if h.closed {
				w.destState.EndWatch(h)
			} else {
				w.destState.Park(h)
			}
			continue
		}
		silent = append(silent, h)
	}
	for _, h := range silent {
		key := iphealth.BanKey(h.dst, int(h.port))
		w.countSilentConnection(cfg, h, key, now, answered[key])
	}
}

func (w *Worker) countSilentConnection(cfg *config.Config, h helloWatch, key string, now time.Time, answeredInThisSweep bool) {
	silent := 0
	switch {
	case answeredInThisSweep:
		if _, watched := w.destState.CountOnce(h, func() int { return 0 }); !watched {
			return
		}
		log.Tracef("IP bans: %s took the ClientHello and sent nothing back, but answered another connection that came due with it, so this one is not counted (host: %s, set: %s)", key, h.host, h.set)
	case h.counted:
		silent = w.bans.silentHellos(key, now)
	default:
		watched := false
		silent, watched = w.destState.CountOnce(h, func() int { return w.bans.countSilentHello(key, now) })
		if !watched {
			return
		}
		if silent < silentHellosToBan {
			log.Tracef("IP bans: %s took the ClientHello and sent nothing back, unanswered connection %d of the %d in a row that get the address checked with neutral names (host: %s, set: %s)", key, silent, silentHellosToBan, h.host, h.set)
		}
	}
	if silent < silentHellosToBan {
		if h.closed {
			w.destState.EndWatch(h)
		} else {
			w.destState.Park(h)
		}
		return
	}
	log.Tracef("IP bans: %s took the ClientHello and sent nothing back on %d connections in a row, checking it with neutral names (host: %s, set: %s)", key, silent, h.host, h.set)
	if !w.bans.confirmSilence(cfg, h.dst, h.port, h.host, h.set) {
		w.destState.Unprobe(h)
	}
}

func (p *Pool) sweepSilentHellos(now time.Time) {
	if p == nil || len(p.Workers) == 0 {
		return
	}
	p.Workers[0].sweepSilentHellos(p.GetFirstWorkerConfig(), now)
}

func (w *Worker) settleSilentHellos(ip net.IP, port int, banned bool) {
	if w == nil || port <= 0 || port > 65535 {
		return
	}
	cfg := w.getConfig()
	banned = banned && cfg != nil && !cfg.Queue.IsDiscovery && w.bans.on()
	for _, h := range w.destState.TakeSilent(ip, uint16(port)) {
		if banned && !h.closed && silenceWatchedSet(cfg, h.setID) {
			w.resetSilentHello(h)
		}
	}
}

func (w *Worker) silentHelloGotAnswer(ip net.IP, port int) bool {
	if w == nil || w.destState == nil || port <= 0 || port > 65535 {
		return false
	}
	for _, h := range w.destState.WatchesTo(ip, uint16(port)) {
		if h.closed {
			continue
		}
		state, err := checkServerSilence(h)
		if err != nil {
			break
		}
		if state == silenceAnswered {
			w.destState.EndWatch(h)
			return true
		}
	}
	return false
}

func (w *Worker) resetSilentHello(h helloWatch) {
	if h.ver == IPv4 {
		w.sendRSTToClientV4(h.hdr, h.ihl, h.client, h.ownPort, h.dst)
	} else {
		w.sendRSTToClientV6(h.hdr, h.client, h.dst)
	}
	log.LogConnection("TCP", h.sniSet, h.host, h.client.String(), h.cport, h.ipSet, h.dst.String(), h.port, h.mac, config.TLSVersionString(h.tls), "ipblock-ban")
}
