package web

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4hub/internal/api"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/catalogue"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	PathIndex = "/"
	PathSet   = "/s/"
	PathAdmin = "/admin"

	maxDomainLength = 253
	searchLimit     = 50
	cacheNever      = "no-cache"
)

//go:embed templates/*.html
var templateFS embed.FS

type Searcher interface {
	Search(domain string, limit int) ([]api.Hit, int)
}

type Server struct {
	Store         *store.Store
	Blobs         hubdata.Blobs
	Catalogue     *catalogue.Builder
	Search        Searcher
	ASN           *asn.Resolver
	AdminPassword string
	Rebuild       func() error
	Now           func() time.Time

	pages map[string]*template.Template
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) Mount(mux *http.ServeMux) {
	s.pages = parsePages()
	mux.HandleFunc("GET /{$}", s.index)
	mux.HandleFunc("GET "+PathSet+"{id}", s.set)
	s.mountAdmin(mux)
}

func (s *Server) Router() *http.ServeMux {
	mux := http.NewServeMux()
	s.Mount(mux)
	return mux
}

var templateFuncs = template.FuncMap{
	"day": func(stamp string) string {
		if len(stamp) >= 10 {
			return stamp[:10]
		}
		return stamp
	},
	"when": func(t time.Time) string {
		if t.IsZero() {
			return ""
		}
		return t.UTC().Format("2006-01-02 15:04")
	},
	"join": strings.Join,
	"n": func(v float64) string {
		return fmt.Sprintf("%.1f", v)
	},
}

func parsePages() map[string]*template.Template {
	names := []string{"index", "set", "admin", "keys", "message"}
	pages := make(map[string]*template.Template, len(names))
	for _, name := range names {
		pages[name] = template.Must(template.New("layout").Funcs(templateFuncs).ParseFS(templateFS, "templates/layout.html", "templates/"+name+".html"))
	}
	return pages
}

type Base struct {
	Title string
	Admin bool
}

