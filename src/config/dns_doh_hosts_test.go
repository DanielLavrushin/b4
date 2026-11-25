package config

import (
	"slices"
	"testing"
)

func dohSet(doh string, upstreams ...string) *SetConfig {
	set := NewSetConfig()
	set.Enabled = true
	set.DNS.Enabled = true
	set.DNS.DoHURL = doh
	set.DNS.Upstreams = upstreams
	return &set
}

func TestDoHHostsAreTheNamesASetsResolversAreReachedBy(t *testing.T) {
	for _, tc := range []struct {
		name string
		set  *SetConfig
		want []string
	}{
		{"a host name", dohSet("https://dns.example/dns-query"), []string{"dns.example"}},
		{"a port, capitals and a trailing dot", dohSet("https://DNS.Example.:8443/dns-query"), []string{"dns.example"}},
		{"no path", dohSet("https://dns.example"), []string{"dns.example"}},
		{"a query straight after the host", dohSet("https://dns.example?x=1"), []string{"dns.example"}},
		{"a user name", dohSet("https://me@dns.example/dns-query"), []string{"dns.example"}},
		{"addresses", dohSet("https://192.0.2.5/dns-query", "https://[2001:db8::5]:8443/dns-query", "192.0.2.6"), nil},
		{"the list, once each", dohSet("https://192.0.2.5/dns-query", "https://a.example/dns-query", "https://b.example/x", "https://a.example/y"), []string{"a.example", "b.example"}},
		{"a list without a first resolver", dohSet("", "https://a.example/dns-query"), []string{"a.example"}},
		{"nothing", dohSet(""), nil},
	} {
		if got := tc.set.DNS.DoHHosts(); !slices.Equal(got, tc.want) {
			t.Errorf("%s: %q, want %q", tc.name, got, tc.want)
		}
	}

	off := dohSet("https://dns.example/dns-query")
	off.DNS.Enabled = false
	if got := off.DNS.DoHHosts(); got != nil {
		t.Errorf("a set with the redirect off asks nobody, got %q", got)
	}
	var none *DNSConfig
	if got := none.DoHHosts(); got != nil {
		t.Errorf("no DNS settings, got %q", got)
	}
}

func TestIsDoHHostLooksAtEveryActiveSet(t *testing.T) {
	disabled := dohSet("https://disabled.example/dns-query")
	disabled.Enabled = false
	off := dohSet("https://off.example/dns-query")
	off.DNS.Enabled = false
	cfg := NewConfig()
	cfg.Sets = []*SetConfig{nil, disabled, off, dohSet("https://First.Example:443/dns-query", "192.0.2.6", "https://second.example/dns-query", "https://me@third.example/dns-query")}

	for name, want := range map[string]bool{
		"first.example":     true,
		"second.example":    true,
		"third.example":     true,
		"first.exam":        false,
		"me":                false,
		"www.first.example": false,
		"example":           false,
		"irst.example":      false,
		"disabled.example":  false,
		"off.example":       false,
		"":                  false,
	} {
		if got := cfg.IsDoHHost(name); got != want {
			t.Errorf("IsDoHHost(%q) = %v, want %v", name, got, want)
		}
	}
	var none *Config
	if none.IsDoHHost("first.example") {
		t.Error("no config knows no host")
	}
}
