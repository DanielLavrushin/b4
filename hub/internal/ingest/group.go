package ingest

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	CodeInvalidGroup   = "invalid_group"
	CodeTooManyMembers = "too_many_members"
	CodeDuplicateGroup = "duplicate_group"
)

func memberFail(r Response, index int) Response {
	r.Body["member"] = index
	return r
}

func (s *Service) group(ctx context.Context, entry record) Response {
	var body hubwire.GroupBody
	if err := json.Unmarshal(entry.rec.Body, &body); err != nil {
		return fail(http.StatusBadRequest, CodeBadRecord, "group body does not decode: "+err.Error())
	}
	switch {
	case len(body.Members) == 0:
		return fail(http.StatusBadRequest, CodeInvalidGroup, "the group has no sets")
	case len(body.Members) > hubwire.MaxGroupMembers:
		return fail(http.StatusBadRequest, CodeTooManyMembers, "a group holds at most "+strconv.Itoa(hubwire.MaxGroupMembers)+" sets")
	}
	title := clip(body.Title, MaxTitleRunes)
	if title == "" {
		return fail(http.StatusBadRequest, CodeInvalidGroup, "the group has no title")
	}
	for i := range body.Members {
		if field, limit := hubwire.EscalationOutOfRange(body.Members[i].Escalate); field != "" {
			return memberFail(fail(http.StatusBadRequest, CodeInvalidGroup, "set "+strconv.Itoa(i)+": escalate."+field+" must be between 0 and "+strconv.Itoa(limit)), i)
		}
		if !hubwire.ValidGroupEscalation(body.Members[i].Escalate, i, len(body.Members)) {
			return memberFail(fail(http.StatusBadRequest, CodeInvalidGroup, "set "+strconv.Itoa(i)+" escalates to a set that is not another member of the group"), i)
		}
	}
	members := make([]*member, len(body.Members))
	seen := make(map[string]int, len(body.Members))
	for i := range body.Members {
		m, refused := openMember(&body.Members[i].Envelope, entry.now)
		if refused != nil {
			return memberFail(*refused, i)
		}
		key := m.fp + "|" + m.targetsKey
		if first, dup := seen[key]; dup {
			return memberFail(fail(http.StatusBadRequest, CodeInvalidGroup, "sets "+strconv.Itoa(first)+" and "+strconv.Itoa(i)+" are the same set"), i)
		}
		seen[key] = i
		members[i] = m
	}
	var resp Response
	err := s.Store.Update(ctx, func(t *store.Tx) error {
		var err error
		resp, err = s.fileGroup(ctx, t, entry, &body, title, members)
		return err
	})
	if err != nil {
		return internalError(err)
	}
	return resp
}

