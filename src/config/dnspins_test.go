package config

import "testing"

func TestNormalizePinDomain(t *testing.T) {
	cases := map[string]string{
		"www.instagram.com":  "www.instagram.com",
		"WWW.Instagram.COM":  "www.instagram.com",
		"www.instagram.com.": "www.instagram.com",
		"*.instagram.com":    "instagram.com",
		"  instagram.com  ":  "instagram.com",
		"*.instagram.com.":   "instagram.com",
		"":                   "",
	}
	for in, want := range cases {
		if got := NormalizePinDomain(in); got != want {
			t.Errorf("NormalizePinDomain(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPinnedAddressesMatching(t *testing.T) {
	cfg := &DNSConfig{Pins: map[string][]string{
		"instagram.com":     {"1.1.1.1"},
		"www.instagram.com": {"2.2.2.2"},
	}}

	cases := map[string]string{
		"www.instagram.com": "2.2.2.2",
		"cdn.instagram.com": "1.1.1.1",
		"instagram.com":     "1.1.1.1",
		"WWW.Instagram.Com": "2.2.2.2",
	}
	for domain, want := range cases {
		got := cfg.PinnedAddresses(domain)
		if len(got) != 1 || got[0] != want {
			t.Errorf("PinnedAddresses(%q) = %v, want [%s]", domain, got, want)
		}
	}

	for _, domain := range []string{"example.com", "notinstagram.com", ""} {
		if got := cfg.PinnedAddresses(domain); got != nil {
			t.Errorf("PinnedAddresses(%q) = %v, want no match", domain, got)
		}
	}
}

func TestPinnedAddressesLongestSuffixWins(t *testing.T) {
	cfg := &DNSConfig{Pins: map[string][]string{
		"com":           {"1.1.1.1"},
		"instagram.com": {"2.2.2.2"},
	}}

	got := cfg.PinnedAddresses("cdn.instagram.com")
	if len(got) != 1 || got[0] != "2.2.2.2" {
		t.Errorf("PinnedAddresses = %v, want the most specific pin to win", got)
	}
}

func TestPinnedAddressesEmptyConfig(t *testing.T) {
	var nilCfg *DNSConfig
	if got := nilCfg.PinnedAddresses("example.com"); got != nil {
		t.Errorf("nil config returned %v", got)
	}
	if got := (&DNSConfig{}).PinnedAddresses("example.com"); got != nil {
		t.Errorf("config without pins returned %v", got)
	}
}

func TestValidateSanitizesPins(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "s1"
	set.DNS.Pins = map[string][]string{
		"*.Instagram.COM.": {"157.240.0.174", "not-an-ip", " 2a03:2880::1 "},
		"broken.example":   {"nope"},
		"  ":               {"1.1.1.1"},
	}
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}

	pins := cfg.Sets[0].DNS.Pins
	if len(pins) != 1 {
		t.Fatalf("pins = %v, want only the entry that had a usable address", pins)
	}
	got, ok := pins["instagram.com"]
	if !ok {
		t.Fatalf("pins = %v, want the key normalized to instagram.com", pins)
	}
	if len(got) != 2 || got[0] != "157.240.0.174" || got[1] != "2a03:2880::1" {
		t.Errorf("addresses = %v, want the two valid ones kept and trimmed", got)
	}
}

func TestValidateClearsPinsWhenNoneAreUsable(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id = "s1"
	set.DNS.Pins = map[string][]string{"example.com": {"nope"}}
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if cfg.Sets[0].DNS.Pins != nil {
		t.Errorf("pins = %v, want nil so the config file omits the field", cfg.Sets[0].DNS.Pins)
	}
}
