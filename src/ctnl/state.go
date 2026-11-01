package ctnl

import (
	"errors"
	"net/netip"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

const requestTimeout = 2 * time.Second

const (
	stateUntested int32 = iota
	stateAvailable
	stateUnavailable
)

var (
	dial = func() (*netlink.Conn, error) {
		return netlink.Dial(unix.NETLINK_NETFILTER, nil)
	}

	probeState   atomic.Int32
	probeMu      sync.Mutex
	probeReason  string
	warnedReason string
)

var selfTestTuple = Tuple{
	proto: unix.IPPROTO_UDP,
	src:   netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 255, 255, 254}), 1),
	dst:   netip.AddrPortFrom(netip.AddrFrom4([4]byte{127, 255, 255, 253}), 1),
}

type Status struct {
	Tested    bool   `json:"tested"`
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

func Available() bool {
	switch probeState.Load() {
	case stateAvailable:
		return true
	case stateUnavailable:
		return false
	}
	return selfTest()
}

func Probe() Status {
	Available()
	return CurrentStatus()
}

func CurrentStatus() Status {
	probeMu.Lock()
	defer probeMu.Unlock()
	s := probeState.Load()
	return Status{Tested: s != stateUntested, Available: s == stateAvailable, Reason: probeReason}
}

func Retest() {
	probeMu.Lock()
	defer probeMu.Unlock()
	probeReason = ""
	probeState.Store(stateUntested)
}

func selfTest() bool {
	probeMu.Lock()
	defer probeMu.Unlock()
	if s := probeState.Load(); s != stateUntested {
		return s == stateAvailable
	}
	m, err := request(msgGet, selfTestTuple, Original)
	if err == nil {
		err = exchange(m)
	}
	switch {
	case errors.Is(err, unix.ENOENT):
		setAvailableLocked()
		return true
	case err == nil:
		setUnavailableLocked("the self-test found a conntrack entry for a tuple that cannot exist")
	default:
		setUnavailableLocked("self-test: " + err.Error())
	}
	return false
}

func exchange(m netlink.Message) error {
	c, err := dial()
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(requestTimeout))
	_, err = c.Execute(m)
	return err
}

func disable(reason string) {
	probeMu.Lock()
	defer probeMu.Unlock()
	setUnavailableLocked(reason)
}

func setAvailableLocked() {
	probeReason = ""
	probeState.Store(stateAvailable)
	if warnedReason != "" {
		warnedReason = ""
		log.Infof("Conntrack netlink is available: b4 deletes the conntrack entry of a QUIC connection that a set drops, rejects or blocks")
	}
}

func setUnavailableLocked(reason string) {
	probeReason = reason
	probeState.Store(stateUnavailable)
	if reason == warnedReason {
		return
	}
	warnedReason = reason
	log.Warnf("Conntrack netlink is unavailable (%s): b4 cannot delete the conntrack entry of a QUIC connection that a set drops, rejects or blocks, so unless b4's nftables drop set covers the connection, its datagrams after the first ones b4 receives pass through to the server. It also cannot confirm that a server left a connection's ClientHello unanswered, so IP block detection checks such an address only once the device gives up on the connection. The nf_conntrack_netlink kernel module provides conntrack netlink (OpenWrt package kmod-nf-conntrack-netlink)", reason)
}
