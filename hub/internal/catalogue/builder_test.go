package catalogue

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

func testBuilder(t *testing.T) (*Builder, *store.Store) {
	t.Helper()
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
	clock := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	b := &Builder{
		Store:      st,
		Identity:   id,
		PublicDir:  layout.Public(),
		PublicURL:  "https://hub.example/",
		GeoSources: []hubwire.GeoSource{{SiteURL: "https://geo.example/geosite.dat"}},
		Now:        func() time.Time { return clock },
	}
	return b, st
}

func addActiveSet(t *testing.T, st *store.Store, id, fp string) {
	t.Helper()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	v := &store.Version{
		FP:         fp,
		TargetsKey: "targets-" + id,
		Title:      "set " + id,
		Projection: map[string]interface{}{"targets": map[string]interface{}{"sni_domains": []interface{}{id + ".example"}}},
		Payloads:   []hubwire.BlobRef{{SHA256: "aa" + fp, Protocol: "tls", Size: 3}},
		Status:     hubwire.SetStatusPending,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := st.CreateSet(context.Background(), store.Set{ID: id, AuthorHMAC: "author-" + id, CreatedAt: now, UpdatedAt: now}, v); err != nil {
		t.Fatal(err)
	}
	if err := st.Approve(context.Background(), id, 1, now); err != nil {
		t.Fatal(err)
	}
}

func TestBuildPublishesSignedManifestAndKeepsThreeFiles(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "fp-a")
	addActiveSet(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAW", "fp-b")

	var last *Result
	for i := 0; i < 5; i++ {
		result, err := b.Build(ctx)
		if err != nil {
			t.Fatal(err)
		}
		last = result
	}
	if last.Manifest.Seq != 5 {
		t.Errorf("seq must count builds, got %d", last.Manifest.Seq)
	}
	if err := hubwire.VerifyManifest(last.Manifest, []string{b.Identity.KeyID()}); err != nil {
		t.Errorf("manifest must verify with the hub key: %v", err)
	}
	if last.Manifest.Mirrors[0] != "https://hub.example" || len(last.Manifest.DoHAllowlist) == 0 || len(last.Manifest.GeoSources) != 1 {
		t.Errorf("manifest metadata missing: %+v", last.Manifest)
	}
	if last.Manifest.Expired(b.Now()) || !last.Manifest.Expired(b.Now().Add(15*24*time.Hour)) {
		t.Errorf("expires_at must be fourteen days out, got %s", last.Manifest.ExpiresAt)
	}
	if len(last.Catalogue.Sets) != 2 || len(last.Catalogue.Blobs) != 2 || last.Catalogue.Sets[0].Author != hubdata.AuthorLabel("author-01ARZ3NDEKTSV4RRFFQ69G5FAV") {
		t.Errorf("unexpected catalogue %+v", last.Catalogue)
	}
	entries, _ := os.ReadDir(b.PublicDir)
	files := 0
	for _, e := range entries {
		if catalogueFilePattern.MatchString(e.Name()) {
			files++
		}
	}
	if files != DefaultKeep {
		t.Errorf("expected %d catalogue files kept, got %d", DefaultKeep, files)
	}
	if _, err := os.Stat(filepath.Join(b.PublicDir, last.Manifest.Catalogue.File)); err != nil {
		t.Errorf("the current file must survive pruning: %v", err)
	}

	needed, _ := b.NeedsBuild(ctx)
	if needed {
		t.Errorf("a fresh build must not need a rebuild")
	}
	if err := st.MarkDirty(ctx); err != nil {
		t.Fatal(err)
	}
	if needed, _ = b.NeedsBuild(ctx); !needed {
		t.Errorf("a dirty store must trigger a rebuild")
	}

	fresh := &Builder{Store: st, Identity: b.Identity, PublicDir: b.PublicDir}
	if err := fresh.LoadPublished(); err != nil {
		t.Fatal(err)
	}
	if fresh.Latest().Manifest.Seq != 5 || len(fresh.Latest().ByID) != 2 {
		t.Errorf("LoadPublished must restore the latest build")
	}
}

func TestLoadPublishedWithoutFiles(t *testing.T) {
	b, _ := testBuilder(t)
	if err := b.LoadPublished(); !errors.Is(err, ErrNotPublished) {
		t.Errorf("expected ErrNotPublished, got %v", err)
	}
	if needed, _ := b.NeedsBuild(context.Background()); !needed {
		t.Errorf("an unpublished hub must build")
	}
}
