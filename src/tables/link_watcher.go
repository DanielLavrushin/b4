package tables

import (
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/netif"
	"github.com/josharian/native"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const (
	ifInfoMsgSize       = 16
	linkWatcherDebounce = 500 * time.Millisecond
	ndMsgSize           = 12
)

type linkWatcher struct {
	cfgPtr  *atomic.Pointer[config.Config]
	conn    *netlink.Conn
	stop    chan struct{}
	wg      sync.WaitGroup
	started bool

	debounceMu    sync.Mutex
	debounceTimer *time.Timer
	pendingIfaces map[string]struct{}
}

func newLinkWatcher(cfgPtr *atomic.Pointer[config.Config]) *linkWatcher {
	return &linkWatcher{
		cfgPtr:        cfgPtr,
		stop:          make(chan struct{}),
		pendingIfaces: make(map[string]struct{}),
	}
}

func (w *linkWatcher) Start() error {
	if w.started {
		return nil
	}
	conn, err := netlink.Dial(unix.NETLINK_ROUTE, &netlink.Config{
		Groups: unix.RTMGRP_LINK | unix.RTMGRP_NEIGH,
	})
	if err != nil {
		return err
	}
	w.conn = conn
	w.started = true
	w.wg.Add(1)
	go w.loop()
	return nil
}

func (w *linkWatcher) Stop() {
	select {
	case <-w.stop:
		return
	default:
	}
	close(w.stop)
	if w.conn != nil {
		_ = w.conn.Close()
	}
	w.wg.Wait()

	w.debounceMu.Lock()
	if w.debounceTimer != nil {
		w.debounceTimer.Stop()
		w.debounceTimer = nil
	}
	w.debounceMu.Unlock()
}

func (w *linkWatcher) loop() {
	defer w.wg.Done()
	for {
		msgs, err := w.conn.Receive()
		if err != nil {
			select {
			case <-w.stop:
				return
			default:
			}
			log.Tracef("Link watcher: receive error: %v", err)
			time.Sleep(time.Second)
			continue
		}
		for _, m := range msgs {
			switch m.Header.Type {
			case unix.RTM_NEWLINK:
				if name, up := parseIfInfoMsg(m.Data); name != "" {
					w.handleEvent(name, true, up)
				}
			case unix.RTM_DELLINK:
				if name, _ := parseIfInfoMsg(m.Data); name != "" {
					w.handleEvent(name, false, false)
				}
			case unix.RTM_NEWNEIGH, unix.RTM_DELNEIGH:
				name, ip, mac, alive, verdict := parseNeighMsg(m.Header.Type, m.Data)
				switch verdict {
				case neighOK:
					w.handleNeighEvent(name, ip, mac, alive)
				case neighFilteredState:
					log.Tracef("Link watcher: ignoring neighbor message in an uninteresting state type=%d len=%d", m.Header.Type, len(m.Data))
				default:
					log.Tracef("Link watcher: ignoring malformed neighbor message type=%d len=%d", m.Header.Type, len(m.Data))
				}
			}
		}
	}
}

func parseIfInfoMsg(b []byte) (name string, up bool) {
	if len(b) < ifInfoMsgSize {
		return "", false
	}
	flags := native.Endian.Uint32(b[8:12])
	up = flags&unix.IFF_UP != 0
	ad, err := netlink.NewAttributeDecoder(b[ifInfoMsgSize:])
	if err != nil {
		return "", up
	}
	for ad.Next() {
		if ad.Type() == unix.IFLA_IFNAME {
			name = strings.TrimRight(ad.String(), "\x00")
		}
	}
	if err := ad.Err(); err != nil {
		log.Tracef("Link watcher: failed to decode link attributes: %v", err)
		return "", up
	}
	return name, up
}

type neighVerdict int

const (
	neighOK neighVerdict = iota
	neighFilteredState
	neighMalformed
)

func parseNeighMsg(msgType netlink.HeaderType, b []byte) (ifname, ip, mac string, alive bool, verdict neighVerdict) {
	if len(b) < ndMsgSize {
		return "", "", "", false, neighMalformed
	}
	// RTM_DELNEIGH means the entry is gone regardless of the ndm_state in the payload.
	if msgType != unix.RTM_DELNEIGH {
		switch native.Endian.Uint16(b[8:10]) {
		case unix.NUD_REACHABLE, unix.NUD_STALE, unix.NUD_DELAY, unix.NUD_PROBE, unix.NUD_PERMANENT:
			alive = true
		case unix.NUD_FAILED, unix.NUD_INCOMPLETE, unix.NUD_NOARP:
			// alive stays false
		default:
			return "", "", "", false, neighFilteredState
		}
	}
	ifindex := int(native.Endian.Uint32(b[4:8]))
	var dst, lladdr []byte
	ad, err := netlink.NewAttributeDecoder(b[ndMsgSize:])
	if err != nil {
		return "", "", "", false, neighMalformed
	}
	for ad.Next() {
		switch ad.Type() {
		case unix.NDA_DST:
			dst = append([]byte(nil), ad.Bytes()...)
		case unix.NDA_LLADDR:
			lladdr = append([]byte(nil), ad.Bytes()...)
		}
	}
	if err := ad.Err(); err != nil || len(dst) == 0 {
		return "", "", "", false, neighMalformed
	}
	iface, err := net.InterfaceByIndex(ifindex)
	if err != nil {
		return "", "", "", false, neighMalformed
	}
	if len(lladdr) > 0 {
		mac = net.HardwareAddr(lladdr).String()
	}
	return iface.Name, net.IP(dst).String(), mac, alive, neighOK
}
func (w *linkWatcher) handleNeighEvent(ifname, ip, mac string, alive bool) {
	cfg := w.cfgPtr.Load()
	if cfg == nil {
		return
	}
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled || !set.Routing.Enabled {
			continue
		}
		if set.Routing.Mode != "" && set.Routing.Mode != config.RoutingModeInterface {
			continue
		}
		if set.Routing.EgressInterface != ifname || set.Routing.EgressGateway == "" {
			continue
		}
		if !strings.EqualFold(set.Routing.EgressGateway, ip) {
			continue
		}
		routeMu.Lock()
		st, ok := routeRuleCache[set.Id]
		routeMu.Unlock()
		cached := ""
		if ok {
			cached = st.gwMAC
			if !alive {
				if st.gwMAC == "" {
					return
				}
			} else if mac != "" && strings.EqualFold(st.gwMAC, mac) {
				return
			}
		}
		if !alive {
			log.Infof("Link watcher: neighbor %s on %s died, scheduling routing reinstall for set '%s'", ip, ifname, set.Name)
		} else {
			log.Infof("Link watcher: neighbor %s on %s is now %s (was %s), scheduling routing reinstall for set '%s'", ip, ifname, mac, cached, set.Name)
		}
		w.scheduleReinstall(ifname)
		return
	}
}

