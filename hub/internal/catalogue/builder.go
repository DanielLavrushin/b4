package catalogue

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	ManifestFile    = "manifest.json"
	lockFile        = ".build.lock"
	DefaultKeep     = 3
	DefaultInterval = 5 * time.Minute
	DefaultMaxAge   = 24 * time.Hour
	DefaultDebounce = 1500 * time.Millisecond
	MaxDebounce     = 10 * time.Second
	RequestPoll     = 10 * time.Second
	RescoreDelta    = 0.02
	maxChangeList   = 100

	BuildIdle     = "idle"
	BuildQueued   = "queued"
	BuildBuilding = "building"

	TriggerManual   = "manual"
	TriggerSchedule = "schedule"
	TriggerStartup  = "startup"
	TriggerCLI      = "cli"
	TriggerMirrors  = "mirrors"
	TriggerAutoHide = "autohide"
)

var (
	catalogueFilePattern = regexp.MustCompile(`^catalogue-([0-9]+)-([0-9]+)\.json\.gz$`)

	ErrNotPublished = errors.New("no catalogue has been published yet")
	ErrNewDatabase  = errors.New("this database has never published a catalogue")
	ErrBehind       = errors.New("the database is behind the catalogue the network already has")
)

type Result struct {
	Manifest  *hubwire.Manifest
	Catalogue *hubwire.Catalogue
	ByID      map[string]*hubwire.CatalogueSet
}

type Builder struct {
	Store      *store.Store
	Identity   *hubwire.Identity
	PublicDir  string
	PublicURL  string
	GeoSources []hubwire.GeoSource
	Mirrors    *MirrorHealth
	Now        func() time.Time
	Keep       int
	MaxAge     time.Duration
	Debounce   time.Duration
	OnBuild    func(*Result)

	BuiltinKey  bool
	NewDatabase bool

	mu     sync.Mutex
	latest atomic.Pointer[Result]
	kick   chan struct{}
	once   sync.Once

	smu      sync.Mutex
	state    string
	trigger  string
	force    bool
	pending  bool
	next     string
	queuedAt time.Time
	started  time.Time
	lastOK   *store.BuildRun
	lastFail *store.BuildRun
	loaded   bool
}

type BuildStatus struct {
	State     string
	Trigger   string
	QueuedAt  time.Time
	StartedAt time.Time
	LastOK    *store.BuildRun
	LastError *store.BuildRun
}

func (b *Builder) now() time.Time {
	if b.Now != nil {
		return b.Now()
	}
	return time.Now()
}

func (b *Builder) keep() int {
	if b.Keep <= 0 {
		return DefaultKeep
	}
	return b.Keep
}

func (b *Builder) maxAge() time.Duration {
	if b.MaxAge <= 0 {
		return DefaultMaxAge
	}
	return b.MaxAge
}

func (b *Builder) Latest() *Result {
	return b.latest.Load()
}

func (b *Builder) manifestPath() string {
	return filepath.Join(b.PublicDir, ManifestFile)
}

func indexResult(m *hubwire.Manifest, cat *hubwire.Catalogue) *Result {
	byID := make(map[string]*hubwire.CatalogueSet, len(cat.Sets))
	for i := range cat.Sets {
		byID[cat.Sets[i].ID] = &cat.Sets[i]
	}
	return &Result{Manifest: m, Catalogue: cat, ByID: byID}
}

func ValidFileName(name string) bool {
	return catalogueFilePattern.MatchString(name)
}

func ReadPublished(dir string) (*Result, error) {
	raw, err := os.ReadFile(filepath.Join(dir, ManifestFile))
	if errors.Is(err, os.ErrNotExist) {
		return nil, ErrNotPublished
	}
	if err != nil {
		return nil, err
	}
	var m hubwire.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, fmt.Errorf("published manifest does not decode: %w", err)
	}
	if !ValidFileName(m.Catalogue.File) {
		return nil, fmt.Errorf("published manifest names %q", m.Catalogue.File)
	}
	gz, err := os.ReadFile(filepath.Join(dir, m.Catalogue.File))
	if err != nil {
		return nil, err
	}
	if hubwire.BlobHash(gz) != m.Catalogue.SHA256 {
		return nil, fmt.Errorf("published %s does not match the manifest hash", m.Catalogue.File)
	}
	cat, err := Decode(gz)
	if err != nil {
		return nil, err
	}
	return indexResult(&m, cat), nil
}

