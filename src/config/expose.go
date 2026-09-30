package config

import (
	"net/netip"
	"strings"
)

const (
	ExposeWebServer       = "web_server"
	ExposeMTProto         = "mtproto"
	ExposeMTProtoWebProxy = "mtproto_web_proxy"
	ExposeSocks5          = "socks5"
)

const (
	ExposeBlockedNoAuth       = "no_auth"
	ExposeBlockedOpenRelay    = "open_relay"
	ExposeBlockedSharedPort   = "shared_port"
	ExposeBlockedLoopback     = "loopback"
	ExposeBlockedInvalidBind  = "invalid_bind"
	ExposeBlockedWebNoAuth    = "web_no_auth"
	ExposeBlockedNotListening = "not_listening"
)

type WebListener struct {
	Port        int
	BindAddress string
}

type ExposedPort struct {
	Service string `json:"service"`
	Port    int    `json:"port"`
	Address string `json:"address,omitempty"`
	V4      bool   `json:"v4"`
	V6      bool   `json:"v6"`
}

type ExposeBlock struct {
	Service string `json:"service"`
	Reason  string `json:"reason"`
}

func ExposeSwitchPath(service string) string {
	switch service {
	case ExposeWebServer:
		return "system.web_server.expose"
	case ExposeMTProto:
		return "system.mtproto.expose"
	case ExposeMTProtoWebProxy:
		return "system.mtproto.web_proxy.expose"
	case ExposeSocks5:
		return "system.socks5.expose"
	}
	return ""
}

func (c *Config) ConfiguredWebListener() WebListener {
	return WebListener{Port: c.System.WebServer.Port, BindAddress: c.System.WebServer.BindAddress}
}

func (c *Config) ExposurePlan(web WebListener) ([]ExposedPort, []ExposeBlock) {
	var plan exposurePlan

	ws := c.System.WebServer
	if ws.Expose && exposablePort(web.Port) {
		if ws.Username == "" || ws.Password == "" {
			plan.block(ExposeWebServer, ExposeBlockedNoAuth)
		} else {
			plan.add(ExposeWebServer, web.Port, web.BindAddress)
		}
	}

	mt := c.System.MTProto
	if mt.Enabled && mt.Expose && exposablePort(mt.Port) {
		plan.add(ExposeMTProto, mt.Port, mt.BindAddress)
	}

	if wp := mt.WebProxy; mt.Enabled && wp.Enabled && wp.Expose {
		switch {
		case wp.Port <= 0:
			plan.block(ExposeMTProtoWebProxy, ExposeBlockedSharedPort)
		case exposablePort(wp.Port):
			plan.add(ExposeMTProtoWebProxy, wp.Port, mt.BindAddress)
		}
	}

	s5 := c.System.Socks5
	if s5.Enabled && s5.Expose && exposablePort(s5.Port) {
		switch {
		case s5.Username == "" && !hasSourceEntries(s5.AllowedSources):
			plan.block(ExposeSocks5, ExposeBlockedOpenRelay)
		case c.WebInterfaceUnprotectedWith(web):
			plan.block(ExposeSocks5, ExposeBlockedWebNoAuth)
		default:
			plan.add(ExposeSocks5, s5.Port, s5.BindAddress)
		}
	}

	return plan.ports, plan.blocked
}

func (c *Config) WebInterfaceUnprotectedWith(running WebListener) bool {
	ws := c.System.WebServer
	return (running.Port > 0 || ws.Port > 0) && (ws.Username == "" || ws.Password == "")
}

func ExposureAdded(oldCfg, newCfg *Config) []ExposedPort {
	oldPorts, _ := oldCfg.ExposurePlan(oldCfg.ConfiguredWebListener())
	newPorts, _ := newCfg.ExposurePlan(newCfg.ConfiguredWebListener())

	open := make(map[exposeTuple]bool)
	for _, p := range oldPorts {
		for _, t := range p.tuples() {
			open[t] = true
		}
	}

	var added []ExposedPort
	for _, p := range newPorts {
		for _, t := range p.tuples() {
			if !open[t] && !open[t.wildcard()] {
				added = append(added, p)
				break
			}
		}
	}
	return added
}

type exposeTuple struct {
	port    int
	v6      bool
	address string
}

func (t exposeTuple) wildcard() exposeTuple {
	return exposeTuple{port: t.port, v6: t.v6}
}

func (p ExposedPort) tuples() []exposeTuple {
	var out []exposeTuple
	if p.V4 {
		out = append(out, exposeTuple{port: p.Port, address: p.Address})
	}
	if p.V6 {
		out = append(out, exposeTuple{port: p.Port, v6: true, address: p.Address})
	}
	return out
}

type exposurePlan struct {
	ports   []ExposedPort
	blocked []ExposeBlock
}

func (p *exposurePlan) add(service string, port int, bind string) {
	address, v4, v6, reason := classifyExposeBind(bind)
	if reason != "" {
		p.block(service, reason)
		return
	}
	p.ports = append(p.ports, ExposedPort{Service: service, Port: port, Address: address, V4: v4, V6: v6})
}

func (p *exposurePlan) block(service, reason string) {
	p.blocked = append(p.blocked, ExposeBlock{Service: service, Reason: reason})
}

func classifyExposeBind(bind string) (address string, v4, v6 bool, reason string) {
	bind = strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(bind), "["), "]")
	if bind == "" {
		return "", true, true, ""
	}
	ip, err := netip.ParseAddr(bind)
	if err != nil || ip.Zone() != "" {
		return "", false, false, ExposeBlockedInvalidBind
	}
	ip = ip.Unmap()
	switch {
	case ip.IsUnspecified():
		return "", true, true, ""
	case ip.IsLoopback():
		return "", false, false, ExposeBlockedLoopback
	case ip.Is4():
		return ip.String(), true, false, ""
	default:
		return ip.String(), false, true, ""
	}
}

func exposablePort(port int) bool {
	return port > 0 && port <= 65535
}

func hasSourceEntries(entries []string) bool {
	for _, e := range entries {
		if strings.TrimSpace(e) != "" {
			return true
		}
	}
	return false
}
