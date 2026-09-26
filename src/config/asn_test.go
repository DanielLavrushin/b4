package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
)

func newTestAsnStore(t *testing.T) *AsnStore {
	t.Helper()
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")
	return NewAsnStore(configPath)
}

func useTestAsnStore(t *testing.T) *AsnStore {
	t.Helper()
	prev := asnStore.Load()
	s := InitAsnStore(filepath.Join(t.TempDir(), "b4.json"))
	t.Cleanup(func() { asnStore.Store(prev) })
	return s
}

func mustPut(t *testing.T, s *AsnStore, info *AsnInfo) {
	t.Helper()
	if err := s.Put(info); err != nil {
		t.Fatalf("Put(%+v): %v", info, err)
	}
}

func TestAsnStore_PutAndGetAll(t *testing.T) {
	s := newTestAsnStore(t)

	mustPut(t, s, &AsnInfo{
		ID:        "AS13335",
		Name:      " Cloudflare ",
		Prefixes:  []string{"1.1.1.0/24", "1.0.0.0/24", "10.0.0.0/8", "0.0.0.0/0"},
		UpdatedAt: 1700000000,
		Source:    AsnSourceRIPEstat,
	})

	all := s.GetAll()
	if len(all) != 1 {
		t.Fatalf("expected 1 ASN, got %d", len(all))
	}
	got := all["13335"]
	if got == nil {
		t.Fatal("expected ASN 13335 under its canonical id")
	}
	want := &AsnInfo{ID: "13335", Name: "Cloudflare", Prefixes: []string{"1.0.0.0/24", "1.1.1.0/24"}, UpdatedAt: 1700000000, Source: AsnSourceRIPEstat}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("stored entry = %+v, want %+v", got, want)
	}
	if byAlias := s.Get("as13335"); !reflect.DeepEqual(byAlias, want) {
		t.Fatalf("Get(as13335) = %+v", byAlias)
	}
	if s.Get("15169") != nil || s.Get("garbage") != nil {
		t.Fatal("Get must return nil for unknown or malformed ids")
	}
}

func TestAsnStore_PutRejectsInvalidID(t *testing.T) {
	s := newTestAsnStore(t)
	for _, id := range []string{"", "0", "64512", "AS23456", "abc"} {
		if err := s.Put(&AsnInfo{ID: id, Prefixes: []string{"8.8.8.0/24"}}); err == nil {
			t.Errorf("Put accepted id %q", id)
		}
	}
	if err := s.Put(nil); err == nil {
		t.Error("Put accepted a nil entry")
	}
	if len(s.GetAll()) != 0 {
		t.Fatal("rejected entries must not be stored")
	}
}

func TestAsnStore_FindByIPLongestPrefix(t *testing.T) {
	s := newTestAsnStore(t)

	mustPut(t, s, &AsnInfo{ID: "3356", Name: "Transit", Prefixes: []string{"8.0.0.0/9", "2001:1900::/28"}})
	mustPut(t, s, &AsnInfo{ID: "15169", Name: "Google", Prefixes: []string{"8.8.8.0/24", "2001:4860::/32"}})
	mustPut(t, s, &AsnInfo{ID: "13335", Name: "Cloudflare", Prefixes: []string{"1.1.1.0/24", "104.16.0.0/12"}})

	tests := []struct {
		ip       string
		wantName string
	}{
		{"8.8.8.8", "Google"},
		{"8.8.4.4", "Transit"},
		{"8.8.8.8:443", "Google"},
		{"[8.8.8.8]", "Google"},
		{"1.1.1.1", "Cloudflare"},
		{"104.16.5.100", "Cloudflare"},
		{"2001:4860:4860::8888", "Google"},
		{"[2001:4860:4860::8888]:443", "Google"},
		{"2001:1900::1", "Transit"},
		{"::ffff:8.8.8.8", "Google"},
		{"192.168.1.1", ""},
		{"invalid", ""},
		{"", ""},
	}
	for _, tt := range tests {
		for i := 0; i < 5; i++ {
			result := s.FindByIP(tt.ip)
			if tt.wantName == "" {
				if result != nil {
					t.Fatalf("FindByIP(%q) = %+v, want nil", tt.ip, result)
				}
				continue
			}
			if result == nil || result.Name != tt.wantName {
				t.Fatalf("FindByIP(%q) = %+v, want %q", tt.ip, result, tt.wantName)
			}
		}
	}

	prefix, matches := s.LookupIP("8.8.8.8")
	if prefix != "8.8.8.0/24" || len(matches) != 1 || matches[0].ID != "15169" {
		t.Fatalf("LookupIP(8.8.8.8) = %q, %+v", prefix, matches)
	}
}

