package config

import (
	"encoding/json"
	"net/url"
	"strings"
)

const RedactedMarker = "[redacted]"

func (c *Config) FileBytes() ([]byte, error) {
	return MarshalSparse(stripCLIOverrides(c))
}

func (c *Config) RedactedCopy() (*Config, error) {
	data, err := json.Marshal(c)
	if err != nil {
		return nil, err
	}
	var out Config
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, err
	}
	out.ConfigPath = c.ConfigPath
	out.RedactSecrets()
	return &out, nil
}

func (c *Config) RedactSecrets() {
	ws := &c.System.WebServer
	if ws.Password != "" {
		ws.Password = ""
		ws.PasswordSet = true
	}
	ws.Username = maskValue(ws.Username)
	ws.MCP.Token = ""

	c.System.Socks5.Username = maskValue(c.System.Socks5.Username)
	c.System.Socks5.Password = maskValue(c.System.Socks5.Password)

	mt := &c.System.MTProto
	for i := range mt.Secrets {
		mt.Secrets[i].Name = maskValue(mt.Secrets[i].Name)
		mt.Secrets[i].Secret = maskValue(mt.Secrets[i].Secret)
	}
	mt.DCRelay = maskValue(mt.DCRelay)
	mt.WSCustomDomain = maskValue(mt.WSCustomDomain)
	mt.CFWorkerDomain = maskValue(mt.CFWorkerDomain)
	mt.WebProxy.Hostname = maskValue(mt.WebProxy.Hostname)
	mt.CFProxyURL = maskURL(mt.CFProxyURL)
	mt.DCFallbackURL = maskURL(mt.DCFallbackURL)

	c.System.API.IPInfoToken = maskValue(c.System.API.IPInfoToken)
	c.System.AI.APIKeyRef = maskValue(c.System.AI.APIKeyRef)
	c.System.AI.Endpoint = maskURL(c.System.AI.Endpoint)

	c.System.Geo.GeoSiteURL = maskURL(c.System.Geo.GeoSiteURL)
	c.System.Geo.GeoIpURL = maskURL(c.System.Geo.GeoIpURL)
	c.System.Update.Mirrors = maskValues(c.System.Update.Mirrors)
	c.System.Hub.URLs = maskURLs(c.System.Hub.URLs)

	for _, set := range c.Sets {
		redactSetSecrets(set)
	}
}

func redactSetSecrets(set *SetConfig) {
	if set == nil {
		return
	}
	set.Routing.Upstream.Username = maskValue(set.Routing.Upstream.Username)
	set.Routing.Upstream.Password = maskValue(set.Routing.Upstream.Password)
	set.DNS.DoHURL = maskDoHURL(set.DNS.DoHURL)
	set.Discovery.URLs = maskURLs(set.Discovery.URLs)
}

func maskValue(v string) string {
	if v == "" {
		return ""
	}
	return RedactedMarker
}

func maskValues(values []string) []string {
	if values == nil {
		return nil
	}
	out := make([]string, len(values))
	for i, v := range values {
		out[i] = maskValue(v)
	}
	return out
}

func maskURLs(urls []string) []string {
	if urls == nil {
		return nil
	}
	out := make([]string, len(urls))
	for i, u := range urls {
		out[i] = maskURL(u)
	}
	return out
}

func maskURL(raw string) string {
	return redactURL(raw, true)
}

func maskDoHURL(raw string) string {
	return redactURL(raw, false)
}

func redactURL(raw string, keepPath bool) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return RedactedMarker
	}

	var b strings.Builder
	b.WriteString(u.Scheme)
	b.WriteString("://")
	if u.User != nil {
		b.WriteString(RedactedMarker)
		b.WriteString("@")
	}
	b.WriteString(u.Host)

	path := u.EscapedPath()
	if keepPath || path == "" || path == "/" || path == "/dns-query" {
		b.WriteString(path)
	} else {
		b.WriteString("/")
		b.WriteString(RedactedMarker)
	}

	if u.RawQuery != "" {
		parts := strings.Split(u.RawQuery, "&")
		for i, part := range parts {
			if key, _, ok := strings.Cut(part, "="); ok {
				parts[i] = key + "=" + RedactedMarker
			} else {
				parts[i] = RedactedMarker
			}
		}
		b.WriteString("?")
		b.WriteString(strings.Join(parts, "&"))
	}
	if u.Fragment != "" || u.RawFragment != "" {
		b.WriteString("#")
		b.WriteString(RedactedMarker)
	}
	return b.String()
}
