package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/geodat"
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
	addActiveSetFrom(t, st, id, fp, nil)
}

func addActiveSetFrom(t *testing.T, st *store.Store, id, fp string, geo *hubwire.GeoSource) {
	t.Helper()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	v := &store.Version{
		FP:         fp,
		TargetsKey: "targets-" + id,
		Title:      "set " + id,
		Projection: map[string]interface{}{"targets": map[string]interface{}{"sni_domains": []interface{}{id + ".example"}}},
		Payloads:   []hubwire.BlobRef{{SHA256: "aa" + fp, Protocol: "tls", Size: 3}},
		Geo:        geo,
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

func TestAStoredCustomGeodataAddressIsNotPublished(t *testing.T) {
	b, st := testBuilder(t)
	private := "https://canaryuser:canarypass@files.example/canarytoken/geosite.dat?sig=canarysig"
	sources, err := geodat.Sources()
	if err != nil || len(sources) == 0 {
		t.Fatalf("no built-in sources: %v", err)
	}
	site, ip := sources[0].GeositeURL, sources[0].GeoipURL
	both, one, plain := "01ARZ3NDEKTSV4RRFFQ69G5FAV", "01ARZ3NDEKTSV4RRFFQ69G5FAW", "01ARZ3NDEKTSV4RRFFQ69G5FAX"
	addActiveSetFrom(t, st, both, "fp-a", &hubwire.GeoSource{SiteURL: private, IPURL: private})
	addActiveSetFrom(t, st, one, "fp-b", &hubwire.GeoSource{SiteURL: site, IPURL: private})
	addActiveSetFrom(t, st, plain, "fp-c", &hubwire.GeoSource{SiteURL: site, IPURL: ip})

	result, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	gz, err := os.ReadFile(filepath.Join(b.PublicDir, result.Manifest.Catalogue.File))
	if err != nil {
		t.Fatal(err)
	}
	published, err := Decode(gz)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(published)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "canary") || strings.Contains(string(raw), "files.example") {
		t.Errorf("the catalogue names the private address: %s", raw)
	}
	want := map[string]*hubwire.GeoSource{both: nil, one: {SiteURL: site}, plain: {SiteURL: site, IPURL: ip}}
	if len(published.Sets) != len(want) {
		t.Fatalf("the sets themselves are published: %+v", published.Sets)
	}
	for _, cs := range published.Sets {
		if w := want[cs.ID]; (cs.Geo == nil) != (w == nil) || cs.Geo != nil && *cs.Geo != *w {
			t.Errorf("set %s names %+v, want %+v", cs.ID, cs.Geo, w)
		}
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
