package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
	"github.com/daniellavrushin/b4/sni"
	"github.com/daniellavrushin/b4/watchdog"
	"github.com/google/uuid"
)

const (
	HubStateUnmodified = "unmodified"
	HubStateModified   = "modified"
)

type HubEnvelopeResponse struct {
	Envelope *hubwire.Envelope `json:"envelope"`
	Report   *hubwire.Report   `json:"report"`
}

type HubInstalledPayload struct {
	Protocol string `json:"protocol"`
	Domain   string `json:"domain"`
	File     string `json:"file"`
	Size     int    `json:"size"`
}

type HubImportResponse struct {
	Set      *config.SetConfig     `json:"set"`
	Warnings []hubwire.Warning     `json:"warnings"`
	Payloads []HubInstalledPayload `json:"payloads"`
}

const (
	hubEnvelopeBodyLimit = 1 << 20
	hubImportBodyLimit   = 4 << 20
	hubSmallBodyLimit    = 16 << 10
	hubReportReasonLimit = 500
)

func (api *API) RegisterHubApi() {
	api.attachHubCategoryResolver()
	api.mux.HandleFunc("/api/hub/envelope", api.handleHubEnvelope)
	api.mux.HandleFunc("/api/hub/import", api.handleHubImport)
	api.mux.HandleFunc("/api/hub/status", api.handleHubStatus)
	api.mux.HandleFunc("/api/hub/sync", api.handleHubSync)
	api.mux.HandleFunc("/api/hub/sets", api.handleHubSets)
	api.mux.HandleFunc("/api/hub/sets/{id}", api.handleHubSetByID)
	api.mux.HandleFunc("/api/hub/sets/{id}/apply", api.handleHubApply)
	api.mux.HandleFunc("/api/hub/sets/{id}/vote", api.handleHubVote)
	api.mux.HandleFunc("/api/hub/sets/{id}/report", api.handleHubReport)
	api.mux.HandleFunc("/api/hub/sets/{id}/test", api.handleHubTest)
	api.mux.HandleFunc("/api/hub/share", api.handleHubShare)
	api.mux.HandleFunc("/api/hub/identity", api.handleHubIdentity)
	api.mux.HandleFunc("/api/hub/identity/recovery", api.handleHubIdentityRecovery)
	api.mux.HandleFunc("/api/hub/identity/restore", api.handleHubIdentityRestore)
}

func (api *API) hubRequest(w http.ResponseWriter, r *http.Request, limit int64) bool {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	return true
}

func hubStateOf(cfg *config.Config, set *config.SetConfig) string {
	if set == nil || set.Hub == nil || set.Hub.Hash == "" {
		return ""
	}
	fp, err := hubwire.LiveFingerprint(set, cfg.ReadCapturePayload)
	if err != nil {
		return HubStateModified
	}
	if strings.EqualFold(fp, set.Hub.Hash) {
		return HubStateUnmodified
	}
	return HubStateModified
}

// @Summary Build a shareable envelope from a set
// @Description Strips everything private from the set, attaches its payload files and returns the envelope with a report of what was left out.
// @Tags Hub
// @Accept json
// @Produce json
// @Param set body config.SetConfig true "Set configuration"
// @Success 200 {object} HubEnvelopeResponse
// @Security BearerAuth
// @Router /hub/envelope [post]
func (api *API) handleHubEnvelope(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubEnvelopeBodyLimit) {
		return
	}
	set := config.NewSetConfig()
	if err := json.NewDecoder(r.Body).Decode(&set); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	cfg := api.getCfg()
	env, report, err := hubwire.Build(&set, hubwire.BuildOptions{
		B4Version:   Version,
		Engine:      cfg.Queue.Mode,
		GeoSiteURL:  cfg.System.Geo.GeoSiteURL,
		GeoIPURL:    cfg.System.Geo.GeoIpURL,
		ReadPayload: cfg.ReadCapturePayload,
	})
	if err != nil {
		writeAPIError(w, err)
		return
	}
	sendResponse(w, HubEnvelopeResponse{Envelope: env, Report: report})
}