func (b *Builder) LoadPublished() error {
	result, err := ReadPublished(b.PublicDir)
	if err != nil {
		return err
	}
	b.latest.Store(result)
	return nil
}

func Decode(gz []byte) (*hubwire.Catalogue, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("catalogue is not gzip: %w", err)
	}
	defer zr.Close()
	var cat hubwire.Catalogue
	if err := json.NewDecoder(zr).Decode(&cat); err != nil {
		return nil, fmt.Errorf("catalogue does not decode: %w", err)
	}
	return &cat, nil
}

func (b *Builder) HubBase() string {
	return strings.TrimRight(strings.TrimSpace(b.PublicURL), "/")
}

func (b *Builder) catalogueSet(v *store.Version, set store.Set, votes []store.Vote, now time.Time) hubwire.CatalogueSet {
	scoreVotes := make([]score.Vote, 0, len(votes))
	for _, vote := range votes {
		scoreVotes = append(scoreVotes, vote.ScoreVote())
	}
	author := set.AuthorHMAC
	if author == "" {
		author = v.UploaderHMAC
	}
	cs := hubwire.CatalogueSet{
		ID:          v.SetID,
		Version:     v.Version,
		FP:          v.FP,
		Title:       v.Title,
		Description: v.Description,
		Author:      hubdata.AuthorLabel(author),
		B4Min:       v.B4Min,
		B4Version:   v.B4Version,
		Engine:      v.Engine,
		Family:      v.Family,
		Flags:       v.Flags,
		Status:      hubwire.SetStatusActive,
		CreatedAt:   score.DayStamp(v.CreatedAt),
		UpdatedAt:   score.DayStamp(v.UpdatedAt),
		Geo:         v.Geo,
		Set:         v.Projection,
		Payloads:    v.Payloads,
		Scores:      score.Aggregate(scoreVotes, now),
	}
	if len(cs.Payloads) == 0 {
		cs.Payloads = nil
	}
	if len(cs.Flags) == 0 {
		cs.Flags = nil
	}
	if set.DerivedFromID != "" {
		cs.DerivedFrom = &hubwire.Origin{ID: set.DerivedFromID, Version: set.DerivedFromVersion}
	}
	return cs
}

func (b *Builder) Build(ctx context.Context) (*Result, error) {
	return b.BuildFor(ctx, TriggerManual)
}

func (b *Builder) BuildFor(ctx context.Context, trigger string) (*Result, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	started := b.now().UTC()
	b.markBuilding(trigger, started)
	run := store.BuildRun{Trigger: trigger, StartedAt: started}
	if id, err := b.Store.StartBuild(ctx, trigger, started); err == nil {
		run.ID = id
	} else {
		log.Printf("catalogue: build history: %v", err)
	}
	prev, result, err := b.publishLocked(ctx, started)
	run.FinishedAt = b.now().UTC()
	run.DurationMs = run.FinishedAt.Sub(started).Milliseconds()
	if err != nil {
		run.Error = err.Error()
	} else {
		run.OK = true
		run.Epoch = result.Manifest.Epoch
		run.Seq = result.Manifest.Seq
		run.File = result.Manifest.Catalogue.File
		run.Size = result.Manifest.Catalogue.Size
		run.Sets = len(result.Catalogue.Sets)
		run.Blobs = len(result.Catalogue.Blobs)
		run.Mirrors = len(result.Manifest.Mirrors)
		run.Changes = Diff(prev, result)
	}
	if run.ID != 0 {
		if err := b.Store.FinishBuild(ctx, run); err != nil {
			log.Printf("catalogue: build history: %v", err)
		}
	}
	b.markFinished(run)
	return result, err
}

