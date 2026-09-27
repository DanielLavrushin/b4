package moderation

import (
	"context"
	"errors"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

var transitions = map[string][]string{
	ActionApprove: {hubwire.SetStatusPending, hubwire.SetStatusRejected},
	ActionReject:  {hubwire.SetStatusPending},
	ActionHide:    {hubwire.SetStatusActive, hubwire.SetStatusPending},
	ActionRestore: {hubwire.SetStatusHidden},
}

var doneCodes = map[string]string{
	ActionApprove: "set.approved",
	ActionReject:  "set.rejected",
	ActionHide:    "set.hidden",
	ActionRestore: "set.restored",
}

var doneWords = map[string]string{
	ActionApprove: "approved",
	ActionReject:  "rejected",
	ActionHide:    "hidden",
	ActionRestore: "restored",
}

type Ref struct {
	SetID        string
	Version      int
	ExpectStatus string
}

type Options struct {
	Reason      string
	Force       bool
	Withdraw    bool
	KeepReports bool
	Partial     bool
	DryRun      bool
}

type Effect struct {
	SetID       string
	Version     int
	Title       string
	From        string
	To          string
	Listed      int
	ListedAfter int
	Withheld    string
	Reports     int
}

type Item struct {
	Effect
	OK     bool
	Code   string
	Params map[string]interface{}
}

var errDryRun = errors.New("dry run")

func IsVersionAction(action string) bool {
	_, ok := transitions[action]
	return ok
}

func catalogueListed(versions []store.Version, withheld bool) int {
	if withheld {
		return 0
	}
	listed := 0
	for _, v := range versions {
		if v.Status == hubwire.SetStatusActive && v.Version > listed {
			listed = v.Version
		}
	}
	return listed
}

func withheldReason(withdrawn, banned bool) string {
	switch {
	case withdrawn:
		return store.WithheldWithdrawn
	case banned:
		return store.WithheldAuthorBanned
	}
	return ""
}

func authorBanned(ctx context.Context, t *store.Tx, set *store.Set) (bool, error) {
	if set.AuthorHMAC == "" {
		return false, nil
	}
	k, err := t.Key(ctx, set.AuthorHMAC)
	if errors.Is(err, store.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return k.Banned, nil
}

func allowedFrom(action, status string) bool {
	for _, from := range transitions[action] {
		if from == status {
			return true
		}
	}
	return false
}

func (s *Service) plan(ctx context.Context, t *store.Tx, action string, r Ref, opts Options) (Effect, *store.Version, error) {
	eff := Effect{SetID: r.SetID, Version: r.Version}
	v, err := t.Version(ctx, r.SetID, r.Version)
	if errors.Is(err, store.ErrNotFound) {
		return eff, nil, fail(CodeNotFound, "no such set version "+ref(r.SetID, r.Version), nil)
	}
	if err != nil {
		return eff, nil, err
	}
	set, err := t.Set(ctx, r.SetID)
	if err != nil {
		return eff, nil, err
	}
	versions, err := t.Versions(ctx, r.SetID)
	if err != nil {
		return eff, nil, err
	}
	banned, err := authorBanned(ctx, t, set)
	if err != nil {
		return eff, nil, err
	}
	withdrawn := !set.WithdrawnAt.IsZero()
	eff.Title = v.Title
	eff.From = v.Status
	eff.Listed = catalogueListed(versions, withdrawn || banned)
	params := map[string]interface{}{"set_id": r.SetID, "version": r.Version, "from": v.Status}
	if r.ExpectStatus != "" && r.ExpectStatus != v.Status {
		return eff, v, fail(CodeStale, ref(r.SetID, r.Version)+" is "+v.Status+" now", params)
	}
	if !allowedFrom(action, v.Status) {
		return eff, v, fail(CodeInvalidTransition, "cannot "+action+" a "+v.Status+" version", params)
	}
	switch action {
	case ActionApprove:
		eff.To = hubwire.SetStatusActive
		if banned && !opts.Force {
			return eff, v, fail(CodeAuthorBanned, "the author of "+ref(r.SetID, r.Version)+" is banned", params)
		}
		if withdrawn && !opts.Force {
			return eff, v, fail(CodeSetWithdrawn, "set "+r.SetID+" is withdrawn", params)
		}
	case ActionReject:
		eff.To = hubwire.SetStatusRejected
		if strings.TrimSpace(opts.Reason) == "" {
			return eff, v, fail(CodeReasonRequired, "a rejection needs a reason", params)
		}
	case ActionHide:
		eff.To = hubwire.SetStatusHidden
	case ActionRestore:
		eff.To = hubwire.SetStatusActive
		if v.HiddenFrom == hubwire.SetStatusPending {
			eff.To = hubwire.SetStatusPending
		}
	}
	after := make([]store.Version, len(versions))
	copy(after, versions)
	for i := range after {
		if after[i].Version == v.Version {
			after[i].Status = eff.To
		}
	}
	withdrawnAfter := withdrawn || (opts.Withdraw && (action == ActionHide || action == ActionReject))
	eff.ListedAfter = catalogueListed(after, withdrawnAfter || banned)
	eff.Withheld = withheldReason(withdrawnAfter, banned)
	if action != ActionApprove && (action == ActionRestore || !opts.KeepReports) {
		n, err := t.OpenReports(ctx, r.SetID, r.Version)
		if err != nil {
			return eff, v, err
		}
		eff.Reports = n
	}
	return eff, v, nil
}

func (s *Service) apply(ctx context.Context, t *store.Tx, a Actor, action string, v *store.Version, eff Effect, opts Options, reason, batchID string) error {
	now := s.now()
	var err error
	resolution := ""
	switch action {
	case ActionApprove:
		err = t.SetStatus(ctx, v.SetID, v.Version, hubwire.SetStatusActive, "", now)
	case ActionReject:
		err = t.SetStatus(ctx, v.SetID, v.Version, hubwire.SetStatusRejected, reason, now)
		resolution = store.ResolutionRejected
	case ActionHide:
		err = t.SetStatus(ctx, v.SetID, v.Version, hubwire.SetStatusHidden, reason, now)
		resolution = store.ResolutionHidden
	case ActionRestore:
		_, err = t.Restore(ctx, v.SetID, v.Version, now)
	}
	if err != nil {
		return err
	}
	after := map[string]interface{}{"status": eff.To, "listed": eff.ListedAfter}
	if reason != "" && action != ActionApprove && action != ActionRestore {
		after["status_reason"] = reason
	}
	switch {
	case action == ActionRestore:
		n, err := t.ResolveVersionReports(ctx, v.SetID, v.Version, store.ReportDismissed, store.ResolutionRestored, "", now)
		if err != nil {
			return err
		}
		after["reports_dismissed"] = n
	case resolution != "" && !opts.KeepReports:
		n, err := t.ResolveVersionReports(ctx, v.SetID, v.Version, store.ReportResolved, resolution, "", now)
		if err != nil {
			return err
		}
		after["reports_resolved"] = n
	}
	withdraw := opts.Withdraw && (action == ActionHide || action == ActionReject)
	if withdraw {
		set, err := t.Set(ctx, v.SetID)
		if err != nil {
			return err
		}
		if set.WithdrawnAt.IsZero() {
			if err := t.WithdrawSet(ctx, v.SetID, reason, now); err != nil {
				return err
			}
			after["withdrawn"] = true
			resolved := 0
			if !opts.KeepReports {
				if resolved, err = t.ResolveSetReports(ctx, v.SetID, store.ReportResolved, store.ResolutionWithdrawn, "", now); err != nil {
					return err
				}
			}
			w := entry(a, now, "set.withdraw", store.TargetSet, v.SetID, 0, reason)
			w.BatchID = batchID
			w.Before = map[string]interface{}{"withdrawn": false}
			w.After = map[string]interface{}{"withdrawn": true, "reports_resolved": resolved}
			if _, err := t.Audit(ctx, w); err != nil {
				return err
			}
		}
	}
	switch action {
	case ActionReject:
		err = t.TouchReason(ctx, store.ScopeReject, reason, now)
	case ActionHide:
		err = t.TouchReason(ctx, store.ScopeHide, reason, now)
	}
	if err != nil {
		return err
	}
	e := entry(a, now, "set."+action, store.TargetSet, v.SetID, v.Version, reason)
	e.BatchID = batchID
	e.Before = map[string]interface{}{"status": v.Status, "listed": eff.Listed}
	if v.StatusReason != "" {
		e.Before["status_reason"] = v.StatusReason
	}
	e.After = after
	if opts.Force {
		e.After["forced"] = true
	}
	_, err = t.Audit(ctx, e)
	return err
}

func (s *Service) Moderate(ctx context.Context, a Actor, action string, refs []Ref, opts Options) (Result, error) {
	if !IsVersionAction(action) {
		return Result{}, fail(CodeUnknownAction, "moderation knows approve, reject, hide and restore", nil)
	}
	if len(refs) == 0 {
		return Result{}, fail(CodeNothingToDo, "no set versions were named", nil)
	}
	if len(refs) > MaxBatch {
		return Result{}, fail(CodeTooMany, "at most 200 versions at once", map[string]interface{}{"max": MaxBatch})
	}
	reason := CleanText(opts.Reason, MaxReasonRunes)
	opts.Reason = reason
	batchID := ""
	if len(refs) > 1 {
		batchID = newBatchID()
	}
	items := make([]Item, 0, len(refs))
	applied := 0
	var single error
	invalid := false
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		for _, r := range refs {
			eff, v, err := s.plan(ctx, t, action, r, opts)
			item := Item{Effect: eff}
			if err != nil {
				var modErr *Error
				if !errors.As(err, &modErr) {
					return err
				}
				item.Code = modErr.Code
				item.Params = modErr.Params
				items = append(items, item)
				invalid = true
				single = err
				continue
			}
			if !opts.DryRun {
				if err := s.apply(ctx, t, a, action, v, eff, opts, reason, batchID); err != nil {
					return err
				}
				applied++
			}
			item.OK = true
			items = append(items, item)
		}
		if invalid && !opts.Partial {
			return errDryRun
		}
		if opts.DryRun {
			return errDryRun
		}
		return nil
	})
	if err != nil && !errors.Is(err, errDryRun) {
		return Result{}, err
	}
	if invalid && !opts.Partial && !opts.DryRun {
		if len(refs) == 1 {
			return Result{Items: items}, single
		}
		return Result{Items: items}, &Error{Code: CodeBatchInvalid, Message: "some versions cannot be changed; nothing was applied", Items: items}
	}
	if applied > 0 {
		s.request("moderation")
	}
	res := Result{Items: items, BatchID: batchID}
	switch {
	case opts.DryRun:
		res.Code = "moderation.preview"
		res.Notice = "preview of " + action
	case len(refs) == 1 && applied == 1:
		it := items[0]
		res.Code = doneCodes[action]
		res.Params = map[string]interface{}{"set_id": it.SetID, "version": it.Version, "title": it.Title, "listed_after": it.ListedAfter}
		res.Notice = doneWords[action] + " " + ref(it.SetID, it.Version)
	default:
		res.Code = "moderation.batch"
		res.Params = map[string]interface{}{"action": action, "applied": applied, "skipped": len(refs) - applied}
		res.Notice = action + ": " + itoa(applied) + " applied, " + itoa(len(refs)-applied) + " skipped"
	}
	return res, nil
}

