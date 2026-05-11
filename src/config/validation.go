package config

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/utils"
)

type ValidationField struct {
	Path    string         `json:"path"`
	Code    string         `json:"code"`
	Message string         `json:"message"`
	Params  map[string]any `json:"params,omitempty"`
}

type ValidationError struct {
	Fields []ValidationField
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Fields) == 0 {
		return "validation failed"
	}
	parts := make([]string, len(e.Fields))
	for i, f := range e.Fields {
		parts[i] = f.Path + ": " + f.Message
	}
	return strings.Join(parts, "; ")
}

type validator struct {
	fields []ValidationField
}

func (v *validator) add(path, code, message string, params map[string]any) {
	v.fields = append(v.fields, ValidationField{Path: path, Code: code, Message: message, Params: params})
}

func (v *validator) addf(path, code string, params map[string]any, format string, args ...any) {
	v.add(path, code, fmt.Sprintf(format, args...), params)
}

func (v *validator) hasErrors() bool {
	return len(v.fields) > 0
}

func (v *validator) result() error {
	if !v.hasErrors() {
		return nil
	}
	out := make([]ValidationField, len(v.fields))
	copy(out, v.fields)
	return &ValidationError{Fields: out}
}

func (c *Config) Validate() error {
	v := &validator{}
	c.System.WebServer.IsEnabled = c.System.WebServer.Port > 0 && c.System.WebServer.Port <= 65535

	hasCert := c.System.WebServer.TLSCert != ""
	hasKey := c.System.WebServer.TLSKey != ""
	if hasCert != hasKey {
		v.add("system.web_server.tls_cert", "tls_pair_required", "both tls_cert and tls_key must be specified together", nil)
		return v.result()
	}
	if hasCert {
		if _, err := os.Stat(c.System.WebServer.TLSCert); err != nil {
			v.addf("system.web_server.tls_cert", "file_not_found", map[string]any{"path": c.System.WebServer.TLSCert}, "TLS certificate file not found: %s", c.System.WebServer.TLSCert)
			return v.result()
		}
		if _, err := os.Stat(c.System.WebServer.TLSKey); err != nil {
			v.addf("system.web_server.tls_key", "file_not_found", map[string]any{"path": c.System.WebServer.TLSKey}, "TLS key file not found: %s", c.System.WebServer.TLSKey)
			return v.result()
		}
	}

	c.checkPortCollisions(v)
	if v.hasErrors() {
		return v.result()
	}

	if _, err := ParseMemoryLimit(c.System.MemoryLimit); err != nil {
		v.addf("system.memory_limit", "invalid_value", map[string]any{"value": c.System.MemoryLimit}, "%v", err)
		return v.result()
	}

	if c.Queue.TCPConnBytesLimit < DefaultConfig.Queue.TCPConnBytesLimit {
		c.Queue.TCPConnBytesLimit = DefaultConfig.Queue.TCPConnBytesLimit
	} else if c.Queue.TCPConnBytesLimit > 100 {
		c.Queue.TCPConnBytesLimit = 100
	}
	if c.Queue.UDPConnBytesLimit < DefaultConfig.Queue.UDPConnBytesLimit {
		c.Queue.UDPConnBytesLimit = DefaultConfig.Queue.UDPConnBytesLimit
	} else if c.Queue.UDPConnBytesLimit > 30 {
		c.Queue.UDPConnBytesLimit = 30
	}

	for setIdx, set := range c.Sets {
		if set.Routing.Table < 0 {
			set.Routing.Table = 0
		}
		if set.Routing.IPTTLSeconds <= 0 {
			set.Routing.IPTTLSeconds = DefaultSetConfig.Routing.IPTTLSeconds
		}
		switch set.Routing.Mode {
		case "":
			set.Routing.Mode = RoutingModeInterface
		case RoutingModeProxy, RoutingModeInterface:
		default:
			v.addf(fmt.Sprintf("sets[%d].routing.mode", setIdx), "invalid_routing_mode", map[string]any{"set": set.Name, "mode": set.Routing.Mode}, "set %q: unknown routing mode %q", set.Name, set.Routing.Mode)
			return v.result()
		}
		set.Routing.EgressInterface = sanitizeIfaceName(set.Routing.EgressInterface)
		for i, src := range set.Routing.SourceInterfaces {
			set.Routing.SourceInterfaces[i] = sanitizeIfaceName(src)
		}

		if set.Routing.Enabled && set.Routing.Mode == RoutingModeProxy {
			if set.Routing.Upstream.Port < 1 || set.Routing.Upstream.Port > 65535 {
				v.addf(fmt.Sprintf("sets[%d].routing.upstream.port", setIdx), "out_of_range", map[string]any{"set": set.Name, "min": 1, "max": 65535}, "set %q: upstream proxy port must be 1-65535", set.Name)
				return v.result()
			}
			h := strings.ToLower(strings.TrimSpace(set.Routing.Upstream.Host))
			if h == "" {
				h = "127.0.0.1"
			}
			if c.System.Socks5.Enabled && set.Routing.Upstream.Port == c.System.Socks5.Port {
				if h == "127.0.0.1" || h == "::1" || h == "localhost" || h == "0.0.0.0" {
					v.addf(fmt.Sprintf("sets[%d].routing.upstream.port", setIdx), "socks5_loop", map[string]any{"set": set.Name}, "set %q: upstream proxy points to b4's own SOCKS5 server (loop)", set.Name)
					return v.result()
				}
			}
		}

		if len(set.Fragmentation.SeqOverlapPattern) > 0 {
			set.Fragmentation.SeqOverlapBytes = make([]byte, len(set.Fragmentation.SeqOverlapPattern))
			for i, s := range set.Fragmentation.SeqOverlapPattern {
				s = strings.TrimPrefix(s, "0x")
				b, _ := strconv.ParseUint(s, 16, 8)
				set.Fragmentation.SeqOverlapBytes[i] = byte(b)
			}
		}

		if set.TCP.Duplicate.Enabled {
			if set.TCP.Duplicate.Count < 1 {
				set.TCP.Duplicate.Count = 1
			}
			if set.TCP.Duplicate.Count > 10 {
				set.TCP.Duplicate.Count = 10
			}
			if len(set.Targets.IPs) == 0 && len(set.Targets.GeoIpCategories) == 0 {
				log.Warnf("Set '%s' has duplication enabled but no IP targets configured", set.Name)
			}
		}

		if set.TCP.ConnBytesLimit > c.Queue.TCPConnBytesLimit {
			set.TCP.ConnBytesLimit = c.Queue.TCPConnBytesLimit
		}
		if set.UDP.ConnBytesLimit > c.Queue.UDPConnBytesLimit {
			set.UDP.ConnBytesLimit = c.Queue.UDPConnBytesLimit
		}

		if len(set.Targets.GeoSiteCategories) > 0 && c.System.Geo.GeoSitePath == "" {
			v.add(fmt.Sprintf("sets[%d].targets.geosite_categories", setIdx), "geosite_path_missing", "geosite path must be configured to use geosite categories", nil)
			return v.result()
		}

		if len(set.Targets.GeoIpCategories) > 0 && c.System.Geo.GeoIpPath == "" {
			v.add(fmt.Sprintf("sets[%d].targets.geoip_categories", setIdx), "geoip_path_missing", "geoip path must be configured to use geoip categories", nil)
			return v.result()
		}
	}

	c.sanitizeEscalation()

	// Validate global MSS clamp
	if c.Queue.MSSClamp.Enabled {
		if c.Queue.MSSClamp.Size < 10 {
			c.Queue.MSSClamp.Size = 10
		}
		if c.Queue.MSSClamp.Size > 1460 {
			c.Queue.MSSClamp.Size = 1460
		}
	}

	for i := range c.Queue.Devices.Devices {
		d := &c.Queue.Devices.Devices[i]
		d.MAC = strings.ToUpper(strings.TrimSpace(d.MAC))
		if d.MSSClamp > 0 {
			if d.MSSClamp < 10 {
				d.MSSClamp = 10
			}
			if d.MSSClamp > 1460 {
				d.MSSClamp = 1460
			}
		}
	}

	if c.Queue.Threads < 1 {
		v.add("queue.threads", "out_of_range", "threads must be at least 1", nil)
		return v.result()
	}

	if c.Queue.StartNum < 0 || c.Queue.StartNum > 65535 {
		v.add("queue.start_num", "out_of_range", "queue-num must be between 0 and 65535", nil)
		return v.result()
	}

	c.Queue.Mark = c.MainInjectedMark()

	maxMark := uint(^uint32(0))
	if c.Queue.Mark > maxMark {
		v.addf("queue.mark", "out_of_range", map[string]any{"mark": fmt.Sprintf("0x%x", c.Queue.Mark)}, "mark value 0x%x exceeds uint32 max", c.Queue.Mark)
		return v.result()
	}
	if c.Queue.Mark > maxMark-2 && c.System.Checker.DiscoveryFlowMark == 0 {
		v.addf("queue.mark", "out_of_range", map[string]any{"mark": fmt.Sprintf("0x%x", c.Queue.Mark)}, "mark value 0x%x is too high for auto-derived discovery marks", c.Queue.Mark)
		return v.result()
	}

	const perSetReachableBits uint32 = 0x17FFF
	if c.Queue.Mark != 0 && uint32(c.Queue.Mark)&^perSetReachableBits == 0 {
		v.addf("queue.mark", "mark_conflict", map[string]any{"mark": fmt.Sprintf("0x%x", c.Queue.Mark)}, "mark value 0x%x conflicts with per-set mark bits {0-14, 16}; bypass rule would catch TPROXY-redirected traffic. Use a value with at least one bit in {15, 17-31} (default 0x8000 has bit 15)", c.Queue.Mark)
		return v.result()
	}

	c.System.Checker.DiscoveryFlowMark = c.DiscoveryFlowMark()
	c.System.Checker.DiscoveryInjectedMark = c.DiscoveryInjectedMark()

	if c.System.Checker.DiscoveryFlowMark > maxMark || c.System.Checker.DiscoveryInjectedMark > maxMark {
		v.add("queue.mark", "out_of_range", "discovery mark values exceed uint32 max", nil)
		return v.result()
	}
	if c.Queue.Mark == c.System.Checker.DiscoveryFlowMark ||
		c.Queue.Mark == c.System.Checker.DiscoveryInjectedMark ||
		c.System.Checker.DiscoveryFlowMark == c.System.Checker.DiscoveryInjectedMark {
		v.add("queue.mark", "mark_conflict", "queue marks must be unique: mark, discovery_flow_mark, discovery_injected_mark", nil)
		return v.result()
	}

	for setIdx, set := range c.Sets {
		if set.Id == "" {
			v.add(fmt.Sprintf("sets[%d].id", setIdx), "required", "each set must have a unique non-empty ID", nil)
			return v.result()
		}

		set.UDP.DPortFilter = utils.ValidatePorts(set.UDP.DPortFilter)
		set.TCP.DPortFilter = utils.ValidatePorts(set.TCP.DPortFilter)
	}

	c.LoadCapturePayloads()
	c.BuildTCPPortMap()
	c.BuildSetPortRanges()

	return v.result()
}

