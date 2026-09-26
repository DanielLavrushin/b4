package geodat

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/urlesistiana/v2dat/v2data"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func fourCategorySite(t *testing.T) ([]byte, []int) {
	t.Helper()
	list := &v2data.GeoSiteList{}
	var ends []int
	var total []byte
	for _, e := range []*v2data.GeoSite{
		{CountryCode: "FIRST", Domain: []*v2data.Domain{dom(v2data.Domain_Domain, "first.example"), dom(v2data.Domain_Full, "www.first.example")}},
		{CountryCode: "SECOND", Domain: []*v2data.Domain{dom(v2data.Domain_Domain, "second.example")}},
		{CountryCode: "THIRD", Domain: []*v2data.Domain{dom(v2data.Domain_Domain, "third.example"), dom(v2data.Domain_Domain, "third.test")}},
		{CountryCode: "FOURTH", Domain: []*v2data.Domain{dom(v2data.Domain_Domain, "fourth.example")}},
	} {
		list.Entry = append(list.Entry, e)
		b, err := proto.Marshal(&v2data.GeoSiteList{Entry: []*v2data.GeoSite{e}})
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		total = append(total, b...)
		ends = append(ends, len(total))
	}
	b, err := proto.Marshal(list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !slices.Equal(b, total) {
		t.Fatal("entries do not marshal independently")
	}
	return b, ends
}

func writeBytes(t *testing.T, name string, b []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	return path
}

func rawEntry(tag string, records ...[]byte) []byte {
	var body []byte
	body = protowire.AppendTag(body, 1, protowire.BytesType)
	body = protowire.AppendString(body, tag)
	for _, rec := range records {
		body = protowire.AppendTag(body, 2, protowire.BytesType)
		body = protowire.AppendBytes(body, rec)
	}
	var out []byte
	out = protowire.AppendTag(out, 1, protowire.BytesType)
	return protowire.AppendBytes(out, body)
}

func rawDomain(value string) []byte {
	var rec []byte
	rec = protowire.AppendTag(rec, 1, protowire.VarintType)
	rec = protowire.AppendVarint(rec, domainTypeDomain)
	rec = protowire.AppendTag(rec, 2, protowire.BytesType)
	return protowire.AppendString(rec, value)
}

func TestLoadDomainsByCategorySalvagesCategoriesBeforeTheCut(t *testing.T) {
	b, ends := fourCategorySite(t)
	path := writeBytes(t, "geosite.dat", b[:ends[1]+3])

	found, err := LoadDomainsByCategory(path, []string{"first", "THIRD", "fourth"})

	var damage *DamageError
	if !errors.As(err, &damage) || damage.Path != path {
		t.Fatalf("want a DamageError for %s, got %v", path, err)
	}
	if got := found["first"]; !slices.Equal(got, []string{"first.example", "www.first.example"}) {
		t.Fatalf("the category before the cut must load completely, got %v", got)
	}
	for _, category := range []string{"THIRD", "fourth"} {
		if _, ok := found[category]; ok {
			t.Fatalf("%s sits at or after the cut and must be reported as unread", category)
		}
	}
	if !IsDamaged(path) {
		t.Fatal("the file must be remembered as damaged")
	}
}

func TestLoadDomainsByCategoryKeepsCategoriesAroundADamagedRecord(t *testing.T) {
	broken := []byte{0x12, 0x05, 'a'}
	var b []byte
	b = append(b, rawEntry("FIRST", rawDomain("first.example"))...)
	b = append(b, rawEntry("SECOND", rawDomain("second.example"), broken)...)
	b = append(b, rawEntry("THIRD", rawDomain("third.example"))...)
	path := writeBytes(t, "geosite.dat", b)

	found, err := LoadDomainsByCategory(path, []string{"first", "second", "third"})

	var damage *DamageError
	if !errors.As(err, &damage) {
		t.Fatalf("want a DamageError, got %v", err)
	}
	if _, ok := found["second"]; ok {
		t.Fatalf("a category with a broken record must not be half loaded: %v", found["second"])
	}
	if !slices.Equal(found["first"], []string{"first.example"}) || !slices.Equal(found["third"], []string{"third.example"}) {
		t.Fatalf("the intact categories must load, got %v", found)
	}
}

