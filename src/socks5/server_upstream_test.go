package socks5

import (
	"context"
	"errors"
	"io"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type fakeUpstreams struct {
	mu      sync.Mutex
	calls   []string
	target  string
	handled bool
}

func (f *fakeUpstreams) DialViaSet(setID, host string, port int) (net.Conn, bool, error) {
	f.mu.Lock()
	f.calls = append(f.calls, setID+" "+net.JoinHostPort(host, strconv.Itoa(port)))
	f.mu.Unlock()
	if !f.handled {
		return nil, false, nil
	}
	conn, err := net.DialTimeout("tcp", f.target, 2*time.Second)
	return conn, true, err
}

func (f *fakeUpstreams) seen() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func startGreeter(t *testing.T, greeting string) string {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			_, _ = c.Write([]byte(greeting))
			_ = c.Close()
		}
	}()
	return ln.Addr().String()
}

func torSet(mode string) *config.SetConfig {
	set := config.NewSetConfig()
	set.Id = "tor"
	set.Name = "TOR"
	set.Enabled = true
	set.Routing.Enabled = true
	set.Routing.Mode = mode
	set.Routing.Upstream.Host = "192.0.2.10"
	set.Routing.Upstream.Port = 9050
	set.Targets.SNIDomains = []string{"blocked.example"}
	set.Targets.DomainsToMatch = []string{"blocked.example"}
	return &set
}

func startUpstreamTestServer(t *testing.T, up UpstreamDialer, sets ...*config.SetConfig) int {
	t.Helper()
	cfg := &config.Config{}
	cfg.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: "127.0.0.1", Port: freePort(t)}
	cfg.Sets = sets
	s := NewServer(cfg)
	s.SetUpstreamDialer(up)
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return cfg.System.Socks5.Port
}

func fetchThrough(t *testing.T, serverPort int, host string, port int) (string, error) {
	t.Helper()
	conn, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(serverPort)), 3*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	if err := clientGreet(conn, "", ""); err != nil {
		return "", err
	}
	if err := clientConnect(conn, host, port); err != nil {
		return "", err
	}
	got, err := io.ReadAll(conn)
	return string(got), err
}

func TestAHostnameOfAProxySetGoesToTheSetsUpstream(t *testing.T) {
	up := &fakeUpstreams{handled: true, target: startGreeter(t, "via-upstream")}
	port := startUpstreamTestServer(t, up, torSet(config.RoutingModeProxy))

	got, err := fetchThrough(t, port, "www.blocked.example", 443)
	if err != nil {
		t.Fatalf("CONNECT: %v", err)
	}
	if got != "via-upstream" {
		t.Fatalf("the client reached %q, want the set's upstream", got)
	}
	if calls := up.seen(); len(calls) != 1 || calls[0] != "tor www.blocked.example:443" {
		t.Fatalf("upstream dialer calls %v, want one for the name the client asked for", calls)
	}
}