func (c *Config) checkPortCollisions(v *validator) {
	type portRef struct {
		path string
		port int
	}
	portRangeParams := map[string]any{"min": 1, "max": 65535}
	var refs []portRef
	if c.System.WebServer.Port > 0 && c.System.WebServer.Port <= 65535 {
		refs = append(refs, portRef{"system.web_server.port", c.System.WebServer.Port})
	}
	if c.System.Socks5.Enabled {
		if c.System.Socks5.Port < 1 || c.System.Socks5.Port > 65535 {
			v.add("system.socks5.port", "out_of_range", "port must be between 1 and 65535", portRangeParams)
		} else {
			refs = append(refs, portRef{"system.socks5.port", c.System.Socks5.Port})
		}
	}
	if c.System.MTProto.Enabled {
		if c.System.MTProto.Port < 1 || c.System.MTProto.Port > 65535 {
			v.add("system.mtproto.port", "out_of_range", "port must be between 1 and 65535", portRangeParams)
		} else {
			refs = append(refs, portRef{"system.mtproto.port", c.System.MTProto.Port})
		}
		switch c.System.MTProto.UpstreamMode {
		case "", "tcp", "ws", "auto":
		default:
			v.addf("system.mtproto.upstream_mode", "invalid_value",
				map[string]any{"value": c.System.MTProto.UpstreamMode, "allowed": []string{"tcp", "ws", "auto"}},
				"upstream_mode must be one of tcp, ws, auto (got %q)", c.System.MTProto.UpstreamMode)
		}
		if h := c.System.MTProto.WSEndpointHost; h != "" {
			if strings.HasPrefix(h, "[") || (strings.Contains(h, ":") && net.ParseIP(h) == nil) {
				v.addf("system.mtproto.ws_endpoint_host", "invalid_host",
					map[string]any{"value": h},
					"ws_endpoint_host must be a host or IP without port (got %q)", h)
			}
		}
	}
	for i := 0; i < len(refs); i++ {
		for j := i + 1; j < len(refs); j++ {
			if refs[i].port == refs[j].port {
				v.addf(refs[j].path, "port_in_use", map[string]any{"port": refs[j].port, "conflict": refs[i].path}, "port %d is already used by %s", refs[j].port, refs[i].path)
			}
		}
	}
}
