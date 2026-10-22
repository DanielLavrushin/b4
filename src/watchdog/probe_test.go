package watchdog

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"fmt"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/netprobe"
	"github.com/daniellavrushin/b4/utils"
)

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
		if !utils.IsReservedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s must be refused: probing it reaches the network b4 runs on", s)
		}
	}

	public := []string{"1.1.1.1", "8.8.8.8", "104.22.45.1", "2606:4700::1111", "100.63.255.255", "100.128.0.1"}
	for _, s := range public {
		if utils.IsReservedAddr(netip.MustParseAddr(s)) {
			t.Errorf("%s is a public address and must not be refused", s)
		}
	}
}

func TestProbeHostRefusesLiteralPrivateAddress(t *testing.T) {
	var priv *ErrPrivateDestination
	_, err := ProbeHost(context.Background(), "192.168.1.1", ProbeOptions{Timeout: time.Second})
	if !errors.As(err, &priv) {
		t.Fatalf("a private literal must be refused before any connection, got %v", err)
	}
}

func TestProbeHostRejectsNonHostnames(t *testing.T) {
	for _, in := range []string{
		"http://10.0.0.1/cgi-bin/factory_reset",
		"https://127.0.0.1:8080/admin",
	} {
		var priv *ErrPrivateDestination
		_, err := ProbeHost(context.Background(), in, ProbeOptions{Timeout: time.Second})
		if !errors.As(err, &priv) {
			t.Errorf("%q reduces to a private host and must be refused, got %v", in, err)
		}
	}

	if _, err := ProbeHost(context.Background(), "   ", ProbeOptions{}); err == nil {
		t.Error("an empty host must be refused")
	}
}

func TestIsReservedHostOnlyJudgesLiterals(t *testing.T) {
	for _, in := range []string{"192.168.1.1", "127.0.0.1", "::1", "0.0.0.0", "https://10.0.0.1/admin"} {
		if !IsReservedHost(in) {
			t.Errorf("%q resolves to a reserved literal and must be refused", in)
		}
	}
	for _, in := range []string{"rutracker.org", "1.1.1.1", "example.com", ""} {
		if IsReservedHost(in) {
			t.Errorf("%q is not a reserved literal and must be allowed", in)
		}
	}
}

func fakeResolver(t *testing.T, answers map[uint16][][]byte) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })

	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			q := append([]byte(nil), buf[:n]...)
			if n < 12 {
				continue
			}
			i := 12
			for i < n && q[i] != 0 {
				i += int(q[i]) + 1
			}
			if i >= n || i+5 > n {
				continue
			}
			qtype := uint16(q[i+1])<<8 | uint16(q[i+2])
			resp := append([]byte(nil), q[:i+5]...)
			resp[2], resp[3] = 0x81, 0x80
			rrs := answers[qtype]
			resp[6], resp[7] = 0, byte(len(rrs))
			resp[8], resp[9], resp[10], resp[11] = 0, 0, 0, 0
			for _, rdata := range rrs {
				rr := []byte{0xC0, 0x0C, byte(qtype >> 8), byte(qtype), 0, 1, 0, 0, 0, 30, 0, byte(len(rdata))}
				resp = append(append(resp, rr...), rdata...)
			}
			pc.WriteTo(resp, from)
		}
	}()

	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", pc.LocalAddr().String())
		},
	}
	t.Cleanup(func() { net.DefaultResolver = previous })
}

func TestProbeHostRefusesWhenEveryAnswerIsReserved(t *testing.T) {
	fakeResolver(t, map[uint16][][]byte{
		1:  {{10, 0, 0, 7}, {192, 168, 3, 4}},
		28: {net.ParseIP("fd00::1").To16()},
	})

	var priv *ErrPrivateDestination
	_, err := ProbeHost(context.Background(), "sinkholed.invalid", ProbeOptions{Timeout: 3 * time.Second})
	if !errors.As(err, &priv) {
		t.Fatalf("a name whose every answer is reserved must be refused, got %v", err)
	}
}

