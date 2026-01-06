package http

import (
	"embed"
	"fmt"
	"io"
	stdhttp "net/http"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/http/handler"
	"github.com/daniellavrushin/b4/http/ws"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/nfq"
)

//go:embed ui/dist/*
var uiDist embed.FS

func StartServer(cfg *config.Config, pool *nfq.Pool) (*stdhttp.Server, error) {
	if cfg.System.WebServer.Port == 0 {
		log.Infof("Web server disabled (port 0)")
		return nil, nil
	}

	mux := stdhttp.NewServeMux()

	handler.SetNFQPool(pool)
	registerWebSocketEndpoints(mux)

	registerAPIEndpoints(mux, cfg)

	handler.RegisterSpa(mux, uiDist)

	var httpHandler stdhttp.Handler = mux
	httpHandler = cors(httpHandler)

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

	log.Infof("Starting web server on %s", addr)

	metrics := handler.GetMetricsCollector()
	metrics.RecordEvent("info", fmt.Sprintf("Web server started on port %d", cfg.System.WebServer.Port))

	srv := &stdhttp.Server{
		Addr:              addr,
		Handler:           httpHandler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		if err := srv.ListenAndServe(); err != nil && err != stdhttp.ErrServerClosed {
			log.Errorf("Web server error: %v", err)
			metrics := handler.GetMetricsCollector()
			metrics.RecordEvent("error", fmt.Sprintf("Web server error: %v", err))
		}
	}()

	return srv, nil
}

// registerWebSocketEndpoints registers all WebSocket handlers
func registerWebSocketEndpoints(mux *stdhttp.ServeMux) {
	mux.HandleFunc("/api/ws/logs", ws.HandleLogsWebSocket)
	mux.HandleFunc("/api/ws/metrics", ws.HandleMetricsWebSocket)
	mux.HandleFunc("/api/ws/discovery", ws.HandleDiscoveryWebSocket)
	log.Tracef("WebSocket endpoints registered: /api/ws/logs, /api/ws/metrics, /api/ws/discovery")
}

// registerAPIEndpoints registers all REST API handlers
func registerAPIEndpoints(mux *stdhttp.ServeMux, cfg *config.Config) {

	api := handler.NewAPIHandler(cfg)
	api.RegisterEndpoints(mux, cfg)

	log.Tracef("REST API endpoints registered")
}

func LogWriter() io.Writer {
	return ws.LogWriter()
}

func Shutdown() {
	// Shutdown the log hub
	ws.Shutdown()
}
