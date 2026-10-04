package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

func TestSeedFillsEveryConsoleState(t *testing.T) {
	dir := t.TempDir()
	if err := run(options{data: dir, publicURL: "http://127.0.0.1:1", days: 45, seed: 1}); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	st, err := store.Open(hubdata.Layout{Root: dir}.DBPath())
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	for _, status := range []string{hubwire.SetStatusActive, hubwire.SetStatusPending, hubwire.SetStatusHidden, hubwire.SetStatusRejected} {
		versions, err := st.VersionsByStatus(ctx, status)
		if err != nil {
			t.Fatal(err)
		}
		if len(versions) == 0 {
			t.Errorf("no %s set versions", status)
		}
	}

	reports, err := st.AllReports(ctx)
	if err != nil {
		t.Fatal(err)
	}
	states := make(map[string]int)
	for _, r := range reports {
		states[r.State]++
	}
	for _, state := range []string{store.ReportOpen, store.ReportDismissed, store.ReportResolved} {
		if states[state] == 0 {
			t.Errorf("no %s reports", state)
		}
	}

	mirrors, err := st.Mirrors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mirrorStates := make(map[string]int)
	failing := 0
	for _, m := range mirrors {
		mirrorStates[m.Status]++
		if m.Status == store.MirrorApproved && !m.Healthy() {
			failing++
		}
	}
	for _, status := range []string{store.MirrorPending, store.MirrorApproved, store.MirrorRejected} {
		if mirrorStates[status] == 0 {
			t.Errorf("no %s mirrors", status)
		}
	}
	if failing == 0 {
		t.Error("no approved mirror fails its check")
	}
}

func TestSeedKeepsAnExistingDatabaseWithoutReset(t *testing.T) {
	dir := t.TempDir()
	o := options{data: dir, publicURL: "http://127.0.0.1:1", days: 45, seed: 1}
	if err := run(o); err != nil {
		t.Fatal(err)
	}
	err := run(o)
	if err == nil || !strings.Contains(err.Error(), "--reset") {
		t.Fatalf("second run without --reset: %v", err)
	}
	o.reset = true
	if err := run(o); err != nil {
		t.Fatal(err)
	}
}

func TestSeedWritesNothingWhileAHubAnswers(t *testing.T) {
	hub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer hub.Close()
	dir := t.TempDir()
	layout := hubdata.Layout{Root: dir}
	if err := os.WriteFile(layout.DBPath(), []byte("live"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := run(options{data: dir, publicURL: hub.URL, reset: true, days: 45, seed: 1})
	if err == nil || !strings.Contains(err.Error(), "a hub answers") {
		t.Fatalf("seeding next to a live hub: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != hubdata.DBFile {
		names := make([]string, 0, len(entries))
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("data directory changed: %v", names)
	}
	if raw, err := os.ReadFile(layout.DBPath()); err != nil || string(raw) != "live" {
		t.Fatalf("database touched: %q %v", raw, err)
	}
}
