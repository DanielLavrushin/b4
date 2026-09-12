package handler

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/daniellavrushin/b4/config"
	"github.com/daniellavrushin/b4/hub"
	"github.com/daniellavrushin/b4/hubwire"
	"github.com/daniellavrushin/b4/sni"
)

var globalHubService *hub.Service

func SetHubService(s *hub.Service) {
	globalHubService = s
}

func (api *API) hubService(w http.ResponseWriter) (*hub.Service, bool) {
	if !api.getCfg().System.Hub.Enabled {
		writeAPIError(w, &APIError{Status: http.StatusConflict, Code: "hub_disabled", Message: "The community hub is disabled"})
		return nil, false
	}
	svc := globalHubService
	if svc == nil {
		writeAPIError(w, &APIError{Status: http.StatusServiceUnavailable, Code: "hub_unavailable", Message: "The community hub service is not running"})
		return nil, false
	}
	if !svc.Configured() {
		writeAPIError(w, &APIError{Status: http.StatusConflict, Code: "hub_not_configured", Message: "No trusted hub key is configured"})
		return nil, false
	}
	return svc, true
}

func hubGetRequest(w http.ResponseWriter, r *http.Request) bool {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return false
	}
	return true
}

func writeHubError(w http.ResponseWriter, err error) {
	var he *hub.HubError
	switch {
	case errors.As(err, &he):
		setJsonHeader(w)
		w.WriteHeader(he.Status)
		_ = json.NewEncoder(w).Encode(HubRemoteError{Code: he.Code, Message: he.Error(), HubID: he.SetID, Version: he.Version, RetryAfter: he.RetryAfter})
	case errors.Is(err, hub.ErrUnreachable), errors.Is(err, hub.ErrShareNotQueued):
		writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "hub_unreachable", Message: err.Error()})
	case errors.Is(err, hub.ErrRecordTooBig):
		writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "record_too_big", Message: err.Error()})
	case errors.Is(err, hub.ErrNotConfigured):
		writeAPIError(w, &APIError{Status: http.StatusConflict, Code: "hub_not_configured", Message: err.Error()})
	default:
		writeAPIError(w, err)
	}
}

func writeHubBlobError(w http.ResponseWriter, ref hubwire.BlobRef, err error) {
	short := ref.SHA256
	if len(short) > 12 {
		short = short[:12]
	}
	switch {
	case errors.Is(err, hub.ErrBlobNotFound):
		writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "payload_missing", Message: "The hub does not have payload " + short})
	case errors.Is(err, hub.ErrBlobRef):
		writeAPIError(w, &APIError{Status: http.StatusBadRequest, Code: "payload_invalid", Message: err.Error()})
	case errors.Is(err, hub.ErrUnreachable):
		writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "hub_unreachable", Message: err.Error()})
	default:
		writeAPIError(w, &APIError{Status: http.StatusBadGateway, Code: "payload_invalid", Message: err.Error()})
	}
}

func hubOpenError(err error) *APIError {
	var pe *hubwire.PayloadError
	switch {
	case errors.Is(err, hubwire.ErrUnsupportedFormat):
		return &APIError{Status: http.StatusBadRequest, Code: "unsupported_format", Message: err.Error()}
	case errors.Is(err, hubwire.ErrNoSet):
		return &APIError{Status: http.StatusBadRequest, Code: "no_set", Message: err.Error()}
	case errors.As(err, &pe):
		return &APIError{Status: http.StatusBadRequest, Code: "payload_invalid", Message: err.Error()}
	default:
		return ErrBadRequest(err.Error())
	}
}

func hubAppliedIndex(cfg *config.Config) map[string]*HubApplied {
	index := map[string]*HubApplied{}
	for _, set := range cfg.Sets {
		if set == nil || set.Hub == nil || set.Hub.ID == "" {
			continue
		}
		state := hubStateOf(cfg, set)
		if state == "" {
			state = HubStateModified
		}
		if existing, ok := index[set.Hub.ID]; ok && existing.HubState == HubStateUnmodified {
			continue
		}
		index[set.Hub.ID] = &HubApplied{SetID: set.Id, SetName: set.Name, HubState: state, Version: set.Hub.Version, Vote: set.Hub.Vote, VotedAt: set.Hub.VotedAt}
	}
	return index
}

func hubSetByLocalID(cfg *config.Config, id string) *config.SetConfig {
	for _, set := range cfg.Sets {
		if set != nil && set.Id == id {
			return set
		}
	}
	return nil
}

func hubLocalSet(cfg *config.Config, hubID string) *config.SetConfig {
	var first *config.SetConfig
	for _, set := range cfg.Sets {
		if set == nil || set.Hub == nil || set.Hub.ID != hubID {
			continue
		}
		if hubStateOf(cfg, set) == HubStateUnmodified {
			return set
		}
		if first == nil {
			first = set
		}
	}
	return first
}

func nonNilStrings(list []string) []string {
	if list == nil {
		return []string{}
	}
	return list
}

func hubSetView(res hub.Result, applied map[string]*HubApplied) HubSet {
	cs := res.Set
	payloads := cs.Payloads
	if payloads == nil {
		payloads = []hubwire.BlobRef{}
	}
	view := HubSet{
		ID:          cs.ID,
		Version:     cs.Version,
		FP:          cs.FP,
		Title:       cs.Title,
		Description: cs.Description,
		Author:      cs.Author,
		B4Min:       cs.B4Min,
		B4Version:   cs.B4Version,
		Engine:      cs.Engine,
		Family:      cs.Family,
		Flags:       nonNilStrings(cs.Flags),
		Status:      cs.Status,
		CreatedAt:   cs.CreatedAt,
		UpdatedAt:   cs.UpdatedAt,
		Geo:         cs.Geo,
		Set:         cs.Set,
		Payloads:    payloads,
		Targets: HubTargets{
			Domains: nonNilStrings(hub.TargetList(cs.Set, "sni_domains")),
			GeoSite: nonNilStrings(hub.TargetList(cs.Set, "geosite_categories")),
			GeoIP:   nonNilStrings(hub.TargetList(cs.Set, "geoip_categories")),
			IPCount: len(hub.TargetList(cs.Set, "ip")),
		},
		Display: res.Display,
		Match:   res.Match,
		Applied: applied[cs.ID],
	}
	if view.Set == nil {
		view.Set = map[string]interface{}{}
	}
	return view
}

func firstPlainDomain(entries []string) string {
	for _, entry := range entries {
		value, isRegex := sni.ParseDomainEntry(entry)
		if isRegex || value == "" || strings.ContainsAny(value, "*/") {
			continue
		}
		return value
	}
	return ""
}

func hubProbeResultOf(r mcpProbeResult) HubProbeResult {
	return HubProbeResult{OK: r.OK, Status: r.Verdict, Detail: r.Error}
}
