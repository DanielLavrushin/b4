package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/daniellavrushin/b4/asnprefix"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
)

var (
	asnResolve        = asnprefix.Resolve
	asnLookupUpstream = asnprefix.LookupIP
	asnLookupTimeout  = 8 * time.Second
)

type AsnView struct {
	ID        string   `json:"id"`
	Name      string   `json:"name"`
	Prefixes  []string `json:"prefixes"`
	UpdatedAt int64    `json:"updated_at"`
	Source    string   `json:"source,omitempty"`
	config.AsnCounts
	UsedBy    []string `json:"used_by"`
	LastError string   `json:"last_error,omitempty"`
}

type AsnResolveRequest struct {
	ASN     string `json:"asn"`
	Refresh bool   `json:"refresh"`
}

type AsnInUseError struct {
	APIError
	UsedBy []string `json:"used_by"`
}

func (api *API) RegisterAsnApi() {
	api.mux.HandleFunc("/api/asn", api.handleAsn)
	api.mux.HandleFunc("/api/asn/resolve", api.handleAsnResolve)
	api.mux.HandleFunc("/api/asn/lookup", api.handleAsnLookup)
}

func (a *API) handleAsn(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getAsnAll(w, r)
	case http.MethodDelete:
		a.deleteAsn(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func asnUsers(cfg *config.Config) map[string][]string {
	users := make(map[string][]string)
	if cfg == nil {
		return users
	}
	for _, set := range cfg.Sets {
		if set == nil {
			continue
		}
		for _, id := range config.NormalizeASNs(set.Targets.ASNs) {
			users[id] = append(users[id], set.Name)
		}
	}
	return users
}

func setReferencesASN(set *config.SetConfig, ids map[string]bool) bool {
	if set == nil {
		return false
	}
	for _, raw := range set.Targets.ASNs {
		if id, ok := config.NormalizeASN(raw); ok && ids[id] {
			return true
		}
	}
	return false
}

func asnView(info *config.AsnInfo, usedBy []string) AsnView {
	prefixes := info.Prefixes
	if prefixes == nil {
		prefixes = []string{}
	}
	if usedBy == nil {
		usedBy = []string{}
	}
	return AsnView{
		ID:        info.ID,
		Name:      info.Name,
		Prefixes:  prefixes,
		UpdatedAt: info.UpdatedAt,
		Source:    info.Source,
		AsnCounts: info.Counts(),
		UsedBy:    usedBy,
		LastError: asnprefix.LastError(info.ID),
	}
}

func errAsnInvalid(raw string) *APIError {
	return &APIError{
		Status:  http.StatusBadRequest,
		Code:    "asn_invalid",
		Message: fmt.Sprintf("%q is not a public AS number", strings.TrimSpace(raw)),
		Fields: []FieldError{{
			Path:    "asn",
			Code:    "asn_invalid",
			Message: "not a public AS number",
			Params:  map[string]any{"value": strings.TrimSpace(raw)},
		}},
	}
}

// @Summary List the ASNs b4 knows
// @Description Every ASN in the server-side ASN cache and every ASN a set references, keyed by the number without "AS". An ASN that no fetch has resolved yet has no prefixes and updated_at 0. used_by names the sets that reference it; last_error is the most recent failed fetch.
// @Tags ASN
// @Produce json
// @Success 200 {object} map[string]AsnView
// @Security BearerAuth
// @Router /asn [get]
func (a *API) getAsnAll(w http.ResponseWriter, _ *http.Request) {
	users := asnUsers(a.getCfg())
	all := config.Asns().GetAll()
	out := make(map[string]AsnView, len(all)+len(users))
	for id, info := range all {
		out[id] = asnView(info, users[id])
	}
	for id, names := range users {
		if _, ok := out[id]; !ok {
			out[id] = asnView(&config.AsnInfo{ID: id}, names)
		}
	}
	sendResponse(w, out)
}

// @Summary Resolve an ASN to its announced prefixes
// @Description Returns the cached entry while it is fresh (fetched within 20 hours); otherwise, or with refresh, b4 fetches the prefixes from RIPEstat and stores them. When the fetch fails without refresh and a cached copy exists, that copy is returned with last_error set. A fetch that covers less than half of the cached copy's addresses is not stored, with or without refresh: the background refresh accepts such a shrink only after seeing it on 3 consecutive fetches an hour apart. Until then the cached copy is returned with last_error without refresh, and 502 asn_fetch_failed with refresh.
// @Tags ASN
// @Accept json
// @Produce json
// @Param body body AsnResolveRequest true "ASN, as AS15169 or 15169"
// @Success 200 {object} AsnView
// @Failure 400 {object} APIError "code: asn_invalid"
// @Failure 502 {object} APIError "code: asn_fetch_failed"
// @Security BearerAuth
// @Router /asn/resolve [post]
func (a *API) handleAsnResolve(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req AsnResolveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	id, ok := config.NormalizeASN(req.ASN)
	if !ok {
		writeAPIError(w, errAsnInvalid(req.ASN))
		return
	}
	before := config.Asns().Get(id)
	info, err := asnResolve(r.Context(), id, req.Refresh)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return
		}
		if !req.Refresh && before != nil && len(before.Prefixes) > 0 {
			view := asnView(before, asnUsers(a.getCfg())[id])
			view.LastError = err.Error()
			sendResponse(w, view)
			return
		}
		log.Warnf("ASN AS%s: resolve failed: %v", id, err)
		writeAPIError(w, &APIError{
			Status:  http.StatusBadGateway,
			Code:    "asn_fetch_failed",
			Message: fmt.Sprintf("Could not fetch the prefixes of AS%s: %v", id, err),
		})
		return
	}
	if before == nil || !slices.Equal(before.Prefixes, info.Prefixes) {
		a.ReloadASNTargets([]string{id})
	}
	sendResponse(w, asnView(info, asnUsers(a.getCfg())[id]))
}

