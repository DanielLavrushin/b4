package web

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log"
	"net/http"
	"path"
	"strings"
	"time"

	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/geo"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ratelimit"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/ui"
)

const (
	PathAdmin = "/admin"
	PathAPI   = PathAdmin + "/api"

	cacheNever   = "no-store"
	cacheForever = "public, max-age=31536000, immutable"

	codeUnconfigured = "unconfigured"
	codeUnauthorized = "unauthorized"
	codeForbidden    = "forbidden"
	codeNotFound     = "not_found"
	codeBadRequest   = "bad_request"
	codeTooMany      = "too_many_attempts"
	codeInternal     = "internal"
	codeNotBuilt     = "not_built"
)

type Server struct {
	Store         *store.Store
	Blobs         hubdata.Blobs
	Catalogue     *catalogue.Builder
	Geo           *geo.Service
	ASN           *asn.Resolver
	Secret        []byte
	AdminPassword string
	Version       string
	Source        string
	KeyID         string
	PublicURL     string
	Rebuild       func() error
	Now           func() time.Time

	logins *ratelimit.Limiter
	dist   fs.FS
	index  []byte
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) Mount(mux *http.ServeMux) {
	s.logins = ratelimit.New(s.Now)
	s.loadDist()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, PathAdmin+"/", http.StatusFound)
	})
	s.mountAPI(mux)
	mux.HandleFunc("GET "+PathAdmin, s.spa)
	mux.HandleFunc("GET "+PathAdmin+"/{$}", s.spa)
	mux.HandleFunc("GET "+PathAdmin+"/{rest...}", s.spa)
}

func (s *Server) Router() *http.ServeMux {
	mux := http.NewServeMux()
	s.Mount(mux)
	return mux
}

func (s *Server) loadDist() {
	dist, err := fs.Sub(ui.Dist, "dist")
	if err != nil {
		return
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		log.Printf("web: admin console is not built, %s serves a placeholder", PathAdmin)
		return
	}
	s.dist = dist
	s.index = index
}

func (s *Server) spa(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	if s.index == nil {
		w.Header().Set("Cache-Control", cacheNever)
		http.Error(w, "the admin console was not built into this binary", http.StatusServiceUnavailable)
		return
	}
	rest := strings.TrimPrefix(r.PathValue("rest"), "/")
	if rest != "" && !strings.HasSuffix(rest, "/") {
		name := path.Clean(rest)
		if f, err := s.dist.Open(name); err == nil {
			info, statErr := f.Stat()
			f.Close()
			if statErr == nil && !info.IsDir() {
				if strings.HasPrefix(name, "assets/") {
					w.Header().Set("Cache-Control", cacheForever)
				} else {
					w.Header().Set("Cache-Control", cacheNever)
				}
				http.ServeFileFS(w, r, s.dist, name)
				return
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", cacheNever)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(s.index)
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", cacheNever)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"code": code, "error": message})
}

func (s *Server) fail(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, err.Error())
		return
	}
	log.Printf("web: %v", err)
	writeError(w, http.StatusInternalServerError, codeInternal, err.Error())
}

const maxBodyBytes = 64 << 10

func readBody(w http.ResponseWriter, r *http.Request, into interface{}) error {
	if r.Body == nil || r.ContentLength == 0 {
		return nil
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(into); err != nil {
		return err
	}
	return nil
}
