package hubtest

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
)

type Answer struct {
	Status int
	Body   interface{}
}

type FakeHub struct {
	Identity    *hubwire.Identity
	Server      *httptest.Server
	Mirrors     []string
	RevokedKeys []string
	Network     *hubwire.NetworkInfo

	mu        sync.Mutex
	manifest  *hubwire.Manifest
	catalogue []byte
	blobs     map[string][]byte
	records   []*hubwire.Record
	answer    func(rec *hubwire.Record) Answer
	down      bool
	noFile    bool
}

func New(t testing.TB) *FakeHub {
	t.Helper()
	id, err := hubwire.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}
	f := &FakeHub{Identity: id, blobs: map[string][]byte{}}
	mux := http.NewServeMux()
	mux.HandleFunc(hubwire.PathHealth, f.serveHealth)
	mux.HandleFunc(hubwire.PathManifest, f.serveManifest)
	mux.HandleFunc(hubwire.PathBlob+"{hash}", f.serveBlob)
	mux.HandleFunc(hubwire.PathMessage, f.serveMessage)
	mux.HandleFunc(hubwire.PathFiles+"{file}", f.serveFile)
	mux.HandleFunc(hubwire.PathNetwork, f.serveNetwork)
	f.Server = httptest.NewTLSServer(mux)
	t.Cleanup(f.Server.Close)
	return f
}

func (f *FakeHub) URL() string {
	return f.Server.URL
}

func (f *FakeHub) Client() *http.Client {
	return f.Server.Client()
}

func (f *FakeHub) Config(t testing.TB, configPath string) *config.Config {
	t.Helper()
	cfg := config.NewConfig()
	cfg.ConfigPath = configPath
	cfg.System.Hub.Enabled = true
	cfg.System.Hub.PublicKey = f.Identity.KeyID()
	cfg.System.Hub.URLs = []string{f.URL()}
	return &cfg
}

func (f *FakeHub) SetDown(down bool) {
	f.mu.Lock()
	f.down = down
	f.mu.Unlock()
}

func (f *FakeHub) SetCatalogueMissing(missing bool) {
	f.mu.Lock()
	f.noFile = missing
	f.mu.Unlock()
}

func (f *FakeHub) SetAnswer(fn func(rec *hubwire.Record) Answer) {
	f.mu.Lock()
	f.answer = fn
	f.mu.Unlock()
}

func (f *FakeHub) Records() []*hubwire.Record {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]*hubwire.Record(nil), f.records...)
}

func (f *FakeHub) Manifest() *hubwire.Manifest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.manifest
}

func (f *FakeHub) Publish(t testing.TB, cat *hubwire.Catalogue, expiresAt time.Time) *hubwire.Manifest {
	t.Helper()
	if cat.GeneratedAt == "" {
		cat.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	}
	gz, err := encodeCatalogue(cat)
	if err != nil {
		t.Fatal(err)
	}
	m := &hubwire.Manifest{
		Epoch:       cat.Epoch,
		Seq:         cat.Seq,
		GeneratedAt: cat.GeneratedAt,
		ExpiresAt:   expiresAt.UTC().Format(time.RFC3339),
		Catalogue: hubwire.FileRef{
			File:   hubwire.CatalogueFileName(cat.Epoch, cat.Seq),
			SHA256: hubwire.BlobHash(gz),
			Size:   int64(len(gz)),
		},
		Mirrors:     f.Mirrors,
		RevokedKeys: f.RevokedKeys,
	}
	if err := hubwire.SignManifest(m, f.Identity); err != nil {
		t.Fatal(err)
	}
	f.mu.Lock()
	f.manifest = m
	f.catalogue = gz
	f.mu.Unlock()
	return m
}

