package web

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/store"
)

const (
	codeInvalidSet = "invalid_set"
	codeNotPending = "not_pending"
	codeDuplicate  = "duplicate_strategy"
	codeUnchanged  = "unchanged"
	codeStale      = "stale"

	noTargetsWarning = "no_targets"

	maxEditBytes = 128 << 10

	strippedPrivate      = "private"
	strippedNotShareable = "not_shareable"
	unknownFieldsWarning = "unknown_fields"
)

type editFailure struct {
	status  int
	code    string
	message string
}

type editResult struct {
	preview    EditPreview
	targetsKey string
}

func lookupPath(m map[string]interface{}, path string) (interface{}, bool) {
	var cur interface{} = m
	for _, part := range strings.Split(path, ".") {
		sub, ok := cur.(map[string]interface{})
		if !ok {
			return nil, false
		}
		if cur, ok = sub[part]; !ok {
			return nil, false
		}
	}
	return cur, true
}

func leafPaths(m map[string]interface{}, prefix string, out map[string]interface{}) {
	for k, v := range m {
		path := k
		if prefix != "" {
			path = prefix + "." + k
		}
		if sub, ok := v.(map[string]interface{}); ok {
			if _, leaf := hubwire.Fields[path]; !leaf {
				leafPaths(sub, path, out)
				continue
			}
		}
		out[path] = v
	}
}

func isPrivatePath(path string) bool {
	for i := len(path); i > 0; i-- {
		if i < len(path) && path[i] != '.' {
			continue
		}
		if f, ok := hubwire.Fields[path[:i]]; ok && f.Class == hubwire.Never {
			return true
		}
	}
	return false
}

func unknownFields(warnings []hubwire.Warning) map[string]bool {
	out := make(map[string]bool)
	for _, w := range warnings {
		if w.Code != unknownFieldsWarning {
			continue
		}
		if paths, ok := w.Params["paths"].([]string); ok {
			for _, p := range paths {
				out[p] = true
			}
		}
	}
	return out
}

func strippedPaths(requested, result map[string]interface{}, warnings []hubwire.Warning) ([]hubwire.Stripped, error) {
	defSet := config.NewSetConfig()
	def, err := config.SetToMap(&defSet)
	if err != nil {
		return nil, err
	}
	unknown := unknownFields(warnings)
	leaves := make(map[string]interface{})
	leafPaths(requested, "", leaves)
	out := make([]hubwire.Stripped, 0)
	for path, value := range leaves {
		if unknown[path] {
			continue
		}
		if _, kept := lookupPath(result, path); kept {
			continue
		}
		if defValue, ok := lookupPath(def, path); ok && sameJSON(value, defValue) {
			continue
		}
		reason := strippedNotShareable
		if isPrivatePath(path) {
			reason = strippedPrivate
		}
		out = append(out, hubwire.Stripped{Path: path, Reason: reason})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}

func sameJSON(a, b interface{}) bool {
	ca, errA := hubwire.Canonical(a)
	cb, errB := hubwire.Canonical(b)
	return errA == nil && errB == nil && bytes.Equal(ca, cb)
}

func (s *Server) versionFromPath(w http.ResponseWriter, r *http.Request) (*store.Version, bool) {
	id := r.PathValue("id")
	version, err := strconv.Atoi(r.PathValue("version"))
	if !hubdata.ValidSetID(id) || err != nil || version <= 0 {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set version")
		return nil, false
	}
	v, err := s.Store.GetVersion(r.Context(), id, version)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, codeNotFound, "the address does not name a set version")
		return nil, false
	}
	if err != nil {
		s.fail(w, err)
		return nil, false
	}
	return v, true
}

