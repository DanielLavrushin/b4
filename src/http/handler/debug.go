package handler

import (
	"net"
	"net/http"
	"net/http/pprof"
	"os"

	"github.com/daniellavrushin/b4/config"
)

func pprofEnabled(cfg *config.Config) bool {
	if os.Getenv("B4_PPROF") != "" {
		return true
	}
	return cfg.System.Pprof
}

func isLoopbackRemote(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (api *API) RegisterDebugApi() {
	api.mux.HandleFunc("/api/debug/pprof/", api.pprofGuard(pprof.Index))
	api.mux.HandleFunc("/api/debug/pprof/cmdline", api.pprofGuard(pprof.Cmdline))
	api.mux.HandleFunc("/api/debug/pprof/profile", api.pprofGuard(pprof.Profile))
	api.mux.HandleFunc("/api/debug/pprof/symbol", api.pprofGuard(pprof.Symbol))
	api.mux.HandleFunc("/api/debug/pprof/trace", api.pprofGuard(pprof.Trace))

	for _, name := range []string{"goroutine", "heap", "allocs", "threadcreate", "block", "mutex"} {
		api.mux.HandleFunc("/api/debug/pprof/"+name, api.pprofGuard(pprof.Handler(name).ServeHTTP))
	}
}

func (api *API) pprofGuard(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := api.getCfg()
		if !pprofEnabled(cfg) {
			http.NotFound(w, r)
			return
		}
		authConfigured := cfg.System.WebServer.Username != "" && cfg.System.WebServer.Password != ""
		if !authConfigured && !isLoopbackRemote(r.RemoteAddr) {
			http.NotFound(w, r)
			return
		}
		h(w, r)
	}
}