func (s *Service) fileGroup(ctx context.Context, t *store.Tx, entry record, body *hubwire.GroupBody, title string, members []*member) (Response, error) {
	description := clip(body.Description, MaxDescriptionRunes)
	h := hints{ASN: body.ASNHint, Country: body.CountryHint, Engine: body.Engine, B4Version: body.B4Version}
	taken := make(map[string]bool, len(members))
	versions := make([]*store.Version, len(members))
	linked := make([]bool, len(members))
	for _, find := range []func(context.Context, string, string) ([]store.Version, error){t.CurrentMatches, t.ListedOrHiddenMatches} {
		for i, m := range members {
			if linked[i] {
				continue
			}
			matches, err := find(ctx, m.fp, m.targetsKey)
			if err != nil {
				return Response{}, err
			}
			prefer := ""
			if d := derivedSet(m.env); d != nil {
				prefer = d.ID
			}
			if v := linkTarget(matches, taken, prefer); v != nil {
				versions[i], linked[i] = v, true
				taken[v.SetID] = true
			}
		}
	}

	var own *store.Group
	var previous *store.GroupVersion
	var derivedFrom *hubwire.Origin
	if d := derivedOrigin(body.DerivedFrom); d != nil {
		g, history, err := t.Group(ctx, d.ID)
		switch {
		case errors.Is(err, store.ErrNotFound):
		case err != nil:
			return Response{}, err
		case g.AuthorHMAC == entry.keyHMAC:
			own = g
			for i := len(history) - 1; i >= 0; i-- {
				if history[i].Status != hubwire.SetStatusRejected {
					previous = &history[i]
					break
				}
			}
		default:
			derivedFrom = d
		}
	}
	if previous != nil && unchangedGroup(previous, title, description, body.Members, versions, linked) {
		return Response{Status: http.StatusConflict, Body: map[string]interface{}{
			"code":     CodeDuplicateGroup,
			"error":    "this group is already on the hub unchanged",
			"group_id": previous.GroupID,
			"version":  previous.Version,
		}}, nil
	}

	for i, m := range members {
		if linked[i] {
			continue
		}
		v, err := s.newVersion(entry, m, h)
		if err != nil {
			return Response{}, err
		}
		derived := derivedSet(m.env)
		if derived != nil && !taken[derived.ID] {
			parent, err := t.Set(ctx, derived.ID)
			if err != nil && !errors.Is(err, store.ErrNotFound) {
				return Response{}, err
			}
			if err == nil && parent.AuthorHMAC == entry.keyHMAC {
				v.SetID = parent.ID
				if err := t.AddVersion(ctx, v); err != nil {
					return Response{}, err
				}
				versions[i], taken[parent.ID] = v, true
				continue
			}
		}
		id, err := hubdata.NewSetID()
		if err != nil {
			return Response{}, err
		}
		set := store.Set{ID: id, AuthorHMAC: entry.keyHMAC, CreatedAt: entry.now, UpdatedAt: entry.now}
		if derived != nil {
			set.DerivedFromID = derived.ID
			set.DerivedFromVersion = derived.Version
		}
		if err := t.CreateSet(ctx, set, v); err != nil {
			return Response{}, err
		}
		versions[i], taken[id] = v, true
	}

	gv := &store.GroupVersion{
		Title:           title,
		Description:     description,
		Status:          hubwire.SetStatusPending,
		RecordID:        entry.id,
		UploaderHMAC:    entry.keyHMAC,
		ASNObserved:     entry.origin.ASN,
		CountryObserved: entry.origin.Country,
		ASNHint:         clip(h.ASN, MaxHintRunes),
		CountryHint:     strings.ToUpper(clip(h.Country, 2)),
		GFP:             groupFingerprintOf(versions, body.Members),
		CreatedAt:       entry.now,
		UpdatedAt:       entry.now,
		Members:         make([]store.GroupMember, len(members)),
	}
	results := make([]hubwire.GroupMemberResult, len(members))
	for i, v := range versions {
		gv.Members[i] = store.GroupMember{SetID: v.SetID, SetVersion: v.Version, Brought: !linked[i], Escalate: body.Members[i].Escalate}
		results[i] = hubwire.GroupMemberResult{SetID: v.SetID, Version: v.Version, Status: v.Status, Linked: linked[i]}
		if err := t.UpsertVote(ctx, uploadVote(entry, v, h)); err != nil {
			return Response{}, err
		}
	}
	if own != nil {
		gv.GroupID = own.ID
		if err := t.AddGroupVersion(ctx, gv); err != nil {
			return Response{}, err
		}
	} else {
		id, err := hubdata.NewSetID()
		if err != nil {
			return Response{}, err
		}
		g := store.Group{ID: id, AuthorHMAC: entry.keyHMAC, CreatedAt: entry.now, UpdatedAt: entry.now}
		if derivedFrom != nil {
			g.DerivedFromID = derivedFrom.ID
			g.DerivedFromVersion = derivedFrom.Version
		}
		if err := t.CreateGroup(ctx, g, gv); err != nil {
			return Response{}, err
		}
	}
	upload := uploadVote(entry, &store.Version{SetID: gv.GroupID, Version: gv.Version, FP: gv.GFP}, h)
	if err := t.UpsertGroupVote(ctx, upload); err != nil {
		return Response{}, err
	}
	if err := t.InsertRecord(ctx, store.Record{ID: entry.id, Kind: entry.rec.Kind, KeyHMAC: entry.keyHMAC, SetID: gv.GroupID, Version: gv.Version, ReceivedAt: entry.now}); err != nil {
		return Response{}, err
	}
	return Response{Status: http.StatusAccepted, Body: map[string]interface{}{
		"id":       entry.id,
		"kind":     entry.rec.Kind,
		"group_id": gv.GroupID,
		"version":  gv.Version,
		"status":   gv.Status,
		"members":  results,
	}}, nil
}

func linkTarget(matches []store.Version, taken map[string]bool, prefer string) *store.Version {
	var first *store.Version
	for j := range matches {
		switch {
		case taken[matches[j].SetID]:
		case matches[j].SetID == prefer:
			return &matches[j]
		case first == nil:
			first = &matches[j]
		}
	}
	return first
}

func (s *Service) replayedGroup(ctx context.Context, seen *store.Record) Response {
	body := map[string]interface{}{"id": seen.ID, "duplicate": true}
	gv, err := s.Store.GetGroupVersion(ctx, seen.SetID, seen.Version)
	if errors.Is(err, store.ErrNotFound) {
		return Response{Status: http.StatusOK, Body: body}
	}
	if err != nil {
		return internalError(err)
	}
	members := make([]hubwire.GroupMemberResult, 0, len(gv.Members))
	for _, m := range gv.Members {
		result := hubwire.GroupMemberResult{SetID: m.SetID, Version: m.SetVersion, Linked: !m.Brought}
		v, err := s.Store.GetVersion(ctx, m.SetID, m.SetVersion)
		switch {
		case err == nil:
			result.Status = v.Status
		case !errors.Is(err, store.ErrNotFound):
			return internalError(err)
		}
		members = append(members, result)
	}
	body["group_id"], body["version"], body["status"], body["members"] = gv.GroupID, gv.Version, gv.Status, members
	return Response{Status: http.StatusOK, Body: body}
}

func unchangedGroup(previous *store.GroupVersion, title, description string, members []hubwire.GroupMember, versions []*store.Version, linked []bool) bool {
	if previous.Title != title || previous.Description != description || len(previous.Members) != len(members) {
		return false
	}
	for i, m := range previous.Members {
		if !linked[i] || m.SetID != versions[i].SetID || !sameEscalation(m.Escalate, members[i].Escalate) {
			return false
		}
	}
	return true
}

func sameEscalation(a, b *hubwire.GroupEscalation) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}
