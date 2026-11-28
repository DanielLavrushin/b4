package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/score"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const CodeMemberMismatch = "member_mismatch"

func (s *Service) groupVote(ctx context.Context, entry record) Response {
	var body hubwire.GroupVoteBody
	if err := json.Unmarshal(entry.rec.Body, &body); err != nil {
		return fail(http.StatusBadRequest, CodeBadRecord, "group vote body does not decode: "+err.Error())
	}
	kind, ok := voteKind(body.Kind)
	if !ok {
		return fail(http.StatusBadRequest, CodeBadRecord, "vote kind must be works or broken")
	}
	gv, failure := s.listedGroupVersion(ctx, body.GroupID, body.Version)
	if failure != nil {
		return *failure
	}
	if len(body.Members) != len(gv.Members) {
		return fail(http.StatusBadRequest, CodeMemberMismatch, "the group version holds "+strconv.Itoa(len(gv.Members))+" sets, the vote names "+strconv.Itoa(len(body.Members)))
	}
	members := make([]hubwire.GroupFingerprintMember, len(gv.Members))
	var domains []string
	for i, m := range gv.Members {
		named := body.Members[i]
		if named.SetID != m.SetID {
			return memberFail(fail(http.StatusBadRequest, CodeMemberMismatch, "set "+strconv.Itoa(i)+" is not the group's set at that position"), i)
		}
		v, failure := s.lookupVersion(ctx, named.SetID, named.Version)
		if failure != nil {
			return memberFail(*failure, i)
		}
		if v.Status != hubwire.SetStatusActive {
			return memberFail(fail(http.StatusBadRequest, CodeNotActive, "set "+strconv.Itoa(i)+": votes are accepted only for listed set versions"), i)
		}
		members[i] = hubwire.GroupFingerprintMember{FP: v.FP, Escalate: m.Escalate}
		domains = append(domains, store.TargetList(v.Projection, "sni_domains")...)
	}
	domain := sni.NormalizeDomain(body.Domain)
	if domain != "" && !domainTargeted(domain, domains) {
		domain = ""
	}
	vote := uploadVote(entry, &store.Version{SetID: gv.GroupID, Version: gv.Version, FP: hubwire.GroupFingerprint(members)},
		hints{ASN: body.ASNHint, Country: body.CountryHint, Engine: body.Engine, B4Version: body.B4Version})
	vote.Kind, vote.Weight, vote.Domain = kind, score.Weights[kind], domain
	if err := s.Store.UpsertGroupVote(ctx, vote); err != nil {
		return internalError(err)
	}
	if err := s.remember(ctx, entry, gv.GroupID, gv.Version); err != nil {
		return internalError(err)
	}
	return Response{Status: http.StatusAccepted, Body: map[string]interface{}{"id": entry.id, "kind": entry.rec.Kind}}
}

func (s *Service) listedGroupVersion(ctx context.Context, groupID string, version int) (*store.GroupVersion, *Response) {
	if !hubdata.ValidSetID(groupID) || version <= 0 {
		r := fail(http.StatusBadRequest, CodeBadRecord, "group_id or version is malformed")
		return nil, &r
	}
	g, versions, err := s.Store.GetGroup(ctx, groupID)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		r := internalError(err)
		return nil, &r
	}
	for i := range versions {
		if versions[i].Version != version {
			continue
		}
		if !g.WithdrawnAt.IsZero() || versions[i].Status != hubwire.SetStatusActive {
			r := fail(http.StatusBadRequest, CodeNotActive, "votes are accepted only for listed group versions")
			return nil, &r
		}
		return &versions[i], nil
	}
	r := fail(http.StatusBadRequest, CodeUnknownSet, "no such group version")
	return nil, &r
}

func groupFingerprintOf(versions []*store.Version, members []hubwire.GroupMember) string {
	fps := make([]hubwire.GroupFingerprintMember, len(versions))
	for i, v := range versions {
		fps[i] = hubwire.GroupFingerprintMember{FP: v.FP, Escalate: members[i].Escalate}
	}
	return hubwire.GroupFingerprint(fps)
}
