package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	MaxReasonRunes = 500
	ReportsToHide  = 3
	ReasonReports  = "reports"
)

func voteKind(kind string) (string, bool) {
	switch kind {
	case hubwire.VoteWorks:
		return score.KindManualWorks, true
	case hubwire.VoteBroken:
		return score.KindManualBroken, true
	}
	return "", false
}

func (s *Service) lookupVersion(ctx context.Context, setID string, version int) (*store.Version, *Response) {
	if !hubdata.ValidSetID(setID) || version <= 0 {
		r := fail(http.StatusBadRequest, CodeBadRecord, "set_id or version is malformed")
		return nil, &r
	}
	v, err := s.Store.GetVersion(ctx, setID, version)
	if errors.Is(err, store.ErrNotFound) {
		r := fail(http.StatusBadRequest, CodeUnknownSet, "no such set version")
		return nil, &r
	}
	if err != nil {
		r := internalError(err)
		return nil, &r
	}
	return v, nil
}

func (s *Service) vote(ctx context.Context, entry record) Response {
	var body hubwire.VoteBody
	if err := json.Unmarshal(entry.rec.Body, &body); err != nil {
		return fail(http.StatusBadRequest, CodeBadRecord, "vote body does not decode: "+err.Error())
	}
	kind, ok := voteKind(body.Kind)
	if !ok {
		return fail(http.StatusBadRequest, CodeBadRecord, "vote kind must be works or broken")
	}
	v, failure := s.lookupVersion(ctx, body.SetID, body.Version)
	if failure != nil {
		return *failure
	}
	if v.Status != hubwire.SetStatusActive {
		return fail(http.StatusBadRequest, CodeNotActive, "votes are accepted only for listed set versions")
	}
	if !strings.EqualFold(strings.TrimSpace(body.FP), v.FP) {
		return fail(http.StatusBadRequest, CodeFingerprint, "the fingerprint does not match this set version")
	}
	domain := sni.NormalizeDomain(body.Domain)
	if domain != "" && !domainTargeted(domain, store.TargetList(v.Projection, "sni_domains")) {
		domain = ""
	}
	err := s.Store.UpsertVote(ctx, store.Vote{
		RecordID:        entry.id,
		SetID:           v.SetID,
		Version:         v.Version,
		FP:              v.FP,
		KeyHMAC:         entry.keyHMAC,
		Kind:            kind,
		Weight:          score.Weights[kind],
		ASNObserved:     entry.origin.ASN,
		CountryObserved: entry.origin.Country,
		ASNHint:         clip(body.ASNHint, MaxHintRunes),
		CountryHint:     strings.ToUpper(clip(body.CountryHint, 2)),
		OriginVerified:  entry.origin.verified(),
		Domain:          domain,
		B4Version:       clip(body.B4Version, MaxHintRunes),
		Engine:          clip(body.Engine, MaxHintRunes),
		Bucket:          score.BucketOf(entry.now),
		ReceivedAt:      entry.now,
	})
	if err != nil {
		return internalError(err)
	}
	if err := s.remember(ctx, entry, v.SetID, v.Version); err != nil {
		return internalError(err)
	}
	return Response{Status: http.StatusAccepted, Body: map[string]interface{}{"id": entry.id, "kind": entry.rec.Kind}}
}

func (s *Service) report(ctx context.Context, entry record) Response {
	var body hubwire.ReportBody
	if err := json.Unmarshal(entry.rec.Body, &body); err != nil {
		return fail(http.StatusBadRequest, CodeBadRecord, "report body does not decode: "+err.Error())
	}
	v, failure := s.lookupVersion(ctx, body.SetID, body.Version)
	if failure != nil {
		return *failure
	}
	err := s.Store.InsertReport(ctx, store.Report{
		RecordID:    entry.id,
		SetID:       v.SetID,
		Version:     v.Version,
		KeyHMAC:     entry.keyHMAC,
		ASNObserved: entry.origin.ASN,
		Reason:      clip(body.Reason, MaxReasonRunes),
		ReceivedAt:  entry.now,
	})
	if err != nil {
		return internalError(err)
	}
	if err := s.remember(ctx, entry, v.SetID, v.Version); err != nil {
		return internalError(err)
	}
	if v.Status == hubwire.SetStatusActive {
		independent, err := s.Store.IndependentReports(ctx, v.SetID, v.Version)
		if err != nil {
			return internalError(err)
		}
		if independent >= ReportsToHide {
			if err := s.Store.Hide(ctx, v.SetID, v.Version, ReasonReports, entry.now); err != nil {
				return internalError(err)
			}
		}
	}
	return Response{Status: http.StatusAccepted, Body: map[string]interface{}{"id": entry.id, "kind": entry.rec.Kind}}
}
