package tun

import (
	"sync/atomic"
	"time"
)

type DiagInfo struct {
	DeviceName           string
	Address              string
	AddressV6            string
	OutInterface         string
	OutGateway           string
	ResolvedSrc          string
	Capture              string
	RouteTable           int
	Mark                 uint
	ReplyCapture         bool
	SkipTables           bool
	PacketsForwarded     uint64
	ForwardErrors        uint64
	IPv6Dropped          uint64
	SteerConflicts       []string
	CaptureRules         int
	CaptureRulesExpected int
	CaptureRestores      int
	LastCaptureRestore   time.Time
}

func (e *Engine) DiagInfo() DiagInfo {
	di := DiagInfo{
		DeviceName:       e.tunName,
		PacketsForwarded: atomic.LoadUint64(&e.fwdCount),
		ForwardErrors:    atomic.LoadUint64(&e.fwdErrCount),
		IPv6Dropped:      atomic.LoadUint64(&e.v6DropCount),
		CaptureRules:     -1,
	}

	if r := e.routes; r != nil {
		r.mu.Lock()
		di.Address = r.tunAddr
		di.AddressV6 = r.tunAddrV6
		di.OutInterface = r.outIface
		di.OutGateway = r.outGateway
		di.ResolvedSrc = r.srcIP
		di.Capture = r.resolvedCapture
		di.RouteTable = r.activeTable()
		di.Mark = r.mark
		di.ReplyCapture = r.replyCapture
		di.SkipTables = r.skipTables
		for _, c := range r.conflicts {
			di.SteerConflicts = append(di.SteerConflicts, c.String())
		}
		di.CaptureRulesExpected = r.captureInstalled
		di.CaptureRestores = r.captureRestores
		di.LastCaptureRestore = r.lastCaptureRestore
		if di.Capture == "ports" {
			di.CaptureRules = 0
			if out, err := run("iptables", "-t", "mangle", "-S", tunCaptureChain); err == nil {
				di.CaptureRules = countChainRules(out, tunCaptureChain)
			}
		}
		r.mu.Unlock()
	}

	return di
}