// @Summary Delete a cached ASN
// @Description Removes an ASN from the server-side cache. An ASN that a set still references is refused with 409 asn_in_use and the names of those sets in used_by.
// @Tags ASN
// @Produce json
// @Param id query string true "ASN, as AS15169 or 15169"
// @Success 200 {object} map[string]bool
// @Failure 400 {object} APIError "code: asn_invalid"
// @Failure 409 {object} AsnInUseError "code: asn_in_use"
// @Security BearerAuth
// @Router /asn [delete]
func (a *API) deleteAsn(w http.ResponseWriter, r *http.Request) {
	raw := r.URL.Query().Get("id")
	id, ok := config.NormalizeASN(raw)
	if !ok {
		writeAPIError(w, errAsnInvalid(raw))
		return
	}
	unlock := config.LockWrites()
	usedBy := asnUsers(a.getCfg())[id]
	if len(usedBy) > 0 {
		unlock()
		setJsonHeader(w)
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(AsnInUseError{
			APIError: APIError{
				Code:    "asn_in_use",
				Message: fmt.Sprintf("AS%s is used by %s; remove it from those sets first", id, strings.Join(usedBy, ", ")),
			},
			UsedBy: usedBy,
		})
		return
	}
	err := config.Asns().Delete(id)
	unlock()
	if err != nil {
		log.Errorf("ASN AS%s: could not delete it from the ASN cache: %v", id, err)
		writeAPIError(w, ErrInternal("Failed to delete the ASN"))
		return
	}
	sendResponse(w, map[string]bool{"success": true})
}

// @Summary Find the ASN announcing an IP address
// @Description Asks RIPEstat network-info for the prefix and origin ASNs of the address; when RIPEstat cannot be reached, answers from the server-side ASN cache (longest prefix). Private and reserved addresses are answered with no ASN and never sent out. cached tells whether the ASN's prefixes are already in the cache.
// @Tags ASN
// @Produce json
// @Param ip query string true "IP address, optionally with a port"
// @Success 200 {object} asnprefix.Lookup
// @Failure 400 {object} APIError "code: ip_invalid"
// @Failure 502 {object} APIError "code: asn_lookup_failed"
// @Security BearerAuth
// @Router /asn/lookup [get]
func (a *API) handleAsnLookup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	raw := r.URL.Query().Get("ip")
	addr, ok := asnprefix.ParseIP(raw)
	if !ok {
		writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "ip_invalid", Message: fmt.Sprintf("%q is not an IP address", strings.TrimSpace(raw))})
		return
	}
	ip := addr.String()
	ctx, cancel := context.WithTimeout(r.Context(), asnLookupTimeout)
	defer cancel()
	res, err := asnLookupUpstream(ctx, ip)
	if err == nil {
		sendResponse(w, res)
		return
	}
	if r.Context().Err() != nil {
		return
	}
	if prefix, matches := config.Asns().LookupIP(ip); len(matches) > 0 {
		log.Debugf("ASN lookup of %s failed (%v), answering from the ASN cache", ip, err)
		out := asnprefix.Lookup{IP: ip, Prefix: prefix, ASNs: make([]asnprefix.LookupASN, 0, len(matches))}
		for _, info := range matches {
			out.ASNs = append(out.ASNs, asnprefix.LookupASN{ID: info.ID, Name: info.Name, Cached: true})
		}
		sendResponse(w, out)
		return
	}
	log.Warnf("ASN lookup of %s failed: %v", ip, err)
	writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "asn_lookup_failed", Message: fmt.Sprintf("Could not look up %s: %v", ip, err)})
}

