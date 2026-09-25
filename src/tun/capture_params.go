package tun

import (
	"slices"

	"github.com/daniellavrushin/b4/config"
)

type captureParams struct {
	tcpPorts       []string
	udpPorts       []string
	tcpLimit       int
	udpLimit       int
	dupIPs         []string
	replyCapture   bool
	devicesEnabled bool
	whiteIsBlack   bool
	selectedMACs   []string
}

func captureParamsFrom(cfg *config.Config) captureParams {
	tcpLimit := cfg.Queue.TCPConnBytesLimit
	if tcpLimit <= 0 {
		tcpLimit = 19
	}
	udpLimit := cfg.Queue.UDPConnBytesLimit
	if udpLimit <= 0 {
		udpLimit = 8
	}
	dupV4, _ := cfg.CollectDuplicateIPs()
	return captureParams{
		tcpPorts:       normalizePorts(cfg.CollectTCPPorts()),
		udpPorts:       normalizePorts(cfg.CollectUDPPorts()),
		tcpLimit:       tcpLimit,
		udpLimit:       udpLimit,
		dupIPs:         dupV4,
		replyCapture:   replyCaptureNeeded(cfg),
		devicesEnabled: cfg.Queue.Devices.Enabled,
		whiteIsBlack:   cfg.Queue.Devices.WhiteIsBlack,
		selectedMACs:   cfg.Queue.Devices.SelectedMACs(),
	}
}

func (p captureParams) sameChain(o captureParams) bool {
	return slices.Equal(p.tcpPorts, o.tcpPorts) &&
		slices.Equal(p.udpPorts, o.udpPorts) &&
		p.tcpLimit == o.tcpLimit &&
		p.udpLimit == o.udpLimit &&
		slices.Equal(p.dupIPs, o.dupIPs) &&
		p.replyCapture == o.replyCapture
}

func (p captureParams) sameGate(o captureParams) bool {
	return p.devicesEnabled == o.devicesEnabled &&
		p.whiteIsBlack == o.whiteIsBlack &&
		slices.Equal(p.selectedMACs, o.selectedMACs)
}

func (r *routeManager) captureParams() captureParams {
	return captureParams{
		tcpPorts:       r.tcpPorts,
		udpPorts:       r.udpPorts,
		tcpLimit:       r.tcpLimit,
		udpLimit:       r.udpLimit,
		dupIPs:         r.dupIPs,
		replyCapture:   r.replyCapture,
		devicesEnabled: r.devicesEnabled,
		whiteIsBlack:   r.whiteIsBlack,
		selectedMACs:   r.selectedMACs,
	}
}

func (r *routeManager) setCaptureParams(p captureParams) {
	r.tcpPorts = p.tcpPorts
	ports := p.tcpPorts
	r.liveTCPPorts.Store(&ports)
	r.udpPorts = p.udpPorts
	r.tcpLimit = p.tcpLimit
	r.udpLimit = p.udpLimit
	r.dupIPs = p.dupIPs
	r.replyCapture = p.replyCapture
	r.devicesEnabled = p.devicesEnabled
	r.whiteIsBlack = p.whiteIsBlack
	r.selectedMACs = p.selectedMACs
}

func (r *routeManager) replyPorts() []string {
	if p := r.liveTCPPorts.Load(); p != nil {
		return *p
	}
	return nil
}

func (r *routeManager) updateCapture(p captureParams) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur := r.captureParams()
	chain, gate := !cur.sameChain(p), !cur.sameGate(p)
	if !chain && !gate {
		return false
	}
	r.setCaptureParams(p)
	r.captureDirty = r.captureDirty || chain
	r.gateDirty = r.gateDirty || gate
	return true
}