// @Summary Open a shared envelope
// @Description Validates the envelope, installs its payload files and returns the set ready to be saved, with warnings about anything this device lacks.
// @Tags Hub
// @Accept json
// @Produce json
// @Param envelope body hubwire.Envelope true "Shared set envelope"
// @Success 200 {object} HubImportResponse
// @Security BearerAuth
// @Router /hub/import [post]
func (api *API) handleHubImport(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubImportBodyLimit) {
		return
	}
	var env hubwire.Envelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	imp, err := hubwire.Open(&env, hubwire.OpenOptions{B4Version: Version})
	if err != nil {
		writeAPIError(w, hubOpenError(err))
		return
	}

	set := imp.Set
	installed, err := api.installHubPayloads(&set, imp.Payloads)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	warnings := append(imp.Warnings, api.hubGeoWarnings(&set)...)

	sendResponse(w, HubImportResponse{Set: &set, Warnings: warnings, Payloads: installed})
}

func payloadInstallError(p hubwire.Payload, err error) *APIError {
	path := "udp.fake_payload_file"
	if p.Protocol == hubwire.ProtocolTLS {
		path = "faking.payload_file"
	}
	message := fmt.Sprintf("The %s payload for %s could not be saved to the captures folder: %v", p.Protocol, p.Domain, err)
	return &APIError{
		Status:  http.StatusInternalServerError,
		Code:    "payload_install_failed",
		Message: message,
		Fields: []FieldError{{
			Path:    path,
			Code:    "payload_install_failed",
			Message: message,
			Params:  map[string]any{"protocol": p.Protocol, "domain": p.Domain, "reason": err.Error()},
		}},
	}
}

func (api *API) installHubPayloads(set *config.SetConfig, payloads []hubwire.Payload) ([]HubInstalledPayload, error) {
	installed := make([]HubInstalledPayload, 0, len(payloads))
	manager := capture.GetManager(api.getCfg())
	for _, p := range payloads {
		rel, err := manager.SaveImportedPayload(p.Protocol, p.Domain, p.Data)
		if err != nil {
			log.Errorf("Hub: could not install %s payload for %s: %v", p.Protocol, p.Domain, err)
			return nil, payloadInstallError(p, err)
		}
		ref := hubwire.RefPrefix + p.SHA256
		if strings.EqualFold(set.Faking.PayloadFile, ref) {
			set.Faking.PayloadFile = rel
		}
		if strings.EqualFold(set.UDP.FakePayloadFile, ref) {
			set.UDP.FakePayloadFile = rel
		}
		installed = append(installed, HubInstalledPayload{Protocol: p.Protocol, Domain: p.Domain, File: rel, Size: p.Size})
	}
	if strings.HasPrefix(set.Faking.PayloadFile, hubwire.RefPrefix) {
		set.Faking.PayloadFile = ""
	}
	if strings.HasPrefix(set.UDP.FakePayloadFile, hubwire.RefPrefix) {
		set.UDP.FakePayloadFile = ""
	}
	return installed, nil
}

type hubStatusResponse struct {
	hub.Status
	SetMatches []SetDomainMatch `json:"set_matches"`
}

func (api *API) hubStatus(svc *hub.Service) hubStatusResponse {
	st := svc.Status()
	hosts := make([]string, 0, len(st.URLs))
	addresses := svc.ConnectedAddresses()
	seen := map[string]bool{}
	for _, raw := range st.URLs {
		u, err := url.Parse(raw)
		if err != nil || u.Hostname() == "" || seen[u.Hostname()] {
			continue
		}
		seen[u.Hostname()] = true
		if net.ParseIP(u.Hostname()) != nil {
			addresses = append(addresses, u.Hostname())
		}
		hosts = append(hosts, u.Hostname())
	}
	matches := make([]SetDomainMatch, 0)
	for _, m := range api.matchDomainsToSets(hosts, "") {
		if m.Enabled {
			matches = append(matches, m)
		}
	}
	if globalPool != nil {
		matches = append(matches, matchAddressesToSets(globalPool.GetMatcher(), addresses)...)
	}
	return hubStatusResponse{Status: st, SetMatches: matches}
}

