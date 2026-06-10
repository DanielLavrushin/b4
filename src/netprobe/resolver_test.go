package netprobe

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestResolveDoHOnceJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("name") != "example.com" {
			t.Errorf("unexpected name param: %q", r.URL.Query().Get("name"))
		}
		w.Header().Set("Content-Type", "application/dns-json")
		w.Write([]byte(`{"Answer":[{"type":1,"data":"93.184.216.34"},{"type":28,"data":"2606:2800:220:1:248:1893:25c8:1946"}]}`))
	}))
	defer srv.Close()

	r := &Resolver{Timeout: 2 * time.Second}
	ips, err := r.ResolveDoHOnce(context.Background(), DoHServer{URL: srv.URL, Format: DoHJSON}, "example.com", "A")
	if err != nil {
		t.Fatalf("ResolveDoHOnce error: %v", err)
	}
	if len(ips) != 1 || ips[0] != "93.184.216.34" {
		t.Fatalf("want [93.184.216.34], got %v", ips)
	}

	v6, err := r.ResolveDoHOnce(context.Background(), DoHServer{URL: srv.URL, Format: DoHJSON}, "example.com", "AAAA")
	if err != nil {
		t.Fatalf("ResolveDoHOnce AAAA error: %v", err)
	}
	if len(v6) != 1 || !strings.HasPrefix(v6[0], "2606:2800") {
		t.Fatalf("want one AAAA, got %v", v6)
	}
}

func TestResolveResilientFallsThroughToWorkingServer(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Answer":[{"type":1,"data":"1.2.3.4"}]}`))
	}))
	defer good.Close()

	r := &Resolver{
		Timeout: 2 * time.Second,
		DoH: []DoHServer{
			{URL: bad.URL, Format: DoHJSON},
			{URL: good.URL, Format: DoHJSON},
		},
		UDP: []string{},
	}
	out, err := r.ResolveResilient(context.Background(), "example.com", "A")
	if err != nil {
		t.Fatalf("ResolveResilient error: %v", err)
	}
	if len(out.IPs) != 1 || out.IPs[0] != "1.2.3.4" {
		t.Fatalf("want [1.2.3.4], got %v", out.IPs)
	}
	if out.DoHURL != good.URL {
		t.Fatalf("want winner %s, got %s", good.URL, out.DoHURL)
	}
}

func TestResolveResilientFallsThroughToUDP(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer bad.Close()

	udpAddr, stop := startUDPResponder(t, dnsAResponse())
	defer stop()

	r := &Resolver{
		Timeout: 2 * time.Second,
		DoH:     []DoHServer{{URL: bad.URL, Format: DoHJSON}},
		UDP:     []string{udpAddr},
	}
	out, err := r.ResolveResilient(context.Background(), "example.com", "A")
	if err != nil {
		t.Fatalf("ResolveResilient error: %v", err)
	}
	if len(out.IPs) != 1 || out.IPs[0] != "5.6.7.8" {
		t.Fatalf("want [5.6.7.8] from UDP, got %v", out.IPs)
	}
	if out.UDPSrv != udpAddr {
		t.Fatalf("want udp winner %s, got %s", udpAddr, out.UDPSrv)
	}
}

