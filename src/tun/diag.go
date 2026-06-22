package tun

import "sync/atomic"

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
	}

	return di
}
