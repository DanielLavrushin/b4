package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	MaxTitleRunes       = hubdata.MaxTitleRunes
	MaxDescriptionRunes = hubdata.MaxDescriptionRunes
	MaxHintRunes        = 32
)

type member struct {
	env        *hubwire.Envelope
	imp        *hubwire.Imported
	projection map[string]interface{}
	fp         string
	targetsKey string
}

type hints struct {
	ASN       string
	Country   string
	Engine    string
	B4Version string
}

func openMember(env *hubwire.Envelope, now time.Time) (*member, *Response) {
	refuse := func(code, message string) (*member, *Response) {
		r := fail(http.StatusBadRequest, code, message)
		return nil, &r
	}
	imp, err := hubwire.Open(env, hubwire.OpenOptions{Now: func() time.Time { return now }})
	if err != nil {
		var payloadErr *hubwire.PayloadError
		if errors.As(err, &payloadErr) {
			return refuse(CodePayloadInvalid, err.Error())
		}
		return refuse(CodeInvalidSet, err.Error())
	}
	projection, report, err := hubwire.Scrub(&imp.Set)
	if err != nil {
		return refuse(CodeInvalidSet, err.Error())
	}
	for _, w := range report.Warnings {
		switch w.Code {
		case "no_targets":
			return refuse(CodeInvalidSet, "the set has no targets")
		case "udp_l7_tcp_ports":
			return refuse(CodeInvalidSet, "a set that handles only calls has no targets and takes no TCP port filter")
		}
	}
	return &member{env: env, imp: imp, projection: projection, fp: imp.Fingerprint, targetsKey: store.TargetsKey(projection)}, nil
}

func (s *Service) newVersion(entry record, m *member, h hints) (*store.Version, error) {
	payloads := make([]hubwire.BlobRef, 0, len(m.imp.Payloads))
	for _, p := range m.imp.Payloads {
		hash, err := s.Blobs.Put(p.Data)
		if err != nil {
			return nil, err
		}
		payloads = append(payloads, hubwire.BlobRef{SHA256: hash, Protocol: p.Protocol, Domain: p.Domain, Size: len(p.Data)})
	}
	engine := h.Engine
	if engine == "" {
		engine = m.env.Engine
	}
	b4Version := h.B4Version
	if b4Version == "" {
		b4Version = m.env.B4Version
	}
	return &store.Version{
		FP:              m.fp,
		TargetsKey:      m.targetsKey,
		Title:           clip(m.imp.Set.Name, MaxTitleRunes),
		Description:     clip(m.env.Description, MaxDescriptionRunes),
		Projection:      m.projection,
		Payloads:        payloads,
		Flags:           Flags(m.projection, payloads),
		Geo:             hubwire.BuiltInGeo(m.env.Geo),
		B4Min:           hubwire.MinVersion(m.projection),
		B4Version:       clip(b4Version, MaxHintRunes),
		Engine:          clip(engine, MaxHintRunes),
		Family:          Family(&m.imp.Set),
		Status:          hubwire.SetStatusPending,
		RecordID:        entry.id,
		UploaderHMAC:    entry.keyHMAC,
		ASNObserved:     entry.origin.ASN,
		CountryObserved: entry.origin.Country,
		ASNHint:         clip(h.ASN, MaxHintRunes),
		CountryHint:     strings.ToUpper(clip(h.Country, 2)),
		CreatedAt:       entry.now,
		UpdatedAt:       entry.now,
	}, nil
}

func derivedSet(env *hubwire.Envelope) *hubwire.Origin {
	return derivedOrigin(env.DerivedFrom)
}

func derivedOrigin(o *hubwire.Origin) *hubwire.Origin {
	if o == nil || !hubdata.ValidSetID(o.ID) {
		return nil
	}
	d := *o
	if d.Version < 1 || d.Version > math.MaxInt32 {
		d.Version = 0
	}
	return &d
}

func (s *Service) share(ctx context.Context, entry record) Response {
	var body hubwire.ShareBody
	if err := json.Unmarshal(entry.rec.Body, &body); err != nil {
		return fail(http.StatusBadRequest, CodeBadRecord, "share body does not decode: "+err.Error())
	}
	m, refused := openMember(&body.Envelope, entry.now)
	if refused != nil {
		return *refused
	}
	h := hints{ASN: body.ASNHint, Country: body.CountryHint, Engine: body.Engine, B4Version: body.B4Version}

	if existing, err := s.Store.FindDuplicate(ctx, m.fp, m.targetsKey); err == nil {
		if err := s.Store.UpsertVote(ctx, uploadVote(entry, existing, h)); err != nil {
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

	version, err := s.newVersion(entry, m, h)
	if err != nil {
		return internalError(err)
	}

	derived := derivedSet(&body.Envelope)
	if derived != nil {
		parent, _, err := s.Store.GetSet(ctx, derived.ID)
		if err == nil && parent.AuthorHMAC == entry.keyHMAC {
			version.SetID = parent.ID
			if err := s.Store.AddVersion(ctx, version); err != nil {
				return internalError(err)
			}
			return s.accepted(ctx, entry, version, h)
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
		return s.accepted(ctx, entry, version, h)
	}
	id, err := hubdata.NewSetID()
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
	return s.accepted(ctx, entry, version, h)
}

func (s *Service) authorSibling(ctx context.Context, authorHMAC string, version *store.Version) (*store.Set, error) {
	sets, err := s.Store.SetsByAuthor(ctx, authorHMAC)
	if err != nil {
		return nil, err
	}
	var match *store.Set
	for i := range sets {
		latest, err := s.Store.LatestVersion(ctx, sets[i].ID)
		if errors.Is(err, store.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if latest.TargetsKey != version.TargetsKey && !(sameTitle(latest.Title, version.Title) && siblingTargets(version.Projection, latest.Projection)) {
			continue
		}
		if match == nil || sets[i].UpdatedAt.After(match.UpdatedAt) {
			match = &sets[i]
		}
	}
	return match, nil
}

func (s *Service) accepted(ctx context.Context, entry record, version *store.Version, h hints) Response {
	if err := s.Store.UpsertVote(ctx, uploadVote(entry, version, h)); err != nil {
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

func uploadVote(entry record, version *store.Version, h hints) store.Vote {
	return store.Vote{
		RecordID:        entry.id,
		SetID:           version.SetID,
		Version:         version.Version,
		FP:              version.FP,
		KeyHMAC:         entry.keyHMAC,
		Kind:            score.KindUpload,
		Weight:          score.Weights[score.KindUpload],
		ASNObserved:     entry.origin.ASN,
		CountryObserved: entry.origin.Country,
		ASNHint:         clip(h.ASN, MaxHintRunes),
		CountryHint:     strings.ToUpper(clip(h.Country, 2)),
		OriginVerified:  entry.origin.verified(),
		B4Version:       clip(h.B4Version, MaxHintRunes),
		Engine:          clip(h.Engine, MaxHintRunes),
		Bucket:          score.BucketOf(entry.now),
		ReceivedAt:      entry.now,
	}
}
