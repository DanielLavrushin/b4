package hub

import (
	"context"
	"errors"
	"net/http"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub/hubtest"
	"github.com/daniellavrushin/b4/hubwire"
)

type testBox struct {
	cfg *atomic.Pointer[config.Config]
	svc *Service
}

func newTestBox(t *testing.T, f *hubtest.FakeHub, dir string) *testBox {
	t.Helper()
	cfg := f.Config(t, filepath.Join(dir, "config.json"))
	ptr := &atomic.Pointer[config.Config]{}
	ptr.Store(cfg)
	svc := New(func() *config.Config { return ptr.Load() }, Options{Version: "1.83.0", HTTPClient: f.Client(), BuiltinBases: []string{}})
	return &testBox{cfg: ptr, svc: svc}
}

func (b *testBox) update(fn func(cfg *config.Config)) {
	clone := b.cfg.Load().Clone()
	fn(clone)
	b.cfg.Store(clone)
}

func strategySet(name string, domains []string, categories []string, ttl uint8) config.SetConfig {
	set := config.NewSetConfig()
	set.Name = name
	set.Targets.SNIDomains = domains
	set.Targets.GeoSiteCategories = categories
	set.Fragmentation.Strategy = "tls"
	set.Faking.SNI = true
	set.Faking.TTL = ttl
	return set
}

func sampleCatalogue(t *testing.T, epoch, seq int64) *hubwire.Catalogue {
	t.Helper()
	byDomain := strategySet("YouTube direct", []string{"youtube.com"}, nil, 7)
	byCategory := strategySet("YouTube category", nil, []string{"youtube"}, 8)
	pending := strategySet("Not yet reviewed", []string{"youtube.com"}, nil, 9)
	direct, _ := hubtest.CatalogueSet(t, "direct", 1, &byDomain, nil)
	category, _ := hubtest.CatalogueSet(t, "category", 1, &byCategory, nil)
	unreviewed, _ := hubtest.CatalogueSet(t, "pending", 1, &pending, nil)
	unreviewed.Status = hubwire.SetStatusPending
	category.Scores.Global = hubwire.Score{Score: 0.9, N: 5, Devices: 4}
	return &hubwire.Catalogue{Epoch: epoch, Seq: seq, Sets: []hubwire.CatalogueSet{category, direct, unreviewed}}
}

func TestSyncStoresAndReloadsTheCatalogue(t *testing.T) {
	f := hubtest.New(t)
	f.Publish(t, sampleCatalogue(t, 1, 3), time.Now().Add(7*24*time.Hour))
	dir := t.TempDir()
	box := newTestBox(t, f, dir)

	changed, err := box.svc.Sync(context.Background())
	if err != nil || !changed {
		t.Fatalf("first sync must fetch the catalogue: changed=%v err=%v", changed, err)
	}
	st := box.svc.Status()
	if st.Catalogue == nil || st.Catalogue.Epoch != 1 || st.Catalogue.Seq != 3 || st.Catalogue.Sets != 3 || st.Catalogue.Expired {
		t.Fatalf("unexpected catalogue status %+v", st.Catalogue)
	}
	if st.LastSync == "" || st.LastError != "" || !st.Enabled || !st.Configured {
		t.Errorf("unexpected status %+v", st)
	}

	changed, err = box.svc.Sync(context.Background())
	if err != nil || changed {
		t.Errorf("a second sync of the same manifest must be a no-op: changed=%v err=%v", changed, err)
	}

	reloaded := newTestBox(t, f, dir)
	if m := reloaded.svc.Manifest(); m == nil || m.Seq != 3 {
		t.Fatalf("a new service must load the stored catalogue, got %+v", m)
	}
	if _, ok := reloaded.svc.Get("direct"); !ok {
		t.Errorf("stored catalogue must be searchable before any sync")
	}
}

func TestSyncIgnoresOlderManifestsAndReportsExpiry(t *testing.T) {
	f := hubtest.New(t)
	f.Publish(t, sampleCatalogue(t, 2, 5), time.Now().Add(24*time.Hour))
	box := newTestBox(t, f, t.TempDir())
	if _, err := box.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	f.Publish(t, sampleCatalogue(t, 2, 4), time.Now().Add(24*time.Hour))
	changed, err := box.svc.Sync(context.Background())
	if err != nil || changed {
		t.Errorf("an older seq must not replace the stored catalogue: changed=%v err=%v", changed, err)
	}
	if m := box.svc.Manifest(); m.Seq != 5 {
		t.Errorf("stored manifest was replaced by an older one: seq %d", m.Seq)
	}

	f.Publish(t, sampleCatalogue(t, 3, 1), time.Now().Add(-time.Hour))
	changed, err = box.svc.Sync(context.Background())
	if err != nil || !changed {
		t.Fatalf("a new epoch must be accepted even with a lower seq: changed=%v err=%v", changed, err)
	}
	st := box.svc.Status()
	if st.Catalogue == nil || st.Catalogue.Epoch != 3 || !st.Catalogue.Expired {
		t.Errorf("an expired manifest must be kept and reported as expired: %+v", st.Catalogue)
	}
}

