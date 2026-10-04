package config

import (
	"encoding/json"
	"fmt"
	"net"
	"net/url"
	"sort"
	"strings"

	"github.com/daniellavrushin/b4/geodat"
)

const RedactedMarker = "[redacted]"

var placeholderPaths = map[string]bool{
	"system.web_server.username":              true,
	"system.web_server.password":              true,
	"system.web_server.tls_cert":              true,
	"system.web_server.tls_key":               true,
	"system.web_server.mcp.token":             true,
	"system.web_server.mcp.allowed_origins[]": true,
	"system.socks5.username":                  true,
	"system.socks5.password":                  true,
	"system.mtproto.secret":                   true,
	"system.mtproto.secrets[].name":           true,
	"system.mtproto.secrets[].secret":         true,
	"system.mtproto.dc_relay":                 true,
	"system.mtproto.ws_custom_domain":         true,
	"system.mtproto.cfworker_domain":          true,
	"system.mtproto.cfproxy_url":              true,
	"system.mtproto.dc_fallback_url":          true,
	"system.mtproto.web_proxy.hostname":       true,
	"system.mtproto.web_proxy.tls_cert":       true,
	"system.mtproto.web_proxy.tls_key":        true,
	"system.api.ipinfo_token":                 true,
	"system.ai.api_key_ref":                   true,
	"system.ai.endpoint":                      true,
	"system.geo.sitedat_url":                  true,
	"system.geo.ipdat_url":                    true,
	"system.update.mirrors[]":                 true,
	"system.hub.urls[]":                       true,
	"system.checker.watchdog.domains[]":       true,
	"sets[].routing.upstream.username":        true,
	"sets[].routing.upstream.password":        true,
	"sets[].dns.doh_url":                      true,
	"sets[].discovery.urls[]":                 true,
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
	for _, path := range []*string{&ws.TLSCert, &ws.TLSKey, &mt.WebProxy.TLSCert, &mt.WebProxy.TLSKey} {
		*path = maskFilePath(*path, mt.WebProxy.Hostname)
	}
	ws.MCP.AllowedOrigins = maskOrigins(ws.MCP.AllowedOrigins)

	mt.DCRelay = maskValue(mt.DCRelay)
	mt.WSCustomDomain = maskValue(mt.WSCustomDomain)
	mt.CFWorkerDomain = maskValue(mt.CFWorkerDomain)
	mt.WebProxy.Hostname = maskValue(mt.WebProxy.Hostname)
	mt.CFProxyURL = maskCustomValue(mt.CFProxyURL, TGCFProxyURL)
	mt.DCFallbackURL = maskCustomValue(mt.DCFallbackURL, TGDCFallbackURL)

	c.System.AI.Endpoint = maskURL(c.System.AI.Endpoint)
	c.System.Geo.GeoSiteURL = maskGeoURL(c.System.Geo.GeoSiteURL)
	c.System.Geo.GeoIpURL = maskGeoURL(c.System.Geo.GeoIpURL)
	c.System.Update.Mirrors = maskValues(c.System.Update.Mirrors)
	c.System.Hub.URLs = maskURLs(c.System.Hub.URLs)
	c.System.Checker.Watchdog.Domains = maskWatchdogEntries(c.System.Checker.Watchdog.Domains)

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
	return placeholderValuePaths(m)
}

func placeholderValuePaths(doc any) []string {
	var paths []string
	collectPlaceholders(doc, "", "", &paths)
	sort.Strings(paths)
	return paths
}

func collectPlaceholders(v any, path, pattern string, out *[]string) {
	switch t := v.(type) {
	case map[string]any:
		for key, child := range t {
			collectPlaceholders(child, joinKey(path, key), joinKey(pattern, key), out)
		}
	case []any:
		for i, child := range t {
			collectPlaceholders(child, fmt.Sprintf("%s[%d]", path, i), pattern+"[]", out)
		}
	case string:
		if placeholderPaths[pattern] && strings.Contains(t, RedactedMarker) {
			*out = append(*out, path)
		}
	}
}

func joinKey(base, key string) string {
	if base == "" {
		return key
	}
	return base + "." + key
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

func maskFilePath(path, host string) string {
	if path == "" {
		return ""
	}
	if host != "" {
		path = strings.ReplaceAll(path, host, RedactedMarker)
	}
	segments := strings.Split(path, "/")
	last := len(segments) - 1
	for i, s := range segments {
		name := strings.TrimPrefix(s, ".")
		if s == "." || s == ".." || !strings.Contains(name, ".") {
			continue
		}
		if i < last {
			segments[i] = RedactedMarker
		} else if strings.Count(name, ".") > 1 {
			segments[i] = RedactedMarker + s[strings.LastIndex(s, "."):]
		}
	}
	return strings.Join(segments, "/")
}

func maskOrigins(origins []string) []string {
	if origins == nil {
		return nil
	}
	out := make([]string, len(origins))
	for i, origin := range origins {
		out[i] = maskOrigin(origin)
	}
	return out
}

func maskOrigin(raw string) string {
	if raw == "" || strings.TrimSpace(raw) == "*" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return RedactedMarker
	}
	name := u.Hostname()
	if name == "localhost" || net.ParseIP(name) != nil {
		return raw
	}
	masked := u.Scheme + "://" + RedactedMarker
	if port := u.Port(); port != "" {
		masked += ":" + port
	}
	return masked
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
	return redactURL(raw, keepHost, plainPath)
}

func maskGeoURL(raw string) string {
	if geodat.IsSourceURL(raw) {
		return raw
	}
	return maskURL(raw)
}

func maskWatchdogEntries(entries []string) []string {
	if entries == nil {
		return nil
	}
	out := make([]string, len(entries))
	for i, entry := range entries {
		switch {
		case strings.Contains(entry, "://"):
			out[i] = maskURL(entry)
		case strings.ContainsAny(entry, "@?#/"):
			out[i] = strings.TrimPrefix(maskURL("https://"+entry), "https://")
		default:
			out[i] = entry
		}
	}
	return out
}

func maskDoHURL(raw string) string {
	return redactURL(raw, maskResolverHost, dohPath)
}

func keepHost(u *url.URL) string {
	return u.Host
}

func plainPath(path string) bool {
	return path == "" || path == "/"
}

func dohPath(path string) bool {
	return plainPath(path) || path == "/dns-query"
}

func redactURL(raw string, host func(*url.URL) string, keepPath func(string) bool) string {
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
	b.WriteString(host(u))

	path := u.EscapedPath()
	if keepPath(path) {
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

var knownDoHZones = func() map[string]bool {
	zones := map[string]bool{"cloudflare-gateway.com": true}
	for host := range KnownDoHHosts {
		if net.ParseIP(host) != nil {
			continue
		}
		zones[lastLabels(host, 2)] = true
	}
	return zones
}()

func lastLabels(host string, n int) string {
	labels := strings.Split(host, ".")
	if len(labels) > n {
		labels = labels[len(labels)-n:]
	}
	return strings.Join(labels, ".")
}

func maskResolverHost(u *url.URL) string {
	name := strings.ToLower(u.Hostname())
	if KnownDoHHosts[name] || net.ParseIP(name) != nil {
		return u.Host
	}
	masked := RedactedMarker
	if zone := lastLabels(name, 2); zone != name && knownDoHZones[zone] {
		masked += "." + zone
	}
	if port := u.Port(); port != "" {
		masked += ":" + port
	}
	return masked
}
