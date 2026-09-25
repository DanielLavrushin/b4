package utils

import (
	"errors"
	"net/netip"
	"testing"
)

func TestNormalizeProbeURL(t *testing.T) {
	cases := []struct {
		in, want, host string
	}{
		{"youtube.com", "https://youtube.com/", "youtube.com"},
		{"  https://www.YouTube.com  ", "https://www.youtube.com/", "www.youtube.com"},
		{"HTTP://Example.COM/Path?q=1#frag", "http://example.com/Path?q=1", "example.com"},
		{"\"https://meduza.io/\"", "https://meduza.io/", "meduza.io"},
		{"`rutracker.org/forum/index.php`", "https://rutracker.org/forum/index.php", "rutracker.org"},
		{"'x.com'", "https://x.com/", "x.com"},
		{"example.com:8443/a", "https://example.com:8443/a", "example.com"},
		{"https://[2606:4700::1111]/", "https://[2606:4700::1111]/", "2606:4700::1111"},
		{"https://1.1.1.1", "https://1.1.1.1/", "1.1.1.1"},
		{"https://example.com/?", "https://example.com/", "example.com"},
		{"https://Example.com./x", "https://example.com/x", "example.com"},
		{"2606:4700::1111", "https://[2606:4700::1111]/", "2606:4700::1111"},
		{"2606:4700::abcd", "https://[2606:4700::abcd]/", "2606:4700::abcd"},
		{"https://2606:4700::abcd/path", "https://[2606:4700::abcd]/path", "2606:4700::abcd"},
	}
	for _, c := range cases {
		got, host, err := NormalizeProbeURL(c.in)
		if err != nil {
			t.Errorf("%q: unexpected error %v", c.in, err)
			continue
		}
		if got != c.want || host != c.host {
			t.Errorf("%q: got (%q, %q), want (%q, %q)", c.in, got, host, c.want, c.host)
		}
		again, _, err := NormalizeProbeURL(got)
		if err != nil || again != got {
			t.Errorf("%q: normalising the canonical form must be stable, got %q (%v)", got, again, err)
		}
	}
}

func TestNormalizeProbeURLRefuses(t *testing.T) {
	cases := []struct {
		in   string
		want error
	}{
		{"", ErrProbeURLEmpty},
		{"  \"\"  ", ErrProbeURLEmpty},
		{"ftp://example.com/", ErrProbeURLScheme},
		{"javascript://example.com/", ErrProbeURLScheme},
		{"https://user:pass@example.com/", ErrProbeURLUserinfo},
		{"https://user@example.com/", ErrProbeURLUserinfo},
		{"https:///path", ErrProbeURLNoHost},
		{"http://localhost/", ErrProbeURLReservedHost},
		{"LOCALHOST:8080", ErrProbeURLReservedHost},
		{"https://127.0.0.1/", ErrProbeURLReservedHost},
		{"192.168.1.1", ErrProbeURLReservedHost},
		{"https://[::1]:8080/", ErrProbeURLReservedHost},
		{"https://[fe80::1]/", ErrProbeURLReservedHost},
		{"100.64.0.1", ErrProbeURLReservedHost},
		{"0.0.0.0", ErrProbeURLReservedHost},
		{"240.0.0.1", ErrProbeURLReservedHost},
		{"http://[::ffff:10.0.0.1]/", ErrProbeURLReservedHost},
		{"::1", ErrProbeURLReservedHost},
		{"https://::1/", ErrProbeURLReservedHost},
		{"fe80::1", ErrProbeURLReservedHost},
		{"fd00::1/x", ErrProbeURLReservedHost},
	}
	for _, c := range cases {
		if _, _, err := NormalizeProbeURL(c.in); !errors.Is(err, c.want) {
			t.Errorf("%q: got %v, want %v", c.in, err, c.want)
		}
	}
	for _, in := range []string{"https://exa mple.com/", "https://example.com:port/", "https://abc:def:ghi/"} {
		if _, _, err := NormalizeProbeURL(in); err == nil {
			t.Errorf("%q must not parse", in)
		}
	}
}

func TestSanitizeProbeURLs(t *testing.T) {
	var dropped []string
	got := SanitizeProbeURLs([]string{
		"",
		"youtube.com",
		"https://YOUTUBE.com/watch",
		"https://www.youtube.com/",
		"http://localhost/",
		"ftp://example.com/",
		"a.example",
		"b.example",
		"c.example",
		"d.example",
	}, func(raw string, err error) {
		dropped = append(dropped, raw)
	})

	want := []string{
		"https://youtube.com/",
		"https://www.youtube.com/",
		"https://a.example/",
		"https://b.example/",
		"https://c.example/",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("[%d] got %q, want %q", i, got[i], want[i])
		}
	}
	wantDropped := []string{"https://YOUTUBE.com/watch", "http://localhost/", "ftp://example.com/", "d.example"}
	if len(dropped) != len(wantDropped) {
		t.Fatalf("dropped %v, want %v", dropped, wantDropped)
	}
	for i := range wantDropped {
		if dropped[i] != wantDropped[i] {
			t.Errorf("dropped[%d] = %q, want %q", i, dropped[i], wantDropped[i])
		}
	}
}

func TestSanitizeProbeURLsNeverReturnsNil(t *testing.T) {
	if got := SanitizeProbeURLs(nil, nil); got == nil || len(got) != 0 {
		t.Errorf("an empty input must give an empty, non-nil list, got %#v", got)
	}
	if got := SanitizeProbeURLs([]string{"http://127.0.0.1/"}, nil); got == nil || len(got) != 0 {
		t.Errorf("a list of refused URLs must give an empty, non-nil list, got %#v", got)
	}
}

func TestIsReservedAddrCoversTheWholeLocalSpace(t *testing.T) {
	reserved := []string{
		"127.0.0.1", "::1",
		"10.1.2.3", "192.168.1.1", "172.16.0.1",
		"169.254.1.1", "fe80::1",
		"100.64.0.1", "100.127.255.255",
		"0.0.0.0", "::",
		"224.0.0.1", "ff02::1",
		"240.0.0.1",
		"fd00::1",
		"::ffff:192.168.1.1",
	}
	for _, s := range reserved {
		if !IsReservedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s must be refused", s)
		}
	}
	public := []string{"1.1.1.1", "8.8.8.8", "104.22.45.1", "2606:4700::1111", "100.63.255.255", "100.128.0.1"}
	for _, s := range public {
		if IsReservedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s is a public address and must not be refused", s)
		}
	}
}

func TestIsReservedHost(t *testing.T) {
	for _, in := range []string{"localhost", "LocalHost.", "127.0.0.1", "[::1]", "10.0.0.1", " 192.168.0.1 "} {
		if !IsReservedHost(in) {
			t.Errorf("%q must be reserved", in)
		}
	}
	for _, in := range []string{"", "example.com", "1.1.1.1", "localhost.example.com", "[2606:4700::1111]"} {
		if IsReservedHost(in) {
			t.Errorf("%q must not be reserved", in)
		}
	}
}