func TestProbeHostDoesNotLatchARefusalWhenAPublicAnswerExists(t *testing.T) {
	fakeResolver(t, map[uint16][][]byte{
		1:  {{127, 0, 0, 1}, {203, 0, 113, 9}},
		28: {net.ParseIP("fd00::1").To16()},
	})

	var priv *ErrPrivateDestination
	res, err := ProbeHost(context.Background(), "mixed.invalid", ProbeOptions{Timeout: 3 * time.Second})
	if errors.As(err, &priv) {
		t.Fatalf("one reserved answer must not decide the verdict when a public address was also offered: %v", err)
	}
	if err != nil {
		t.Fatalf("the probe should report a verdict, not a caller error: %v", err)
	}
	if res.OK {
		t.Error("203.0.113.9 is unroutable here, so the probe cannot have succeeded")
	}
	if res.Verdict == "" {
		t.Error("a failed fetch must carry the verdict the tool exists to report")
	}
}

func TestChecksGoThroughTheEngine(t *testing.T) {
	cfg := config.NewConfig()

	if markThroughEngine == cfg.MainInjectedMark() {
		t.Fatalf("a check carrying the injected mark 0x%x is accepted before b4's queue, so it would report the unbypassed path",
			cfg.MainInjectedMark())
	}
	if markThroughEngine == cfg.DiscoveryFlowMark() {
		t.Fatalf("the discovery flow mark 0x%x only reaches a queue while a discovery run has installed its steering rules", cfg.DiscoveryFlowMark())
	}
	if markThroughEngine == uint(config.SelfDialMark) {
		t.Fatalf("the self-dial mark 0x%x is b4 exempting its own traffic, which is what a check must not do", config.SelfDialMark)
	}
}

func TestClassifyProbeBodyDoesNotCallAStallASuccess(t *testing.T) {
	res, err := classifyProbeBody(context.DeadlineExceeded, nil, 13*1024, 1300, 200)
	if err != nil {
		t.Fatalf("a stalled read is a verdict, not a caller error: %v", err)
	}
	if res.OK {
		t.Fatal("the read ran out of time, so the fetch did not succeed and must not be reported as working")
	}
	if res.Verdict != netprobe.DomainTCP16 {
		t.Errorf("a stall at %d bytes is inside b4's own 12-69KB fat-flow window and must be named as that signature, got %q", 13*1024, res.Verdict)
	}
	if res.BytesRead != 13*1024 || res.Speed == 0 {
		t.Errorf("how far it got before stalling is the useful part of the answer: %+v", res)
	}
}

