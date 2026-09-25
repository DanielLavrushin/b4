package mtproto

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

type frontEdge struct {
	mu      sync.Mutex
	names   []string
	hosts   []string
	swallow map[string]bool
	stop    chan struct{}
}

func edgeCertificate(t *testing.T, names ...string) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: names[0]},
		DNSNames:     names,
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
}

func startFrontEdge(t *testing.T, swallow ...string) (*frontEdge, string) {
	t.Helper()
	return startEdgeWithCert(t, edgeCertificate(t, "*.telegram.org"), swallow...)
}

func startEdgeWithCert(t *testing.T, cert tls.Certificate, swallow ...string) (*frontEdge, string) {
	t.Helper()
	e := &frontEdge{swallow: map[string]bool{}, stop: make(chan struct{})}
	for _, n := range swallow {
		e.swallow[n] = true
	}
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		e.mu.Lock()
		e.hosts = append(e.hosts, r.Host)
		e.mu.Unlock()
		hj, ok := w.(http.Hijacker)
		if !ok {
			return
		}
		conn, bufrw, err := hj.Hijack()
		if err != nil {
			return
		}
		defer conn.Close()
		_, _ = bufrw.WriteString("HTTP/1.1 101 Switching Protocols\r\nUpgrade: websocket\r\nConnection: Upgrade\r\n\r\n")
		_ = bufrw.Flush()
		select {
		case <-e.stop:
		case <-time.After(2 * time.Second):
		}
	}))
	srv.TLS = &tls.Config{Certificates: []tls.Certificate{cert}, GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
		e.mu.Lock()
		e.names = append(e.names, h.ServerName)
		hang := e.swallow[h.ServerName]
		e.mu.Unlock()
		if hang {
			select {
			case <-e.stop:
			case <-time.After(3 * time.Second):
			}
		}
		return nil, nil
	}}
	srv.StartTLS()
	t.Cleanup(func() {
		close(e.stop)
		srv.CloseClientConnections()
		srv.Close()
	})
	_, port, _ := net.SplitHostPort(strings.TrimPrefix(srv.URL, "https://"))
	withWSDialPort(t, port)
	return e, port
}

func (e *frontEdge) seen() ([]string, []string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.names...), append([]string(nil), e.hosts...)
}

func TestFrontedDialSendsTheFrontNameAndKeepsTheHost(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	e, _ := startFrontEdge(t)

	conn, err := dialWSAs("127.0.0.1", "sprinthost.ru", "kws2.web.telegram.org", "", 2*time.Second, 0)
	if err != nil {
		t.Fatalf("fronted dial: %v", err)
	}
	_ = conn.Close()
	names, hosts := e.seen()
	if len(names) != 1 || names[0] != "sprinthost.ru" {
		t.Fatalf("TLS name sent %v, want sprinthost.ru", names)
	}
	if len(hosts) != 1 || hosts[0] != "kws2.web.telegram.org" {
		t.Fatalf("Host sent %v, want kws2.web.telegram.org: the edge picks the cluster by Host", hosts)
	}
}

func TestPoolFrontsTheEdgeWhenItSwallowsItsOwnNames(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	e, _ := startFrontEdge(t, "kws2.web.telegram.org")
	cfg := MTProtoUpstream{WSEndpointHost: "127.0.0.1", FrontSNI: "sprinthost.ru"}
	p := newWSPool(cfg, 0, 1)
	defer p.close()

	plans := wsPlansForDC(2, &cfg)
	c, err := p.dialPlan(2, plans[0], time.Second)
	if err != nil {
		t.Fatalf("the pool found no way in: %v", err)
	}
	_ = c.conn.Close()
	if c.plan.frontSNI != "sprinthost.ru" || c.plan.sni != "kws2.web.telegram.org" {
		t.Fatalf("spare dialled as %s, want kws2 under the front name", c.plan.describe())
	}
	if !wsFrontPreferred("127.0.0.1", "sprinthost.ru") {
		t.Fatal("a working front name was not remembered")
	}
	for _, pl := range wsPlansForDC(2, &cfg) {
		if pl.native && (pl.frontSNI != "sprinthost.ru" || pl.sni != "kws2.web.telegram.org") {
			t.Fatalf("pool plan %s after fronting worked", pl.describe())
		}
	}
	_, hosts := e.seen()
	for _, h := range hosts {
		if h != "kws2.web.telegram.org" {
			t.Fatalf("a primary session asked the edge for %s", h)
		}
	}
}

