package endpoint

import (
	"strings"
	"testing"
)

func TestParseAcceptsTheUsualForms(t *testing.T) {
	cases := []struct {
		in        string
		transport Transport
		canonical string
	}{
		{"9.9.9.9", UDP, "9.9.9.9"},
		{" 9.9.9.9 ", UDP, "9.9.9.9"},
		{"127.0.0.1:53053", UDP, "127.0.0.1:53053"},
		{"9.9.9.9:53", UDP, "9.9.9.9"},
		{"2620:fe::fe", UDP, "2620:fe::fe"},
		{"[2620:fe::fe]", UDP, "2620:fe::fe"},
		{"[::1]:5353", UDP, "[::1]:5353"},
		{"udp://8.8.8.8", UDP, "8.8.8.8"},
		{"UDP://8.8.8.8:5353/", UDP, "8.8.8.8:5353"},
		{"tcp://9.9.9.9", TCP, "tcp://9.9.9.9"},
		{"tcp://[2001:db8::1]", TCP, "tcp://[2001:db8::1]"},
		{"tcp://2001:db8::1", TCP, "tcp://[2001:db8::1]"},
		{"tcp+udp://127.0.0.1:53053", TCPUDP, "tcp+udp://127.0.0.1:53053"},
		{"::ffff:1.2.3.4", UDP, "1.2.3.4"},
		{"https://dns.google/dns-query", HTTPS, "https://dns.google/dns-query"},
		{"https://dns.google", HTTPS, "https://dns.google/dns-query"},
		{"HTTPS://1.1.1.1/dns-query", HTTPS, "https://1.1.1.1/dns-query"},
		{"https://dns.example:8443/q#frag", HTTPS, "https://dns.example:8443/q"},
	}
	for _, tc := range cases {
		ep, err := Parse(tc.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.in, err)
			continue
		}
		if ep.Transport != tc.transport || ep.String() != tc.canonical {
			t.Errorf("Parse(%q) = %s %q, want %s %q", tc.in, ep.Transport, ep.String(), tc.transport, tc.canonical)
		}
		again, err := Parse(ep.String())
		if err != nil || again != ep {
			t.Errorf("the canonical form %q of %q does not parse back to the same endpoint: %+v, %v", ep.String(), tc.in, again, err)
		}
	}
}

func TestParseRejectsWithAReason(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "no DNS server"},
		{"   ", "no DNS server"},
		{"dns.google", "host name"},
		{"udp://dns.google:53", "host name"},
		{"tcp://", "no address"},
		{"http://dns.google/dns-query", "https://"},
		{"tls://1.1.1.1", "not supported"},
		{"quic://dns.adguard-dns.com", "not supported"},
		{"sdns://AgcAAAAAAAAA", "not supported"},
		{"ftp://1.1.1.1", "unknown scheme"},
		{"0.0.0.0", "not a server address"},
		{"[::]:53", "not a server address"},
		{"9.9.9.9:0", "port 0"},
		{"9.9.9.9:99999", "invalid port"},
		{"fe80::1%eth0", "zone"},
		{"https://", "not a valid"},
		{"https://user:pass@dns.example/dns-query", "user name"},
		{"https://dns.example:0/dns-query", "invalid port"},
		{"1.2.3", "host name"},
		{"[1.2.3.4]:53:53", "not an IP address"},
	}
	for _, tc := range cases {
		ep, err := Parse(tc.in)
		if err == nil {
			t.Errorf("Parse(%q) = %+v, want an error", tc.in, ep)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Parse(%q) error %q, want it to mention %q", tc.in, err, tc.want)
		}
	}
}

func TestZeroEndpoint(t *testing.T) {
	var ep Endpoint
	if !ep.IsZero() || ep.String() != "" {
		t.Fatalf("the zero endpoint means no server, got %+v %q", ep, ep.String())
	}
}
