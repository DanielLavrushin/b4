package tun

import (
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const egressWatchDebounce = 500 * time.Millisecond

type egressWatcher struct {
	conn    *netlink.Conn
	stop    chan struct{}
	wg      sync.WaitGroup
	trigger func()
	started bool

	dmu    sync.Mutex
	dtimer *time.Timer
}

func newEgressWatcher(trigger func()) *egressWatcher {
	return &egressWatcher{
		stop:    make(chan struct{}),
		trigger: trigger,
	}
}

func (w *egressWatcher) Start() error {
	conn, err := netlink.Dial(unix.NETLINK_ROUTE, &netlink.Config{
		Groups: unix.RTMGRP_LINK | unix.RTMGRP_IPV4_ROUTE | unix.RTMGRP_IPV4_IFADDR,
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

func (w *egressWatcher) Stop() {
	if !w.started {
		return
	}
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

	w.dmu.Lock()
	if w.dtimer != nil {
		w.dtimer.Stop()
		w.dtimer = nil
	}
	w.dmu.Unlock()
}

func (w *egressWatcher) loop() {
	defer w.wg.Done()
	for {
		if _, err := w.conn.Receive(); err != nil {
			select {
			case <-w.stop:
				return
			default:
			}
			log.Tracef("TUN: egress watcher receive error: %v", err)
			time.Sleep(time.Second)
			continue
		}
		w.schedule()
	}
}

func (w *egressWatcher) schedule() {
	w.dmu.Lock()
	defer w.dmu.Unlock()
	if w.dtimer != nil {
		return
	}
	w.dtimer = time.AfterFunc(egressWatchDebounce, func() {
		select {
		case <-w.stop:
			return
		default:
		}
		w.dmu.Lock()
		w.dtimer = nil
		w.dmu.Unlock()
		w.trigger()
	})
}