func matchAddressesToSets(matcher *sni.SuffixSet, addresses []string) []SetDomainMatch {
	if matcher == nil {
		return nil
	}
	var matches []SetDomainMatch
	seen := map[string]bool{}
	for _, addr := range addresses {
		ip := net.ParseIP(addr)
		if ip == nil || seen[ip.String()] {
			continue
		}
		seen[ip.String()] = true
		if ok, set := matcher.MatchIP(ip); ok && set != nil {
			matches = append(matches, SetDomainMatch{
				Domain:   ip.String(),
				SetName:  set.Name,
				SetId:    set.Id,
				Via:      "ip",
				Relation: string(sni.RelationCovered),
				Entry:    ip.String(),
				Enabled:  set.Enabled,
			})
		}
	}
	return matches
}

// @Summary Hub status
// @Description Reports whether the community hub is enabled and configured, the identity key, the last sync and the loaded catalogue.
// @Tags Hub
// @Produce json
// @Success 200 {object} hubStatusResponse
// @Failure 409 {object} APIError "hub_disabled or hub_not_configured"
// @Security BearerAuth
// @Router /hub/status [get]
func (api *API) handleHubStatus(w http.ResponseWriter, r *http.Request) {
	if !hubGetRequest(w, r) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	sendResponse(w, api.hubStatus(svc))
}

// @Summary Sync the hub catalogue now
// @Description Fetches the signed manifest and the catalogue from the first hub base that answers, then delivers any queued votes.
// @Tags Hub
// @Produce json
// @Success 200 {object} hubStatusResponse
// @Failure 502 {object} APIError "sync_failed"
// @Security BearerAuth
// @Router /hub/sync [post]
func (api *API) handleHubSync(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubSmallBodyLimit) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	if _, err := svc.Sync(r.Context()); err != nil {
		writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "sync_failed", Message: err.Error()})
		return
	}
	svc.FlushOutbox(r.Context())
	sendResponse(w, api.hubStatus(svc))
}

// @Summary List hub sets
// @Description Without a domain lists every active set in the catalogue, ranked; with one, only the sets that cover it.
// @Tags Hub
// @Produce json
// @Param domain query string false "Domain to look up"
// @Param limit query int false "Maximum number of sets, default 50"
// @Success 200 {object} HubSetsResponse
// @Security BearerAuth
// @Router /hub/sets [get]
func (api *API) handleHubSets(w http.ResponseWriter, r *http.Request) {
	if !hubGetRequest(w, r) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	results, total := svc.Search(r.URL.Query().Get("domain"), limit)
	applied := hubAppliedIndex(api.getCfg())
	sets := make([]HubSet, 0, len(results))
	for _, res := range results {
		sets = append(sets, hubSetView(res, applied))
	}
	sendResponse(w, HubSetsResponse{Sets: sets, Total: total})
}

// @Summary Get one hub set
// @Tags Hub
// @Produce json
// @Param id path string true "Hub set id"
// @Success 200 {object} HubSet
// @Failure 404 {object} APIError
// @Security BearerAuth
// @Router /hub/sets/{id} [get]
func (api *API) handleHubSetByID(w http.ResponseWriter, r *http.Request) {
	if !hubGetRequest(w, r) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	res, found := svc.Get(r.PathValue("id"))
	if !found {
		writeAPIError(w, ErrNotFound("Hub set not found"))
		return
	}
	sendResponse(w, hubSetView(res, hubAppliedIndex(api.getCfg())))
}

