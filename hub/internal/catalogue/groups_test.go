package catalogue

import (
	"context"
	"reflect"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	memberA = "01ARZ3NDEKTSV4RRFFQ69G5FA1"
	memberB = "01ARZ3NDEKTSV4RRFFQ69G5FA2"
	memberC = "01ARZ3NDEKTSV4RRFFQ69G5FA3"

	groupOne = "01ARZ3NDEKTSV4RRFFQ69G5GA1"
	groupTwo = "01ARZ3NDEKTSV4RRFFQ69G5GA2"
)

func addGroup(t *testing.T, st *store.Store, id, author, status string, members ...store.GroupMember) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	err := st.Update(ctx, func(tx *store.Tx) error {
		v := &store.GroupVersion{Title: "group " + id, Status: hubwire.SetStatusPending, UploaderHMAC: author, CreatedAt: now, UpdatedAt: now, Members: members}
		if _, _, err := tx.Group(ctx, id); err == nil {
			v.GroupID = id
			if err := tx.AddGroupVersion(ctx, v); err != nil {
				return err
			}
		} else if err := tx.CreateGroup(ctx, store.Group{ID: id, AuthorHMAC: author, CreatedAt: now, UpdatedAt: now}, v); err != nil {
			return err
		}
		if status == hubwire.SetStatusPending {
			return nil
		}
		return tx.SetGroupStatus(ctx, id, v.Version, status, "", now)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func listedGroups(t *testing.T, b *Builder) []hubwire.CatalogueGroup {
	t.Helper()
	result, err := b.Build(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	return result.Catalogue.Groups
}

func TestTheCatalogueListsAGroupOnlyWhileEveryMemberIsListed(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	addActiveSet(t, st, memberA, "fp-a")
	addActiveSet(t, st, memberB, "fp-b")
	escalate := &hubwire.GroupEscalation{To: 1, RstThreshold: 3}
	addGroup(t, st, groupOne, "author-group", hubwire.SetStatusActive, store.GroupMember{SetID: memberB, SetVersion: 1, Escalate: escalate}, store.GroupMember{SetID: memberA, SetVersion: 1})
	addGroup(t, st, groupTwo, "author-group", hubwire.SetStatusPending, store.GroupMember{SetID: memberA, SetVersion: 1})

	groups := listedGroups(t, b)
	want := []hubwire.CatalogueGroup{{
		ID: groupOne, Version: 1, Title: "group " + groupOne, Author: hubdata.AuthorLabel("author-group"),
		CreatedAt: "2026-09-12T00:00:00Z", UpdatedAt: "2026-09-12T00:00:00Z",
		Members: []hubwire.CatalogueGroupMember{{SetID: memberB, Escalate: escalate}, {SetID: memberA}},
	}}
	if !reflect.DeepEqual(groups, want) {
		t.Fatalf("only the active group is listed, members in order:\n got %+v\nwant %+v", groups, want)
	}

	addGroup(t, st, groupOne, "author-group", hubwire.SetStatusPending, store.GroupMember{SetID: memberA, SetVersion: 1})
	if groups := listedGroups(t, b); len(groups) != 1 || groups[0].Version != 1 {
		t.Fatalf("a pending version leaves the listed one in place, got %+v", groups)
	}

	pending := &store.Version{FP: "fp-c", TargetsKey: "targets-c", Title: "c", Projection: map[string]interface{}{}, Status: hubwire.SetStatusPending, CreatedAt: now, UpdatedAt: now}
	if err := st.CreateSet(ctx, store.Set{ID: memberC, AuthorHMAC: "author-group", CreatedAt: now, UpdatedAt: now}, pending); err != nil {
		t.Fatal(err)
	}
	addGroup(t, st, groupTwo, "author-group", hubwire.SetStatusActive, store.GroupMember{SetID: memberA, SetVersion: 1}, store.GroupMember{SetID: memberC, SetVersion: 1})
	if groups := listedGroups(t, b); len(groups) != 1 || groups[0].ID != groupOne {
		t.Fatalf("a group with a pending member stays out, got %+v", groups)
	}
	if err := st.Approve(ctx, memberC, 1, now); err != nil {
		t.Fatal(err)
	}
	if groups := listedGroups(t, b); len(groups) != 2 || groups[0].ID != groupOne || groups[1].ID != groupTwo {
		t.Fatalf("the group is listed once its member is, sorted by id, got %+v", groups)
	}

	if err := st.Hide(ctx, memberC, 1, "reports", now); err != nil {
		t.Fatal(err)
	}
	if groups := listedGroups(t, b); len(groups) != 1 || groups[0].ID != groupOne {
		t.Fatalf("a hidden member takes its group out, got %+v", groups)
	}

	if err := st.BanKey(ctx, "author-group", "spam", now); err != nil {
		t.Fatal(err)
	}
	result, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Catalogue.Groups) != 0 || len(result.Catalogue.Sets) != 2 {
		t.Fatalf("a banned author's group is withheld while the members of other authors stay, got %d groups and %d sets", len(result.Catalogue.Groups), len(result.Catalogue.Sets))
	}
}

func TestDeletingAListedMemberSetTakesItsGroupOut(t *testing.T) {
	b, st := testBuilder(t)
	ctx := context.Background()
	addActiveSet(t, st, memberA, "fp-a")
	addActiveSet(t, st, memberB, "fp-b")
	addGroup(t, st, groupOne, "author-group", hubwire.SetStatusActive, store.GroupMember{SetID: memberA, SetVersion: 1, Brought: true}, store.GroupMember{SetID: memberB, SetVersion: 1, Brought: true})
	if groups := listedGroups(t, b); len(groups) != 1 {
		t.Fatalf("the group is listed, got %+v", groups)
	}
	if _, err := st.DeleteSet(ctx, memberB); err != nil {
		t.Fatalf("a set a listed group holds can be deleted: %v", err)
	}
	result, err := b.Build(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Catalogue.Groups) != 0 || len(result.Catalogue.Sets) != 1 {
		t.Fatalf("the group leaves with its deleted member, got %d groups and %d sets", len(result.Catalogue.Groups), len(result.Catalogue.Sets))
	}
	_, versions, err := st.GetGroup(ctx, groupOne)
	if err != nil || len(versions[0].Members) != 2 {
		t.Fatalf("the group keeps its membership, got %+v %v", versions, err)
	}
	if brought, err := st.BroughtVersions(ctx, groupOne, 1); err != nil || len(brought) != 1 || brought[0].SetID != memberA {
		t.Errorf("a deleted member is skipped among the brought versions, got %+v %v", brought, err)
	}
}