func TestPoolGoesBackToTheEdgeNamesWhenFrontingStops(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	startFrontEdge(t, "sprinthost.ru")
	cfg := MTProtoUpstream{WSEndpointHost: "127.0.0.1", FrontSNI: "sprinthost.ru"}
	p := newWSPool(cfg, 0, 1)
	defer p.close()
	wsFrontRecord("127.0.0.1", "sprinthost.ru", true)

	c, err := p.dialPlan(2, wsPlansForDC(2, &cfg)[0], time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = c.conn.Close()
	if c.plan.frontSNI != "" || wsFrontPreferred("127.0.0.1", "sprinthost.ru") {
		t.Fatal("the pool stayed on a front name the edge no longer answers")
	}
}

func TestFrontingThatLandsElsewhereIsRefusedBeforeAnyRequest(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	e, _ := startEdgeWithCert(t, edgeCertificate(t, "www.example.com"), "kws2.web.telegram.org")
	cfg := MTProtoUpstream{WSEndpointHost: "127.0.0.1", FrontSNI: "sprinthost.ru"}
	p := newWSPool(cfg, 0, 1)
	defer p.close()

	if _, err := p.dialPlan(2, wsPlansForDC(2, &cfg)[0], time.Second); err == nil {
		t.Fatal("a host that is not Telegram's edge was accepted")
	}
	if !wsFrontRefused("127.0.0.1", "sprinthost.ru") {
		t.Fatal("a front name that led somewhere else was not remembered")
	}
	if wsFrontPreferred("127.0.0.1", "sprinthost.ru") {
		t.Fatal("a front name that led somewhere else was preferred")
	}
	if _, hosts := e.seen(); len(hosts) != 0 {
		t.Fatalf("a request for %v reached a host that is not Telegram's edge", hosts)
	}
}

func TestFrontingIsTriedAtMostOncePerInterval(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	e, _ := startFrontEdge(t, "kws2.web.telegram.org", "sprinthost.ru")
	cfg := MTProtoUpstream{WSEndpointHost: "127.0.0.1", FrontSNI: "sprinthost.ru"}
	p := newWSPool(cfg, 0, 1)
	defer p.close()

	plan := wsPlansForDC(2, &cfg)[0]
	for i := 0; i < 2; i++ {
		if _, err := p.dialPlan(2, plan, 600*time.Millisecond); err == nil {
			t.Fatal("an edge that swallows every name answered")
		}
	}
	names, _ := e.seen()
	fronted := 0
	for _, n := range names {
		if n == "sprinthost.ru" {
			fronted++
		}
	}
	if fronted != 1 {
		t.Fatalf("the front name was tried %d times within one interval, want 1 (names %v)", fronted, names)
	}
}

func TestAShortTLSWindowIsNotBlamedOnTheName(t *testing.T) {
	if isTLSStage(&wsTLSError{err: net.ErrClosed, judged: false}) {
		t.Fatal("a handshake left a sliver of time by a slow connect was judged as filtered")
	}
	if !isTLSStage(&wsTLSError{err: net.ErrClosed, judged: true}) {
		t.Fatal("a handshake that had time and still stalled was not judged")
	}
}

func TestClientPlansCarryTheFrontNameOnlyOnceItWorked(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	cfg := &config.MTProtoConfig{UpstreamMode: "ws", WSFrontSNI: "sprinthost.ru"}
	edge := func(c *config.MTProtoConfig) transportPlan {
		plans, err := planTransports(c, config.QueueConfig{}, 2, dialTarget{})
		if err != nil || len(plans) == 0 || !plans[0].native {
			t.Fatalf("plans %v, %v", plans, err)
		}
		return plans[0]
	}
	if p := edge(cfg); p.frontSNI != "" {
		t.Fatalf("an untested front name was used on a client session: %s", p.describe())
	}
	wsFrontRecord(telegramWSEdgeIP, "sprinthost.ru", true)
	if p := edge(cfg); p.frontSNI != "sprinthost.ru" || p.sni != "kws2.web.telegram.org" {
		t.Fatalf("client plan %s after the front name worked", p.describe())
	}
	if p := edge(&config.MTProtoConfig{UpstreamMode: "ws"}); p.frontSNI != "" {
		t.Fatalf("fronting is off by default, yet the plan is %s", p.describe())
	}
	if p := edge(&config.MTProtoConfig{UpstreamMode: "ws", WSFrontSNI: "cdn.example.org"}); p.frontSNI != "" {
		t.Fatalf("a name that never worked was used on a client session: %s", p.describe())
	}
}

func TestFrontNameSetting(t *testing.T) {
	for in, want := range map[string]string{"": "", "off": "", "OFF": "", " cdn.example.org ": "cdn.example.org"} {
		if got := wsFrontName(in); got != want {
			t.Errorf("wsFrontName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestPoolDropsAFrontNameThatStopsReachingTelegram(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	startEdgeWithCert(t, edgeCertificate(t, "www.example.com"), "kws2.web.telegram.org")
	cfg := MTProtoUpstream{WSEndpointHost: "127.0.0.1", FrontSNI: "sprinthost.ru"}
	p := newWSPool(cfg, 0, 1)
	defer p.close()
	wsFrontRecord("127.0.0.1", "sprinthost.ru", true)

	plan := wsPlansForDC(2, &cfg)[0]
	if plan.frontSNI != "sprinthost.ru" {
		t.Fatalf("setup: plan %s is not fronted", plan.describe())
	}
	if _, err := p.dialPlan(2, plan, time.Second); err == nil {
		t.Fatal("an edge answering neither name was accepted")
	}
	if wsFrontPreferred("127.0.0.1", "sprinthost.ru") || !wsFrontRefused("127.0.0.1", "sprinthost.ru") {
		t.Fatal("a front name that now lands elsewhere stayed in use")
	}
	if next := wsPlansForDC(2, &cfg)[0]; next.frontSNI != "" {
		t.Fatalf("the pool kept dialling %s", next.describe())
	}
}