func TestLoadDomainsByCategoryOnAnIntactFile(t *testing.T) {
	path := sampleGeoSite(t)

	found, err := LoadDomainsByCategory(path, []string{"Google@ads", "empty", "absent"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(found["Google@ads"]) != 4 {
		t.Fatalf("attributes are ignored as before, got %v", found["Google@ads"])
	}
	if _, ok := found["empty"]; !ok {
		t.Fatal("a present but empty category is found")
	}
	if _, ok := found["absent"]; ok {
		t.Fatal("an absent category is not found")
	}
	if IsDamaged(path) {
		t.Fatal("an intact file is not damaged")
	}
}

func TestLoadDomainsByCategoryWithoutTheFile(t *testing.T) {
	found, err := LoadDomainsByCategory(filepath.Join(t.TempDir(), "missing.dat"), []string{"google"})
	if err != nil || len(found) != 0 {
		t.Fatalf("a missing file is skipped without an error, got %v %v", found, err)
	}
}

func TestLoadIpsByCategory(t *testing.T) {
	path := writeGeoIP(t,
		&v2data.GeoIP{CountryCode: "TG", Cidr: []*v2data.CIDR{{Ip: []byte{91, 108, 4, 0}, Prefix: 22}}},
		&v2data.GeoIP{CountryCode: "LOCAL", Cidr: []*v2data.CIDR{{Ip: []byte{127, 0, 0, 0}, Prefix: 8}}},
	)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	truncated := writeBytes(t, "geoip.dat", b[:len(b)-3])

	found, err := LoadIpsByCategory(truncated, []string{"tg", "local"})
	if err == nil {
		t.Fatal("want an error for the truncated second entry")
	}
	if !slices.Equal(found["tg"], []string{"91.108.4.0/22"}) {
		t.Fatalf("the first category must load, got %v", found)
	}
	if _, ok := found["local"]; ok {
		t.Fatal("the truncated category must be unread")
	}
}

func TestEmptyFileIsDamaged(t *testing.T) {
	path := writeBytes(t, "geosite.dat", nil)

	_, err := LoadDomainsFromCategories(path, []string{"google"})
	if !errors.Is(err, ErrEmpty) {
		t.Fatalf("want ErrEmpty, got %v", err)
	}
	if !IsDamaged(path) {
		t.Fatal("an empty file must be remembered as damaged")
	}
}

func TestIsDamagedForgetsAReplacedFile(t *testing.T) {
	b, ends := fourCategorySite(t)
	path := writeBytes(t, "geosite.dat", b[:ends[0]+2])
	if _, err := LoadDomainsFromCategories(path, []string{"fourth"}); err == nil {
		t.Fatal("want an error")
	}
	if !IsDamaged(path) {
		t.Fatal("want damaged")
	}

	if err := os.WriteFile(path+".new", b, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(path+".new", path); err != nil {
		t.Fatal(err)
	}
	if IsDamaged(path) {
		t.Fatal("a replaced file is no longer damaged")
	}
}

func TestValidate(t *testing.T) {
	site := sampleGeoSite(t)
	ip := writeGeoIP(t, &v2data.GeoIP{CountryCode: "TG", Cidr: []*v2data.CIDR{{Ip: []byte{91, 108, 4, 0}, Prefix: 22}}})
	siteBytes, err := os.ReadFile(site)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		kind Kind
		ok   bool
	}{
		{"geosite", site, KindSite, true},
		{"geoip", ip, KindIP, true},
		{"geoip used as geosite", ip, KindSite, false},
		{"geosite used as geoip", site, KindIP, false},
		{"truncated", writeBytes(t, "t.dat", siteBytes[:len(siteBytes)/2]), KindSite, false},
		{"empty", writeBytes(t, "e.dat", nil), KindSite, false},
		{"html", writeBytes(t, "h.dat", []byte("<!DOCTYPE html><html><body>blocked</body></html>")), KindSite, false},
		{"lf html", writeBytes(t, "l.dat", []byte("\n<!DOCTYPE html>\n<html></html>")), KindSite, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := Validate(tc.path, tc.kind)
			if tc.ok && err != nil {
				t.Fatalf("want valid, got %v", err)
			}
			if !tc.ok && !errors.Is(err, ErrUnusable) {
				t.Fatalf("want ErrUnusable, got %v", err)
			}
		})
	}
}

func TestValidateDoesNotMarkTheFileDamaged(t *testing.T) {
	path := writeBytes(t, "h.dat", []byte("<html>"))
	if err := Validate(path, KindSite); err == nil {
		t.Fatal("want an error")
	}
	if IsDamaged(path) {
		t.Fatal("rejected downloads must not enter the damage registry")
	}
}

func TestPreloadCategoriesKeepsWhatItCouldRead(t *testing.T) {
	b, ends := fourCategorySite(t)
	path := writeBytes(t, "geosite.dat", b[:ends[1]+3])
	gm := NewGeodataManager(path, "")

	counts, err := gm.PreloadCategories(GEOSITE, []string{"first", "second", "third"})
	if err != nil {
		t.Fatalf("preload reports damage through the log only: %v", err)
	}
	if counts["first"] != 2 || counts["second"] != 1 || counts["third"] != 0 {
		t.Fatalf("counts = %v", counts)
	}
	gm.mu.RLock()
	_, cachedFirst := gm.categoryDomains["first"]
	_, cachedThird := gm.categoryDomains["third"]
	gm.mu.RUnlock()
	if !cachedFirst || cachedThird {
		t.Fatalf("only categories that were read are cached (first=%v third=%v)", cachedFirst, cachedThird)
	}
}

func TestRemoveStaleDownloads(t *testing.T) {
	dir := t.TempDir()
	old := time.Now().Add(-2 * staleDownloadAge)
	stale := []string{".download-1.tmp", ".geodat-upload-2.tmp", "geosite.dat.part", "geosite.dat.new", "geoip.dat.new.part"}
	for _, name := range stale {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	fresh := filepath.Join(dir, "geosite.dat.new.part")
	kept := []string{fresh, filepath.Join(dir, "geosite.dat"), filepath.Join(dir, "b4.json")}
	for _, p := range kept {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range kept[1:] {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	RemoveStaleDownloads(filepath.Join(dir, "geosite.dat"), filepath.Join(dir, "geoip.dat"), "relative.dat")

	for _, name := range stale {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Errorf("%s should be removed", name)
		}
	}
	for _, p := range kept {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s should be kept: %v", p, err)
		}
	}
}

func startedScheduler(t *testing.T, st GeoDatConfig, refresh RefreshFunc) *Scheduler {
	t.Helper()
	s := NewScheduler(func() GeoDatConfig { return st }, refresh, nil)
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.stop = make(chan struct{})
	t.Cleanup(s.cancel)
	return s
}

func TestStartupRedownloadsADamagedFile(t *testing.T) {
	b, ends := fourCategorySite(t)
	path := writeBytes(t, "geosite.dat", b[:ends[0]+2])
	if _, err := LoadDomainsByCategory(path, []string{"fourth"}); err == nil {
		t.Fatal("want an error")
	}

	calls := 0
	s := startedScheduler(t, GeoDatConfig{GeoSitePath: path, GeoSiteURL: "https://example.invalid/geosite.dat"}, func(_ context.Context, dest, siteURL, ipURL string) error {
		calls++
		if dest != filepath.Dir(path) || siteURL == "" || ipURL != "" {
			t.Errorf("refresh(%q, %q, %q)", dest, siteURL, ipURL)
		}
		return nil
	})
	s.runStartup()
	if calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls)
	}
}

