package mirror

import (
	"bytes"
	"embed"
	"encoding/json"
	"errors"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
)

const (
	cacheForever = "public, max-age=31536000, immutable"
	cacheNever   = "no-cache"
)

//go:embed templates/index.html
var templateFS embed.FS

var page = template.Must(template.New("index.html").Funcs(template.FuncMap{
	"when": func(t time.Time) string {
		if t.IsZero() {
			return "never"
		}
		return t.UTC().Format("2006-01-02 15:04 UTC")
	},
}).ParseFS(templateFS, "templates/index.html"))

func (s *Service) Router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+hubwire.PathHealth, s.health)
	mux.HandleFunc("GET "+hubwire.PathManifest, s.manifest)
	mux.HandleFunc("GET "+hubwire.PathFiles+"{file}", s.catalogueFile)
	mux.HandleFunc("GET "+hubwire.PathBlob+"{hash}", s.blob)
	mux.HandleFunc("POST "+hubwire.PathMessage, s.message)
	mux.HandleFunc("GET /{$}", s.index)
	return mux
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "error": message})
}

func (s *Service) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", cacheNever)
	if s.current() == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte("no catalogue copy yet"))
		return
	}
	_, _ = w.Write([]byte("ok"))
}

func (s *Service) manifest(w http.ResponseWriter, r *http.Request) {
	raw, err := os.ReadFile(filepath.Join(s.opts.Layout.Public(), catalogue.ManifestFile))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusServiceUnavailable, "not_synced", "no catalogue copy yet")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", cacheNever)
	_, _ = w.Write(raw)
}

func (s *Service) catalogueFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !catalogue.ValidFileName(name) {
		writeError(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	f, err := os.Open(filepath.Join(s.opts.Layout.Public(), name))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "not_found", "no such catalogue")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	w.Header().Set("Cache-Control", cacheForever)
	http.ServeContent(w, r, name, info.ModTime(), f)
}

func (s *Service) blob(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(r.PathValue("hash"))
	blobs := s.opts.Layout.Blobs()
	if !hubdata.ValidBlobHash(hash) || !blobs.Exists(hash) {
		writeError(w, http.StatusNotFound, "not_found", "no such payload")
		return
	}
	data, err := blobs.Read(hash)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such payload")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", cacheForever)
	http.ServeContent(w, r, hash, time.Time{}, bytes.NewReader(data))
}

func (s *Service) message(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, ingest.MaxBodyBytes)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, ingest.CodeTooLarge, "record exceeds the message limit")
			return
		}
		writeError(w, http.StatusBadRequest, ingest.CodeBadRecord, err.Error())
		return
	}
	answer := s.relay.Handle(r.Context(), raw)
	contentType := answer.ContentType
	if contentType == "" {
		contentType = "application/json"
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", cacheNever)
	w.WriteHeader(answer.Status)
	_, _ = w.Write(answer.Body)
}

func (s *Service) index(w http.ResponseWriter, r *http.Request) {
	status := s.Status()
	status.Queued = s.relay.Queued()
	var buf bytes.Buffer
	if err := page.Execute(&buf, status); err != nil {
		log.Printf("mirror: render index: %v", err)
		http.Error(w, "page failed to render", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", cacheNever)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = w.Write(buf.Bytes())
}
