package hub

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hub/hubtest"
	"github.com/daniellavrushin/b4/hubwire"
)

func groupIDs(groups []*hubwire.CatalogueGroup) []string {
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		ids = append(ids, g.ID)
	}
	return ids
}

func TestCatalogueGroupsAreIndexedOnlyWhenEveryMemberResolves(t *testing.T) {
	f := hubtest.New(t)
	cat := sampleCatalogue(t, 1, 1)
	video := strategySet("Video CDN", []string{"googlevideo.com"}, nil, 11)
	cdn, _ := hubtest.CatalogueSet(t, "cdn", 1, &video, nil)
	cat.Sets = append(cat.Sets, cdn)
	cat.Groups = []hubwire.CatalogueGroup{
		{ID: "g-video", Version: 2, Title: "video", Members: []hubwire.CatalogueGroupMember{{SetID: "direct", Escalate: &hubwire.GroupEscalation{To: 1}}, {SetID: "cdn"}}},
		{ID: "g-gone", Version: 1, Title: "gone", Members: []hubwire.CatalogueGroupMember{{SetID: "direct"}, {SetID: "deleted"}}},
		{ID: "g-pending", Version: 1, Title: "pending", Members: []hubwire.CatalogueGroupMember{{SetID: "pending"}}},
		{ID: "g-loop", Version: 1, Title: "loop", Members: []hubwire.CatalogueGroupMember{{SetID: "cdn", Escalate: &hubwire.GroupEscalation{To: 0}}}},
		{ID: "g-cat", Version: 1, Title: "category", Members: []hubwire.CatalogueGroupMember{{SetID: "category"}}},
		{ID: "g-ttl", Version: 1, Title: "ttl", Members: []hubwire.CatalogueGroupMember{{SetID: "direct", Escalate: &hubwire.GroupEscalation{To: 1, TtlSec: 86401}}, {SetID: "cdn"}}},
	}
	f.Publish(t, cat, time.Now().Add(time.Hour))
	box := newTestBox(t, f, t.TempDir())
	if _, err := box.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := groupIDs(box.svc.Groups("")); len(got) != 2 || got[0] != "g-video" || got[1] != "g-cat" {
		t.Fatalf("only groups whose members are all listed and whose links and thresholds are valid are indexed, got %v", got)
	}
	if g, ok := box.svc.Group("g-video"); !ok || g.Version != 2 || len(g.Members) != 2 {
		t.Fatalf("group lookup by id failed: %+v %v", g, ok)
	}
	if _, ok := box.svc.Group("g-gone"); ok {
		t.Errorf("a group with a member missing from the catalogue must not resolve")
	}
	if got := groupIDs(box.svc.Groups("rr1.googlevideo.com")); len(got) != 1 || got[0] != "g-video" {
		t.Errorf("a domain search lists a group when any member matches, got %v", got)
	}
	if got := groupIDs(box.svc.Groups("example.org")); len(got) != 0 {
		t.Errorf("no member matches, got %v", got)
	}
	if got := groupIDs(box.svc.GroupsOf("direct")); len(got) != 1 || got[0] != "g-video" {
		t.Errorf("GroupsOf must name the listed groups holding the set, got %v", got)
	}
	if results, _ := box.svc.Search("googlevideo.com", 0); len(results) != 1 || results[0].Set.ID != "cdn" {
		t.Errorf("Search keeps returning member sets on their own, got %+v", results)
	}
}

