package catalogue

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
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
	DefaultKeep     = 3
	DefaultInterval = 5 * time.Minute
	DefaultMaxAge   = 24 * time.Hour
)

var (
	catalogueFilePattern = regexp.MustCompile(`^catalogue-([0-9]+)-([0-9]+)\.json\.gz$`)

	ErrNotPublished = errors.New("no catalogue has been published yet")
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
	Now        func() time.Time
	Keep       int
	MaxAge     time.Duration
	OnBuild    func(*Result)

	mu     sync.Mutex
	latest atomic.Pointer[Result]
	kick   chan struct{}
	once   sync.Once
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

func (b *Builder) LoadPublished() error {
	raw, err := os.ReadFile(b.manifestPath())
	if errors.Is(err, os.ErrNotExist) {
		return ErrNotPublished
	}
	if err != nil {
		return err
	}
	var m hubwire.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return fmt.Errorf("published manifest does not decode: %w", err)
	}
	if !catalogueFilePattern.MatchString(m.Catalogue.File) {
		return fmt.Errorf("published manifest names %q", m.Catalogue.File)
	}
	gz, err := os.ReadFile(filepath.Join(b.PublicDir, m.Catalogue.File))
	if err != nil {
		return err
	}
	if hubwire.BlobHash(gz) != m.Catalogue.SHA256 {
		return fmt.Errorf("published %s does not match the manifest hash", m.Catalogue.File)
	}
	cat, err := Decode(gz)
	if err != nil {
		return err
	}
	b.latest.Store(indexResult(&m, cat))
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
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.Identity == nil {
		return nil, hubdata.ErrNoIdentity
	}
	now := b.now().UTC()
	versions, err := b.Store.ListedVersions(ctx)
	if err != nil {
		return nil, err
	}
	sets, err := b.Store.Sets(ctx)
	if err != nil {
		return nil, err
	}
	votesByFP, err := b.Store.VotesByFP(ctx)
	if err != nil {
		return nil, err
	}
	names, err := b.Store.ASNNames(ctx)
	if err != nil {
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
		GeoSources:   b.GeoSources,
		DoHAllowlist: hubwire.DoHAllowlist(),
	}
	if base := strings.TrimRight(strings.TrimSpace(b.PublicURL), "/"); base != "" {
		m.Mirrors = []string{base}
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
	b.prune(file)
	if err := b.Store.MarkBuilt(ctx, now); err != nil {
		return nil, err
	}
	result := indexResult(m, cat)
	b.latest.Store(result)
	if b.OnBuild != nil {
		b.OnBuild(result)
	}
	return result, nil
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

func (b *Builder) prune(current string) {
	entries, err := os.ReadDir(b.PublicDir)
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
		if f.name == current || kept < b.keep() {
			kept++
			continue
		}
		_ = os.Remove(filepath.Join(b.PublicDir, f.name))
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
	needed, err := b.NeedsBuild(ctx)
	if err != nil || !needed {
		return b.Latest(), false, err
	}
	result, err := b.Build(ctx)
	return result, err == nil, err
}

func (b *Builder) Kick() {
	b.init()
	select {
	case b.kick <- struct{}{}:
	default:
	}
}

func (b *Builder) init() {
	b.once.Do(func() { b.kick = make(chan struct{}, 1) })
}

func (b *Builder) Run(ctx context.Context, interval time.Duration) {
	b.init()
	if interval <= 0 {
		interval = DefaultInterval
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	b.tick(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.tick(ctx)
		case <-b.kick:
			b.tick(ctx)
		}
	}
}

func (b *Builder) tick(ctx context.Context) {
	result, built, err := b.BuildIfNeeded(ctx)
	if err != nil {
		log.Printf("catalogue: build failed: %v", err)
		return
	}
	if built {
		log.Printf("catalogue: published %s with %d sets", result.Manifest.Catalogue.File, len(result.Catalogue.Sets))
	}
}
