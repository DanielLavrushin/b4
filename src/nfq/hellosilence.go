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
	dst      net.IP
	port     uint16
	client   net.IP
	cport    uint16
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
	tls      uint16
	deadline time.Time
	probing  bool
	parked   bool
	closed   bool
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
	if _, ok := t.watches[h.key]; ok || len(t.watches) >= maxHelloWatches {
		return
	}
	if t.watches == nil {
		t.watches = make(map[string]*helloWatch)
	}
	t.watches[h.key] = h
	t.watching.Store(int32(len(t.watches)))
}

func (t *destStateTracker) NoteClientPacket(key string, ack uint32, acked, closing bool, now time.Time) {
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
	if answered || now.Before(h.deadline) {
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

func (t *destStateTracker) updateWatch(key string, change func(*helloWatch)) {
	if !t.Watching() {
		return
	}
	t.watchMu.Lock()
	defer t.watchMu.Unlock()
	if h, ok := t.watches[key]; ok {
		change(h)
	}
}

func (t *destStateTracker) Unprobe(key string) {
	t.updateWatch(key, func(h *helloWatch) { h.probing = false })
}

func (t *destStateTracker) Park(key string) {
	t.updateWatch(key, func(h *helloWatch) { h.probing, h.parked = false, true })
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
		if (h.probing || h.parked) && h.port == port && h.dst.Equal(ip) {
			taken = append(taken, *h)
			t.unwatchLocked(key)
		}
	}
	return taken
}

func (t *destStateTracker) unwatchLocked(key string) {
	delete(t.watches, key)
	t.watching.Store(int32(len(t.watches)))
}

func (w *Worker) watchHelloSilence(cfg *config.Config, set *config.SetConfig, pkt *pktInfo, connKey, host, sniSet, ipSet string, sport, dport, tlsVersion uint16, deadline time.Time) {
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
		tls:      tlsVersion,
		deadline: deadline,
	})
}

func (w *Worker) noteClientProgress(connKey string, tcp []byte) {
	if w == nil || !w.destState.Watching() {
		return
	}
	closing, acked := tcp[13]&0x05 != 0, tcp[13]&0x10 != 0
	if !closing && !acked {
		return
	}
	w.destState.NoteClientPacket(connKey, binary.BigEndian.Uint32(tcp[8:12]), acked, closing, time.Now())
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
	if flow.HasState && flow.TCPState != ctnl.TCPEstablished {
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
	for _, h := range due {
		switch {
		case !active || !silenceWatchedSet(cfg, h.setID):
			w.destState.Unwatch(h.key)
			continue
		case w.bans.banned(h.dst, h.port):
			if w.destState.Unwatch(h.key) && !h.closed {
				w.resetSilentHello(h)
			}
			continue
		case w.bans.answeredRecently(h.dst, h.port, now):
			w.destState.Unwatch(h.key)
			continue
		}
		if !h.closed {
			if lookupsOff {
				w.destState.Park(h.key)
				continue
			}
			if lookups >= maxSilenceLookups {
				w.destState.Unprobe(h.key)
				continue
			}
			lookups++
			state, err := checkServerSilence(h)
			if err != nil {
				lookupsOff = true
			}
			switch state {
			case silenceGone, silenceAnswered:
				w.destState.Unwatch(h.key)
				continue
			case silenceUnknown:
				w.destState.Park(h.key)
				continue
			}
		}
		log.Tracef("IP bans: %s took the ClientHello and sent nothing back, checking it with neutral names (host: %s, set: %s)", iphealth.BanKey(h.dst, int(h.port)), h.host, h.set)
		if !w.bans.confirm(cfg, h.dst, h.port, h.host, h.set) {
			w.destState.Unprobe(h.key)
		}
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

func (w *Worker) resetSilentHello(h helloWatch) {
	if h.ver == IPv4 {
		w.sendRSTToClientV4(h.hdr, h.ihl, h.client, h.dst)
	} else {
		w.sendRSTToClientV6(h.hdr, h.client, h.dst)
	}
	log.LogConnection("TCP", h.sniSet, h.host, h.client.String(), h.cport, h.ipSet, h.dst.String(), h.port, h.mac, config.TLSVersionString(h.tls), "ipblock-ban")
}
