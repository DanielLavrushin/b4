package nfq

import (
	"errors"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/dns"
	"github.com/florianl/go-nfqueue"
)

func hostLooksUpAt(t *testing.T, servers ...string) {
	t.Helper()
	prevServers, prevNetworks := systemNameservers, ownNetworks
	systemNameservers = func() []string { return servers }
	ownNetworks = func() ([]net.Addr, error) {
		var out []net.Addr
		for _, c := range []string{"127.0.0.1/8", "192.168.77.1/24", "11.9.0.2/24", "2a02:6b8:0:77::1/64"} {
			ip, n, err := net.ParseCIDR(c)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, &net.IPNet{IP: ip, Mask: n.Mask})
		}
		return append(out, &net.UnixAddr{}), nil
	}
	t.Cleanup(func() { systemNameservers, ownNetworks = prevServers, prevNetworks })
}

func TestADoHHostLookupIsLeftAloneOnlyWhereTheHostMayWaitForIt(t *testing.T) {
	thisHost(t)
	set := strictSet(dohByName("all", "https://dns.example/dns-query", "regexp:.*"))
	w := redirectWorker(t, false, set)
	for _, tc := range []struct {
		name    string
		servers []string
		pass    bool
	}{
		{"an outside resolver", []string{"8.8.8.8"}, false},
		{"three outside resolvers", []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"}, false},
		{"an outside resolver written as IPv6", []string{"::ffff:8.8.8.8"}, false},
		{"a forwarder on the host", []string{"127.0.0.1"}, true},
		{"the systemd stub", []string{"127.0.0.53"}, true},
		{"the Docker resolver", []string{"127.0.0.11"}, true},
		{"a forwarder on the host over IPv6", []string{"::1"}, true},
		{"the unspecified address", []string{"0.0.0.0"}, true},
		{"a forwarder on the network", []string{"192.168.77.2"}, true},
		{"a private address elsewhere", []string{"10.66.0.1"}, true},
		{"a carrier's address", []string{"100.64.0.53"}, true},
		{"a local IPv6 address", []string{"fd00::53"}, true},
		{"a link-local address", []string{"169.254.169.254"}, true},
		{"a link-local address with a zone", []string{"fe80::1%br0"}, true},
		{"the host's own public address", []string{"11.9.0.2"}, true},
		{"a public address on the host's network", []string{"11.9.0.53"}, true},
		{"a public IPv6 address on the host's network", []string{"2a02:6b8:0:77::53"}, true},
		{"a forwarder after an outside resolver", []string{"8.8.8.8", "127.0.0.1"}, true},
		{"a forwarder before an outside resolver", []string{"192.168.77.2", "8.8.8.8"}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hostLooksUpAt(t, tc.servers...)
			lines := connectionLines(t)
			q := newFakeQueue()
			w.processDnsPacket(&verdictCtx{id: 101, q: q}, lanQuery("192.0.2.1", "203.0.113.53"), 40000, 53, dns.BuildQuery("dns.example", 101, dnsTypeA))
			want := nfqueue.NfDrop
			if tc.pass {
				want = nfqueue.NfAccept
			}
			if v := q.now(t); v != want {
				t.Fatalf("verdict %d, want %d", v, want)
			}
			if n := countAction(drainLines(lines), dnsActionDoHHost); n == 1 != tc.pass {
				t.Fatalf("%d %s rows, want one %v", n, dnsActionDoHHost, tc.pass)
			}
		})
	}
}

func TestEverySiteAsksTheSetForItsDoHHostWhereTheHostLooksItUpOutside(t *testing.T) {
	thisHost(t)
	hostLooksUpAt(t, "8.8.8.8")
	const byAddress = "https://192.0.2.71/dns-query"
	set := strictSet(listed(dohByName("all", "https://dns.resolver.example/dns-query", "regexp:.*"), byAddress))
	w := redirectWorker(t, false, set)
	cfg := w.getConfig()
	client, server := net.ParseIP("192.0.2.1").To4(), net.ParseIP("203.0.113.53").To4()
	for _, q := range []struct {
		name  string
		qtype uint16
	}{{"dns.resolver.example", dnsTypeA}, {"resolver.example", dnsTypeDS}, {"example", dnsTypeDNSKEY}} {
		query := dns.BuildQuery(q.name, 102, q.qtype)
		all := redirectServers(cfg, set)
		if got := w.serversToAsk(cfg, set, redirectServers(cfg, set), q.name, query, "", server); !reflect.DeepEqual(got, all) {
			t.Errorf("%s: a lookup is asked of %+v, want the whole list", q.name, got)
		}
		if got := w.answeringServers(cfg, set, q.name, query, client, nil); len(got) != len(all) {
			t.Errorf("%s: an escalation asks %+v, want the whole list", q.name, got)
		}
		s := newDNSTCPServer(w, 0)
		if _, err := s.answerVia(set, cfg, query, q.name, client); errors.Is(err, errDoHHost) {
			t.Errorf("%s: a TCP escalation was refused", q.name)
		}
		s.cancel()
		lines := connectionLines(t)
		upstream := tcpDNSUpstream(t)
		dnsOverTCPAnsweredByTheSet(t, w, query)
		if n := countAction(drainLines(lines), dnsActionDoHHost); n != 0 || len(upstream.queries) != 0 {
			t.Errorf("%s over TCP: %d %s rows and %d plain queries, want none", q.name, n, dnsActionDoHHost, len(upstream.queries))
		}
	}
}

func dnsOverTCPAnsweredByTheSet(t *testing.T, w *Worker, query []byte) {
	t.Helper()
	s := newDNSTCPServer(w, 0)
	defer s.cancel()
	a, b := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handle(remoteAddrConn{Conn: b, remote: &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: 40000}})
	}()
	_ = a.SetDeadline(time.Now().Add(5 * time.Second))
	if err := writeDNSTCPMessage(a, query, time.Second); err != nil {
		t.Fatal(err)
	}
	if resp, err := readDNSTCPMessage(a); err != nil || len(resp) == 0 {
		t.Errorf("no answer over TCP: %v", err)
	}
	a.Close()
	<-done
}

func TestAnInterfaceListThatCannotBeReadKeepsTheDoHHostLookupUnchanged(t *testing.T) {
	thisHost(t)
	hostLooksUpAt(t, "8.8.8.8")
	ownNetworks = func() ([]net.Addr, error) { return nil, errors.New("no list") }
	w := redirectWorker(t, false, strictSet(dohByName("all", "https://dns.example/dns-query", "regexp:.*")))
	q := newFakeQueue()
	w.processDnsPacket(&verdictCtx{id: 103, q: q}, lanQuery("192.0.2.1", "203.0.113.53"), 40000, 53, dns.BuildQuery("dns.example", 103, dnsTypeA))
	if v := q.now(t); v != nfqueue.NfAccept {
		t.Fatalf("verdict %d, want the lookup forwarded unchanged", v)
	}
}