func encodeCatalogue(cat *hubwire.Catalogue) ([]byte, error) {
	raw, err := json.Marshal(cat)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(raw); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func (f *FakeHub) AddBlob(p hubwire.Payload) hubwire.BlobRef {
	f.mu.Lock()
	f.blobs[strings.ToLower(p.SHA256)] = p.Data
	f.mu.Unlock()
	return hubwire.BlobRef{SHA256: p.SHA256, Protocol: p.Protocol, Domain: p.Domain, Size: p.Size}
}

func (f *FakeHub) ReplaceBlob(hash string, data []byte) {
	f.mu.Lock()
	f.blobs[strings.ToLower(hash)] = data
	f.mu.Unlock()
}

func CatalogueSet(t testing.TB, id string, version int, set *config.SetConfig, read func(string) ([]byte, error)) (hubwire.CatalogueSet, []hubwire.Payload) {
	t.Helper()
	env, _, err := hubwire.Build(set, hubwire.BuildOptions{B4Version: "1.83.0", Engine: "nfqueue", ReadPayload: read})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	cs := hubwire.CatalogueSet{
		ID:          id,
		Version:     version,
		FP:          env.Fingerprint,
		Title:       env.Title,
		Description: env.Description,
		Author:      "tester",
		B4Min:       env.MinB4,
		B4Version:   env.B4Version,
		Engine:      env.Engine,
		Status:      hubwire.SetStatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
		Geo:         env.Geo,
		Set:         env.Set,
	}
	for _, p := range env.Payloads {
		cs.Payloads = append(cs.Payloads, hubwire.BlobRef{SHA256: p.SHA256, Protocol: p.Protocol, Domain: p.Domain, Size: p.Size})
	}
	return cs, env.Payloads
}

func (f *FakeHub) unavailable(w http.ResponseWriter) bool {
	f.mu.Lock()
	down := f.down
	f.mu.Unlock()
	if down {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}
	return down
}

func (f *FakeHub) serveNetwork(w http.ResponseWriter, _ *http.Request) {
	if f.unavailable(w) {
		return
	}
	f.mu.Lock()
	info := f.Network
	f.mu.Unlock()
	if info == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, http.StatusOK, info)
}

func (f *FakeHub) serveHealth(w http.ResponseWriter, _ *http.Request) {
	if f.unavailable(w) {
		return
	}
	w.Header().Set("Content-Type", "text/plain")
	_, _ = w.Write([]byte("ok"))
}

func (f *FakeHub) serveManifest(w http.ResponseWriter, _ *http.Request) {
	if f.unavailable(w) {
		return
	}
	f.mu.Lock()
	m := f.manifest
	f.mu.Unlock()
	if m == nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache")
	_ = json.NewEncoder(w).Encode(m)
}

func (f *FakeHub) serveFile(w http.ResponseWriter, r *http.Request) {
	if f.unavailable(w) {
		return
	}
	f.mu.Lock()
	m, gz, missing := f.manifest, f.catalogue, f.noFile
	f.mu.Unlock()
	if m == nil || missing || r.PathValue("file") != m.Catalogue.File {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/gzip")
	_, _ = w.Write(gz)
}

func (f *FakeHub) serveBlob(w http.ResponseWriter, r *http.Request) {
	if f.unavailable(w) {
		return
	}
	f.mu.Lock()
	data, ok := f.blobs[strings.ToLower(r.PathValue("hash"))]
	f.mu.Unlock()
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, _ = w.Write(data)
}

func writeJSON(w http.ResponseWriter, status int, body interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func (f *FakeHub) serveMessage(w http.ResponseWriter, r *http.Request) {
	if f.unavailable(w) {
		return
	}
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var rec hubwire.Record
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 96<<10)).Decode(&rec); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "bad_record", "error": err.Error()})
		return
	}
	if _, err := hubwire.VerifyRecord(&rec); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"code": "bad_signature", "error": err.Error()})
		return
	}
	f.mu.Lock()
	f.records = append(f.records, &rec)
	answer := f.answer
	f.mu.Unlock()
	if answer != nil {
		a := answer(&rec)
		writeJSON(w, a.Status, a.Body)
		return
	}
	switch rec.Kind {
	case hubwire.RecordShare:
		writeJSON(w, http.StatusAccepted, map[string]interface{}{
			"id": rec.ID(), "kind": rec.Kind, "set_id": "hub-" + rec.ID()[:8], "version": 1, "status": hubwire.SetStatusPending,
		})
	default:
		writeJSON(w, http.StatusAccepted, map[string]interface{}{"id": rec.ID(), "kind": rec.Kind})
	}
}
