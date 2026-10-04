package config

import (
	"strings"

	"github.com/daniellavrushin/b4/dns/endpoint"
	"github.com/daniellavrushin/b4/log"
)

func (c *Config) TrustedDNSServer() (endpoint.Endpoint, bool) {
	if c == nil || c.System.Checker.DNSServer == "" {
		return endpoint.Endpoint{}, false
	}
	ep, err := endpoint.Parse(c.System.Checker.DNSServer)
	return ep, err == nil
}

func (c *Config) sanitizeTrustedDNSServer() {
	server := strings.TrimSpace(c.System.Checker.DNSServer)
	c.System.Checker.DNSServer = server
	if server == "" {
		return
	}
	if _, err := endpoint.Parse(server); err != nil {
		log.Warnf("Discovery: ignoring the trusted DNS server %q: %v", server, err)
		c.System.Checker.DNSServer = ""
	}
}