func TestSyncRefusesAnUntrustedSigner(t *testing.T) {
	f := hubtest.New(t)
	f.Publish(t, sampleCatalogue(t, 1, 1), time.Now().Add(time.Hour))
	box := newTestBox(t, f, t.TempDir())
	other, _ := hubwire.NewIdentity()
	box.update(func(cfg *config.Config) { cfg.System.Hub.PublicKey = other.KeyID() })

	_, err := box.svc.Sync(context.Background())
	if !errors.Is(err, ErrUnreachable) || !errors.Is(err, hubwire.ErrManifestSigner) {
		t.Fatalf("a manifest signed by another key must be refused, got %v", err)
	}
	if box.svc.Status().LastError == "" {
		t.Errorf("the failure must be visible in the status")
	}

	box.update(func(cfg *config.Config) { cfg.System.Hub.PublicKey = "" })
	if box.svc.Configured() {
		t.Fatal("with no key the service is not configured")
	}
	if _, err := box.svc.Sync(context.Background()); !errors.Is(err, ErrNotConfigured) {
		t.Errorf("expected ErrNotConfigured, got %v", err)
	}
}

func TestSearchRanksDomainMatchAboveCategory(t *testing.T) {
	f := hubtest.New(t)
	f.Publish(t, sampleCatalogue(t, 1, 1), time.Now().Add(time.Hour))
	box := newTestBox(t, f, t.TempDir())
	if _, err := box.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	results, total := box.svc.Search("www.youtube.com", 0)
	if total != 2 || len(results) != 2 {
		t.Fatalf("expected the two active sets, got %d (%d)", len(results), total)
	}
	if results[0].Set.ID != "direct" || results[0].Match == nil || results[0].Match.Via != MatchViaDomain || results[0].Match.Relation != "covered" {
		t.Errorf("a domain match must rank first even against a better scored category set: %+v %+v", results[0].Set.ID, results[0].Match)
	}
	if results[1].Set.ID != "category" || results[1].Match == nil || results[1].Match.Via != MatchViaCategory || results[1].Match.Entry != "youtube" {
		t.Errorf("the category set must match through its geosite category: %+v %+v", results[1].Set.ID, results[1].Match)
	}

	all, total := box.svc.Search("", 1)
	if total != 2 || len(all) != 1 {
		t.Errorf("without a domain every active set is listed and the limit applies: %d of %d", len(all), total)
	}
	if all[0].Set.ID != "category" || all[0].Display.Bucket != hubwire.BucketGlobal {
		t.Errorf("without a domain the best scored set leads: %s (%s)", all[0].Set.ID, all[0].Display.Bucket)
	}
	if _, total := box.svc.Search("example.org", 0); total != 0 {
		t.Errorf("an unrelated domain must match nothing")
	}
	if _, ok := box.svc.Get("pending"); !ok {
		t.Errorf("a pending set stays fetchable by id")
	}
}

func TestFetchBlobVerifiesTheHash(t *testing.T) {
	f := hubtest.New(t)
	box := newTestBox(t, f, t.TempDir())
	ref := f.AddBlob(hubwire.Payload{SHA256: hubwire.BlobHash(config.FakeSNI1), Protocol: hubwire.ProtocolTLS, Domain: "www.google.com", Size: len(config.FakeSNI1), Data: config.FakeSNI1})

	data, err := box.svc.FetchBlob(context.Background(), ref)
	if err != nil || string(data) != string(config.FakeSNI1) {
		t.Fatalf("the blob must round trip: %v", err)
	}

	f.ReplaceBlob(ref.SHA256, append([]byte("x"), config.FakeSNI1[1:]...))
	if _, err := box.svc.FetchBlob(context.Background(), ref); err == nil {
		t.Fatal("a blob that does not hash to its reference must be refused")
	}

	missing := ref
	missing.SHA256 = hubwire.BlobHash([]byte("nothing"))
	if _, err := box.svc.FetchBlob(context.Background(), missing); !errors.Is(err, ErrBlobNotFound) {
		t.Errorf("expected ErrBlobNotFound, got %v", err)
	}
	if _, err := box.svc.FetchBlob(context.Background(), hubwire.BlobRef{SHA256: "nope"}); !errors.Is(err, ErrBlobRef) {
		t.Errorf("expected ErrBlobRef, got %v", err)
	}
}

