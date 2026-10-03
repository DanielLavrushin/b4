package endpoint

import (
	"errors"
	"fmt"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
)

type Transport string

const (
	UDP    Transport = "udp"
	TCP    Transport = "tcp"
	TCPUDP Transport = "tcp+udp"
	HTTPS  Transport = "https"
)

const DefaultPort = 53

type Endpoint struct {
	Transport Transport
	Addr      netip.AddrPort
	URL       string
}

func Parse(raw string) (Endpoint, error) {
	s := strings.TrimSpace(raw)
	if s == "" {
		return Endpoint{}, errors.New("no DNS server given")
	}
	scheme, rest, hasScheme := strings.Cut(s, "://")
	if !hasScheme {
		return plain(UDP, s)
	}
	switch strings.ToLower(scheme) {
	case "udp":
		return plain(UDP, rest)
	case "tcp":
		return plain(TCP, rest)
	case "tcp+udp":
		return plain(TCPUDP, rest)
	case "https":
		return dohURL(s)
	case "http":
		return Endpoint{}, errors.New("DNS over HTTPS needs an https:// URL")
	case "tls", "dot", "quic", "doq", "h3", "sdns":
		return Endpoint{}, fmt.Errorf("%s:// is not supported; run a local DNS proxy for it and give the proxy's address", strings.ToLower(scheme))
	}
	return Endpoint{}, fmt.Errorf("unknown scheme %s://; use udp://, tcp://, tcp+udp:// or https://", scheme)
}

func plain(t Transport, s string) (Endpoint, error) {
	s = strings.TrimSuffix(strings.TrimSpace(s), "/")
	if s == "" {
		return Endpoint{}, errors.New("no address given")
	}
	if ap, err := netip.ParseAddrPort(s); err == nil {
		return checked(t, ap)
	}
	host := strings.TrimSuffix(strings.TrimPrefix(s, "["), "]")
	if addr, err := netip.ParseAddr(host); err == nil {
		return checked(t, netip.AddrPortFrom(addr, DefaultPort))
	}
	if strings.HasPrefix(s, "[") || strings.Count(s, ":") > 1 {
		return Endpoint{}, fmt.Errorf("%q is not an IP address with an optional port", s)
	}
	name, port, hasPort := strings.Cut(s, ":")
	if hasPort {
		if _, err := strconv.ParseUint(port, 10, 16); err != nil {
			return Endpoint{}, fmt.Errorf("%q has an invalid port", s)
		}
	}
	return Endpoint{}, fmt.Errorf("%q is a host name; plain DNS needs an IP address, only an https:// URL takes a name", name)
}

func checked(t Transport, ap netip.AddrPort) (Endpoint, error) {
	addr := ap.Addr()
	switch {
	case addr.Zone() != "":
		return Endpoint{}, errors.New("an IPv6 address with a zone is not supported")
	case addr.IsUnspecified():
		return Endpoint{}, fmt.Errorf("%s is not a server address", addr)
	case ap.Port() == 0:
		return Endpoint{}, errors.New("port 0 is not a server port")
	}
	return Endpoint{Transport: t, Addr: netip.AddrPortFrom(addr.Unmap(), ap.Port())}, nil
}

func dohURL(s string) (Endpoint, error) {
	u, err := url.Parse(s)
	if err != nil || u.Hostname() == "" {
		return Endpoint{}, fmt.Errorf("%q is not a valid https:// URL", s)
	}
	if u.User != nil {
		return Endpoint{}, errors.New("a DNS over HTTPS URL cannot carry a user name or password")
	}
	if port := u.Port(); port != "" {
		if n, err := strconv.ParseUint(port, 10, 16); err != nil || n == 0 {
			return Endpoint{}, fmt.Errorf("%q has an invalid port", s)
		}
	}
	u.Scheme = "https"
	u.Fragment = ""
	if u.Path == "" || u.Path == "/" {
		u.Path = "/dns-query"
	}
	return Endpoint{Transport: HTTPS, URL: u.String()}, nil
}

func (e Endpoint) IsZero() bool {
	return e.Transport == ""
}

func (e Endpoint) String() string {
	switch e.Transport {
	case "":
		return ""
	case HTTPS:
		return e.URL
	case UDP:
		return hostPort(e.Addr, false)
	}
	return string(e.Transport) + "://" + hostPort(e.Addr, true)
}

func hostPort(ap netip.AddrPort, bracket bool) string {
	switch {
	case ap.Port() != DefaultPort:
		return ap.String()
	case bracket && ap.Addr().Is6():
		return "[" + ap.Addr().String() + "]"
	}
	return ap.Addr().String()
}
