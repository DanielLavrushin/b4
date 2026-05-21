package http

import (
	"crypto/tls"
	"embed"
	"fmt"
	"io"
	stdlog "log"
	stdhttp "net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
	"github.com/daniellavrushin/b4/http/ws"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nfq"
)

//go:embed ui/dist/*
var uiDist embed.FS

type errLogFilter struct{ w io.Writer }

func (f errLogFilter) Write(p []byte) (int, error) {
	s := string(p)
	if strings.Contains(s, "TLS handshake error") ||
		strings.Contains(s, "tls: ") ||
		strings.Contains(s, "http: URL query contains semicolon") {
		return len(p), nil
	}
	return f.w.Write(p)
}

func StartServer(cfgPtr *atomic.Pointer[config.Config], pool *nfq.Pool) (*stdhttp.Server, *handler.API, error) {
	cfg := cfgPtr.Load()
	if cfg.System.WebServer.Port == 0 {
		log.Infof("Web server disabled (port 0)")
		return nil, nil, nil
	}

	mux := stdhttp.NewServeMux()

	handler.SetNFQPool(pool)
	registerWebSocketEndpoints(mux)

	api := registerAPIEndpoints(mux, cfgPtr)
	registerAuthEndpoints(mux, cfgPtr)

	handler.RegisterSpa(mux, uiDist)

	var httpHandler stdhttp.Handler = mux
	httpHandler = authMiddleware(cfgPtr, httpHandler)
	httpHandler = cors(httpHandler)

	if authEnabled(cfg) {
		log.Infof("Web server authentication enabled")
	}

	bindAddr := cfg.System.WebServer.BindAddress
	if bindAddr == "" {
		bindAddr = "0.0.0.0"
	}

	var addr string
	if strings.Contains(bindAddr, ":") {
		addr = fmt.Sprintf("[%s]:%d", bindAddr, cfg.System.WebServer.Port)
	} else {
		addr = fmt.Sprintf("%s:%d", bindAddr, cfg.System.WebServer.Port)
	}

	tlsEnabled := cfg.System.WebServer.TLSCert != "" && cfg.System.WebServer.TLSKey != ""

	if tlsEnabled {
		if _, err := tls.LoadX509KeyPair(cfg.System.WebServer.TLSCert, cfg.System.WebServer.TLSKey); err != nil {
			log.Warnf("Invalid TLS certificate/key pair: %v — falling back to HTTP", err)
			tlsEnabled = false
		}
	}

	protocol := "http"
	if tlsEnabled {
		protocol = "https"
	}
	log.Infof("Starting web server on %s://%s", protocol, addr)

	metrics := handler.GetMetricsCollector()
	metrics.RecordEvent("info", fmt.Sprintf("Web server started on %s://%s", protocol, addr))

	srv := &stdhttp.Server{
		Addr:              addr,
		Handler:           httpHandler,
		ReadHeaderTimeout: 5 * time.Second,
		ErrorLog:          stdlog.New(errLogFilter{w: os.Stderr}, "", stdlog.LstdFlags),
	}

	go func() {
		var err error
		if tlsEnabled {
			err = srv.ListenAndServeTLS(cfg.System.WebServer.TLSCert, cfg.System.WebServer.TLSKey)
		} else {
			err = srv.ListenAndServe()
		}

		if err != nil && err != stdhttp.ErrServerClosed {
			log.Errorf("Web server error: %v", err)
			metrics := handler.GetMetricsCollector()
			metrics.RecordEvent("error", fmt.Sprintf("Web server error: %v", err))
		}
	}()

	return srv, api, nil
}

// registerWebSocketEndpoints registers all WebSocket handlers
func registerWebSocketEndpoints(mux *stdhttp.ServeMux) {
	mux.HandleFunc("/api/ws/logs", ws.HandleLogsWebSocket)
	mux.HandleFunc("/api/ws/metrics", ws.HandleMetricsWebSocket)
	mux.HandleFunc("/api/ws/discovery", ws.HandleDiscoveryWebSocket)
	mux.HandleFunc("/api/ws/connections", ws.HandleConnectionsWebSocket)
	log.Tracef("WebSocket endpoints registered: /api/ws/logs, /api/ws/metrics, /api/ws/discovery, /api/ws/connections")
}

// registerAPIEndpoints registers all REST API handlers
func registerAPIEndpoints(mux *stdhttp.ServeMux, cfgPtr *atomic.Pointer[config.Config]) *handler.API {

	api := handler.NewAPIHandler(cfgPtr)
	api.RegisterEndpoints(mux, cfgPtr)

	log.Tracef("REST API endpoints registered")
	return api
}

func LogWriter() io.Writer {
	return ws.LogWriter()
}

func Shutdown() {
	// Shutdown the log hub
	ws.Shutdown()
}
