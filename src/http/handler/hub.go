package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"

	"github.com/daniellavrushin/b4/capture"
	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/log"
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
)

func (api *API) RegisterHubApi() {
	api.mux.HandleFunc("/api/hub/envelope", api.handleHubEnvelope)
	api.mux.HandleFunc("/api/hub/import", api.handleHubImport)
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
		var pe *hubwire.PayloadError
		switch {
		case errors.Is(err, hubwire.ErrUnsupportedFormat):
			writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "unsupported_format", Message: err.Error()})
		case errors.Is(err, hubwire.ErrNoSet):
			writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "no_set", Message: err.Error()})
		case errors.As(err, &pe):
			writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "payload_invalid", Message: err.Error()})
		default:
			writeAPIError(w, ErrBadRequest(err.Error()))
		}
		return
	}

	cfg := api.getCfg()
	set := imp.Set
	warnings := imp.Warnings
	installed := make([]HubInstalledPayload, 0, len(imp.Payloads))
	manager := capture.GetManager(cfg)
	for _, p := range imp.Payloads {
		rel, err := manager.SaveImportedPayload(p.Protocol, p.Domain, p.Data)
		if err != nil {
			log.Errorf("Hub import: could not install %s payload for %s: %v", p.Protocol, p.Domain, err)
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

	warnings = append(warnings, api.hubGeoWarnings(&set)...)

	sendResponse(w, HubImportResponse{Set: &set, Warnings: warnings, Payloads: installed})
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
