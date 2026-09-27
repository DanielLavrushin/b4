package ingest

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func TestOnAcceptedSeesAcceptedRecordsOnly(t *testing.T) {
	f := newFixture(t)
	var kinds []string
	f.svc.OnAccepted = func(kind string) { kinds = append(kinds, kind) }

	author := testkit.Identity(t)
	set := testkit.SampleSet("Hooked", "hooked.example")
	raw := testkit.SignShare(t, author, testkit.BuildEnvelope(t, &set), f.clock)
	accepted := f.post(t, raw, peerA)
	expect(t, accepted, http.StatusAccepted, "")
	if accepted.Body["status"] != hubwire.SetStatusPending || accepted.Body["version"] != 1 {
		t.Fatalf("the hook must not change the response: %v", accepted.Body)
	}
	expect(t, f.post(t, raw, peerA), http.StatusOK, "")
	expect(t, f.post(t, []byte("{not json"), peerA), http.StatusBadRequest, CodeBadRecord)
	expect(t, f.share(t, testkit.Identity(t), set, peerB), http.StatusConflict, CodeDuplicateStrategy)
	if strings.Join(kinds, ",") != hubwire.RecordShare {
		t.Fatalf("only the accepted share may fire, got %v", kinds)
	}

	setID := accepted.Body["set_id"].(string)
	if err := f.store.Approve(context.Background(), setID, 1, f.clock); err != nil {
		t.Fatal(err)
	}
	fileReport(t, f, testkit.Identity(t), setID, peerB)
	mirror := testkit.Sign(t, testkit.Identity(t), hubwire.RecordMirror, hubwire.MirrorBody{URL: "https://mirror.example", Version: "1.0"}, f.clock)
	expect(t, f.post(t, mirror, peerC), http.StatusAccepted, "")
	if want := hubwire.RecordShare + "," + hubwire.RecordReport + "," + hubwire.RecordMirror; strings.Join(kinds, ",") != want {
		t.Fatalf("expected %s, got %v", want, kinds)
	}

	f.svc.OnAccepted = nil
	other := testkit.SampleSet("Unhooked", "unhooked.example")
	expect(t, f.share(t, author, other, peerA), http.StatusAccepted, "")
}