func (s *Server) render(w http.ResponseWriter, status int, page string, data interface{}) {
	tmpl, ok := s.pages[page]
	if !ok {
		http.Error(w, "no such page", http.StatusInternalServerError)
		return
	}
	var buf bytes.Buffer
	if err := tmpl.ExecuteTemplate(&buf, "layout", data); err != nil {
		log.Printf("web: render %s: %v", page, err)
		http.Error(w, "page failed to render", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", cacheNever)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.WriteHeader(status)
	_, _ = w.Write(buf.Bytes())
}

type MessagePage struct {
	Base
	Heading string
	Text    string
}

func (s *Server) message(w http.ResponseWriter, status int, admin bool, heading, text string) {
	s.render(w, status, "message", MessagePage{Base: Base{Title: heading, Admin: admin}, Heading: heading, Text: text})
}

type Viewer struct {
	ASN     string
	Country string
	Name    string
}

func (s *Server) viewer(r *http.Request) Viewer {
	if s.ASN == nil {
		return Viewer{}
	}
	ip := asn.ClientIP(r)
	if !asn.Routable(ip) {
		return Viewer{}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	info := s.ASN.Lookup(ctx, ip)
	return Viewer{ASN: info.ASN, Country: info.Country, Name: info.Name}
}

type Card struct {
	ID        string
	Version   int
	Title     string
	Family    string
	Targets   string
	Strategy  []string
	Rating    Rating
	Flags     []string
	UpdatedAt string
	Match     *api.Match
}

func (s *Server) card(cs *hubwire.CatalogueSet, names map[string]string, viewer Viewer, match *api.Match) Card {
	c := Card{
		ID:        cs.ID,
		Version:   cs.Version,
		Title:     cs.Title,
		Family:    cs.Family,
		Targets:   TargetsOf(cs.Set).Summary(),
		Rating:    RatingOf(cs.Scores, names, viewer.ASN, viewer.Country),
		Flags:     cs.Flags,
		UpdatedAt: cs.UpdatedAt,
		Match:     match,
	}
	if set, err := DecodeSet(cs.Set); err == nil {
		c.Strategy = StrategyWords(&set, cs.Payloads)
	}
	return c
}

type IndexPage struct {
	Base
	Domain    string
	Query     bool
	Cards     []Card
	Total     int
	Viewer    Viewer
	Catalogue *hubwire.Catalogue
}

func cleanDomain(raw string) string {
	domain := sni.NormalizeDomain(strings.TrimSpace(raw))
	if len(domain) > maxDomainLength {
		return ""
	}
	return domain
}

func (s *Server) index(w http.ResponseWriter, r *http.Request) {
	domain := cleanDomain(r.URL.Query().Get("domain"))
	page := IndexPage{Base: Base{Title: "Shared sets"}, Domain: domain, Query: r.URL.Query().Has("domain")}
	if latest := s.Catalogue.Latest(); latest != nil {
		page.Catalogue = latest.Catalogue
	}
	if page.Query && domain == "" {
		s.render(w, http.StatusOK, "index", page)
		return
	}
	page.Viewer = s.viewer(r)
	var names map[string]string
	if page.Catalogue != nil {
		names = page.Catalogue.ASNNames
	}
	hits, total := s.Search.Search(domain, searchLimit)
	page.Total = total
	page.Cards = make([]Card, 0, len(hits))
	for i := range hits {
		page.Cards = append(page.Cards, s.card(&hits[i].CatalogueSet, names, page.Viewer, hits[i].Match))
	}
	s.render(w, http.StatusOK, "index", page)
}

type PayloadView struct {
	Protocol string
	Domain   string
	Size     int
	SHA256   string
	Missing  bool
}

type SetPage struct {
	Base
	Set      *hubwire.CatalogueSet
	Card     Card
	Targets  Targets
	Payloads []PayloadView
	Envelope string
	Viewer   Viewer
}

func (s *Server) envelopeJSON(cs *hubwire.CatalogueSet) (string, []PayloadView) {
	env := cs.ToEnvelope()
	views := make([]PayloadView, 0, len(cs.Payloads))
	for _, ref := range cs.Payloads {
		view := PayloadView{Protocol: ref.Protocol, Domain: ref.Domain, Size: ref.Size, SHA256: ref.SHA256}
		data, err := s.Blobs.Read(ref.SHA256)
		if err != nil {
			view.Missing = true
			views = append(views, view)
			continue
		}
		env.Payloads = append(env.Payloads, hubwire.Payload{SHA256: ref.SHA256, Protocol: ref.Protocol, Domain: ref.Domain, Size: len(data), Data: data})
		views = append(views, view)
	}
	raw, err := json.MarshalIndent(env, "", "  ")
	if err != nil {
		return "", views
	}
	return string(raw), views
}

func (s *Server) set(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !hubdata.ValidSetID(id) {
		s.message(w, http.StatusNotFound, false, "No such set", "The address does not name a shared set.")
		return
	}
	latest := s.Catalogue.Latest()
	var cs *hubwire.CatalogueSet
	if latest != nil {
		cs = latest.ByID[id]
	}
	if cs == nil {
		s.unlistedSet(w, r, id)
		return
	}
	viewer := s.viewer(r)
	envelope, payloads := s.envelopeJSON(cs)
	page := SetPage{
		Base:     Base{Title: cs.Title},
		Set:      cs,
		Card:     s.card(cs, latest.Catalogue.ASNNames, viewer, nil),
		Targets:  TargetsOf(cs.Set),
		Payloads: payloads,
		Envelope: envelope,
		Viewer:   viewer,
	}
	s.render(w, http.StatusOK, "set", page)
}

func (s *Server) unlistedSet(w http.ResponseWriter, r *http.Request, id string) {
	if s.Store == nil {
		s.message(w, http.StatusNotFound, false, "No such set", "This set is not in the published catalogue.")
		return
	}
	v, err := s.Store.LatestVersion(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		s.message(w, http.StatusNotFound, false, "No such set", "This set is not in the published catalogue.")
		return
	}
	if err != nil {
		s.message(w, http.StatusInternalServerError, false, "Error", err.Error())
		return
	}
	text := "This set is not listed."
	switch v.Status {
	case hubwire.SetStatusPending:
		text = "This set is waiting for moderation and is not listed yet."
	case hubwire.SetStatusHidden:
		text = "This set was hidden from the catalogue."
		if v.StatusReason != "" {
			text += " Reason: " + v.StatusReason + "."
		}
	case hubwire.SetStatusRejected:
		text = "This set was not accepted."
	case hubwire.SetStatusActive:
		text = "This set was approved and will appear once the catalogue is rebuilt."
	}
	s.message(w, http.StatusNotFound, false, "Set not listed", text)
}
