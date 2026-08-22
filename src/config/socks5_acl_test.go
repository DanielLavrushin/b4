package config

import "testing"

func TestParseSourcePrefix(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"192.168.1.10", "192.168.1.10/32"},
		{" 192.168.1.0/24 ", "192.168.1.0/24"},
		{"192.168.1.55/24", "192.168.1.0/24"},
		{"::1", "::1/128"},
		{"fd00::/8", "fd00::/8"},
		{"::ffff:192.168.1.10", "192.168.1.10/32"},
		{"::ffff:192.168.1.0/120", "192.168.1.0/24"},
	}
	for _, c := range cases {
		got, err := ParseSourcePrefix(c.in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if got.String() != c.want {
			t.Errorf("%q: got %s, want %s", c.in, got, c.want)
		}
	}
}

func TestParseSourcePrefixRejects(t *testing.T) {
	for _, in := range []string{"", "   ", "not-an-ip", "192.168.1.0/33", "192.168.1.256", "fe80::1%eth0", "fe80::%eth0/64", "fe80::/64%eth0", "192.168.1.0/-1"} {
		if p, err := ParseSourcePrefix(in); err == nil {
			t.Errorf("%q should be rejected, got %s", in, p)
		}
	}
}

func TestParseSourceACLSkipsBlanks(t *testing.T) {
	got, err := ParseSourceACL([]string{"192.168.1.0/24", "  ", "", "10.0.0.1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 prefixes, got %d (%v)", len(got), got)
	}
}

func TestParseSourceACLFailsOnFirstBadEntry(t *testing.T) {
	if _, err := ParseSourceACL([]string{"192.168.1.0/24", "garbage"}); err == nil {
		t.Fatal("a malformed entry must fail the whole list")
	}
}

func TestValidate_Socks5AllowedSources(t *testing.T) {
	t.Run("malformed entry", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Socks5.Enabled = true
		cfg.System.Socks5.AllowedSources = []string{"192.168.1.0/24", "nonsense"}
		ve := mustValidationErr(t, cfg.Validate())
		if findField(ve, "system.socks5.allowed_sources", "socks5_invalid_source") == nil {
			t.Errorf("expected socks5_invalid_source, got %v", ve.Fields)
		}
	})

	t.Run("catch-all entry", func(t *testing.T) {
		for _, entry := range []string{"0.0.0.0/0", "::/0"} {
			cfg := NewConfig()
			cfg.System.Socks5.Enabled = true
			cfg.System.Socks5.AllowedSources = []string{entry}
			ve := mustValidationErr(t, cfg.Validate())
			if findField(ve, "system.socks5.allowed_sources", "socks5_source_all") == nil {
				t.Errorf("%s: expected socks5_source_all, got %v", entry, ve.Fields)
			}
		}
	})

	t.Run("unspecified address matches no client", func(t *testing.T) {
		for _, entry := range []string{"0.0.0.0", "::", "::ffff:0.0.0.0", "0.0.0.0/8", "::/64"} {
			cfg := NewConfig()
			cfg.System.Socks5.Enabled = true
			cfg.System.Socks5.AllowedSources = []string{entry}
			ve := mustValidationErr(t, cfg.Validate())
			if findField(ve, "system.socks5.allowed_sources", "socks5_source_unspecified") == nil {
				t.Errorf("%s: expected socks5_source_unspecified, got %v", entry, ve.Fields)
			}
		}
	})

	t.Run("a catch-all keeps its own advice", func(t *testing.T) {
		for _, entry := range []string{"0.0.0.0/0", "::/0"} {
			cfg := NewConfig()
			cfg.System.Socks5.Enabled = true
			cfg.System.Socks5.AllowedSources = []string{entry}
			ve := mustValidationErr(t, cfg.Validate())
			if findField(ve, "system.socks5.allowed_sources", "socks5_source_unspecified") != nil {
				t.Errorf("%s: a catch-all must report socks5_source_all, not the unspecified-address code, got %v", entry, ve.Fields)
			}
		}
	})

	t.Run("valid list passes", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Socks5.Enabled = true
		cfg.System.Socks5.AllowedSources = []string{"192.168.1.0/24", "127.0.0.1", "fd00::/8"}
		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})

	t.Run("empty list passes", func(t *testing.T) {
		cfg := NewConfig()
		cfg.System.Socks5.Enabled = true
		if err := cfg.Validate(); err != nil {
			t.Fatalf("unexpected validation error: %v", err)
		}
	})
}