// @Summary Apply a hub set
// @Description Fetches the set's payloads from the hub, opens the envelope and creates a local set from it in front of the others; with a replace id the new set takes that local set's id, position and enabled flag instead.
// @Tags Hub
// @Accept json
// @Produce json
// @Param id path string true "Hub set id"
// @Param body body HubApplyRequest false "Local set to replace"
// @Success 202 {object} HubApplyResponse
// @Failure 404 {object} APIError
// @Failure 502 {object} APIError "hub_unreachable or payload_missing"
// @Security BearerAuth
// @Router /hub/sets/{id}/apply [post]
func (api *API) handleHubApply(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubSmallBodyLimit) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	var req HubApplyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	replace := strings.TrimSpace(req.Replace)
	if replace != "" && hubSetByLocalID(api.getCfg(), replace) == nil {
		writeAPIError(w, ErrNotFound("Set not found"))
		return
	}
	res, found := svc.Get(r.PathValue("id"))
	if !found {
		writeAPIError(w, ErrNotFound("Hub set not found"))
		return
	}
	cs := res.Set
	env := cs.ToEnvelope()
	for _, ref := range cs.Payloads {
		data, err := svc.FetchBlob(r.Context(), ref)
		if err != nil {
			writeHubBlobError(w, ref, err)
			return
		}
		env.Payloads = append(env.Payloads, hubwire.Payload{SHA256: ref.SHA256, Protocol: ref.Protocol, Domain: ref.Domain, Size: len(data), Data: data})
	}
	imp, err := hubwire.Open(env, hubwire.OpenOptions{B4Version: Version})
	if err != nil {
		writeAPIError(w, hubOpenError(err))
		return
	}

	set := imp.Set
	installed, err := api.installHubPayloads(&set, imp.Payloads)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	warnings := append(imp.Warnings, api.hubGeoWarnings(&set)...)

	set.Id = uuid.New().String()
	if set.Name == "" {
		set.Name = cs.Title
	}
	if set.Name == "" {
		set.Name = firstPlainDomain(set.Targets.SNIDomains)
	}
	if set.Name == "" {
		set.Name = cs.ID
	}
	if len(set.Targets.GeoSiteCategories) > 0 && !api.geodataManager.IsGeositeConfigured() {
		log.Warnf("Set '%s': dropping geosite categories %v, no geosite database is installed", set.Name, set.Targets.GeoSiteCategories)
		set.Targets.GeoSiteCategories = []string{}
	}
	if len(set.Targets.GeoIpCategories) > 0 && !api.geodataManager.IsGeoipConfigured() {
		log.Warnf("Set '%s': dropping geoip categories %v, no geoip database is installed", set.Name, set.Targets.GeoIpCategories)
		set.Targets.GeoIpCategories = []string{}
	}
	if len(set.Targets.SNIDomains) == 0 && len(set.Targets.IPs) == 0 && len(set.Targets.GeoSiteCategories) == 0 && len(set.Targets.GeoIpCategories) == 0 {
		writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "no_targets", Message: "The set targets nothing this device can match"})
		return
	}

	api.loadTargetsForSetCached(&set)
	config.ApplySetDefaults(&set)

	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()
	replaced := false
	if replace != "" {
		for i, existing := range newCfg.Sets {
			if existing == nil || existing.Id != replace {
				continue
			}
			set.Id = existing.Id
			set.Enabled = existing.Enabled
			newCfg.Sets[i] = &set
			replaced = true
			break
		}
		if !replaced {
			writeAPIError(w, ErrNotFound("Set not found"))
			return
		}
	}
	moved := api.releaseDomainsFromOtherSets(newCfg.Sets, set.Id, set.Targets.SNIDomains)
	if moved == nil {
		moved = []DomainReassignment{}
	}
	if !replaced {
		newCfg.Sets = append([]*config.SetConfig{&set}, newCfg.Sets...)
	}

	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Hub apply: failed to save config: %v", err)
		writeAPIError(w, err)
		return
	}
	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}
	if replaced {
		log.Infof("Hub: replaced set '%s' with %s v%d", set.Name, cs.ID, cs.Version)
	} else {
		log.Infof("Hub: applied set %s v%d as '%s'", cs.ID, cs.Version, set.Name)
	}

	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(HubApplyResponse{ID: set.Id, Name: set.Name, Moved: moved, Warnings: warnings, Payloads: installed})
}