func TestClassifyProbeBodyKeepsATimeoutOutOfInsufficientData(t *testing.T) {
	res, err := classifyProbeBody(context.DeadlineExceeded, nil, 200, 20, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != netprobe.DomainTimeout {
		t.Errorf("200 bytes and then a deadline is a timeout, not merely a short body, got %q", res.Verdict)
	}
	if strings.Contains(res.Error, "insufficient data") {
		t.Errorf("reporting the symptom hides the cause: %q", res.Error)
	}
}

func TestClassifyProbeBodyKeepsABlockPageOverATimeout(t *testing.T) {
	page := []byte("<html><body>Доступ к информационному ресурсу ограничен</body></html>")
	if netprobe.DetectBlockPageBody(page) == "" {
		t.Skip("this fixture is not recognised as a block page")
	}
	res, err := classifyProbeBody(context.DeadlineExceeded, page, 2000, 200, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != netprobe.DomainISPPage {
		t.Errorf("the body proves what happened, so it must win over the timeout that followed it, got %q", res.Verdict)
	}
}

func TestClassifyProbeBodyReportsCancellationAsCallerError(t *testing.T) {
	_, err := classifyProbeBody(context.Canceled, nil, 5000, 500, 200)
	if err == nil {
		t.Fatal("the caller went away; inventing a censorship verdict from that would be a false positive")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("the reason must survive: %v", err)
	}
}

func TestClassifyProbeBodyStillSucceedsOnACleanRead(t *testing.T) {
	res, err := classifyProbeBody(nil, nil, 4096, 4096, 200)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK || res.BytesRead != 4096 {
		t.Errorf("a complete read must still read as success: %+v", res)
	}
}

func TestClassifyProbeBodyNamesTheStatusOfAShortReply(t *testing.T) {
	res, err := classifyProbeBody(nil, nil, 577, 100, 202)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK || res.Verdict != netprobe.DomainError {
		t.Errorf("a reply under 1 KB still counts as a failed fetch: %+v", res)
	}
	if !res.Answered || res.StatusCode != 202 || res.BytesRead != 577 || !strings.Contains(res.Error, "HTTP 202") {
		t.Errorf("the server did answer, and the status is what tells a bot check from a block: %+v", res)
	}
}

func fakeNames(t *testing.T, names map[string][]string) {
	t.Helper()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { pc.Close() })

	go func() {
		buf := make([]byte, 1500)
		for {
			n, from, err := pc.ReadFrom(buf)
			if err != nil {
				return
			}
			if n < 12 {
				continue
			}
			q := append([]byte(nil), buf[:n]...)
			var labels []string
			i := 12
			for i < n && q[i] != 0 && i+1+int(q[i]) <= n {
				labels = append(labels, string(q[i+1:i+1+int(q[i])]))
				i += int(q[i]) + 1
			}
			if i >= n || i+5 > n {
				continue
			}
			qtype := uint16(q[i+1])<<8 | uint16(q[i+2])
			resp := append([]byte(nil), q[:i+5]...)
			resp[2], resp[3] = 0x81, 0x80
			addrs, known := names[strings.ToLower(strings.Join(labels, "."))]
			if !known {
				resp[3] = 0x83
			}
			var rrs [][]byte
			for _, s := range addrs {
				addr := netip.MustParseAddr(s)
				switch {
				case qtype == 1 && addr.Is4():
					b := addr.As4()
					rrs = append(rrs, b[:])
				case qtype == 28 && addr.Is6():
					b := addr.As16()
					rrs = append(rrs, b[:])
				}
			}
			resp[6], resp[7] = 0, byte(len(rrs))
			resp[8], resp[9], resp[10], resp[11] = 0, 0, 0, 0
			for _, rdata := range rrs {
				rr := []byte{0xC0, 0x0C, byte(qtype >> 8), byte(qtype), 0, 1, 0, 0, 0, 30, 0, byte(len(rdata))}
				resp = append(append(resp, rr...), rdata...)
			}
			pc.WriteTo(resp, from)
		}
	}()

	previous := net.DefaultResolver
	net.DefaultResolver = &net.Resolver{
		PreferGo: true,
		Dial: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, "udp", pc.LocalAddr().String())
		},
	}
	t.Cleanup(func() { net.DefaultResolver = previous })
}

type probeLab struct {
	port string
	mu   sync.Mutex
	hits map[string]int
}

func newProbeLab(t *testing.T, names map[string][]string, serve func(host string, w http.ResponseWriter, r *http.Request)) *probeLab {
	t.Helper()
	lab := &probeLab{hits: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		lab.mu.Lock()
		lab.hits[host]++
		lab.mu.Unlock()
		serve(host, w, r)
	}))
	t.Cleanup(srv.Close)
	_, lab.port, _ = net.SplitHostPort(srv.Listener.Addr().String())
	fakeNames(t, names)
	return lab
}

func (l *probeLab) url(host, path string) string {
	return "http://" + host + ":" + l.port + path
}

func (l *probeLab) probe(host string, isReserved func(netip.Addr) bool) (CheckResult, error) {
	return probe(context.Background(), host, l.url(host, "/"), ProbeOptions{Timeout: 3 * time.Second}, newDialGuard(isReserved), nil)
}

func (l *probeLab) hitsOf(host string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.hits[host]
}

var labLoopback = []string{"127.0.0.1"}

