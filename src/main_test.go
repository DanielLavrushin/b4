package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
)

func TestKeepUnreadableConfig(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "b4.json")

	if notice := keepUnreadableConfig(path, nil); notice != "" {
		t.Fatalf("a clean load must not produce a notice, got %q", notice)
	}
	if notice := keepUnreadableConfig(path, errors.New("stat failed")); notice != "" {
		t.Fatalf("a missing file has nothing to keep, got %q", notice)
	}

	broken := `{"sets": [{"id": "a"`
	if err := os.WriteFile(path, []byte(broken), 0600); err != nil {
		t.Fatal(err)
	}
	c := config.NewConfig()
	_, loadErr := c.LoadWithMigration(path)
	if loadErr == nil {
		t.Fatal("a truncated config must fail to load")
	}
	notice := keepUnreadableConfig(path, loadErr)
	if !strings.Contains(notice, path+".corrupt") {
		t.Fatalf("the notice must name the kept copy, got %q", notice)
	}
	kept, err := os.ReadFile(path + ".corrupt")
	if err != nil || string(kept) != broken {
		t.Fatalf("copy = %q, %v", kept, err)
	}
	original, _ := os.ReadFile(path)
	if string(original) != broken {
		t.Fatal("the original must stay in place until the next save")
	}
}

func headlessASNConfig() *config.Config {
	c := config.NewConfig()
	tg := config.NewSetConfig()
	tg.Id, tg.Name, tg.Enabled = "tg", "Telegram", true
	tg.Targets.ASNs = []string{"AS62041"}
	tg.Targets.IPs = []string{"198.51.100.7"}
	tg.Targets.IpsToMatch = []string{"198.51.100.7"}
	other := config.NewSetConfig()
	other.Id, other.Name, other.Enabled = "other", "Other", true
	other.Targets.ASNs = []string{"13335"}
	other.Targets.IpsToMatch = []string{"203.0.113.9"}
	c.Sets = []*config.SetConfig{&tg, &other}
	return &c
}

func TestHeadlessASNReloadAppliesNewPrefixesWithoutTheWebServer(t *testing.T) {
	s := config.InitAsnStore(filepath.Join(t.TempDir(), "b4.json"))
	t.Cleanup(func() { config.InitAsnStore("") })
	prefixes := []string{"91.108.4.0/22", "149.154.160.0/20"}
	if err := s.Put(&config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: prefixes, UpdatedAt: time.Now().Unix(), Source: config.AsnSourceRIPEstat}); err != nil {
		t.Fatal(err)
	}
	current := headlessASNConfig()
	load := func() *config.Config { return current }
	commits := 0
	commit := func(previous, next *config.Config) error {
		if previous != current {
			t.Error("the commit gets the config it was built from")
		}
		commits++
		current = next
		return nil
	}

	if reloadASNTargetsHeadless(context.Background(), load, []string{"62041"}, commit) {
		t.Error("new prefixes on a set without duplicate or MSS clamp need no firewall refresh")
	}
	if commits != 1 {
		t.Fatalf("one commit, got %d", commits)
	}
	got := current.GetSetById("tg").Targets.IpsToMatch
	if !slices.Equal(got, append(append([]string{}, prefixes...), "198.51.100.7")) {
		t.Fatalf("the set using the changed ASN matches its prefixes: %v", got)
	}
	if got := current.GetSetById("other").Targets.IpsToMatch; !slices.Equal(got, []string{"203.0.113.9"}) {
		t.Errorf("a set using another ASN is left alone: %v", got)
	}

	for _, changed := range [][]string{nil, {"bogus"}, {"15169"}} {
		reloadASNTargetsHeadless(context.Background(), load, changed, commit)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	reloadASNTargetsHeadless(ctx, load, []string{"62041"}, commit)
	if commits != 1 {
		t.Fatalf("no set references those ASNs or b4 is stopping, so nothing is committed: %d", commits)
	}
}

func TestHeadlessASNReloadKeepsTheConfigWhenTheCommitFails(t *testing.T) {
	s := config.InitAsnStore(filepath.Join(t.TempDir(), "b4.json"))
	t.Cleanup(func() { config.InitAsnStore("") })
	if err := s.Put(&config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: []string{"91.108.4.0/22"}, UpdatedAt: time.Now().Unix(), Source: config.AsnSourceRIPEstat}); err != nil {
		t.Fatal(err)
	}
	current := headlessASNConfig()
	refresh := reloadASNTargetsHeadless(context.Background(), func() *config.Config { return current }, []string{"62041"}, func(_, _ *config.Config) error {
		return errors.New("pool rejected the config")
	})
	if refresh {
		t.Error("a failed commit needs no firewall refresh")
	}
	if got := current.GetSetById("tg").Targets.IpsToMatch; !slices.Equal(got, []string{"198.51.100.7"}) {
		t.Fatalf("the live config is never mutated: %v", got)
	}
}

func TestHeadlessASNReloadAsksForAFirewallRefreshWhenTheDuplicateSetsChange(t *testing.T) {
	s := config.InitAsnStore(filepath.Join(t.TempDir(), "b4.json"))
	t.Cleanup(func() { config.InitAsnStore("") })
	if err := s.Put(&config.AsnInfo{ID: "62041", Name: "Telegram", Prefixes: []string{"91.108.4.0/22"}, UpdatedAt: time.Now().Unix(), Source: config.AsnSourceRIPEstat}); err != nil {
		t.Fatal(err)
	}
	current := headlessASNConfig()
	current.Sets[0].TCP.Duplicate.Enabled = true
	current.Sets[0].TCP.Duplicate.Count = 2
	refresh := reloadASNTargetsHeadless(context.Background(), func() *config.Config { return current }, []string{"62041"}, func(_, next *config.Config) error {
		current = next
		return nil
	})
	if !refresh {
		t.Fatal("new prefixes on a duplicate set must rebuild the duplicate firewall sets")
	}
}
