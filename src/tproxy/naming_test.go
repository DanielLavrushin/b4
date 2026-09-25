package tproxy

import (
	"bytes"
	"crypto/tls"
	"io"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/dns"
)

func realClientHello(t *testing.T, serverName string) []byte {
	t.Helper()
	c, s := net.Pipe()
	defer s.Close()
	go func() {
		defer c.Close()
		_ = tls.Client(c, &tls.Config{ServerName: serverName, InsecureSkipVerify: true}).Handshake()
	}()
	_ = s.SetReadDeadline(time.Now().Add(5 * time.Second))
	head := make([]byte, tlsRecordHeader)
	if _, err := io.ReadFull(s, head); err != nil {
		t.Fatalf("read record header: %v", err)
	}
	body := make([]byte, int(head[3])<<8|int(head[4]))
	if _, err := io.ReadFull(s, body); err != nil {
		t.Fatalf("read record body: %v", err)
	}
	return append(head, body...)
}

func sniffOver(t *testing.T, write func(w net.Conn)) (sniffResult, time.Duration) {
	t.Helper()
	client, server := net.Pipe()
	defer server.Close()
	go func() {
		write(client)
	}()
	defer client.Close()
	start := time.Now()
	res := sniffClient(server, 150*time.Millisecond, time.Second, sniffMaxBytes)
	return res, time.Since(start)
}

func TestSniffClientReadsAHelloSplitAcrossWrites(t *testing.T) {
	hello := realClientHello(t, "api.ipify.org")
	if len(hello) < 600 {
		t.Fatalf("expected a full-size ClientHello, got %d bytes", len(hello))
	}
	res, _ := sniffOver(t, func(w net.Conn) {
		_, _ = w.Write(hello[:200])
		time.Sleep(60 * time.Millisecond)
		_, _ = w.Write(hello[200:])
	})
	if res.host != "api.ipify.org" {
		t.Fatalf("host = %q, want api.ipify.org", res.host)
	}
	if !bytes.Equal(res.prefix, hello) {
		t.Fatalf("prefix is %d bytes, want the whole %d-byte hello", len(res.prefix), len(hello))
	}
	if res.tlsVersion == 0 {
		t.Fatal("expected the hello's TLS version")
	}
}

func TestSniffClientTakesTheHTTPHost(t *testing.T) {
	req := "GET / HTTP/1.1\r\nUser-Agent: curl\r\nHost: Example.COM:8080\r\n\r\n"
	res, _ := sniffOver(t, func(w net.Conn) {
		_, _ = w.Write([]byte(req[:20]))
		time.Sleep(30 * time.Millisecond)
		_, _ = w.Write([]byte(req[20:]))
	})
	if res.host != "example.com" {
		t.Fatalf("host = %q, want example.com", res.host)
	}
	if string(res.prefix) != req {
		t.Fatalf("prefix = %q, want the whole request", res.prefix)
	}
}

func TestSniffClientGivesUpOnASilentPeer(t *testing.T) {
	res, took := sniffOver(t, func(w net.Conn) {
		_, _ = w.Read(make([]byte, 1))
	})
	if len(res.prefix) != 0 || res.host != "" {
		t.Fatalf("expected nothing from a silent peer, got %q / %q", res.prefix, res.host)
	}
	if took > 350*time.Millisecond {
		t.Fatalf("waited %v for a silent peer, the first-byte wait is 150ms", took)
	}
}

func TestSniffClientKeepsBytesItCannotName(t *testing.T) {
	junk := []byte("SSH-2.0-OpenSSH_9.6\r\n")
	res, _ := sniffOver(t, func(w net.Conn) { _, _ = w.Write(junk) })
	if res.host != "" {
		t.Fatalf("host = %q, want none", res.host)
	}
	if !bytes.Equal(res.prefix, junk) {
		t.Fatalf("prefix = %q, want %q", res.prefix, junk)
	}
}

func TestSniffedHostRejectsAddressesAndBareNames(t *testing.T) {
	for _, h := range []string{"1.2.3.4", "localhost", "[::1]:80"} {
		req := []byte("GET / HTTP/1.1\r\nHost: " + h + "\r\n\r\n")
		if got, _ := sniffedHost(req); got != "" {
			t.Errorf("Host %q sniffed as %q, want nothing", h, got)
		}
	}
}

