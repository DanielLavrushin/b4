package mtproto

import (
	"context"
	"crypto/tls"
	stdlog "log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

type webListenerSpec struct {
	addr string
	cert string
	key  string
}

func webListenerSpecFor(cfg *config.Config) (webListenerSpec, bool) {
	if cfg == nil {
		return webListenerSpec{}, false
	}
	mt := cfg.System.MTProto
	wp := mt.WebProxy
	if !mt.Enabled || !wp.Enabled || wp.Port <= 0 || wp.Port > 65535 {
		return webListenerSpec{}, false
	}
	spec := webListenerSpec{
		addr: net.JoinHostPort(mt.BindAddress, strconv.Itoa(wp.Port)),
		cert: wp.TLSCert,
		key:  wp.TLSKey,
	}
	if spec.cert == "" && spec.key == "" {
		spec.cert = cfg.System.WebServer.TLSCert
		spec.key = cfg.System.WebServer.TLSKey
	}
	return spec, true
}

func (s *Server) WebProxyPort() int {
	cfg := s.cfg.Load()
	if cfg == nil {
		return 0
	}
	return cfg.System.MTProto.WebProxy.Port
}

type webErrLog struct{}

func (webErrLog) Write(p []byte) (int, error) {
	log.Debugf("MTProto WEB proxy: %s", strings.TrimSpace(string(p)))
	return len(p), nil
}

func (s *Server) webHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.ServeWebProxy(w, r) {
			return
		}
		status := http.StatusNotFound
		if r.URL.Path == "/" && (r.Method == http.MethodGet || r.Method == http.MethodHead) {
			status = http.StatusOK
		}
		s.webWriteSite(w, r, status)
	})
}

func (s *Server) startWebListenerLocked(cfg *config.Config) {
	spec, ok := webListenerSpecFor(cfg)
	if !ok {
		return
	}
	var tlsCfg *tls.Config
	if spec.cert != "" {
		pair, err := tls.LoadX509KeyPair(spec.cert, spec.key)
		if err != nil {
			log.Errorf("MTProto WEB proxy: TLS certificate/key pair not loaded: %v (relay port %s not started)", err, spec.addr)
			return
		}
		tlsCfg = &tls.Config{Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12}
	}
	ln, err := net.Listen("tcp", spec.addr)
	if err != nil {
		log.Errorf("MTProto WEB proxy listen: %v (relay port not started)", err)
		return
	}
	srv := &http.Server{
		Handler:           s.webHandler(),
		ReadHeaderTimeout: 5 * time.Second,
		TLSConfig:         tlsCfg,
		ErrorLog:          stdlog.New(webErrLog{}, "", 0),
	}
	s.webSrv = srv
	s.webSpec = spec
	if tlsCfg != nil {
		log.Infof("MTProto WEB proxy relay listening on https://%s", spec.addr)
		go func() { _ = srv.ServeTLS(ln, "", "") }()
		return
	}
	log.Warnf("MTProto WEB proxy relay listening on http://%s without TLS; Telegram Desktop needs HTTPS, so a TLS-terminating proxy has to sit in front of it", spec.addr)
	go func() { _ = srv.Serve(ln) }()
}

func (s *Server) stopWebListenerLocked() {
	if s.webSrv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	if err := s.webSrv.Shutdown(ctx); err != nil {
		_ = s.webSrv.Close()
	}
	cancel()
	s.webSrv = nil
	s.webSpec = webListenerSpec{}
}

func (s *Server) reconcileWebListenerLocked(cfg *config.Config) {
	spec, ok := webListenerSpecFor(cfg)
	if ok && s.webSrv != nil && spec == s.webSpec {
		return
	}
	s.stopWebListenerLocked()
	if ok {
		s.startWebListenerLocked(cfg)
	}
}
