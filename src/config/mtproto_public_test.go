package config

import (
	"strings"
	"testing"
)

func TestPublicHostIsValidatedEvenWithTheProxyOff(t *testing.T) {
	for _, tc := range []struct {
		value string
		ok    bool
	}{
		{"", true},
		{"proxy.example.org", true},
		{"  proxy.example.org  ", true},
		{"203.0.113.7", true},
		{"2001:db8::1", true},
		{"router", true},
		{"my_host.lan", true},
		{"xn--80ak6aa92e.com", true},
		{strings.Repeat("a", 63) + ".example.org", true},
		{"proxy." + strings.Repeat("b", 63), true},
		{strings.Repeat("a", 64) + ".example.org", false},
		{"proxy." + strings.Repeat("b", 64), false},
		{"https://proxy.example.org", false},
		{"proxy.example.org:443", false},
		{"proxy.example.org/proxy", false},
		{"[2001:db8::1]", false},
		{"user@host.example", false},
		{"two words.example", false},
		{"host..example", false},
		{".example", false},
		{"example.", false},
		{"пример.рф", false},
		{strings.Repeat("a", 254), false},
	} {
		cfg := NewConfig()
		cfg.System.MTProto.Enabled = false
		cfg.System.MTProto.PublicHost = tc.value
		err := cfg.Validate()
		hit := err != nil && strings.Contains(err.Error(), "public_host")
		if tc.ok && hit {
			t.Errorf("%q was refused: %v", tc.value, err)
		}
		if !tc.ok && !hit {
			t.Errorf("%q was accepted", tc.value)
		}
	}
}

func TestPublicPortRange(t *testing.T) {
	for _, tc := range []struct {
		value int
		ok    bool
	}{
		{0, true},
		{1, true},
		{443, true},
		{65535, true},
		{-1, false},
		{65536, false},
		{1 << 20, false},
	} {
		cfg := NewConfig()
		cfg.System.MTProto.PublicPort = tc.value
		err := cfg.Validate()
		hit := err != nil && strings.Contains(err.Error(), "public_port")
		if tc.ok && hit {
			t.Errorf("%d was refused: %v", tc.value, err)
		}
		if !tc.ok && !hit {
			t.Errorf("%d was accepted", tc.value)
		}
	}
}

func TestPublicPortIsNotALocalListener(t *testing.T) {
	cfg := NewConfig()
	cfg.System.MTProto.Enabled = true
	cfg.System.MTProto.Port = 7001
	cfg.System.WebServer.Port = 7000
	cfg.System.MTProto.PublicHost = "b4tgws.example.org"
	cfg.System.MTProto.PublicPort = cfg.System.WebServer.Port
	if err := cfg.Validate(); err != nil && strings.Contains(err.Error(), "public_port") {
		t.Fatalf("a public port equal to a listener port was treated as a clash: %v", err)
	}
}

func TestPublicAddressIsOmittedFromTheSparseFileWhenUnset(t *testing.T) {
	cfg := NewConfig()
	out, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(out), "public_") {
		t.Fatalf("unset public address written to the sparse file: %s", out)
	}

	cfg.System.MTProto.PublicHost = "b4tgws.example.org"
	cfg.System.MTProto.PublicPort = 443
	out, err = MarshalSparse(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"public_host"`, `"b4tgws.example.org"`, `"public_port"`, `443`} {
		if !strings.Contains(string(out), want) {
			t.Fatalf("sparse file lacks %s: %s", want, out)
		}
	}
}