func TestProbeFollowsARedirectThatStaysOnTheSite(t *testing.T) {
	var lab *probeLab
	lab = newProbeLab(t, map[string][]string{"example.test": labLoopback, "www.example.test": labLoopback},
		func(host string, w http.ResponseWriter, r *http.Request) {
			if host == "example.test" {
				http.Redirect(w, r, lab.url("www.example.test", "/"), http.StatusMovedPermanently)
				return
			}
			fmt.Fprint(w, strings.Repeat("a", 4096))
		})

	res, err := lab.probe("example.test", allowLoopback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK || res.BytesRead != 4096 {
		t.Fatalf("a bare domain that sends its visitors to its own www. host loads: %+v", res)
	}
}

func TestProbeStopsAtARedirectToAnotherSite(t *testing.T) {
	var lab *probeLab
	lab = newProbeLab(t, map[string][]string{"example.test": labLoopback, "other.test": labLoopback},
		func(host string, w http.ResponseWriter, r *http.Request) {
			if host == "example.test" {
				http.Redirect(w, r, lab.url("other.test", "/alerts"), http.StatusFound)
				return
			}
			fmt.Fprint(w, strings.Repeat("a", 4096))
		})

	res, err := lab.probe("example.test", allowLoopback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK || res.Verdict != VerdictRedirect || res.RedirectTo != "other.test" || !res.Answered {
		t.Fatalf("another site's answer is not this domain's, so the probe stops and names the target: %+v", res)
	}
	if !strings.Contains(res.Error, "different site") {
		t.Errorf("the error must say why the redirect was not followed: %q", res.Error)
	}
	if n := lab.hitsOf("other.test"); n != 0 {
		t.Errorf("other.test must never be contacted, got %d request(s)", n)
	}
}

func TestProbeReportsABlockPageRedirect(t *testing.T) {
	lab := newProbeLab(t, map[string][]string{"example.test": labLoopback},
		func(host string, w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://warning.rt.ru/?id=1", http.StatusFound)
		})

	res, err := lab.probe("example.test", allowLoopback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != netprobe.DomainISPPage || res.Answered {
		t.Errorf("a redirect to a known block page is the ISP's answer, not the site's: %+v", res)
	}
}

func TestProbeTreatsARedirectIntoTheLANAsABlockPage(t *testing.T) {
	for _, target := range []string{"http://10.1.2.3/admin", "http://localhost/"} {
		lab := newProbeLab(t, map[string][]string{"example.test": labLoopback},
			func(host string, w http.ResponseWriter, r *http.Request) {
				http.Redirect(w, r, target, http.StatusFound)
			})

		var dialled []netip.Addr
		var mu sync.Mutex
		spy := func(a netip.Addr) bool {
			mu.Lock()
			dialled = append(dialled, a)
			mu.Unlock()
			return false
		}
		res, err := lab.probe("example.test", spy)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", target, err)
		}
		if res.Verdict != netprobe.DomainISPPage || !strings.Contains(res.Error, "private or local address") {
			t.Errorf("%s: a public site does not send its visitors into the LAN: %+v", target, res)
		}
		mu.Lock()
		if len(dialled) != 1 {
			t.Errorf("%s: only example.test may be dialled, the target never: %v", target, dialled)
		}
		mu.Unlock()
	}
}

func TestProbeFollowsFiveRedirectsAndStopsAtTheSixth(t *testing.T) {
	lab := newProbeLab(t, map[string][]string{"example.test": labLoopback},
		func(host string, w http.ResponseWriter, r *http.Request) {
			left, _ := strconv.Atoi(r.URL.Query().Get("left"))
			if left > 0 {
				http.Redirect(w, r, fmt.Sprintf("/?left=%d", left-1), http.StatusFound)
				return
			}
			fmt.Fprint(w, strings.Repeat("a", 2048))
		})

	start := func(left int) (CheckResult, error) {
		return probe(context.Background(), "example.test", lab.url("example.test", fmt.Sprintf("/?left=%d", left)),
			ProbeOptions{Timeout: 3 * time.Second}, newDialGuard(allowLoopback), nil)
	}
	if res, err := start(ProbeMaxRedirects); err != nil || !res.OK {
		t.Errorf("%d redirects within the site are followed: %+v %v", ProbeMaxRedirects, res, err)
	}
	res, err := start(ProbeMaxRedirects + 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK || res.Verdict != VerdictRedirect || !res.Answered || res.RedirectTo != "example.test" {
		t.Fatalf("one redirect more is a chain that does not settle: %+v", res)
	}
	if !strings.Contains(res.Error, fmt.Sprintf("after %d redirects", ProbeMaxRedirects)) {
		t.Errorf("the count must match what was followed: %q", res.Error)
	}
}

func TestProbeReportsASinkholedRedirectTarget(t *testing.T) {
	var lab *probeLab
	lab = newProbeLab(t, map[string][]string{"example.test": labLoopback, "www.example.test": {"10.0.0.7"}},
		func(host string, w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, lab.url("www.example.test", "/"), http.StatusMovedPermanently)
		})

	tenOnly := func(a netip.Addr) bool { return a.Is4() && a.As4()[0] == 10 }
	_, err := lab.probe("example.test", tenOnly)
	var priv *ErrPrivateDestination
	if !errors.As(err, &priv) {
		t.Fatalf("the first hop's usable dial must not hide a refusal on the second: %v", err)
	}
	if priv.Addr != "10.0.0.7" || priv.Host != "www.example.test" {
		t.Errorf("the refusal must name the hop whose answer is private: %+v", priv)
	}
}

func wwwLab(t *testing.T, www func(w http.ResponseWriter, r *http.Request)) *probeLab {
	t.Helper()
	var lab *probeLab
	lab = newProbeLab(t, map[string][]string{"example.test": labLoopback, "www.example.test": labLoopback},
		func(host string, w http.ResponseWriter, r *http.Request) {
			if host == "example.test" {
				http.Redirect(w, r, lab.url("www.example.test", "/"), http.StatusMovedPermanently)
				return
			}
			www(w, r)
		})
	return lab
}

func TestProbeNamesTheHopThatSentAShortReply(t *testing.T) {
	lab := wwwLab(t, func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusAccepted) })

	res, err := lab.probe("example.test", allowLoopback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK || res.StatusCode != http.StatusAccepted || !res.Answered {
		t.Fatalf("an empty 202 is a short reply: %+v", res)
	}
	if !strings.Contains(res.Error, "HTTP 202") || !strings.Contains(res.Error, "at www.example.test after a redirect from example.test") {
		t.Errorf("a failure past a redirect must name the host that failed: %q", res.Error)
	}
}

func TestProbeNamesTheHopThatDroppedTheConnection(t *testing.T) {
	lab := wwwLab(t, func(w http.ResponseWriter, _ *http.Request) {
		if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
			conn.Close()
		}
	})

	res, err := lab.probe("example.test", allowLoopback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OK || res.Answered || !strings.Contains(res.Error, "at www.example.test after a redirect from example.test") {
		t.Errorf("a dropped connection past a redirect must name the host that dropped it: %+v", res)
	}
}

func labCert(t *testing.T, names ...string) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: names[0]},
		DNSNames:              names,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(leaf)
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}, pool
}