func TestStartupRedownloadsAnEmptyFile(t *testing.T) {
	path := writeBytes(t, "geosite.dat", nil)
	calls := 0
	s := startedScheduler(t, GeoDatConfig{GeoSitePath: path, GeoSiteURL: "https://example.invalid/geosite.dat"}, func(context.Context, string, string, string) error {
		calls++
		return nil
	})
	s.runStartup()
	if calls != 1 {
		t.Fatalf("refresh calls = %d, want 1", calls)
	}
}

func TestStartupLeavesAnIntactFileAlone(t *testing.T) {
	path := sampleGeoSite(t)
	s := startedScheduler(t, GeoDatConfig{GeoSitePath: path, GeoSiteURL: "https://example.invalid/geosite.dat"}, func(context.Context, string, string, string) error {
		t.Error("an intact file must not be downloaded again")
		return nil
	})
	s.runStartup()
}

func TestStartupDoesNotRetryAnUnusableDownload(t *testing.T) {
	path := writeBytes(t, "geosite.dat", nil)
	calls := 0
	s := startedScheduler(t, GeoDatConfig{GeoSitePath: path, GeoSiteURL: "https://example.invalid/geosite.dat"}, func(context.Context, string, string, string) error {
		calls++
		return errors.Join(errors.New("failed to download geosite.dat"), ErrUnusable)
	})
	done := make(chan struct{})
	go func() {
		s.runStartup()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("an unusable download must not enter the 60s retry loop")
	}
	if calls != 1 || s.failures != 1 || s.lastFailure.IsZero() {
		t.Fatalf("calls=%d failures=%d lastFailure=%v", calls, s.failures, s.lastFailure)
	}
}

func TestScheduledTickHealsADamagedFileWithBackoff(t *testing.T) {
	path := writeBytes(t, "geosite.dat", nil)
	if _, err := LoadDomainsFromCategories(path, []string{"x"}); err == nil {
		t.Fatal("want an error")
	}
	calls := 0
	s := startedScheduler(t, GeoDatConfig{GeoSitePath: path, GeoSiteURL: "https://example.invalid/geosite.dat"}, func(context.Context, string, string, string) error {
		calls++
		return errors.New("offline")
	})

	s.runScheduled()
	s.runScheduled()
	if calls != 1 {
		t.Fatalf("a failed heal waits for the backoff, calls = %d", calls)
	}

	s.lastFailure = time.Now().Add(-failureBackoff - time.Minute)
	s.runScheduled()
	if calls != 2 {
		t.Fatalf("the heal is retried after the backoff, calls = %d", calls)
	}
	if got := s.backoff(); got != 2*failureBackoff {
		t.Fatalf("the backoff doubles after repeated failures, got %s", got)
	}
}

func TestSchedulerStopCancelsARunningRefresh(t *testing.T) {
	path := writeBytes(t, "geosite.dat", nil)

	started := make(chan struct{})
	s := NewScheduler(func() GeoDatConfig {
		return GeoDatConfig{GeoSitePath: path, GeoSiteURL: "https://example.invalid/geosite.dat"}
	}, func(ctx context.Context, _, _, _ string) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}, nil)
	s.ctx, s.cancel = context.WithCancel(context.Background())
	s.stop = make(chan struct{})
	s.stopped = make(chan struct{})
	go func() {
		defer close(s.stopped)
		s.runStartup()
	}()

	<-started
	done := make(chan struct{})
	go func() {
		s.Stop()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop must cancel the refresh instead of waiting for it")
	}
}
