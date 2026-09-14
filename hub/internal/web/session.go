package web

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/binary"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4hub/internal/asn"
)

const (
	adminUser     = "admin"
	sessionCookie = "b4hub_session"
	sessionTTL    = 7 * 24 * time.Hour
	sessionNonce  = 16

	loginScope    = "login"
	loginAttempts = 10
	loginWindow   = 15 * time.Minute
)

type sessionRequest struct {
	Password string `json:"password"`
}

type sessionState struct {
	Configured    bool   `json:"configured"`
	Authenticated bool   `json:"authenticated"`
	Version       string `json:"version"`
}

func (s *Server) sessionKey() []byte {
	mac := hmac.New(sha256.New, s.Secret)
	mac.Write([]byte("b4hub-session|"))
	mac.Write([]byte(s.AdminPassword))
	return mac.Sum(nil)
}

func (s *Server) sign(payload []byte) []byte {
	mac := hmac.New(sha256.New, s.sessionKey())
	mac.Write(payload)
	return mac.Sum(nil)
}

func (s *Server) issueToken() (string, time.Time, error) {
	expires := s.now().Add(sessionTTL)
	payload := make([]byte, 8+sessionNonce)
	binary.BigEndian.PutUint64(payload, uint64(expires.Unix()))
	if _, err := rand.Read(payload[8:]); err != nil {
		return "", time.Time{}, err
	}
	token := base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(s.sign(payload))
	return token, expires, nil
}

func (s *Server) validToken(token string) bool {
	rawPayload, rawSig, ok := strings.Cut(token, ".")
	if !ok {
		return false
	}
	payload, err := base64.RawURLEncoding.DecodeString(rawPayload)
	if err != nil || len(payload) != 8+sessionNonce {
		return false
	}
	sig, err := base64.RawURLEncoding.DecodeString(rawSig)
	if err != nil {
		return false
	}
	if !hmac.Equal(sig, s.sign(payload)) {
		return false
	}
	expires := time.Unix(int64(binary.BigEndian.Uint64(payload)), 0)
	return s.now().Before(expires)
}

func (s *Server) passwordMatches(password string) bool {
	return s.AdminPassword != "" && subtle.ConstantTimeCompare([]byte(password), []byte(s.AdminPassword)) == 1
}

func (s *Server) authenticated(r *http.Request) bool {
	if s.AdminPassword == "" {
		return false
	}
	if c, err := r.Cookie(sessionCookie); err == nil && s.validToken(c.Value) {
		return true
	}
	if user, pass, ok := r.BasicAuth(); ok {
		return subtle.ConstantTimeCompare([]byte(user), []byte(adminUser)) == 1 && s.passwordMatches(pass)
	}
	return false
}

func secureRequest(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func (s *Server) setSessionCookie(w http.ResponseWriter, r *http.Request, token string, expires time.Time) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     PathAdmin,
		Expires:  expires,
		MaxAge:   int(time.Until(expires) / time.Second),
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func (s *Server) clearSessionCookie(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     PathAdmin,
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secureRequest(r),
		SameSite: http.SameSiteStrictMode,
	})
}

func sameSiteRequest(r *http.Request) bool {
	if site := r.Header.Get("Sec-Fetch-Site"); site != "" {
		return site == "same-origin" || site == "none"
	}
	for _, header := range []string{"Origin", "Referer"} {
		raw := strings.TrimSpace(r.Header.Get(header))
		if raw == "" {
			continue
		}
		u, err := url.Parse(raw)
		if err != nil || u.Host == "" {
			return false
		}
		return strings.EqualFold(u.Host, r.Host)
	}
	return false
}

func (s *Server) session(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, sessionState{Configured: s.AdminPassword != "", Authenticated: s.authenticated(r), Version: s.Version})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.AdminPassword == "" {
		writeError(w, http.StatusServiceUnavailable, codeUnconfigured, "moderation is not configured: set B4HUB_ADMIN_PASSWORD in the service environment")
		return
	}
	if !sameSiteRequest(r) {
		writeError(w, http.StatusForbidden, codeForbidden, "sign-in is accepted only from the console itself")
		return
	}
	ip := asn.ClientIP(r)
	client := ""
	if ip != nil {
		client = ip.String()
	}
	if ok, retry := s.logins.Allow(loginScope, client, loginAttempts, loginWindow); !ok {
		w.Header().Set("Retry-After", retryAfter(retry))
		writeError(w, http.StatusTooManyRequests, codeTooMany, "too many sign-in attempts, try again later")
		return
	}
	var req sessionRequest
	if err := readBody(w, r, &req); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return
	}
	if !s.passwordMatches(req.Password) {
		writeError(w, http.StatusUnauthorized, codeUnauthorized, "wrong moderation password")
		return
	}
	token, expires, err := s.issueToken()
	if err != nil {
		s.fail(w, err)
		return
	}
	s.setSessionCookie(w, r, token, expires)
	writeJSON(w, http.StatusOK, sessionState{Configured: true, Authenticated: true, Version: s.Version})
}

func retryAfter(d time.Duration) string {
	seconds := int(d / time.Second)
	if seconds < 1 {
		seconds = 1
	}
	return strconv.Itoa(seconds)
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	s.clearSessionCookie(w, r)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) guard(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.AdminPassword == "" {
			writeError(w, http.StatusServiceUnavailable, codeUnconfigured, "moderation is not configured: set B4HUB_ADMIN_PASSWORD in the service environment")
			return
		}
		if !s.authenticated(r) {
			writeError(w, http.StatusUnauthorized, codeUnauthorized, "moderator sign-in required")
			return
		}
		if r.Method != http.MethodGet && !sameSiteRequest(r) {
			writeError(w, http.StatusForbidden, codeForbidden, "moderation actions are accepted only from the console itself")
			return
		}
		next(w, r)
	})
}
