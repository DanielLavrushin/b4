package api

import (
	"bytes"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/geo"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	PathSearch = "/v1/search"

	cacheForever = "public, max-age=31536000, immutable"
	cacheNever   = "no-cache"

	adminUser = "admin"
)

var catalogueFilePattern = regexp.MustCompile(`^catalogue-[0-9]+-[0-9]+\.json\.gz$`)

type Server struct {
	Store         *store.Store
	Blobs         hubdata.Blobs
	PublicDir     string
	Ingest        *ingest.Service
	Catalogue     *catalogue.Builder
	Geo           *geo.Index
	AdminPassword string
}

func (s *Server) Router() *http.ServeMux {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+hubwire.PathHealth, s.health)
	mux.HandleFunc("GET "+hubwire.PathManifest, s.manifest)
	mux.HandleFunc("GET "+hubwire.PathFiles+"{file}", s.catalogueFile)
	mux.HandleFunc("GET "+hubwire.PathBlob+"{hash}", s.blob)
	mux.HandleFunc("POST "+hubwire.PathMessage, s.message)
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

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", cacheNever)
	_, _ = w.Write([]byte("ok"))
}

func (s *Server) manifest(w http.ResponseWriter, r *http.Request) {
	raw, err := os.ReadFile(filepath.Join(s.PublicDir, catalogue.ManifestFile))
	if errors.Is(err, os.ErrNotExist) {
		writeError(w, http.StatusNotFound, "not_built", "no catalogue has been published yet")
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

func (s *Server) catalogueFile(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("file")
	if !catalogueFilePattern.MatchString(name) {
		writeError(w, http.StatusNotFound, "not_found", "no such file")
		return
	}
	f, err := os.Open(filepath.Join(s.PublicDir, name))
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

func (s *Server) blob(w http.ResponseWriter, r *http.Request) {
	hash := strings.ToLower(r.PathValue("hash"))
	if !hubdata.ValidBlobHash(hash) || !s.Blobs.Exists(hash) {
		writeError(w, http.StatusNotFound, "not_found", "no such payload")
		return
	}
	data, err := s.Blobs.Read(hash)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "no such payload")
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", cacheForever)
	http.ServeContent(w, r, hash, time.Time{}, bytes.NewReader(data))
}

func (s *Server) message(w http.ResponseWriter, r *http.Request) {
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
	resp := s.Ingest.Handle(r.Context(), raw, asn.ClientIP(r))
	writeJSON(w, resp.Status, resp.Body)
}

func (s *Server) BasicAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if s.AdminPassword == "" || !ok ||
			subtle.ConstantTimeCompare([]byte(user), []byte(adminUser)) != 1 ||
			subtle.ConstantTimeCompare([]byte(pass), []byte(s.AdminPassword)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="b4hub"`)
			writeError(w, http.StatusUnauthorized, "unauthorized", "moderator credentials required")
			return
		}
		next.ServeHTTP(w, r)
	})
}