func (a *API) ReloadASNTargets(changed []string) {
	ids := make(map[string]bool, len(changed))
	for _, raw := range changed {
		if id, ok := config.NormalizeASN(raw); ok {
			ids[id] = true
		}
	}
	if len(ids) == 0 || !slices.ContainsFunc(a.getCfg().Sets, func(s *config.SetConfig) bool { return setReferencesASN(s, ids) }) {
		return
	}
	var names []string
	oldCfg, newCfg, err := a.editConfig(func(next *config.Config) error {
		names = names[:0]
		for _, set := range next.Sets {
			if setReferencesASN(set, ids) {
				a.loadTargetsForSetCached(set)
				names = append(names, set.Name)
			}
		}
		if len(names) == 0 {
			return errConfigUnchanged
		}
		return nil
	})
	switch {
	case errors.Is(err, errConfigUnchanged):
		return
	case err != nil:
		log.Errorf("ASN prefixes changed but the sets using them could not be reloaded: %v", err)
		return
	}
	log.Infof("Reloaded the ASN targets of %s", strings.Join(names, ", "))
	a.PerformSoftRestart(newCfg, oldCfg)
}

func unresolvedASNs(ids []string) []string {
	if len(ids) == 0 {
		return nil
	}
	var out []string
	s := config.Asns()
	for _, raw := range ids {
		id, ok := config.NormalizeASN(raw)
		if !ok {
			out = append(out, strings.TrimSpace(raw))
			continue
		}
		if info := s.Get(id); info == nil || len(info.Prefixes) == 0 {
			out = append(out, id)
		}
	}
	return out
}

func configHasUnresolvedASNs(cfg *config.Config) bool {
	for _, set := range cfg.Sets {
		if set != nil && len(unresolvedASNs(set.Targets.ASNs)) > 0 {
			return true
		}
	}
	return false
}

func asnTargetStats(t config.TargetsConfig) (int, map[string]int, []string) {
	if len(t.ASNs) == 0 {
		return 0, nil, nil
	}
	prefixes, unresolved := config.ExpandASNs(t.ASNs, t.IPVersion)
	breakdown := make(map[string]int)
	s := config.Asns()
	for _, id := range config.NormalizeASNs(t.ASNs) {
		info := s.Get(id)
		if info == nil || len(info.Prefixes) == 0 {
			continue
		}
		breakdown[id] = countFamilyPrefixes(info.Prefixes, t.IPVersion)
	}
	return len(prefixes), breakdown, unresolved
}

func countFamilyPrefixes(prefixes []string, ipVersion string) int {
	if ipVersion != "4" && ipVersion != "6" {
		return len(prefixes)
	}
	n := 0
	for _, p := range prefixes {
		if (strings.IndexByte(p, ':') >= 0) == (ipVersion == "6") {
			n++
		}
	}
	return n
}

type DiagASN struct {
	CachePath  string          `json:"cache_path,omitempty"`
	Cached     int             `json:"cached"`
	Referenced []DiagASNTarget `json:"referenced,omitempty"`
	Unresolved []string        `json:"unresolved,omitempty"`
}

type DiagASNTarget struct {
	ID        string   `json:"id"`
	Name      string   `json:"name,omitempty"`
	Prefixes  int      `json:"prefix_count"`
	UpdatedAt int64    `json:"updated_at,omitempty"`
	Sets      []string `json:"sets"`
	LastError string   `json:"last_error,omitempty"`
}

func collectASNDiag(cfg *config.Config) *DiagASN {
	s := config.Asns()
	all := s.GetAll()
	users := asnUsers(cfg)
	if len(all) == 0 && len(users) == 0 {
		return nil
	}
	diag := &DiagASN{CachePath: s.Path(), Cached: len(all)}
	ids := make([]string, 0, len(users))
	for id := range users {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(x, y string) int {
		if len(x) != len(y) {
			return len(x) - len(y)
		}
		return strings.Compare(x, y)
	})
	for _, id := range ids {
		entry := DiagASNTarget{ID: id, Sets: users[id], LastError: asnprefix.LastError(id)}
		if info := all[id]; info != nil {
			entry.Name = info.Name
			entry.Prefixes = len(info.Prefixes)
			entry.UpdatedAt = info.UpdatedAt
		}
		if entry.Prefixes == 0 {
			diag.Unresolved = append(diag.Unresolved, id)
		}
		diag.Referenced = append(diag.Referenced, entry)
	}
	return diag
}