func (s *Service) Withdraw(ctx context.Context, a Actor, setID, reason string) (Result, error) {
	reason = CleanText(reason, MaxReasonRunes)
	var title string
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		set, err := t.Set(ctx, setID)
		if err != nil {
			return err
		}
		if !set.WithdrawnAt.IsZero() {
			return fail(CodeSetWithdrawn, "set "+setID+" is already withdrawn", map[string]interface{}{"set_id": setID})
		}
		versions, err := t.Versions(ctx, setID)
		if err != nil {
			return err
		}
		title = latestTitle(versions)
		now := s.now()
		if err := t.WithdrawSet(ctx, setID, reason, now); err != nil {
			return err
		}
		n, err := t.ResolveSetReports(ctx, setID, store.ReportResolved, store.ResolutionWithdrawn, "", now)
		if err != nil {
			return err
		}
		if err := t.TouchReason(ctx, store.ScopeWithdraw, reason, now); err != nil {
			return err
		}
		e := entry(a, now, "set.withdraw", store.TargetSet, setID, 0, reason)
		e.Before = map[string]interface{}{"withdrawn": false, "listed": catalogueListed(versions, false)}
		e.After = map[string]interface{}{"withdrawn": true, "listed": 0, "reports_resolved": n}
		_, err = t.Audit(ctx, e)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	s.request("moderation")
	return Result{Code: "set.withdrawn", Params: map[string]interface{}{"set_id": setID, "title": title}, Notice: "withdrew " + setID}, nil
}

