package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	MaxTitleRunes       = 120
	MaxDescriptionRunes = 2000
	MaxHintRunes        = 32
)

func (s *Service) share(ctx context.Context, entry record) Response {
	var body hubwire.ShareBody
	if err := json.Unmarshal(entry.rec.Body, &body); err != nil {
		return fail(http.StatusBadRequest, CodeBadRecord, "share body does not decode: "+err.Error())
	}
	imp, err := hubwire.Open(&body.Envelope, hubwire.OpenOptions{Now: func() time.Time { return entry.now }})
	if err != nil {
		var payloadErr *hubwire.PayloadError
		if errors.As(err, &payloadErr) {
			return fail(http.StatusBadRequest, CodePayloadInvalid, err.Error())
		}
		return fail(http.StatusBadRequest, CodeInvalidSet, err.Error())
	}
	projection, report, err := hubwire.Scrub(&imp.Set)
	if err != nil {
		return fail(http.StatusBadRequest, CodeInvalidSet, err.Error())
	}
	for _, w := range report.Warnings {
		if w.Code == "no_targets" {
			return fail(http.StatusBadRequest, CodeInvalidSet, "the set has no targets")
		}
	}
	fp := imp.Fingerprint
	targetsKey := TargetsKey(projection)

	if existing, err := s.Store.FindDuplicate(ctx, fp, targetsKey); err == nil {
		if err := s.uploadVote(ctx, entry, existing, body); err != nil {
			return internalError(err)
		}
		if err := s.remember(ctx, entry, existing.SetID, existing.Version); err != nil {
			return internalError(err)
		}
		return Response{Status: http.StatusConflict, Body: map[string]interface{}{
			"code":    CodeDuplicateStrategy,
			"error":   "a set with the same strategy and targets already exists",
			"set_id":  existing.SetID,
			"version": existing.Version,
		}}
	} else if !errors.Is(err, store.ErrNotFound) {
		return internalError(err)
	}

	payloads := make([]hubwire.BlobRef, 0, len(imp.Payloads))
	for _, p := range imp.Payloads {
		hash, err := s.Blobs.Put(p.Data)
		if err != nil {
			return internalError(err)
		}
		payloads = append(payloads, hubwire.BlobRef{SHA256: hash, Protocol: p.Protocol, Domain: p.Domain, Size: len(p.Data)})
	}

	engine := body.Engine
	if engine == "" {
		engine = body.Envelope.Engine
	}
	b4Version := body.B4Version
	if b4Version == "" {
		b4Version = body.Envelope.B4Version
	}
	version := &store.Version{
		FP:              fp,
		TargetsKey:      targetsKey,
		Title:           clip(imp.Set.Name, MaxTitleRunes),
		Description:     clip(body.Envelope.Description, MaxDescriptionRunes),
		Projection:      projection,
		Payloads:        payloads,
		Flags:           Flags(projection, payloads),
		Geo:             body.Envelope.Geo,
		B4Min:           hubwire.MinVersion(projection),
		B4Version:       clip(b4Version, MaxHintRunes),
		Engine:          clip(engine, MaxHintRunes),
		Family:          Family(&imp.Set),
		Status:          hubwire.SetStatusPending,
		RecordID:        entry.id,
		UploaderHMAC:    entry.keyHMAC,
		ASNObserved:     entry.origin.ASN,
		CountryObserved: entry.origin.Country,
		ASNHint:         clip(body.ASNHint, MaxHintRunes),
		CountryHint:     strings.ToUpper(clip(body.CountryHint, 2)),
		CreatedAt:       entry.now,
		UpdatedAt:       entry.now,
	}

	derived := body.Envelope.DerivedFrom
	if derived != nil && !hubdata.ValidSetID(derived.ID) {
		derived = nil
	}
	if derived != nil {
		parent, _, err := s.Store.GetSet(ctx, derived.ID)
		if err == nil && parent.AuthorHMAC == entry.keyHMAC {
			version.SetID = parent.ID
			if err := s.Store.AddVersion(ctx, version); err != nil {
				return internalError(err)
			}
			return s.accepted(ctx, entry, version, body)
		} else if err != nil && !errors.Is(err, store.ErrNotFound) {
			return internalError(err)
		}
	}
	sibling, err := s.authorSibling(ctx, entry.keyHMAC, version)
	if err != nil {
		return internalError(err)
	}
	if sibling != nil {
		version.SetID = sibling.ID
		if err := s.Store.AddVersion(ctx, version); err != nil {
			return internalError(err)
		}
		return s.accepted(ctx, entry, version, body)
	}
	id, err := hubdata.NewSetID(entry.now)
	if err != nil {
		return internalError(err)
	}
	set := store.Set{ID: id, AuthorHMAC: entry.keyHMAC, CreatedAt: entry.now, UpdatedAt: entry.now}
	if derived != nil {
		set.DerivedFromID = derived.ID
		set.DerivedFromVersion = derived.Version
	}
	if err := s.Store.CreateSet(ctx, set, version); err != nil {
		return internalError(err)
	}
	return s.accepted(ctx, entry, version, body)
}

func (s *Service) authorSibling(ctx context.Context, authorHMAC string, version *store.Version) (*store.Set, error) {
	sets, err := s.Store.SetsByAuthor(ctx, authorHMAC)
	if err != nil {
		return nil, err
	}
	targets := TargetSet(version.Projection)
	var match *store.Set
	for i := range sets {
		latest, err := s.Store.LatestVersion(ctx, sets[i].ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if latest.TargetsKey != version.TargetsKey && !(sameTitle(latest.Title, version.Title) && targetsOverlap(targets, TargetSet(latest.Projection))) {
			continue
		}
		if match == nil || sets[i].UpdatedAt.After(match.UpdatedAt) {
			match = &sets[i]
		}
	}
	return match, nil
}

func (s *Service) accepted(ctx context.Context, entry record, version *store.Version, body hubwire.ShareBody) Response {
	if err := s.uploadVote(ctx, entry, version, body); err != nil {
		return internalError(err)
	}
	if err := s.remember(ctx, entry, version.SetID, version.Version); err != nil {
		return internalError(err)
	}
	return Response{Status: http.StatusAccepted, Body: map[string]interface{}{
		"id":      entry.id,
		"kind":    entry.rec.Kind,
		"set_id":  version.SetID,
		"version": version.Version,
		"status":  version.Status,
	}}
}

func (s *Service) uploadVote(ctx context.Context, entry record, version *store.Version, body hubwire.ShareBody) error {
	return s.Store.UpsertVote(ctx, store.Vote{
		RecordID:        entry.id,
		SetID:           version.SetID,
		Version:         version.Version,
		FP:              version.FP,
		KeyHMAC:         entry.keyHMAC,
		Kind:            score.KindUpload,
		Weight:          score.Weights[score.KindUpload],
		ASNObserved:     entry.origin.ASN,
		CountryObserved: entry.origin.Country,
		ASNHint:         clip(body.ASNHint, MaxHintRunes),
		CountryHint:     strings.ToUpper(clip(body.CountryHint, 2)),
		OriginVerified:  entry.origin.verified(),
		B4Version:       clip(body.B4Version, MaxHintRunes),
		Engine:          clip(body.Engine, MaxHintRunes),
		Bucket:          score.BucketOf(entry.now),
		ReceivedAt:      entry.now,
	})
}
