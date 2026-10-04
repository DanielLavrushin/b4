package web

import (
	"bytes"
	"context"
	"errors"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4hub/internal/hubdata"
	"github.com/daniellavrushin/b4hub/internal/ingest"
	"github.com/daniellavrushin/b4hub/internal/moderation"
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

	privateAddressesWarning  = "private_addresses"
	invalidAddressesWarning  = "invalid_addresses"
	catchAllAddressesWarning = "catch_all_addresses"
	invalidDomainsWarning    = "invalid_domains"
	pinNotTargetedWarning    = "pin_not_targeted"
	pinPrivateAddressWarning = "pin_private_address"

	pinsPath = "dns.pins"
)

type editFailure struct {
	status  int
	code    string
	message string
	params  map[string]interface{}
}

type InvalidFieldView struct {
	Path    string                 `json:"path"`
	Code    string                 `json:"code"`
	Message string                 `json:"message"`
	Params  map[string]interface{} `json:"params,omitempty"`
}

func invalidSet(err error) *editFailure {
	failure := &editFailure{status: http.StatusBadRequest, code: codeInvalidSet, message: err.Error()}
	var invalid *config.ValidationError
	if !errors.As(err, &invalid) || len(invalid.Fields) == 0 {
		return failure
	}
	fields := make([]InvalidFieldView, 0, len(invalid.Fields))
	for _, f := range invalid.Fields {
		fields = append(fields, InvalidFieldView{Path: strings.TrimPrefix(f.Path, "sets[0]."), Code: f.Code, Message: f.Message, Params: f.Params})
	}
	failure.params = map[string]interface{}{"fields": fields}
	return failure
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

func pinsWarned(value interface{}, warnings []hubwire.Warning) bool {
	pins, ok := value.(map[string]interface{})
	if !ok || len(pins) == 0 {
		return false
	}
	warned := make(map[string]bool)
	for _, w := range warnings {
		if w.Code != pinNotTargetedWarning && w.Code != pinPrivateAddressWarning {
			continue
		}
		if domain, ok := w.Params["domain"].(string); ok {
			warned[domain] = true
		}
	}
	for domain := range pins {
		if !warned[config.NormalizePinDomain(domain)] {
			return false
		}
	}
	return true
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
		if path == pinsPath && pinsWarned(value, warnings) {
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

func classifyAddress(raw string) (invalid, catchAll bool) {
	value := strings.TrimSpace(raw)
	if !strings.Contains(value, "/") {
		return net.ParseIP(value) == nil, false
	}
	_, network, err := net.ParseCIDR(value)
	if err != nil {
		return true, false
	}
	ones, _ := network.Mask.Size()
	return false, ones == 0
}

func addressWarnings(warnings []hubwire.Warning, addresses []string) []hubwire.Warning {
	var invalid, catchAll []string
	moved := make(map[string]bool)
	for _, address := range addresses {
		if moved[address] {
			continue
		}
		bad, all := classifyAddress(address)
		switch {
		case bad:
			invalid = append(invalid, address)
		case all:
			catchAll = append(catchAll, address)
		default:
			continue
		}
		moved[address] = true
	}
	if len(moved) == 0 {
		return warnings
	}
	out := make([]hubwire.Warning, 0, len(warnings)+2)
	for _, w := range warnings {
		if listed, ok := w.Params["addresses"].([]string); ok && w.Code == privateAddressesWarning {
			kept := make([]string, 0, len(listed))
			for _, address := range listed {
				if !moved[address] {
					kept = append(kept, address)
				}
			}
			if len(kept) == 0 {
				continue
			}
			w = hubwire.Warning{Code: w.Code, Params: map[string]interface{}{"addresses": kept}}
		}
		out = append(out, w)
	}
	if len(invalid) > 0 {
		out = append(out, hubwire.Warning{Code: invalidAddressesWarning, Params: map[string]interface{}{"addresses": invalid}})
	}
	if len(catchAll) > 0 {
		out = append(out, hubwire.Warning{Code: catchAllAddressesWarning, Params: map[string]interface{}{"addresses": catchAll}})
	}
	return out
}

func domainSeparator(r rune) bool {
	return unicode.IsSpace(r) || r == ',' || r == ';'
}

func domainWarnings(domains []string) []hubwire.Warning {
	var invalid []string
	seen := make(map[string]bool)
	for _, domain := range domains {
		value, isRegex := sni.ParseDomainEntry(domain)
		if isRegex || seen[domain] || !strings.ContainsFunc(value, domainSeparator) {
			continue
		}
		seen[domain] = true
		invalid = append(invalid, domain)
	}
	if len(invalid) == 0 {
		return nil
	}
	return []hubwire.Warning{{Code: invalidDomainsWarning, Params: map[string]interface{}{"domains": invalid}}}
}

func (s *Server) prepareEdit(ctx context.Context, v *store.Version, req EditRequest) (*editResult, *editFailure) {
	if req.Projection == nil {
		return nil, &editFailure{status: http.StatusBadRequest, code: codeBadRequest, message: "the edit needs a projection"}
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
			return nil, &editFailure{status: http.StatusInternalServerError, code: codeInternal, message: "payload " + ref.SHA256 + " cannot be read, the set was left untouched: " + err.Error()}
		}
		env.Payloads = append(env.Payloads, hubwire.Payload{SHA256: ref.SHA256, Protocol: ref.Protocol, Domain: ref.Domain, Size: len(data), Data: data})
	}
	imp, err := hubwire.Open(&env, hubwire.OpenOptions{Now: s.now})
	if err != nil {
		return nil, invalidSet(err)
	}
	projection, report, err := hubwire.Scrub(&imp.Set)
	if err != nil {
		return nil, invalidSet(err)
	}
	for _, w := range report.Warnings {
		if w.Code == noTargetsWarning {
			return nil, &editFailure{status: http.StatusBadRequest, code: codeInvalidSet, message: "the set has no targets"}
		}
	}
	payloads := make([]hubwire.BlobRef, 0, len(imp.Payloads))
	for _, p := range imp.Payloads {
		payloads = append(payloads, hubwire.BlobRef{SHA256: p.SHA256, Protocol: p.Protocol, Domain: p.Domain, Size: p.Size})
	}
	stripped, err := strippedPaths(req.Projection, projection, imp.Warnings)
	if err != nil {
		return nil, &editFailure{status: http.StatusInternalServerError, code: codeInternal, message: err.Error()}
	}
	warnings := addressWarnings(imp.Warnings, store.TargetList(projection, "ip"))
	warnings = append(warnings, domainWarnings(store.TargetList(projection, "sni_domains"))...)
	warnings = append(warnings, s.categoryWarnings(projection)...)
	title := clipRunes(imp.Set.Name, ingest.MaxTitleRunes)
	preview := EditPreview{
		Title:       title,
		Description: env.Description,
		Projection:  projection,
		Payloads:    payloads,
		Warnings:    warnings,
		Stripped:    append(report.Stripped, stripped...),
		FP:          imp.Fingerprint,
		FPChanged:   imp.Fingerprint != v.FP,
		Changed:     title != v.Title || env.Description != v.Description || !sameJSON(projection, v.Projection),
		Targets:     targetsView(TargetsOf(projection)),
		Strategy:    Techniques(&imp.Set, payloads),
		Flags:       orEmpty(ingest.Flags(projection, payloads)),
		Family:      ingest.Family(&imp.Set),
		B4Min:       hubwire.MinVersion(projection),
		Tidy:        TidyDomains(store.TargetList(projection, "sni_domains")),
	}
	if preview.Warnings == nil {
		preview.Warnings = []hubwire.Warning{}
	}
	targetsKey := store.TargetsKey(projection)
	existing, err := s.Store.FindDuplicateExcept(ctx, imp.Fingerprint, targetsKey, v.RowID)
	switch {
	case err == nil:
		preview.Duplicate = &DuplicateView{SetID: existing.SetID, Version: existing.Version, Title: existing.Title, Status: existing.Status}
	case !errors.Is(err, store.ErrNotFound):
		return nil, &editFailure{status: http.StatusInternalServerError, code: codeInternal, message: err.Error()}
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
		writeJSON(w, failure.status, ErrorBody{Code: failure.code, Error: failure.message, Params: failure.params})
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
		writeJSON(w, http.StatusConflict, ErrorBody{
			Code:   codeDuplicate,
			Error:  "the edited set duplicates " + p.Duplicate.SetID + "/" + strconv.Itoa(p.Duplicate.Version),
			Params: map[string]interface{}{"set_id": p.Duplicate.SetID, "version": p.Duplicate.Version},
		})
		return
	}
	if !p.Changed {
		writeError(w, http.StatusBadRequest, codeUnchanged, "nothing differs from the received set")
		return
	}
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
		Note:        req.Note,
	}
	if req.Expect != nil {
		edit.Expect = *req.Expect
	}
	ctx := r.Context()
	res, err := s.Moderation.Edit(ctx, s.actor(r), moderation.Ref{SetID: v.SetID, Version: v.Version}, edit, req.Approve)
	if err != nil {
		s.failModeration(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.result(ctx, res))
}
