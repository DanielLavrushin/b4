package mtproto

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/gorilla/websocket"
)

const (
	webCarrierPath    = "/api/v1/ws"
	webSessionPath    = "/api/v1/session"
	webMaxCarriers    = 256
	webHandshakeGrace = 30 * time.Second
	webTicketTTL      = 2 * time.Minute
	webMaxTickets     = 1024
	webSubprotoPrefix = "tproxy-v1."
)

var webCarriers atomic.Int64

var webUpgrader = websocket.Upgrader{
	HandshakeTimeout:  10 * time.Second,
	ReadBufferSize:    32 << 10,
	WriteBufferSize:   32 << 10,
	EnableCompression: false,
	CheckOrigin:       func(*http.Request) bool { return true },
}

type webTicket struct {
	secret  *Secret
	expires time.Time
}

type webTicketStore struct {
	mu      sync.Mutex
	entries map[string]webTicket
}

func (t *webTicketStore) issue(secret *Secret) (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	token := base64.RawURLEncoding.EncodeToString(raw)

	t.mu.Lock()
	defer t.mu.Unlock()
	if t.entries == nil {
		t.entries = make(map[string]webTicket)
	}
	now := time.Now()
	for k, v := range t.entries {
		if now.After(v.expires) {
			delete(t.entries, k)
		}
	}
	if len(t.entries) >= webMaxTickets {
		return "", errors.New("web proxy ticket store full")
	}
	t.entries[token] = webTicket{secret: secret, expires: now.Add(webTicketTTL)}
	return token, nil
}

func (t *webTicketStore) redeem(token string) *Secret {
	if token == "" {
		return nil
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	entry, ok := t.entries[token]
	if !ok {
		return nil
	}
	delete(t.entries, token)
	if time.Now().After(entry.expires) {
		return nil
	}
	return entry.secret
}

func (s *Server) WebProxyHost() string {
	cfg := s.cfg.Load()
	if cfg == nil || !cfg.System.MTProto.Enabled {
		return ""
	}
	w := cfg.System.MTProto.WebProxy
	if !w.Enabled {
		return ""
	}
	return CanonicalWebHost(w.Hostname)
}

func (s *Server) WebProxyLinks() []WebProxyLinkInfo {
	host := s.WebProxyHost()
	if host == "" {
		return nil
	}
	ptr := s.secrets.Load()
	if ptr == nil {
		return nil
	}
	out := make([]WebProxyLinkInfo, 0, len(*ptr))
	for _, sec := range *ptr {
		_, padded := WebSecretForms(sec.Key)
		out = append(out, WebProxyLinkInfo{
			ID:     sec.ID,
			Name:   sec.Label(),
			Host:   host,
			Secret: padded,
			Link:   WebProxyLink(host, sec.Key),
		})
	}
	return out
}

type WebProxyLinkInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Host   string `json:"host"`
	Secret string `json:"secret"`
	Link   string `json:"link"`
}

func webRequestHost(r *http.Request) string {
	h := r.Host
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			v = v[:i]
		}
		h = strings.TrimSpace(v)
	}
	if hostOnly, _, err := net.SplitHostPort(h); err == nil {
		h = hostOnly
	}
	return strings.TrimSuffix(strings.ToLower(h), ".")
}

func (s *Server) ServeWebProxy(w http.ResponseWriter, r *http.Request) bool {
	host := s.WebProxyHost()
	if host == "" || webRequestHost(r) != host {
		return false
	}

	switch {
	case r.URL.Path == webCarrierPath:
		s.serveWebCarrier(w, r, host)
	case r.URL.Path == webSessionPath && r.Method == http.MethodDelete:
		webWriteSite(w, r, http.StatusNotFound)
	case r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		s.serveWebRoot(w, r, host)
	default:
		webWriteSite(w, r, http.StatusNotFound)
	}
	return true
}

func (s *Server) serveWebRoot(w http.ResponseWriter, r *http.Request, host string) {
	capability := r.URL.Query().Get("bridge")
	secret := s.webSecretFor(host, capability)
	if capability == "" || secret == nil {
		webWriteSite(w, r, http.StatusOK)
		return
	}
	token, err := s.webTickets.issue(secret)
	if err != nil {
		webWriteSite(w, r, http.StatusOK)
		return
	}
	webPageHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(webBridgePage(token))
}

func (s *Server) webSecretFor(host, capability string) *Secret {
	if capability == "" {
		return nil
	}
	ptr := s.secrets.Load()
	if ptr == nil {
		return nil
	}
	want := []byte(capability)
	var found *Secret
	for _, sec := range *ptr {
		for _, raw := range webSecretBytes(sec.Key) {
			got := []byte(WebBridgeCapability(host, raw))
			if subtle.ConstantTimeCompare(got, want) == 1 && found == nil {
				found = sec
			}
		}
	}
	return found
}

func (s *Server) serveWebCarrier(w http.ResponseWriter, r *http.Request, host string) {
	if r.Method != http.MethodGet {
		webWriteSite(w, r, http.StatusNotFound)
		return
	}
	if !websocket.IsWebSocketUpgrade(r) {
		webWriteSite(w, r, http.StatusNotFound)
		return
	}
	subproto := webRequestedSubprotocol(r)
	secret := s.webTickets.redeem(strings.TrimPrefix(subproto, webSubprotoPrefix))
	if subproto == "" || secret == nil {
		webWriteSite(w, r, http.StatusNotFound)
		return
	}
	if webCarriers.Load() >= webMaxCarriers {
		webWriteSite(w, r, http.StatusNotFound)
		return
	}

	conn, err := webUpgrader.Upgrade(w, r, http.Header{
		"Sec-Websocket-Protocol": []string{subproto},
	})
	if err != nil {
		return
	}
	webCarriers.Add(1)

	id := nextConnID()
	tag := tg(id)
	remote := webAddr{network: "tcp", value: webClientAddr(r)}
	log.Infof("%s web carrier up from %s (secret=%s)", tag, remote, secret.Label())

	go func() {
		defer webCarriers.Add(-1)
		sess := newWebSession(s, secret, conn, host, remote, tag)
		sess.run()
		log.Infof("%s web carrier down from %s: %v", tag, remote, sess.closeErr)
	}()
}

func webRequestedSubprotocol(r *http.Request) string {
	for _, header := range r.Header.Values("Sec-Websocket-Protocol") {
		for _, candidate := range strings.Split(header, ",") {
			candidate = strings.TrimSpace(candidate)
			if strings.HasPrefix(candidate, webSubprotoPrefix) && len(candidate) > len(webSubprotoPrefix) {
				return candidate
			}
		}
	}
	return ""
}

func webClientAddr(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			v = v[:i]
		}
		if v = strings.TrimSpace(v); v != "" {
			return net.JoinHostPort(v, "0")
		}
	}
	return r.RemoteAddr
}

func (s *Server) serveWebStream(st *webStream, secret *Secret, host string) {
	clientAddr := st.RemoteAddr().String()
	id := nextConnID()
	tag := tg(id)

	defer func() {
		if r := recover(); r != nil {
			log.Errorf("%s web proxy panic from %s: %v", tag, clientAddr, r)
		}
	}()

	log.Infof("%s web proxy new stream %d from %s", tag, st.id, clientAddr)
	if err := st.SetDeadline(time.Now().Add(webHandshakeGrace)); err != nil {
		return
	}
	s.serveClient(st, st, secret, clientAddr, id, tag, "WEB", host)
}
