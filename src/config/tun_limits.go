package config

func TUNLimitations(c *Config) []string {
	if c == nil || c.Queue.Mode != "tun" {
		return nil
	}

	var out []string
	if c.Queue.IPv6Enabled {
		out = append(out, "ipv6")
	}
	if c.DNSTCPInterceptEnabled() {
		out = append(out, "dns_tcp_router_queries")
	}
	if c.System.Checker.Watchdog.Enabled {
		out = append(out, "watchdog_heal")
	}
	out = append(out, "discovery")

	for _, set := range c.Sets {
		if set == nil || !set.Enabled || !set.Routing.Enabled {
			continue
		}
		if set.Routing.EgressIP != "" {
			out = append(out, "egress_ip")
			break
		}
	}
	return out
}
