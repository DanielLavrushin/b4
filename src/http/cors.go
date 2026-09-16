package http

import (
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
)

func cors(cfgPtr *atomic.Pointer[config.Config], next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		foreign := origin != "" && cfgPtr != nil && !trustedCaller(cfgPtr.Load(), r, origin)
		if origin != "" && !foreign {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		if foreign && r.Method != http.MethodGet && r.Method != http.MethodHead && strings.HasPrefix(r.URL.Path, "/api/") {
			writeAuthJSON(w, http.StatusForbidden, map[string]string{"error": "cross-site requests are refused while the web server has no credentials"})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func trustedCaller(cfg *config.Config, r *http.Request, origin string) bool {
	if authEnabled(cfg) || sameSiteOrigin(r, origin) {
		return true
	}
	if r.URL.Path == handler.MCPEndpoint {
		return r.Method == http.MethodOptions || handler.MCPTokenAccepts(cfg, extractBearerToken(r))
	}
	return false
}

func sameSiteOrigin(r *http.Request, origin string) bool {
	switch r.Header.Get("Sec-Fetch-Site") {
	case "same-origin", "none":
		return true
	case "same-site", "cross-site":
		return false
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || u.Scheme == "" {
		return false
	}
	return canonicalOrigin(u.Scheme, u.Host) == canonicalOrigin(requestScheme(r), r.Host)
}

func requestScheme(r *http.Request) string {
	if proto := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])); proto == "http" || proto == "https" {
		return proto
	}
	if r.TLS != nil {
		return "https"
	}
	return "http"
}

func canonicalOrigin(scheme, hostport string) string {
	scheme = strings.ToLower(scheme)
	host, port, err := net.SplitHostPort(hostport)
	if err != nil {
		host, port = strings.Trim(hostport, "[]"), ""
	}
	if port == "" {
		port = "80"
		if scheme == "https" {
			port = "443"
		}
	}
	return scheme + "://" + strings.ToLower(host) + ":" + port
}
