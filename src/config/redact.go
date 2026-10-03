package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"
)

const RedactedMarker = "[redacted]"

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
	out.RedactForSharing()
	return &out, nil
}

func (c *Config) RedactCredentials() {
	ws := &c.System.WebServer
	if ws.Password != "" {
		ws.Password = ""
		ws.PasswordSet = true
	}
	ws.Username = maskValue(ws.Username)
	ws.MCP.Token = maskValue(ws.MCP.Token)

	c.System.Socks5.Username = maskValue(c.System.Socks5.Username)
	c.System.Socks5.Password = maskValue(c.System.Socks5.Password)

	mt := &c.System.MTProto
	for i := range mt.Secrets {
		mt.Secrets[i].Name = maskValue(mt.Secrets[i].Name)
		mt.Secrets[i].Secret = maskValue(mt.Secrets[i].Secret)
	}

	c.System.API.IPInfoToken = maskValue(c.System.API.IPInfoToken)
	c.System.AI.APIKeyRef = maskValue(c.System.AI.APIKeyRef)

	for _, set := range c.Sets {
		set.RedactCredentials()
	}
}

func (s *SetConfig) RedactCredentials() {
	if s == nil {
		return
	}
	s.Routing.Upstream.Username = maskValue(s.Routing.Upstream.Username)
	s.Routing.Upstream.Password = maskValue(s.Routing.Upstream.Password)
}

func (c *Config) RedactForSharing() {
	c.RedactCredentials()

	mt := &c.System.MTProto
	ws := &c.System.WebServer
	if host := mt.WebProxy.Hostname; host != "" {
		for _, path := range []*string{&ws.TLSCert, &ws.TLSKey, &mt.WebProxy.TLSCert, &mt.WebProxy.TLSKey} {
			*path = strings.ReplaceAll(*path, host, RedactedMarker)
		}
	}

	mt.DCRelay = maskValue(mt.DCRelay)
	mt.WSCustomDomain = maskValue(mt.WSCustomDomain)
	mt.CFWorkerDomain = maskValue(mt.CFWorkerDomain)
	mt.WebProxy.Hostname = maskValue(mt.WebProxy.Hostname)
	mt.CFProxyURL = maskCustomValue(mt.CFProxyURL, TGCFProxyURL)
	mt.DCFallbackURL = maskCustomValue(mt.DCFallbackURL, TGDCFallbackURL)

	c.System.AI.Endpoint = maskURL(c.System.AI.Endpoint)
	c.System.Geo.GeoSiteURL = maskURL(c.System.Geo.GeoSiteURL)
	c.System.Geo.GeoIpURL = maskURL(c.System.Geo.GeoIpURL)
	c.System.Update.Mirrors = maskValues(c.System.Update.Mirrors)
	c.System.Hub.URLs = maskURLs(c.System.Hub.URLs)

	for _, set := range c.Sets {
		if set == nil {
			continue
		}
		set.DNS.DoHURL = maskDoHURL(set.DNS.DoHURL)
		set.Discovery.URLs = maskURLs(set.Discovery.URLs)
	}
}

func (c *Config) RedactedValuePaths() []string {
	m, err := toMap(c)
	if err != nil {
		return nil
	}
	var paths []string
	collectRedactedPaths(m, "", &paths)
	sort.Strings(paths)
	return paths
}

func collectRedactedPaths(v interface{}, path string, out *[]string) {
	switch t := v.(type) {
	case map[string]interface{}:
		for key, child := range t {
			next := key
			if path != "" {
				next = path + "." + key
			}
			collectRedactedPaths(child, next, out)
		}
	case []interface{}:
		for i, child := range t {
			collectRedactedPaths(child, fmt.Sprintf("%s[%d]", path, i), out)
		}
	case string:
		if strings.Contains(t, RedactedMarker) {
			*out = append(*out, path)
		}
	}
}

func maskValue(v string) string {
	if v == "" {
		return ""
	}
	return RedactedMarker
}

func maskCustomValue(v, publicDefault string) string {
	if v == "" || v == publicDefault {
		return v
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
	if keepPath {
		b.WriteString(u.Host)
	} else {
		b.WriteString(maskSubdomain(u))
	}

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

func maskSubdomain(u *url.URL) string {
	name := u.Hostname()
	if net.ParseIP(name) != nil {
		return u.Host
	}
	labels := strings.Split(name, ".")
	if len(labels) <= 2 {
		return u.Host
	}
	masked := RedactedMarker + "." + strings.Join(labels[len(labels)-2:], ".")
	if port := u.Port(); port != "" {
		masked += ":" + port
	}
	return masked
}
