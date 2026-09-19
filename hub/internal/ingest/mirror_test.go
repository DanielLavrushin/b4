package ingest

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func TestMirrorAnnounceIsStoredPendingAndRefreshed(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	child := testkit.Identity(t)
	announceVersion := func(url, version string) Response {
		return f.post(t, testkit.Sign(t, child, hubwire.RecordMirror, hubwire.MirrorBody{URL: url, Version: version}, f.clock), peerA)
	}
	announce := func(url string) Response {
		return announceVersion(url, "1.82.0")
	}

	resp := announce("https://Mirror.example/")
	expect(t, resp, http.StatusAccepted, "")
	if resp.Body["status"] != store.MirrorPending || resp.Body["kind"] != hubwire.RecordMirror {
		t.Fatalf("unexpected acceptance %v", resp.Body)
	}
	mirrors, err := f.store.Mirrors(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(mirrors) != 1 || mirrors[0].URL != "https://mirror.example" || mirrors[0].KeyHMAC != hubdata.KeyHMAC(f.svc.Secret, child.KeyID()) || !mirrors[0].LastSeen.Equal(f.clock) || mirrors[0].Version != "1.82.0" {
		t.Fatalf("mirror not stored as announced: %+v", mirrors)
	}
	firstSeen := mirrors[0].FirstSeen

	f.clock = f.clock.Add(time.Hour)
	expect(t, announceVersion("https://mirror.example", " 1.0.1\x00\n"+strings.Repeat("x", 60)), http.StatusAccepted, "")
	mirrors, _ = f.store.Mirrors(ctx)
	if len(mirrors) != 1 || !mirrors[0].LastSeen.Equal(f.clock) || !mirrors[0].FirstSeen.Equal(firstSeen) {
		t.Fatalf("a re-announce must refresh last_seen only: %+v", mirrors)
	}
	if want := "1.0.1" + strings.Repeat("x", MaxMirrorVersionLength-5); mirrors[0].Version != want {
		t.Fatalf("a re-announce must carry the cleaned version, got %q", mirrors[0].Version)
	}

	for _, bad := range []string{"", "http://mirror.example", "https://user:pw@mirror.example", "https://mirror.example/?x=1", "https://mirror.example/#top", "ftp://mirror.example"} {
		expect(t, announce(bad), http.StatusBadRequest, CodeBadMirrorURL)
	}
	expect(t, announce("http://192.168.1.10:7100/"), http.StatusAccepted, "")
	if mirrors, _ = f.store.Mirrors(ctx); len(mirrors) != 2 {
		t.Fatalf("a private http mirror must be accepted, got %d mirrors", len(mirrors))
	}
}
