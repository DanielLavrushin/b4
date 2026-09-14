package ingest

import (
	"context"
	"net/http"
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
	announce := func(url string) Response {
		return f.post(t, testkit.Sign(t, child, hubwire.RecordMirror, hubwire.MirrorBody{URL: url, Version: "1.82.0"}, f.clock), peerA)
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
	if len(mirrors) != 1 || mirrors[0].URL != "https://mirror.example" || mirrors[0].KeyHMAC != hubdata.KeyHMAC(f.svc.Secret, child.KeyID()) || !mirrors[0].LastSeen.Equal(f.clock) {
		t.Fatalf("mirror not stored as announced: %+v", mirrors)
	}
	firstSeen := mirrors[0].FirstSeen

	f.clock = f.clock.Add(time.Hour)
	expect(t, announce("https://mirror.example"), http.StatusAccepted, "")
	mirrors, _ = f.store.Mirrors(ctx)
	if len(mirrors) != 1 || !mirrors[0].LastSeen.Equal(f.clock) || !mirrors[0].FirstSeen.Equal(firstSeen) {
		t.Fatalf("a re-announce must refresh last_seen only: %+v", mirrors)
	}

	for _, bad := range []string{"", "http://mirror.example", "https://user:pw@mirror.example", "https://mirror.example/?x=1", "https://mirror.example/#top", "ftp://mirror.example"} {
		expect(t, announce(bad), http.StatusBadRequest, CodeBadMirrorURL)
	}
	expect(t, announce("http://192.168.1.10:7100/"), http.StatusAccepted, "")
	if mirrors, _ = f.store.Mirrors(ctx); len(mirrors) != 2 {
		t.Fatalf("a private http mirror must be accepted, got %d mirrors", len(mirrors))
	}
}
