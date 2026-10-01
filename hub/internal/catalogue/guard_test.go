package catalogue

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

func TestANewDatabaseDoesNotSignAnEmptyCatalogueWithABuiltinKey(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	b.BuiltinKey = true
	if _, err := b.Build(ctx); !errors.Is(err, ErrNewDatabase) {
		t.Fatalf("an empty first build with a built-in key must be refused, got %v", err)
	}
	if epoch, seq, err := st.CurrentSeq(ctx); err != nil || epoch != 0 || seq != 0 {
		t.Fatalf("a refused build must not spend a sequence number, got %d-%d %v", epoch, seq, err)
	}
	if b.Latest() != nil {
		t.Fatal("nothing may be published")
	}
	b.NewDatabase = true
	result, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Catalogue.Sets) != 0 || result.Manifest.Seq != 1 {
		t.Fatalf("--new-database publishes the empty catalogue: %+v", result.Manifest)
	}
}

func TestANewDatabaseDoesNotEmptyAPublishedCatalogue(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "fp-a")
	if _, err := b.Build(ctx); err != nil {
		t.Fatal(err)
	}

	fresh, err := store.Open(hubdata.Layout{Root: t.TempDir()}.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fresh.Close() })
	restarted := &Builder{Store: fresh, Identity: b.Identity, PublicDir: b.PublicDir, PublicURL: b.PublicURL, Now: b.Now}
	if err := restarted.LoadPublished(); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Build(ctx); !errors.Is(err, ErrNewDatabase) {
		t.Fatalf("a new database next to a published catalogue with sets must not replace it with nothing, got %v", err)
	}
}

func TestANewEpochOrOneApprovalDoesNotLetANewDatabaseReplaceTheCatalogue(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "fp-a")
	addActiveSet(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAW", "fp-b")
	if _, err := b.Build(ctx); err != nil {
		t.Fatal(err)
	}

	fresh, err := store.Open(hubdata.Layout{Root: t.TempDir()}.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fresh.Close() })
	restarted := &Builder{Store: fresh, Identity: b.Identity, PublicDir: b.PublicDir, PublicURL: b.PublicURL, Now: b.Now, BuiltinKey: true}
	if err := restarted.LoadPublished(); err != nil {
		t.Fatal(err)
	}
	if _, err := fresh.NewEpoch(ctx, b.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Build(ctx); !errors.Is(err, ErrNewDatabase) {
		t.Fatalf("a new epoch must not make a never-published database look published, got %v", err)
	}
	addActiveSet(t, fresh, "01ARZ3NDEKTSV4RRFFQ69G5FAX", "fp-c")
	if _, err := restarted.Build(ctx); !errors.Is(err, ErrNewDatabase) {
		t.Fatalf("one approved set must not replace a published catalogue of two, got %v", err)
	}
	if restarted.Latest().Manifest.Seq != 1 || len(restarted.Latest().Catalogue.Sets) != 2 {
		t.Fatalf("the published catalogue stays: %+v", restarted.Latest().Manifest)
	}
	restarted.NewDatabase = true
	if _, err := restarted.Build(ctx); err != nil {
		t.Fatalf("--new-database lets the operator publish it: %v", err)
	}
}

func TestABuildOneBehindIsRefusedInsteadOfReusingANumber(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "fp-a")
	for i := 0; i < 2; i++ {
		if _, err := b.Build(ctx); err != nil {
			t.Fatal(err)
		}
	}
	file := b.Latest().Manifest.Catalogue.File
	if err := st.SetMeta(ctx, "seq", "1"); err != nil {
		t.Fatal(err)
	}
	later := b.Now().Add(time.Minute)
	b.Now = func() time.Time { return later }
	if _, err := b.Build(ctx); !errors.Is(err, ErrBehind) {
		t.Fatalf("a build that would reuse the published number must be refused, got %v", err)
	}
	if b.Latest().Manifest.Catalogue.File != file || b.Latest().Manifest.Seq != 2 {
		t.Fatalf("the published file must stay, got %+v", b.Latest().Manifest)
	}
}