func TestOtherDestinationsKeepTheDirectDial(t *testing.T) {
	direct := startGreeter(t, "direct")
	host, portStr, _ := net.SplitHostPort(direct)
	directPort, _ := strconv.Atoi(portStr)

	scoped := torSet(config.RoutingModeProxy)
	scoped.Targets.SourceDevices = []string{"AA:BB:CC:DD:EE:FF"}
	disabled := torSet(config.RoutingModeProxy)
	disabled.Routing.Enabled = false
	selfLoop := torSet(config.RoutingModeProxy)
	selfLoop.Routing.Upstream.Host = "127.0.0.1"
	selfByName := torSet(config.RoutingModeProxy)
	selfByName.Routing.Upstream.Host = "localhost"
	domainOnly := torSet(config.RoutingModeProxy)
	domainOnly.Targets.DomainOnly = true

	cases := []struct {
		name string
		set  *config.SetConfig
		host string
		self bool
	}{
		{"an address, not a name", torSet(config.RoutingModeProxy), host, false},
		{"a name no set lists", torSet(config.RoutingModeProxy), "localhost", false},
		{"an interface set", withDomain(torSet(config.RoutingModeInterface), "localhost"), "localhost", false},
		{"a set limited to source devices", withDomain(scoped, "localhost"), "localhost", false},
		{"a set with routing off", withDomain(disabled, "localhost"), "localhost", false},
		{"a set whose upstream is this server", withDomain(selfLoop, "localhost"), "localhost", true},
		{"a set whose upstream names this server", withDomain(selfByName, "localhost"), "localhost", true},
		{"a set with domain-only matching", withDomain(domainOnly, "localhost"), "localhost", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			up := &fakeUpstreams{handled: true, target: startGreeter(t, "via-upstream")}
			cfg := &config.Config{}
			cfg.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: "127.0.0.1", Port: freePort(t)}
			if tc.self {
				tc.set.Routing.Upstream.Port = cfg.System.Socks5.Port
			}
			cfg.Sets = []*config.SetConfig{tc.set}
			s := NewServer(cfg)
			s.SetUpstreamDialer(up)
			if err := s.Start(); err != nil {
				t.Fatalf("start: %v", err)
			}
			t.Cleanup(func() { _ = s.Stop() })

			got, err := fetchThrough(t, cfg.System.Socks5.Port, tc.host, directPort)
			if err != nil {
				t.Fatalf("CONNECT: %v", err)
			}
			if got != "direct" || len(up.seen()) != 0 {
				t.Fatalf("reached %q with upstream calls %v, want the direct dial", got, up.seen())
			}
		})
	}
}

func TestAnAbsoluteNameMatchesLikeTheBareOne(t *testing.T) {
	up := &fakeUpstreams{handled: true, target: startGreeter(t, "via-upstream")}
	port := startUpstreamTestServer(t, up, torSet(config.RoutingModeProxy))

	got, err := fetchThrough(t, port, "WWW.Blocked.Example.", 443)
	if err != nil {
		t.Fatalf("CONNECT: %v", err)
	}
	if got != "via-upstream" {
		t.Fatalf("the client reached %q, want the set's upstream", got)
	}
	if calls := up.seen(); len(calls) != 1 || calls[0] != "tor www.blocked.example:443" {
		t.Fatalf("upstream dialer calls %v", calls)
	}
}

func TestASetWithoutAListenerKeepsTheDirectDial(t *testing.T) {
	direct := startGreeter(t, "direct")
	_, portStr, _ := net.SplitHostPort(direct)
	directPort, _ := strconv.Atoi(portStr)
	up := &fakeUpstreams{}
	port := startUpstreamTestServer(t, up, withDomain(torSet(config.RoutingModeProxy), "localhost"))

	got, err := fetchThrough(t, port, "localhost", directPort)
	if err != nil {
		t.Fatalf("CONNECT: %v", err)
	}
	if got != "direct" || len(up.seen()) != 1 {
		t.Fatalf("reached %q after %v, want one declined upstream call and the direct dial", got, up.seen())
	}
}

func withDomain(set *config.SetConfig, domain string) *config.SetConfig {
	set.Targets.SNIDomains = []string{domain}
	set.Targets.DomainsToMatch = []string{domain}
	return set
}

func TestAnUpstreamNameThatDoesNotResolveKeepsTheSetsPolicy(t *testing.T) {
	up := &fakeUpstreams{handled: true, target: startGreeter(t, "via-upstream")}
	cfg := &config.Config{}
	cfg.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: "127.0.0.1", Port: freePort(t)}
	set := torSet(config.RoutingModeProxy)
	set.Routing.Upstream.Host = "upstream.b4-test.invalid"
	set.Routing.Upstream.Port = cfg.System.Socks5.Port
	cfg.Sets = []*config.SetConfig{set}
	s := NewServer(cfg)
	s.SetUpstreamDialer(up)
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	if _, err := fetchThrough(t, cfg.System.Socks5.Port, "www.blocked.example", 443); err != nil {
		t.Fatalf("CONNECT: %v", err)
	}
	if calls := up.seen(); len(calls) != 1 {
		t.Fatalf("an upstream name that failed to resolve was taken for this server and the set's upstream was skipped, calls %v", calls)
	}
}