func TestResolveResilientUDPNotStarvedByBlockedDoH(t *testing.T) {
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer hang.Close()

	udpAddr, stop := startUDPResponder(t, dnsAResponse())
	defer stop()

	r := &Resolver{
		Timeout: 1500 * time.Millisecond,
		DoH: []DoHServer{
			{URL: hang.URL, Format: DoHJSON},
			{URL: hang.URL, Format: DoHJSON},
			{URL: hang.URL, Format: DoHJSON},
		},
		UDP: []string{udpAddr},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := r.ResolveResilient(ctx, "example.com", "A")
	if err != nil {
		t.Fatalf("ResolveResilient error (blocked DoH starved UDP): %v", err)
	}
	if len(out.IPs) != 1 || out.IPs[0] != "5.6.7.8" || out.UDPSrv != udpAddr {
		t.Fatalf("want UDP answer [5.6.7.8] from %s, got %+v", udpAddr, out)
	}
}

func TestResolveUDPOnceNXDomain(t *testing.T) {
	resp := make([]byte, 12)
	resp[3] = 0x03
	udpAddr, stop := startUDPResponder(t, resp)
	defer stop()

	r := &Resolver{Timeout: 2 * time.Second}
	ans, err := r.ResolveUDPOnce(context.Background(), udpAddr, "example.com", "A")
	if err != nil {
		t.Fatalf("ResolveUDPOnce error: %v", err)
	}
	if !ans.NXDomain {
		t.Fatalf("want NXDomain, got %+v", ans)
	}
}

func TestResolveUDPOnceEmpty(t *testing.T) {
	udpAddr, stop := startUDPResponder(t, make([]byte, 12))
	defer stop()

	r := &Resolver{Timeout: 2 * time.Second}
	ans, err := r.ResolveUDPOnce(context.Background(), udpAddr, "example.com", "A")
	if err != nil {
		t.Fatalf("ResolveUDPOnce error: %v", err)
	}
	if !ans.Empty || ans.NXDomain {
		t.Fatalf("want Empty, got %+v", ans)
	}
}

func TestDefaultDoHServersIPHostFirst(t *testing.T) {
	if len(DefaultDoHServers) == 0 {
		t.Fatal("DefaultDoHServers empty")
	}
	host := dohHost(t, DefaultDoHServers[0].URL)
	if net.ParseIP(host) == nil {
		t.Fatalf("first DoH server must be IP-host (DNS-poison safe), got %q", host)
	}
}

func TestMatchesFamily(t *testing.T) {
	if !matchesFamily(net.ParseIP("1.2.3.4"), "A") {
		t.Error("1.2.3.4 should match A")
	}
	if matchesFamily(net.ParseIP("1.2.3.4"), "AAAA") {
		t.Error("1.2.3.4 should not match AAAA")
	}
	if !matchesFamily(net.ParseIP("2606:2800::1"), "AAAA") {
		t.Error("v6 should match AAAA")
	}
}

func dohHost(t *testing.T, rawURL string) string {
	t.Helper()
	s := strings.TrimPrefix(rawURL, "https://")
	s = strings.TrimPrefix(s, "http://")
	if i := strings.IndexAny(s, "/:"); i >= 0 {
		s = s[:i]
	}
	return s
}

func startUDPResponder(t *testing.T, reply []byte) (string, func()) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("ListenPacket: %v", err)
	}
	go func() {
		buf := make([]byte, 1500)
		for {
			n, addr, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			out := make([]byte, len(reply))
			copy(out, reply)
			if len(out) >= 2 && n >= 2 {
				out[0] = buf[0]
				out[1] = buf[1]
			}
			pc.WriteTo(out, addr)
		}
	}()
	host, port, _ := net.SplitHostPort(pc.LocalAddr().String())
	_ = host
	return net.JoinHostPort("127.0.0.1", port), func() { pc.Close() }
}

func dnsAResponse() []byte {
	msg := []byte{
		0x00, 0x00,
		0x81, 0x80,
		0x00, 0x01,
		0x00, 0x01,
		0x00, 0x00,
		0x00, 0x00,
	}
	question := []byte{
		0x07, 'e', 'x', 'a', 'm', 'p', 'l', 'e',
		0x03, 'c', 'o', 'm',
		0x00,
		0x00, 0x01,
		0x00, 0x01,
	}
	answer := []byte{
		0xc0, 0x0c,
		0x00, 0x01,
		0x00, 0x01,
		0x00, 0x00, 0x00, 0x3c,
		0x00, 0x04,
		5, 6, 7, 8,
	}
	msg = append(msg, question...)
	msg = append(msg, answer...)
	return msg
}
