package http

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestAConnectionOpenedUnderLoginKeepsNeedingATokenAfterLoginIsTurnedOff(t *testing.T) {
	withAuth := config.NewConfig()
	withAuth.System.WebServer.Username = "admin"
	withAuth.System.WebServer.Password = "hashed"
	cfgPtr := &atomic.Pointer[config.Config]{}
	cfgPtr.Store(&withAuth)

	srv := httptest.NewUnstartedServer(authMiddleware(cfgPtr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	srv.Config.ConnContext = authAtAccept(cfgPtr)
	srv.Start()
	t.Cleanup(srv.Close)

	do := func(client *http.Client, token string) *http.Response {
		t.Helper()
		req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/config", nil)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		return resp
	}
	get := func(client *http.Client, token string) int {
		t.Helper()
		return do(client, token).StatusCode
	}

	early := &http.Client{Transport: &http.Transport{MaxConnsPerHost: 1}}
	t.Cleanup(early.CloseIdleConnections)
	if got := get(early, ""); got != http.StatusUnauthorized {
		t.Fatalf("login is on, a request without a token must be refused, got %d", got)
	}

	withoutAuth := config.NewConfig()
	cfgPtr.Store(&withoutAuth)

	resp := do(early, "")
	if resp.StatusCode != http.StatusUnauthorized || !resp.Close {
		t.Fatalf("a connection accepted while login was required must be refused and closed, got %d close=%v", resp.StatusCode, resp.Close)
	}
	token, err := issueToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	t.Cleanup(func() { revokeToken(token) })
	if got := get(early, token); got != http.StatusOK {
		t.Fatalf("the owner's session token keeps working, got %d", got)
	}

	fresh := &http.Client{Transport: &http.Transport{}}
	t.Cleanup(fresh.CloseIdleConnections)
	if got := get(fresh, ""); got != http.StatusOK {
		t.Fatalf("a connection opened after login was turned off needs no token, got %d", got)
	}
}

func TestAConnectionAcceptedJustAfterLoginWasDroppedStillNeedsAToken(t *testing.T) {
	withoutAuth := config.NewConfig()
	cfgPtr := &atomic.Pointer[config.Config]{}
	cfgPtr.Store(&withoutAuth)
	prev := loginDropGrace
	t.Cleanup(func() {
		loginDropGrace = prev
		loginDroppedAt.Store(0)
	})

	srv := httptest.NewUnstartedServer(authMiddleware(cfgPtr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})))
	srv.Config.ConnContext = authAtAccept(cfgPtr)
	srv.Start()
	t.Cleanup(srv.Close)

	noteLoginDropped()
	status := func() int {
		client := &http.Client{Transport: &http.Transport{}}
		defer client.CloseIdleConnections()
		resp, err := client.Get(srv.URL + "/api/config")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		resp.Body.Close()
		return resp.StatusCode
	}
	if got := status(); got != http.StatusUnauthorized {
		t.Fatalf("a TCP handshake started before login was dropped can complete after it; such a connection must still need a token, got %d", got)
	}

	loginDropGrace = 0
	if got := status(); got != http.StatusOK {
		t.Fatalf("after the grace window a new connection needs no token, got %d", got)
	}
}
