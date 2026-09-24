package utils

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

const MaxProbeURLs = 5

var (
	ErrProbeURLEmpty        = errors.New("empty URL")
	ErrProbeURLScheme       = errors.New("only http and https URLs can be probed")
	ErrProbeURLUserinfo     = errors.New("a probe URL cannot carry credentials")
	ErrProbeURLNoHost       = errors.New("the URL names no host")
	ErrProbeURLReservedHost = errors.New("the host is a private or local address")
	ErrProbeURLDuplicate    = errors.New("another URL already probes this host")
	ErrProbeURLTooMany      = fmt.Errorf("at most %d probe URLs are kept", MaxProbeURLs)
)

func IsReservedAddr(addr netip.Addr) bool {
	addr = addr.Unmap()
	switch {
	case !addr.IsValid(),
		addr.IsLoopback(),
		addr.IsUnspecified(),
		addr.IsLinkLocalUnicast(),
		addr.IsLinkLocalMulticast(),
		addr.IsMulticast(),
		addr.IsInterfaceLocalMulticast(),
		addr.IsPrivate():
		return true
	}
	if addr.Is4() {
		b := addr.As4()
		if b[0] == 100 && b[1] >= 64 && b[1] <= 127 {
			return true
		}
		if b[0] == 0 || b[0] >= 240 {
			return true
		}
	}
	return false
}

func IsReservedHost(host string) bool {
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(strings.TrimSpace(host), "[]")), ".")
	if host == "" {
		return false
	}
	if host == "localhost" {
		return true
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return IsReservedAddr(addr)
	}
	return false
}

func NormalizeProbeURL(raw string) (string, string, error) {
	s := strings.TrimSpace(strings.Trim(strings.TrimSpace(raw), "\"'`"))
	if s == "" {
		return "", "", ErrProbeURLEmpty
	}
	if !strings.Contains(s, "://") {
		s = "https://" + s
	}
	u, err := url.Parse(s)
	if err != nil {
		return "", "", fmt.Errorf("%q is not a URL: %w", raw, err)
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return "", "", ErrProbeURLScheme
	}
	if u.User != nil {
		return "", "", ErrProbeURLUserinfo
	}
	if !strings.HasPrefix(u.Host, "[") && strings.Count(u.Host, ":") > 1 {
		addr, err := netip.ParseAddr(u.Host)
		if err != nil {
			return "", "", ErrProbeURLNoHost
		}
		u.Host = "[" + addr.String() + "]"
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return "", "", ErrProbeURLNoHost
	}
	if IsReservedHost(host) {
		return "", "", ErrProbeURLReservedHost
	}

	hostPort := host
	if strings.Contains(host, ":") {
		hostPort = "[" + host + "]"
	}
	if port := u.Port(); port != "" {
		hostPort = net.JoinHostPort(host, port)
	}

	u.Scheme = scheme
	u.Host = hostPort
	u.Fragment = ""
	u.RawFragment = ""
	u.ForceQuery = false
	if u.Path == "" {
		u.Path = "/"
		u.RawPath = ""
	}
	return u.String(), host, nil
}

func SanitizeProbeURLs(raw []string, onDrop func(raw string, err error)) []string {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	for _, entry := range raw {
		if strings.TrimSpace(entry) == "" {
			continue
		}
		canonical, host, err := NormalizeProbeURL(entry)
		switch {
		case err != nil:
		case seen[host]:
			err = ErrProbeURLDuplicate
		case len(out) >= MaxProbeURLs:
			err = ErrProbeURLTooMany
		}
		if err != nil {
			if onDrop != nil {
				onDrop(entry, err)
			}
			continue
		}
		seen[host] = true
		out = append(out, canonical)
	}
	return out
}