func TestAsnStore_LookupIPListsEveryOriginOfASharedPrefix(t *testing.T) {
	s := newTestAsnStore(t)
	mustPut(t, s, &AsnInfo{ID: "20940", Name: "B", Prefixes: []string{"23.0.0.0/12"}})
	mustPut(t, s, &AsnInfo{ID: "16625", Name: "A", Prefixes: []string{"23.0.0.0/12"}})

	prefix, matches := s.LookupIP("23.1.2.3")
	if prefix != "23.0.0.0/12" || len(matches) != 2 {
		t.Fatalf("LookupIP = %q, %+v", prefix, matches)
	}
	if matches[0].ID != "16625" || matches[1].ID != "20940" {
		t.Fatalf("shared origins must be ordered by AS number, got %s, %s", matches[0].ID, matches[1].ID)
	}
	if got := s.FindByIP("23.1.2.3"); got == nil || got.ID != "16625" {
		t.Fatalf("FindByIP must pick the lowest AS number of a shared prefix, got %+v", got)
	}
	if prefix, matches := s.LookupIP("9.9.9.9"); prefix != "" || matches != nil {
		t.Fatalf("an address outside every prefix must return nothing, got %q, %+v", prefix, matches)
	}
}

func TestAsnStore_IndexFollowsChanges(t *testing.T) {
	s := newTestAsnStore(t)
	mustPut(t, s, &AsnInfo{ID: "15169", Name: "Google", Prefixes: []string{"8.8.8.0/24"}})
	mustPut(t, s, &AsnInfo{ID: "15169", Name: "Google", Prefixes: []string{"8.8.4.0/24"}})

	if s.FindByIP("8.8.8.8") != nil {
		t.Fatal("a replaced entry must drop its old prefixes from the index")
	}
	if got := s.FindByIP("8.8.4.4"); got == nil || got.ID != "15169" {
		t.Fatalf("FindByIP(8.8.4.4) = %+v", got)
	}
}

func TestAsnStore_Delete(t *testing.T) {
	s := newTestAsnStore(t)

	mustPut(t, s, &AsnInfo{ID: "13335", Name: "Cloudflare", Prefixes: []string{"1.1.1.0/24"}})
	mustPut(t, s, &AsnInfo{ID: "15169", Name: "Google", Prefixes: []string{"8.8.8.0/24"}})

	if err := s.Delete("AS13335"); err != nil {
		t.Fatalf("Delete failed: %v", err)
	}
	if err := s.Delete("13335"); err != nil {
		t.Fatalf("deleting a missing entry must succeed, got %v", err)
	}
	if err := s.Delete("not-an-asn"); err == nil {
		t.Fatal("Delete must reject a malformed id")
	}

	all := s.GetAll()
	if len(all) != 1 || all["15169"] == nil {
		t.Fatalf("expected only Google to remain, got %v", all)
	}
	if s.FindByIP("1.1.1.1") != nil {
		t.Error("expected nil for deleted ASN IP")
	}

	reloaded := NewAsnStore(filepath.Join(filepath.Dir(s.Path()), "config.json"))
	if len(reloaded.GetAll()) != 1 {
		t.Fatalf("the delete must be persisted, got %v", reloaded.GetAll())
	}
}

