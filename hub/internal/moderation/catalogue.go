package moderation

import (
	"context"
	"slices"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/store"
)

var mirrorDoneCodes = map[string]string{
	ActionApprove: "mirror.approved",
	ActionReject:  "mirror.rejected",
	ActionRemove:  "mirror.removed",
}

func IsMirrorAction(action string) bool {
	_, ok := mirrorDoneCodes[action]
	return ok
}

func (s *Service) Mirror(ctx context.Context, a Actor, id int64, action, reason string) (Result, *store.Mirror, error) {
	if !IsMirrorAction(action) {
		return Result{}, nil, fail(CodeUnknownAction, "mirrors can be approved, rejected or removed", nil)
	}
	reason = CleanText(reason, MaxReasonRunes)
	if action == ActionReject && reason == "" {
		return Result{}, nil, fail(CodeReasonRequired, "a rejection needs a reason", nil)
	}
	var m *store.Mirror
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		var err error
		m, err = t.Mirror(ctx, id)
		if err != nil {
			return err
		}
		switch action {
		case ActionApprove:
			err = t.SetMirrorStatus(ctx, id, store.MirrorApproved, "")
		case ActionReject:
			err = t.SetMirrorStatus(ctx, id, store.MirrorRejected, reason)
			if err == nil {
				err = t.TouchReason(ctx, store.ScopeMirrorReject, reason, s.now())
			}
		case ActionRemove:
			err = t.DeleteMirror(ctx, id)
		}
		if err != nil {
			return err
		}
		e := entry(a, s.now(), "mirror."+action, store.TargetMirror, strconv.FormatInt(id, 10), 0, reason)
		e.Before = map[string]interface{}{"status": m.Status, "url": m.URL}
		switch action {
		case ActionApprove:
			e.After = map[string]interface{}{"status": store.MirrorApproved}
		case ActionReject:
			e.After = map[string]interface{}{"status": store.MirrorRejected}
		}
		_, err = t.Audit(ctx, e)
		return err
	})
	if err != nil {
		return Result{}, nil, err
	}
	s.request("mirrors")
	return Result{Code: mirrorDoneCodes[action], Params: map[string]interface{}{"url": m.URL}, Notice: action + " mirror " + m.URL}, m, nil
}

func canonicalKey(raw string) (string, bool) {
	pub, err := hubwire.DecodeKey(strings.TrimSpace(raw))
	if err != nil {
		return "", false
	}
	return hubwire.EncodeKey(pub), true
}

type RevokeOptions struct {
	Confirm      string
	AllowBuiltin bool
}

func (s *Service) Revoke(ctx context.Context, a Actor, keyID string, opts RevokeOptions) (Result, error) {
	canonical, ok := canonicalKey(keyID)
	if !ok {
		return Result{}, fail(CodeBadKey, "key_id is not an ed25519 key id", nil)
	}
	params := map[string]interface{}{"key_id": canonical}
	if confirmed, ok := canonicalKey(opts.Confirm); !ok || confirmed != canonical {
		return Result{}, fail(CodeConfirmMismatch, "type the key id again to confirm the revocation", params)
	}
	if own, ok := canonicalKey(s.HubKeyID); ok && own == canonical {
		return Result{}, fail(CodeOwnKey, "refusing to revoke the key this hub signs with", params)
	}
	if slices.Contains(s.builtinKeys(), canonical) && !opts.AllowBuiltin {
		return Result{}, fail(CodeBuiltinKey, "the key is built into b4; revoking it needs an explicit override", params)
	}
	added := false
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		var err error
		added, err = t.RevokeKey(ctx, canonical)
		if err != nil || !added {
			return err
		}
		e := entry(a, s.now(), "catalogue.revoke", store.TargetCatalogue, canonical, 0, "")
		e.After = map[string]interface{}{"revoked": canonical}
		_, err = t.Audit(ctx, e)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	if !added {
		return Result{Code: "catalogue.revoke_known", Params: params, Notice: canonical + " was already revoked"}, nil
	}
	s.request("revoke")
	return Result{Code: "catalogue.revoked", Params: params, Notice: "revoked " + canonical}, nil
}

func (s *Service) NewEpoch(ctx context.Context, a Actor) (Result, error) {
	var epoch int64
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		var err error
		epoch, err = t.NewEpoch(ctx, s.now())
		if err != nil {
			return err
		}
		e := entry(a, s.now(), "catalogue.epoch", store.TargetCatalogue, strconv.FormatInt(epoch, 10), 0, "")
		e.After = map[string]interface{}{"epoch": epoch}
		_, err = t.Audit(ctx, e)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	s.request("epoch")
	return Result{Code: "catalogue.epoch", Params: map[string]interface{}{"epoch": epoch}, Notice: "started epoch " + strconv.FormatInt(epoch, 10)}, nil
}

func (s *Service) RecordBuildRequest(ctx context.Context, a Actor) error {
	_, err := s.Store.Audit(ctx, entry(a, s.now(), "catalogue.build", store.TargetCatalogue, "", 0, ""))
	return err
}

func itoa(n int) string {
	return strconv.Itoa(n)
}
