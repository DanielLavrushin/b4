package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/moderation"
	"github.com/daniellavrushin/b4hub/internal/store"
)

func printed(t *testing.T, print func()) string {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	saved := os.Stdout
	os.Stdout = w
	print()
	w.Close()
	os.Stdout = saved
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func TestARefusedGroupActionPrintsNoTransition(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	now := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	author := "aa00000000000000000000000000000000000000000000000000000000000001"
	group := func(status string) (string, string) {
		t.Helper()
		setID, _ := hubdata.NewSetID()
		v := &store.Version{FP: "fp-" + setID, TargetsKey: "tk-" + setID, Title: "Video", Projection: map[string]interface{}{}, Status: hubwire.SetStatusPending, UploaderHMAC: author, CreatedAt: now, UpdatedAt: now}
		if err := st.CreateSet(ctx, store.Set{ID: setID, AuthorHMAC: author, CreatedAt: now, UpdatedAt: now}, v); err != nil {
			t.Fatal(err)
		}
		groupID, _ := hubdata.NewSetID()
		gv := &store.GroupVersion{Title: "media", Status: status, UploaderHMAC: author, CreatedAt: now, UpdatedAt: now,
			Members: []store.GroupMember{{SetID: setID, SetVersion: 1, Brought: true}}}
		if err := st.Update(ctx, func(tx *store.Tx) error {
			return tx.CreateGroup(ctx, store.Group{ID: groupID, AuthorHMAC: author, CreatedAt: now, UpdatedAt: now}, gv)
		}); err != nil {
			t.Fatal(err)
		}
		return groupID, setID
	}
	mod := &moderation.Service{Store: st, Now: func() time.Time { return now }}
	approve := func(groupID string) moderation.GroupResult {
		t.Helper()
		res, err := mod.ModerateGroup(ctx, cliActor(), moderation.ActionApprove, moderation.GroupRef{GroupID: groupID, Version: 1}, moderation.Options{})
		if err == nil {
			t.Fatal("the approve must be refused")
		}
		return res
	}

	active, _ := group(hubwire.SetStatusActive)
	if out := printed(t, func() { printGroupEffects(approve(active)) }); out != "group "+active+"/1: refused (invalid_transition)\n" {
		t.Fatalf("an active group cannot be approved: %q", out)
	}

	pending, member := group(hubwire.SetStatusPending)
	if _, _, err := st.TouchKey(ctx, author, now); err != nil {
		t.Fatal(err)
	}
	if err := st.BanKey(ctx, author, "spam", now); err != nil {
		t.Fatal(err)
	}
	want := "group " + pending + "/1: refused (author_banned)\n" + member + "/1: refused (author_banned)\n"
	if out := printed(t, func() { printGroupEffects(approve(pending)) }); out != want {
		t.Fatalf("a banned author's group waits for --force: %q", out)
	}
}

func TestAKeptMemberIsPrintedAsKept(t *testing.T) {
	res := moderation.GroupResult{
		Group: moderation.GroupEffect{GroupID: "G", Version: 1, From: hubwire.SetStatusPending, To: hubwire.SetStatusRejected, OK: true},
		Members: []moderation.MemberItem{
			{Item: moderation.Item{Effect: moderation.Effect{SetID: "A", Version: 1, From: hubwire.SetStatusPending}, OK: true, Code: moderation.CodeKeptNewerGroupVersion}, Brought: true},
			{Item: moderation.Item{Effect: moderation.Effect{SetID: "B", Version: 1, From: hubwire.SetStatusPending, To: hubwire.SetStatusRejected}, OK: true}, Position: 1, Brought: true},
		},
	}
	want := "group G/1: pending -> rejected, no catalogue change\nA/1: kept (kept_newer_group_version)\nB/1: pending -> rejected, no catalogue change\n"
	if out := printed(t, func() { printGroupEffects(res) }); out != want {
		t.Fatalf("a member a newer group version still includes is printed as kept: %q", out)
	}
}