func TestABuildBehindThePublishedCatalogueIsRefused(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "fp-a")
	for i := 0; i < 3; i++ {
		if _, err := b.Build(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := st.SetMeta(ctx, "seq", "1"); err != nil {
		t.Fatal(err)
	}
	_, err := b.Build(ctx)
	if !errors.Is(err, ErrBehind) || !strings.Contains(err.Error(), "new epoch") {
		t.Fatalf("a database restored behind the published catalogue must be refused with a way out, got %v", err)
	}
	if _, seq, _ := st.CurrentSeq(ctx); seq != 1 {
		t.Fatalf("refusals must not count up to the published seq, got %d", seq)
	}
	if b.Latest().Manifest.Seq != 3 {
		t.Fatalf("the published catalogue stays, got seq %d", b.Latest().Manifest.Seq)
	}
	if _, err := st.NewEpoch(ctx, b.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	result, err := b.Build(ctx)
	if err != nil {
		t.Fatalf("a new epoch publishes the restored database: %v", err)
	}
	if result.Manifest.Seq != 1 || !result.Manifest.Newer(&hubwire.Manifest{Epoch: 1, Seq: 3}) {
		t.Fatalf("unexpected manifest after the new epoch: %+v", result.Manifest)
	}
}

func TestABuildBehindWhatAMirrorServesIsRefused(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "fp-a")
	first, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.AnnounceMirror(ctx, "https://mirror.example", "k", "1.3.0", b.Now())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMirrorStatus(ctx, m.ID, store.MirrorApproved, "", b.Now()); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordMirrorCheck(ctx, m.ID, store.MirrorCheck{At: b.Now(), OK: true, Epoch: first.Manifest.Epoch, Seq: 40, GeneratedAt: first.Manifest.GeneratedAt}); err != nil {
		t.Fatal(err)
	}
	_, err = b.Build(ctx)
	if !errors.Is(err, ErrBehind) || !strings.Contains(err.Error(), "mirror https://mirror.example") {
		t.Fatalf("a mirror serving a later build of this key means the database is behind, got %v", err)
	}
}

func signedMirror(t *testing.T, id *hubwire.Identity) (*httptest.Server, func(hubwire.Manifest)) {
	t.Helper()
	var mu sync.Mutex
	var served hubwire.Manifest
	mux := http.NewServeMux()
	mux.HandleFunc(hubwire.PathHealth, func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })
	mux.HandleFunc(hubwire.PathManifest, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		m := served
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(m)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, func(m hubwire.Manifest) {
		if err := hubwire.SignManifest(&m, id); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		served = m
		mu.Unlock()
	}
}

func TestTheFirstBuildAfterALongOutageStillListsTheMirrors(t *testing.T) {
	b, st := testBuilder(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	addActiveSet(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "fp-a")
	srv, serve := signedMirror(t, b.Identity)
	serve(hubwire.Manifest{Epoch: 1, Seq: 1, GeneratedAt: b.Now().Format(time.RFC3339)})
	now := b.Now()
	b.Mirrors = &MirrorHealth{Store: st, KeyID: b.Identity.KeyID(), Now: func() time.Time { return now }}
	m, err := st.AnnounceMirror(ctx, srv.URL, "k", "1.3.0", now)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetMirrorStatus(ctx, m.ID, store.MirrorApproved, "", now); err != nil {
		t.Fatal(err)
	}
	if err := st.RecordMirrorCheck(ctx, m.ID, store.MirrorCheck{At: now.Add(-3 * DefaultMirrorWindow), OK: true, Epoch: 1, Seq: 1}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		b.Run(ctx, time.Hour)
	}()
	deadline := time.Now().Add(10 * time.Second)
	for b.Latest() == nil {
		if time.Now().After(deadline) {
			t.Fatal("the startup build did not happen")
		}
		time.Sleep(10 * time.Millisecond)
	}
	cancel()
	<-done
	mirrors := b.Latest().Manifest.Mirrors
	if len(mirrors) != 2 || mirrors[1] != srv.URL {
		t.Fatalf("the startup build must check the mirrors first and list the one that passes, got %v", mirrors)
	}
}

func TestCheckIfStaleSkipsARecentRound(t *testing.T) {
	_, st := testBuilder(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	h := &MirrorHealth{Store: st, Now: func() time.Time { return now }}
	if ran, err := h.CheckIfStale(ctx, time.Minute); err != nil || !ran {
		t.Fatalf("the first round must run, got %v %v", ran, err)
	}
	if ran, _ := h.CheckIfStale(ctx, time.Minute); ran {
		t.Fatal("a round within maxAge must be skipped")
	}
	now = now.Add(2 * time.Minute)
	if ran, _ := h.CheckIfStale(ctx, time.Minute); !ran {
		t.Fatal("a stale round must run again")
	}
}

func TestANewDatabaseDoesNotReplaceACatalogueOfTheSameSize(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, "01ARZ3NDEKTSV4RRFFQ69G5FAV", "fp-a")
	if _, err := b.Build(ctx); err != nil {
		t.Fatal(err)
	}
	fresh, err := store.Open(hubdata.Layout{Root: t.TempDir()}.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { fresh.Close() })
	addActiveSet(t, fresh, "01ARZ3NDEKTSV4RRFFQ69G5FAX", "fp-x")
	restarted := &Builder{Store: fresh, Identity: b.Identity, PublicDir: b.PublicDir, PublicURL: b.PublicURL, Now: b.Now}
	if err := restarted.LoadPublished(); err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Build(ctx); !errors.Is(err, ErrNewDatabase) || !strings.Contains(err.Error(), "--new-database") {
		t.Fatalf("a never-published database must not replace a published catalogue of the same size, got %v", err)
	}
}
