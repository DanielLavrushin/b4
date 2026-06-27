package handler

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/daniellavrushin/b4/config"
)

func TestPprofGuardAccess(t *testing.T) {
	call := func(pprofOn bool, user, pass, remote string) int {
		var ptr atomic.Pointer[config.Config]
		cfg := &config.Config{}
		cfg.System.Pprof = pprofOn
		cfg.System.WebServer.Username = user
		cfg.System.WebServer.Password = pass
		ptr.Store(cfg)

		api := &API{cfgPtr: &ptr}
		h := api.pprofGuard(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		})

		req := httptest.NewRequest(http.MethodGet, "/api/debug/pprof/heap", nil)
		req.RemoteAddr = remote
		rec := httptest.NewRecorder()
		h(rec, req)
		return rec.Code
	}

	cases := []struct {
		name       string
		pprof      bool
		user, pass string
		remote     string
		want       int
	}{
		{"disabled blocks even from loopback", false, "", "", "127.0.0.1:5", http.StatusNotFound},
		{"auth configured allows remote", true, "u", "p", "1.2.3.4:5", http.StatusOK},
		{"no auth allows loopback v4", true, "", "", "127.0.0.1:5", http.StatusOK},
		{"no auth allows loopback v6", true, "", "", "[::1]:5", http.StatusOK},
		{"no auth blocks remote", true, "", "", "1.2.3.4:5", http.StatusNotFound},
	}
	for _, c := range cases {
		if got := call(c.pprof, c.user, c.pass, c.remote); got != c.want {
			t.Errorf("%s: got %d want %d", c.name, got, c.want)
		}
	}
}
