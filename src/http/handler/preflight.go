package handler

import (
	"fmt"
	"net"
	"strconv"

	"github.com/daniellavrushin/b4/config"
)

func preflightConfig(newCfg, oldCfg *config.Config) []FieldError {
	var fields []FieldError

	if newCfg.System.WebServer.Port > 0 && newCfg.System.WebServer.Port <= 65535 {
		old := oldCfg.System.WebServer
		nw := newCfg.System.WebServer
		oldEnabled := old.Port > 0 && old.Port <= 65535
		if !oldEnabled || old.Port != nw.Port {
			if f := probePort("system.web_server.port", nw.BindAddress, nw.Port); f != nil {
				fields = append(fields, *f)
			}
		}
	}

	if newCfg.System.MTProto.Enabled {
		old := oldCfg.System.MTProto
		nw := newCfg.System.MTProto
		if !old.Enabled || old.Port != nw.Port {
			if f := probePort("system.mtproto.port", nw.BindAddress, nw.Port); f != nil {
				fields = append(fields, *f)
			}
		}
	}

	if newCfg.System.Socks5.Enabled {
		old := oldCfg.System.Socks5
		nw := newCfg.System.Socks5
		if !old.Enabled || old.Port != nw.Port {
			if f := probePort("system.socks5.port", nw.BindAddress, nw.Port); f != nil {
				fields = append(fields, *f)
			}
		}
	}

	if newCfg.DNSTCPInterceptEnabled() {
		oldPort := oldCfg.DNSTCPListenPort()
		newPort := newCfg.DNSTCPListenPort()
		if !oldCfg.DNSTCPInterceptEnabled() || oldPort != newPort {
			if f := probePort("system.dns.tcp_port", "0.0.0.0", newPort); f != nil {
				fields = append(fields, *f)
			}
		}
	}

	return fields
}

func probePort(path, bindAddr string, port int) *FieldError {
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}
	addr := net.JoinHostPort(bindAddr, strconv.Itoa(port))
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return &FieldError{
			Path:    path,
			Code:    "port_in_use_system",
			Message: fmt.Sprintf("port %d on %s is already in use", port, bindAddr),
			Params:  map[string]any{"port": port, "bind": bindAddr},
		}
	}
	_ = ln.Close()
	return nil
}
