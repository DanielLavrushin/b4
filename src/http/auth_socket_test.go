package http

import (
	"crypto/tls"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/ws"
	"github.com/gorilla/websocket"
)

type socketRig struct {
	srv      *httptest.Server
	returned chan string
}

func newSocketRig(t *testing.T, useTLS bool) *socketRig {
	t.Helper()
	cfg := config.NewConfig()
	cfg.System.WebServer.Username = "admin"
	cfg.System.WebServer.Password = "hashed"
	cfgPtr := &atomic.Pointer[config.Config]{}
	cfgPtr.Store(&cfg)

	rig := &socketRig{returned: make(chan string, 8)}
	mux := http.NewServeMux()
	registerAuthEndpoints(mux, cfgPtr)
	mux.HandleFunc("/api/ws/connections", ws.HandleConnectionsWebSocket)

	guarded := authMiddleware(cfgPtr, mux)
	rig.srv = httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		guarded.ServeHTTP(w, r)
		if isWebSocketRequest(r) {
			rig.returned <- r.URL.Query().Get("token")
		}
	}))
	rig.srv.Config.ConnContext = authAtAccept(cfgPtr)
	if useTLS {
		rig.srv.StartTLS()
	} else {
		rig.srv.Start()
	}
	t.Cleanup(rig.srv.Close)
	return rig
}

func (rig *socketRig) dial(t *testing.T, token string) (*websocket.Conn, *http.Response, error) {
	t.Helper()
	dialer := websocket.Dialer{
		HandshakeTimeout: 5 * time.Second,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: true},
	}
	url := "ws" + strings.TrimPrefix(rig.srv.URL, "http") + "/api/ws/connections?token=" + token
	return dialer.Dial(url, nil)
}

func (rig *socketRig) open(t *testing.T, token string) *websocket.Conn {
	t.Helper()
	conn, _, err := rig.dial(t, token)
	if err != nil {
		t.Fatalf("a socket with a live token must open: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func (rig *socketRig) signOut(t *testing.T, token string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, rig.srv.URL+"/api/auth/logout", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := rig.srv.Client().Do(req)
	if err != nil {
		t.Fatalf("sign out: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("sign out answered %d", resp.StatusCode)
	}
}

func newSessionToken(t *testing.T) string {
	t.Helper()
	token, err := issueToken()
	if err != nil {
		t.Fatalf("issue token: %v", err)
	}
	t.Cleanup(func() { revokeToken(token) })
	return token
}

func expectClosed(t *testing.T, conn *websocket.Conn) {
	t.Helper()
	conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	for {
		_, _, err := conn.ReadMessage()
		if err == nil {
			continue
		}
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatalf("the socket of a signed-out session stayed open")
		}
		return
	}
}

func TestSigningOutClosesTheSocketsOfThatSession(t *testing.T) {
	for _, tc := range []struct {
		name   string
		useTLS bool
	}{{"http", false}, {"https", true}} {
		t.Run(tc.name, func(t *testing.T) {
			rig := newSocketRig(t, tc.useTLS)
			leaving := newSessionToken(t)
			staying := newSessionToken(t)

			first := rig.open(t, leaving)
			second := rig.open(t, leaving)
			other := rig.open(t, staying)

			rig.signOut(t, leaving)

			expectClosed(t, first)
			expectClosed(t, second)
			for i := 0; i < 2; i++ {
				select {
				case token := <-rig.returned:
					if token != leaving {
						t.Fatalf("the handler of another session returned")
					}
				case <-time.After(3 * time.Second):
					t.Fatalf("a handler of the signed-out session is still running")
				}
			}

			select {
			case <-rig.returned:
				t.Fatalf("signing out one session closed a socket of another")
			case <-time.After(200 * time.Millisecond):
			}
			if err := other.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
				t.Fatalf("the other session's socket must stay usable: %v", err)
			}

			if _, resp, err := rig.dial(t, leaving); err == nil || resp == nil || resp.StatusCode != http.StatusUnauthorized {
				t.Fatalf("a revoked token must not open a new socket, err=%v", err)
			}
			<-rig.returned
		})
	}
}

func TestAnOpenSocketKeepsItsSessionAlive(t *testing.T) {
	rig := newSocketRig(t, false)
	token := newSessionToken(t)
	conn := rig.open(t, token)

	expire := func() {
		activeTokensMu.Lock()
		activeTokens[token] = time.Now().Add(-time.Minute)
		activeTokensMu.Unlock()
	}

	expire()
	if !validateToken(token) {
		t.Fatalf("a session with an open socket is in use and must not expire")
	}

	expire()
	if _, err := issueToken(); err != nil {
		t.Fatalf("issue token: %v", err)
	}
	activeTokensMu.Lock()
	_, kept := activeTokens[token]
	activeTokensMu.Unlock()
	if !kept {
		t.Fatalf("the sweep of expired tokens dropped a session with an open socket")
	}

	expire()
	conn.Close()
	select {
	case <-rig.returned:
	case <-time.After(3 * time.Second):
		t.Fatalf("the handler did not return after the client closed the socket")
	}
	activeTokensMu.Lock()
	expiry := activeTokens[token]
	tracked := len(tokenSockets[token])
	activeTokensMu.Unlock()
	if tracked != 0 {
		t.Fatalf("a closed socket is still tracked")
	}
	if time.Until(expiry) < tokenTTL-time.Minute {
		t.Fatalf("closing the last socket must start a full idle period, expiry in %s", time.Until(expiry))
	}

	expire()
	if validateToken(token) {
		t.Fatalf("an idle session without sockets must expire")
	}
}
