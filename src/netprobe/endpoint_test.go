package netprobe

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/dns/endpoint"
)

type stubAnswer struct {
	rcode     byte
	ip        string
	truncated bool
	silent    bool
}

func stubQuestionEnd(query []byte) int {
	i := 12
	for i < len(query) && query[i] != 0 {
		i += int(query[i]) + 1
	}
	return i + 5
}

func stubReply(query []byte, a stubAnswer) []byte {
	end := stubQuestionEnd(query)
	if end > len(query) {
		return nil
	}
	reply := append([]byte(nil), query[:end]...)
	reply[2] = 0x80 | query[2]&0x01
	reply[3] = 0x80 | a.rcode
	if a.truncated {
		reply[2] |= 0x02
	}
	for i := 6; i < 12; i++ {
		reply[i] = 0
	}
	if ip := net.ParseIP(a.ip).To4(); ip != nil && a.rcode == 0 && !a.truncated {
		reply[7] = 1
		reply = append(reply, 0xc0, 0x0c, 0, 1, 0, 1, 0, 0, 0, 60, 0, 4)
		reply = append(reply, ip...)
	}
	return reply
}

type dnsStub struct {
	addr string
	pc   net.PacketConn
	ln   net.Listener
}

func newDNSStub(t *testing.T) *dnsStub {
	t.Helper()
	for range 20 {
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatalf("listen tcp: %v", err)
		}
		pc, err := net.ListenPacket("udp", ln.Addr().String())
		if err != nil {
			ln.Close()
			continue
		}
		t.Cleanup(func() {
			pc.Close()
			ln.Close()
		})
		return &dnsStub{addr: ln.Addr().String(), pc: pc, ln: ln}
	}
	t.Fatal("no port is free for both TCP and UDP")
	return nil
}

func udpStub(t *testing.T, a stubAnswer) (string, *atomic.Int32) {
	t.Helper()
	s := newDNSStub(t)
	return s.addr, s.serveUDP(a)
}

func (s *dnsStub) serveUDP(a stubAnswer) *atomic.Int32 {
	var hits atomic.Int32
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := s.pc.ReadFrom(buf)
			if err != nil {
				return
			}
			hits.Add(1)
			if a.silent {
				continue
			}
			if reply := stubReply(buf[:n], a); reply != nil {
				s.pc.WriteTo(reply, addr)
			}
		}
	}()
	return &hits
}

func (s *dnsStub) serveTCP(a stubAnswer) *atomic.Int32 {
	var hits atomic.Int32
	go func() {
		for {
			conn, err := s.ln.Accept()
			if err != nil {
				return
			}
			hits.Add(1)
			go func(conn net.Conn) {
				defer conn.Close()
				var size [2]byte
				if _, err := io.ReadFull(conn, size[:]); err != nil {
					return
				}
				query := make([]byte, binary.BigEndian.Uint16(size[:]))
				if _, err := io.ReadFull(conn, query); err != nil {
					return
				}
				reply := stubReply(query, a)
				framed := append([]byte{byte(len(reply) >> 8), byte(len(reply))}, reply...)
				conn.Write(framed)
			}(conn)
		}
	}()
	return &hits
}

func mustEndpoint(t *testing.T, s string) endpoint.Endpoint {
	t.Helper()
	ep, err := endpoint.Parse(s)
	if err != nil {
		t.Fatalf("parse %q: %v", s, err)
	}
	return ep
}

func TestResolveEndpointPlainUDP(t *testing.T) {
	addr, _ := udpStub(t, stubAnswer{ip: "203.0.113.7"})
	r := &Resolver{Timeout: 2 * time.Second}

	ans, err := r.ResolveEndpoint(context.Background(), mustEndpoint(t, addr), "site.example", "A")
	if err != nil || len(ans.IPs) != 1 || ans.IPs[0] != "203.0.113.7" || !ans.OverUDP {
		t.Fatalf("want 203.0.113.7 over UDP, got %+v %v", ans, err)
	}
}

func TestResolveEndpointTellsNXDomainFromNoData(t *testing.T) {
	nx, _ := udpStub(t, stubAnswer{rcode: 3})
	nodata, _ := udpStub(t, stubAnswer{})
	r := &Resolver{Timeout: 2 * time.Second}

	_, err := r.ResolveEndpoint(context.Background(), mustEndpoint(t, nx), "typo.example", "A")
	var nxErr *NXDomainError
	if !errors.As(err, &nxErr) || nxErr.Server != nx {
		t.Fatalf("want NXDOMAIN named after %s, got %v", nx, err)
	}

	_, err = r.ResolveEndpoint(context.Background(), mustEndpoint(t, nodata), "noaddr.example", "A")
	var noData *NoDataError
	if !errors.As(err, &noData) || !NoAddressAnswer(err) {
		t.Fatalf("a NOERROR answer without an address means the name exists without one, got %v", err)
	}
}

func TestResolveEndpointServerFailureIsNotAnAnswer(t *testing.T) {
	addr, _ := udpStub(t, stubAnswer{rcode: 2})
	r := &Resolver{Timeout: 2 * time.Second}

	_, err := r.ResolveEndpoint(context.Background(), mustEndpoint(t, addr), "site.example", "A")
	var rcode *RcodeError
	if NoAddressAnswer(err) || !errors.As(err, &rcode) || rcode.Rcode != 2 || !strings.Contains(err.Error(), "SERVFAIL") {
		t.Fatalf("SERVFAIL is a failure, not an answer about the name, got %v", err)
	}
}