func TestSendOrQueueQueuesWhileTheHubIsDownAndFlushDrains(t *testing.T) {
	f := hubtest.New(t)
	box := newTestBox(t, f, t.TempDir())
	f.SetDown(true)

	vote, err := box.svc.Sign(hubwire.RecordVote, hubwire.VoteBody{SetID: "direct", Version: 1, FP: "ff", Kind: hubwire.VoteWorks})
	if err != nil {
		t.Fatal(err)
	}
	sent, queued, _, err := box.svc.SendOrQueue(context.Background(), vote)
	if err != nil || sent || !queued {
		t.Fatalf("a vote must be queued while the hub is down: sent=%v queued=%v err=%v", sent, queued, err)
	}
	if box.svc.OutboxCount() != 1 {
		t.Fatalf("outbox must hold the vote, got %d", box.svc.OutboxCount())
	}

	share, _ := box.svc.Sign(hubwire.RecordShare, hubwire.ShareBody{Envelope: hubwire.Envelope{Format: hubwire.Format, Set: map[string]interface{}{}}})
	if _, _, _, err := box.svc.SendOrQueue(context.Background(), share); !errors.Is(err, ErrShareNotQueued) {
		t.Errorf("a share is never queued, got %v", err)
	}

	box.svc.FlushOutbox(context.Background())
	if box.svc.OutboxCount() != 1 {
		t.Fatalf("flushing against a dead hub must keep the record")
	}

	f.SetDown(false)
	box.svc.FlushOutbox(context.Background())
	if box.svc.OutboxCount() != 0 {
		t.Fatalf("the outbox must drain once the hub answers, %d left", box.svc.OutboxCount())
	}
	records := f.Records()
	if len(records) != 1 || records[0].Kind != hubwire.RecordVote || records[0].ID() != vote.ID() {
		t.Errorf("the queued vote must reach the hub unchanged: %+v", records)
	}

	f.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: http.StatusForbidden, Body: map[string]string{"code": "banned"}}
	})
	sent, queued, _, err = box.svc.SendOrQueue(context.Background(), vote)
	var he *HubError
	if sent || queued || !errors.As(err, &he) || he.Code != "banned" || he.Status != http.StatusForbidden {
		t.Errorf("a refusal must come back as a HubError and never queue: sent=%v queued=%v err=%v", sent, queued, err)
	}
}

func TestIdentityRestoreRoundTrip(t *testing.T) {
	f := hubtest.New(t)
	first := newTestBox(t, f, t.TempDir())
	id, created, err := first.svc.Identity()
	if err != nil || created.IsZero() {
		t.Fatalf("identity must be created on first use: %v", err)
	}
	again, _, _ := first.svc.Identity()
	if again.KeyID() != id.KeyID() {
		t.Fatal("identity must be stable")
	}

	second := newTestBox(t, f, t.TempDir())
	other, _, _ := second.svc.Identity()
	if other.KeyID() == id.KeyID() {
		t.Fatal("a fresh directory gets its own identity")
	}
	restored, err := second.svc.RestoreIdentity(id.RecoveryCode())
	if err != nil || restored.KeyID() != id.KeyID() {
		t.Fatalf("restore must reproduce the key: %v", err)
	}
	if _, err := second.svc.RestoreIdentity("garbage"); !errors.Is(err, hubwire.ErrRecoveryCode) {
		t.Errorf("a malformed code must be refused, got %v", err)
	}
	reloaded := newTestBox(t, f, filepath.Dir(second.cfg.Load().ConfigPath))
	if persisted, _, _ := reloaded.svc.Identity(); persisted.KeyID() != id.KeyID() {
		t.Errorf("the restored identity must be persisted")
	}
}

func TestSchedulerFollowsTheEnabledSwitch(t *testing.T) {
	f := hubtest.New(t)
	f.Publish(t, sampleCatalogue(t, 1, 1), time.Now().Add(time.Hour))
	box := newTestBox(t, f, t.TempDir())
	box.update(func(cfg *config.Config) { cfg.System.Hub.Enabled = false })

	box.svc.Start()
	defer box.svc.Stop()

	box.svc.Kick()
	time.Sleep(100 * time.Millisecond)
	if box.svc.Status().LastSync != "" {
		t.Fatal("a disabled hub must never sync")
	}

	box.update(func(cfg *config.Config) { cfg.System.Hub.Enabled = true })
	box.svc.Kick()
	deadline := time.Now().Add(5 * time.Second)
	for box.svc.Status().LastSync == "" {
		if time.Now().After(deadline) {
			t.Fatal("the scheduler did not sync after the switch was flipped on")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if box.svc.Status().Catalogue == nil {
		t.Errorf("the kick must have fetched the catalogue")
	}
	box.svc.Stop()
	box.svc.Stop()
}

func TestBaseURLsNormalisation(t *testing.T) {
	f := hubtest.New(t)
	box := newTestBox(t, f, t.TempDir())
	box.update(func(cfg *config.Config) {
		cfg.System.Hub.URLs = []string{" https://one.example/ ", "http://plain.example", "https://user:pw@two.example", "https://one.example", "https://three.example/path?x=1", "https://four.example/base"}
	})
	box.svc.builtin = DefaultBases
	got := box.svc.BaseURLs()
	want := []string{"https://one.example", "https://four.example/base", DefaultBaseURL}
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("base %d = %q want %q", i, got[i], want[i])
		}
	}
}