func (s *Server) prepareEdit(ctx context.Context, v *store.Version, req EditRequest) (*editResult, *editFailure) {
	if req.Projection == nil {
		return nil, &editFailure{http.StatusBadRequest, codeBadRequest, "the edit needs a projection"}
	}
	env := hubwire.Envelope{
		Format:      hubwire.Format,
		Title:       clipRunes(req.Title, ingest.MaxTitleRunes),
		Description: clipRunes(req.Description, ingest.MaxDescriptionRunes),
		Set:         req.Projection,
	}
	for _, ref := range v.Payloads {
		data, err := s.Blobs.Read(ref.SHA256)
		if err != nil {
			return nil, &editFailure{http.StatusInternalServerError, codeInternal, "payload " + ref.SHA256 + " cannot be read, the set was left untouched: " + err.Error()}
		}
		env.Payloads = append(env.Payloads, hubwire.Payload{SHA256: ref.SHA256, Protocol: ref.Protocol, Domain: ref.Domain, Size: len(data), Data: data})
	}
	imp, err := hubwire.Open(&env, hubwire.OpenOptions{Now: s.now})
	if err != nil {
		return nil, &editFailure{http.StatusBadRequest, codeInvalidSet, err.Error()}
	}
	projection, report, err := hubwire.Scrub(&imp.Set)
	if err != nil {
		return nil, &editFailure{http.StatusBadRequest, codeInvalidSet, err.Error()}
	}
	for _, w := range report.Warnings {
		if w.Code == noTargetsWarning {
			return nil, &editFailure{http.StatusBadRequest, codeInvalidSet, "the set has no targets"}
		}
	}
	payloads := make([]hubwire.BlobRef, 0, len(imp.Payloads))
	for _, p := range imp.Payloads {
		payloads = append(payloads, hubwire.BlobRef{SHA256: p.SHA256, Protocol: p.Protocol, Domain: p.Domain, Size: p.Size})
	}
	stripped, err := strippedPaths(req.Projection, projection, imp.Warnings)
	if err != nil {
		return nil, &editFailure{http.StatusInternalServerError, codeInternal, err.Error()}
	}
	title := clipRunes(imp.Set.Name, ingest.MaxTitleRunes)
	preview := EditPreview{
		Title:       title,
		Description: env.Description,
		Projection:  projection,
		Payloads:    payloads,
		Warnings:    imp.Warnings,
		Stripped:    append(report.Stripped, stripped...),
		FP:          imp.Fingerprint,
		FPChanged:   imp.Fingerprint != v.FP,
		Changed:     title != v.Title || env.Description != v.Description || !sameJSON(projection, v.Projection),
		Targets:     targetsView(TargetsOf(projection)),
		Strategy:    orEmpty(StrategyWords(&imp.Set, payloads)),
		Flags:       orEmpty(ingest.Flags(projection, payloads)),
		Family:      ingest.Family(&imp.Set),
		B4Min:       hubwire.MinVersion(projection),
		Tidy:        TidyDomains(store.TargetList(projection, "sni_domains")),
	}
	if preview.Warnings == nil {
		preview.Warnings = []hubwire.Warning{}
	}
	targetsKey := ingest.TargetsKey(projection)
	existing, err := s.Store.FindDuplicate(ctx, imp.Fingerprint, targetsKey)
	switch {
	case err == nil && (existing.SetID != v.SetID || existing.Version != v.Version):
		preview.Duplicate = &DuplicateView{SetID: existing.SetID, Version: existing.Version, Title: existing.Title, Status: existing.Status}
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return nil, &editFailure{http.StatusInternalServerError, codeInternal, err.Error()}
	}
	return &editResult{preview: preview, targetsKey: targetsKey}, nil
}

func (s *Server) readEdit(w http.ResponseWriter, r *http.Request, v *store.Version) (*editResult, *EditRequest, bool) {
	var req EditRequest
	if err := readBodyLimit(w, r, &req, maxEditBytes); err != nil {
		writeError(w, http.StatusBadRequest, codeBadRequest, err.Error())
		return nil, nil, false
	}
	result, failure := s.prepareEdit(r.Context(), v, req)
	if failure != nil {
		writeError(w, failure.status, failure.code, failure.message)
		return nil, nil, false
	}
	return result, &req, true
}

func (s *Server) setPreview(w http.ResponseWriter, r *http.Request) {
	v, ok := s.versionFromPath(w, r)
	if !ok {
		return
	}
	result, _, ok := s.readEdit(w, r, v)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, result.preview)
}

func (s *Server) setEdit(w http.ResponseWriter, r *http.Request) {
	v, ok := s.versionFromPath(w, r)
	if !ok {
		return
	}
	if v.Status != hubwire.SetStatusPending {
		writeError(w, http.StatusConflict, codeNotPending, "only a pending version can be edited; hide it and let the author publish again")
		return
	}
	result, req, ok := s.readEdit(w, r, v)
	if !ok {
		return
	}
	p := result.preview
	if p.Duplicate != nil {
		writeError(w, http.StatusConflict, codeDuplicate, "the edited set duplicates "+p.Duplicate.SetID+"/"+strconv.Itoa(p.Duplicate.Version))
		return
	}
	if !p.Changed {
		writeError(w, http.StatusBadRequest, codeUnchanged, "nothing differs from the received set")
		return
	}
	ctx := r.Context()
	now := s.now()
	edit := store.VersionEdit{
		Title:       p.Title,
		Description: p.Description,
		Projection:  p.Projection,
		Payloads:    p.Payloads,
		Flags:       p.Flags,
		FP:          p.FP,
		TargetsKey:  result.targetsKey,
		B4Min:       p.B4Min,
		Family:      p.Family,
		Note:        cleanReason(req.Note),
		Approve:     req.Approve,
	}
	if req.Expect != nil {
		edit.Expect = *req.Expect
	}
	err := s.Store.EditVersion(ctx, v.SetID, v.Version, edit, now)
	switch {
	case errors.Is(err, store.ErrNotPending):
		writeError(w, http.StatusConflict, codeNotPending, err.Error())
		return
	case errors.Is(err, store.ErrStale):
		writeError(w, http.StatusConflict, codeStale, "the version was changed by someone else since the dialog was opened; close it and open the current one")
		return
	case err != nil:
		s.fail(w, err)
		return
	}
	ref := v.SetID + "/" + strconv.Itoa(v.Version)
	notice := "edited " + ref
	if req.Approve {
		s.rebuild()
		notice = "edited and approved " + ref
	}
	writeJSON(w, http.StatusOK, ActionResult{Notice: notice})
}
