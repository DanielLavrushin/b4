package catalogue

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

func TestMirrorHealthRequiresAManifestSignedByTheHub(t *testing.T) {
	layout := hubdata.Layout{Root: t.TempDir()}
	if err := layout.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(layout.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	id, err := hubwire.NewIdentity()
	if err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var served hubwire.Manifest
	serve := func(m hubwire.Manifest) {
		mu.Lock()
		served = m
		mu.Unlock()
	}
	mux := http.NewServeMux()
	mux.HandleFunc(hubwire.PathHealth, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc(hubwire.PathManifest, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		m := served
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(m)
	})
	mirror := httptest.NewServer(mux)
	t.Cleanup(mirror.Close)

	ctx := context.Background()
	now := time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	row, err := st.AnnounceMirror(ctx, mirror.URL, "hmac", "1.0.0", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMirrorStatus(ctx, row.ID, store.MirrorApproved, "", now); err != nil {
		t.Fatal(err)
	}
	h := &MirrorHealth{Store: st, KeyID: id.KeyID(), Now: func() time.Time { return now }}

	base := hubwire.Manifest{Epoch: 1, Seq: 1, GeneratedAt: now.Format(time.RFC3339), ExpiresAt: now.Add(time.Hour).Format(time.RFC3339)}
	unsigned := base
	unsigned.V = hubwire.WireVersion
	unsigned.KeyID = id.KeyID()
	serve(unsigned)
	healthy, err := h.Healthy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(healthy) != 0 {
		t.Fatalf("a mirror serving an unsigned manifest with the hub's key id must not be advertised, got %v", healthy)
	}
	checked, err := st.GetMirror(ctx, row.ID)
	if err != nil {
		t.Fatal(err)
	}
	if checked.Healthy() || checked.Reason == "" {
		t.Errorf("the failed check must be recorded with its reason, got %+v", checked)
	}

	signed := base
	if err := hubwire.SignManifest(&signed, id); err != nil {
		t.Fatal(err)
	}
	serve(signed)
	healthy, err = h.Healthy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(healthy) != 1 || healthy[0] != mirror.URL {
		t.Fatalf("a mirror serving the hub's signed manifest must be advertised, got %v", healthy)
	}

	other, _ := hubwire.NewIdentity()
	foreign := base
	if err := hubwire.SignManifest(&foreign, other); err != nil {
		t.Fatal(err)
	}
	serve(foreign)
	now = now.Add(2 * DefaultMirrorWindow)
	healthy, err = h.Healthy(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(healthy) != 0 {
		t.Errorf("a mirror serving a manifest signed by another key must drop out, got %v", healthy)
	}
}
