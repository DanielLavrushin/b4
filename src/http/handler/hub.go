package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

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
)

func (api *API) RegisterHubApi() {
	api.mux.HandleFunc("/api/hub/envelope", api.handleHubEnvelope)
	api.mux.HandleFunc("/api/hub/import", api.handleHubImport)
	api.mux.HandleFunc("/api/hub/status", api.handleHubStatus)
	api.mux.HandleFunc("/api/hub/sync", api.handleHubSync)
	api.mux.HandleFunc("/api/hub/sets", api.handleHubSets)
	api.mux.HandleFunc("/api/hub/sets/{id}", api.handleHubSetByID)
	api.mux.HandleFunc("/api/hub/sets/{id}/apply", api.handleHubApply)
	api.mux.HandleFunc("/api/hub/sets/{id}/vote", api.handleHubVote)
	api.mux.HandleFunc("/api/hub/sets/{id}/test", api.handleHubTest)
	api.mux.HandleFunc("/api/hub/share", api.handleHubShare)
	api.mux.HandleFunc("/api/hub/identity", api.handleHubIdentity)
	api.mux.HandleFunc("/api/hub/identity/recovery", api.handleHubIdentityRecovery)
	api.mux.HandleFunc("/api/hub/identity/restore", api.handleHubIdentityRestore)
}

func hubSameOrigin(r *http.Request) bool {
	origin := strings.TrimSpace(r.Header.Get("Origin"))
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	oHost, oPort := splitHostPort(u.Host)
	hHost, hPort := splitHostPort(r.Host)
	if !strings.EqualFold(oHost, hHost) {
		return false
	}
	return oPort == "" || hPort == "" || strings.EqualFold(oPort, hPort)
}

func (api *API) hubRequest(w http.ResponseWriter, r *http.Request, limit int64) bool {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	if !hubSameOrigin(r) {
		writeAPIError(w, &APIError{Status: http.StatusForbidden, Code: "origin_not_allowed", Message: "Origin not allowed"})
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
		return ""
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
	installed, warnings := api.installHubPayloads(&set, imp.Payloads)
	warnings = append(imp.Warnings, warnings...)
	warnings = append(warnings, api.hubGeoWarnings(&set)...)

	sendResponse(w, HubImportResponse{Set: &set, Warnings: warnings, Payloads: installed})
}

func (api *API) installHubPayloads(set *config.SetConfig, payloads []hubwire.Payload) ([]HubInstalledPayload, []hubwire.Warning) {
	installed := make([]HubInstalledPayload, 0, len(payloads))
	warnings := []hubwire.Warning{}
	manager := capture.GetManager(api.getCfg())
	for _, p := range payloads {
		rel, err := manager.SaveImportedPayload(p.Protocol, p.Domain, p.Data)
		if err != nil {
			log.Errorf("Hub: could not install %s payload for %s: %v", p.Protocol, p.Domain, err)
			warnings = append(warnings, hubwire.Warning{Code: "payload_install_failed", Params: map[string]interface{}{"protocol": p.Protocol, "domain": p.Domain}})
			continue
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
	return installed, warnings
}

// @Summary Hub status
// @Description Reports whether the community hub is enabled and configured, the identity key, the last sync and the loaded catalogue.
// @Tags Hub
// @Produce json
// @Success 200 {object} hub.Status
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
	sendResponse(w, svc.Status())
}

// @Summary Sync the hub catalogue now
// @Description Fetches the signed manifest and the catalogue from the first hub base that answers, then delivers any queued votes.
// @Tags Hub
// @Produce json
// @Success 200 {object} hub.Status
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
	sendResponse(w, svc.Status())
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
// @Description Fetches the set's payloads from the hub, opens the envelope and creates a local set from it in front of the others.
// @Tags Hub
// @Accept json
// @Produce json
// @Param id path string true "Hub set id"
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
	installed, warnings := api.installHubPayloads(&set, imp.Payloads)
	warnings = append(imp.Warnings, warnings...)
	warnings = append(warnings, api.hubGeoWarnings(&set)...)

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
	moved := api.releaseDomainsFromOtherSets(newCfg.Sets, set.Id, set.Targets.SNIDomains)
	if moved == nil {
		moved = []DomainReassignment{}
	}
	newCfg.Sets = append([]*config.SetConfig{&set}, newCfg.Sets...)

	if err := api.saveAndPushConfig(newCfg); err != nil {
		log.Errorf("Hub apply: failed to save config: %v", err)
		writeAPIError(w, err)
		return
	}
	if api.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}
	log.Infof("Hub: applied set %s v%d as '%s'", cs.ID, cs.Version, set.Name)

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
	log.Infof("Hub: %s vote for %s v%d %s", kind, id, local.Hub.Version, map[bool]string{true: "sent", false: "queued"}[sent])
	setJsonHeader(w)
	w.WriteHeader(http.StatusAccepted)
	_ = json.NewEncoder(w).Encode(HubVoteResponse{Queued: queued, Sent: sent})
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