// @Summary Vote on an applied hub set
// @Description Sends a signed works or broken vote for the local set applied from this hub id; the strategy must be unmodified since it was applied.
// @Tags Hub
// @Accept json
// @Produce json
// @Param id path string true "Hub set id"
// @Param body body HubVoteRequest true "Vote"
// @Success 202 {object} HubVoteResponse
// @Failure 409 {object} APIError "not_applied or modified"
// @Security BearerAuth
// @Router /hub/sets/{id}/vote [post]
func (api *API) handleHubVote(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubSmallBodyLimit) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var req HubVoteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	kind := strings.ToLower(strings.TrimSpace(req.Kind))
	if kind != hubwire.VoteWorks && kind != hubwire.VoteBroken {
		writeAPIError(w, ErrBadRequest("kind must be works or broken"))
		return
	}
	cfg := api.getCfg()
	local := hubLocalSet(cfg, id)
	if local == nil {
		writeAPIError(w, &APIError{Status: http.StatusConflict, Code: "not_applied", Message: "No local set was applied from this hub set"})
		return
	}
	if hubStateOf(cfg, local) != HubStateUnmodified {
		writeAPIError(w, &APIError{Status: http.StatusConflict, Code: "modified", Message: "The strategy of the applied set was edited, so a vote would not describe the hub set"})
		return
	}
	network := svc.Network()
	body := hubwire.VoteBody{
		SetID:       id,
		Version:     local.Hub.Version,
		FP:          local.Hub.Hash,
		Kind:        kind,
		Domain:      sni.NormalizeDomain(req.Domain),
		ASNHint:     network.ASN,
		CountryHint: network.CC,
		Engine:      cfg.Queue.Mode,
		B4Version:   Version,
	}
	rec, err := svc.Sign(hubwire.RecordVote, body)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	sent, queued, _, err := svc.SendOrQueue(r.Context(), rec)
	if err != nil {
		writeHubError(w, err)
		return
	}
	api.stampHubVote(id, kind)
	log.Infof("Hub: %s vote for %s v%d %s", kind, id, local.Hub.Version, map[bool]string{true: "sent", false: "queued"}[sent])
	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(HubVoteResponse{Queued: queued, Sent: sent})
}

// @Summary Report a hub set to the moderators
// @Description Sends a signed report with a free-text reason about a hub set; the set does not have to be applied on this device.
// @Tags Hub
// @Accept json
// @Produce json
// @Param id path string true "Hub set id"
// @Param body body HubReportRequest true "Report"
// @Success 202 {object} HubReportResponse
// @Failure 400 {object} APIError "bad_request"
// @Failure 404 {object} APIError
// @Security BearerAuth
// @Router /hub/sets/{id}/report [post]
func (api *API) handleHubReport(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubSmallBodyLimit) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	id := r.PathValue("id")
	var req HubReportRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	reason := strings.TrimSpace(req.Reason)
	if reason == "" {
		writeAPIError(w, ErrBadRequest("reason is required"))
		return
	}
	if utf8.RuneCountInString(reason) > hubReportReasonLimit {
		writeAPIError(w, ErrBadRequest(fmt.Sprintf("reason is longer than %d characters", hubReportReasonLimit)))
		return
	}
	version := 0
	if res, found := svc.Get(id); found {
		version = res.Set.Version
	} else if local := hubLocalSet(api.getCfg(), id); local != nil {
		version = local.Hub.Version
	} else {
		writeAPIError(w, ErrNotFound("Hub set not found"))
		return
	}
	rec, err := svc.Sign(hubwire.RecordReport, hubwire.ReportBody{SetID: id, Version: version, Reason: reason})
	if err != nil {
		writeAPIError(w, err)
		return
	}
	sent, queued, _, err := svc.SendOrQueue(r.Context(), rec)
	if err != nil {
		writeHubError(w, err)
		return
	}
	log.Infof("Hub: report on %s v%d %s", id, version, map[bool]string{true: "sent", false: "queued"}[sent])
	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(HubReportResponse{Queued: queued, Sent: sent})
}

