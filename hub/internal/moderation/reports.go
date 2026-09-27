package moderation

import (
	"context"
	"strconv"

	"github.com/daniellavrushin/b4hub/internal/store"
)

var reportStates = map[string]string{
	ActionDismiss: store.ReportDismissed,
	ActionResolve: store.ReportResolved,
	ActionReopen:  store.ReportOpen,
}

func IsReportAction(action string) bool {
	_, ok := reportStates[action]
	return ok
}

func (s *Service) Reports(ctx context.Context, a Actor, ids []int64, action, note string) (Result, error) {
	state, ok := reportStates[action]
	if !ok {
		return Result{}, fail(CodeUnknownAction, "reports can be dismissed, resolved or reopened", nil)
	}
	if len(ids) == 0 {
		return Result{}, fail(CodeNothingToDo, "no reports were named", nil)
	}
	if len(ids) > MaxBatch {
		return Result{}, fail(CodeTooMany, "at most 200 reports at once", map[string]interface{}{"max": MaxBatch})
	}
	ids = uniqueIDs(ids)
	note = CleanText(note, MaxReasonRunes)
	changed := 0
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		reports, err := t.Reports(ctx, ids)
		if err != nil {
			return err
		}
		if len(reports) != len(ids) {
			return fail(CodeNotFound, "some reports do not exist", nil)
		}
		now := s.now()
		batchID := ""
		if len(reports) > 1 {
			batchID = newBatchID()
		}
		for _, r := range reports {
			if r.State == state {
				continue
			}
			if err := t.SetReportState(ctx, r.ID, state, "", note, now); err != nil {
				return err
			}

			e := entry(a, now, "report."+action, store.TargetReport, strconv.FormatInt(r.ID, 10), r.Version, note)
			e.BatchID = batchID
			e.Before = map[string]interface{}{"state": r.State, "set_id": r.SetID}
			e.After = map[string]interface{}{"state": state}
			if _, err := t.Audit(ctx, e); err != nil {
				return err
			}
			changed++
		}
		if action == ActionDismiss && changed > 0 {
			return t.TouchReason(ctx, store.ScopeReportDismiss, note, now)
		}
		return nil
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Code: "reports.updated", Params: map[string]interface{}{"count": changed, "state": state}, Notice: action + " " + itoa(changed) + " report(s)"}, nil
}

func (s *Service) VersionReports(ctx context.Context, a Actor, setID string, version int, action, note string) (Result, error) {
	state, ok := reportStates[action]
	if !ok || state == store.ReportOpen {
		return Result{}, fail(CodeUnknownAction, "a version's open reports can be dismissed or resolved", nil)
	}
	note = CleanText(note, MaxReasonRunes)
	changed := 0
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		if _, err := t.Version(ctx, setID, version); err != nil {
			return err
		}
		now := s.now()
		var err error
		changed, err = t.ResolveVersionReports(ctx, setID, version, state, "", note, now)
		if err != nil || changed == 0 {
			return err
		}
		e := entry(a, now, "set.reports_"+action, store.TargetSet, setID, version, note)
		e.After = map[string]interface{}{"state": state, "count": changed}
		_, err = t.Audit(ctx, e)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	return Result{Code: "reports.updated", Params: map[string]interface{}{"count": changed, "state": state}, Notice: action + " " + itoa(changed) + " report(s)"}, nil
}

func uniqueIDs(ids []int64) []int64 {
	seen := make(map[int64]bool, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	return out
}
