package tun

import "github.com/daniellavrushin/b4/engine"

func (r *routeManager) tcpDirection(raw []byte) engine.Direction {
	if !r.ctdirOK || r.resolvedCapture != "ports" || len(raw) < 20 || raw[0]>>4 != 4 || raw[9] != 6 {
		return engine.DirUnknown
	}
	ihl := int(raw[0]&0x0f) * 4
	if ihl < 20 || len(raw) < ihl+14 || (uint16(raw[6])<<8|uint16(raw[7]))&0x3fff != 0 {
		return engine.DirUnknown
	}
	if raw[ihl+13]&0x04 != 0 {
		return engine.DirUnknown
	}
	return engine.DirOriginal
}

func (e *Engine) packetDirection(raw []byte, flow engine.Direction) engine.Direction {
	r := e.routes
	if r == nil || r.skipTables {
		return engine.DirUnknown
	}
	if cfg := e.config(); cfg == nil || cfg.System.Tables.SkipSetup {
		return engine.DirUnknown
	}
	if dir := r.tcpDirection(raw); dir != engine.DirUnknown {
		return dir
	}
	return flow
}

func (e *Engine) tcpDirectionExact() bool {
	r := e.routes
	if r == nil || r.skipTables {
		return false
	}
	return e.limiter != nil || (r.resolvedCapture == "ports" && r.ctdirOK)
}