func (b *Builder) publishLocked(ctx context.Context, started time.Time) (*Result, *Result, error) {
	if b.Identity == nil {
		return nil, nil, hubdata.ErrNoIdentity
	}
	unlock, err := lockPublish(b.PublicDir)
	if err != nil {
		return nil, nil, err
	}
	defer unlock()
	b.adoptNewerPublished()
	prev := b.latest.Load()
	result, err := b.publish(ctx, started)
	return prev, result, err
}

func (b *Builder) refreshPublished() {
	if !b.mu.TryLock() {
		return
	}
	defer b.mu.Unlock()
	b.adoptNewerPublished()
}

func (b *Builder) adoptNewerPublished() {
	raw, err := os.ReadFile(b.manifestPath())
	if err != nil {
		return
	}
	var m hubwire.Manifest
	if json.Unmarshal(raw, &m) != nil {
		return
	}
	current := b.latest.Load()
	if current != nil && !m.Newer(current.Manifest) {
		return
	}
	if current == nil && m.Seq == 0 {
		return
	}
	if result, err := ReadPublished(b.PublicDir); err == nil {
		b.latest.Store(result)
	}
}

func (b *Builder) publish(ctx context.Context, now time.Time) (*Result, error) {
	generation, err := b.Store.DirtyGeneration(ctx)
	if err != nil {
		return nil, err
	}
	versions, err := b.Store.CatalogueVersions(ctx)
	if err != nil {
		return nil, err
	}
	sets, err := b.Store.Sets(ctx)
	if err != nil {
		return nil, err
	}
	votesByFP, err := b.Store.ScoringVotesByFP(ctx)
	if err != nil {
		return nil, err
	}
	names, err := b.Store.ASNNames(ctx)
	if err != nil {
		return nil, err
	}
	mirrors, err := b.mirrors(ctx)
	if err != nil {
		return nil, err
	}
	revoked, err := b.Store.RevokedKeys(ctx)
	if err != nil {
		return nil, err
	}
	if err := b.guard(ctx, now, len(versions)); err != nil {
		return nil, err
	}
	epoch, seq, err := b.Store.NextSeq(ctx, now)
	if err != nil {
		return nil, err
	}

	cat := &hubwire.Catalogue{
		Epoch:       epoch,
		Seq:         seq,
		GeneratedAt: now.Format(time.RFC3339),
		Sets:        make([]hubwire.CatalogueSet, 0, len(versions)),
	}
	blobs := make(map[string]hubwire.BlobRef)
	usedASNs := make(map[string]struct{})
	for i := range versions {
		v := &versions[i]
		cs := b.catalogueSet(v, sets[v.SetID], votesByFP[v.FP], now)
		for _, ref := range cs.Payloads {
			blobs[ref.SHA256] = ref
		}
		for asn := range cs.Scores.ASN {
			usedASNs[asn] = struct{}{}
		}
		cat.Sets = append(cat.Sets, cs)
	}
	sort.Slice(cat.Sets, func(i, j int) bool { return cat.Sets[i].ID < cat.Sets[j].ID })
	if len(blobs) > 0 {
		cat.Blobs = make([]hubwire.BlobRef, 0, len(blobs))
		for _, ref := range blobs {
			cat.Blobs = append(cat.Blobs, ref)
		}
		sort.Slice(cat.Blobs, func(i, j int) bool { return cat.Blobs[i].SHA256 < cat.Blobs[j].SHA256 })
	}
	for asn := range usedASNs {
		if name, ok := names[asn]; ok {
			if cat.ASNNames == nil {
				cat.ASNNames = make(map[string]string)
			}
			cat.ASNNames[asn] = name
		}
	}

	gz, err := encode(cat)
	if err != nil {
		return nil, err
	}
	file := hubwire.CatalogueFileName(epoch, seq)
	if err := os.MkdirAll(b.PublicDir, 0o755); err != nil {
		return nil, err
	}
	if err := hubdata.WriteFileAtomic(filepath.Join(b.PublicDir, file), gz, 0o644); err != nil {
		return nil, err
	}
	m := &hubwire.Manifest{
		Epoch:        epoch,
		Seq:          seq,
		GeneratedAt:  cat.GeneratedAt,
		ExpiresAt:    now.Add(hubwire.ManifestTTL).Format(time.RFC3339),
		Catalogue:    hubwire.FileRef{File: file, SHA256: hubwire.BlobHash(gz), Size: int64(len(gz))},
		Mirrors:      mirrors,
		GeoSources:   b.GeoSources,
		DoHAllowlist: hubwire.DoHAllowlist(),
		RevokedKeys:  revoked,
	}
	if err := hubwire.SignManifest(m, b.Identity); err != nil {
		return nil, err
	}
	rawManifest, err := json.Marshal(m)
	if err != nil {
		return nil, err
	}
	if err := hubdata.WriteFileAtomic(b.manifestPath(), rawManifest, 0o644); err != nil {
		return nil, err
	}
	result := indexResult(m, cat)
	b.latest.Store(result)
	b.prune(file)
	if err := b.Store.MarkPublished(ctx, now, generation); err != nil {
		log.Printf("catalogue: published %s but could not record it: %v", file, err)
	}
	if b.OnBuild != nil {
		b.OnBuild(result)
	}
	return result, nil
}

