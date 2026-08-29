package mtproto

import (
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestNetworkKeyOf(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"85.233.150.240", "85.233.150.240"},
		{"::ffff:85.233.150.240", "85.233.150.240"},
		{"2a00:1370:8190:1234:abcd:1:2:3", "2a00:1370:8190:1234::/64"},
		{"2a00:1370:8190:1234:ffff:ffff:ffff:ffff", "2a00:1370:8190:1234::/64"},
		{"2a00:1370:8190:9999::1", "2a00:1370:8190:9999::/64"},
		{"fe80::1%eth0", "fe80::/64"},
		{"not-an-ip", "not-an-ip"},
	}
	for _, tc := range cases {
		if got := networkKeyOf(tc.in); got != tc.want {
			t.Errorf("networkKeyOf(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func statForSecret(t *testing.T, srv *Server, name string) SecretStat {
	t.Helper()
	for _, s := range srv.Stats().Secrets {
		if s.Name == name {
			return s
		}
	}
	t.Fatalf("no stats for secret %q", name)
	return SecretStat{}
}

func TestStatsCountsNetworksNotConnections(t *testing.T) {
	srv, _, secA, secB := revocationTestServer(t)

	same := []*closeRecordConn{
		{remoteAddr: &net.TCPAddr{IP: net.ParseIP("85.233.150.240"), Port: 1}},
		{remoteAddr: &net.TCPAddr{IP: net.ParseIP("85.233.150.240"), Port: 2}},
		{remoteAddr: &net.TCPAddr{IP: net.ParseIP("85.233.150.240"), Port: 3}},
	}
	for _, c := range same {
		srv.trackConn(secA, c)
	}
	rotated := &closeRecordConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP("2a00:1370:8190:1234::1"), Port: 4}}
	rotatedAgain := &closeRecordConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP("2a00:1370:8190:1234:5:6:7:8"), Port: 5}}
	srv.trackConn(secA, rotated)
	srv.trackConn(secA, rotatedAgain)

	srv.trackConn(secB, &closeRecordConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP("10.0.0.1"), Port: 6}})

	got := statForSecret(t, srv, "Max")
	if got.Networks != 2 {
		t.Fatalf("five connections from one IPv4 host and one IPv6 /64 must be 2 networks, got %d", got.Networks)
	}
	want := []string{"2a00:1370:8190:1234::/64", "85.233.150.240"}
	if len(got.NetworkAddrs) != len(want) {
		t.Fatalf("NetworkAddrs = %v, want %v", got.NetworkAddrs, want)
	}
	for i := range want {
		if got.NetworkAddrs[i] != want[i] {
			t.Fatalf("NetworkAddrs = %v, want %v", got.NetworkAddrs, want)
		}
	}
	if other := statForSecret(t, srv, "Ivan"); other.Networks != 1 {
		t.Fatalf("networks must not leak between secrets, Ivan has %d", other.Networks)
	}
}

func TestStatsNetworksReleasedOnUntrack(t *testing.T) {
	srv, _, secA, _ := revocationTestServer(t)

	c1 := &closeRecordConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP("85.233.150.240"), Port: 1}}
	c2 := &closeRecordConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP("178.130.140.98"), Port: 2}}
	_, untrack1 := srv.trackConn(secA, c1)
	srv.trackConn(secA, c2)

	if got := statForSecret(t, srv, "Max").Networks; got != 2 {
		t.Fatalf("expected 2 networks, got %d", got)
	}

	untrack1()

	got := statForSecret(t, srv, "Max")
	if got.Networks != 1 {
		t.Fatalf("expected 1 network after untrack, got %d", got.Networks)
	}
	if len(got.NetworkAddrs) != 1 || got.NetworkAddrs[0] != "178.130.140.98" {
		t.Fatalf("NetworkAddrs = %v, want [178.130.140.98]", got.NetworkAddrs)
	}
}

func TestStatsNetworkAddrsAreCapped(t *testing.T) {
	srv, _, secA, _ := revocationTestServer(t)

	total := maxNetworkAddrsPerSecret + 7
	for i := 0; i < total; i++ {
		ip := fmt.Sprintf("10.%d.%d.1", i/256, i%256)
		srv.trackConn(secA, &closeRecordConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 1000 + i}})
	}

	got := statForSecret(t, srv, "Max")
	if got.Networks != total {
		t.Fatalf("Networks = %d, want the true total %d", got.Networks, total)
	}
	if len(got.NetworkAddrs) != maxNetworkAddrsPerSecret {
		t.Fatalf("NetworkAddrs length = %d, want %d", len(got.NetworkAddrs), maxNetworkAddrsPerSecret)
	}
}

