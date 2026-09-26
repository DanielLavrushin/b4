package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/netip"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/log"
	"github.com/google/uuid"
)

type AddGeoIpRequest struct {
	Cidr    []string `json:"cidr,omitempty"`
	ASNs    []string `json:"asns,omitempty"`
	SetId   string   `json:"set_id,omitempty"`
	SetName string   `json:"set_name,omitempty"`
}

type AddIpResponse struct {
	Success        bool     `json:"success"`
	Message        string   `json:"message"`
	SetId          string   `json:"set_id"`
	TotalCidrs     int      `json:"total_cidrs"`
	TotalASNs      int      `json:"total_asns"`
	AddedCidrs     int      `json:"added_cidrs"`
	AddedASNs      int      `json:"added_asns"`
	UnresolvedASNs []string `json:"unresolved_asns,omitempty"`
}

const maxTargetFieldErrors = 50

func (api *API) RegisterGeoipApi() {
	api.mux.HandleFunc("/api/geoip", api.handleGeoIp)
}

func (a *API) handleGeoIp(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		a.getGeoIpTags(w)
	case http.MethodPut:
		a.AddGeoIpTag(w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// @Summary Add IP/CIDR blocks and ASNs to a set
// @Description Adds addresses (an IP or a CIDR, validated and masked) to targets.ips and AS numbers (AS15169 or 15169) to targets.asns of the set set_id, or of a new set placed first when set_id is empty. At least one of cidr and asns is required. An ASN without known prefixes is added anyway and resolved in the background; it is listed in unresolved_asns.
// @Tags GeoIP
// @Accept json
// @Produce json
// @Param body body AddGeoIpRequest true "Addresses and ASNs to add"
// @Success 200 {object} AddIpResponse
// @Failure 400 {object} APIError "code: cidr_invalid, asn_invalid, validation_failed or bad_request"
// @Failure 404 {object} APIError "code: not_found"
// @Security BearerAuth
// @Router /geoip [put]
func (a *API) AddGeoIpTag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req AddGeoIpRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeAPIError(w, ErrInvalidJSON())
		return
	}

	cidrs, fields := parseTargetCIDRs(req.Cidr)
	asns, asnFields := parseTargetASNs(req.ASNs)
	if apiErr := targetFieldsError(append(fields, asnFields...)); apiErr != nil {
		writeAPIError(w, apiErr)
		return
	}
	if len(cidrs) == 0 && len(asns) == 0 {
		writeAPIError(w, ErrBadRequest("cidr or asns is required"))
		return
	}

	setID := req.SetId
	create := setID == "" || setID == config.CreateSetSentinel
	if create {
		setID = uuid.New().String()
	}

	var result AddIpResponse
	oldCfg, newCfg, err := a.editConfig(func(next *config.Config) error {
		var set *config.SetConfig
		if create {
			fresh := config.NewSetConfig()
			fresh.Id = setID
			fresh.Name = strings.TrimSpace(req.SetName)
			if fresh.Name == "" {
				fresh.Name = fmt.Sprintf("Set %d", len(next.Sets)+1)
			}
			a.initializeSetDefaults(&fresh)
			set = &fresh
			next.Sets = append([]*config.SetConfig{set}, next.Sets...)
		} else if set = next.GetSetById(setID); set == nil {
			return ErrNotFound(fmt.Sprintf("Set %q not found", setID))
		}

		before := len(set.Targets.IPs)
		_ = set.Targets.AppendIP(cidrs)
		added, _ := set.Targets.AppendASNs(asns)
		result = AddIpResponse{
			SetId:          set.Id,
			TotalCidrs:     len(set.Targets.IPs),
			TotalASNs:      len(set.Targets.ASNs),
			AddedCidrs:     len(set.Targets.IPs) - before,
			AddedASNs:      len(added),
			UnresolvedASNs: unresolvedASNs(asns),
		}
		if create {
			a.loadTargetsForSetCached(set)
		}
		if !create && result.AddedCidrs == 0 && result.AddedASNs == 0 {
			return errConfigUnchanged
		}
		return nil
	})
	unchanged := errors.Is(err, errConfigUnchanged)
	if err != nil && !unchanged {
		if !isRefusal(err) {
			log.Errorf("Failed to add targets to set %s: %v", setID, err)
		}
		writeAPIError(w, err)
		return
	}

	if len(result.UnresolvedASNs) > 0 {
		config.RequestASNRefresh()
	}
	if !unchanged && a.PerformSoftRestart(newCfg, oldCfg) {
		log.Infof("Soft restart completed successfully")
	}

	log.Infof("Added %d addresses and %d ASNs to set %s", result.AddedCidrs, result.AddedASNs, result.SetId)
	result.Success = true
	result.Message = fmt.Sprintf("Added %d addresses and %d ASNs", result.AddedCidrs, result.AddedASNs)
	sendResponse(w, result)
}