func (b *Builder) guard(ctx context.Context, now time.Time, sets int) error {
	latest := b.latest.Load()
	if !b.NewDatabase {
		builtAt, err := b.Store.BuiltAt(ctx)
		if err != nil {
			return err
		}
		if builtAt.IsZero() {
			if latest != nil {
				return fmt.Errorf("%w, and its first build would replace catalogue %s with %d sets already published; restore the hub's database, or confirm a new one with --new-database", ErrNewDatabase, latest.Manifest.Catalogue.File, len(latest.Catalogue.Sets))
			}
			if b.BuiltinKey && sets == 0 {
				return fmt.Errorf("%w, and its first build would sign an empty catalogue with a key built into b4; restore the hub's database, or confirm a new one with --new-database", ErrNewDatabase)
			}
		}
	}
	epoch, seq, err := b.Store.CurrentSeq(ctx)
	if err != nil {
		return err
	}
	if epoch == 0 {
		epoch = now.Unix()
	}
	seq++
	known, where, err := b.newestKnown(ctx, latest)
	if err != nil {
		return err
	}
	if known != nil && (epoch < known.Epoch || (epoch == known.Epoch && seq <= known.Seq)) {
		return fmt.Errorf("%w: this build would be %d-%d while %s already has %d-%d, and a catalogue number is never reused or lowered; restore the newer database, or start a new epoch to publish this one", ErrBehind, epoch, seq, where, known.Epoch, known.Seq)
	}
	return nil
}

func (b *Builder) newestKnown(ctx context.Context, latest *Result) (*hubwire.Manifest, string, error) {
	var newest *hubwire.Manifest
	where := ""
	if latest != nil {
		newest, where = latest.Manifest, "the published directory"
	}
	mirrors, err := b.Store.MirrorsByStatus(ctx, store.MirrorApproved)
	if err != nil {
		return nil, "", err
	}
	for _, m := range mirrors {
		if m.ServedEpoch == 0 {
			continue
		}
		served := &hubwire.Manifest{Epoch: m.ServedEpoch, Seq: m.ServedSeq, GeneratedAt: m.ServedGeneratedAt}
		if newest == nil || served.Newer(newest) {
			newest, where = served, "mirror "+m.URL
		}
	}
	return newest, where, nil
}