func TestGroupedSetsAreListedOnlyThroughTheirGroup(t *testing.T) {
	f := hubtest.New(t)
	cat := sampleCatalogue(t, 1, 1)
	video := strategySet("Video CDN", []string{"googlevideo.com"}, nil, 11)
	images := strategySet("Images", []string{"ytimg.com"}, nil, 12)
	hiddenSet := strategySet("Hidden", []string{"ggpht.com"}, nil, 13)
	cdn, _ := hubtest.CatalogueSet(t, "cdn", 1, &video, nil)
	img, _ := hubtest.CatalogueSet(t, "img", 1, &images, nil)
	hidden, _ := hubtest.CatalogueSet(t, "hidden", 1, &hiddenSet, nil)
	cdn.Status, img.Status, hidden.Status = hubwire.SetStatusGrouped, hubwire.SetStatusGrouped, hubwire.SetStatusHidden
	cat.Sets = append(cat.Sets, cdn, img, hidden)
	cat.Groups = []hubwire.CatalogueGroup{
		{ID: "g-brought", Version: 1, Title: "brought", Members: []hubwire.CatalogueGroupMember{{SetID: "cdn"}, {SetID: "img"}, {SetID: "direct"}}},
		{ID: "g-hidden", Version: 1, Title: "hidden", Members: []hubwire.CatalogueGroupMember{{SetID: "cdn"}, {SetID: "hidden"}}},
		{ID: "g-pending", Version: 1, Title: "pending", Members: []hubwire.CatalogueGroupMember{{SetID: "img"}, {SetID: "pending"}}},
	}
	f.Publish(t, cat, time.Now().Add(time.Hour))
	box := newTestBox(t, f, t.TempDir())
	if _, err := box.svc.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}

	if got := groupIDs(box.svc.Groups("")); len(got) != 1 || got[0] != "g-brought" {
		t.Fatalf("a group of grouped and active members resolves, one with a hidden or pending member does not, got %v", got)
	}
	if got := groupIDs(box.svc.Groups("googlevideo.com")); len(got) != 1 || got[0] != "g-brought" {
		t.Errorf("a domain search finds the group through its grouped member, got %v", got)
	}
	if results, total := box.svc.Search("", 0); total != 2 {
		t.Errorf("the standalone list keeps only active sets, got %d: %+v", total, results)
	}
	if results, _ := box.svc.Search("googlevideo.com", 0); len(results) != 0 {
		t.Errorf("a grouped set never appears on its own, got %+v", results)
	}
	if results, _ := box.svc.SearchWithGrouped("googlevideo.com", 0); len(results) != 1 || results[0].Set.ID != "cdn" {
		t.Errorf("the search Discovery uses must find the grouped set, got %+v", results)
	}
	if results, _ := box.svc.SearchWithGrouped("ggpht.com", 0); len(results) != 0 {
		t.Errorf("a hidden set stays out of every search, got %+v", results)
	}
	if res, ok := box.svc.Get("cdn"); !ok || res.Set.Status != hubwire.SetStatusGrouped {
		t.Errorf("a lookup by id still finds a grouped set, got %+v %v", res, ok)
	}
}