func (s *Service) Reinstate(ctx context.Context, a Actor, setID string) (Result, error) {
	var title string
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		set, err := t.Set(ctx, setID)
		if err != nil {
			return err
		}
		if set.WithdrawnAt.IsZero() {
			return fail(CodeNotWithdrawn, "set "+setID+" is not withdrawn", map[string]interface{}{"set_id": setID})
		}
		versions, err := t.Versions(ctx, setID)
		if err != nil {
			return err
		}
		title = latestTitle(versions)
		if err := t.ReinstateSet(ctx, setID); err != nil {
			return err
		}
		e := entry(a, s.now(), "set.reinstate", store.TargetSet, setID, 0, "")
		e.Before = map[string]interface{}{"withdrawn": true, "withdraw_reason": set.WithdrawReason}
		e.After = map[string]interface{}{"withdrawn": false, "listed": catalogueListed(versions, false)}
		_, err = t.Audit(ctx, e)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	s.request("moderation")
	return Result{Code: "set.reinstated", Params: map[string]interface{}{"set_id": setID, "title": title}, Notice: "reinstated " + setID}, nil
}

func (s *Service) Delete(ctx context.Context, a Actor, setID, confirm string) (Result, error) {
	if !hubdata.ValidSetID(setID) {
		return Result{}, fail(CodeNotFound, "the address does not name a set", nil)
	}
	if strings.TrimSpace(confirm) != setID {
		return Result{}, fail(CodeConfirmMismatch, "type the set id to confirm the deletion", nil)
	}
	var title string
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		set, err := t.Set(ctx, setID)
		if err != nil {
			return err
		}
		versions, err := t.Versions(ctx, setID)
		if err != nil {
			return err
		}
		title = latestTitle(versions)
		if _, err := t.DeleteSet(ctx, setID); err != nil {
			return err
		}
		e := entry(a, s.now(), "set.delete", store.TargetSet, setID, 0, "")
		e.Before = map[string]interface{}{"title": title, "versions": len(versions), "author": hubdata.AuthorLabel(set.AuthorHMAC), "listed": catalogueListed(versions, false)}
		_, err = t.Audit(ctx, e)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	s.request("moderation")
	return Result{Code: "set.deleted", Params: map[string]interface{}{"set_id": setID, "title": title}, Notice: "deleted " + setID + " permanently"}, nil
}

func latestTitle(versions []store.Version) string {
	if len(versions) == 0 {
		return ""
	}
	return versions[len(versions)-1].Title
}
