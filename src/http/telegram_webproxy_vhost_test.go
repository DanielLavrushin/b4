package http

import (
	stdhttp "net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
)

type stubRelay struct {
	host  string
	taken []string
}

func (s *stubRelay) WebProxyHost() string { return s.host }

func (s *stubRelay) UpdateConfig(*config.Config) {}

func (s *stubRelay) ServeWebProxy(w stdhttp.ResponseWriter, r *stdhttp.Request) bool {
	if s.host == "" || r.Host != s.host {
		return false
	}
	s.taken = append(s.taken, r.URL.Path)
	w.WriteHeader(stdhttp.StatusOK)
	_, _ = w.Write([]byte("relay"))
	return true
}

var _ handler.MTProtoWebProxy = (*stubRelay)(nil)

func chainWithRelay(t *testing.T, relay *stubRelay) stdhttp.Handler {
	t.Helper()

	prev := handler.MTProtoWebProxyServer()
	handler.SetMTProtoServer(relay)
	t.Cleanup(func() {
		if prev == nil {
			handler.SetMTProtoServer(nil)
			return
		}
		if r, ok := prev.(handler.ConfigRefresher); ok {
			handler.SetMTProtoServer(r)
		}
	})

	cfg := &config.Config{}
	cfg.System.WebServer.Username = "admin"
	cfg.System.WebServer.Password = "secret"
	var ptr atomic.Pointer[config.Config]
	ptr.Store(cfg)

	mux := stdhttp.NewServeMux()
	mux.HandleFunc("/api/config", func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.WriteHeader(stdhttp.StatusOK)
		_, _ = w.Write([]byte("ui"))
	})

	var h stdhttp.Handler = mux
	h = authMiddleware(&ptr, h)
	h = cors(h)
	return telegramWebProxyVhost(h)
}

func TestTelegramWebProxyVhostSkipsAuth(t *testing.T) {
	relay := &stubRelay{host: "relay.example.org"}
	h := chainWithRelay(t, relay)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(stdhttp.MethodGet, "http://relay.example.org/", nil)
	h.ServeHTTP(w, r)

	if w.Code != stdhttp.StatusOK || w.Body.String() != "relay" {
		t.Fatalf("relay request got %d %q, want the relay response without credentials", w.Code, w.Body.String())
	}
	if len(relay.taken) != 1 {
		t.Fatalf("relay saw %d requests, want 1", len(relay.taken))
	}
}

func TestTelegramWebProxyVhostLeavesOtherHosts(t *testing.T) {
	relay := &stubRelay{host: "relay.example.org"}
	h := chainWithRelay(t, relay)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(stdhttp.MethodGet, "http://b4.example.net/api/config", nil)
	h.ServeHTTP(w, r)

	if w.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("unauthenticated UI request got %d, want 401", w.Code)
	}
	if len(relay.taken) != 0 {
		t.Fatalf("relay took a request for another hostname: %v", relay.taken)
	}
}

func TestTelegramWebProxyVhostInactiveWhenUnconfigured(t *testing.T) {
	relay := &stubRelay{}
	h := chainWithRelay(t, relay)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(stdhttp.MethodGet, "http://relay.example.org/api/config", nil)
	h.ServeHTTP(w, r)

	if w.Code != stdhttp.StatusUnauthorized {
		t.Fatalf("got %d, want the ordinary chain to answer with 401", w.Code)
	}
}
