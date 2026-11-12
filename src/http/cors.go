package http

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
	"github.com/daniellavrushin/b4/log"
	"github.com/gorilla/websocket"
)

const crossSiteRefusalWindow = 10 * time.Minute

var crossSiteRefusals struct {
	sync.Mutex
	last       time.Time
	suppressed int
}

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
		if foreign && strings.HasPrefix(r.URL.Path, "/api/") && (websocket.IsWebSocketUpgrade(r) || (r.Method != http.MethodGet && r.Method != http.MethodHead)) {
			logCrossSiteRefusal(r, origin)
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
	if canonicalOrigin(u.Scheme, u.Host) == canonicalOrigin(requestScheme(r), r.Host) {
		return true
	}
	return hostPortDroppedByProxy(r) && strings.EqualFold(u.Hostname(), strings.Trim(r.Host, "[]"))
}

func hostPortDroppedByProxy(r *http.Request) bool {
	if _, _, err := net.SplitHostPort(r.Host); err == nil {
		return false
	}
	local, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr)
	if !ok || local == nil {
		return false
	}
	_, port, err := net.SplitHostPort(local.String())
	return err == nil && port != "80" && port != "443"
}

func logCrossSiteRefusal(r *http.Request, origin string) {
	crossSiteRefusals.Lock()
	now := time.Now()
	if !crossSiteRefusals.last.IsZero() && now.Sub(crossSiteRefusals.last) < crossSiteRefusalWindow {
		crossSiteRefusals.suppressed++
		crossSiteRefusals.Unlock()
		return
	}
	suppressed := crossSiteRefusals.suppressed
	crossSiteRefusals.last = now
	crossSiteRefusals.suppressed = 0
	crossSiteRefusals.Unlock()

	kind := r.Method
	if websocket.IsWebSocketUpgrade(r) {
		kind = "WebSocket"
	}
	more := ""
	if suppressed > 0 {
		more = fmt.Sprintf(" (%d more refused since the previous line)", suppressed)
	}
	log.Warnf("Web server: refused a %s to %q from the page at %q, because without a web login b4 accepts it only from its own address, and this request names that address as %q%s. Set a username and password under Settings > System > Web Server, or make the reverse proxy in front of b4 pass the original Host header with its port, and X-Forwarded-Proto",
		kind, r.URL.Path, origin, canonicalOrigin(requestScheme(r), r.Host), more)
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