func Diff(prev, next *Result) store.BuildChanges {
	var c store.BuildChanges
	if next == nil {
		return c
	}
	before := map[string]*hubwire.CatalogueSet{}
	if prev != nil {
		before = prev.ByID
	}
	for i := range next.Catalogue.Sets {
		cur := &next.Catalogue.Sets[i]
		old, ok := before[cur.ID]
		switch {
		case !ok:
			c.Added = appendCapped(c.Added, store.BuildSetRef{SetID: cur.ID, Version: cur.Version, Title: cur.Title})
		case old.Version != cur.Version:
			if len(c.Updated) < maxChangeList {
				c.Updated = append(c.Updated, store.BuildVersionChange{SetID: cur.ID, Title: cur.Title, From: old.Version, To: cur.Version})
			}
		case old.Title != cur.Title || old.Description != cur.Description:
			c.Edited = appendCapped(c.Edited, store.BuildSetRef{SetID: cur.ID, Version: cur.Version, Title: cur.Title})
		case math.Abs(old.Scores.Global.Score-cur.Scores.Global.Score) >= RescoreDelta || old.Scores.Global.Devices != cur.Scores.Global.Devices:
			c.Rescored++
		}
	}
	for id, old := range before {
		if _, ok := next.ByID[id]; !ok {
			c.Removed = appendCapped(c.Removed, store.BuildSetRef{SetID: id, Version: old.Version, Title: old.Title})
		}
	}
	sort.Slice(c.Removed, func(i, j int) bool { return c.Removed[i].SetID < c.Removed[j].SetID })
	var prevMirrors, prevRevoked []string
	if prev != nil {
		prevMirrors = prev.Manifest.Mirrors
		prevRevoked = prev.Manifest.RevokedKeys
	}
	c.MirrorsAdded, c.MirrorsRemoved = setDiff(prevMirrors, next.Manifest.Mirrors)
	c.RevokedAdded, _ = setDiff(prevRevoked, next.Manifest.RevokedKeys)
	return c
}

func appendCapped(list []store.BuildSetRef, ref store.BuildSetRef) []store.BuildSetRef {
	if len(list) >= maxChangeList {
		return list
	}
	return append(list, ref)
}

func setDiff(before, after []string) ([]string, []string) {
	had := make(map[string]bool, len(before))
	for _, v := range before {
		had[v] = true
	}
	has := make(map[string]bool, len(after))
	var added, removed []string
	for _, v := range after {
		has[v] = true
		if !had[v] {
			added = append(added, v)
		}
	}
	for _, v := range before {
		if !has[v] {
			removed = append(removed, v)
		}
	}
	return added, removed
}

