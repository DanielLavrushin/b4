package mtproto

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/gorilla/websocket"
)

const (
	webCarrierPath    = "/api/v1/session"
	webMaxCarriers    = 256
	webHandshakeGrace = 30 * time.Second
)

var webCarriers atomic.Int64

var webUpgrader = websocket.Upgrader{
	HandshakeTimeout:  10 * time.Second,
	ReadBufferSize:    32 << 10,
	WriteBufferSize:   32 << 10,
	EnableCompression: false,
	CheckOrigin:       func(*http.Request) bool { return true },
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
	case r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead):
		s.serveWebRoot(w, r, host)
	default:
		webWriteSite(w, r, http.StatusNotFound)
	}
	return true
}

func (s *Server) serveWebRoot(w http.ResponseWriter, r *http.Request, host string) {
	capability := r.URL.Query().Get("bridge")
	if capability == "" || s.webSecretFor(host, capability) == nil {
		webWriteSite(w, r, http.StatusOK)
		return
	}
	webPageHeaders(w)
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(webBridgePage())
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
	if origin := r.Header.Get("Origin"); origin != "https://"+host {
		webWriteSite(w, r, http.StatusNotFound)
		return
	}
	secret := s.webSecretFor(host, r.URL.Query().Get("b"))
	if secret == nil {
		webWriteSite(w, r, http.StatusNotFound)
		return
	}
	if webCarriers.Load() >= webMaxCarriers {
		webWriteSite(w, r, http.StatusNotFound)
		return
	}

	conn, err := webUpgrader.Upgrade(w, r, nil)
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