// @Summary Share a saved set with the hub
// @Description Builds the shareable envelope from the saved set, signs it and sends it to the hub; on acceptance the local set is stamped with the hub id.
// @Tags Hub
// @Accept json
// @Produce json
// @Param body body HubShareRequest true "Set to share"
// @Success 202 {object} HubShareResponse
// @Failure 409 {object} HubRemoteError "duplicate_strategy"
// @Failure 502 {object} APIError "hub_unreachable"
// @Security BearerAuth
// @Router /hub/share [post]
func (api *API) handleHubShare(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubSmallBodyLimit) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	var req HubShareRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	if strings.TrimSpace(req.SetID) == "" {
		writeAPIError(w, ErrBadRequest("set_id is required"))
		return
	}
	cfg := api.getCfg()
	var set *config.SetConfig
	for _, candidate := range cfg.Sets {
		if candidate != nil && candidate.Id == req.SetID {
			set = candidate
			break
		}
	}
	if set == nil {
		writeAPIError(w, ErrNotFound("Set not found"))
		return
	}
	env, _, err := hubwire.Build(set, hubwire.BuildOptions{
		B4Version:   Version,
		Engine:      cfg.Queue.Mode,
		GeoSiteURL:  cfg.System.Geo.GeoSiteURL,
		GeoIPURL:    cfg.System.Geo.GeoIpURL,
		Description: strings.TrimSpace(req.Description),
		ReadPayload: cfg.ReadCapturePayload,
	})
	if err != nil {
		writeAPIError(w, err)
		return
	}
	network := svc.Network()
	body := hubwire.ShareBody{Envelope: *env, ASNHint: network.ASN, CountryHint: network.CC, Engine: cfg.Queue.Mode, B4Version: Version}
	rec, err := svc.Sign(hubwire.RecordShare, body)
	if err != nil {
		writeAPIError(w, err)
		return
	}
	resp, err := svc.Send(r.Context(), rec)
	if err != nil {
		writeHubError(w, err)
		return
	}
	if resp.SetID != "" {
		api.stampHubOrigin(set.Id, resp.SetID, resp.Version, env.Fingerprint)
	}
	status := resp.Status
	if status == "" && resp.Duplicate {
		status = "duplicate"
	}
	log.Infof("Hub: set '%s' shared as %s v%d (%s)", set.Name, resp.SetID, resp.Version, status)
	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(HubShareResponse{HubID: resp.SetID, Version: resp.Version, Status: status})
}

func (api *API) stampHubVote(hubID, kind string) {
	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()
	now := time.Now().UTC().Format(time.RFC3339)
	changed := false
	for _, set := range newCfg.Sets {
		if set == nil || set.Hub == nil || set.Hub.ID != hubID {
			continue
		}
		set.Hub.Vote = kind
		set.Hub.VotedAt = now
		changed = true
	}
	if !changed {
		return
	}
	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Hub: %s vote for %s could not be remembered: %v", kind, hubID, err)
	}
}

func (api *API) stampHubOrigin(localID, hubID string, version int, hash string) {
	oldCfg := api.getCfg()
	newCfg := oldCfg.Clone()
	for _, set := range newCfg.Sets {
		if set == nil || set.Id != localID {
			continue
		}
		origin := &config.HubOrigin{ID: hubID, Version: version, Hash: hash}
		if set.Hub != nil {
			origin.AppliedAt = set.Hub.AppliedAt
		}
		set.Hub = origin
		if err := api.saveAndPushConfig(newCfg); err != nil {
			log.Errorf("Hub: shared set '%s' could not be stamped with %s: %v", set.Name, hubID, err)
		}
		return
	}
}

// @Summary Fetch a domain through and around b4 for a hub set
// @Description Fetches the domain once through b4 and once with b4 bypassed and reports both outcomes; the first plain domain of the set is used when none is given.
// @Tags Hub
// @Accept json
// @Produce json
// @Param id path string true "Hub set id"
// @Param body body HubTestRequest true "Domain to fetch"
// @Success 200 {object} HubTestResponse
// @Failure 400 {object} APIError "no_domain"
// @Security BearerAuth
// @Router /hub/sets/{id}/test [post]
func (api *API) handleHubTest(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubSmallBodyLimit) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	res, found := svc.Get(r.PathValue("id"))
	if !found {
		writeAPIError(w, ErrNotFound("Hub set not found"))
		return
	}
	var req HubTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	domain := sni.NormalizeDomain(watchdog.ExtractDomain(strings.TrimSpace(req.Domain)))
	if domain == "" {
		domain = firstPlainDomain(hub.TargetList(res.Set.Set, "sni_domains"))
	}
	if domain == "" {
		writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "no_domain", Message: "The set lists no plain domain to fetch; pass one"})
		return
	}
	if watchdog.IsReservedHost(domain) {
		writeAPIError(w, ErrBadRequest(domain+" is a private or local address"))
		return
	}
	cfg := api.getCfg()
	log.Infof("Hub: probing %s for set %s", domain, res.Set.ID)
	through, bypassed := probeDomainBothWays(r.Context(), cfg, domain, mcpProbeTimeout(0))
	sendResponse(w, HubTestResponse{Domain: domain, ThroughB4: hubProbeResultOf(through), Bypassed: hubProbeResultOf(bypassed)})
}