func TestAGroupRecordIsNeverQueued(t *testing.T) {
	f := hubtest.New(t)
	box := newTestBox(t, f, t.TempDir())
	f.SetDown(true)
	rec, err := box.svc.Sign(hubwire.RecordGroup, hubwire.GroupBody{Title: "video", Members: []hubwire.GroupMember{{Envelope: hubwire.Envelope{Format: hubwire.Format, Set: map[string]interface{}{}}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, err := box.svc.SendOrQueue(context.Background(), rec); !errors.Is(err, ErrShareNotQueued) {
		t.Fatalf("a group record is never queued, got %v", err)
	}
	if box.svc.OutboxCount() != 0 {
		t.Errorf("the outbox must stay empty, got %d", box.svc.OutboxCount())
	}
}

func TestAGroupVoteIsQueuedAndFlushed(t *testing.T) {
	f := hubtest.New(t)
	box := newTestBox(t, f, t.TempDir())
	f.SetDown(true)
	rec, err := box.svc.Sign(hubwire.RecordGroupVote, hubwire.GroupVoteBody{GroupID: "g-video", Version: 2, Members: []hubwire.GroupVoteMember{{SetID: "s1", Version: 3}}, Kind: hubwire.VoteWorks})
	if err != nil {
		t.Fatal(err)
	}
	sent, queued, _, err := box.svc.SendOrQueue(context.Background(), rec)
	if err != nil || sent || !queued || box.svc.OutboxCount() != 1 {
		t.Fatalf("a group vote is queued while the hub is down: sent=%v queued=%v err=%v outbox=%d", sent, queued, err, box.svc.OutboxCount())
	}
	f.SetDown(false)
	box.svc.FlushOutbox(context.Background())
	if box.svc.OutboxCount() != 0 {
		t.Fatalf("the outbox must drain once the hub answers, %d left", box.svc.OutboxCount())
	}
	if records := f.Records(); len(records) != 1 || records[0].Kind != hubwire.RecordGroupVote || records[0].ID() != rec.ID() {
		t.Errorf("the queued group vote must reach the hub unchanged: %+v", records)
	}
}

func TestSendDecodesTheGroupAnswer(t *testing.T) {
	f := hubtest.New(t)
	box := newTestBox(t, f, t.TempDir())
	body := hubwire.GroupBody{Title: "video", Members: []hubwire.GroupMember{{Envelope: hubwire.Envelope{Format: hubwire.Format}}, {Envelope: hubwire.Envelope{Format: hubwire.Format}}}}
	rec, err := box.svc.Sign(hubwire.RecordGroup, body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := box.svc.Send(context.Background(), rec)
	if err != nil {
		t.Fatal(err)
	}
	if resp.GroupID == "" || resp.Version != 1 || resp.Status != hubwire.SetStatusPending || len(resp.Members) != 2 || resp.Members[1].SetID == "" {
		t.Fatalf("the group answer must decode: %+v", resp)
	}

	f.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: 400, Body: map[string]interface{}{"code": "invalid_set", "error": "no targets", "member": 0}}
	})
	_, err = box.svc.Send(context.Background(), rec)
	var he *HubError
	if !errors.As(err, &he) || he.Code != "invalid_set" || he.Member == nil || *he.Member != 0 {
		t.Fatalf("a member refusal must carry the member index, even index 0: %+v", err)
	}
	f.SetAnswer(func(*hubwire.Record) hubtest.Answer {
		return hubtest.Answer{Status: 409, Body: map[string]interface{}{"code": "duplicate_group", "error": "unchanged", "group_id": "g-1", "version": 3}}
	})
	_, err = box.svc.Send(context.Background(), rec)
	if !errors.As(err, &he) || he.GroupID != "g-1" || he.Version != 3 || he.Member != nil {
		t.Fatalf("a duplicate group must carry the group id: %+v", err)
	}
}

func TestAnUndecodableGroupLeavesTheRestOfTheCatalogue(t *testing.T) {
	set := `{"id":"%s","version":1,"fp":"f","title":"%s","author":"a","b4_min":"1","status":"active","created_at":"x","updated_at":"x","set":{},"scores":{}}`
	sets := fmt.Sprintf(set, "s1", "A") + "," + fmt.Sprintf(set, "s2", "B")
	for _, c := range []struct {
		groups string
		want   []string
	}{
		{`[{"id":"g-big","version":1,"title":"big","author":"a","created_at":"x","updated_at":"x","members":[{"set_id":"s1","escalate":{"to":1,"ttl_sec":99999999999999999999}},{"set_id":"s2"}]},` +
			`{"id":"g-type","version":1,"title":"type","author":"a","created_at":"x","updated_at":"x","members":[{"set_id":"s1","escalate":{"to":"s2"}},{"set_id":"s2"}]},` +
			`{"id":"g-ok","version":1,"title":"ok","author":"a","created_at":"x","updated_at":"x","members":[{"set_id":"s1","escalate":{"to":1,"rst_threshold":4}},{"set_id":"s2"}]}]`, []string{"g-ok"}},
		{`{"id":"g-ok"}`, nil},
	} {
		var buf bytes.Buffer
		zw := gzip.NewWriter(&buf)
		if _, err := fmt.Fprintf(zw, `{"epoch":1,"seq":2,"generated_at":"x","sets":[%s],"groups":%s}`, sets, c.groups); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		cat, err := decodeCatalogue(buf.Bytes())
		if err != nil {
			t.Fatalf("one bad group must not fail the catalogue: %v", err)
		}
		if len(cat.Sets) != 2 {
			t.Errorf("every set survives a bad group, got %d", len(cat.Sets))
		}
		got := make([]string, 0, len(cat.Groups))
		for _, g := range cat.Groups {
			got = append(got, g.ID)
		}
		if len(got) != len(c.want) || (len(got) == 1 && got[0] != c.want[0]) {
			t.Errorf("groups %s decode to %v, want %v", c.groups, got, c.want)
		}
		if len(cat.Groups) == 1 && (cat.Groups[0].Members[0].Escalate == nil || cat.Groups[0].Members[0].Escalate.RstThreshold != 4) {
			t.Errorf("the good group keeps its escalation, got %+v", cat.Groups[0].Members)
		}
	}
}
