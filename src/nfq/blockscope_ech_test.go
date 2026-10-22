package nfq

import (
	"context"
	"io"
	"net"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/dns"
	"github.com/daniellavrushin/b4/sock"
)

const echCover = "cloudflare-ech.com"

type echClient struct {
	name  string
	ip    string
	ip6   string
	in    uint32
	keeps bool
}

func keepsECH(resp []byte) bool {
	return len(dns.ResponseECHPublicNames(resp)) != 0
}

func echAnswerFor(query []byte) []byte {
	domain, _ := dns.ParseQueryDomain(query)
	qtype, _ := dns.QuestionType(query)
	answer := httpsAnswerWithECH(domain, qtype, echCover)
	copy(answer[:2], query[:2])
	return answer
}

func tcpUpstreamReplying(t *testing.T, reply func(query []byte) []byte) {
	t.Helper()
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().(*net.TCPAddr)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			if query, err := readDNSTCPMessage(conn); err == nil {
				_ = writeDNSTCPMessage(conn, reply(query), time.Second)
				_, _ = io.Copy(io.Discard, conn)
			}
			conn.Close()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		<-done
	})
	prevDst, prevDial := dnsTCPOriginalDst, dialDNSTCPUpstream
	dnsTCPOriginalDst = func(net.Conn) (net.IP, int, error) { return addr.IP, addr.Port, nil }
	dialDNSTCPUpstream = func(ctx context.Context, _ *config.Config, a string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "tcp", a)
	}
	t.Cleanup(func() { dnsTCPOriginalDst, dialDNSTCPUpstream = prevDst, prevDial })
}

func guestECHWorker(t *testing.T, idx map[string]uint32) *Worker {
	t.Helper()
	thisHost(t, scopeRouter)
	routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"], "2001:db8::60": idx["br0"], "2001:db8::50": idx["br-guest"]})
	captureSends(t)
	cfg := config.NewConfig()
	cfg.Sets = []*config.SetConfig{
		scopeToGuest(echTestSet("block", true, echCover), config.BlockActionDrop),
		echTestSet("site", false, "example.org"),
	}
	w := newTestWorker(t, &cfg)
	w.sock = &sock.Sender{}
	t.Cleanup(w.wg.Wait)
	return w
}

func TestAScopedBlockSetStripsECHKeysOnlyOnItsInterface(t *testing.T) {
	idx := scopeIfaces(t)
	clients := []echClient{
		{"br0", scopeLAN, "2001:db8::60", idx["br0"], true},
		{"br-guest", scopeGuest, "2001:db8::50", idx["br-guest"], false},
	}
	for _, path := range []struct {
		name string
		kept func(t *testing.T, w *Worker, c echClient) bool
	}{
		{"a forwarded answer", func(t *testing.T, w *Worker, c echClient) bool {
			raw := makeV4UDPPacket(httpsAnswerWithECH("www.example.org", dnsTypeHTTPS, echCover), net.ParseIP(rigUpstream), net.ParseIP(c.ip), 53, 40700)
			pkt, ok := w.parseIPHeaders(raw)
			if !ok {
				t.Fatal("the answer did not parse")
			}
			q := newFakeModQueue()
			w.processDnsPacket(&verdictCtx{id: 1, q: q}, pkt, 53, 40700, raw[28:])
			v := q.now(t)
			return v.packet == nil || keepsECH(v.packet[28:])
		}},
		{"a redirected answer", func(t *testing.T, w *Worker, c echClient) bool {
			site := w.getConfig().GetSetById("site")
			site.DNS.Enabled, site.DNS.DoHURL = true, dohServerReplying(t, w, echAnswerFor)
			replies := watchReplies(t)
			query := makeV4UDPPacket(dns.BuildQuery("www.example.org", 0x971, dnsTypeHTTPS), net.ParseIP(c.ip), net.ParseIP(rigUpstream), 40701, 53)
			queuedVerdict(t, w, query, nil, 0, c.in)
			resp := oneReply(t, replies)
			holdsDone(t, w)
			return keepsECH(resp)
		}},
		{"a TCP answer", func(t *testing.T, w *Worker, c echClient) bool {
			site := w.getConfig().GetSetById("site")
			site.DNS.Enabled, site.DNS.DoHURL = true, dohServerReplying(t, w, echAnswerFor)
			return keepsECH(tcpQueryFrom(t, w, c.ip, dns.BuildQuery("www.example.org", 0x972, dnsTypeHTTPS)))
		}},
		{"a relayed TCP answer", func(t *testing.T, w *Worker, c echClient) bool {
			tcpECHUpstream(t)
			return keepsECH(tcpQueryFrom(t, w, c.ip, dns.BuildQuery("www.example.org", 0x973, dnsTypeHTTPS)))
		}},
		{"an IPv6 answer while IPv6 is off", func(t *testing.T, w *Worker, c echClient) bool {
			raw := v6Answer(httpsAnswerWithECH("www."+echCover, dnsTypeHTTPS, echCover))
			copy(raw[24:40], net.ParseIP(c.ip6).To16())
			q := newFakeModQueue()
			w.dispatch(&verdictCtx{id: 1, q: q}, raw)
			v := q.now(t)
			return v.packet == nil || keepsECH(v.packet[48:])
		}},
		{"a TCP query over IPv6 while IPv6 is off", func(t *testing.T, w *Worker, c echClient) bool {
			tcpECHUpstream(t)
			return keepsECH(tcpQueryFrom(t, w, c.ip6, dns.BuildQuery("www."+echCover, 0x974, dnsTypeHTTPS)))
		}},
	} {
		for _, c := range clients {
			t.Run(path.name+"/"+c.name, func(t *testing.T) {
				w := guestECHWorker(t, idx)
				if kept := path.kept(t, w, c); kept != c.keeps {
					t.Fatalf("the answer kept its ECH key: %v, want %v; a block set scoped to br-guest removes keys only for br-guest", kept, c.keeps)
				}
			})
		}
	}
}

