package sni

import (
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestParseHTTPHost(t *testing.T) {
	cases := []struct {
		name string
		req  string
		host string
		ok   bool
	}{
		{"plain", "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n", "example.com", true},
		{"port and case", "POST /x HTTP/1.1\r\nUser-Agent: a\r\nhOsT:  API.Example.com:8080 \r\n\r\n", "api.example.com", true},
		{"trailing dot", "HEAD / HTTP/1.1\r\nHost: example.com.\r\n\r\n", "example.com", true},
		{"no host header", "GET / HTTP/1.1\r\nAccept: */*\r\n\r\n", "", false},
		{"host after the headers end", "GET / HTTP/1.1\r\n\r\nHost: example.com\r\n", "", false},
		{"unfinished host line", "GET / HTTP/1.1\r\nHost: examp", "", false},
		{"ipv6 literal", "GET / HTTP/1.1\r\nHost: [2001:db8::1]:80\r\n\r\n", "", false},
		{"not http", "\x16\x03\x01\x00\x10Host: example.com\r\n", "", false},
		{"lookalike header", "GET / HTTP/1.1\r\nX-Host: evil.com\r\nHost: good.com\r\n\r\n", "good.com", true},
	}
	for _, tc := range cases {
		host, ok := ParseHTTPHost([]byte(tc.req))
		if host != tc.host || ok != tc.ok {
			t.Errorf("%s: got (%q, %v), want (%q, %v)", tc.name, host, ok, tc.host, tc.ok)
		}
	}
}

func TestHTTPHeadersComplete(t *testing.T) {
	if HTTPHeadersComplete([]byte("GET / HTTP/1.1\r\nHost: a.com\r\n")) {
		t.Error("headers without the blank line are not complete")
	}
	if !HTTPHeadersComplete([]byte("GET / HTTP/1.1\r\nHost: a.com\r\n\r\nbody")) {
		t.Error("headers with the blank line are complete")
	}
}

func TestSetHasDomain(t *testing.T) {
	tor := makeSetWithDomains("tor", "ipify.org", "shared.example.com", "regexp:^cdn[0-9]+\\.example\\.net$")
	tor.Id = "tor"
	other := makeSetWithDomains("other", "shared.example.com", "api.ipify.org")
	other.Id = "other"
	ss := NewSuffixSet([]*config.SetConfig{tor, other})

	cases := []struct {
		set, host string
		want      bool
	}{
		{"tor", "api.ipify.org", true},
		{"tor", "API.IPIFY.ORG.", true},
		{"other", "api.ipify.org", true},
		{"other", "www.ipify.org", false},
		{"tor", "shared.example.com", true},
		{"other", "shared.example.com", true},
		{"tor", "cdn42.example.net", true},
		{"other", "cdn42.example.net", false},
		{"tor", "ipify.org.evil.com", false},
		{"missing", "api.ipify.org", false},
		{"tor", "", false},
	}
	for _, tc := range cases {
		if got := ss.SetHasDomain(tc.set, tc.host); got != tc.want {
			t.Errorf("SetHasDomain(%q, %q) = %v, want %v", tc.set, tc.host, got, tc.want)
		}
	}
	var nilSet *SuffixSet
	if nilSet.SetHasDomain("tor", "api.ipify.org") {
		t.Error("a nil matcher matched")
	}
}
