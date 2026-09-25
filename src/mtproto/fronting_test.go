package mtproto

import (
	"crypto/tls"
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

func startFrontEdge(t *testing.T, swallow ...string) (*frontEdge, string) {
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
	srv.TLS = &tls.Config{GetConfigForClient: func(h *tls.ClientHelloInfo) (*tls.Config, error) {
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
	if !wsFrontPreferred("127.0.0.1") {
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
	if c.plan.frontSNI != "" || wsFrontPreferred("127.0.0.1") {
		t.Fatal("the pool stayed on a front name the edge no longer answers")
	}
}

func TestFrontingIsNotTriedWhenTheAddressIsUnreachable(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	_, port, _ := net.SplitHostPort(ln.Addr().String())
	_ = ln.Close()
	withWSDialPort(t, port)
	cfg := MTProtoUpstream{WSEndpointHost: "127.0.0.1", FrontSNI: "sprinthost.ru"}
	p := newWSPool(cfg, 0, 1)
	defer p.close()

	if _, err := p.dialPlan(2, wsPlansForDC(2, &cfg)[0], 500*time.Millisecond); err == nil {
		t.Fatal("a closed port answered")
	}
	if wsFrontPreferred("127.0.0.1") {
		t.Fatal("fronting was recorded for an address that refused the connection")
	}
}

func TestClientPlansCarryTheFrontNameOnlyOnceItWorked(t *testing.T) {
	wsResetState()
	t.Cleanup(wsResetState)
	cfg := &config.MTProtoConfig{UpstreamMode: "ws"}
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
	wsFrontRecord(telegramWSEdgeIP, wsDefaultFrontSNI, true)
	if p := edge(cfg); p.frontSNI != wsDefaultFrontSNI || p.sni != "kws2.web.telegram.org" {
		t.Fatalf("client plan %s after the front name worked", p.describe())
	}
	if p := edge(&config.MTProtoConfig{UpstreamMode: "ws", WSFrontSNI: "off"}); p.frontSNI != "" {
		t.Fatalf("fronting switched off, yet the plan is %s", p.describe())
	}
}

func TestFrontNameSetting(t *testing.T) {
	for in, want := range map[string]string{"": wsDefaultFrontSNI, "off": "", " cdn.example.org ": "cdn.example.org"} {
		if got := wsFrontName(in); got != want {
			t.Errorf("wsFrontName(%q) = %q, want %q", in, got, want)
		}
	}
}
