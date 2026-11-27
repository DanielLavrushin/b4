package store

import (
	"context"
	"testing"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
)

func TestAGroupVersionStatusFollowsTheSetRules(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 9, 12, 12, 0, 0, 0, time.UTC)
	const id = "01ARZ3NDEKTSV4RRFFQ69G5GA1"
	err := st.Update(ctx, func(tx *Tx) error {
		v := &GroupVersion{Title: "youtube", Status: hubwire.SetStatusPending, CreatedAt: now, UpdatedAt: now,
			Members: []GroupMember{{SetID: "01ARZ3NDEKTSV4RRFFQ69G5FA1", SetVersion: 1, Brought: true}}}
		return tx.CreateGroup(ctx, Group{ID: id, AuthorHMAC: "author", CreatedAt: now, UpdatedAt: now}, v)
	})
	if err != nil {
		t.Fatal(err)
	}
	step := func(fn func(*Tx) error) {
		t.Helper()
		before, _ := st.DirtyGeneration(ctx)
		if err := st.Update(ctx, fn); err != nil {
			t.Fatal(err)
		}
		if after, _ := st.DirtyGeneration(ctx); after == before {
			t.Fatalf("a group status change marks the catalogue dirty")
		}
	}
	restored := ""
	step(func(tx *Tx) error { return tx.SetGroupStatus(ctx, id, 1, hubwire.SetStatusHidden, "spam", now) })
	step(func(tx *Tx) error {
		var err error
		restored, err = tx.RestoreGroup(ctx, id, 1, now)
		return err
	})
	if restored != hubwire.SetStatusPending {
		t.Errorf("a version hidden while pending is restored to pending, got %q", restored)
	}
	step(func(tx *Tx) error { return tx.SetGroupStatus(ctx, id, 1, hubwire.SetStatusActive, "", now) })
	step(func(tx *Tx) error { return tx.SetGroupStatus(ctx, id, 1, hubwire.SetStatusHidden, "reports", now) })
	step(func(tx *Tx) error { return tx.SetGroupStatus(ctx, id, 1, hubwire.SetStatusHidden, "again", now) })
	step(func(tx *Tx) error {
		var err error
		restored, err = tx.RestoreGroup(ctx, id, 1, now)
		return err
	})
	if restored != hubwire.SetStatusActive {
		t.Errorf("a version hidden twice after approval is restored to active, got %q", restored)
	}
	v, err := st.GetGroupVersion(ctx, id, 1)
	if err != nil || v.Status != hubwire.SetStatusActive || v.HiddenFrom != "" || len(v.Members) != 1 || !v.Members[0].Brought {
		t.Errorf("unexpected group version %+v %v", v, err)
	}
	if err := st.Update(ctx, func(tx *Tx) error { return tx.SetGroupStatus(ctx, id, 2, hubwire.SetStatusActive, "", now) }); err != ErrNotFound {
		t.Errorf("a missing group version is not found, got %v", err)
	}
}
