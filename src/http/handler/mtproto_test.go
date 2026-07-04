package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/mtproto"
)

type stubMTProtoServer struct {
	sessions []mtproto.SessionInfo
}

func (s *stubMTProtoServer) UpdateConfig(*config.Config) {}

func (s *stubMTProtoServer) Sessions() []mtproto.SessionInfo { return s.sessions }

func newMTProtoTestMux(t *testing.T, srv ConfigRefresher) *http.ServeMux {
	t.Helper()
	prev := globalMTProtoServer
	globalMTProtoServer = srv
	t.Cleanup(func() { globalMTProtoServer = prev })

	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterMTProtoApi()
	return mux
}

func TestHandleMTProtoSessions(t *testing.T) {
	t0 := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	stub := &stubMTProtoServer{sessions: []mtproto.SessionInfo{
		{
			ID: "a", Name: "Max", ClientIP: "85.233.150.240", ClientPort: 42378,
			Destination: "149.154.167.220:443", ConnectedAt: t0, LastSeen: t0.Add(15 * time.Second),
		},
	}}
	mux := newMTProtoTestMux(t, stub)

	req := httptest.NewRequest(http.MethodGet, "/api/mtproto/sessions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	var out []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 1 {
		t.Fatalf("expected 1 session, got %d", len(out))
	}
	got := out[0]
	want := map[string]any{
		"id": "a", "name": "Max", "client_ip": "85.233.150.240",
		"client_port": float64(42378), "destination": "149.154.167.220:443",
		"connected_at": "2026-07-02T10:00:00Z", "last_seen": "2026-07-02T10:00:15Z",
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("field %q = %v, want %v", k, got[k], v)
		}
	}
}

func TestHandleMTProtoSessionsMethodNotAllowed(t *testing.T) {
	mux := newMTProtoTestMux(t, &stubMTProtoServer{})
	req := httptest.NewRequest(http.MethodPost, "/api/mtproto/sessions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestHandleMTProtoSessionsEmptyWhenNoServer(t *testing.T) {
	prev := globalMTProtoServer
	globalMTProtoServer = nil
	t.Cleanup(func() { globalMTProtoServer = prev })

	cfg := config.NewConfig()
	api := &API{cfgPtr: testCfgPtr(&cfg)}
	mux := http.NewServeMux()
	api.mux = mux
	api.RegisterMTProtoApi()

	req := httptest.NewRequest(http.MethodGet, "/api/mtproto/sessions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	var out []map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 0 {
		t.Fatalf("expected empty array, got %d entries", len(out))
	}
}

func TestHandleMTProtoActiveClients(t *testing.T) {
	t0 := time.Date(2026, 7, 2, 10, 0, 0, 0, time.UTC)
	stub := &stubMTProtoServer{sessions: []mtproto.SessionInfo{
		{ID: "a", Name: "Max", ClientIP: "85.233.150.240", ClientPort: 42378, ConnectedAt: t0, LastSeen: t0.Add(15 * time.Second)},
		{ID: "a", Name: "Max", ClientIP: "85.233.150.240", ClientPort: 42379, ConnectedAt: t0, LastSeen: t0.Add(18 * time.Second)},
		{ID: "a", Name: "Max", ClientIP: "178.130.140.98", ClientPort: 55120, ConnectedAt: t0, LastSeen: t0.Add(17 * time.Second)},
		{ID: "b", Name: "Ivan", ClientIP: "10.0.0.5", ClientPort: 5000, ConnectedAt: t0, LastSeen: t0.Add(1 * time.Second)},
	}}
	mux := newMTProtoTestMux(t, stub)

	req := httptest.NewRequest(http.MethodGet, "/api/mtproto/active-clients", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}

	type clientOut struct {
		ID                string   `json:"id"`
		Name              string   `json:"name"`
		ActiveConnections int      `json:"active_connections"`
		ActiveIPs         []string `json:"active_ips"`
		ActiveIPCount     int      `json:"active_ip_count"`
		LastSeen          string   `json:"last_seen"`
	}
	var out []clientOut
	if err := json.NewDecoder(rec.Body).Decode(&out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("expected 2 clients, got %d", len(out))
	}

	byID := make(map[string]clientOut, len(out))
	for _, c := range out {
		byID[c.ID] = c
	}

	a, ok := byID["a"]
	if !ok {
		t.Fatalf("client a missing")
	}
	if a.ActiveConnections != 3 {
		t.Errorf("active_connections = %d, want 3", a.ActiveConnections)
	}
	if a.ActiveIPCount != 2 {
		t.Errorf("active_ip_count = %d, want 2", a.ActiveIPCount)
	}
	wantIPs := []string{"178.130.140.98", "85.233.150.240"}
	if len(a.ActiveIPs) != len(wantIPs) {
		t.Fatalf("active_ips = %v, want %v", a.ActiveIPs, wantIPs)
	}
	for i, ip := range wantIPs {
		if a.ActiveIPs[i] != ip {
			t.Errorf("active_ips[%d] = %q, want %q (expected sorted)", i, a.ActiveIPs[i], ip)
		}
	}
	if a.LastSeen != "2026-07-02T10:00:18Z" {
		t.Errorf("last_seen = %q, want the most recent activity 2026-07-02T10:00:18Z", a.LastSeen)
	}

	if b := byID["b"]; b.ActiveConnections != 1 || b.ActiveIPCount != 1 {
		t.Errorf("client b unexpected: conns=%d ips=%d", b.ActiveConnections, b.ActiveIPCount)
	}
}