func canonicalTargetCIDR(raw string) (string, bool) {
	if p, err := netip.ParsePrefix(raw); err == nil {
		return p.Masked().String(), true
	}
	if addr, err := netip.ParseAddr(raw); err == nil && addr.Zone() == "" {
		return addr.Unmap().String(), true
	}
	return "", false
}

func parseTargetCIDRs(raw []string) ([]string, []FieldError) {
	out := make([]string, 0, len(raw))
	seen := make(map[string]bool, len(raw))
	var fields []FieldError
	for i, entry := range raw {
		s := strings.TrimSpace(entry)
		if s == "" {
			continue
		}
		canon, ok := canonicalTargetCIDR(s)
		if !ok {
			fields = append(fields, FieldError{
				Path:    fmt.Sprintf("cidr[%d]", i),
				Code:    "cidr_invalid",
				Message: fmt.Sprintf("%q is not an IP address or CIDR", s),
				Params:  map[string]any{"value": s},
			})
			continue
		}
		if !seen[canon] {
			seen[canon] = true
			out = append(out, canon)
		}
	}
	return out, fields
}

func parseTargetASNs(raw []string) ([]string, []FieldError) {
	var valid []string
	var fields []FieldError
	for i, entry := range raw {
		s := strings.TrimSpace(entry)
		if s == "" {
			continue
		}
		if _, ok := config.NormalizeASN(s); !ok {
			fields = append(fields, FieldError{
				Path:    fmt.Sprintf("asns[%d]", i),
				Code:    "asn_invalid",
				Message: fmt.Sprintf("%q is not a public AS number", s),
				Params:  map[string]any{"value": s},
			})
			continue
		}
		valid = append(valid, s)
	}
	return config.NormalizeASNs(valid), fields
}

func targetFieldsError(fields []FieldError) *APIError {
	if len(fields) == 0 {
		return nil
	}
	code := fields[0].Code
	for _, f := range fields[1:] {
		if f.Code != code {
			code = "validation_failed"
			break
		}
	}
	msg := fmt.Sprintf("%d entries are not valid", len(fields))
	if len(fields) == 1 {
		msg = fields[0].Message
	}
	if len(fields) > maxTargetFieldErrors {
		fields = fields[:maxTargetFieldErrors]
	}
	return &APIError{Status: http.StatusBadRequest, Code: code, Message: msg, Fields: fields}
}

// @Summary List geoip categories
// @Tags GeoIP
// @Produce json
// @Success 200 {object} GeoipResponse
// @Security BearerAuth
// @Router /geoip [get]
func (a *API) getGeoIpTags(w http.ResponseWriter) {

	setJsonHeader(w)
	enc := json.NewEncoder(w)

	if !a.geodataManager.IsGeoipConfigured() {
		log.Tracef("Geoip path is not configured")
		_ = enc.Encode(GeoipResponse{Tags: []string{}})
		return
	}

	tags, err := a.geodataManager.ListCategories(a.geodataManager.GetGeoipPath())
	if err != nil {
		writeAPIError(w, ErrInternal("Failed to load geoip tags: "+err.Error()))
		return
	}

	response := GeoipResponse{
		Tags: tags,
	}

	_ = enc.Encode(response)
}