func encode(cat *hubwire.Catalogue) ([]byte, error) {
	var buf bytes.Buffer
	zw, err := gzip.NewWriterLevel(&buf, gzip.BestCompression)
	if err != nil {
		return nil, err
	}
	if err := json.NewEncoder(zw).Encode(cat); err != nil {
		return nil, err
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

type publishedFile struct {
	name  string
	epoch int64
	seq   int64
}

func (b *Builder) MirrorsDrift(ctx context.Context) (bool, error) {
	if b.Mirrors == nil {
		return false, nil
	}
	want, err := b.listedMirrors(ctx)
	if err != nil {
		return false, err
	}
	base := b.HubBase()
	latest := b.Latest()
	if latest == nil {
		return len(want) > 0, nil
	}
	have := make([]string, 0, len(latest.Manifest.Mirrors))
	for _, u := range latest.Manifest.Mirrors {
		if u != base {
			have = append(have, u)
		}
	}
	added, removed := setDiff(have, want)
	return len(added) > 0 || len(removed) > 0, nil
}

func (b *Builder) mirrors(ctx context.Context) ([]string, error) {
	out := make([]string, 0, 1)
	if base := b.HubBase(); base != "" {
		out = append(out, base)
	}
	listed, err := b.listedMirrors(ctx)
	if err != nil {
		return nil, err
	}
	out = append(out, listed...)
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func (b *Builder) ListedMirrors(ctx context.Context) ([]string, error) {
	return b.listedMirrors(ctx)
}

func (b *Builder) listedMirrors(ctx context.Context) ([]string, error) {
	if b.Mirrors == nil {
		return nil, nil
	}
	base := b.HubBase()
	healthy, err := b.Mirrors.Announceable(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(healthy))
	for _, u := range healthy {
		if u != base {
			out = append(out, u)
		}
	}
	latest := b.Latest()
	if len(out) > 0 || latest == nil {
		return out, nil
	}
	approved, err := b.Store.MirrorsByStatus(ctx, store.MirrorApproved)
	if err != nil {
		return nil, err
	}
	still := make(map[string]bool, len(approved))
	for _, m := range approved {
		still[m.URL] = true
	}
	for _, u := range latest.Manifest.Mirrors {
		if u != base && still[u] {
			out = append(out, u)
		}
	}
	return out, nil
}

func (b *Builder) prune(current string) {
	Prune(b.PublicDir, current, b.keep())
}

func Prune(dir, current string, keep int) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	files := make([]publishedFile, 0, len(entries))
	for _, e := range entries {
		match := catalogueFilePattern.FindStringSubmatch(e.Name())
		if match == nil {
			continue
		}
		epoch, _ := strconv.ParseInt(match[1], 10, 64)
		seq, _ := strconv.ParseInt(match[2], 10, 64)
		files = append(files, publishedFile{name: e.Name(), epoch: epoch, seq: seq})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].epoch != files[j].epoch {
			return files[i].epoch > files[j].epoch
		}
		return files[i].seq > files[j].seq
	})
	kept := 0
	for _, f := range files {
		if f.name == current || kept < keep {
			kept++
			continue
		}
		_ = os.Remove(filepath.Join(dir, f.name))
	}
}

func (b *Builder) NeedsBuild(ctx context.Context) (bool, error) {
	if b.Latest() == nil {
		return true, nil
	}
	dirty, err := b.Store.Dirty(ctx)
	if err != nil {
		return false, err
	}
	if dirty {
		return true, nil
	}
	builtAt, err := b.Store.BuiltAt(ctx)
	if err != nil {
		return false, err
	}
	return builtAt.IsZero() || b.now().Sub(builtAt) >= b.maxAge(), nil
}

func (b *Builder) BuildIfNeeded(ctx context.Context) (*Result, bool, error) {
	return b.buildIfNeeded(ctx, TriggerSchedule, false)
}

func (b *Builder) buildIfNeeded(ctx context.Context, trigger string, force bool) (*Result, bool, error) {
	if !force {
		needed, err := b.NeedsBuild(ctx)
		if err != nil || !needed {
			b.markIdle()
			return b.Latest(), false, err
		}
	}
	result, err := b.BuildFor(ctx, trigger)
	return result, err == nil, err
}

func (b *Builder) Kick() {
	b.init()
	select {
	case b.kick <- struct{}{}:
	default:
	}
}

func (b *Builder) Request(trigger string) {
	b.request(trigger, false)
}

func (b *Builder) RequestForced(trigger string) {
	b.request(trigger, true)
}

func (b *Builder) request(trigger string, force bool) {
	b.smu.Lock()
	b.force = b.force || force
	switch b.state {
	case BuildBuilding:
		b.pending = true
		b.next = trigger
	case BuildQueued:
		b.trigger = trigger
	default:
		b.state = BuildQueued
		b.trigger = trigger
		b.queuedAt = b.now().UTC()
	}
	b.smu.Unlock()
	b.Kick()
}

func (b *Builder) takeRequest() (string, bool) {
	b.smu.Lock()
	defer b.smu.Unlock()
	trigger, force := b.trigger, b.force
	b.force = false
	if trigger == "" {
		trigger = TriggerSchedule
	}
	return trigger, force
}

func (b *Builder) markBuilding(trigger string, at time.Time) {
	b.smu.Lock()
	b.state = BuildBuilding
	b.trigger = trigger
	b.started = at
	b.queuedAt = time.Time{}
	b.smu.Unlock()
}

