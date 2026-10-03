package netprobe

import (
	"context"
	"errors"
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

func nxdomainWireResponse() []byte {
	return []byte{0x00, 0x00, 0x81, 0x83, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
}

func TestResolveDoHOnceReportsNXDomain(t *testing.T) {
	jsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Status":3,"Question":[{"name":"typo.example.","type":1}]}`))
	}))
	defer jsonSrv.Close()
	wireSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/dns-message")
		w.Write(nxdomainWireResponse())
	}))
	defer wireSrv.Close()

	r := &Resolver{Timeout: 2 * time.Second}
	for _, srv := range []DoHServer{{URL: jsonSrv.URL, Format: DoHJSON}, {URL: wireSrv.URL, Format: DoHWire}} {
		ips, err := r.ResolveDoHOnce(context.Background(), srv, "typo.example", "A")
		var nx *NXDomainError
		if !errors.As(err, &nx) {
			t.Fatalf("%s: want an NXDomainError, got ips=%v err=%v", srv.Format, ips, err)
		}
		if nx.Server != srv.URL || nx.Domain != "typo.example" {
			t.Fatalf("%s: the error must name the server and the domain, got %+v", srv.Format, nx)
		}
	}
}

func TestResolveDoHOnceAnswerWithoutTheFamilyIsNotNXDomain(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Status":0,"Answer":[{"type":28,"data":"2606:2800::1"}]}`))
	}))
	defer srv.Close()

	r := &Resolver{Timeout: 2 * time.Second}
	ips, err := r.ResolveDoHOnce(context.Background(), DoHServer{URL: srv.URL, Format: DoHJSON}, "v6only.example", "A")
	if err != nil || len(ips) != 0 {
		t.Fatalf("the name exists without an IPv4 address, want no error and no address, got ips=%v err=%v", ips, err)
	}
}

func TestResolveResilientReportsEncryptedNXDomain(t *testing.T) {
	nx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Status":3}`))
	}))
	defer nx.Close()
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()

	resp := make([]byte, 12)
	resp[3] = 0x03
	udpAddr, stop := startUDPResponder(t, resp)
	defer stop()

	r := &Resolver{
		Timeout: 2 * time.Second,
		DoH:     []DoHServer{{URL: broken.URL, Format: DoHJSON}, {URL: nx.URL, Format: DoHJSON}},
		UDP:     []string{udpAddr},
	}
	_, err := r.ResolveResilient(context.Background(), "typo.example", "A")
	var nxErr *NXDomainError
	if !errors.As(err, &nxErr) || nxErr.Server != nx.URL {
		t.Fatalf("want the NXDOMAIN from %s, got %v", nx.URL, err)
	}
}

func TestResolveResilientAddressBeatsNXDomain(t *testing.T) {
	nx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Status":3}`))
	}))
	defer nx.Close()

	udpAddr, stop := startUDPResponder(t, dnsAResponse())
	defer stop()

	r := &Resolver{
		Timeout: 2 * time.Second,
		DoH:     []DoHServer{{URL: nx.URL, Format: DoHJSON}},
		UDP:     []string{udpAddr},
	}
	out, err := r.ResolveResilient(context.Background(), "example.com", "A")
	if err != nil || len(out.IPs) != 1 || out.IPs[0] != "5.6.7.8" {
		t.Fatalf("any server with an address wins over an NXDOMAIN, got %+v, %v", out, err)
	}
}

func TestResolveResilientPlainNXDomainIsNotTrusted(t *testing.T) {
	broken := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer broken.Close()

	resp := make([]byte, 12)
	resp[3] = 0x03
	udpAddr, stop := startUDPResponder(t, resp)
	defer stop()

	r := &Resolver{
		Timeout: 2 * time.Second,
		DoH:     []DoHServer{{URL: broken.URL, Format: DoHJSON}},
		UDP:     []string{udpAddr},
	}
	_, err := r.ResolveResilient(context.Background(), "typo.example", "A")
	var nx *NXDomainError
	if err == nil || errors.As(err, &nx) {
		t.Fatalf("an NXDOMAIN over plain UDP can be forged on the path, it must not read as one: %v", err)
	}
}

func TestResolveResilientSlowerAddressBeatsEarlierNXDomain(t *testing.T) {
	nx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Status":3}`))
	}))
	defer nx.Close()
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.Write([]byte(`{"Status":0,"Answer":[{"type":1,"data":"5.6.7.8"}]}`))
	}))
	defer slow.Close()

	resp := make([]byte, 12)
	resp[2], resp[3] = 0x81, 0x83
	udpAddr, stop := startUDPResponder(t, resp)
	defer stop()

	r := &Resolver{
		Timeout: 2 * time.Second,
		DoH:     []DoHServer{{URL: nx.URL, Format: DoHJSON}, {URL: slow.URL, Format: DoHJSON}},
		UDP:     []string{udpAddr},
	}
	out, err := r.ResolveResilient(context.Background(), "filtered.example", "A")
	if err != nil || len(out.IPs) != 1 || out.IPs[0] != "5.6.7.8" {
		t.Fatalf("an address from a slower DoH server must beat an earlier NXDOMAIN, got %+v, %v", out, err)
	}
}

func TestResolveResilientKeepsNXDomainWhenAnotherServerIsSilent(t *testing.T) {
	nx := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"Status":3}`))
	}))
	defer nx.Close()
	release := make(chan struct{})
	hang := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer hang.Close()
	defer close(release)

	resp := make([]byte, 12)
	resp[2], resp[3] = 0x81, 0x83
	udpAddr, stop := startUDPResponder(t, resp)
	defer stop()

	r := &Resolver{
		Timeout: 2 * time.Second,
		DoH:     []DoHServer{{URL: nx.URL, Format: DoHJSON}, {URL: hang.URL, Format: DoHJSON}},
		UDP:     []string{udpAddr},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	_, err := r.ResolveResilient(ctx, "typo.example", "A")
	var nxErr *NXDomainError
	if !errors.As(err, &nxErr) {
		t.Fatalf("the NXDOMAIN must survive a DoH server that never answers, got %v", err)
	}
}