func (w *linkWatcher) handleEvent(ifname string, isNew bool, up bool) {
	cfg := w.cfgPtr.Load()
	if cfg == nil {
		return
	}
	if !isWatchedIface(cfg, ifname) {
		return
	}
	if isNew && up {
		log.Infof("Link watcher: interface %s is up, scheduling routing reinstall", ifname)
		w.scheduleReinstall(ifname)
		return
	}
	netif.MarkDown(ifname)
	if !isNew {
		log.Infof("Link watcher: interface %s went away; routing will be reinstalled when it returns", ifname)
	}
}

func isWatchedIface(cfg *config.Config, ifname string) bool {
	for _, set := range cfg.Sets {
		if set == nil || !set.Enabled || !set.Routing.Enabled {
			continue
		}
		if set.Routing.EgressInterface == ifname {
			return true
		}
	}
	return false
}

func (w *linkWatcher) scheduleReinstall(ifname string) {
	w.debounceMu.Lock()
	defer w.debounceMu.Unlock()
	w.pendingIfaces[ifname] = struct{}{}
	if w.debounceTimer != nil {
		return
	}
	w.debounceTimer = time.AfterFunc(linkWatcherDebounce, func() {
		select {
		case <-w.stop:
			return
		default:
		}
		w.debounceMu.Lock()
		ifaces := w.pendingIfaces
		w.pendingIfaces = make(map[string]struct{})
		w.debounceTimer = nil
		w.debounceMu.Unlock()

		cfg := w.cfgPtr.Load()
		if cfg == nil {
			return
		}
		for iface := range ifaces {
			RoutingReinstallForInterface(cfg, iface)
		}
	})
}
