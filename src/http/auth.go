package http

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
)

const (
	tokenTTL      = 24 * time.Hour
	loginMaxFails = 5
	loginWindow   = 15 * time.Minute
	loginLockout  = 5 * time.Minute
)

var (
	activeTokens   = make(map[string]time.Time)
	activeTokensMu sync.Mutex
)

func issueToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	now := time.Now()
	activeTokensMu.Lock()
	for t, exp := range activeTokens {
		if now.After(exp) {
			delete(activeTokens, t)
		}
	}
	activeTokens[tok] = now.Add(tokenTTL)
	activeTokensMu.Unlock()
	return tok, nil
}

func validateToken(tok string) bool {
	if tok == "" {
		return false
	}
	now := time.Now()
	activeTokensMu.Lock()
	defer activeTokensMu.Unlock()
	exp, ok := activeTokens[tok]
	if !ok {
		return false
	}
	if now.After(exp) {
		delete(activeTokens, tok)
		return false
	}
	activeTokens[tok] = now.Add(tokenTTL)
	return true
}

func revokeToken(tok string) {
	if tok == "" {
		return
	}
	activeTokensMu.Lock()
	delete(activeTokens, tok)
	activeTokensMu.Unlock()
}

type loginAttempt struct {
	fails       int
	windowStart time.Time
	lockedUntil time.Time
}

var (
	loginAttempts   = make(map[string]*loginAttempt)
	loginAttemptsMu sync.Mutex
	loginLastSweep  time.Time
)

func sweepLoginAttemptsLocked(now time.Time) {
	if now.Sub(loginLastSweep) < loginWindow {
		return
	}
	loginLastSweep = now
	for ip, a := range loginAttempts {
		if now.After(a.lockedUntil) && now.Sub(a.windowStart) > loginWindow {
			delete(loginAttempts, ip)
		}
	}
}

func loginAllowed(ip string) (bool, time.Duration) {
	now := time.Now()
	loginAttemptsMu.Lock()
	defer loginAttemptsMu.Unlock()
	a := loginAttempts[ip]
	if a == nil {
		return true, 0
	}
	if now.Before(a.lockedUntil) {
		return false, a.lockedUntil.Sub(now)
	}
	return true, 0
}

func recordLoginFailure(ip string) {
	now := time.Now()
	loginAttemptsMu.Lock()
	defer loginAttemptsMu.Unlock()
	sweepLoginAttemptsLocked(now)
	a := loginAttempts[ip]
	if a == nil || now.Sub(a.windowStart) > loginWindow {
		a = &loginAttempt{windowStart: now}
		loginAttempts[ip] = a
	}
	a.fails++
	if a.fails >= loginMaxFails {
		a.lockedUntil = now.Add(loginLockout)
		a.fails = 0
		a.windowStart = now
	}
}

func recordLoginSuccess(ip string) {
	loginAttemptsMu.Lock()
	delete(loginAttempts, ip)
	loginAttemptsMu.Unlock()
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func registerAuthEndpoints(mux *http.ServeMux, cfgPtr *atomic.Pointer[config.Config]) {
	mux.HandleFunc("/api/auth/login", handleLogin(cfgPtr))
	mux.HandleFunc("/api/auth/check", handleAuthCheck(cfgPtr))
	mux.HandleFunc("/api/auth/logout", handleLogout)
}

// @Summary Login with credentials
// @Tags Auth
// @Accept json
// @Produce json
// @Param body body object true "Login credentials (username, password)"
// @Success 200 {object} object
// @Failure 401 {object} object
// @Router /auth/login [post]
func handleLogin(cfgPtr *atomic.Pointer[config.Config]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req struct {
			Username string `json:"username"`
			Password string `json:"password"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeAuthJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request"})
			return
		}

		cfg := cfgPtr.Load()
		if !authEnabled(cfg) {
			writeAuthJSON(w, http.StatusOK, map[string]interface{}{"auth_required": false})
			return
		}

		ip := clientIP(r)
		if ok, retry := loginAllowed(ip); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(int(retry.Seconds())+1))
			writeAuthJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many attempts, try again later"})
			return
		}

		expectedUser := cfg.System.WebServer.Username

		userOK := subtle.ConstantTimeCompare([]byte(req.Username), []byte(expectedUser)) == 1
		passOK := config.CheckPassword(cfg.System.WebServer.Password, req.Password)
		if !userOK || !passOK {
			recordLoginFailure(ip)
			writeAuthJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}

		recordLoginSuccess(ip)

		token, err := issueToken()
		if err != nil {
			writeAuthJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
			return
		}

		writeAuthJSON(w, http.StatusOK, map[string]string{"token": token})
	}
}

// @Summary Check authentication status
// @Tags Auth
// @Produce json
// @Success 200 {object} object
// @Security BearerAuth
// @Router /auth/check [get]
func handleAuthCheck(cfgPtr *atomic.Pointer[config.Config]) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !authEnabled(cfgPtr.Load()) {
			writeAuthJSON(w, http.StatusOK, map[string]bool{"auth_required": false})
			return
		}

		token := extractBearerToken(r)
		if token == "" {
			writeAuthJSON(w, http.StatusOK, map[string]interface{}{"auth_required": true, "authenticated": false})
			return
		}

		valid := validateToken(token)

		writeAuthJSON(w, http.StatusOK, map[string]interface{}{"auth_required": true, "authenticated": valid})
	}
}

// @Summary Logout and invalidate token
// @Tags Auth
// @Produce json
// @Success 200 {object} object
// @Security BearerAuth
// @Router /auth/logout [post]
func handleLogout(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	revokeToken(extractBearerToken(r))

	writeAuthJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func authMiddleware(cfgPtr *atomic.Pointer[config.Config], next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !authEnabled(cfgPtr.Load()) {
			next.ServeHTTP(w, r)
			return
		}

		// Allow auth endpoints without token
		if strings.HasPrefix(r.URL.Path, "/api/auth/") {
			next.ServeHTTP(w, r)
			return
		}

		// Allow version endpoint without token (used as health check after updates)
		if r.URL.Path == "/api/version" {
			next.ServeHTTP(w, r)
			return
		}

		// Allow non-API requests (SPA static files) without token —
		// the SPA itself will check auth and show the login page
		if !strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}

		token := extractBearerToken(r)

		// MCP clients authenticate with the long-lived MCP token instead of a
		// web session token; the MCP gate itself verifies it.
		if r.URL.Path == handler.MCPEndpoint && handler.MCPTokenAccepts(cfgPtr.Load(), token) {
			next.ServeHTTP(w, r)
			return
		}
		if token == "" {
			writeAuthJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}

		if !validateToken(token) {
			writeAuthJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
			return
		}

		next.ServeHTTP(w, r)
	})
}

func authEnabled(cfg *config.Config) bool {
	return cfg.System.WebServer.Username != "" && cfg.System.WebServer.Password != ""
}

func isWebSocketRequest(r *http.Request) bool {
	return strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") &&
		strings.EqualFold(r.Header.Get("Upgrade"), "websocket")
}

func extractBearerToken(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if strings.HasPrefix(auth, "Bearer ") {
		return strings.TrimPrefix(auth, "Bearer ")
	}
	// Fallback to query parameter only for WebSocket connections
	if isWebSocketRequest(r) || strings.HasPrefix(r.URL.Path, "/api/ws/") {
		if t := r.URL.Query().Get("token"); t != "" {
			return t
		}
	}
	return ""
}

func writeAuthJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Pragma", "no-cache")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