func (b *Builder) markFinished(run store.BuildRun) {
	b.smu.Lock()
	defer b.smu.Unlock()
	finished := run
	if run.OK {
		b.lastOK = &finished
	} else {
		b.lastFail = &finished
	}
	b.loaded = true
	b.started = time.Time{}
	switch {
	case b.pending:
		b.state = BuildQueued
		b.trigger = b.next
		b.queuedAt = b.now().UTC()
		b.pending = false
		b.next = ""
	case b.state == BuildBuilding:
		b.state = BuildIdle
		b.trigger = ""
	}
}

func (b *Builder) markIdle() {
	b.smu.Lock()
	if b.state == BuildQueued {
		b.state = BuildIdle
		b.trigger = ""
	}
	b.smu.Unlock()
}

func (b *Builder) LoadHistory(ctx context.Context) {
	ok, err := b.Store.LastBuild(ctx, true)
	if err != nil {
		log.Printf("catalogue: build history: %v", err)
		return
	}
	failed, err := b.Store.LastBuild(ctx, false)
	if err != nil {
		log.Printf("catalogue: build history: %v", err)
		return
	}
	b.smu.Lock()
	defer b.smu.Unlock()
	if b.loaded {
		return
	}
	b.lastOK, b.lastFail, b.loaded = ok, failed, true
}

func (b *Builder) Status() BuildStatus {
	b.smu.Lock()
	defer b.smu.Unlock()
	st := BuildStatus{State: b.state, Trigger: b.trigger, QueuedAt: b.queuedAt, StartedAt: b.started, LastOK: b.lastOK}
	if st.State == "" {
		st.State = BuildIdle
	}
	if st.State == BuildIdle {
		st.Trigger = ""
		st.QueuedAt = time.Time{}
	}
	if b.lastFail != nil && (b.lastOK == nil || b.lastFail.ID > b.lastOK.ID) {
		st.LastError = b.lastFail
	}
	return st
}

func (b *Builder) init() {
	b.once.Do(func() { b.kick = make(chan struct{}, 1) })
}

func (b *Builder) debounce() time.Duration {
	if b.Debounce <= 0 {
		return DefaultDebounce
	}
	return b.Debounce
}

func (b *Builder) settle(ctx context.Context) {
	wait := time.NewTimer(b.debounce())
	defer wait.Stop()
	deadline := time.NewTimer(MaxDebounce)
	defer deadline.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-deadline.C:
			return
		case <-wait.C:
			return
		case <-b.kick:
			if !wait.Stop() {
				<-wait.C
			}
			wait.Reset(b.debounce())
		}
	}
}

func (b *Builder) Run(ctx context.Context, interval time.Duration) {
	b.init()
	if interval <= 0 {
		interval = DefaultInterval
	}
	b.LoadHistory(ctx)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	requests := time.NewTicker(RequestPoll)
	defer requests.Stop()
	if b.Mirrors != nil {
		if _, err := b.Mirrors.CheckIfStale(ctx, DefaultMirrorCheckInterval); err != nil {
			log.Printf("catalogue: mirror check before the first build: %v", err)
		}
	}
	b.tick(ctx, TriggerStartup, false)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.tick(ctx, TriggerSchedule, false)
		case <-requests.C:
			b.refreshPublished()
			if taken, err := b.Store.TakeBuildRequest(ctx); err == nil && taken {
				b.Request(TriggerCLI)
			}
		case <-b.kick:
			b.settle(ctx)
			trigger, force := b.takeRequest()
			b.tick(ctx, trigger, force)
		}
	}
}

func (b *Builder) tick(ctx context.Context, trigger string, force bool) {
	result, built, err := b.buildIfNeeded(ctx, trigger, force)
	if err != nil {
		log.Printf("catalogue: build failed: %v", err)
		return
	}
	if built {
		log.Printf("catalogue: published %s with %d sets (%s)", result.Manifest.Catalogue.File, len(result.Catalogue.Sets), trigger)
	}
}
