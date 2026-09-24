package config

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestSetDiscoveryDefaultsToAnEmptyList(t *testing.T) {
	a, b := NewSetConfig(), NewSetConfig()
	if a.Discovery.URLs == nil || len(a.Discovery.URLs) != 0 {
		t.Fatalf("a new set must carry an empty, non-nil URL list, got %#v", a.Discovery.URLs)
	}
	a.Discovery.URLs = append(a.Discovery.URLs, "https://example.com/")
	if len(b.Discovery.URLs) != 0 || len(DefaultSetConfig.Discovery.URLs) != 0 {
		t.Fatal("new sets must not share the default URL slice")
	}

	data, err := json.Marshal(b)
	if err != nil {
		t.Fatal(err)
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(raw["discovery"], map[string]any{"urls": []any{}, "watchdog": false}) {
		t.Errorf("the API shape must be discovery: {urls: [], watchdog: false}, got %v", raw["discovery"])
	}
}

func TestSetDiscoveryIsOmittedFromTheSparseConfigWhenEmpty(t *testing.T) {
	cfg := NewConfig()
	empty := NewSetConfig()
	empty.Id, empty.Name = "empty", "Empty"
	withURLs := NewSetConfig()
	withURLs.Id, withURLs.Name = "yt", "YouTube"
	withURLs.Discovery.URLs = []string{"https://www.youtube.com/"}
	cfg.Sets = []*SetConfig{&empty, &withURLs}

	data, err := MarshalSparse(&cfg)
	if err != nil {
		t.Fatal(err)
	}
	var raw struct {
		Sets []map[string]any `json:"sets"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, has := raw.Sets[0]["discovery"]; has {
		t.Errorf("an empty discovery block must be omitted, got %v", raw.Sets[0]["discovery"])
	}
	if !reflect.DeepEqual(raw.Sets[1]["discovery"], map[string]any{"urls": []any{"https://www.youtube.com/"}}) {
		t.Errorf("stored URLs must survive the sparse write, got %v", raw.Sets[1]["discovery"])
	}
}

func TestValidateSanitizesSetDiscoveryURLs(t *testing.T) {
	cfg := NewConfig()
	set := NewSetConfig()
	set.Id, set.Name = "yt", "YouTube"
	set.Discovery.URLs = []string{
		"www.YouTube.com",
		"https://www.youtube.com/watch",
		"http://192.168.1.1/",
		"ftp://example.com/",
		"https://user:pw@example.com/",
		"m.youtube.com/feed#top",
	}
	cfg.Sets = []*SetConfig{&set}

	if err := cfg.Validate(); err != nil {
		t.Fatalf("bad discovery URLs must never fail validation: %v", err)
	}
	want := []string{"https://www.youtube.com/", "https://m.youtube.com/feed"}
	if !reflect.DeepEqual(set.Discovery.URLs, want) {
		t.Errorf("got %v, want %v", set.Discovery.URLs, want)
	}

	var missing SetConfig
	missing.Id, missing.Name = "legacy", "Legacy"
	ApplySetDefaults(&missing)
	cfg.Sets = []*SetConfig{&missing}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	if missing.Discovery.URLs == nil {
		t.Error("a set loaded without the block must still carry an empty list")
	}
}

func TestResetToDefaultsKeepsDiscoveryURLs(t *testing.T) {
	set := NewSetConfig()
	set.Id, set.Name = "yt", "YouTube"
	set.Fragmentation.Strategy = "oob"
	urls := []string{"https://www.youtube.com/"}
	set.Discovery.URLs = urls

	set.ResetToDefaults()

	if set.Fragmentation.Strategy != DefaultSetConfig.Fragmentation.Strategy {
		t.Errorf("the strategy must reset, got %q", set.Fragmentation.Strategy)
	}
	if !reflect.DeepEqual(set.Discovery.URLs, urls) {
		t.Fatalf("the set's discovery URLs must survive a reset, got %v", set.Discovery.URLs)
	}
	urls[0] = "https://mutated.example/"
	if set.Discovery.URLs[0] != "https://www.youtube.com/" {
		t.Error("the reset set must own its URL slice")
	}
}