func TestStatsNetworksZeroWithoutConns(t *testing.T) {
	srv, _, _, _ := revocationTestServer(t)

	got := statForSecret(t, srv, "Max")
	if got.Networks != 0 || len(got.NetworkAddrs) != 0 {
		t.Fatalf("expected no networks, got %d %v", got.Networks, got.NetworkAddrs)
	}
}

func TestWebClientAddrRejectsNonAddressForwardedFor(t *testing.T) {
	cases := []struct {
		name    string
		header  string
		want    string
		wantErr bool
	}{
		{"absent", "", "203.0.113.7:5555", false},
		{"valid ipv4", "198.51.100.4", "198.51.100.4:0", false},
		{"valid ipv4 with proxy chain", "198.51.100.4, 10.0.0.9", "198.51.100.4:0", false},
		{"valid ipv6", "2a00:1370:8190:1234::1", "[2a00:1370:8190:1234::1]:0", false},
		{"ipv4 mapped ipv6", "::ffff:198.51.100.4", "198.51.100.4:0", false},
		{"not an address", "pwned<b>", "203.0.113.7:5555", false},
		{"empty token", " , 10.0.0.9", "203.0.113.7:5555", false},
		{"host name", "client.example.com", "203.0.113.7:5555", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = "203.0.113.7:5555"
			if tc.header != "" {
				r.Header.Set("X-Forwarded-For", tc.header)
			}
			if got := webClientAddr(r); got != tc.want {
				t.Fatalf("webClientAddr(%q) = %q, want %q", tc.header, got, tc.want)
			}
		})
	}
}

func TestNetworkKeyOfNeverEchoesUnparseableWebAddr(t *testing.T) {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "203.0.113.7:5555"
	r.Header.Set("X-Forwarded-For", "<script>alert(1)</script>")

	host, _, err := net.SplitHostPort(webClientAddr(r))
	if err != nil {
		t.Fatalf("SplitHostPort: %v", err)
	}
	if got := networkKeyOf(host); got != "203.0.113.7" {
		t.Fatalf("network key = %q, want the real peer 203.0.113.7", got)
	}
}

func TestStatsGlobalNetworksAreDeduped(t *testing.T) {
	srv, _, secA, secB := revocationTestServer(t)

	shared := net.ParseIP("85.233.150.240")
	srv.trackConn(secA, &closeRecordConn{remoteAddr: &net.TCPAddr{IP: shared, Port: 1}})
	srv.trackConn(secB, &closeRecordConn{remoteAddr: &net.TCPAddr{IP: shared, Port: 2}})
	srv.trackConn(secB, &closeRecordConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP("178.130.140.98"), Port: 3}})

	st := srv.Stats()
	if st.Networks != 2 {
		t.Fatalf("one address shared by two secrets must count once: Networks = %d, want 2", st.Networks)
	}
	var perSecret int
	for _, s := range st.Secrets {
		perSecret += s.Networks
	}
	if perSecret != 3 {
		t.Fatalf("per-secret counts must still total 3, got %d", perSecret)
	}
}

func TestStatsGlobalNetworksIgnoreRevokedSecrets(t *testing.T) {
	srv, mkCfg, secA, secB := revocationTestServer(t)

	srv.trackConn(secA, &closeRecordConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP("85.233.150.240"), Port: 1}})
	srv.trackConn(secB, &closeRecordConn{remoteAddr: &net.TCPAddr{IP: net.ParseIP("178.130.140.98"), Port: 2}})

	if got := srv.Stats().Networks; got != 2 {
		t.Fatalf("expected 2 networks before revocation, got %d", got)
	}

	srv.UpdateConfig(mkCfg(func(m *config.MTProtoConfig) {
		m.Secrets = m.Secrets[:1]
	}))

	if got := srv.Stats().Networks; got != 1 {
		t.Fatalf("a revoked secret must not contribute to the global count, got %d", got)
	}
}