// @Summary Hub identity
// @Tags Hub
// @Produce json
// @Success 200 {object} HubIdentityResponse
// @Security BearerAuth
// @Router /hub/identity [get]
func (api *API) handleHubIdentity(w http.ResponseWriter, r *http.Request) {
	if !hubGetRequest(w, r) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	id, created, err := svc.Identity()
	if err != nil {
		writeAPIError(w, err)
		return
	}
	sendResponse(w, HubIdentityResponse{KeyID: id.KeyID(), CreatedAt: created.UTC().Format(time.RFC3339)})
}

// @Summary Reveal the identity recovery code
// @Tags Hub
// @Produce json
// @Success 200 {object} HubRecoveryCodeResponse
// @Security BearerAuth
// @Router /hub/identity/recovery [post]
func (api *API) handleHubIdentityRecovery(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubSmallBodyLimit) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	id, _, err := svc.Identity()
	if err != nil {
		writeAPIError(w, err)
		return
	}
	sendResponse(w, HubRecoveryCodeResponse{Code: id.RecoveryCode()})
}

// @Summary Restore the identity from a recovery code
// @Tags Hub
// @Accept json
// @Produce json
// @Param body body HubRestoreRequest true "Recovery code"
// @Success 200 {object} HubRestoreResponse
// @Failure 400 {object} APIError "bad_recovery_code"
// @Security BearerAuth
// @Router /hub/identity/restore [post]
func (api *API) handleHubIdentityRestore(w http.ResponseWriter, r *http.Request) {
	if !api.hubRequest(w, r, hubSmallBodyLimit) {
		return
	}
	svc, ok := api.hubService(w)
	if !ok {
		return
	}
	var req HubRestoreRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}
	id, err := svc.RestoreIdentity(req.Code)
	if err != nil {
		if errors.Is(err, hubwire.ErrRecoveryCode) {
			writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "bad_recovery_code", Message: err.Error()})
			return
		}
		writeAPIError(w, err)
		return
	}
	sendResponse(w, HubRestoreResponse{KeyID: id.KeyID()})
}

func (api *API) hubGeoWarnings(set *config.SetConfig) []hubwire.Warning {
	var out []hubwire.Warning
	if api.geodataManager == nil {
		if len(set.Targets.GeoSiteCategories) > 0 {
			out = append(out, hubwire.Warning{Code: "geosite_missing", Params: map[string]interface{}{"categories": set.Targets.GeoSiteCategories}})
		}
		if len(set.Targets.GeoIpCategories) > 0 {
			out = append(out, hubwire.Warning{Code: "geoip_missing", Params: map[string]interface{}{"categories": set.Targets.GeoIpCategories}})
		}
		return out
	}
	if len(set.Targets.GeoSiteCategories) > 0 {
		if !api.geodataManager.IsGeositeConfigured() {
			out = append(out, hubwire.Warning{Code: "geosite_missing", Params: map[string]interface{}{"categories": set.Targets.GeoSiteCategories}})
		} else if missing := api.missingCategories(api.geodataManager.GetGeositePath(), set.Targets.GeoSiteCategories); len(missing) > 0 {
			out = append(out, hubwire.Warning{Code: "geosite_categories_missing", Params: map[string]interface{}{"categories": missing}})
		}
	}
	if len(set.Targets.GeoIpCategories) > 0 {
		if !api.geodataManager.IsGeoipConfigured() {
			out = append(out, hubwire.Warning{Code: "geoip_missing", Params: map[string]interface{}{"categories": set.Targets.GeoIpCategories}})
		} else if missing := api.missingCategories(api.geodataManager.GetGeoipPath(), set.Targets.GeoIpCategories); len(missing) > 0 {
			out = append(out, hubwire.Warning{Code: "geoip_categories_missing", Params: map[string]interface{}{"categories": missing}})
		}
	}
	return out
}

func (api *API) missingCategories(path string, wanted []string) []string {
	tags, err := api.geodataManager.ListCategories(path)
	if err != nil {
		return nil
	}
	have := make(map[string]bool, len(tags))
	for _, t := range tags {
		have[strings.ToLower(t)] = true
	}
	var missing []string
	for _, w := range wanted {
		if !have[strings.ToLower(w)] {
			missing = append(missing, w)
		}
	}
	return missing
}
