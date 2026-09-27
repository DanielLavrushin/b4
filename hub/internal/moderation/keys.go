package moderation

import (
	"context"
	"errors"
	"strings"

	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

var keyDoneCodes = map[string]string{
	ActionBan:     "key.banned",
	ActionUnban:   "key.unbanned",
	ActionTrust:   "key.trusted",
	ActionUntrust: "key.untrusted",
}

type KeyOptions struct {
	Reason string
	Create bool
}

func IsKeyAction(action string) bool {
	_, ok := keyDoneCodes[action]
	return ok
}

func (s *Service) Key(ctx context.Context, a Actor, key, action string, opts KeyOptions) (Result, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if !hubdata.ValidKeyHMAC(key) {
		return Result{}, fail(CodeBadKey, "a key is named by its 64-character HMAC", nil)
	}
	if !IsKeyAction(action) {
		return Result{}, fail(CodeUnknownAction, "keys can be banned, unbanned, trusted or untrusted", nil)
	}
	reason := CleanText(opts.Reason, MaxReasonRunes)
	label := hubdata.AuthorLabel(key)
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		k, err := t.Key(ctx, key)
		if errors.Is(err, store.ErrNotFound) {
			if !opts.Create || (action != ActionBan && action != ActionTrust) {
				return fail(CodeUnknownKey, "the hub has never seen key "+label, map[string]interface{}{"key": label})
			}
			k = &store.Key{KeyHMAC: key}
		} else if err != nil {
			return err
		}
		now := s.now()
		switch action {
		case ActionBan:
			err = t.BanKey(ctx, key, reason, now)
			if err == nil {
				err = t.TouchReason(ctx, store.ScopeBan, reason, now)
			}
		case ActionUnban:
			err = t.UnbanKey(ctx, key)
		case ActionTrust:
			err = t.TrustKey(ctx, key, now)
		case ActionUntrust:
			err = t.UntrustKey(ctx, key)
		}
		if err != nil {
			return err
		}
		e := entry(a, now, "key."+action, store.TargetKey, key, 0, reason)
		e.Before = map[string]interface{}{"banned": k.Banned, "trusted": k.Trusted}
		if k.BanReason != "" {
			e.Before["ban_reason"] = k.BanReason
		}
		after := map[string]interface{}{"banned": k.Banned, "trusted": k.Trusted}
		switch action {
		case ActionBan:
			after["banned"] = true
		case ActionUnban:
			after["banned"] = false
		case ActionTrust:
			after["trusted"] = true
		case ActionUntrust:
			after["trusted"] = false
		}
		e.After = after
		_, err = t.Audit(ctx, e)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	if action == ActionBan || action == ActionUnban {
		s.request("moderation")
	}
	return Result{Code: keyDoneCodes[action], Params: map[string]interface{}{"key": label}, Notice: action + " " + label}, nil
}

func (s *Service) KeyImpact(ctx context.Context, key string) (*store.KeyImpact, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if !hubdata.ValidKeyHMAC(key) {
		return nil, fail(CodeBadKey, "a key is named by its 64-character HMAC", nil)
	}
	return s.Store.KeyImpact(ctx, key)
}

func (s *Service) KeyProfile(ctx context.Context, a Actor, key string, p store.KeyProfile) (Result, error) {
	key = strings.ToLower(strings.TrimSpace(key))
	if !hubdata.ValidKeyHMAC(key) {
		return Result{}, fail(CodeBadKey, "a key is named by its 64-character HMAC", nil)
	}
	if !store.ValidTag(p.Tag) {
		return Result{}, fail(CodeBadTag, "a tag is staff, test or empty", nil)
	}
	p.Name = CleanText(p.Name, store.MaxKeyNameRunes)
	p.Note = CleanText(p.Note, store.MaxKeyNoteRunes)
	label := hubdata.AuthorLabel(key)
	rescore := false
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		now := s.now()
		before, err := t.SetKeyProfile(ctx, key, p, now)
		if errors.Is(err, store.ErrNotFound) {
			return fail(CodeUnknownKey, "the hub has never seen key "+label, map[string]interface{}{"key": label})
		}
		if err != nil {
			return err
		}
		rescore = (before.Tag == store.TagTest) != (p.Tag == store.TagTest)
		e := entry(a, now, "key.profile", store.TargetKey, key, 0, "")
		e.Before = map[string]interface{}{"name": before.Name, "tag": before.Tag, "note_changed": false}
		e.After = map[string]interface{}{"name": p.Name, "tag": p.Tag, "note_changed": before.Note != p.Note}
		_, err = t.Audit(ctx, e)
		return err
	})
	if err != nil {
		return Result{}, err
	}
	if rescore {
		s.request("moderation")
	}
	return Result{Code: "key.profile", Params: map[string]interface{}{"key": label}, Notice: "saved the profile of " + label}, nil
}
