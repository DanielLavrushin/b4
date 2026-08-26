package tun

import (
	"strconv"
	"strings"
	"sync/atomic"
)

type DiagInfo struct {
	DeviceName       string
	Address          string
	AddressV6        string
	OutInterface     string
	OutGateway       string
	ResolvedSrc      string
	Capture          string
	RouteTable       int
	Mark             uint
	ReplyCapture     bool
	SkipTables       bool
	PacketsForwarded uint64
	ForwardErrors    uint64
	IPv6Dropped      uint64
	SteerRuleOK      bool
	CaptureRules     int
}

func (e *Engine) DiagInfo() DiagInfo {
	di := DiagInfo{
		DeviceName:       e.tunName,
		PacketsForwarded: atomic.LoadUint64(&e.fwdCount),
		ForwardErrors:    atomic.LoadUint64(&e.fwdErrCount),
		IPv6Dropped:      atomic.LoadUint64(&e.v6DropCount),
	}

	if r := e.routes; r != nil {
		r.mu.Lock()
		di.Address = r.tunAddr
		di.AddressV6 = r.tunAddrV6
		di.OutInterface = r.outIface
		di.OutGateway = r.outGateway
		di.ResolvedSrc = r.srcIP
		di.Capture = r.resolvedCapture
		di.RouteTable = r.routeTable
		di.Mark = r.mark
		di.ReplyCapture = r.replyCapture
		di.SkipTables = r.skipTables
		r.mu.Unlock()

		di.SteerRuleOK = r.captureHealthy()
		di.CaptureRules = r.captureRuleCount()
	}

	return di
}

func (r *routeManager) captureHealthy() bool {
	if r.resolvedCapture == "default" {
		out, err := run("ip", "-4", "route", "show", "default")
		return err == nil && strings.Contains(out, "dev "+r.tunName)
	}
	if !r.steerRulePresent(r.steerMarkStr(), strconv.Itoa(r.captureTable)) {
		return false
	}
	out, err := run("ip", "route", "show", "table", strconv.Itoa(r.captureTable))
	return err == nil && strings.Contains(out, "dev "+r.tunName)
}

func (r *routeManager) captureRuleCount() int {
	out, err := run("iptables", "-t", "mangle", "-S", tunCaptureChain)
	if err != nil {
		return 0
	}
	n := 0
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "-A "+tunCaptureChain) {
			n++
		}
	}
	return n
}