func TestTheCoverNameOfAnAnswerIsMatchedWithTheSetsTheQueryWasMatchedWith(t *testing.T) {
	idx := scopeIfaces(t)
	for _, path := range []struct {
		name string
		kept func(t *testing.T, w *Worker, reload func()) bool
	}{
		{"a relayed TCP answer", func(t *testing.T, w *Worker, reload func()) bool {
			tcpUpstreamReplying(t, func(query []byte) []byte {
				reload()
				return echAnswerFor(query)
			})
			return keepsECH(tcpQueryFrom(t, w, scopeLAN, dns.BuildQuery("www.example.org", 0x975, dnsTypeHTTPS)))
		}},
		{"a redirected answer", func(t *testing.T, w *Worker, reload func()) bool {
			site := w.getConfig().GetSetById("site")
			site.DNS.Enabled, site.DNS.DoHURL = true, dohServerReplying(t, w, func(query []byte) []byte {
				reload()
				return echAnswerFor(query)
			})
			replies := watchReplies(t)
			queuedVerdict(t, w, makeV4UDPPacket(dns.BuildQuery("www.example.org", 0x976, dnsTypeHTTPS), net.ParseIP(scopeLAN), net.ParseIP(rigUpstream), 40702, 53), nil, 0, idx["br0"])
			resp := oneReply(t, replies)
			holdsDone(t, w)
			return keepsECH(resp)
		}},
	} {
		t.Run(path.name, func(t *testing.T) {
			thisHost(t, scopeRouter)
			routeByClient(t, map[string]uint32{scopeLAN: idx["br0"], scopeGuest: idx["br-guest"]})
			captureSends(t)
			other := echTestSet("other", true, "other.example")
			site := echTestSet("site", false, "example.org")
			cfg := config.NewConfig()
			cfg.Sets = []*config.SetConfig{other, site}
			w := newTestWorker(t, &cfg)
			w.sock = &sock.Sender{}
			t.Cleanup(w.wg.Wait)
			reload := func() {
				reloaded := config.NewConfig()
				reloaded.Sets = []*config.SetConfig{scopeToGuest(echTestSet("block", true, echCover), config.BlockActionDrop), other, site}
				w.UpdateConfig(&reloaded)
				w.matcher.Store(buildMatcher(&reloaded))
			}
			if !path.kept(t, w, reload) {
				t.Fatal("a block set scoped to br-guest that a reload added during the exchange removed the ECH key of a client on br0")
			}
		})
	}
}
