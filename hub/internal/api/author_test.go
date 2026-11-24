package api

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/asn"
	"github.com/daniellavrushin/b4hub/internal/testkit"
)

const (
	authorPeer    = "203.0.113.5"
	neighbourPeer = "203.0.112.9"
	foreignPeer   = "198.51.100.7"
)

func startHubWithNeighbours(t *testing.T) *hub {
	t.Helper()
	h := startHub(t)
	h.api.Ingest.ASN = asn.New(testkit.CymruLookup(map[string]testkit.Origin{
		authorPeer:    {ASN: "64500", Country: "RU", Name: "EXAMPLE-A ISP A"},
		neighbourPeer: {ASN: "64500", Country: "RU", Name: "EXAMPLE-A ISP A"},
		foreignPeer:   {ASN: "64501", Country: "DE", Name: "EXAMPLE-B ISP B"},
	}), h.store, func() time.Time { return h.clock })
	return h
}

func (h *hub) submit(author *hubwire.Identity, set config.SetConfig, from string) (setID, fp string) {
	h.t.Helper()
	env := testkit.BuildEnvelope(h.t, &set)
	status, body := h.post(testkit.SignShare(h.t, author, env, h.clock), from)
	if status != http.StatusAccepted {
		h.t.Fatalf("share: %d %v", status, body)
	}
	return body["set_id"].(string), env.Fingerprint
}

func (h *hub) publish(author *hubwire.Identity, set config.SetConfig, from string) (setID, fp string) {
	h.t.Helper()
	setID, fp = h.submit(author, set, from)
	if err := h.store.Approve(context.Background(), setID, 1, h.clock); err != nil {
		h.t.Fatal(err)
	}
	return setID, fp
}

func (h *hub) works(voter *hubwire.Identity, setID, fp, from string) {
	h.t.Helper()
	h.clock = h.clock.Add(time.Second)
	raw := testkit.Sign(h.t, voter, hubwire.RecordVote, hubwire.VoteBody{SetID: setID, Version: 1, FP: fp, Kind: hubwire.VoteWorks}, h.clock)
	if status, body := h.post(raw, from); status != http.StatusAccepted {
		h.t.Fatalf("vote from %s: %d %v", from, status, body)
	}
}

func (h *hub) published() map[string]hubwire.Scores {
	h.t.Helper()
	r, _, err := h.builder.BuildIfNeeded(context.Background())
	if err != nil {
		h.t.Fatal(err)
	}
	out := make(map[string]hubwire.Scores)
	for _, s := range r.Catalogue.Sets {
		out[s.ID] = s.Scores
	}
	return out
}

func youtube() config.SetConfig {
	return testkit.SampleSet("YouTube", "youtube.com")
}

func discord() config.SetConfig {
	return testkit.SampleSet("Discord", "discord.com")
}

func TestTheAuthorsNetworkStaysOutOfProviderAndCountryScores(t *testing.T) {
	h := startHubWithNeighbours(t)
	setID, fp := h.publish(testkit.Identity(t), youtube(), authorPeer)
	h.works(testkit.Identity(t), setID, fp, neighbourPeer)
	if s := h.published()[setID]; s.Global.Devices != 2 || s.ASN != nil || s.CC != nil {
		t.Fatalf("one vote beside the author must not list the author's network: %+v", s)
	}
	h.works(testkit.Identity(t), setID, fp, neighbourPeer)
	if s := h.published()[setID]; s.Global.Devices != 3 || s.ASN["64500"].Devices != 2 || s.CC["RU"].Devices != 2 {
		t.Fatalf("the cells must count the two other devices only: %+v", s)
	}
}

func TestAnAuthorStaysOutOfTheScoresOfEverySetWithTheSameStrategy(t *testing.T) {
	h := startHubWithNeighbours(t)
	author := testkit.Identity(t)
	first, fp := h.publish(testkit.Identity(t), youtube(), authorPeer)
	second, other := h.publish(author, discord(), foreignPeer)
	if fp != other {
		t.Fatal("the two sets must share a strategy")
	}
	h.works(testkit.Identity(t), first, fp, neighbourPeer)
	h.works(testkit.Identity(t), second, fp, foreignPeer)
	h.works(author, first, fp, foreignPeer)
	scores := h.published()
	if len(scores) != 2 {
		t.Fatalf("both sets must be listed, got %d", len(scores))
	}
	for _, id := range []string{first, second} {
		if s := scores[id]; s.Global.Devices != 4 || s.ASN != nil || s.CC != nil {
			t.Errorf("set %s must list the network of neither author: %+v", id, s)
		}
	}
}

func TestAnAuthorOfAnotherStrategyCountsLikeAnyOtherDevice(t *testing.T) {
	h := startHubWithNeighbours(t)
	author := testkit.Identity(t)
	tuned := discord()
	tuned.Faking.TTL = 9
	listed, fp := h.publish(testkit.Identity(t), youtube(), foreignPeer)
	if _, other := h.publish(author, tuned, authorPeer); fp == other {
		t.Fatal("the two sets must differ in their strategy")
	}
	h.works(author, listed, fp, authorPeer)
	h.works(testkit.Identity(t), listed, fp, neighbourPeer)
	if s := h.published()[listed]; s.Global.Devices != 3 || s.ASN["64500"].Devices != 2 || s.CC["RU"].Devices != 2 {
		t.Fatalf("an author is left out only where the strategy is their own: %+v", s)
	}
}

func TestTheAuthorOfARejectedSetCountsLikeAnyOtherDevice(t *testing.T) {
	h := startHubWithNeighbours(t)
	listed, fp := h.publish(testkit.Identity(t), youtube(), foreignPeer)
	pending, other := h.submit(testkit.Identity(t), discord(), authorPeer)
	if fp != other {
		t.Fatal("the two sets must share a strategy")
	}
	h.works(testkit.Identity(t), listed, fp, neighbourPeer)
	if s := h.published()[listed]; s.Global.Devices != 3 || s.ASN != nil || s.CC != nil {
		t.Fatalf("the author of a set that may still be listed stays out: %+v", s)
	}
	if err := h.store.Reject(context.Background(), pending, 1, "duplicate", h.clock); err != nil {
		t.Fatal(err)
	}
	if s := h.published()[listed]; s.Global.Devices != 3 || s.ASN["64500"].Devices != 2 || s.CC["RU"].Devices != 2 {
		t.Fatalf("the author of a rejected set has nothing listed to hide: %+v", s)
	}
}
