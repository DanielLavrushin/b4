package ws

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/daniellavrushin/b4/log"
	"github.com/gorilla/websocket"
)

type logClient struct {
	ws   *websocket.Conn
	send chan []byte
}

var (
	logHub    *LogHub
	logOnce   sync.Once
	logWriter *broadcastWriter
)

// GetLogHub returns the singleton log hub
func GetLogHub() *LogHub {
	logOnce.Do(func() {
		logHub = &LogHub{
			clients: map[*logClient]struct{}{},
			in:      make(chan []byte, 1024),
			reg:     make(chan *logClient),
			unreg:   make(chan *logClient),
			stop:    make(chan struct{}),
		}
		go logHub.run()
	})
	return logHub
}

func (h *LogHub) run() {
	for {
		select {
		case <-h.stop:
			// Shutdown: close all client connections
			h.mu.Lock()
			for c := range h.clients {
				close(c.send)
				delete(h.clients, c)
			}
			h.mu.Unlock()
			return

		case c := <-h.reg:
			h.mu.Lock()
			h.clients[c] = struct{}{}
			h.mu.Unlock()

		case c := <-h.unreg:
			h.mu.Lock()
			if _, ok := h.clients[c]; ok {
				delete(h.clients, c)
				close(c.send)
			}
			h.mu.Unlock()

		case msg := <-h.in:
			h.mu.RLock()
			for c := range h.clients {
				select {
				case c.send <- msg:
				default:
					// Client's buffer is full, skip
				}
			}
			h.mu.RUnlock()
		}
	}
}

type broadcastWriter struct {
	h   *LogHub
	mu  sync.Mutex
	buf []byte
}

func (w *broadcastWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.buf = append(w.buf, p...)
	start := 0
	for {
		i := bytes.IndexByte(w.buf[start:], '\n')
		if i < 0 {
			break
		}
		end := start + i
		line := make([]byte, end-start)
		copy(line, w.buf[start:end])
		select {
		case w.h.in <- line:
		case <-w.h.stop:
		}
		start = end + 1
	}
	if start > 0 {
		w.buf = append([]byte{}, w.buf[start:]...)
	}
	w.mu.Unlock()
	return len(p), nil
}

// LogWriter returns a writer that broadcasts to all connected WebSocket clients
func LogWriter() io.Writer {
	hub := GetLogHub()
	if logWriter == nil {
		logWriter = &broadcastWriter{h: hub}
	}
	return logWriter
}

// HandleLogsWebSocket handles WebSocket connections for log streaming
func HandleLogsWebSocket(w http.ResponseWriter, r *http.Request) {
	h := GetLogHub()
	conn, err := Upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Errorf("Failed to upgrade logs WebSocket: %v", err)
		return
	}

	c := &logClient{ws: conn, send: make(chan []byte, 256)}
	log.Tracef("Logs WebSocket client connected: %s", r.RemoteAddr)

	h.reg <- c
	go c.writePump()
	c.readPump(h, r.Context())
}

func (h *LogHub) Stop() {
	select {
	case <-h.stop:
		// Already stopped
		return
	default:
		close(h.stop)
	}
}

// Export a Shutdown function to be called during app shutdown:
func Shutdown() {
	if logHub != nil {
		logHub.Stop()
	}
}

func (c *logClient) writePump() {
	ticker := time.NewTicker(54 * time.Second)
	defer func() {
		ticker.Stop()
		c.ws.Close()
	}()

	for {
		select {
		case message, ok := <-c.send:
			c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if !ok {
				c.ws.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			if err := c.ws.WriteMessage(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			c.ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if err := c.ws.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

func (c *logClient) readPump(h *LogHub, ctx context.Context) {
	defer func() {
		h.unreg <- c
		c.ws.Close()
	}()

	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			if _, _, err := c.ws.ReadMessage(); err != nil {
				return
			}
		}
	}()

	select {
	case <-done:
	case <-ctx.Done():
		c.ws.Close()
	}
}
