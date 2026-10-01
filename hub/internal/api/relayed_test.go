package api

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/store"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

func (h *hub) postWith(raw []byte, forwardedFor string, headers map[string]string) int {
	h.t.Helper()
	req, err := http.NewRequest(http.MethodPost, h.server.URL+hubwire.PathMessage, bytes.NewReader(raw))
	if err != nil {
		h.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Forwarded-For", forwardedFor)
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	resp, err := h.server.Client().Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestOnlyASignedApprovedMirrorTakesTheNetworkOffARecord(t *testing.T) {
	h := startHub(t)
	ctx := context.Background()
	author := testkit.Identity(t)
	set := testkit.SampleSet("YouTube", "youtube.com")
	status, body := h.post(testkit.SignShare(t, author, testkit.BuildEnvelope(t, &set), h.clock), "203.0.113.5")
	if status != http.StatusAccepted {
		t.Fatalf("share: %d %v", status, body)
	}
	setID := body["set_id"].(string)
	if err := h.store.Approve(ctx, setID, 1, h.clock); err != nil {
		t.Fatal(err)
	}
	v, err := h.store.GetVersion(ctx, setID, 1)
	if err != nil {
		t.Fatal(err)
	}
	mirror := testkit.Identity(t)
	row, err := h.store.AnnounceMirror(ctx, "https://relay.example", hubdata.KeyHMAC(h.api.Ingest.Secret, mirror.KeyID()), "1.3.0", h.clock)
	if err != nil {
		t.Fatal(err)
	}
	if err := h.store.SetMirrorStatus(ctx, row.ID, store.MirrorApproved, "", h.clock); err != nil {
		t.Fatal(err)
	}

	claimed := testkit.Identity(t)
	relayed := testkit.Identity(t)
	vote := func(id *hubwire.Identity) []byte {
		return testkit.Sign(t, id, hubwire.RecordVote, hubwire.VoteBody{SetID: setID, Version: 1, FP: v.FP, Kind: hubwire.VoteWorks}, h.clock)
	}
	if code := h.postWith(vote(claimed), "198.51.100.7", map[string]string{"User-Agent": ingest.RelayAgent}); code != http.StatusAccepted {
		t.Fatalf("vote claiming to be relayed: %d", code)
	}
	signed := vote(relayed)
	if code := h.postWith(signed, "198.51.100.7", map[string]string{"User-Agent": ingest.RelayAgent, hubdata.HeaderRelay: hubdata.SignRelay(mirror, h.clock, signed)}); code != http.StatusAccepted {
		t.Fatalf("vote relayed by an approved mirror: %d", code)
	}

	votes, err := h.store.VotesForFP(ctx, v.FP)
	if err != nil {
		t.Fatal(err)
	}
	seen := 0
	for _, vote := range votes {
		switch vote.KeyHMAC {
		case hubdata.KeyHMAC(h.api.Ingest.Secret, claimed.KeyID()):
			seen++
			if vote.ASNObserved != "64501" || !vote.OriginVerified {
				t.Errorf("a mirror user agent alone proves nothing, so the sender's network stays, got %+v", vote)
			}
		case hubdata.KeyHMAC(h.api.Ingest.Secret, relayed.KeyID()):
			seen++
			if vote.ASNObserved != "" || vote.CountryObserved != "" || vote.OriginVerified {
				t.Errorf("a vote signed over by an approved mirror must not count on the mirror's network, got %+v", vote)
			}
		}
	}
	if seen != 2 {
		t.Fatalf("expected both votes stored, saw %d", seen)
	}
}
