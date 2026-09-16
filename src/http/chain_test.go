package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	stdhttp "net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func startChain(t *testing.T, username string) (string, *stdhttp.Server) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	ln.Close()
	cfg := config.NewConfig()
	cfg.ConfigPath = filepath.Join(t.TempDir(), "config.json")
	cfg.System.WebServer.BindAddress = "127.0.0.1"
	cfg.System.WebServer.Port = port
	cfg.System.WebServer.Username = username
	if username != "" {
		hash, err := config.HashPassword(username)
		if err != nil {
			t.Fatal(err)
		}
		cfg.System.WebServer.Password = hash
	}
	ptr := &atomic.Pointer[config.Config]{}
	ptr.Store(&cfg)
	srv, _, err := StartServer(ptr, nil)
	if err != nil || srv == nil {
		t.Fatalf("StartServer: %v", err)
	}
	t.Cleanup(func() { _ = srv.Shutdown(context.Background()) })
	base := fmt.Sprintf("http://127.0.0.1:%d", port)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if resp, err := stdhttp.Get(base + "/api/version"); err == nil {
			resp.Body.Close()
			return base, srv
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("server did not come up")
	return "", nil
}

type browserRequest struct {
	method, path, origin, fetchSite, token string
	body                                   interface{}
}

func (br browserRequest) send(t *testing.T, base string) *stdhttp.Response {
	t.Helper()
	var body *bytes.Reader
	if br.body != nil {
		raw, _ := json.Marshal(br.body)
		body = bytes.NewReader(raw)
	} else {
		body = bytes.NewReader(nil)
	}
	req, err := stdhttp.NewRequest(br.method, base+br.path, body)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if br.origin != "" {
		req.Header.Set("Origin", br.origin)
	}
	if br.fetchSite != "" {
		req.Header.Set("Sec-Fetch-Site", br.fetchSite)
	}
	if br.token != "" {
		req.Header.Set("Authorization", "Bearer "+br.token)
	}
	resp, err := stdhttp.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp
}

func TestServerChainKeepsTheUIWorkingAndBlocksForeignPages(t *testing.T) {
	base, _ := startChain(t, "")
	own := base

	resp := browserRequest{method: stdhttp.MethodGet, path: "/"}.send(t, base)
	if resp.StatusCode != stdhttp.StatusOK {
		t.Fatalf("the web interface must be served, got %d", resp.StatusCode)
	}
	resp = browserRequest{method: stdhttp.MethodPost, path: "/api/hub/sync", origin: own, fetchSite: "same-origin"}.send(t, base)
	if resp.StatusCode == stdhttp.StatusForbidden || resp.StatusCode == stdhttp.StatusUnauthorized {
		t.Fatalf("the interface's own write must reach the handler, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != own {
		t.Fatalf("the interface's own origin keeps its grant, got %q", resp.Header.Get("Access-Control-Allow-Origin"))
	}
	resp = browserRequest{method: stdhttp.MethodPost, path: "/api/hub/sync", origin: own}.send(t, base)
	if resp.StatusCode == stdhttp.StatusForbidden {
		t.Fatalf("an older browser without Sec-Fetch-Site is recognised by its origin, got %d", resp.StatusCode)
	}
	resp = browserRequest{method: stdhttp.MethodPost, path: "/api/hub/identity/restore", origin: "http://evil.example", fetchSite: "cross-site", body: map[string]string{"code": "x"}}.send(t, base)
	if resp.StatusCode != stdhttp.StatusForbidden || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("a foreign page's write must be refused without a grant, got %d %q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}
	resp = browserRequest{method: stdhttp.MethodGet, path: "/api/version", origin: "http://evil.example", fetchSite: "cross-site"}.send(t, base)
	if resp.StatusCode != stdhttp.StatusOK || resp.Header.Get("Access-Control-Allow-Origin") != "" {
		t.Fatalf("a foreign read is answered without a grant, got %d %q", resp.StatusCode, resp.Header.Get("Access-Control-Allow-Origin"))
	}
	resp = browserRequest{method: stdhttp.MethodPost, path: "/api/hub/sync"}.send(t, base)
	if resp.StatusCode == stdhttp.StatusForbidden {
		t.Fatalf("a non-browser client without Origin is not blocked, got %d", resp.StatusCode)
	}
}

func TestServerChainWithCredentialsIsUnchanged(t *testing.T) {
	base, _ := startChain(t, "admin")
	own := base

	resp := browserRequest{method: stdhttp.MethodPost, path: "/api/hub/sync", origin: own, fetchSite: "same-origin"}.send(t, base)
	if resp.StatusCode != stdhttp.StatusUnauthorized {
		t.Fatalf("without a token the API asks to sign in, got %d", resp.StatusCode)
	}
	loginReq, _ := stdhttp.NewRequest(stdhttp.MethodPost, base+"/api/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"admin"}`)))
	loginReq.Header.Set("Content-Type", "application/json")
	loginReq.Header.Set("Origin", own)
	loginReq.Header.Set("Sec-Fetch-Site", "same-origin")
	loginResp, err := stdhttp.DefaultClient.Do(loginReq)
	if err != nil {
		t.Fatal(err)
	}
	var login struct {
		Token string `json:"token"`
	}
	if err := json.NewDecoder(loginResp.Body).Decode(&login); err != nil || loginResp.StatusCode != stdhttp.StatusOK || login.Token == "" {
		t.Fatalf("login: %d %v %+v", loginResp.StatusCode, err, login)
	}
	loginResp.Body.Close()

	resp = browserRequest{method: stdhttp.MethodPost, path: "/api/hub/sync", origin: "http://tool.example", fetchSite: "cross-site", token: login.Token}.send(t, base)
	if resp.StatusCode == stdhttp.StatusForbidden || resp.StatusCode == stdhttp.StatusUnauthorized {
		t.Fatalf("a cross-origin client with a valid token keeps working, got %d", resp.StatusCode)
	}
	if resp.Header.Get("Access-Control-Allow-Origin") != "http://tool.example" {
		t.Fatalf("a cross-origin client with a token keeps its grant, got %q", resp.Header.Get("Access-Control-Allow-Origin"))
	}
	resp = browserRequest{method: stdhttp.MethodPost, path: "/api/hub/sync", origin: "http://evil.example", fetchSite: "cross-site"}.send(t, base)
	if resp.StatusCode != stdhttp.StatusUnauthorized {
		t.Fatalf("a foreign page without a token is stopped by the token check, got %d", resp.StatusCode)
	}
}