func TestAsnStore_Persistence(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	s1 := NewAsnStore(configPath)
	mustPut(t, s1, &AsnInfo{ID: "13335", Name: "Cloudflare", Prefixes: []string{"1.1.1.0/24"}, UpdatedAt: 42, Source: AsnSourceRIPEstat})

	s2 := NewAsnStore(configPath)
	got := s2.Get("13335")
	want := &AsnInfo{ID: "13335", Name: "Cloudflare", Prefixes: []string{"1.1.1.0/24"}, UpdatedAt: 42, Source: AsnSourceRIPEstat}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("reloaded entry = %+v, want %+v", got, want)
	}
	if got := s2.FindByIP("1.1.1.1"); got == nil {
		t.Fatal("the index must be built on load")
	}
}

func TestAsnStore_WritesAtomically(t *testing.T) {
	dir := t.TempDir()
	s := NewAsnStore(filepath.Join(dir, "config.json"))
	mustPut(t, s, &AsnInfo{ID: "13335", Prefixes: []string{"1.1.1.0/24"}})

	first, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if first.Mode().Perm() != asnCacheFileMode {
		t.Fatalf("asn cache mode = %#o, want %#o", first.Mode().Perm(), asnCacheFileMode)
	}

	mustPut(t, s, &AsnInfo{ID: "15169", Prefixes: []string{"8.8.8.0/24"}})
	second, err := os.Stat(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	if first.Sys().(*syscall.Stat_t).Ino == second.Sys().(*syscall.Stat_t).Ino {
		t.Fatal("the cache was rewritten in place instead of being replaced")
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.Name() != AsnCacheFileName {
			t.Errorf("unexpected file left behind: %s", e.Name())
		}
	}

	var onDisk map[string]*AsnInfo
	raw, _ := os.ReadFile(s.Path())
	if err := json.Unmarshal(raw, &onDisk); err != nil || len(onDisk) != 2 {
		t.Fatalf("cache on disk = %s (%v)", raw, err)
	}
}

func TestAsnStore_GetAllReturnsCopy(t *testing.T) {
	s := newTestAsnStore(t)
	mustPut(t, s, &AsnInfo{ID: "1", Name: "Test", Prefixes: []string{"11.0.0.0/8"}})

	all := s.GetAll()
	all["1"].Name = "Modified"
	all["1"].Prefixes[0] = "12.0.0.0/8"

	fresh := s.GetAll()
	if fresh["1"].Name != "Test" || fresh["1"].Prefixes[0] != "11.0.0.0/8" {
		t.Error("GetAll should return a copy, not a reference")
	}
	one := s.Get("1")
	one.Prefixes[0] = "13.0.0.0/8"
	if s.Get("1").Prefixes[0] != "11.0.0.0/8" {
		t.Error("Get should return a copy, not a reference")
	}
}

func TestAsnStore_PutKeepsNoReferenceToTheCallersEntry(t *testing.T) {
	s := newTestAsnStore(t)
	info := &AsnInfo{ID: "1", Name: "Test", Prefixes: []string{"11.0.0.0/8"}}
	mustPut(t, s, info)
	info.Prefixes[0] = "12.0.0.0/8"
	info.Name = "Changed"
	if got := s.Get("1"); got.Name != "Test" || got.Prefixes[0] != "11.0.0.0/8" {
		t.Fatalf("the store shares memory with the caller: %+v", got)
	}
}

func TestAsnStore_FindByIPReturnsCopy(t *testing.T) {
	s := newTestAsnStore(t)
	mustPut(t, s, &AsnInfo{ID: "1", Name: "Test", Prefixes: []string{"11.0.0.0/8"}})

	result := s.FindByIP("11.0.0.1")
	result.Name = "Modified"

	result2 := s.FindByIP("11.0.0.1")
	if result2.Name != "Test" {
		t.Error("FindByIP should return a copy, not a reference")
	}
}

func TestAsnStore_EmptyFile(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.json")

	s := NewAsnStore(configPath)
	if len(s.GetAll()) != 0 {
		t.Errorf("expected empty store, got %d entries", len(s.GetAll()))
	}
	if result := s.FindByIP("1.1.1.1"); result != nil {
		t.Error("expected nil for empty store")
	}
	if _, err := os.Stat(filepath.Join(dir, AsnCacheFileName)); !os.IsNotExist(err) {
		t.Error("loading must not create the cache file")
	}
}

func TestAsnStore_CorruptFileIsMovedAside(t *testing.T) {
	dir := t.TempDir()
	asnPath := filepath.Join(dir, AsnCacheFileName)
	if err := os.WriteFile(asnPath, []byte("{invalid json"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(asnPath+".corrupt", []byte("older"), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewAsnStore(filepath.Join(dir, "config.json"))
	if len(s.GetAll()) != 0 {
		t.Errorf("expected empty store after corrupt file, got %d entries", len(s.GetAll()))
	}
	if _, err := os.Stat(asnPath); !os.IsNotExist(err) {
		t.Fatal("the corrupt cache must be moved away from its name")
	}
	older, _ := os.ReadFile(asnPath + ".corrupt")
	if string(older) != "older" {
		t.Fatalf("an existing .corrupt copy was overwritten: %q", older)
	}
	kept, err := os.ReadFile(asnPath + ".corrupt.1")
	if err != nil || string(kept) != "{invalid json" {
		t.Fatalf("the corrupt cache was not kept as .corrupt.1: %q, %v", kept, err)
	}

	mustPut(t, s, &AsnInfo{ID: "13335", Prefixes: []string{"1.1.1.0/24"}})
	if reloaded := NewAsnStore(filepath.Join(dir, "config.json")); reloaded.Get("13335") == nil {
		t.Fatal("the store must work normally after a corrupt file")
	}
}

func TestAsnStore_WrongShapeCountsAsCorrupt(t *testing.T) {
	dir := t.TempDir()
	asnPath := filepath.Join(dir, AsnCacheFileName)
	if err := os.WriteFile(asnPath, []byte(`[{"id":"13335"}]`), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewAsnStore(filepath.Join(dir, "config.json"))
	if len(s.GetAll()) != 0 {
		t.Fatal("an array is not a valid cache")
	}
	if _, err := os.Stat(asnPath + ".corrupt"); err != nil {
		t.Fatalf("the unreadable cache was not kept aside: %v", err)
	}
}

func TestAsnStore_LoadsLegacyBrowserCache(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
  "13335": {"id": "13335", "name": "AS13335 CLOUDFLARENET", "prefixes": ["1.1.1.0/24", "1.1.0.0/16", "1.0.0.0/24", "1.0.1.0/24", "2606:4700::/32", "2606:4700:10::/44", "10.0.0.0/8", "0.0.0.0/0", "junk"]},
  "AS15169": {"id": "AS15169", "name": "Google", "prefixes": ["8.8.8.0/24", "8.8.4.0/24", "8.8.8.8/32"]},
  "32934": {"name": "Facebook", "prefixes": ["157.240.0.0/17", "157.240.128.0/17"]},
  "64512": {"id": "64512", "name": "private", "prefixes": ["5.5.5.0/24"]},
  "junk": {"id": "junk", "name": "junk", "prefixes": ["6.6.6.0/24"]},
  "null-entry": null
}`
	if err := os.WriteFile(filepath.Join(dir, AsnCacheFileName), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}

	s := NewAsnStore(filepath.Join(dir, "config.json"))
	all := s.GetAll()
	if len(all) != 3 {
		t.Fatalf("expected 3 usable legacy entries, got %d: %v", len(all), all)
	}
	want := map[string][]string{
		"13335": {"1.0.0.0/23", "1.1.0.0/16", "2606:4700::/32"},
		"15169": {"8.8.4.0/24", "8.8.8.0/24"},
		"32934": {"157.240.0.0/16"},
	}
	for id, prefixes := range want {
		got := all[id]
		if got == nil {
			t.Fatalf("legacy entry %s missing", id)
		}
		if got.ID != id || !reflect.DeepEqual(got.Prefixes, prefixes) {
			t.Errorf("legacy entry %s = %+v, want prefixes %v", id, got, prefixes)
		}
		if got.UpdatedAt != 0 || got.Source != "" {
			t.Errorf("legacy entry %s must read as never fetched by the server: %+v", id, got)
		}
	}
	if all["13335"].Name != "AS13335 CLOUDFLARENET" {
		t.Errorf("legacy name lost: %q", all["13335"].Name)
	}
	if got := s.FindByIP("1.1.200.1"); got == nil || got.ID != "13335" {
		t.Fatalf("FindByIP over legacy data = %+v", got)
	}
	if s.FindByIP("10.1.1.1") != nil {
		t.Fatal("a legacy private prefix must not survive the load")
	}
}

func TestAsnStore_LegacyAliasesMergeIntoOneEntry(t *testing.T) {
	dir := t.TempDir()
	legacy := `{
  "AS15169": {"id": "AS15169", "name": "Google", "prefixes": ["8.8.8.0/24"]},
  "15169": {"id": "15169", "name": "", "prefixes": ["8.8.4.0/24"]},
  "13335": {"id": "13335", "name": "old", "prefixes": ["1.1.1.0/24"], "updated_at": 10},
  "as13335": {"id": "as13335", "name": "new", "prefixes": ["1.0.0.0/24"], "updated_at": 20}
}`
	if err := os.WriteFile(filepath.Join(dir, AsnCacheFileName), []byte(legacy), 0644); err != nil {
		t.Fatal(err)
	}
	s := NewAsnStore(filepath.Join(dir, "config.json"))
	google := s.Get("15169")
	if google == nil || google.Name != "Google" || !reflect.DeepEqual(google.Prefixes, []string{"8.8.4.0/24", "8.8.8.0/24"}) {
		t.Fatalf("aliases of one never-fetched ASN must merge, got %+v", google)
	}
	cf := s.Get("13335")
	if cf == nil || cf.Name != "new" || !reflect.DeepEqual(cf.Prefixes, []string{"1.0.0.0/24"}) {
		t.Fatalf("the newer of two aliases must win, got %+v", cf)
	}
}

func TestAsnStore_MemoryOnlyWithoutConfigPath(t *testing.T) {
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	s := NewAsnStore("")
	mustPut(t, s, &AsnInfo{ID: "13335", Prefixes: []string{"1.1.1.0/24"}})
	if s.Path() != "" {
		t.Fatalf("a store without a config path must not have a file, got %q", s.Path())
	}
	if _, err := os.Stat(filepath.Join(cwd, AsnCacheFileName)); err == nil {
		t.Fatal("a memory-only store wrote a cache into the working directory")
	}
	if s.FindByIP("1.1.1.1") == nil {
		t.Fatal("a memory-only store must still serve lookups")
	}
}

func TestAsnStore_Expand(t *testing.T) {
	s := newTestAsnStore(t)
	mustPut(t, s, &AsnInfo{ID: "15169", Prefixes: []string{"8.8.8.0/24", "8.8.4.0/24", "2001:4860::/32"}})
	mustPut(t, s, &AsnInfo{ID: "13335", Prefixes: []string{"1.1.1.0/24", "8.8.8.0/24", "2606:4700::/32"}})
	mustPut(t, s, &AsnInfo{ID: "32934", Name: "no prefixes yet"})

	cases := []struct {
		name           string
		ids            []string
		ipVersion      string
		wantPrefixes   []string
		wantUnresolved []string
	}{
		{"both families in id order with shared prefixes once", []string{"AS15169", "13335"}, "", []string{"8.8.4.0/24", "8.8.8.0/24", "2001:4860::/32", "1.1.1.0/24", "2606:4700::/32"}, nil},
		{"v4 only", []string{"15169", "13335"}, "4", []string{"8.8.4.0/24", "8.8.8.0/24", "1.1.1.0/24"}, nil},
		{"v6 only", []string{"15169", "13335"}, "6", []string{"2001:4860::/32", "2606:4700::/32"}, nil},
		{"unknown and empty entries are unresolved", []string{"15169", "65000", "32934", "174", "junk"}, "4", []string{"8.8.4.0/24", "8.8.8.0/24"}, []string{"65000", "32934", "174", "junk"}},
		{"repeated id counts once", []string{"15169", "as15169"}, "6", []string{"2001:4860::/32"}, nil},
		{"nothing requested", nil, "", nil, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prefixes, unresolved := s.Expand(tc.ids, tc.ipVersion)
			if !reflect.DeepEqual(prefixes, tc.wantPrefixes) {
				t.Errorf("prefixes = %v, want %v", prefixes, tc.wantPrefixes)
			}
			if !reflect.DeepEqual(unresolved, tc.wantUnresolved) {
				t.Errorf("unresolved = %v, want %v", unresolved, tc.wantUnresolved)
			}
		})
	}
}

func TestExpandASNsUsesTheSharedStore(t *testing.T) {
	s := useTestAsnStore(t)
	if Asns() != s {
		t.Fatal("Asns must return the store InitAsnStore created")
	}
	mustPut(t, s, &AsnInfo{ID: "15169", Prefixes: []string{"8.8.8.0/24", "2001:4860::/32"}})

	prefixes, unresolved := ExpandASNs([]string{"AS15169", "13335"}, "4")
	if !reflect.DeepEqual(prefixes, []string{"8.8.8.0/24"}) || !reflect.DeepEqual(unresolved, []string{"13335"}) {
		t.Fatalf("ExpandASNs = %v, %v", prefixes, unresolved)
	}
	if p, u := ExpandASNs(nil, ""); p != nil || u != nil {
		t.Fatalf("ExpandASNs(nil) = %v, %v", p, u)
	}
	if !strings.HasSuffix(s.Path(), AsnCacheFileName) {
		t.Fatalf("the shared store must live next to the config, got %q", s.Path())
	}
}

func TestAsnsIsNeverNil(t *testing.T) {
	prev := asnStore.Load()
	t.Cleanup(func() { asnStore.Store(prev) })
	asnStore.Store(nil)

	s := Asns()
	if s == nil {
		t.Fatal("Asns returned nil before InitAsnStore")
	}
	if Asns() != s {
		t.Fatal("Asns must keep returning the same fallback store")
	}
	if s.Path() != "" {
		t.Fatalf("the fallback store must not write anywhere, got %q", s.Path())
	}
}

func TestRequestASNRefreshCoalescesAndNeverBlocks(t *testing.T) {
	drain := func() int {
		n := 0
		for {
			select {
			case <-ASNRefreshRequests():
				n++
			default:
				return n
			}
		}
	}
	drain()
	for i := 0; i < 10; i++ {
		RequestASNRefresh()
	}
	if n := drain(); n != 1 {
		t.Fatalf("ten requests produced %d wake-ups, want 1", n)
	}
	if n := drain(); n != 0 {
		t.Fatalf("a drained channel still held %d wake-ups", n)
	}
}

func TestAsnStore_ConcurrentReadersAndWriters(t *testing.T) {
	s := newTestAsnStore(t)
	mustPut(t, s, &AsnInfo{ID: "15169", Prefixes: []string{"8.8.8.0/24"}})
	done := make(chan struct{})
	for w := 0; w < 4; w++ {
		go func(w int) {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 50; i++ {
				id := asnKey(uint32(1000 + w*100 + i))
				_ = s.Put(&AsnInfo{ID: id, Prefixes: []string{"11." + itoa(w) + "." + itoa(i) + ".0/24"}})
				if i%3 == 0 {
					_ = s.Delete(id)
				}
			}
		}(w)
	}
	for r := 0; r < 4; r++ {
		go func() {
			defer func() { done <- struct{}{} }()
			for i := 0; i < 200; i++ {
				if got := s.FindByIP("8.8.8.8"); got == nil || got.ID != "15169" {
					t.Errorf("FindByIP lost a stable entry: %+v", got)
					return
				}
				s.Expand([]string{"15169", "1000"}, "")
				s.GetAll()
			}
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}
	reloaded := NewAsnStore(filepath.Join(filepath.Dir(s.Path()), "config.json"))
	if !reflect.DeepEqual(reloaded.GetAll(), s.GetAll()) {
		t.Fatal("the file on disk does not match the store after concurrent writes")
	}
}