func TestSelfDetectionFollowsTheBindAddress(t *testing.T) {
	serverAt := func(bind string) *Server {
		cfg := &config.Config{}
		cfg.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: bind, Port: 1080}
		return NewServer(cfg)
	}
	up := func(host string) config.UpstreamProxyConfig {
		return config.UpstreamProxyConfig{Host: host, Port: 1080}
	}

	cases := []struct {
		bind, host string
		self       bool
	}{
		{"127.0.0.1", "127.0.0.1", true},
		{"127.0.0.1", "localhost", true},
		{"127.0.0.1", "0.0.0.0", true},
		{"127.0.0.1", "127.0.0.2", false},
		{"127.0.0.1", "::", true},
		{"::1", "::", true},
		{"::1", "0.0.0.0", false},
		{"192.0.2.1", "127.0.0.1", false},
		{"0.0.0.0", "127.0.0.2", true},
		{"0.0.0.0", "::1", true},
		{"", "127.0.0.1", true},
		{"0.0.0.0", "192.0.2.77", false},
	}
	for _, tc := range cases {
		if got := serverAt(tc.bind).upstreamIsSelf(up(tc.host)); got != tc.self {
			t.Errorf("bind %q, upstream %q: self = %v, want %v", tc.bind, tc.host, got, tc.self)
		}
	}
	if serverAt("0.0.0.0").upstreamIsSelf(config.UpstreamProxyConfig{Host: "127.0.0.1", Port: 9050}) {
		t.Error("an upstream on another port is never this server")
	}
}

type selfDialingUpstreams struct {
	mu     sync.Mutex
	calls  int
	server int
}

func (f *selfDialingUpstreams) DialViaSet(setID, host string, port int) (net.Conn, bool, error) {
	f.mu.Lock()
	f.calls++
	calls := f.calls
	f.mu.Unlock()
	if calls > 5 {
		return nil, true, errors.New("hand-off recursion")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	conn, err := DialUpstream(ctx, ClientConfig{Host: "127.0.0.1", Port: f.server, Timeout: 3 * time.Second}, host, port)
	return conn, true, err
}

func TestAHandOffThatComesBackToThisServerIsNotHandedOffAgain(t *testing.T) {
	direct := startGreeter(t, "direct")
	_, portStr, _ := net.SplitHostPort(direct)
	directPort, _ := strconv.Atoi(portStr)

	cfg := &config.Config{}
	cfg.System.Socks5 = config.Socks5Config{Enabled: true, BindAddress: "127.0.0.1", Port: freePort(t)}
	set := withDomain(torSet(config.RoutingModeProxy), "localhost")
	set.Routing.Upstream.Host = "127.0.0.2"
	set.Routing.Upstream.Port = cfg.System.Socks5.Port
	cfg.Sets = []*config.SetConfig{set}
	up := &selfDialingUpstreams{server: cfg.System.Socks5.Port}
	s := NewServer(cfg)
	s.SetUpstreamDialer(up)
	if err := s.Start(); err != nil {
		t.Fatalf("start: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	got, err := fetchThrough(t, cfg.System.Socks5.Port, "localhost", directPort)
	if err != nil {
		t.Fatalf("CONNECT: %v", err)
	}
	up.mu.Lock()
	calls := up.calls
	up.mu.Unlock()
	if got != "direct" || calls != 1 {
		t.Fatalf("reached %q after %d hand-offs; a request that arrives from b4's own upstream dial must not be handed off again", got, calls)
	}
}