func containsHost(hosts []string, host string) bool {
	for _, h := range hosts {
		if h == host {
			return true
		}
	}
	return false
}

func TestChooseTarget(t *testing.T) {
	inSet := func(names ...string) func(string) bool {
		return func(h string) bool { return containsHost(names, h) }
	}
	cases := []struct {
		name       string
		in         targetInputs
		wantHost   string
		wantSource string
	}{
		{
			name: "nothing known sends the address",
			in:   targetInputs{},
		},
		{
			name:       "the client's own resolved name is sent when nothing was sniffed",
			in:         targetInputs{names: dns.NameMatches{Own: []string{"vps.example.net"}}},
			wantHost:   "vps.example.net",
			wantSource: "dns",
		},
		{
			name:     "a sniffed name that no DNS answer or set domain backs keeps the address",
			in:       targetInputs{sniffed: "www.microsoft.com", names: dns.NameMatches{Own: []string{"vps.example.net"}}},
			wantHost: "",
		},
		{
			name:       "an SNI among the resolved names picks that name",
			in:         targetInputs{sniffed: "b.example.com", names: dns.NameMatches{Own: []string{"a.example.com"}, Others: []string{"b.example.com"}}},
			wantHost:   "b.example.com",
			wantSource: "sni",
		},
		{
			name:       "another device's name never beats the client's own SNI",
			in:         targetInputs{sniffed: "www.youtube.com", names: dns.NameMatches{Others: []string{"mail.google.com"}}, inSet: inSet("www.youtube.com")},
			wantHost:   "www.youtube.com",
			wantSource: "sni",
		},
		{
			name:     "another device's name is not sent for a connection with no name of its own",
			in:       targetInputs{names: dns.NameMatches{Others: []string{"evil.example"}}},
			wantHost: "",
		},
		{
			name:       "an SNI with no DNS record is used when the set lists it",
			in:         targetInputs{sniffed: "api.ipify.org", inSet: inSet("api.ipify.org")},
			wantHost:   "api.ipify.org",
			wantSource: "sni",
		},
		{
			name:       "the set's learned name comes before b4's own resolution",
			in:         targetInputs{learned: "myip.dk", names: dns.NameMatches{Local: []string{"other.myip.dk"}}},
			wantHost:   "myip.dk",
			wantSource: "learned",
		},
		{
			name:       "b4's own resolution names a server-first connection",
			in:         targetInputs{names: dns.NameMatches{Local: []string{"mail.example.com"}}},
			wantHost:   "mail.example.com",
			wantSource: "dns",
		},
		{
			name:     "a pinned name keeps the address",
			in:       targetInputs{names: dns.NameMatches{Own: []string{"pinned.example.com"}}, pinned: func(h string) bool { return h == "pinned.example.com" }},
			wantHost: "",
		},
	}
	for _, tc := range cases {
		host, source := chooseTarget(tc.in)
		if host != tc.wantHost || source != tc.wantSource {
			t.Errorf("%s: got (%q, %q), want (%q, %q)", tc.name, host, source, tc.wantHost, tc.wantSource)
		}
	}
}

func TestNeedsSniffOnlyWhenItCanChangeTheName(t *testing.T) {
	cases := []struct {
		name       string
		names      dns.NameMatches
		setDomains bool
		want       bool
	}{
		{"one own name settles it", dns.NameMatches{Own: []string{"a.example.com"}}, true, false},
		{"several own names", dns.NameMatches{Own: []string{"a.example.com", "b.example.com"}}, false, true},
		{"only other devices' names", dns.NameMatches{Others: []string{"a.example.com"}}, false, true},
		{"no names but the set lists domains", dns.NameMatches{}, true, true},
		{"no names and an address-only set", dns.NameMatches{}, false, false},
	}
	for _, tc := range cases {
		if got := needsSniff(tc.names, tc.setDomains); got != tc.want {
			t.Errorf("%s: needsSniff = %v, want %v", tc.name, got, tc.want)
		}
	}
}