func TestResolveEndpointTCP(t *testing.T) {
	s := newDNSStub(t)
	udpHits := s.serveUDP(stubAnswer{ip: "198.51.100.1"})
	tcpHits := s.serveTCP(stubAnswer{ip: "203.0.113.9"})
	r := &Resolver{Timeout: 2 * time.Second}

	ans, err := r.ResolveEndpoint(context.Background(), mustEndpoint(t, "tcp://"+s.addr), "site.example", "A")
	if err != nil || len(ans.IPs) != 1 || ans.IPs[0] != "203.0.113.9" || ans.OverUDP {
		t.Fatalf("tcp:// asks over TCP only, got %+v %v", ans, err)
	}
	if udpHits.Load() != 0 || tcpHits.Load() != 1 {
		t.Fatalf("udp=%d tcp=%d queries, want 0 and 1", udpHits.Load(), tcpHits.Load())
	}
}

func TestResolveEndpointTruncatedUDPRetriesOverTCP(t *testing.T) {
	s := newDNSStub(t)
	s.serveUDP(stubAnswer{truncated: true})
	s.serveTCP(stubAnswer{ip: "203.0.113.9"})
	r := &Resolver{Timeout: 2 * time.Second}

	ans, err := r.ResolveEndpoint(context.Background(), mustEndpoint(t, s.addr), "big.example", "A")
	if err != nil || len(ans.IPs) != 1 || ans.OverUDP {
		t.Fatalf("a truncated UDP answer is asked again over TCP, got %+v %v", ans, err)
	}
}

func TestResolveEndpointTCPUDPFallsBackWhenUDPFails(t *testing.T) {
	s := newDNSStub(t)
	udpHits := s.serveUDP(stubAnswer{silent: true})
	tcpHits := s.serveTCP(stubAnswer{ip: "203.0.113.9"})
	r := &Resolver{Timeout: 500 * time.Millisecond}

	ans, err := r.ResolveEndpoint(context.Background(), mustEndpoint(t, "tcp+udp://"+s.addr), "site.example", "A")
	if err != nil || len(ans.IPs) != 1 || ans.OverUDP {
		t.Fatalf("tcp+udp:// retries over TCP when UDP fails, got %+v %v", ans, err)
	}
	if udpHits.Load() == 0 || tcpHits.Load() != 1 {
		t.Fatalf("udp=%d tcp=%d, want UDP first, then one TCP query", udpHits.Load(), tcpHits.Load())
	}

	plain := newDNSStub(t)
	plain.serveUDP(stubAnswer{silent: true})
	plainTCP := plain.serveTCP(stubAnswer{ip: "203.0.113.9"})
	_, err = r.ResolveEndpoint(context.Background(), mustEndpoint(t, plain.addr), "site.example", "A")
	if err == nil || plainTCP.Load() != 0 {
		t.Fatalf("plain UDP has no TCP fallback for a silent server, got %v with %d TCP queries", err, plainTCP.Load())
	}
}

func TestResolveEndpointTCPUDPKeepsAnUDPAnswer(t *testing.T) {
	for _, answer := range []stubAnswer{{rcode: 3}, {rcode: 2}, {rcode: 5}} {
		s := newDNSStub(t)
		s.serveUDP(answer)
		tcpHits := s.serveTCP(stubAnswer{ip: "203.0.113.9"})
		r := &Resolver{Timeout: 2 * time.Second}

		ans, err := r.ResolveEndpoint(context.Background(), mustEndpoint(t, "tcp+udp://"+s.addr), "typo.example", "A")
		var nx *NXDomainError
		var rcode *RcodeError
		answered := errors.As(err, &nx) || errors.As(err, &rcode)
		if !answered || !ans.OverUDP || tcpHits.Load() != 0 {
			t.Fatalf("rcode %d over UDP is an answer, not a failure to retry, got %+v %v with %d TCP queries", answer.rcode, ans, err, tcpHits.Load())
		}
	}
}

func TestResolveEndpointDoHWire(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(stubReply(query, stubAnswer{ip: "203.0.113.5"}))
	}))
	defer srv.Close()
	orig := endpointDoHClient
	endpointDoHClient = func(int, time.Duration) *http.Client {
		return &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}}
	}
	t.Cleanup(func() { endpointDoHClient = orig })

	r := &Resolver{Timeout: 2 * time.Second}
	ans, err := r.ResolveEndpoint(context.Background(), mustEndpoint(t, srv.URL+"/dns-query"), "site.example", "A")
	if err != nil || len(ans.IPs) != 1 || ans.IPs[0] != "203.0.113.5" || ans.OverUDP {
		t.Fatalf("want the DoH answer, got %+v %v", ans, err)
	}
}

func TestResolveResilientNoDataBeatsNXDomain(t *testing.T) {
	nx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Status":3}`))
	}))
	defer nx.Close()
	nodata := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(50 * time.Millisecond)
		w.Write([]byte(`{"Status":0,"Authority":[{"type":6,"data":"ns1.example. hostmaster.example. 1 2 3 4 5"}]}`))
	}))
	defer nodata.Close()
	udpAddr, _ := udpStub(t, stubAnswer{rcode: 3})

	r := &Resolver{
		Timeout: 2 * time.Second,
		DoH:     []DoHServer{{URL: nx.URL, Format: DoHJSON}, {URL: nodata.URL, Format: DoHJSON}},
		UDP:     []string{udpAddr},
	}
	_, err := r.ResolveResilient(context.Background(), "noaddr.example", "A")
	var nd *NoDataError
	if !errors.As(err, &nd) || nd.Server != nodata.URL {
		t.Fatalf("one server saying the name exists outweighs one saying it does not, got %v", err)
	}
}