func TestProbeSpeaksHTTP2OnEveryHop(t *testing.T) {
	cert, pool := labCert(t, "example.test", "www.example.test")
	var mu sync.Mutex
	protos := map[string]int{}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host, _, _ := net.SplitHostPort(r.Host)
		mu.Lock()
		protos[host] = r.ProtoMajor
		mu.Unlock()
		if host == "example.test" {
			http.Redirect(w, r, "https://www."+r.Host+"/", http.StatusMovedPermanently)
			return
		}
		fmt.Fprint(w, strings.Repeat("a", 4096))
	}))
	srv.EnableHTTP2 = true
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	_, port, _ := net.SplitHostPort(srv.Listener.Addr().String())
	fakeNames(t, map[string][]string{"example.test": labLoopback, "www.example.test": labLoopback})

	dials := 0
	spy := func(netip.Addr) bool {
		mu.Lock()
		dials++
		mu.Unlock()
		return false
	}
	res, err := probe(context.Background(), "example.test", "https://example.test:"+port+"/",
		ProbeOptions{Timeout: 3 * time.Second}, newDialGuard(spy), pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.OK {
		t.Fatalf("the www. host serves the page: %+v", res)
	}
	mu.Lock()
	defer mu.Unlock()
	if protos["example.test"] != 2 || protos["www.example.test"] != 2 {
		t.Errorf("browsers speak HTTP/2, and some sites answer HTTP/1.1 clients with an empty error page: %v", protos)
	}
	if dials != 2 {
		t.Errorf("each host is its own connection, so the guard must judge %d dials, saw %d", 2, dials)
	}
}

