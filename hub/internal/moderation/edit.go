package moderation

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	CodeNotPending = "not_pending"
	CodeDuplicate  = "duplicate_strategy"
)

func (s *Service) Edit(ctx context.Context, a Actor, r Ref, edit store.VersionEdit, approve bool) (Result, error) {
	edit.Approve = false
	edit.Note = CleanText(edit.Note, MaxReasonRunes)
	var title string
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		before, err := t.Version(ctx, r.SetID, r.Version)
		if errors.Is(err, store.ErrNotFound) {
			return fail(CodeNotFound, "no such set version "+ref(r.SetID, r.Version), nil)
		}
		if err != nil {
			return err
		}
		if approve {
			if _, _, err := s.plan(ctx, t, ActionApprove, r, Options{}); err != nil {
				return err
			}
		}
		now := s.now()
		err = t.EditVersion(ctx, r.SetID, r.Version, edit, now)
		var duplicate *store.DuplicateError
		switch {
		case errors.Is(err, store.ErrNotPending):
			return fail(CodeNotPending, "only a pending version can be edited", map[string]interface{}{"status": before.Status})
		case errors.Is(err, store.ErrStale):
			return fail(CodeStale, "the version changed since the dialog was opened", nil)
		case errors.As(err, &duplicate):
			return fail(CodeDuplicate, err.Error(), map[string]interface{}{"set_id": duplicate.SetID, "version": duplicate.Version})
		case err != nil:
			return err
		}
		title = edit.Title
		e := entry(a, now, "set.edit", store.TargetSet, r.SetID, r.Version, edit.Note)
		e.Before = map[string]interface{}{"title": before.Title, "description": before.Description, "fp": before.FP, "targets_key": before.TargetsKey}
		e.After = map[string]interface{}{"title": edit.Title, "description": edit.Description, "fp": edit.FP, "targets_key": edit.TargetsKey, "fp_changed": edit.FP != before.FP}
		if _, err := t.Audit(ctx, e); err != nil {
			return err
		}
		if !approve {
			return nil
		}
		eff, v, err := s.plan(ctx, t, ActionApprove, r, Options{})
		if err != nil {
			return err
		}
		return s.apply(ctx, t, a, ActionApprove, v, eff, Options{}, "", "")
	})
	if err != nil {
		return Result{}, err
	}
	params := map[string]interface{}{"set_id": r.SetID, "version": r.Version, "title": title}
	if approve {
		s.request("moderation")
		return Result{Code: "set.edited_approved", Params: params, Notice: "edited and approved " + ref(r.SetID, r.Version)}, nil
	}
	return Result{Code: "set.edited", Params: params, Notice: "edited " + ref(r.SetID, r.Version)}, nil
}

const (
	CodeNotEditable   = "not_editable"
	CodeTitleRequired = "title_required"
	CodeUnchanged     = "unchanged"
	CodeWouldChange   = "would_change_strategy"
)

type TextEdit struct {
	Title       string
	Description string
	Note        string
	Expect      time.Time
}

func (s *Service) EditText(ctx context.Context, a Actor, r Ref, e TextEdit) (Result, error) {
	title := CleanText(e.Title, hubdata.MaxTitleRunes)
	description := CleanText(e.Description, hubdata.MaxDescriptionRunes)
	note := CleanText(e.Note, MaxReasonRunes)
	if title == "" {
		return Result{}, fail(CodeTitleRequired, "a set needs a title", nil)
	}
	listed := false
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		v, err := t.Version(ctx, r.SetID, r.Version)
		if errors.Is(err, store.ErrNotFound) {
			return fail(CodeNotFound, "no such set version "+ref(r.SetID, r.Version), nil)
		}
		if err != nil {
			return err
		}
		if v.Title == title && v.Description == description {
			return fail(CodeUnchanged, "nothing differs", nil)
		}
		projection, err := withName(v.Projection, title)
		if err != nil {
			return err
		}
		if hubwire.Fingerprint(projection) != hubwire.Fingerprint(v.Projection) {
			return fail(CodeWouldChange, "renaming would change the strategy fingerprint", nil)
		}
		now := s.now()
		before, err := t.EditText(ctx, r.SetID, r.Version, store.TextEdit{Title: title, Description: description, Note: note, Projection: projection, Expect: e.Expect}, now)
		switch {
		case errors.Is(err, store.ErrNotEditable):
			return fail(CodeNotEditable, "a rejected version cannot be edited", map[string]interface{}{"status": before.Status})
		case errors.Is(err, store.ErrStale):
			return fail(CodeStale, "the version changed since the dialog was opened", nil)
		case err != nil:
			return err
		}
		listed = before.Status == hubwire.SetStatusActive
		entry := entry(a, now, "set.text", store.TargetSet, r.SetID, r.Version, note)
		entry.Before = map[string]interface{}{"title": before.Title, "description": before.Description}
		entry.After = map[string]interface{}{"title": title, "description": description}
		_, err = t.Audit(ctx, entry)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	if listed {
		s.request("moderation")
	}
	return Result{Code: "set.text_edited", Params: map[string]interface{}{"set_id": r.SetID, "version": r.Version, "title": title}, Notice: "retitled " + ref(r.SetID, r.Version)}, nil
}

func withName(projection map[string]interface{}, name string) (map[string]interface{}, error) {
	raw, err := json.Marshal(projection)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	out["name"] = name
	return out, nil
}