func TestProbeNamesTheHopForEveryLaterOutcome(t *testing.T) {
	for _, c := range []struct {
		name    string
		path    string
		www     func(lab *probeLab, w http.ResponseWriter, r *http.Request)
		verdict netprobe.DomainStatus
	}{
		{"an ISP block status", "/", func(_ *probeLab, w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusUnavailableForLegalReasons)
		}, netprobe.DomainISPPage},
		{"a redirect to another site", "/", func(lab *probeLab, w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, lab.url("other.test", "/"), http.StatusFound)
		}, VerdictRedirect},
		{"a dropped connection on a path that reads like a verdict", "/alerts", func(_ *probeLab, w http.ResponseWriter, _ *http.Request) {
			if conn, _, err := w.(http.Hijacker).Hijack(); err == nil {
				conn.Close()
			}
		}, netprobe.DomainTLSReset},
	} {
		var lab *probeLab
		lab = newProbeLab(t, map[string][]string{"example.test": labLoopback, "www.example.test": labLoopback, "other.test": labLoopback},
			func(host string, w http.ResponseWriter, r *http.Request) {
				if host == "example.test" {
					http.Redirect(w, r, lab.url("www.example.test", c.path), http.StatusMovedPermanently)
					return
				}
				c.www(lab, w, r)
			})

		res, err := lab.probe("example.test", allowLoopback)
		if err != nil {
			t.Fatalf("%s: unexpected error: %v", c.name, err)
		}
		if res.Verdict != c.verdict {
			t.Errorf("%s: verdict %q, want %q (the URL text must not decide it): %+v", c.name, res.Verdict, c.verdict, res)
		}
		if !strings.Contains(res.Error, "at www.example.test after a redirect from example.test") {
			t.Errorf("%s: a result past a redirect must name the host that produced it: %q", c.name, res.Error)
		}
	}
}

func TestProbeComparesHopsWithoutTheTrailingDot(t *testing.T) {
	var lab *probeLab
	lab = newProbeLab(t, map[string][]string{"example.test": labLoopback},
		func(host string, w http.ResponseWriter, r *http.Request) {
			if !strings.HasSuffix(host, ".") {
				http.Redirect(w, r, lab.url("example.test.", "/"), http.StatusFound)
				return
			}
			w.WriteHeader(http.StatusAccepted)
		})

	res, err := lab.probe("example.test", allowLoopback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !res.Answered || strings.Contains(res.Error, "after a redirect") {
		t.Errorf("example.test. is the probed host itself, not a redirect target: %+v", res)
	}
}

func TestProbeStopsAtARedirectThatNamesNoHost(t *testing.T) {
	lab := newProbeLab(t, map[string][]string{"example.test": labLoopback},
		func(host string, w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "http:///nowhere")
			w.WriteHeader(http.StatusFound)
		})

	res, err := lab.probe("example.test", allowLoopback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictRedirect || !res.Answered || res.RedirectTo != "" || !strings.Contains(res.Error, "names no host") {
		t.Errorf("the site answered, with a redirect that leads nowhere: %+v", res)
	}
}

func TestProbeCallsAnAddressTargetABareAddress(t *testing.T) {
	lab := newProbeLab(t, map[string][]string{"example.test": labLoopback},
		func(host string, w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://203.0.113.5/", http.StatusFound)
		})

	res, err := lab.probe("example.test", allowLoopback)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.Verdict != VerdictRedirect || res.RedirectTo != "203.0.113.5" || !strings.Contains(res.Error, "bare address") {
		t.Errorf("an address is not a site the AI can test by name: %+v", res)
	}
}

func TestValidateProbeHostRefusesWhatCannotBeFetched(t *testing.T) {
	for _, bad := range []string{"", "a b.com", "user@example.com", "example.com#top", "example.com/path", "пример.рф"} {
		if ValidateProbeHost(bad) == nil {
			t.Errorf("%q must be refused before any fetch", bad)
		}
	}
	if err := ValidateProbeHost("пример.рф"); err == nil || !strings.Contains(err.Error(), "xn--") {
		t.Errorf("a name outside ASCII must be refused with the spelling DNS uses: %v", err)
	}
	for _, good := range []string{"example.com", "xn--e1afmkfd.xn--p1ai", "1.1.1.1", "www.youtube.com"} {
		if err := ValidateProbeHost(good); err != nil {
			t.Errorf("%q is a valid probe target: %v", good, err)
		}
	}
}

func TestCheckerSkipsTheMarkSyscallWhenThereIsNoMarkToSet(t *testing.T) {
	if netprobe.Dialer(int(markThroughEngine), time.Second, time.Second).Control != nil {
		t.Errorf("a check through the engine sets no mark, so it must not make the privileged SO_MARK call at all: " +
			"setting it to 0 changes nothing and can only fail with EPERM, which would fail every check and trigger healing")
	}
	if netprobe.Dialer(0x8000, time.Second, time.Second).Control == nil {
		t.Error("a real mark must still be applied")
	}
}
